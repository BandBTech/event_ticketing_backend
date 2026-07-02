package services

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"mime"
	"net/smtp"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	wd, err := os.Getwd()
	if err != nil {
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
	Attachments   []models.EmailAttachment
	EventTitle    string
	EventDate     string
	EventLocation string
	TicketNumber  string
	QRCode        string
	TicketURL     string
	GuestName     string
	EventName     string
	EventTime     string
	Venue         string
	OrganizerName string
	TotalTickets  int
	TotalAmount   float64
	Data          map[string]interface{}
}

// SendEmail sends an email using the provided template and data
func (s *EmailService) SendEmail(to, subject, templateName string, data EmailData) error {
	data.To = to
	data.Subject = subject
	data.AppName = "Timro Tickets"
	data.SupportEmail = s.smtpConfig.FromEmail
	data.CurrentYear = time.Now().Year()

	if data.Title == "" {
		data.Title = subject
	}

	body, err := s.parseTemplate(templateName, data)
	if err != nil {
		return err
	}

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
	case "password_reset":
		subject = "Password Reset OTP"
		title = "Password Reset"
		message = "You've requested to reset your password. Please use the verification code below to proceed."
	default:
		subject = "Your OTP Code"
		title = "Verification Code"
		message = "Please use the verification code below to proceed."
	}
	templateName = "otp_email.html"

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
			"LoginURL":  "https://timroticket.com/login",
		},
	}

	return s.SendEmail(user.Email, subject, templateName, data)
}

// SendTicketConfirmationEmail sends ticket confirmation
func (s *EmailService) SendTicketConfirmationEmail(to, eventName string, totalAmount float64, currency string, ticketCount int, transactionItems interface{}) error {
	subject := fmt.Sprintf("Order Confirmation - %s", eventName)
	templateName := "ticket_confirmation_email.html"

	data := EmailData{
		Title:        "Order Confirmation",
		Message:      "Your order has been confirmed! Your tickets are ready for use.",
		EventName:    eventName,
		TotalAmount:  totalAmount,
		TotalTickets: ticketCount,
		Data: map[string]interface{}{
			"EventName":        eventName,
			"TotalAmount":      totalAmount,
			"Currency":         currency,
			"TicketCount":      ticketCount,
			"TransactionItems": transactionItems,
		},
	}

	return s.SendEmail(to, subject, templateName, data)
}

// SendPaymentFailedEmail sends a payment failed notification
func (s *EmailService) SendPaymentFailedEmail(to, eventName string, amount float64, currency, reason string) error {
	data := EmailData{
		Title:       "Payment Failed",
		Message:     "Your payment could not be processed.",
		EventName:   eventName,
		TotalAmount: amount,
		Data: map[string]interface{}{
			"currency": currency,
			"reason":   reason,
		},
	}
	return s.SendEmail(to, "Payment Failed - "+eventName, "payment_failed.html", data)
}

// SendPaymentCanceledEmail sends a payment canceled notification
func (s *EmailService) SendPaymentCanceledEmail(to, eventName string, amount float64, currency string) error {
	data := EmailData{
		Title:       "Payment Canceled",
		Message:     "Your payment has been canceled.",
		EventName:   eventName,
		TotalAmount: amount,
		Data: map[string]interface{}{
			"currency": currency,
		},
	}
	return s.SendEmail(to, "Payment Canceled - "+eventName, "payment_canceled.html", data)
}

// SendRefundProcessedEmail sends a refund processed notification
func (s *EmailService) SendRefundProcessedEmail(to, eventName string, amount float64, currency string, ticketCount int) error {
	data := EmailData{
		Title:        "Refund Processed",
		Message:      "Your refund has been processed successfully.",
		EventName:    eventName,
		TotalAmount:  amount,
		TotalTickets: ticketCount,
		Data: map[string]interface{}{
			"currency": currency,
		},
	}
	return s.SendEmail(to, "Refund Processed - "+eventName, "refund_processed.html", data)
}

// SendEventCancellationEmail sends an event cancellation notification
func (s *EmailService) SendEventCancellationEmail(to, userName, eventName, organizerName string, refundAmount float64, currency string, ticketCount int, transactionID string, eventDate, eventLocation string) error {
	data := EmailData{
		Title:        "Event Cancelled",
		Message:      "The event you purchased tickets for has been cancelled.",
		EventName:    eventName,
		TotalAmount:  refundAmount,
		TotalTickets: ticketCount,
		Data: map[string]interface{}{
			"user_name":      userName,
			"event_name":     eventName,
			"organizer_name": organizerName,
			"refund_amount":  refundAmount,
			"currency":       currency,
			"ticket_count":   ticketCount,
			"transaction_id": transactionID,
			"event_date":     eventDate,
			"event_location": eventLocation,
			"completed_at":   time.Now().Format("January 2, 2006 at 3:04 PM"),
		},
	}
	return s.SendEmail(to, "Event Cancelled - "+eventName, "event_cancellation.html", data)
}

