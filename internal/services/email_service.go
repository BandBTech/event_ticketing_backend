package services

import (
	"bytes"
	"fmt"
	"html/template"
	"net/smtp"
	"os"
	"path/filepath"
	"time"

	"event-ticketing-backend/pkg/config"
)

// EmailService handles email sending functionality
type EmailService struct {
	smtpConfig   *config.SMTPConfig
	templatesDir string
}

// NewEmailService creates a new email service instance
func NewEmailService(cfg *config.Config) *EmailService {
	// Get the current working directory to build absolute path
	wd, err := os.Getwd()
	if err != nil {
		// Fallback to relative path if we can't get working directory
		wd = "."
	}

	templatesDir := filepath.Join(wd, "internal", "templates", "email")

	return &EmailService{
		smtpConfig:   &cfg.SMTP,
		templatesDir: templatesDir,
	}
}

// EmailData represents the data structure for email templates
type EmailData struct {
	To            string
	Subject       string
	Title         string
	Message       string
	RecipientName string
	OTP           string
	AppName       string
	SupportEmail  string
	CurrentYear   int
	// Additional fields can be added as needed
	Data map[string]interface{}
}

// SendEmail sends an email using the provided template and data
func (s *EmailService) SendEmail(to, subject, templateName string, data EmailData) error {
	// Set common data
	data.To = to
	data.Subject = subject
	data.AppName = "Event Ticketing"
	data.SupportEmail = s.smtpConfig.FromEmail
	data.CurrentYear = time.Now().Year()

	// Set default title and message if not provided
	if data.Title == "" {
		data.Title = subject
	}

	// Parse and execute template
	body, err := s.parseTemplate(templateName, data)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	// Send email via SMTP
	return s.sendSMTP(to, subject, body)
}

// SendOTPEmail sends an OTP email for verification purposes
func (s *EmailService) SendOTPEmail(to, otp, otpType string) error {
	var subject, templateName, title, message string

	switch otpType {
	case "registration":
		subject = "Verify Your Email - Registration OTP"
		title = "Email Verification"
		message = "Thank you for registering! Please use the verification code below to complete your email verification."
		templateName = "otp_email.html"
	case "password_reset":
		subject = "Password Reset OTP"
		title = "Password Reset"
		message = "You've requested to reset your password. Please use the verification code below to proceed."
		templateName = "reset_password_email.html"
	default:
		subject = "Your OTP Code"
		title = "Verification Code"
		message = "Please use the verification code below to proceed."
		templateName = "otp_email.html"
	}

	data := EmailData{
		Title:   title,
		Message: message,
		OTP:     otp,
		Data: map[string]interface{}{
			"OTPType": otpType,
		},
	}

	return s.SendEmail(to, subject, templateName, data)
}

// SendWelcomeEmail sends a welcome email to new users
func (s *EmailService) SendWelcomeEmail(to, firstName string) error {
	subject := "Welcome to Event Ticketing!"
	templateName := "welcome_email.html"

	data := EmailData{
		Title:         "Welcome to Event Ticketing!",
		Message:       fmt.Sprintf("Welcome %s! We're excited to have you join our community.", firstName),
		RecipientName: firstName,
	}

	return s.SendEmail(to, subject, templateName, data)
}

// SendWelcomeEmailWithCredentials sends welcome email with login credentials
func (s *EmailService) SendWelcomeEmailWithCredentials(user interface{}, password, orgName string) error {
	// This method signature matches the existing call in organization service
	// You can implement this based on your user model structure
	return fmt.Errorf("not implemented yet - will be added when needed")
}

// parseTemplate parses and executes the email template
func (s *EmailService) parseTemplate(templateName string, data EmailData) (string, error) {
	templatePath := filepath.Join(s.templatesDir, templateName)

	// Check if template file exists
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		return "", fmt.Errorf("template file does not exist: %s (templates dir: %s)", templatePath, s.templatesDir)
	}

	tmpl, err := template.ParseFiles(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to parse template file %s: %w", templatePath, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// sendSMTP sends email via SMTP
func (s *EmailService) sendSMTP(to, subject, body string) error {
	// Create SMTP authentication
	auth := smtp.PlainAuth("", s.smtpConfig.Username, s.smtpConfig.Password, s.smtpConfig.Host)

	// Compose email message
	msg := s.composeMessage(to, subject, body)

	// Send email
	addr := fmt.Sprintf("%s:%d", s.smtpConfig.Host, s.smtpConfig.Port)
	err := smtp.SendMail(addr, auth, s.smtpConfig.FromEmail, []string{to}, []byte(msg))
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// composeMessage creates the email message with headers
func (s *EmailService) composeMessage(to, subject, body string) string {
	msg := fmt.Sprintf("From: %s\r\n", s.smtpConfig.FromEmail)
	msg += fmt.Sprintf("To: %s\r\n", to)
	msg += fmt.Sprintf("Subject: %s\r\n", subject)
	msg += "MIME-Version: 1.0\r\n"
	msg += "Content-Type: text/html; charset=UTF-8\r\n"
	msg += "\r\n"
	msg += body

	return msg
}
