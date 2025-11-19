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

// OTPWorker processes OTP jobs from the queue
type OTPWorker struct {
	server       *asynq.Server
	mux          *asynq.ServeMux
	otpService   *services.OTPService
	emailService *services.EmailService
	cfg          *config.Config
}

// NewOTPWorker creates a new OTP worker
func NewOTPWorker(cfg *config.Config, otpService *services.OTPService, emailService *services.EmailService) *OTPWorker {
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

	// Configure server with high priority for OTP queue
	serverConfig := asynq.Config{
		Concurrency: 5, // Multiple workers for OTP processing
		Queues: map[string]int{
			"queue:otp:urgent": 10, // Highest priority for OTPs
		},
		// Configure retry delays - shorter for OTPs
		RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
			// 10s, 30s, 1min for OTP retries
			delays := []time.Duration{10 * time.Second, 30 * time.Second, 1 * time.Minute}
			if n < len(delays) {
				return delays[n]
			}
			return 2 * time.Minute
		},
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			log.Printf("OTP task failed: %v, Error: %v", task.Type(), err)
		}),
	}

	server := asynq.NewServer(redisOpts, serverConfig)
	mux := asynq.NewServeMux()

	worker := &OTPWorker{
		server:       server,
		mux:          mux,
		otpService:   otpService,
		emailService: emailService,
		cfg:          cfg,
	}

	// Register task handlers
	worker.registerHandlers()

	return worker
}

// registerHandlers registers all OTP task handlers
func (w *OTPWorker) registerHandlers() {
	// Register the main OTP sending handler
	w.mux.HandleFunc("otp:send", w.handleOTPSend)
}

// handleOTPSend processes OTP sending tasks
func (w *OTPWorker) handleOTPSend(ctx context.Context, task *asynq.Task) error {
	// Parse the OTP job from task payload
	var otpJob models.OTPJob
	if err := json.Unmarshal(task.Payload(), &otpJob); err != nil {
		return fmt.Errorf("failed to unmarshal OTP job: %w", err)
	}

	log.Printf("Processing OTP job: ID=%s, Type=%s, To=%s, Attempt=%d/%d",
		otpJob.ID, otpJob.Type, otpJob.Identifier, otpJob.Attempts+1, otpJob.MaxRetries+1)

	// Send the OTP via email
	err := w.sendOTP(otpJob)
	if err != nil {
		log.Printf("Failed to send OTP: ID=%s, Error=%v", otpJob.ID, err)

		// Check if we should retry
		if otpJob.Attempts < otpJob.MaxRetries {
			otpJob.Attempts++
			log.Printf("Retrying OTP job: ID=%s, Attempt=%d/%d",
				otpJob.ID, otpJob.Attempts+1, otpJob.MaxRetries+1)
			return err // Return error to trigger retry
		}

		// Max retries reached, log failure
		log.Printf("OTP job failed permanently: ID=%s, Max retries exceeded", otpJob.ID)
		return nil // Don't retry anymore
	}

	log.Printf("OTP sent successfully: ID=%s, To=%s", otpJob.ID, otpJob.Identifier)
	return nil
}

// sendOTP sends the OTP based on its type
func (w *OTPWorker) sendOTP(job models.OTPJob) error {
	switch job.Type {
	case "registration":
		return w.sendRegistrationOTP(job.Identifier, job.OTP)
	case "password_reset":
		return w.sendPasswordResetOTP(job.Identifier, job.OTP)
	case "2fa":
		return w.sendTwoFactorOTP(job.Identifier, job.OTP)
	default:
		return fmt.Errorf("unknown OTP type: %s", job.Type)
	}
}

// sendRegistrationOTP sends registration OTP email
func (w *OTPWorker) sendRegistrationOTP(email, otp string) error {
	emailData := services.EmailData{
		To:            email,
		Subject:       "Verify Your Email - Registration OTP",
		Title:         "Email Verification",
		Message:       "Thank you for registering! Please use the verification code below to complete your email verification.",
		RecipientName: w.extractNameFromEmail(email),
		OTP:           otp,
		Data: map[string]interface{}{
			"Title":   "Email Verification",
			"Message": "Thank you for registering! Please use the verification code below to complete your email verification.",
			"OTP":     otp,
			"OTPType": "registration",
		},
	}

	return w.emailService.SendEmail(
		email,
		"Verify Your Email - Registration OTP",
		"otp_email.html",
		emailData,
	)
}

// sendPasswordResetOTP sends password reset OTP email
func (w *OTPWorker) sendPasswordResetOTP(email, otp string) error {
	emailData := services.EmailData{
		To:            email,
		Subject:       "Password Reset OTP",
		Title:         "Password Reset",
		Message:       "You've requested to reset your password. Please use the verification code below to proceed.",
		RecipientName: w.extractNameFromEmail(email),
		OTP:           otp,
		Data: map[string]interface{}{
			"Title":   "Password Reset",
			"Message": "You've requested to reset your password. Please use the verification code below to proceed.",
			"OTP":     otp,
			"OTPType": "password_reset",
		},
	}

	return w.emailService.SendEmail(
		email,
		"Password Reset OTP",
		"otp_email.html",
		emailData,
	)
}

// sendTwoFactorOTP sends 2FA OTP email
func (w *OTPWorker) sendTwoFactorOTP(email, otp string) error {
	emailData := services.EmailData{
		To:            email,
		Subject:       "Two-Factor Authentication Code",
		Title:         "Two-Factor Authentication",
		Message:       "Please use the verification code below to complete your login.",
		RecipientName: w.extractNameFromEmail(email),
		OTP:           otp,
		Data: map[string]interface{}{
			"Title":   "Two-Factor Authentication",
			"Message": "Please use the verification code below to complete your login.",
			"OTP":     otp,
			"OTPType": "2fa",
		},
	}

	return w.emailService.SendEmail(
		email,
		"Two-Factor Authentication Code",
		"otp_email.html",
		emailData,
	)
}

// extractNameFromEmail extracts a name from email (before @)
func (w *OTPWorker) extractNameFromEmail(email string) string {
	for i, char := range email {
		if char == '@' {
			return email[:i]
		}
	}
	return email
}

// Start starts the OTP worker
func (w *OTPWorker) Start() {
	log.Println("Starting OTP worker...")

	go func() {
		if err := w.server.Run(w.mux); err != nil {
			log.Fatalf("Failed to start OTP worker: %v", err)
		}
	}()

	log.Println("OTP worker started successfully")
}

// Stop stops the OTP worker gracefully
func (w *OTPWorker) Stop() {
	log.Println("Stopping OTP worker...")
	w.server.Shutdown()
	log.Println("OTP worker stopped")
}