// parseTemplate parses and executes the email template
func (s *EmailService) parseTemplate(templateName string, data EmailData) (string, error) {
	// Ensure .html extension
	if !strings.HasSuffix(templateName, ".html") {
		templateName += ".html"
	}

	templatePath := filepath.Join(s.templatesDir, templateName)

	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		return "", utils.NewNotFoundError(fmt.Sprintf("template file %s", templateName))
	}

	tmpl, err := template.ParseFiles(templatePath)
	if err != nil {
		return "", utils.NewInternalServerError(fmt.Sprintf("Failed to parse template file %s.", templateName), err)
	}

	var buf bytes.Buffer

	// Build template data with ALL fields
	templateData := map[string]interface{}{
		"To":            data.To,
		"Subject":       data.Subject,
		"Title":         data.Title,
		"Message":       data.Message,
		"RecipientName": data.RecipientName,
		"OTP":           data.OTP,
		"AppName":       data.AppName,
		"SupportEmail":  data.SupportEmail,
		"CurrentYear":   data.CurrentYear,
		"EventTitle":    data.EventTitle,
		"EventDate":     data.EventDate,
		"EventLocation": data.EventLocation,
		"TicketNumber":  data.TicketNumber,
		"QRCode":        data.QRCode,
		"TicketURL":     data.TicketURL,
		"GuestName":     data.GuestName,
		"EventName":     data.EventName,
		"EventTime":     data.EventTime,
		"Venue":         data.Venue,
		"OrganizerName": data.OrganizerName,
		"TotalTickets":  data.TotalTickets,
		"TotalAmount":   data.TotalAmount,
	}

	// Merge Data map
	if data.Data != nil {
		for k, v := range data.Data {
			templateData[k] = v
		}
	}

	if err := tmpl.Execute(&buf, templateData); err != nil {
		log.Printf("TEMPLATE_ERROR: %s - %v", templateName, err)
		return "", utils.NewInternalServerError("Failed to execute template.", err)
	}

	return buf.String(), nil
}

// sendSMTP sends email via SMTP
func (s *EmailService) sendSMTP(to, subject, body string, attachments []models.EmailAttachment) error {
	if s.smtpConfig.Host == "" || s.smtpConfig.Username == "" || s.smtpConfig.Password == "" {
		return utils.NewBusinessLogicError("SMTP configuration is incomplete.")
	}

	auth := smtp.PlainAuth("", s.smtpConfig.Username, s.smtpConfig.Password, s.smtpConfig.Host)

	msg := s.buildSimpleMessage(to, subject, body, attachments)

	addr := fmt.Sprintf("%s:%d", s.smtpConfig.Host, s.smtpConfig.Port)
	log.Printf("Sending email to %s via %s", to, addr)

	err := smtp.SendMail(addr, auth, s.smtpConfig.FromEmail, []string{to}, msg)
	if err != nil {
		log.Printf("SMTP Error: %v", err)
		return utils.NewExternalServiceError("SMTP", "Failed to send email.", err)
	}

	log.Printf("Email sent successfully to %s", to)
	return nil
}

// buildSimpleMessage creates a simple, clean MIME message
func (s *EmailService) buildSimpleMessage(to, subject, htmlBody string, attachments []models.EmailAttachment) []byte {
	var buf bytes.Buffer

	// Headers
	buf.WriteString(fmt.Sprintf("From: %s\r\n", s.smtpConfig.FromEmail))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", to))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", subject)))
	buf.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z)))
	buf.WriteString(fmt.Sprintf("Message-ID: <%d@%s>\r\n", time.Now().UnixNano(), s.smtpConfig.Host))
	buf.WriteString("MIME-Version: 1.0\r\n")

	if len(attachments) > 0 {
		// Multipart mixed for attachments
		boundary := fmt.Sprintf("boundary_%d", time.Now().UnixNano())
		buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary))

		// HTML part
		buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		buf.WriteString(base64Encode(htmlBody))
		buf.WriteString("\r\n")

		// Attachments
		for _, att := range attachments {
			buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
			buf.WriteString(fmt.Sprintf("Content-Type: %s\r\n", att.ContentType))
			buf.WriteString("Content-Transfer-Encoding: base64\r\n")
			buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n\r\n", att.Filename))
			if att.IsBase64 {
				buf.Write(att.Data)
			} else {
				buf.WriteString(base64Encode(string(att.Data)))
			}
			buf.WriteString("\r\n")
		}

		buf.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else {
		// Simple HTML email
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		buf.WriteString(base64Encode(htmlBody))
	}

	return buf.Bytes()
}

// base64Encode encodes a string to base64 with line breaks
func base64Encode(s string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(s))
	var result strings.Builder
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		result.WriteString(encoded[i:end])
		result.WriteString("\r\n")
	}
	return result.String()
}

// Helper functions
func htmlToPlainText(html string) string {
	// Remove style and script tags
	html = regexp.MustCompile(`(?s)<style.*?</style>`).ReplaceAllString(html, "")
	html = regexp.MustCompile(`(?s)<script.*?</script>`).ReplaceAllString(html, "")

	// Replace common tags with newlines
	replacer := strings.NewReplacer(
		"<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"</p>", "\n\n", "</div>", "\n", "</h1>", "\n",
		"</h2>", "\n", "</h3>", "\n", "</li>", "\n",
		"</tr>", "\n", "</td>", " ", "</th>", " ",
	)
	html = replacer.Replace(html)

	// Remove all HTML tags
	html = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(html, "")

	// Decode HTML entities
	html = strings.ReplaceAll(html, "&nbsp;", " ")
	html = strings.ReplaceAll(html, "&amp;", "&")
	html = strings.ReplaceAll(html, "&lt;", "<")
	html = strings.ReplaceAll(html, "&gt;", ">")
	html = strings.ReplaceAll(html, "&quot;", `"`)
	html = strings.ReplaceAll(html, "&#39;", "'")

	// Clean up whitespace
	lines := strings.Split(html, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}

	return strings.Join(cleaned, "\n")
}
