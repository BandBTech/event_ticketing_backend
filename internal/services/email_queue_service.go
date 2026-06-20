package services

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// EmailQueueService handles email job queuing using Asynq
type EmailQueueService struct {
	client          *asynq.Client
	config          *config.Config
	secureQRService *SecureQRService
	jwtService      *utils.JWTService
}

// SetSecureQRService sets the secure QR service dependency
func (s *EmailQueueService) SetSecureQRService(secureQRService *SecureQRService) {
	s.secureQRService = secureQRService
}

// SetJWTService sets the JWT service dependency
func (s *EmailQueueService) SetJWTService(jwtService *utils.JWTService) {
	s.jwtService = jwtService
}

// NewEmailQueueService creates a new email queue service
func NewEmailQueueService(cfg *config.Config) *EmailQueueService {
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

	client := asynq.NewClient(redisOpts)

	return &EmailQueueService{
		client:          client,
		config:          cfg,
		secureQRService: NewSecureQRService(cfg),
	}
}

// QueueOTPEmail queues an OTP email job with enhanced retry and priority settings
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
		MaxRetries: 5,                     // Higher retry count for OTP emails
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

// QueueOrganizationUserCredentialsEmail queues an organization user credentials email job
func (s *EmailQueueService) QueueOrganizationUserCredentialsEmail(user *models.User, password string, roleName string) error {
	// Get user login URL from config
	userLoginURL := s.config.URLs.UserBaseURL
	if userLoginURL == "" {
		userLoginURL = "https://timroticket.com" // fallback
	}

	emailJob := &models.EmailJob{
		Type:         models.EmailTypeOrganizationInvitation,
		To:           user.Email,
		Subject:      "Your Organization Account Credentials - Timro Tickets",
		TemplateFile: "organization_user_credentials.html",
		TemplateData: map[string]interface{}{
			"FirstName": user.FirstName,
			"LastName":  user.LastName,
			"Email":     user.Email,
			"Password":  password,
			"RoleName":  roleName,
			"LoginURL":  userLoginURL,
		},
		Priority:   models.PriorityHigh,
		MaxRetries: 3,
	}
	emailJob.SetDefaults()

	return s.queueEmailJob(emailJob)
}

// QueueOrganizerCredentialsEmail queues an organizer credentials email job
func (s *EmailQueueService) QueueOrganizerCredentialsEmail(user *models.User, password string) error {
	// Get organizer login URL from config
	organizerLoginURL := s.config.URLs.OrganizerBaseURL
	if organizerLoginURL == "" {
		organizerLoginURL = "https://sandbox-organizer.timroticket.com" // fallback
	}

	emailJob := &models.EmailJob{
		Type:         models.EmailTypeOrganizerCredentials,
		To:           user.Email,
		Subject:      "Your Organizer Account Credentials - Timro Tickets",
		TemplateFile: "organizer_credentials.html",
		TemplateData: map[string]interface{}{
			"FirstName": user.FirstName,
			"LastName":  user.LastName,
			"Email":     user.Email,
			"Password":  password,
			"LoginURL":  organizerLoginURL,
		},
		Priority:   models.PriorityHigh,
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
		return utils.NewInternalServerError("Failed to marshal email job.", err)
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
		return utils.NewExternalServiceError("Asynq Queue", "Failed to enqueue email task.", err)
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
		return "Verify Your Email", "Please use the following OTP to complete your registration:"
	case "password_reset":
		return "Reset Your Password", "Please use the following OTP to reset your password:"
	default:
		return "Your OTP Code", "Please use the following OTP code:"
	}
}

// QueueTestTicketEmail queues a test ticket email without attachments
func (s *EmailQueueService) QueueTestTicketEmail(to string, ticketData map[string]interface{}) error {
	emailJob := &models.EmailJob{
		Type:         models.EmailTypeTicketConfirmation,
		To:           to,
		Subject:      "Test Ticket - TIMRO TICKETS",
		TemplateFile: "test_ticket.html",
		TemplateData: ticketData,
		Priority:     models.PriorityNormal,
		MaxRetries:   3,
	}
	emailJob.SetDefaults()

	return s.queueEmailJob(emailJob)
}

// QueueGuestVerificationEmail queues a guest email verification email
func (s *EmailQueueService) QueueGuestVerificationEmail(guestUser *models.GuestUser) error {
	// Generate verification token
	token := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour)

	// Update guest user with token
	guestUser.VerificationToken = token
	guestUser.TokenExpiresAt = &expiresAt

	emailJob := &models.EmailJob{
		Type:         models.EmailTypeVerification,
		To:           guestUser.Email,
		Subject:      "Verify Your Email - Guest Ticket Purchase",
		TemplateFile: "guest_verification.html",
		TemplateData: map[string]interface{}{
			"Title":           "Email Verification Required",
			"Message":         "Thank you for your ticket purchase! Please verify your email to activate your tickets.",
			"RecipientName":   guestUser.FirstName + " " + guestUser.LastName,
			"VerificationURL": fmt.Sprintf("%s/verify-guest/%s", s.config.URLs.UserBaseURL, token),
			"Token":           token,
		},
		Priority:   models.PriorityUrgent, // Verification emails are urgent
		MaxRetries: 5,
	}
	emailJob.SetDefaults()

	return s.queueEmailJob(emailJob)
}
