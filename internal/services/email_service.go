package services

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/smtp"
	"os"
	"path/filepath"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"
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
	Attachments   []models.EmailAttachment // Email attachments
	// Common ticket/event fields used by templates
	EventTitle    string
	EventDate     string
	EventLocation string
	TicketNumber  string
	QRCode        string
	TicketURL     string
	// Guest order confirmation fields
	GuestName     string
	EventName     string
	EventTime     string
	Venue         string
	OrganizerName string
	TotalTickets  int
	TotalAmount   float64

	// Additional fields can be added as needed
	Data map[string]interface{}
}

// SendEmail sends an email using the provided template and data
func (s *EmailService) SendEmail(to, subject, templateName string, data EmailData) error {
	// Set common data
	data.To = to
	data.Subject = subject
	data.AppName = "Timro Tickets"
	data.SupportEmail = s.smtpConfig.FromEmail
	data.CurrentYear = time.Now().Year()

	// Set default title and message if not provided
	if data.Title == "" {
		data.Title = subject
	}

	// Parse and execute template
	body, err := s.parseTemplate(templateName, data)
	if err != nil {
		return err
	}

	// Send email via SMTP with attachments
	return s.sendSMTP(to, subject, body, data.Attachments)
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
		templateName = "otp_email.html"
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
	subject := "Welcome to Timro Tickets!"
	templateName := "welcome_email.html"

	data := EmailData{
		Title:         "Welcome to Timro Tickets!",
		Message:       fmt.Sprintf("Welcome %s! We're excited to have you join our community.", firstName),
		RecipientName: firstName,
	}

	return s.SendEmail(to, subject, templateName, data)
}

// SendWelcomeEmailWithCredentials sends welcome email with login credentials
func (s *EmailService) SendWelcomeEmailWithCredentials(user *models.User, password string) error {
	subject := "Your Organizer Account Credentials - Timro Tickets"
	templateName := "organizer_credentials.html"

	data := EmailData{
		Title:         "Your Organizer Account Credentials",
		Message:       "Your organizer account has been created successfully.",
		RecipientName: user.FirstName + " " + user.LastName,
		Data: map[string]interface{}{
			"FirstName": user.FirstName,
			"LastName":  user.LastName,
			"Email":     user.Email,
			"Password":  password,
			"LoginURL":  "https://timroticket.com/login", // Replace with actual login URL
		},
	}

	return s.SendEmail(user.Email, subject, templateName, data)
}

// parseTemplate parses and executes the email template
func (s *EmailService) parseTemplate(templateName string, data EmailData) (string, error) {
	templatePath := filepath.Join(s.templatesDir, templateName)

	// Check if template file exists
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		return "", utils.NewNotFoundError(fmt.Sprintf("template file %s", templateName))
	}

	tmpl, err := template.ParseFiles(templatePath)
	if err != nil {
		return "", utils.NewInternalServerError(fmt.Sprintf("Failed to parse template file %s.", templateName), err)
	}

	var buf bytes.Buffer

	// Use the Data map if it exists and has content, otherwise use the EmailData struct
	var templateData interface{}
	if data.Data != nil && len(data.Data) > 0 {
		templateData = data.Data
	} else {
		templateData = data
	}

	if err := tmpl.Execute(&buf, templateData); err != nil {
		return "", utils.NewInternalServerError("Failed to execute template.", err)
	}

	return buf.String(), nil
}

// sendSMTP sends email via SMTP
func (s *EmailService) sendSMTP(to, subject, body string, attachments []models.EmailAttachment) error {
	// Check if SMTP is properly configured
	if s.smtpConfig.Host == "" || s.smtpConfig.Username == "" || s.smtpConfig.Password == "" {
		return utils.NewBusinessLogicError("SMTP configuration is incomplete.")
	}

	// Create SMTP authentication
	auth := smtp.PlainAuth("", s.smtpConfig.Username, s.smtpConfig.Password, s.smtpConfig.Host)

	// Compose email message with attachments
	msg := s.composeMessageWithAttachments(to, subject, body, attachments)

	// Send email
	addr := fmt.Sprintf("%s:%d", s.smtpConfig.Host, s.smtpConfig.Port)
	fmt.Printf("Attempting to send email via SMTP: %s to %s\n", addr, to)

	err := smtp.SendMail(addr, auth, s.smtpConfig.FromEmail, []string{to}, msg)
	if err != nil {
		fmt.Printf("SMTP Error: %v\n", err)
		return utils.NewExternalServiceError("SMTP", "Failed to send email.", err)
	}

	fmt.Printf("Email sent successfully to %s\n", to)
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

// composeMessageWithAttachments creates a multipart email message with attachments
func (s *EmailService) composeMessageWithAttachments(to, subject, body string, attachments []models.EmailAttachment) []byte {
	var msg bytes.Buffer

	// Email headers
	msg.WriteString(fmt.Sprintf("From: %s\r\n", s.smtpConfig.FromEmail))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")

	if len(attachments) == 0 {
		// Simple HTML message
		msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		msg.WriteString("\r\n")
		msg.WriteString(body)
	} else {
		// Multipart message with attachments
		boundary := "----=_NextPart_" + fmt.Sprintf("%d", time.Now().Unix())
		msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary))
		msg.WriteString("\r\n")

		// HTML body part
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		msg.WriteString("Content-Transfer-Encoding: 7bit\r\n")
		msg.WriteString("\r\n")
		msg.WriteString(body)
		msg.WriteString("\r\n")

		// Attachment parts
		for _, attachment := range attachments {
			msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
			msg.WriteString(fmt.Sprintf("Content-Type: %s\r\n", attachment.ContentType))
			msg.WriteString("Content-Transfer-Encoding: base64\r\n")
			msg.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", attachment.Filename))
			msg.WriteString("\r\n")

			// Encode attachment data
			var encodedData []byte
			if attachment.IsBase64 {
				// If already base64 encoded, use as-is
				encodedData = attachment.Data
			} else {
				// Encode to base64
				encodedData = make([]byte, base64.StdEncoding.EncodedLen(len(attachment.Data)))
				base64.StdEncoding.Encode(encodedData, attachment.Data)
			}

			// Write in 76-character lines as per MIME standard
			for i := 0; i < len(encodedData); i += 76 {
				end := i + 76
				if end > len(encodedData) {
					end = len(encodedData)
				}
				msg.Write(encodedData[i:end])
				msg.WriteString("\r\n")
			}
		}

		// End boundary
		msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	}

	return msg.Bytes()
}
