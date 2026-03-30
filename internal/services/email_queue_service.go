package services

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
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
		userLoginURL = "https://user.timroticket.com" // fallback
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

// QueueGuestTicketConfirmationEmail queues ticket emails for guest purchases
func (s *EmailQueueService) QueueGuestTicketConfirmationEmail(guestEmail string, tickets []*models.Ticket) error {
	if len(tickets) == 0 {
		return utils.NewBusinessLogicError("No tickets provided for email")
	}

	// Use first ticket for common details
	firstTicket := tickets[0]

	// Generate secure view URL with JWT token (for the first ticket, which represents the order)
	var ticketViewURL string
	if s.jwtService != nil {
		token, err := s.jwtService.GenerateTicketAccessToken(firstTicket)
		if err != nil {
			log.Printf("WARN: Failed to generate ticket JWT token: %v", err)
			// Fallback: just use basic link without token
			ticketViewURL = fmt.Sprintf("%s/tickets/view?ticket_id=%s", s.config.URLs.UserBaseURL, firstTicket.TicketNumber)
		} else {
			ticketViewURL = fmt.Sprintf("%s/tickets/view?token=%s", s.config.URLs.UserBaseURL, token)
		}
	} else {
		// If JWT service not available, use basic link
		ticketViewURL = fmt.Sprintf("%s/tickets/view?ticket_id=%s", s.config.URLs.UserBaseURL, firstTicket.TicketNumber)
	}

	// Generate QR code for first ticket
	qrCodeBase64, err := s.secureQRService.GenerateSecureQR(firstTicket, firstTicket.Event)
	if err != nil {
		return utils.NewInternalServerError(fmt.Sprintf("Failed to generate secure QR code for ticket %s.", firstTicket.TicketNumber), err)
	}

	// Calculate total amount for all tickets
	totalAmount := float64(0)
	for _, ticket := range tickets {
		totalAmount += ticket.TotalAmount
	}

	// Get guest name from guest user
	guestName := "Guest"
	if firstTicket.GuestUser != nil {
		lastName := firstTicket.GuestUser.LastName
		firstName := firstTicket.GuestUser.FirstName
		if firstName != "" && lastName != "" {
			guestName = fmt.Sprintf("%s %s", firstName, lastName)
		} else if firstName != "" {
			guestName = firstName
		} else if lastName != "" {
			guestName = lastName
		}
	}

	// Get organizer name from OrganizerOnboarding (preferred) or Organizer User fields
	organizerName := "Event Organizer"
	if firstTicket.Event != nil && firstTicket.Event.Organizer != nil {
		// Try OrganizerOnboarding first
		if firstTicket.Event.Organizer.OrganizerOnboarding != nil && firstTicket.Event.Organizer.OrganizerOnboarding.BusinessName != "" {
			organizerName = firstTicket.Event.Organizer.OrganizerOnboarding.BusinessName
		} else {
			// Fall back to organizer user's first + last name
			firstName := firstTicket.Event.Organizer.FirstName
			lastName := firstTicket.Event.Organizer.LastName
			if firstName != "" && lastName != "" {
				organizerName = fmt.Sprintf("%s %s", firstName, lastName)
			} else if firstName != "" {
				organizerName = firstName
			} else if lastName != "" {
				organizerName = lastName
			}
		}
	}

	// Extract time from event start date
	eventTime := firstTicket.Event.StartDate.Format("3:04 PM")

	// Build Google Calendar URL
	googleCalendarURL := ""
	if firstTicket.Event != nil {
		// Format: https://calendar.google.com/calendar/render?action=TEMPLATE&text=Event%20Title&dates=20260430T090000/20260430T170000&details=Event%20details&location=Venue&ctz=UTC
		eventTitle := url.QueryEscape(firstTicket.Event.Title)
		startTime := firstTicket.Event.StartDate.Format("20060102T150405")
		endTime := firstTicket.Event.EndDate.Format("20060102T150405")
		googleCalendarURL = fmt.Sprintf("https://calendar.google.com/calendar/render?action=TEMPLATE&text=%s&dates=%s/%s&location=%s&ctz=UTC",
			eventTitle, startTime, endTime, url.QueryEscape(firstTicket.Event.VenueName))
	}

	// Map payment gateway name
	paymentMethod := firstTicket.PaymentGateway
	if paymentMethod == "" {
		paymentMethod = "Online"
	} else if paymentMethod == "stripe" {
		paymentMethod = "Stripe"
	} else if paymentMethod == "cash" {
		paymentMethod = "Cash"
	}

	ticketData := map[string]interface{}{
		// Template field names (must match guest_order_confirmation.html template)
		"guest_name":          guestName,
		"event_name":          firstTicket.Event.Title,
		"event_date":          firstTicket.Event.StartDate.Format("January 2, 2006"),
		"event_time":          eventTime,
		"venue":               firstTicket.Event.VenueName,
		"organizer_name":      organizerName,
		"total_tickets":       len(tickets),
		"payment_gateway":     paymentMethod,
		"total_amount":        totalAmount,
		"google_calendar_url": googleCalendarURL,
		"TicketViewURL":       ticketViewURL,
		"TotalTickets":        len(tickets),
		"year":                time.Now().Year(),
		// Legacy fields (for backward compatibility)
		"Title":         "🎫 Your Tickets Are Ready!",
		"Message":       "Here are your event tickets. The QR code is unique and should be presented at the event entrance.",
		"RecipientName": guestName,
		"EventTitle":    firstTicket.Event.Title,
		"EventDate":     firstTicket.Event.StartDate.Format("January 2, 2006 at 3:04 PM"),
		"EventLocation": firstTicket.Event.Location,
		"VenueAddress":  firstTicket.Event.VenueName,
		"TicketNumber":  firstTicket.TicketNumber,
		"QRCode":        qrCodeBase64,
		"SupportEmail":  "support@timroticket.com",
		"EventID":       firstTicket.Event.ID.String(),
		"EventVenue":    firstTicket.Event.VenueName,
		"EventCategory": firstTicket.Event.Category,
	}

	emailJob := &models.EmailJob{
		Type:         models.EmailTypeTicketConfirmation,
		To:           guestEmail,
		Subject:      fmt.Sprintf("🎫 Your Tickets Are Ready! - %s", firstTicket.Event.Title),
		TemplateFile: "guest_order_confirmation.html",
		TemplateData: ticketData,
		Priority:     models.PriorityHigh,
		MaxRetries:   3,
	}

	emailJob.SetDefaults()

	return s.queueEmailJob(emailJob)
}

