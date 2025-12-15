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
	dbInt := 0
	if cfg.Redis.DB != "" {
		if parsed, err := strconv.Atoi(cfg.Redis.DB); err == nil {
			dbInt = parsed
		}
	}

	redisOpts := asynq.RedisClientOpt{
		Addr:         fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password:     cfg.Redis.Password,
		DB:           dbInt,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		PoolSize:     10,
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
		// Configure retry delays with exponential backoff for OTP emails
		RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
			// Use shorter retry delays for all emails to ensure timely delivery
			return time.Duration(n) * 30 * time.Second // 30s, 1min, 1.5min, etc.
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
		Attachments:   w.convertAttachments(emailJob.Attachments),
	}

	// Populate common ticket/event fields from TemplateData so templates
	// that expect top-level fields (like guest_ticket.html) can access them.
	if v, ok := emailJob.TemplateData["EventTitle"].(string); ok {
		emailData.EventTitle = v
	}
	if v, ok := emailJob.TemplateData["EventDate"].(string); ok {
		emailData.EventDate = v
	}
	if v, ok := emailJob.TemplateData["EventLocation"].(string); ok {
		emailData.EventLocation = v
	}
	if v, ok := emailJob.TemplateData["TicketNumber"].(string); ok {
		emailData.TicketNumber = v
	}
	if v, ok := emailJob.TemplateData["QRCode"].(string); ok {
		emailData.QRCode = v
	}
	if v, ok := emailJob.TemplateData["TicketURL"].(string); ok {
		emailData.TicketURL = v
	}

	// Populate guest order confirmation fields
	if v, ok := emailJob.TemplateData["guest_name"].(string); ok {
		emailData.GuestName = v
	}
	if v, ok := emailJob.TemplateData["event_name"].(string); ok {
		emailData.EventName = v
	}
	if v, ok := emailJob.TemplateData["event_date"].(string); ok {
		emailData.EventDate = v
	}
	if v, ok := emailJob.TemplateData["event_time"].(string); ok {
		emailData.EventTime = v
	}
	if v, ok := emailJob.TemplateData["venue"].(string); ok {
		emailData.Venue = v
	}
	if v, ok := emailJob.TemplateData["organizer_name"].(string); ok {
		emailData.OrganizerName = v
	}
	if v, ok := emailJob.TemplateData["total_tickets"].(float64); ok {
		emailData.TotalTickets = int(v)
	}
	if v, ok := emailJob.TemplateData["total_amount"].(float64); ok {
		emailData.TotalAmount = v
	}
	if v, ok := emailJob.TemplateData["ticket_url"].(string); ok {
		emailData.TicketURL = v
	}

	// Send the email
	err := w.emailService.SendEmail(
		emailJob.To,
		emailJob.Subject,
		emailJob.TemplateFile,
		emailData,
	)

	if err != nil {
		log.Printf("Failed to send email: ID=%s, Type=%s, To=%s, Error=%v", emailJob.ID, emailJob.Type, emailJob.To, err)

		// Special handling for OTP emails - log critical failures
		if emailJob.Type == models.EmailTypeOTP {
			log.Printf("CRITICAL: OTP email failed - ID=%s, To=%s, OTP=%s, Error=%v",
				emailJob.ID, emailJob.To, w.getOTPFromJob(emailJob), err)
		}

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

// convertAttachments converts email job attachments to email service attachments
func (w *EmailWorker) convertAttachments(jobAttachments []models.EmailAttachment) []models.EmailAttachment {
	return jobAttachments // They're already the same type
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
	case models.EmailTypeOrganizerCredentials:
		return "Your organizer account has been created successfully."
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
