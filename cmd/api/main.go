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
	"event-ticketing-backend/internal/workers"
	"event-ticketing-backend/pkg/config"
)

// @title Event Ticketing API
// @version 1.0
// @description A scalable REST API for event ticketing system with secure authentication and role-based access control (RBAC)
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8080
// @BasePath /api/v1
// @schemes http https
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

	// Connect to database
	if err := database.Connect(cfg); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	// Connect to Redis
	if err := redis.Connect(cfg); err != nil {
		log.Printf("Warning: Failed to connect to Redis: %v", err)
		// We continue without Redis as it might be an optional dependency
	} else {
		defer redis.Close()
	}

	// Run migrations
	log.Println("Running database migrations...")

	// Migrate tables in the correct order (tables without foreign keys first)
	if err := database.Migrate(
		// First migrate tables that don't depend on others
		&models.Organization{},
		&models.Role{},
		&models.Permission{},
		&models.Event{},
		// Then migrate tables with foreign keys
		&models.User{},
		&models.Token{},
	); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}
	log.Println("Database migrations completed")

	// Initialize background workers
	emailService := services.NewEmailService(cfg)
	emailWorker := workers.NewEmailWorker(cfg, emailService)
	workerManager := workers.NewWorkerManager(emailWorker)

	// Start background workers
	log.Println("Starting background workers...")
	workerManager.StartAll()

	// Setup router with worker dependencies
	router := routes.SetupRouter()

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

	log.Println("Server exited")
}
