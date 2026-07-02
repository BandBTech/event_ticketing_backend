package services

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/smtp"
	"net/textproto"
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

// SendTicketConfirmationEmail sends ticket confirmation with order details (supports multi-tier)
// Use this for payment confirmations with transaction items breakdown
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
			"TransactionItems": transactionItems, // For multi-tier breakdown
		},
	}

	return s.SendEmail(to, subject, templateName, data)
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

	// Create a merged map that includes ALL fields from EmailData struct
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

	// Merge any additional fields from the Data map
	if data.Data != nil {
		for k, v := range data.Data {
			templateData[k] = v
		}
	}

	if err := tmpl.Execute(&buf, templateData); err != nil {
		// Log detailed template error information for debugging
		log.Printf("TEMPLATE_EXECUTION_ERROR:")
		log.Printf("   Template: %s", templateName)
		log.Printf("   Error: %v", err)
		log.Printf("   Available fields in template data (%d):", len(templateData))
		for k, v := range templateData {
			vType := fmt.Sprintf("%T", v)
			vStr := fmt.Sprintf("%v", v)
			if len(vStr) > 100 {
				vStr = vStr[:100] + "..."
			}
			log.Printf("     - %s (%s) = %s", k, vType, vStr)
		}
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

	// Compose standards-compliant MIME email message
	msg, meta, err := s.buildMIMEMessage(to, subject, body, attachments)
	if err != nil {
		return err
	}

	log.Printf("[EMAIL MIME] content-type=%s", meta.ContentType)
	log.Printf("[EMAIL MIME] boundaries=%s", strings.Join(meta.Boundaries, ", "))
	log.Printf("[EMAIL MIME] transfer-encodings=%s", strings.Join(meta.TransferEncodings, ", "))
	log.Printf("[EMAIL MIME] subject-encoding=%s", meta.SubjectEncoding)

	if err := os.WriteFile("email.eml", msg, 0644); err != nil {
		log.Printf("[EMAIL MIME] warning: failed to write email.eml: %v", err)
	} else {
		log.Printf("[EMAIL MIME] wrote email.eml (%d bytes)", len(msg))
	}

	// Send email
	addr := fmt.Sprintf("%s:%d", s.smtpConfig.Host, s.smtpConfig.Port)
	fmt.Printf("Attempting to send email via SMTP: %s to %s\n", addr, to)

	err = smtp.SendMail(addr, auth, s.smtpConfig.FromEmail, []string{to}, msg)
	if err != nil {
		fmt.Printf("SMTP Error: %v\n", err)
		return utils.NewExternalServiceError("SMTP", "Failed to send email.", err)
	}

	fmt.Printf("Email sent successfully to %s\n", to)
	return nil
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

	return s.SendEmail(to, "Payment Failed - "+eventName, "payment_failed", data)
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

	return s.SendEmail(to, "Payment Canceled - "+eventName, "payment_canceled", data)
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

	return s.SendEmail(to, "Refund Processed - "+eventName, "refund_processed", data)
}

// SendEventCancellationEmail sends an event cancellation notification with refund details
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

	return s.SendEmail(to, "Event Cancelled - "+eventName, "event_cancellation", data)
}

// composeMessage creates the email message with headers
func (s *EmailService) composeMessage(to, subject, body string) string {
	msg, _, err := s.buildMIMEMessage(to, subject, body, nil)
	if err != nil {
		return body
	}

	return string(msg)
}

// composeMessageWithAttachments creates a multipart email message with attachments
func (s *EmailService) composeMessageWithAttachments(to, subject, body string, attachments []models.EmailAttachment) []byte {
	msg, _, err := s.buildMIMEMessage(to, subject, body, attachments)
	if err != nil {
		return nil
	}

	return msg
}

type emailMIMEMetadata struct {
	ContentType       string
	Boundaries        []string
	TransferEncodings []string
	SubjectEncoding   string
	MessageID         string
	Date              string
}

