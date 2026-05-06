package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "event-ticketing-backend/docs"
	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/redis"
	"event-ticketing-backend/internal/routes"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/internal/validators"
	"event-ticketing-backend/internal/workers"
	"event-ticketing-backend/pkg/config"
)

// @title Timro Ticket API
// @version 1.0
// @description A scalable REST API for event ticketing system with secure authentication and role-based access control (RBAC)
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html
// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name Authorization
// @description JWT token authentication. Use the 'Bearer' prefix followed by a space and the access token. Example: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Starting %s v%s in %s mode", cfg.App.Name, cfg.App.Version, cfg.App.Env)

	// Initialize validators
	validators.Initialize()

	// Connect to database
	if err := database.Connect(cfg); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	// Connect to Redis
	if err := redis.Connect(cfg); err != nil {
		log.Printf("Warning: Failed to connect to Redis: %v", err)
		// Continue without Redis if it's optional
	} else {
		defer redis.Close()
	}

	// Run migrations
	log.Println("Running database migrations...")

	// Migrate tables in the correct order (tables without foreign keys first)
	if err := database.Migrate(
		// First migrate tables that don't depend on others
		&models.Role{},
		&models.Permission{},
		&models.CompanyInfo{},
		&models.Category{},
		&models.Event{},
		&models.EventStatusHistory{},  // Event status change history
		&models.OTP{},                 // OTP table for fallback storage
		&models.RegistrationRequest{}, // Temp registration requests
		&models.FileStorage{},         // File storage table
		&models.GuestUser{},           // Guest user table for guest purchases
		// REMOVED: CheckoutSession model completely removed per clean architecture spec
		// All checkout functionality now handled by PaymentIntent + Transaction
		&models.EventTier{},             // Event tier table
		&models.OrganizerTierTemplate{}, // Organizer tier templates
		// Then migrate tables with foreign keys
		&models.User{},
		&models.OrganizerOnboarding{},
		&models.Token{},
		// &models.Ticket{}, // Ticket table for ticket management
		// Payment-related tables
		&models.PaymentIntent{},       // Payment intents for gateway integration
		&models.Refund{},              // Refund records
		&models.RefundStatusHistory{}, // Refund status change history
		&models.WebhookEvent{},        // Webhook events from payment gateways
		&models.PaymentAuditLog{},     // Payment audit logs
		// Finally migrate financial tables
		// &models.EventSales{}, // REMOVED: Redundant - calculate from transactions
		&models.PaymentBill{},
		&models.PaymentHistory{}, // Payment history for bill payments
		&models.Transaction{},    // Transaction records for all purchases
		&models.PaymentAttempt{}, // Payment attempts for tracking retries and failures
		&models.PayoutRequest{},  // Payout requests table
	); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}
	log.Println("Database migrations completed")

	// Seed roles and admin user
	log.Println("Seeding roles and admin user...")
	if err := database.SeedRoles(database.DB); err != nil {
		log.Fatalf("Failed to seed roles: %v", err)
	}
	if err := database.SeedAdminUser(database.DB); err != nil {
		log.Fatalf("Failed to seed admin user: %v", err)
	}
	if err := database.SeedSecondaryAdminUser(database.DB); err != nil {
		log.Fatalf("Failed to seed secondary admin user: %v", err)
	}

	// Initialize permissions and role assignments
	log.Println("Initializing permissions and role assignments...")
	permissionService := services.NewPermissionService()
	if err := permissionService.InitializeSystemPermissions(); err != nil {
		log.Fatalf("Failed to initialize system permissions: %v", err)
	}
	if err := permissionService.InitializeSystemRolesSafely(); err != nil {
		log.Fatalf("Failed to initialize system role permissions: %v", err)
	}
	log.Println("Permissions initialized successfully")

	log.Println("Seeding completed")

	// Initialize background workers (email, OTP, event status)
	emailService := services.NewEmailService(cfg)
	emailWorker := workers.NewEmailWorker(cfg, emailService)

	// Initialize email outbox service and processor worker
	emailOutboxService := services.NewEmailOutboxService(database.DB)
	emailOutboxProcessorWorker := workers.NewEmailOutboxProcessorWorker(cfg, emailOutboxService, emailService)

	otpService := services.NewOTPService()
	otpWorker := workers.NewOTPWorker(cfg, otpService, emailService)

	eventService := services.NewEventService()
	eventStatusWorker := workers.NewEventStatusWorker(cfg, eventService)

	var workerManager *workers.WorkerManager

	// Initialize worker manager with available workers
	workerManager = workers.NewWorkerManager(emailWorker, emailOutboxProcessorWorker, otpWorker, eventStatusWorker)
	log.Println("Initialized worker manager")

	// Start background workers
	log.Println("Starting background workers...")
	workerManager.StartAll()

	// Initialize payment worker for async webhook processing (asynq)
	// ticketService := services.NewTicketService(database.DB, services.NewFinancialService(database.DB), &cfg.JWT, cfg)
	// reservationService := services.NewReservationService(database.DB)
	// ticketService.SetReservationService(reservationService)
	// ticketService.SetEmailOutboxService(emailOutboxService)
	// paymentWorker := workers.NewPaymentWorker(cfg, ticketService)
	// if err := paymentWorker.InitServer(); err != nil {
	// 	log.Fatalf("Failed to initialize payment worker server: %v", err)
	// }
	log.Println("Initialized payment worker (asynq)")

	// Start payment worker server with auto-restart on crash
	// go func() {
	// 	defer func() {
	// 		if r := recover(); r != nil {
	// 			log.Printf("CRITICAL: Payment worker goroutine panicked: %v\n", r)
	// 		}
	// 	}()

	// 	// Auto-restart logic with exponential backoff
	// 	var retries int
	// 	maxRetries := 5
	// 	baseDelay := 2 * time.Second

	// 	for {
	// 		log.Println("Starting payment worker server...")
	// 		if err := paymentWorker.Start(context.Background()); err != nil {
	// 			retries++
	// 			if retries > maxRetries {
	// 				log.Fatalf("CRITICAL: Payment worker failed after %d retries: %v. System shutting down.", maxRetries, err)
	// 			}

	// 			// Exponential backoff: 2s, 4s, 8s, 16s, 32s
	// 			waitTime := baseDelay * time.Duration(1<<uint(retries-1))
	// 			log.Printf("ERROR: Payment worker crashed: %v | Retrying in %v (attempt %d/%d)\n", err, waitTime, retries, maxRetries)
	// 			time.Sleep(waitTime)
	// 			continue
	// 		}

	// 		// If Start() returns without error (shouldn't happen in normal operation)
	// 		log.Printf("WARNING: Payment worker exited normally (unexpected). Restarting...\n")
	// 		retries = 0 // Reset retries on successful connection
	// 		time.Sleep(baseDelay)
	// 	}
	// }()

	// Setup router with worker dependencies and SSE service
	router := routes.SetupRouter(cfg)

	// Create server
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.App.Host, cfg.App.Port),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Server listening on %s:%s", cfg.App.Host, cfg.App.Port)
		log.Printf("API documentation available at http://%s:%s/api/docs", cfg.App.Host, cfg.App.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Give outstanding requests a deadline for completion
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	// Stop all background workers
	log.Println("Shutting down background workers...")
	workerManager.StopAll()

	// Stop payment worker
	log.Println("Shutting down payment worker...")
	// if err := paymentWorker.Close(); err != nil {
	// 	log.Printf("Warning: Payment worker close error: %v\n", err)
	// }

	log.Println("Server exited")
}
