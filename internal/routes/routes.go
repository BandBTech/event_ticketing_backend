package routes

import (
	"net/http"

	"event-ticketing-backend/docs" // Import generated docs
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

func SetupRouter() *gin.Engine {
	router := gin.Default()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	// Configure Swagger info dynamically based on environment
	docs.SwaggerInfo.BasePath = "/api/v1"
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

	// Middleware
	router.Use(middleware.RequestID()) // Add request ID to each request
	router.Use(middleware.Logger())
	router.Use(middleware.CORS())
	router.Use(middleware.RateLimiterMiddleware())
	router.Use(middleware.ErrorHandler())       // Custom panic recovery
	router.Use(middleware.GlobalErrorHandler()) // Handle remaining errors

	// Initialize services
	eventService := services.NewEventService()
	healthService := services.NewHealthService()

	// Initialize handlers
	healthHandler := handlers.NewHealthHandler(healthService)
	eventHandler := handlers.NewEventHandler(eventService)
	authHandler := handlers.NewAuthHandler(cfg)
	organizationHandler := handlers.NewOrganizationHandler(cfg)

	// Health routes - single comprehensive endpoint
	router.GET("/health", healthHandler.Health)

	// Swagger documentation - only available at /api/docs/ URL
	router.GET("/api/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Root docs URL redirects to index.html
	router.GET("/api/docs", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/api/docs/index.html")
	})

	// Test error handling endpoints (remove in production)
	router.GET("/test/panic", func(c *gin.Context) {
		panic("This is a test panic!")
	})

	router.GET("/test/app-error", func(c *gin.Context) {
		err := utils.NewNotFoundError("User")
		utils.HandleAppError(c, err)
	})

	// API v1 routes
	v1 := router.Group("/api/v1")
	{
		// Health route under API namespace
		v1.GET("/health", healthHandler.Health)

		// Auth routes (public)
		auth := v1.Group("/auth")
		auth.Use(middleware.AuthRateLimiter()) // Apply auth-specific rate limiting
		{
			// Regular auth endpoints
			auth.POST("/register", middleware.ValidateJSON(&models.CreateUserRequest{}), authHandler.Register)
			auth.POST("/login", middleware.ValidateJSON(&models.LoginRequest{}), authHandler.Login)

			// Password-related operations use stricter rate limiting
			passwordOps := auth.Group("")
			passwordOps.Use(middleware.PasswordRateLimiter())
			{
				passwordOps.POST("/reset-password-request", middleware.ValidateJSON(&models.ResetPasswordRequest{}), authHandler.ResetPasswordRequest)
				passwordOps.POST("/reset-password", middleware.ValidateJSON(&models.UpdatePasswordRequest{}), authHandler.ResetPassword)
			}

			// OTP-based verification endpoints (most restrictive)
			otpOps := auth.Group("")
			otpOps.Use(middleware.OTPRateLimiter())
			{
				otpOps.POST("/verify-otp", authHandler.VerifyOTP)
				otpOps.POST("/send-otp", authHandler.SendOTP)
			}

			// Token refresh with standard auth rate limiting
			auth.POST("/refresh", authHandler.RefreshToken)

			// Protected auth routes
			authProtected := auth.Group("")
			authProtected.Use(middleware.AuthMiddleware(cfg))
			{
				authProtected.POST("/logout", authHandler.Logout)
				authProtected.GET("/profile", authHandler.GetProfile)
				authProtected.PUT("/profile", authHandler.UpdateProfile)

				// Change password uses password rate limiter
				changePasswordGroup := authProtected.Group("")
				changePasswordGroup.Use(middleware.PasswordRateLimiter())
				{
					changePasswordGroup.POST("/change-password", authHandler.ChangePassword)
				}
			}
		}

		// Event routes
		events := v1.Group("/events")
		events.Use(middleware.EventRateLimiter()) // Apply event-specific rate limiting
		{
			// Public event routes
			events.GET("", eventHandler.GetAllEvents)
			events.GET("/:id", eventHandler.GetEventByID)

			// Protected event routes
			eventsProtected := events.Group("")
			eventsProtected.Use(middleware.AuthMiddleware(cfg))
			{
				// Events can be created by organizers and admins
				eventsProtected.POST("", middleware.IsOrganizer(), middleware.ValidateJSON(&models.EventCreateRequest{}), eventHandler.CreateEvent)
				eventsProtected.PUT("/:id", middleware.IsOrganizer(), middleware.ValidateJSON(&models.EventUpdateRequest{}), eventHandler.UpdateEvent)
				eventsProtected.DELETE("/:id", middleware.IsAdmin(), eventHandler.DeleteEvent)
			}
		}

		// Admin routes - only accessible by admin users
		admin := v1.Group("/admin")
		admin.Use(middleware.AuthMiddleware(cfg))
		admin.Use(middleware.IsAdmin())
		admin.Use(middleware.AdminRateLimiter()) // Apply admin-specific rate limiting
		{
			// Admin-only organization management
			admin.POST("/organizations", organizationHandler.CreateOrganization)
			admin.PUT("/organizations/:id", organizationHandler.UpdateOrganization)
			admin.DELETE("/organizations/:id", organizationHandler.DeleteOrganization)
			admin.GET("/organizations", organizationHandler.GetUserOrganizations) // TODO: Create GetAllOrganizations

			// Admin event management
			admin.GET("/events", eventHandler.GetAllEvents) // TODO: Create GetAllEventsAdmin with more details
			admin.DELETE("/events/:id", eventHandler.DeleteEvent)
		}

		// Organizer routes - accessible by organizers and admins
		organizer := v1.Group("/organizer")
		organizer.Use(middleware.AuthMiddleware(cfg))
		organizer.Use(middleware.IsOrganizer())
		{
			// Organizer organization management (their own organization)
			organizer.GET("/organizations", organizationHandler.GetUserOrganizations)
			organizer.GET("/organizations/:id", middleware.IsOrganizerOfOrganization(), organizationHandler.GetOrganizationByID)

			// Organizer user management (their organization only)
			orgUsers := organizer.Group("/organizations/:id")
			orgUsers.Use(middleware.IsOrganizerOfOrganization())
			{
				orgUsers.POST("/users", organizationHandler.CreateOrganizationUser)
				orgUsers.GET("/users", organizationHandler.GetOrganizationUsers)
				orgUsers.PUT("/users/:userId", organizationHandler.UpdateOrganizationUser)
				orgUsers.DELETE("/users/:userId", organizationHandler.DeleteOrganizationUser)
			}

			// Organizer event management
			organizer.POST("/events", eventHandler.CreateEvent)
			organizer.PUT("/events/:id", eventHandler.UpdateEvent)
			organizer.GET("/events", eventHandler.GetAllEvents) // TODO: Filter to organizer's events only
		}

		// User routes - accessible by all authenticated users
		user := v1.Group("/user")
		user.Use(middleware.AuthMiddleware(cfg))
		{
			// User profile management
			user.GET("/profile", authHandler.GetProfile)
			user.PUT("/profile", authHandler.UpdateProfile)
			user.POST("/change-password", authHandler.ChangePassword)

			// User event interactions
			user.GET("/events", eventHandler.GetAllEvents) // Same as public but could show personalized data
		}

		// Public routes - accessible without authentication
		public := v1.Group("/public")
		public.Use(middleware.PublicRateLimiter()) // Apply public-specific rate limiting (most permissive)
		{
			// Public event browsing
			public.GET("/events", eventHandler.GetAllEvents)
			public.GET("/events/:id", eventHandler.GetEventByID)
		}

		// Legacy organization routes (keep for backward compatibility)
		organizations := v1.Group("/organizations")
		organizations.Use(middleware.AuthMiddleware(cfg))
		{
			// Basic organization operations
			organizations.GET("", organizationHandler.GetUserOrganizations)
			organizations.GET("/:id", organizationHandler.GetOrganizationByID)
		}
	}

	return router
}