func (s *EmailService) buildMIMEMessage(to, subject, body string, attachments []models.EmailAttachment) ([]byte, emailMIMEMetadata, error) {
	plainTextBody := htmlToPlainText(body)
	var msg bytes.Buffer
	meta := emailMIMEMetadata{}

	fromHeader, err := formatMailboxHeader(s.smtpConfig.FromEmail)
	if err != nil {
		return nil, meta, err
	}

	toHeader, err := formatMailboxHeader(to)
	if err != nil {
		return nil, meta, err
	}

	encodedSubject := encodeSubjectHeader(subject)
	messageID := generateMessageID(s.smtpConfig.FromEmail)
	dateHeader := time.Now().UTC().Format(time.RFC1123Z)

	meta.SubjectEncoding = encodedSubject
	meta.MessageID = messageID
	meta.Date = dateHeader
	meta.TransferEncodings = []string{"quoted-printable"}

	writeHeaderLine(&msg, "From", fromHeader)
	writeHeaderLine(&msg, "To", toHeader)
	writeHeaderLine(&msg, "Subject", encodedSubject)
	writeHeaderLine(&msg, "Date", dateHeader)
	writeHeaderLine(&msg, "Message-ID", messageID)
	writeHeaderLine(&msg, "MIME-Version", "1.0")

	if len(attachments) == 0 {
		boundary := generateBoundary("alt")
		meta.ContentType = fmt.Sprintf("multipart/alternative; boundary=%q", boundary)
		meta.Boundaries = []string{boundary}

		writeHeaderLine(&msg, "Content-Type", meta.ContentType)
		msg.WriteString("\r\n")

		writer := multipart.NewWriter(&msg)
		if err := writer.SetBoundary(boundary); err != nil {
			return nil, meta, err
		}

		if err := writeTextPart(writer, "text/plain; charset=UTF-8", "quoted-printable", plainTextBody); err != nil {
			return nil, meta, err
		}
		if err := writeTextPart(writer, "text/html; charset=UTF-8", "quoted-printable", body); err != nil {
			return nil, meta, err
		}
		if err := writer.Close(); err != nil {
			return nil, meta, err
		}

		return msg.Bytes(), meta, nil
	}

	mixedBoundary := generateBoundary("mixed")
	altBoundary := generateBoundary("alt")
	meta.ContentType = fmt.Sprintf("multipart/mixed; boundary=%q", mixedBoundary)
	meta.Boundaries = []string{mixedBoundary, altBoundary}
	meta.TransferEncodings = []string{"quoted-printable", "base64"}

	writeHeaderLine(&msg, "Content-Type", meta.ContentType)
	msg.WriteString("\r\n")

	mixedWriter := multipart.NewWriter(&msg)
	if err := mixedWriter.SetBoundary(mixedBoundary); err != nil {
		return nil, meta, err
	}

	altHeader := textproto.MIMEHeader{}
	altHeader.Set("Content-Type", fmt.Sprintf("multipart/alternative; boundary=%q", altBoundary))
	altPart, err := mixedWriter.CreatePart(altHeader)
	if err != nil {
		return nil, meta, err
	}

	altWriter := multipart.NewWriter(altPart)
	if err := altWriter.SetBoundary(altBoundary); err != nil {
		return nil, meta, err
	}

	if err := writeTextPart(altWriter, "text/plain; charset=UTF-8", "quoted-printable", plainTextBody); err != nil {
		return nil, meta, err
	}
	if err := writeTextPart(altWriter, "text/html; charset=UTF-8", "quoted-printable", body); err != nil {
		return nil, meta, err
	}
	if err := altWriter.Close(); err != nil {
		return nil, meta, err
	}

	for _, attachment := range attachments {
		attachmentHeader := textproto.MIMEHeader{}
		contentType := attachment.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		attachmentHeader.Set("Content-Type", contentType)
		attachmentHeader.Set("Content-Transfer-Encoding", "base64")
		attachmentHeader.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", attachment.Filename))

		attachmentPart, err := mixedWriter.CreatePart(attachmentHeader)
		if err != nil {
			return nil, meta, err
		}

		if err := writeBase64Attachment(attachmentPart, attachment); err != nil {
			return nil, meta, err
		}
	}

	if err := mixedWriter.Close(); err != nil {
		return nil, meta, err
	}

	return msg.Bytes(), meta, nil
}

