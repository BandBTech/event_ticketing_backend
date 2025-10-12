package services

import (
	"fmt"
	"log"

	"event-ticketing-backend/internal/models"
)

// EmailQueuedService provides queued email functionality
type EmailQueuedService struct {
	emailService *EmailService
	queueService *EmailQueueService
}

// NewEmailQueuedService creates a new email service with queuing support
func NewEmailQueuedService(emailService *EmailService) *EmailQueuedService {
	queueService := NewEmailQueueService(emailService)
	return &EmailQueuedService{
		emailService: emailService,
		queueService: queueService,
	}
}

// SendVerificationEmail sends an email with a verification link (queued)
func (s *EmailQueuedService) SendVerificationEmail(user *models.User) error {
	// Create an email job
	job := &models.EmailJob{
		Type:         models.EmailTypeVerification,
		To:           user.Email,
		Subject:      "Verify Your Email Address",
		TemplateFile: "verification_email.html",
		TemplateData: map[string]interface{}{
			"Name":            user.FirstName,
			"VerificationURL": fmt.Sprintf("https://yourdomain.com/verify?code=%s", user.VerificationCode),
		},
		Priority:   0, // High priority
		MaxRetries: 3,
	}

	// Queue the email
	if err := s.queueService.QueueEmail(job); err != nil {
		log.Printf("Failed to queue verification email: %v", err)

		// Fallback to direct sending
		return s.emailService.SendVerificationEmail(user)
	}

	return nil
}

// SendPasswordResetEmail sends an email with a password reset link (queued)
func (s *EmailQueuedService) SendPasswordResetEmail(email, resetToken string) error {
	// Create an email job
	job := &models.EmailJob{
		Type:         models.EmailTypePasswordReset,
		To:           email,
		Subject:      "Reset Your Password",
		TemplateFile: "reset_password_email.html",
		TemplateData: map[string]interface{}{
			"ResetURL": fmt.Sprintf("https://yourdomain.com/reset-password?token=%s", resetToken),
		},
		Priority:   0, // High priority
		MaxRetries: 3,
	}

	// Queue the email
	if err := s.queueService.QueueEmail(job); err != nil {
		log.Printf("Failed to queue password reset email: %v", err)

		// Fallback to direct sending
		return s.emailService.SendPasswordResetEmail(email, resetToken)
	}

	return nil
}

// SendWelcomeEmailWithCredentials sends an email to a new user (queued)
func (s *EmailQueuedService) SendWelcomeEmailWithCredentials(user *models.User, password, orgName string) error {
	return s.queueService.QueueWelcomeEmail(user, password, orgName)
}

// SendOTPEmail sends an email with an OTP code (queued)
func (s *EmailQueuedService) SendOTPEmail(email, otp, otpType string) error {
	return s.queueService.QueueOTPEmail(email, otp, otpType)
}
