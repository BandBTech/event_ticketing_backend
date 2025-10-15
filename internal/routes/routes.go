package routes

import (
	"net/http"

	"event-ticketing-backend/docs" // Import generated docs
	"event-ticketing-backend/internal/handlers"
	"event-ticketing-backend/internal/middleware"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"     // swagger embed files
	ginSwagger "github.com/swaggo/gin-swagger" // gin-swagger middleware
)

func SetupRouter() *gin.Engine {
	router := gin.Default()

	// Configure Swagger info
	docs.SwaggerInfo.BasePath = "/api/v1"

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	// Initialize rate limiters
	middleware.InitRateLimiters()

	// Middleware
	router.Use(middleware.Logger())
	router.Use(middleware.CORS())
	router.Use(middleware.RateLimiterMiddleware())
	router.Use(gin.Recovery())

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
	}) // API v1 routes
	v1 := router.Group("/api/v1")
	{
		// Health route under API namespace
		v1.GET("/health", healthHandler.Health)

		// Auth routes (public)
		auth := v1.Group("/auth")
		{
			// Regular auth endpoints
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)

			// Sensitive auth operations use stricter rate limiting
			sensitiveAuth := auth.Group("")
			// uncomment when StrictRateLimiter is implemented
			// sensitiveAuth.Use(middleware.StrictRateLimiter())
			{
				sensitiveAuth.POST("/refresh", authHandler.RefreshToken)
				sensitiveAuth.POST("/reset-password-request", authHandler.ResetPasswordRequest)
				sensitiveAuth.POST("/reset-password", authHandler.ResetPassword)

				// OTP-based verification endpoints
				sensitiveAuth.POST("/verify-otp", authHandler.VerifyOTP)
				sensitiveAuth.POST("/send-otp", authHandler.SendOTP)
			}

			// Protected auth routes
			authProtected := auth.Group("")
			authProtected.Use(middleware.AuthMiddleware(cfg))
			{
				authProtected.POST("/logout", authHandler.Logout)
				authProtected.GET("/profile", authHandler.GetProfile)
			}
		}

		// Event routes
		events := v1.Group("/events")
		{
			// Public event routes
			events.GET("", eventHandler.GetAllEvents)
			events.GET("/:id", eventHandler.GetEventByID)

			// Protected event routes
			eventsProtected := events.Group("")
			eventsProtected.Use(middleware.AuthMiddleware(cfg))
			{
				// Events can be created by organizers and admins
				eventsProtected.POST("", middleware.IsOrganizer(), eventHandler.CreateEvent)
				eventsProtected.PUT("/:id", middleware.IsOrganizer(), eventHandler.UpdateEvent)
				eventsProtected.DELETE("/:id", middleware.IsAdmin(), eventHandler.DeleteEvent)
			}
		}

		// Organization routes
		organizations := v1.Group("/organizations")
		organizations.Use(middleware.AuthMiddleware(cfg))
		{
			// Basic organization operations
			organizations.GET("", organizationHandler.GetUserOrganizations)
			organizations.GET("/:id", organizationHandler.GetOrganizationByID)

			// Organization user management (only organizers can manage their organization)
			orgProtected := organizations.Group("/:id")
			orgProtected.Use(middleware.IsOrganizerOfOrganization())
			{
				// Endpoints for organizers to manage their organization users
				orgProtected.POST("/users", organizationHandler.CreateOrganizationUser)
				orgProtected.GET("/users", organizationHandler.GetOrganizationUsers)
				orgProtected.PUT("/users/:userId", organizationHandler.UpdateOrganizationUser)
				orgProtected.DELETE("/users/:userId", organizationHandler.DeleteOrganizationUser)
			}

			// Admin-only operations
			adminOrgRoutes := organizations.Group("")
			adminOrgRoutes.Use(middleware.IsAdmin())
			{
				adminOrgRoutes.POST("", organizationHandler.CreateOrganization)
				adminOrgRoutes.PUT("/:id", organizationHandler.UpdateOrganization)
				adminOrgRoutes.DELETE("/:id", organizationHandler.DeleteOrganization)
			}
		}
	}

	return router
}