// QueueUserTicketConfirmationEmail queues ticket emails for logged-in user purchases
func (s *EmailQueueService) QueueUserTicketConfirmationEmail(user *models.User, tickets []*models.Ticket) error {
	if len(tickets) == 0 {
		return utils.NewBusinessLogicError("No tickets provided for email")
	}

	// Use first ticket for common details
	firstTicket := tickets[0]

	// Generate secure view URL with JWT token (for the first ticket, which represents the order)
	var ticketViewURL string
	if s.jwtService != nil {
		token, err := s.jwtService.GenerateTicketAccessToken(firstTicket)
		if err != nil {
			log.Printf("WARN: Failed to generate ticket JWT token: %v", err)
			// Fallback: just use basic link without token
			ticketViewURL = fmt.Sprintf("%s/tickets/view?ticket_id=%s", s.config.URLs.UserBaseURL, firstTicket.TicketNumber)
		} else {
			ticketViewURL = fmt.Sprintf("%s/tickets/view?token=%s", s.config.URLs.UserBaseURL, token)
		}
	} else {
		// If JWT service not available, use basic link
		ticketViewURL = fmt.Sprintf("%s/tickets/view?ticket_id=%s", s.config.URLs.UserBaseURL, firstTicket.TicketNumber)
	}

	// Generate QR code for first ticket
	qrCodeBase64, err := s.secureQRService.GenerateSecureQR(firstTicket, firstTicket.Event)
	if err != nil {
		return utils.NewInternalServerError(fmt.Sprintf("Failed to generate secure QR code for ticket %s.", firstTicket.TicketNumber), err)
	}

	ticketData := map[string]interface{}{
		"Title":         "🎫 Your Tickets Are Ready!",
		"Message":       "Here are your event tickets. The QR code is unique and should be presented at the event entrance.",
		"RecipientName": strings.TrimSpace(user.FirstName + " " + user.LastName),
		"EventTitle":    firstTicket.Event.Title,
		"EventDate":     firstTicket.Event.StartDate.Format("January 2, 2006 at 3:04 PM"),
		"EventLocation": firstTicket.Event.Location,
		"VenueAddress":  firstTicket.Event.VenueName,
		"TicketNumber":  firstTicket.TicketNumber,
		"QRCode":        qrCodeBase64,
		"TicketViewURL": ticketViewURL,
		"TotalTickets":  len(tickets),
		"SupportEmail":  "support@timroticket.com",
		"EventID":       firstTicket.Event.ID.String(),
		"EventVenue":    firstTicket.Event.VenueName,
		"EventCategory": firstTicket.Event.Category,
	}

	emailJob := &models.EmailJob{
		Type:         models.EmailTypeTicketConfirmation,
		To:           user.Email,
		Subject:      fmt.Sprintf("🎫 Your Tickets Are Ready! - %s", firstTicket.Event.Title),
		TemplateFile: "order_confirmation.html",
		TemplateData: ticketData,
		Priority:     models.PriorityHigh,
		MaxRetries:   3,
	}

	emailJob.SetDefaults()

	return s.queueEmailJob(emailJob)
}

// QueueOrderConfirmationEmail queues a single order confirmation email with secure JWT links for logged-in users
func (s *EmailQueueService) QueueOrderConfirmationEmail(to string, emailData map[string]interface{}) error {
	emailJob := &models.EmailJob{
		Type:         models.EmailTypeOrderConfirmation,
		To:           to,
		Subject:      "Your Ticket Order Confirmation",
		TemplateFile: "order_confirmation.html", // New template for order confirmations
		TemplateData: emailData,
		Priority:     models.PriorityHigh,
		MaxRetries:   3,
	}

	emailJob.SetDefaults()

	return s.queueEmailJob(emailJob)
}

// QueueGuestOrderConfirmationEmail queues a single order confirmation email with secure JWT links for guest users
func (s *EmailQueueService) QueueGuestOrderConfirmationEmail(to string, emailData map[string]interface{}) error {
	emailJob := &models.EmailJob{
		Type:         models.EmailTypeGuestOrderConfirmation,
		To:           to,
		Subject:      "Your Ticket Order Confirmation",
		TemplateFile: "guest_order_confirmation.html", // New template for guest order confirmations
		TemplateData: emailData,
		Priority:     models.PriorityHigh,
		MaxRetries:   3,
	}

	emailJob.SetDefaults()

	return s.queueEmailJob(emailJob)
}
