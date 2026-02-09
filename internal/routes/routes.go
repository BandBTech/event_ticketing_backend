package routes

import (
	"context"
	"fmt"
	"net/http"

	"event-ticketing-backend/docs" // Import generated docs
	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/gateways"
	"event-ticketing-backend/internal/handlers"
	"event-ticketing-backend/internal/middleware"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"     // swagger embed files
	ginSwagger "github.com/swaggo/gin-swagger" // gin-swagger middleware
)

func SetupRouter(cfg *config.Config) *gin.Engine {
	router := gin.Default()

	// Configure Swagger info dynamically based on environment
	docs.SwaggerInfo.BasePath = "/"
	if cfg.App.Env == "local" || cfg.App.Env == "development" {
		docs.SwaggerInfo.Host = "localhost:" + cfg.App.Port
		docs.SwaggerInfo.Schemes = []string{"http"}
	} else if cfg.App.Env == "staging" {
		docs.SwaggerInfo.Host = "sandbox.timroticket.com"
		docs.SwaggerInfo.Schemes = []string{"https"}
	} else if cfg.App.Env == "production" {
		docs.SwaggerInfo.Host = "api.timroticket.com"
		docs.SwaggerInfo.Schemes = []string{"https"}
	}

	// Initialize rate limiters
	middleware.InitRateLimiters()

	// Initialize cache service
	cacheService := services.NewCacheService(nil)

	// Initialize caching middleware
	cachingMiddleware := middleware.NewCachingMiddleware(cacheService)

	// Middleware
	router.Use(middleware.RequestID()) // Add request ID to each request
	router.Use(middleware.Logger())
	router.Use(middleware.CORS(cfg))
	router.Use(middleware.RateLimiterMiddleware())
	router.Use(cachingMiddleware.CacheMiddleware())             // Strategic caching for high-traffic endpoints
	router.Use(cachingMiddleware.CacheInvalidationMiddleware()) // Auto cache invalidation
	router.Use(middleware.ErrorHandler())                       // Custom panic recovery
	router.Use(middleware.GlobalErrorHandler())                 // Handle remaining errors

	// Custom 404 handler
	router.NoRoute(func(c *gin.Context) {
		utils.NotFoundErrorResponse(c, "The requested resource was not found", nil)
	})

	// Initialize services
	eventService := services.NewEventService()
	healthService := services.NewHealthService()
	financialService := services.NewFinancialService(database.DB)
	authService := services.NewAuthService(cfg)
	ticketService := services.NewTicketService(database.DB, financialService, &cfg.JWT, cfg)

	// Initialize email and queue services
	emailQueueService := services.NewEmailQueueService(cfg)

	// Initialize secure QR service
	secureQRService := services.NewSecureQRService(cfg)

	// Set dependencies
	emailQueueService.SetSecureQRService(secureQRService)
	// Set secure QR service on ticket service so handlers can generate QR payloads
	ticketService.SetSecureQRService(secureQRService)

	// Set dependencies on ticket service
	ticketService.SetEmailQueueService(emailQueueService)
	ticketService.SetAuthService(authService)

	// Initialize file storage service
	s3Config := &models.S3Config{
		BucketName:      cfg.S3.BucketName,
		Region:          cfg.S3.Region,
		AccessKeyID:     cfg.S3.AccessKeyID,
		SecretAccessKey: cfg.S3.SecretAccessKey,
		PublicReadACL:   cfg.S3.PublicReadACL,
	}
	fileStorageService, err := services.NewFileStorageService(database.DB, s3Config)
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize file storage service: %v", err))
	}

	// Initialize payment gateway factory
	gatewayFactory := gateways.NewFactory(database.DB, cfg)
	// Optional: Initialize gateways from database configurations (can be managed via admin panel)
	// This will load any pre-configured gateways, but admin can add/update them later
	if err := gatewayFactory.InitializeGatewaysFromDB(context.Background()); err != nil {
		fmt.Printf("Info: No payment gateways configured yet. Configure via admin panel: %v\n", err)
		// Don't panic - gateways can be configured via admin panel
	}

	// Initialize payment service
	paymentService := services.NewPaymentService(database.DB, gatewayFactory, cfg)

	// Initialize handlers
	healthHandler := handlers.NewHealthHandler(healthService)
	eventHandler := handlers.NewEventHandler(eventService, fileStorageService)
	authHandler := handlers.NewAuthHandler(cfg)
	ticketHandler := handlers.NewTicketHandler(ticketService, cfg, secureQRService)
	financialHandler := handlers.NewFinancialHandler(financialService)
	permissionHandler := handlers.NewPermissionHandler()
	userManagementHandler := handlers.NewUserManagementHandler(authService, cfg)
	publicHandler := handlers.NewPublicHandler(ticketService, cfg)
	organizerOnboardingHandler := handlers.NewOrganizerOnboardingHandler(cfg, fileStorageService)
	organizerUserHandler := handlers.NewOrganizerUserHandler(authService)
	adminManagementHandler := handlers.NewAdminManagementHandler(fileStorageService, emailQueueService)
	dashboardHandler := handlers.NewDashboardHandler()
	paymentHandler := handlers.NewPaymentHandler(paymentService, cfg)

	// Health routes - single comprehensive endpoint
	router.GET("/health", healthHandler.Health)

	// Swagger documentation - only available at /api/docs/ URL
	router.GET("/api/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Root docs URL redirects to index.html
	router.GET("/api/docs", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/api/docs/index.html")
	})

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// Health route under API namespace
		v1.GET("/health", healthHandler.Health)

		// Auth routes - public endpoints for authentication
		auth := v1.Group("/auth")
		{
			// Public auth endpoints - no authentication required
			auth.POST("/user/register", authHandler.Register)               // User registration
			auth.POST("/organizer/register", authHandler.RegisterOrganizer) // Organizer registration
			auth.POST("/user/login", authHandler.UserLogin)                 // User-specific login
			auth.POST("/admin/login", authHandler.AdminLogin)               // Admin-specific login
			auth.POST("/organizer/login", authHandler.OrganizerLogin)       // Organizer-specific login
			auth.POST("/refresh", authHandler.RefreshToken)

			// Password reset endpoints for each user type
			auth.POST("/user/reset-password-request", authHandler.UserResetPasswordRequest)
			auth.POST("/admin/reset-password-request", authHandler.AdminResetPasswordRequest)
			auth.POST("/organizer/reset-password-request", authHandler.OrganizerResetPasswordRequest)

			auth.POST("/user/verify-otp", authHandler.UserVerifyOTP)
			auth.POST("/admin/verify-otp", authHandler.AdminVerifyOTP)
			auth.POST("/organizer/verify-otp", authHandler.OrganizerVerifyOTP)

			auth.POST("/user/send-otp", authHandler.UserSendOTP)
			auth.POST("/admin/send-otp", authHandler.AdminSendOTP)
			auth.POST("/organizer/send-otp", authHandler.OrganizerSendOTP)

			auth.POST("/user/reset-password", authHandler.UserResetPassword)
			auth.POST("/admin/reset-password", authHandler.AdminResetPassword)
			auth.POST("/organizer/reset-password", authHandler.OrganizerResetPassword)

			auth.POST("/user/set-password", authHandler.SetUserPassword)
			auth.POST("/organizer/set-password", authHandler.SetOrganizerPassword)

			// Protected auth endpoints - require authentication
			auth.Use(middleware.AuthMiddleware(cfg))
			{
				auth.POST("/logout", authHandler.Logout)
				auth.GET("/profile", authHandler.GetProfile)
				auth.PUT("/profile", authHandler.UpdateProfile)
				auth.POST("/change-password", authHandler.ChangePassword)
			}
		}

		// Public routes - no authentication required
		public := v1.Group("/public")
		{
			// Public event routes
			eventsPublic := public.Group("/events")
			{
				eventsPublic.GET("", eventHandler.PublicGetAllEvents)
				eventsPublic.GET("/:id", eventHandler.PublicGetEventByID)
				eventsPublic.GET("/featured", publicHandler.GetFeaturedEvents)
				eventsPublic.GET("/upcoming", publicHandler.GetUpcomingEvents)
				eventsPublic.GET("/search", publicHandler.SearchEvents)
				eventsPublic.GET("/category/:category", publicHandler.GetEventsByCategory)
			}

			// Public company info
			public.GET("/company-info", publicHandler.GetCompanyInfo)

			// Public categories
			public.GET("/categories", publicHandler.GetCategories)

			// Guest ticket purchase and verification
			public.POST("/tickets/guest-purchase", publicHandler.PurchaseTicketAsGuest)
			public.POST("/verify-guest", publicHandler.VerifyGuestEmail)
			public.GET("/guest/tickets", publicHandler.GuestGetTickets)

			// Payment gateway callbacks
			public.POST("/payment/success/:checkout_token", publicHandler.PaymentSuccessCallback)
			public.POST("/payment/failure/:checkout_token", publicHandler.PaymentFailureCallback)
			public.GET("/checkout/:checkout_token", publicHandler.GetCheckoutSession)

			// Secure ticket viewing with JWT token
			public.GET("/tickets/view", publicHandler.ViewTicket)
			public.GET("/tickets/validate-token", publicHandler.ValidateTicketToken)
		}

		// Payment routes - accessible to both guests and authenticated users
		payments := v1.Group("/payments")
		{
			// Public payment endpoints (no auth required)
			payments.GET("/gateways", paymentHandler.GetAvailableGateways) // Get available payment gateways
			payments.POST("/initiate", paymentHandler.InitiatePayment)     // Initiate payment (works for both guest and auth users)

			// Webhook endpoints for payment gateways (no auth required)
			// Dynamic webhook route - supports any gateway configured in the system
			webhooks := v1.Group("/webhooks")
			{
				// Primary dynamic webhook endpoint - works for all gateways
				webhooks.POST("/:gateway", paymentHandler.HandleWebhook)

				// Legacy endpoints for backward compatibility (deprecated)
				// These will be removed in a future version
				// webhooks.POST("/stripe", paymentHandler.HandleStripeWebhook)
				// webhooks.POST("/paypal", paymentHandler.HandlePayPalWebhook)
			}
		}

		// User routes - regular users only (broad access control)
		user := v1.Group("/user")
		user.Use(middleware.AuthMiddleware(cfg))
		user.Use(middleware.IsUser()) // Broad: only regular users can access user area
		{
			// User ticket management (fine-grained permissions within user area)
			userTickets := user.Group("/tickets")
			{
				userTickets.POST("/purchase", middleware.RequirePermission("create:ticket"), ticketHandler.UserPurchaseTicket)
				userTickets.GET("", middleware.RequirePermission("read:ticket"), ticketHandler.UserGetTickets)
				userTickets.GET("/:id", middleware.RequirePermission("read:ticket"), ticketHandler.UserGetTicketByID)
				userTickets.GET("/:id/qr", middleware.RequirePermission("read:ticket"), ticketHandler.UserGetTicketQR)
				userTickets.GET("/stats", middleware.RequirePermission("read:ticket"), ticketHandler.UserGetTicketStats)
			}

			// User event tickets (tickets for specific events)
			userEvents := user.Group("/events")
			{
				userEvents.GET("/:event_id/tickets", middleware.RequirePermission("read:ticket"), ticketHandler.UserGetEventTickets)
			}

			// User transactions
			user.GET("/transactions", middleware.RequirePermission("read:transaction"), financialHandler.GetUserTransactions)

			// User payment management
			userPayments := user.Group("/payments")
			{
				userPayments.GET("", middleware.RequirePermission("read:ticket"), paymentHandler.GetUserPayments)                            // Get user's payment history
				userPayments.GET("/:payment_intent_id", middleware.RequirePermission("read:ticket"), paymentHandler.GetPaymentStatus)        // Get specific payment status
				userPayments.POST("/:payment_intent_id/cancel", middleware.RequirePermission("create:ticket"), paymentHandler.CancelPayment) // Cancel pending payment
				userPayments.POST("/refund", middleware.RequirePermission("create:ticket"), paymentHandler.RequestRefund)                    // Request refund
			}
		}

		// Admin routes - admin and subadmin access (broad access control)
		admin := v1.Group("/admin")
		admin.Use(middleware.AuthMiddleware(cfg))
		admin.Use(middleware.IsAdminOrSubAdmin()) // Broad: admin/subadmin can access admin area
		{
			// Admin dashboard
			admin.GET("/dashboard", dashboardHandler.GetAdminDashboard)

			// Admin event management (fine-grained permissions within admin area)
			adminEvents := admin.Group("/events")
			{
				adminEvents.GET("", middleware.RequirePermission("read:event"), eventHandler.AdminGetAllEvents)
				adminEvents.GET("/:id", middleware.RequirePermission("read:event"), eventHandler.AdminGetEventByID)
				adminEvents.GET("/pending", middleware.RequirePermission("read:event"), eventHandler.AdminGetEventsForApproval)
				adminEvents.PUT("/:id/status", middleware.RequirePermission("approve:event"), eventHandler.AdminUpdateEventStatus)
				adminEvents.PUT("/:id", middleware.RequirePermission("update:event"), eventHandler.AdminUpdateEvent)
				adminEvents.DELETE("/:id", middleware.RequirePermission("delete:event"), eventHandler.AdminDeleteEvent)
				adminEvents.GET("/:id/analytics", middleware.RequirePermission("read:event"), eventHandler.AdminGetEventAnalytics)
				adminEvents.PUT("/:id/cancel", middleware.RequirePermission("update:event"), eventHandler.CancelEvent)
				adminEvents.PUT("/:id/featured", middleware.RequirePermission("update:event"), adminManagementHandler.ToggleEventFeatured)
				adminEvents.GET("/:id/status-history", middleware.RequirePermission("read:event"), eventHandler.AdminGetEventStatusHistory)
			}

			// Admin organizer management
			adminOrganizers := admin.Group("/organizers")
			{
				adminOrganizers.GET("", middleware.RequirePermission("read:user"), authHandler.GetAllOrganizers)
				adminOrganizers.GET("/:id", middleware.RequirePermission("read:user"), authHandler.GetOrganizerByID)
				adminOrganizers.GET("/pending", middleware.RequirePermission("read:user"), authHandler.GetPendingOrganizers)
				adminOrganizers.PUT("/:id/approval", middleware.RequirePermission("approve:organizer"), authHandler.ApproveOrganizer)
			}

			// Admin OTP debugging
			adminOTP := admin.Group("/otp")
			{
				adminOTP.GET("/status", middleware.RequirePermission("read:user"), authHandler.GetOTPStatus)
			}

			// Admin payout management
			adminPayouts := admin.Group("/payouts")
			{
				adminPayouts.GET("", middleware.RequirePermission("read:payout"), eventHandler.GetAllPayoutRequests)
				adminPayouts.PUT("/:id/status", middleware.RequirePermission("update:payout"), eventHandler.UpdatePayoutRequestStatus)
			}

			// Admin user management
			adminUsers := admin.Group("/users")
			{
				adminUsers.GET("", middleware.RequirePermission("read:user"), userManagementHandler.GetAllUsers)
				adminUsers.GET("/statistics", middleware.RequirePermission("read:user"), userManagementHandler.GetUserStatistics)
				adminUsers.GET("/:id", middleware.RequirePermission("read:user"), userManagementHandler.GetUserByID)
				adminUsers.PUT("/:id/promote", middleware.RequirePermission("update:user"), userManagementHandler.PromoteUser)
				adminUsers.PUT("/:id/status", middleware.RequirePermission("update:user"), userManagementHandler.UpdateAccountStatus)
				adminUsers.DELETE("/:id", middleware.RequirePermission("delete:user"), userManagementHandler.SoftDeleteUser)
				adminUsers.DELETE("/:id/delete", middleware.RequirePermission("delete:user"), userManagementHandler.DeleteUser)
				adminUsers.PUT("/:id/restore", middleware.RequirePermission("update:user"), userManagementHandler.RestoreUser)
				adminUsers.POST("/bulk-action", middleware.RequirePermission("update:user"), userManagementHandler.BulkUserAction)
				adminUsers.POST("/organizers", middleware.RequirePermission("create:user"), userManagementHandler.AdminCreateOrganizer)
				adminUsers.GET("/:id/permissions", middleware.RequirePermission("read:user"), permissionHandler.GetUserPermissions)
				adminUsers.GET("/:id/permissions/check", middleware.RequirePermission("read:user"), permissionHandler.CheckUserPermission)
			}

			// Admin company info management
			adminCompany := admin.Group("/company-info")
			{
				adminCompany.GET("", middleware.RequirePermission("read:user"), adminManagementHandler.GetCompanyInfo)
				adminCompany.PUT("", middleware.RequirePermission("update:user"), adminManagementHandler.UpdateCompanyInfo)
			}

			// Admin category management
			adminCategories := admin.Group("/categories")
			{
				adminCategories.GET("", middleware.RequirePermission("read:user"), adminManagementHandler.GetAllCategories)
				adminCategories.POST("", middleware.RequirePermission("create:user"), adminManagementHandler.CreateCategory)
				adminCategories.PUT("/:id", middleware.RequirePermission("update:user"), adminManagementHandler.UpdateCategory)
				adminCategories.DELETE("/:id", middleware.RequirePermission("delete:user"), adminManagementHandler.DeleteCategory)
			}

			// Admin ticket template testing
			admin.POST("/test-ticket", middleware.RequirePermission("event:create"), adminManagementHandler.TestTicketTemplate)

			// Admin permission initialization (accessible to any admin/subadmin - needed to bootstrap permissions)
			admin.POST("/permissions/initialize", middleware.RequirePermission("create:user"), permissionHandler.InitializeSystemPermissions)
			admin.POST("/permissions/initialize-roles", middleware.RequirePermission("create:user"), permissionHandler.InitializeSystemRoles)
			admin.POST("/permissions/initialize-system", middleware.RequirePermission("create:user"), permissionHandler.InitializeSystem)

			// Admin permission management (Admin only - subadmin restricted)
			adminOnlyRoutes := admin.Group("")
			adminOnlyRoutes.Use(middleware.RequirePermission("admin:full")) // Only full admins
			{
				// Permission management
				adminPermissions := adminOnlyRoutes.Group("/permissions")
				{
					adminPermissions.GET("", permissionHandler.GetAllPermissions)
					adminPermissions.POST("", middleware.RequirePermission("create:user"), permissionHandler.CreatePermission)
					adminPermissions.PUT("/:id", middleware.RequirePermission("update:user"), permissionHandler.UpdatePermission)
					adminPermissions.DELETE("/:id", middleware.RequirePermission("delete:user"), permissionHandler.DeletePermission)
				}

				// Role permission management
				adminRolePermissions := adminOnlyRoutes.Group("/roles")
				{
					adminRolePermissions.GET("", permissionHandler.GetAllRoles)
					adminRolePermissions.GET("/:roleId/permissions", permissionHandler.GetRolePermissions)
					adminRolePermissions.POST("/:roleId/permissions", middleware.RequirePermission("update:user"), permissionHandler.AssignPermissionsToRole)
				}
			}

			// Admin financial management (RESTRICTED for subadmin)
			adminFinancial := admin.Group("/financial")
			adminFinancial.Use(middleware.RequirePermission("read:financial")) // Subadmin blocked here
			{
				// Financial summary and overview
				adminFinancial.GET("/summary", financialHandler.GetAdminFinancialSummary)

				// Event sales management
				adminFinancial.GET("/sales", financialHandler.GetAllEventSales)

				// Payment bills management
				adminFinancial.GET("/bills", financialHandler.GetAllPaymentBills)
				adminFinancial.POST("/bills", middleware.RequirePermission("create:financial"), financialHandler.CreatePaymentBill)
				adminFinancial.GET("/bills/:bill_id", financialHandler.GetPaymentBillByID)
				adminFinancial.PUT("/bills/:bill_id", middleware.RequirePermission("update:financial"), financialHandler.UpdatePaymentBill)

				// Organizer-specific financial data
				adminFinancial.GET("/organizers/:organizer_id/summary", financialHandler.GetSpecificOrganizerFinancialSummary)
				adminFinancial.GET("/organizers/:organizer_id/sales", financialHandler.GetSpecificOrganizerSales)
				adminFinancial.GET("/transactions", financialHandler.GetAllTransactions)
			}

			// Admin payment management
			adminPayments := admin.Group("/payments")
			adminPayments.Use(middleware.RequirePermission("read:financial"))
			{
				adminPayments.GET("", paymentHandler.AdminGetAllPayments)                      // Get all payments with filters
				adminPayments.GET("/analytics", paymentHandler.GetPaymentAnalytics)            // Payment analytics
				adminPayments.GET("/audit-logs", paymentHandler.GetAuditLogs)                  // Query audit logs
				adminPayments.POST("/webhooks/:id/retry", paymentHandler.RetryWebhook)         // Retry failed webhook
				adminPayments.POST("/transactions/:id/retry", paymentHandler.RetryTransaction) // Retry failed transaction
			}

			// Admin payment gateway configuration management (Admin only)
			adminGateways := admin.Group("/payment-gateways")
			adminGateways.Use(middleware.RequirePermission("manage:payment_gateway"))
			{
				adminGateways.GET("", paymentHandler.AdminManageGatewayConfigs)          // List (paginated) or Get supported types with ?type=supported
				adminGateways.POST("", paymentHandler.AdminCreateGatewayConfig)          // Create new gateway
				adminGateways.GET("/:gateway_id", paymentHandler.AdminGetGatewayByID)    // Get specific gateway
				adminGateways.PUT("/:gateway_id", paymentHandler.AdminUpdateGateway)     // Update gateway config or credentials
				adminGateways.DELETE("/:gateway_id", paymentHandler.AdminDeleteGateway)  // Delete gateway
				adminGateways.PATCH("/:gateway_id", paymentHandler.AdminGatewayAction)   // Actions: ?action=toggle|test|reload|validate
				adminGateways.POST("/reencrypt", paymentHandler.ReencryptGatewayConfigs) // Re-encrypt all gateway configs (key rotation)
			}

			// Admin refund management
			adminRefunds := admin.Group("/refunds")
			adminRefunds.Use(middleware.RequirePermission("update:financial"))
			{
				adminRefunds.GET("", paymentHandler.AdminGetAllRefunds)                     // Get all refunds
				adminRefunds.POST("/:refund_id/approve", paymentHandler.AdminApproveRefund) // Approve refund
				adminRefunds.POST("/:refund_id/reject", paymentHandler.AdminRejectRefund)   // Reject refund
			}
		}

		// Organizer routes - organizer access (broad access control)
		organizer := v1.Group("/organizer")
		organizer.Use(middleware.AuthMiddleware(cfg))

		// Profile management routes - accessible to organizers regardless of approval status
		// These endpoints are needed for onboarding and profile completion
		organizerProfile := organizer.Group("")
		organizerProfile.Use(middleware.RequirePermission("view:profile"))
		{
			// Organizer onboarding and profile management
			organizerProfile.GET("/status", organizerOnboardingHandler.GetOnboardingStatus)
			organizerProfile.GET("/profile", organizerOnboardingHandler.GetProfile)
			organizerProfile.PUT("/profile", middleware.RequirePermission("update:profile"), organizerOnboardingHandler.UpdateProfile)
		}

		// Approved organizer routes - require approval status (broad access control)
		approvedOrganizer := organizer.Group("")
		approvedOrganizer.Use(middleware.IsApprovedOrganizerOrManager(cfg)) // Broad: approved organizers OR managers OR staff can access organizer area
		{
			// Organizer dashboard
			approvedOrganizer.GET("/dashboard", dashboardHandler.GetOrganizerDashboard)

			// Organizer event management (fine-grained permissions within organizer area)
			organizerEvents := approvedOrganizer.Group("/events")
			{
				// Viewing routes
				organizerEvents.GET("", middleware.RequirePermission("read:event"), eventHandler.OrganizerGetAllEvents)
				organizerEvents.GET("/:id", middleware.RequirePermission("read:event"), eventHandler.OrganizerGetEventByID)
				organizerEvents.GET("/:id/analytics", middleware.RequirePermission("read:event"), eventHandler.GetEventAnalytics)
				organizerEvents.GET("/:id/status-history", middleware.RequirePermission("read:event"), eventHandler.OrganizerGetEventStatusHistory)

				// Modification routes - typically organizer only
				organizerEvents.POST("", middleware.RequirePermission("create:event"), eventHandler.OrganizerCreateEvent)
				organizerEvents.PUT("/:id", middleware.RequirePermission("update:event"), eventHandler.OrganizerUpdateEventByID)
				organizerEvents.DELETE("/:id", middleware.RequirePermission("delete:event"), eventHandler.OrganizerDeleteEventByID)
				organizerEvents.PUT("/:id/cancel", middleware.RequirePermission("update:event"), eventHandler.CancelEvent)

				// Sales control - allow both organizers and managers
				organizerEvents.PUT("/:id/sales/control", middleware.RequirePermission("update:event"), eventHandler.ControlEventSales)

				// Tier template management - organizer only
				organizerEvents.GET("/tier-templates", middleware.RequirePermission("read:event"), eventHandler.GetOrganizerTierTemplates)
				organizerEvents.POST("/tier-templates", middleware.RequirePermission("create:event"), eventHandler.CreateOrganizerTierTemplate)
				organizerEvents.PUT("/tier-templates/:templateId", middleware.RequirePermission("update:event"), eventHandler.UpdateOrganizerTierTemplate)
				organizerEvents.DELETE("/tier-templates/:templateId", middleware.RequirePermission("delete:event"), eventHandler.DeleteOrganizerTierTemplate)
			}

			// Organizer user management
			organizerUsers := approvedOrganizer.Group("/users")
			{
				organizerUsers.GET("", middleware.RequirePermission("read:user"), organizerUserHandler.GetOrganizerUsers)
				organizerUsers.POST("", middleware.RequirePermission("create:user"), organizerUserHandler.CreateOrganizerUser)
				organizerUsers.PUT("/:user_id", middleware.RequirePermission("update:user"), organizerUserHandler.UpdateOrganizerUser)
				organizerUsers.DELETE("/:user_id", middleware.RequirePermission("delete:user"), organizerUserHandler.DeleteOrganizerUser)
			}

			// Organizer analytics
			organizerAnalytics := approvedOrganizer.Group("/analytics")
			{
				organizerAnalytics.GET("/events", middleware.RequirePermission("read:event"), eventHandler.GetAllEventsAnalytics)
			}

			// Organizer payout management
			organizerPayouts := approvedOrganizer.Group("/payouts")
			{
				organizerPayouts.POST("", middleware.RequirePermission("create:payout"), eventHandler.CreatePayoutRequest)
				organizerPayouts.GET("", middleware.RequirePermission("read:payout"), eventHandler.GetOrganizerPayoutRequests)
				organizerPayouts.GET("/summary", middleware.RequirePermission("read:payout"), eventHandler.GetPayoutSummary)
			}

			// Organizer ticket management
			organizerTickets := approvedOrganizer.Group("/tickets")
			{
				organizerTickets.POST("/scan", middleware.RequirePermission("scan:ticket"), ticketHandler.OrganizerScanTicket)
				organizerTickets.POST("/checkin", middleware.RequirePermission("checkin:ticket"), ticketHandler.OrganizerCheckInTicket)
				organizerTickets.POST("/checkout", middleware.RequirePermission("checkout:ticket"), ticketHandler.OrganizerCheckOutTicket)
				organizerTickets.POST("/bulk-checkin", middleware.RequirePermission("checkin:ticket"), ticketHandler.OrganizerBulkCheckInTickets)
				organizerTickets.POST("/bulk-checkout", middleware.RequirePermission("checkout:ticket"), ticketHandler.OrganizerBulkCheckOutTickets)
			}

			// Organizer event tickets
			organizerEventTickets := approvedOrganizer.Group("/events")
			{
				organizerEventTickets.GET("/:id/tickets", middleware.RequirePermission("read:ticket"), ticketHandler.OrganizerGetEventTickets)
				organizerEventTickets.GET("/:id/tickets/stats", middleware.RequirePermission("read:ticket"), ticketHandler.OrganizerGetTicketStats)
			}

			// Organizer financial management
			organizerFinancial := approvedOrganizer.Group("/financial")
			{
				organizerFinancial.GET("/summary", middleware.RequirePermission("summary:financial"), financialHandler.GetOrganizerFinancialSummary)
				organizerFinancial.GET("/sales", middleware.RequirePermission("sales:financial"), financialHandler.GetOrganizerSales)
				organizerFinancial.GET("/bills", middleware.RequirePermission("bills:financial"), financialHandler.GetOrganizerPaymentBills)
			}
		}
	}

	return router
}
