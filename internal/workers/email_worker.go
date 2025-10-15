package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"

	"github.com/hibiken/asynq"
)

// EmailWorker processes email jobs from the queue
type EmailWorker struct {
	server       *asynq.Server
	mux          *asynq.ServeMux
	emailService *services.EmailService
	cfg          *config.Config
}

// NewEmailWorker creates a new email worker
func NewEmailWorker(cfg *config.Config, emailService *services.EmailService) *EmailWorker {
	// Convert DB string to int for Asynq
	db := 0
	if cfg.Redis.DB != "" {
		if dbInt, err := strconv.Atoi(cfg.Redis.DB); err == nil {
			db = dbInt
		}
	}

	redisOpts := asynq.RedisClientOpt{
		Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password: cfg.Redis.Password,
		DB:       db,
	}

	// Configure server with different priority queues
	serverConfig := asynq.Config{
		Concurrency: 10, // Number of concurrent workers
		Queues: map[string]int{
			"queue:email:urgent": 6, // Highest priority (OTP, password reset)
			"queue:email:high":   3, // High priority (welcome, verification)
			"queue:email:normal": 1, // Normal priority (notifications)
			"queue:email:low":    1, // Low priority (marketing)
		},
		// Configure retry delays
		RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
			return time.Duration(n) * time.Minute // 1min, 2min, 3min, etc.
		},
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			log.Printf("Email task failed: %v, Error: %v", task.Type(), err)
		}),
	}

	server := asynq.NewServer(redisOpts, serverConfig)
	mux := asynq.NewServeMux()

	worker := &EmailWorker{
		server:       server,
		mux:          mux,
		emailService: emailService,
		cfg:          cfg,
	}

	// Register task handlers
	worker.registerHandlers()

	return worker
}

// registerHandlers registers all email task handlers
func (w *EmailWorker) registerHandlers() {
	// Register the main email sending handler
	w.mux.HandleFunc("email:send", w.handleEmailSend)
}

// handleEmailSend processes email sending tasks
func (w *EmailWorker) handleEmailSend(ctx context.Context, task *asynq.Task) error {
	// Parse the email job from task payload
	var emailJob models.EmailJob
	if err := json.Unmarshal(task.Payload(), &emailJob); err != nil {
		return fmt.Errorf("failed to unmarshal email job: %w", err)
	}

	log.Printf("Processing email job: ID=%s, Type=%s, To=%s", emailJob.ID, emailJob.Type, emailJob.To)

	// Prepare email data
	emailData := services.EmailData{
		To:            emailJob.To,
		Subject:       emailJob.Subject,
		Title:         w.getTitleFromJob(emailJob),
		Message:       w.getMessageFromJob(emailJob),
		RecipientName: w.getRecipientName(emailJob),
		OTP:           w.getOTPFromJob(emailJob),
		Data:          emailJob.TemplateData,
	}

	// Send the email
	err := w.emailService.SendEmail(
		emailJob.To,
		emailJob.Subject,
		emailJob.TemplateFile,
		emailData,
	)

	if err != nil {
		log.Printf("Failed to send email: ID=%s, Error=%v", emailJob.ID, err)
		return fmt.Errorf("failed to send email: %w", err)
	}

	log.Printf("Email sent successfully: ID=%s, To=%s", emailJob.ID, emailJob.To)
	return nil
}

// getRecipientName extracts recipient name from email job data
func (w *EmailWorker) getRecipientName(emailJob models.EmailJob) string {
	if name, ok := emailJob.TemplateData["RecipientName"].(string); ok {
		return name
	}
	if name, ok := emailJob.TemplateData["FirstName"].(string); ok {
		return name
	}
	return ""
}

// getTitleFromJob extracts title from email job data
func (w *EmailWorker) getTitleFromJob(emailJob models.EmailJob) string {
	if title, ok := emailJob.TemplateData["Title"].(string); ok {
		return title
	}
	// Default to subject if no title specified
	return emailJob.Subject
}

// getMessageFromJob extracts message from email job data
func (w *EmailWorker) getMessageFromJob(emailJob models.EmailJob) string {
	if message, ok := emailJob.TemplateData["Message"].(string); ok {
		return message
	}
	// Provide default message based on email type
	switch emailJob.Type {
	case models.EmailTypeOTP:
		return "Please use the verification code below to proceed."
	case models.EmailTypeWelcome:
		return "Welcome! We're excited to have you join our community."
	default:
		return "Thank you for using our service."
	}
}

// getOTPFromJob extracts OTP from email job data
func (w *EmailWorker) getOTPFromJob(emailJob models.EmailJob) string {
	if otp, ok := emailJob.TemplateData["OTP"].(string); ok {
		return otp
	}
	return ""
}

// Start starts the email worker
func (w *EmailWorker) Start() {
	log.Println("Starting email worker...")

	go func() {
		if err := w.server.Run(w.mux); err != nil {
			log.Fatalf("Failed to start email worker: %v", err)
		}
	}()

	log.Println("Email worker started successfully")
}

// Stop stops the email worker gracefully
func (w *EmailWorker) Stop() {
	log.Println("Stopping email worker...")
	w.server.Shutdown()
	log.Println("Email worker stopped")
}
