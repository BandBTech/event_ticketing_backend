package services

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"

	"github.com/hibiken/asynq"
)

// EmailQueueService handles email job queuing using Asynq
type EmailQueueService struct {
	client *asynq.Client
}

// NewEmailQueueService creates a new email queue service
func NewEmailQueueService(cfg *config.Config) *EmailQueueService {
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

	client := asynq.NewClient(redisOpts)

	return &EmailQueueService{
		client: client,
	}
}

// QueueOTPEmail queues an OTP email job
func (s *EmailQueueService) QueueOTPEmail(to, otp, otpType string) error {
	title, message := s.getOTPTitleAndMessage(otpType)

	emailJob := &models.EmailJob{
		Type:         models.EmailTypeOTP,
		To:           to,
		Subject:      s.getOTPSubject(otpType),
		TemplateFile: s.getOTPTemplate(otpType),
		TemplateData: map[string]interface{}{
			"Title":   title,
			"Message": message,
			"OTP":     otp,
			"OTPType": otpType,
		},
		Priority:   models.PriorityUrgent, // OTP emails are urgent
		MaxRetries: 3,
	}
	emailJob.SetDefaults()

	return s.queueEmailJob(emailJob)
}

// QueueWelcomeEmail queues a welcome email job
func (s *EmailQueueService) QueueWelcomeEmail(to, firstName string) error {
	emailJob := &models.EmailJob{
		Type:         models.EmailTypeWelcome,
		To:           to,
		Subject:      "Welcome to Timro Tickets!",
		TemplateFile: "welcome_email.html",
		TemplateData: map[string]interface{}{
			"Title":         "Welcome to Timro Tickets!",
			"Message":       fmt.Sprintf("Welcome %s! We're excited to have you join our community.", firstName),
			"RecipientName": firstName,
		},
		Priority:   models.PriorityHigh, // Welcome emails are high priority
		MaxRetries: 3,
	}
	emailJob.SetDefaults()

	return s.queueEmailJob(emailJob)
}

// QueueRegistrationOTP queues a registration OTP email
func (s *EmailQueueService) QueueRegistrationOTP(to, otp string) error {
	return s.QueueOTPEmail(to, otp, "registration")
}

// QueuePasswordResetOTP queues a password reset OTP email
func (s *EmailQueueService) QueuePasswordResetOTP(to, otp string) error {
	return s.QueueOTPEmail(to, otp, "password_reset")
}

// queueEmailJob queues an email job with the appropriate priority
func (s *EmailQueueService) queueEmailJob(emailJob *models.EmailJob) error {
	// Serialize the email job
	payload, err := json.Marshal(emailJob)
	if err != nil {
		return fmt.Errorf("failed to marshal email job: %w", err)
	}

	// Create Asynq task
	task := asynq.NewTask("email:send", payload)

	// Set task options based on priority
	opts := []asynq.Option{
		asynq.MaxRetry(emailJob.MaxRetries),
		asynq.Queue(emailJob.GetPriorityQueue()),
	}

	// Add process after time if specified
	if !emailJob.ProcessAfter.IsZero() {
		opts = append(opts, asynq.ProcessAt(emailJob.ProcessAfter))
	}

	// Enqueue the task
	info, err := s.client.Enqueue(task, opts...)
	if err != nil {
		return fmt.Errorf("failed to enqueue email task: %w", err)
	}

	log.Printf("Email job queued successfully: ID=%s, Queue=%s, Type=%s, To=%s",
		info.ID, info.Queue, emailJob.Type, emailJob.To)

	return nil
}

// Close closes the client connection
func (s *EmailQueueService) Close() error {
	return s.client.Close()
}

// getOTPSubject returns the appropriate subject for OTP emails
func (s *EmailQueueService) getOTPSubject(otpType string) string {
	switch otpType {
	case "registration":
		return "Verify Your Email - Registration OTP"
	case "password_reset":
		return "Password Reset OTP"
	default:
		return "Your OTP Code"
	}
}

// getOTPTemplate returns the appropriate template for OTP emails
func (s *EmailQueueService) getOTPTemplate(otpType string) string {
	switch otpType {
	case "registration":
		return "otp_email.html"
	case "password_reset":
		return "otp_email.html"
	default:
		return "otp_email.html"
	}
}

// getOTPTitleAndMessage returns the appropriate title and message for OTP emails
func (s *EmailQueueService) getOTPTitleAndMessage(otpType string) (string, string) {
	switch otpType {
	case "registration":
		return "Email Verification", "Thank you for registering! Please use the verification code below to complete your email verification."
	case "password_reset":
		return "Password Reset", "You've requested to reset your password. Please use the verification code below to proceed."
	default:
		return "Verification Code", "Please use the verification code below to proceed."
	}
}
