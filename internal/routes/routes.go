package routes

import (
	"fmt"
	"net/http"

	"event-ticketing-backend/docs" // Import generated docs
	"event-ticketing-backend/internal/database"
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
	ticketService := services.NewTicketService(database.DB, financialService)

	// Initialize email and queue services
	emailQueueService := services.NewEmailQueueService(cfg)

	// Initialize universal ticket template service
	universalTicketTemplateService := services.NewUniversalTicketTemplateService()

	// Set dependencies on ticket service
	ticketService.SetEmailQueueService(emailQueueService)
	ticketService.SetUniversalTicketTemplateService(universalTicketTemplateService)
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

	// Initialize handlers
	healthHandler := handlers.NewHealthHandler(healthService)
	eventHandler := handlers.NewEventHandler(eventService, fileStorageService)
	authHandler := handlers.NewAuthHandler(cfg)
	organizationHandler := handlers.NewOrganizationHandler(cfg, authService)
	ticketHandler := handlers.NewTicketHandler(ticketService, cfg)
	financialHandler := handlers.NewFinancialHandler(financialService)
	eventManagementHandler := handlers.NewEventManagementHandler()
	permissionHandler := handlers.NewPermissionHandler()
	userManagementHandler := handlers.NewUserManagementHandler(authService, cfg)
	publicHandler := handlers.NewPublicHandler(ticketService)
	organizerOnboardingHandler := handlers.NewOrganizerOnboardingHandler(cfg, fileStorageService)
	adminManagementHandler := handlers.NewAdminManagementHandler(fileStorageService, universalTicketTemplateService, emailQueueService)

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
		}

		// User routes - regular users only
		user := v1.Group("/user")
		user.Use(middleware.AuthMiddleware(cfg))
		user.Use(middleware.IsUser()) // Only regular users
		{
			// User ticket management
			userTickets := user.Group("/tickets")
			{
				userTickets.GET("", ticketHandler.UserGetTickets)
				userTickets.GET("/:id", ticketHandler.UserGetTicketByID)
				userTickets.GET("/:id/qr", ticketHandler.UserGetTicketQR)
				userTickets.GET("/stats", ticketHandler.UserGetTicketStats)
			}

			// User event tickets (tickets for specific events)
			userEvents := user.Group("/events")
			{
				userEvents.GET("/:event_id/tickets", ticketHandler.UserGetEventTickets)
			}
		}

		// Admin routes - admin and subadmin access
		admin := v1.Group("/admin")
		admin.Use(middleware.AuthMiddleware(cfg))
		admin.Use(middleware.IsAdminOrSubAdmin())
		{
			// Admin event management
			adminEvents := admin.Group("/events")
			{
				adminEvents.GET("", eventHandler.AdminGetAllEvents) // Admin can view all events
				adminEvents.GET("/pending", eventHandler.AdminGetEventsForApproval)
				adminEvents.PUT("/:id/approval", eventHandler.AdminApproveEvent)
				adminEvents.POST("", eventHandler.AdminCreateEvent) // Admin can create events
				adminEvents.PUT("/:id", eventHandler.AdminUpdateEvent)
				adminEvents.DELETE("/:id", eventHandler.AdminDeleteEvent)

				// Admin event management (enhanced)
				adminEvents.GET("/:id/analytics", eventManagementHandler.GetEventAnalytics)
				adminEvents.POST("/:id/cancel", eventManagementHandler.CancelEvent)
				adminEvents.PUT("/:id/featured", adminManagementHandler.ToggleEventFeatured)
			}

			// Admin organizer management
			adminOrganizers := admin.Group("/organizers")
			{
				adminOrganizers.GET("", authHandler.GetAllOrganizers)
				adminOrganizers.GET("/pending", authHandler.GetPendingOrganizers)
				adminOrganizers.PUT("/:id/approval", authHandler.ApproveOrganizer)
			}

			// Admin OTP debugging
			adminOTP := admin.Group("/otp")
			{
				adminOTP.GET("/status", authHandler.GetOTPStatus)
			}

			// Admin payout management
			adminPayouts := admin.Group("/payouts")
			{
				adminPayouts.GET("", eventManagementHandler.GetAllPayoutRequests)
				adminPayouts.PUT("/:id/status", eventManagementHandler.UpdatePayoutRequestStatus)
			}

			// Admin user management
			adminUsers := admin.Group("/users")
			{
				adminUsers.GET("", userManagementHandler.GetAllUsers)
				adminUsers.GET("/statistics", userManagementHandler.GetUserStatistics)
				adminUsers.GET("/:id", userManagementHandler.GetUserByID)
				adminUsers.PUT("/:id/promote", userManagementHandler.PromoteUser)
				adminUsers.PUT("/:id/status", userManagementHandler.UpdateAccountStatus)
				adminUsers.DELETE("/:id", userManagementHandler.SoftDeleteUser)
				adminUsers.DELETE("/:id/delete", userManagementHandler.DeleteUser)
				adminUsers.PUT("/:id/restore", userManagementHandler.RestoreUser)
				adminUsers.POST("/bulk-action", userManagementHandler.BulkUserAction)
				adminUsers.POST("/organizers", userManagementHandler.AdminCreateOrganizer)
				adminUsers.GET("/:id/permissions", permissionHandler.GetUserPermissions)
				adminUsers.GET("/:id/permissions/check", permissionHandler.CheckUserPermission)
			}

			// Admin company info management
			adminCompany := admin.Group("/company-info")
			{
				adminCompany.GET("", adminManagementHandler.GetCompanyInfo)
				adminCompany.PUT("", adminManagementHandler.UpdateCompanyInfo)
			}

			// Admin category management
			adminCategories := admin.Group("/categories")
			{
				adminCategories.GET("", adminManagementHandler.GetAllCategories)
				adminCategories.POST("", adminManagementHandler.CreateCategory)
				adminCategories.PUT("/:id", adminManagementHandler.UpdateCategory)
				adminCategories.DELETE("/:id", adminManagementHandler.DeleteCategory)
			}

			// Admin ticket template testing
			admin.POST("/test-ticket", adminManagementHandler.TestTicketTemplate)

			// Admin permission management (Admin only)
			adminOnlyRoutes := admin.Group("")
			// adminOnlyRoutes.Use(middleware.IsAdmin()) // Only main admin, not subadmin
			{
				// Permission management
				adminPermissions := adminOnlyRoutes.Group("/permissions")
				{
					adminPermissions.GET("", permissionHandler.GetAllPermissions)
					adminPermissions.POST("/initialize", permissionHandler.InitializeSystemPermissions)
					adminPermissions.POST("", permissionHandler.CreatePermission)
					adminPermissions.PUT("/:id", permissionHandler.UpdatePermission)
					adminPermissions.DELETE("/:id", permissionHandler.DeletePermission)
				}

				// Role permission management
				adminRolePermissions := adminOnlyRoutes.Group("/roles")
				{
					adminRolePermissions.GET("/:roleId/permissions", permissionHandler.GetRolePermissions)
					adminRolePermissions.POST("/:roleId/permissions", permissionHandler.AssignPermissionsToRole)
				}

				// Organization management (Admin only - requires higher permission)
				adminOnlyRoutes.POST("/organizer", organizationHandler.CreateOrganization)
				adminOnlyRoutes.PUT("/organizer/:id", organizationHandler.UpdateOrganization)
				adminOnlyRoutes.DELETE("/organizer/:id", organizationHandler.DeleteOrganization)
			}

			// Admin financial management
			adminFinancial := admin.Group("/financial")
			{
				// Financial summary and overview
				adminFinancial.GET("/summary", financialHandler.GetAdminFinancialSummary)

				// Event sales management
				adminFinancial.GET("/sales", financialHandler.GetAllEventSales)

				// Payment bills management
				adminFinancial.GET("/bills", financialHandler.GetAllPaymentBills)
				adminFinancial.POST("/bills", financialHandler.CreatePaymentBill)
				adminFinancial.GET("/bills/:bill_id", financialHandler.GetPaymentBillByID)
				adminFinancial.PUT("/bills/:bill_id", financialHandler.UpdatePaymentBill)

				// Organizer-specific financial data
				adminFinancial.GET("/organizers/:organizer_id/summary", financialHandler.GetSpecificOrganizerFinancialSummary)
				adminFinancial.GET("/organizers/:organizer_id/sales", financialHandler.GetSpecificOrganizerSales)
			}
		}

		// Organizer routes - split into profile management and approved organizer features
		organizer := v1.Group("/organizer")
		organizer.Use(middleware.AuthMiddleware(cfg))

		// Profile management routes - accessible to organizers regardless of approval status
		// These endpoints are needed for onboarding and profile completion
		organizerProfile := organizer.Group("")
		organizerProfile.Use(middleware.IsOrganizerRole(cfg))
		{
			// Organizer onboarding and profile management
			organizerProfile.GET("/status", organizerOnboardingHandler.GetOnboardingStatus)
			organizerProfile.GET("/profile", organizerOnboardingHandler.GetProfile)
			organizerProfile.PUT("/profile", organizerOnboardingHandler.UpdateProfile)
		}

		// Approved organizer routes - require approval status
		approvedOrganizer := organizer.Group("")
		approvedOrganizer.Use(middleware.IsApprovedOrganizer(cfg))
		{

			// Organizer event management
			organizerEvents := approvedOrganizer.Group("/events")
			{
				organizerEvents.GET("", eventHandler.OrganizerGetEvents)
				organizerEvents.POST("", eventHandler.OrganizerCreateEvent)
				organizerEvents.PUT("/:id", eventHandler.OrganizerUpdateEvent)
				organizerEvents.DELETE("/:id", eventHandler.OrganizerDeleteEvent) // Organizer can delete their own events

				// Enhanced event management
				organizerEvents.PUT("/:id/sales", eventManagementHandler.ControlEventSales)
				organizerEvents.GET("/:id/analytics", eventManagementHandler.GetEventAnalytics)
				organizerEvents.POST("/:id/cancel", eventManagementHandler.CancelEvent)

				// Tier template management
				organizerEvents.GET("/tier-templates", eventManagementHandler.GetOrganizerTierTemplates)
				organizerEvents.POST("/tier-templates", eventManagementHandler.CreateOrganizerTierTemplate)
				organizerEvents.PUT("/tier-templates/:templateId", eventManagementHandler.UpdateOrganizerTierTemplate)
				organizerEvents.DELETE("/tier-templates/:templateId", eventManagementHandler.DeleteOrganizerTierTemplate)
			}

			// Organizer analytics
			organizerAnalytics := approvedOrganizer.Group("/analytics")
			{
				organizerAnalytics.GET("/events", eventManagementHandler.GetAllEventsAnalytics)
			}

			// Organizer payout management
			organizerPayouts := approvedOrganizer.Group("/payouts")
			{
				organizerPayouts.POST("", eventManagementHandler.CreatePayoutRequest)
				organizerPayouts.GET("", eventManagementHandler.GetOrganizerPayoutRequests)
				organizerPayouts.GET("/summary", eventManagementHandler.GetPayoutSummary)
			}

			// Organizer ticket management (for staff/managers)
			organizerTickets := approvedOrganizer.Group("/tickets")
			{
				organizerTickets.POST("/scan", ticketHandler.OrganizerScanTicket)
				organizerTickets.POST("/checkin", ticketHandler.OrganizerCheckInTicket)
				organizerTickets.POST("/checkout", ticketHandler.OrganizerCheckOutTicket)
			}

			// Organizer event tickets (for organizers to view their event tickets)
			organizerEventTickets := approvedOrganizer.Group("/events")
			{
				organizerEventTickets.GET("/:id/tickets", ticketHandler.OrganizerGetEventTickets)
				organizerEventTickets.GET("/:id/tickets/stats", ticketHandler.OrganizerGetTicketStats)
			}

			// Organizer financial management
			organizerFinancial := approvedOrganizer.Group("/financial")
			{
				organizerFinancial.GET("/summary", financialHandler.GetOrganizerFinancialSummary)
				organizerFinancial.GET("/sales", financialHandler.GetOrganizerSales)
				organizerFinancial.GET("/bills", financialHandler.GetOrganizerPaymentBills)
			}
		}
	}

	return router
}