func writeHeaderLine(buf *bytes.Buffer, key, value string) {
	buf.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
}

func formatMailboxHeader(address string) (string, error) {
	parsed, err := mail.ParseAddress(address)
	if err == nil {
		return parsed.String(), nil
	}

	if address == "" {
		return "", fmt.Errorf("email address is empty")
	}

	return (&mail.Address{Address: address}).String(), nil
}

func encodeSubjectHeader(subject string) string {
	if isASCII(subject) {
		return subject
	}

	return mime.QEncoding.Encode("UTF-8", subject)
}

func isASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] > 127 {
			return false
		}
	}

	return true
}

func generateBoundary(prefix string) string {
	var randomBytes [12]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}

	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixNano(), hex.EncodeToString(randomBytes[:]))
}

func generateMessageID(fromEmail string) string {
	host := "localhost"
	if parsed, err := mail.ParseAddress(fromEmail); err == nil {
		if domain := strings.Split(parsed.Address, "@"); len(domain) == 2 && domain[1] != "" {
			host = domain[1]
		}
	}

	if hostName, err := os.Hostname(); err == nil && hostName != "" {
		host = hostName
	}

	var randomBytes [8]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return fmt.Sprintf("<%d@%s>", time.Now().UnixNano(), host)
	}

	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), hex.EncodeToString(randomBytes[:]), host)
}

func writeTextPart(writer *multipart.Writer, contentType, transferEncoding, body string) error {
	headers := textproto.MIMEHeader{}
	headers.Set("Content-Type", contentType)
	headers.Set("Content-Transfer-Encoding", transferEncoding)

	part, err := writer.CreatePart(headers)
	if err != nil {
		return err
	}

	qpWriter := quotedprintable.NewWriter(part)
	if _, err := qpWriter.Write([]byte(body)); err != nil {
		_ = qpWriter.Close()
		return err
	}

	return qpWriter.Close()
}

func writeBase64Attachment(partWriter io.Writer, attachment models.EmailAttachment) error {
	data := attachment.Data
	if attachment.IsBase64 {
		data = []byte(strings.TrimSpace(string(attachment.Data)))
	} else {
		encoded := make([]byte, base64.StdEncoding.EncodedLen(len(attachment.Data)))
		base64.StdEncoding.Encode(encoded, attachment.Data)
		data = encoded
	}

	for i := 0; i < len(data); i += 76 {
		end := i + 76
		if end > len(data) {
			end = len(data)
		}
		if _, err := partWriter.Write(data[i:end]); err != nil {
			return err
		}
		if _, err := partWriter.Write([]byte("\r\n")); err != nil {
			return err
		}
	}

	return nil
}

func htmlToPlainText(body string) string {
	plain := body
	replacer := strings.NewReplacer(
		"<br>", "\n",
		"<br/>", "\n",
		"<br />", "\n",
		"</p>", "\n\n",
		"</div>", "\n",
		"</h1>", "\n",
		"</h2>", "\n",
		"</h3>", "\n",
		"</li>", "\n",
	)
	plain = replacer.Replace(plain)
	plain = regexp.MustCompile(`(?s)<style.*?</style>`).ReplaceAllString(plain, "")
	plain = regexp.MustCompile(`(?s)<script.*?</script>`).ReplaceAllString(plain, "")
	plain = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(plain, "")
	plain = strings.ReplaceAll(plain, "&nbsp;", " ")
	plain = strings.ReplaceAll(plain, "&amp;", "&")
	plain = strings.ReplaceAll(plain, "&lt;", "<")
	plain = strings.ReplaceAll(plain, "&gt;", ">")
	plain = strings.ReplaceAll(plain, "&quot;", `"`)
	plain = strings.ReplaceAll(plain, "&#39;", "'")

	lines := strings.Split(plain, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}

	return strings.Join(cleaned, "\n")
}
