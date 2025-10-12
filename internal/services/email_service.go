package services

import (
	"bytes"
	"fmt"
	"html/template"
	"net/smtp"
	"os"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
)

// EmailService provides email functionality
type EmailService struct {
	smtpConfig *config.SMTPConfig
}

// NewEmailService creates a new email service
func NewEmailService(cfg *config.Config) *EmailService {
	return &EmailService{
		smtpConfig: &cfg.SMTP,
	}
}

// SendVerificationEmail sends an email with a verification link
func (s *EmailService) SendVerificationEmail(user *models.User) error {
	// Skip actual sending in development mode if SMTP is not configured
	if s.smtpConfig.Host == "" || s.smtpConfig.Username == "" {
		fmt.Printf("SMTP not configured. Would send verification email to %s with code: %s\n",
			user.Email, user.VerificationCode)
		return nil
	}

	subject := "Verify Your Email Address"
	templateData := map[string]interface{}{
		"Name":             user.FirstName,
		"VerificationURL":  fmt.Sprintf("https://yourdomain.com/verify?code=%s", user.VerificationCode),
		"VerificationCode": user.VerificationCode,
		"CurrentYear":      time.Now().Year(),
	}

	body, err := s.parseTemplate("verification_email.html", templateData)
	if err != nil {
		return err
	}

	return s.sendEmail(user.Email, subject, body)
}

// SendPasswordResetEmail sends an email with a password reset link
func (s *EmailService) SendPasswordResetEmail(email, resetToken string) error {
	// Skip actual sending in development mode if SMTP is not configured
	if s.smtpConfig.Host == "" || s.smtpConfig.Username == "" {
		fmt.Printf("SMTP not configured. Would send password reset email to %s with token: %s\n",
			email, resetToken)
		return nil
	}

	subject := "Reset Your Password"
	templateData := map[string]interface{}{
		"ResetURL":    fmt.Sprintf("https://yourdomain.com/reset-password?token=%s", resetToken),
		"CurrentYear": time.Now().Year(),
	}

	body, err := s.parseTemplate("reset_password_email.html", templateData)
	if err != nil {
		return err
	}

	return s.sendEmail(email, subject, body)
}

// parseTemplate parses an HTML template file from the filesystem
func (s *EmailService) parseTemplate(templateName string, data interface{}) (string, error) {
	// Add the current year to all template data maps
	// First, check if data is a map and add CurrentYear to it
	if dataMap, ok := data.(map[string]interface{}); ok {
		// Check if CurrentYear is already set
		if _, exists := dataMap["CurrentYear"]; !exists {
			// Add the current year
			dataMap["CurrentYear"] = time.Now().Year()
		}
	}

	// Define the template directory
	templateDir := "internal/templates/email/"
	templatePath := templateDir + templateName

	// Check if file exists
	_, err := os.Stat(templatePath)
	if err != nil {
		return "", fmt.Errorf("template file not found: %s, error: %w", templatePath, err)
	}

	// Parse the template from file
	tmpl, err := template.ParseFiles(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", templateName, err)
	}

	// Execute template with data
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return "", fmt.Errorf("failed to execute template %s: %w", templateName, err)
	}

	return buf.String(), nil
}

// SendWelcomeEmailWithCredentials sends an email to a new user created by an organizer
// with their login credentials
func (s *EmailService) SendWelcomeEmailWithCredentials(user *models.User, password, orgName string) error {
	// Skip actual sending in development mode if SMTP is not configured
	if s.smtpConfig.Host == "" || s.smtpConfig.Username == "" {
		fmt.Printf("SMTP not configured. Would send welcome email to %s with password: %s\n",
			user.Email, password)
		return nil
	}

	subject := "Welcome to " + orgName + " - Your Account Information"
	templateData := map[string]interface{}{
		"Name":        user.FirstName + " " + user.LastName,
		"OrgName":     orgName,
		"Email":       user.Email,
		"Password":    password,
		"LoginURL":    "https://yourdomain.com/login",
		"CurrentYear": time.Now().Year(),
	}

	body, err := s.parseTemplate("welcome_email.html", templateData)
	if err != nil {
		return err
	}

	return s.sendEmail(user.Email, subject, body)
}

// SendOTPEmail sends an email with an OTP for various verification purposes
func (s *EmailService) SendOTPEmail(email, otp, otpType string) error {
	// Skip actual sending in development mode if SMTP is not configured
	if s.smtpConfig.Host == "" || s.smtpConfig.Username == "" {
		fmt.Printf("SMTP not configured. Would send OTP email to %s with code: %s for purpose: %s\n",
			email, otp, otpType)
		return nil
	}

	var subject, title, message string

	switch otpType {
	case "registration":
		subject = "Verify Your Email Address"
		title = "Email Verification"
		message = "Thank you for registering. Please use the following code to verify your email address."
	case "password_reset":
		subject = "Reset Your Password"
		title = "Password Reset Request"
		message = "We received a request to reset your password. Please use the following code to continue with your password reset."
	case "2fa":
		subject = "Two-Factor Authentication Code"
		title = "Login Authentication Code"
		message = "To complete your login, please enter the following verification code."
	default:
		subject = "Verification Code"
		title = "Your Verification Code"
		message = "Please use the following code to verify your identity."
	}

	templateData := map[string]interface{}{
		"Title":       title,
		"Message":     message,
		"OTP":         otp,
		"CurrentYear": time.Now().Year(),
	}

	body, err := s.parseTemplate("otp_email.html", templateData)
	if err != nil {
		return err
	}

	return s.sendEmail(email, subject, body)
}

// sendEmail sends an email using SMTP
func (s *EmailService) sendEmail(to, subject, body string) error {
	// Debug SMTP configuration
	fmt.Printf("SMTP Configuration: Host=%s, Port=%s, User=%s, From=%s\n",
		s.smtpConfig.Host, s.smtpConfig.Port, s.smtpConfig.Username, s.smtpConfig.From)

	// Format the MIME email
	mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	msg := []byte("Subject: " + subject + "\n" + mime + body)

	// Set up authentication information
	auth := smtp.PlainAuth("", s.smtpConfig.Username, s.smtpConfig.Password, s.smtpConfig.Host)

	// Connect to the server and send email
	addr := fmt.Sprintf("%s:%s", s.smtpConfig.Host, s.smtpConfig.Port)
	err := smtp.SendMail(addr, auth, s.smtpConfig.From, []string{to}, msg)

	if err != nil {
		fmt.Printf("SMTP Error: %v\n", err)
	} else {
		fmt.Printf("Email sent successfully to %s\n", to)
	}

	return err
}
