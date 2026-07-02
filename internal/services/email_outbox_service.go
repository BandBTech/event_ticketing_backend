package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"event-ticketing-backend/internal/models"

	"gorm.io/gorm"
)

// EmailOutboxService implements the outbox pattern for reliable email delivery
type EmailOutboxService struct {
	db *gorm.DB
}

// NewEmailOutboxService creates a new email outbox service
func NewEmailOutboxService(db *gorm.DB) *EmailOutboxService {
	return &EmailOutboxService{db: db}
}

// QueueEmail adds an email to the outbox for reliable delivery
func (s *EmailOutboxService) QueueEmail(ctx context.Context, eventType, recipientEmail, subject string, templateData map[string]interface{}, priority int) error {
	data := models.JSONMap(templateData)
	outbox := &models.EmailOutbox{
		EventType:      eventType,
		RecipientEmail: recipientEmail,
		Subject:        subject,
		TemplateData:   &data,
		Priority:       priority,
		Status:         models.EmailStatusPending,
		MaxRetries:     3,
		NextAttemptAt:  time.Now(), // Immediate for high priority, delayed for others
	}

	// Add delay based on priority
	if priority <= 1 { // Normal/low priority
		outbox.NextAttemptAt = time.Now().Add(5 * time.Minute)
	}

	if err := s.db.Create(outbox).Error; err != nil {
		return fmt.Errorf("failed to queue email: %w", err)
	}

	log.Printf("✓ Email queued: type=%s, to=%s, priority=%d", eventType, recipientEmail, priority)
	return nil
}

// ProcessEmailOutbox processes pending emails from the outbox
func (s *EmailOutboxService) ProcessEmailOutbox(ctx context.Context, emailService *EmailService) error {
	// Get pending emails ready for processing
	var pendingEmails []models.EmailOutbox
	now := time.Now()

	if err := s.db.Where("status = ? AND next_attempt_at <= ?",
		models.EmailStatusPending, now).
		Order("priority DESC, created_at ASC").
		Limit(50). // Process in batches
		Find(&pendingEmails).Error; err != nil {
		return fmt.Errorf("failed to fetch pending emails: %w", err)
	}

	if len(pendingEmails) == 0 {
		return nil // Nothing to process
	}

	processed := 0
	failed := 0

	for _, email := range pendingEmails {
		if err := s.processSingleEmail(ctx, emailService, &email); err != nil {
			failed++
			log.Printf("✗ Failed to process email %s: %v", email.ID, err)
		} else {
			processed++
		}
	}

	log.Printf("✓ Processed %d emails (%d successful, %d failed)", len(pendingEmails), processed, failed)
	return nil
}

// processSingleEmail processes a single email from the outbox
func (s *EmailOutboxService) processSingleEmail(ctx context.Context, emailService *EmailService, email *models.EmailOutbox) error {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Mark as processing
	now := time.Now()
	email.Status = models.EmailStatusProcessing
	email.LastAttemptAt = &now
	if err := tx.Save(email).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to mark email as processing: %w", err)
	}

	// Process based on event type
	var err error
	switch email.EventType {
	case models.EmailEventTicketConfirmation:
		err = s.sendTicketConfirmationEmail(emailService, email)
	case models.EmailEventPaymentFailed:
		err = s.sendPaymentFailedEmail(emailService, email)
	case models.EmailEventPaymentCanceled:
		err = s.sendPaymentCanceledEmail(emailService, email)
	case models.EmailEventRefundProcessed:
		err = s.sendRefundProcessedEmail(emailService, email)
	case models.EmailEventRefundStatusUpdate:
		err = s.sendRefundStatusUpdateEmail(emailService, email)
	case models.EmailEventEventCancellation:
		err = s.sendEventCancellationEmail(emailService, email)
	default:
		err = fmt.Errorf("unknown email event type: %s", email.EventType)
	}

	if err != nil {
		// Handle failure
		email.RetryCount++
		if email.RetryCount >= email.MaxRetries {
			email.Status = models.EmailStatusFailed
			email.ErrorMessage = err.Error()
		} else {
			email.Status = models.EmailStatusPending
			// Exponential backoff: 5min, 30min, 2hours
			delays := []time.Duration{5 * time.Minute, 30 * time.Minute, 2 * time.Hour}
			delayIndex := email.RetryCount - 1
			if delayIndex >= len(delays) {
				delayIndex = len(delays) - 1
			}
			email.NextAttemptAt = time.Now().Add(delays[delayIndex])
		}
	} else {
		// Success
		email.Status = models.EmailStatusSent
	}

	if err := tx.Save(email).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update email status: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit email processing: %w", err)
	}

	return err // Return original error if any
}

// sendTicketConfirmationEmail sends ticket confirmation email with guest/registered user templates
func (s *EmailOutboxService) sendTicketConfirmationEmail(emailService *EmailService, email *models.EmailOutbox) error {
	if email.TemplateData == nil {
		return fmt.Errorf("template data is nil")
	}

	// Extract basic info for template selection
	var ticketCount int
	if v, ok := (*email.TemplateData)["ticket_count"].(int); ok {
		ticketCount = v
	} else if v, ok := (*email.TemplateData)["ticket_count"].(float64); ok {
		ticketCount = int(v)
	}

	// Check if this is a guest user purchase
	isGuest := false
	if v, ok := (*email.TemplateData)["is_guest"].(bool); ok {
		isGuest = v
	}

	// Use different template based on user type
	var templateName string
	if isGuest {
		templateName = "guest_order_confirmation.html"
	} else {
		templateName = "order_confirmation.html"
	}

	// Build email data by copying all template data and ensuring all required fields exist
	// The payment_worker already prepared most fields; we just ensure critical ones are present
	templateData := make(map[string]interface{})

	// Copy all existing template data
	for k, v := range *email.TemplateData {
		templateData[k] = v
	}

	// Ensure critical fields for template rendering
	if _, ok := templateData["total_tickets"]; !ok {
		if _, okUpper := templateData["TotalTickets"]; !okUpper {
			templateData["total_tickets"] = ticketCount
			templateData["TotalTickets"] = ticketCount
		}
	}
	if _, ok := templateData["year"]; !ok {
		if _, okUpper := templateData["Year"]; !okUpper {
			templateData["year"] = time.Now().Year()
			templateData["Year"] = time.Now().Year()
		}
	}
	if _, ok := templateData["is_guest"]; !ok {
		if _, okUpper := templateData["IsGuest"]; !okUpper {
			templateData["is_guest"] = isGuest
			templateData["IsGuest"] = isGuest
		}
	}

	// Extract values from templateData to populate EmailData fields directly
	var eventName, eventDate, eventTime, venue, organizerName string
	var totalAmount float64
	var totalTickets int

	if v, ok := templateData["event_name"].(string); ok {
		eventName = v
	} else if v, ok := templateData["EventName"].(string); ok {
		eventName = v
	}
	if v, ok := templateData["event_date"].(string); ok {
		eventDate = v
	} else if v, ok := templateData["EventDate"].(string); ok {
		eventDate = v
	}
	if v, ok := templateData["event_time"].(string); ok {
		eventTime = v
	} else if v, ok := templateData["EventTime"].(string); ok {
		eventTime = v
	}
	if v, ok := templateData["venue"].(string); ok {
		venue = v
	} else if v, ok := templateData["Venue"].(string); ok {
		venue = v
	}
	if v, ok := templateData["organizer_name"].(string); ok {
		organizerName = v
	} else if v, ok := templateData["OrganizerName"].(string); ok {
		organizerName = v
	}
	if v, ok := templateData["total_amount"].(float64); ok {
		totalAmount = v
	} else if v, ok := templateData["TotalAmount"].(float64); ok {
		totalAmount = v
	}
	if v, ok := templateData["total_tickets"].(int); ok {
		totalTickets = v
	} else if v, ok := templateData["total_tickets"].(float64); ok {
		totalTickets = int(v)
	} else if v, ok := templateData["TotalTickets"].(int); ok {
		totalTickets = v
	} else if v, ok := templateData["TotalTickets"].(float64); ok {
		totalTickets = int(v)
	}
	if totalTickets == 0 {
		totalTickets = ticketCount
	}

	recipientName, _ := templateData["user_name"].(string)
	if recipientName == "" {
		recipientName, _ = templateData["guest_name"].(string)
	}
	if recipientName == "" {
		recipientName, _ = templateData["RecipientName"].(string)
	}
	if recipientName == "" {
		recipientName, _ = templateData["GuestName"].(string)
	}

	// Build email data wrapper
	data := EmailData{
		To:            email.RecipientEmail,
		Subject:       email.Subject,
		Title:         "Order Confirmation",
		Message:       "Your order has been confirmed! Your tickets are ready for use.",
		RecipientName: recipientName,
		CurrentYear:   time.Now().Year(),
		EventName:     eventName,
		EventDate:     eventDate,
		EventTime:     eventTime,
		Venue:         venue,
		OrganizerName: organizerName,
		TotalTickets:  totalTickets,
		TotalAmount:   totalAmount,
		Data:          templateData,
	}

	return emailService.SendEmail(
		email.RecipientEmail,
		email.Subject,
		templateName,
		data,
	)
}

// sendPaymentFailedEmail sends payment failed notification
func (s *EmailOutboxService) sendPaymentFailedEmail(emailService *EmailService, email *models.EmailOutbox) error {
	if email.TemplateData == nil {
		return fmt.Errorf("template data is nil")
	}

	// Extract template data
	eventName, _ := (*email.TemplateData)["event_name"].(string)
	amount, _ := (*email.TemplateData)["amount"].(float64)
	currency, _ := (*email.TemplateData)["currency"].(string)
	reason, _ := (*email.TemplateData)["reason"].(string)

	return emailService.SendPaymentFailedEmail(
		email.RecipientEmail,
		eventName,
		amount,
		currency,
		reason,
	)
}

// sendPaymentCanceledEmail sends payment canceled notification
func (s *EmailOutboxService) sendPaymentCanceledEmail(emailService *EmailService, email *models.EmailOutbox) error {
	if email.TemplateData == nil {
		return fmt.Errorf("template data is nil")
	}

	// Extract template data
	eventName, _ := (*email.TemplateData)["event_name"].(string)
	amount, _ := (*email.TemplateData)["amount"].(float64)
	currency, _ := (*email.TemplateData)["currency"].(string)

	return emailService.SendPaymentCanceledEmail(
		email.RecipientEmail,
		eventName,
		amount,
		currency,
	)
}

// sendRefundProcessedEmail sends refund processed notification
func (s *EmailOutboxService) sendRefundProcessedEmail(emailService *EmailService, email *models.EmailOutbox) error {
	if email.TemplateData == nil {
		return fmt.Errorf("template data is nil")
	}

	// Use the full template data for dynamic content
	data := EmailData{
		Data: *email.TemplateData, // Pass the entire template data map
	}

	return emailService.SendEmail(email.RecipientEmail, email.Subject, "refund_processed.html", data)
}

// ✅ NEW: sendRefundStatusUpdateEmail sends refund status update notification for all refund statuses
func (s *EmailOutboxService) sendRefundStatusUpdateEmail(emailService *EmailService, email *models.EmailOutbox) error {
	if email.TemplateData == nil {
		return fmt.Errorf("template data is nil")
	}

	// Use the full template data for dynamic content
	data := EmailData{
		Data: *email.TemplateData, // Pass the entire template data map
	}

	return emailService.SendEmail(email.RecipientEmail, email.Subject, "refund_status_update.html", data)
}

func (s *EmailOutboxService) sendEventCancellationEmail(emailService *EmailService, email *models.EmailOutbox) error {
	if email.TemplateData == nil {
		return fmt.Errorf("template data is nil")
	}

	// Extract template data
	userName, _ := (*email.TemplateData)["user_name"].(string)
	eventName, _ := (*email.TemplateData)["event_name"].(string)
	organizerName, _ := (*email.TemplateData)["organizer_name"].(string)
	refundAmount, _ := (*email.TemplateData)["refund_amount"].(float64)
	currency, _ := (*email.TemplateData)["currency"].(string)
	ticketCount, _ := (*email.TemplateData)["ticket_count"].(int)
	transactionID, _ := (*email.TemplateData)["transaction_id"].(string)
	eventDate, _ := (*email.TemplateData)["event_date"].(string)
	eventLocation, _ := (*email.TemplateData)["event_location"].(string)

	return emailService.SendEventCancellationEmail(
		email.RecipientEmail,
		userName,
		eventName,
		organizerName,
		refundAmount,
		currency,
		ticketCount,
		transactionID,
		eventDate,
		eventLocation,
	)
}

// GetEmailOutboxStats returns statistics about the email outbox
func (s *EmailOutboxService) GetEmailOutboxStats(ctx context.Context) (map[string]int64, error) {
	var stats struct {
		Pending    int64
		Processing int64
		Sent       int64
		Failed     int64
	}

	// Use SUM with CASE for PostgreSQL compatibility
	err := s.db.Model(&models.EmailOutbox{}).
		Select("SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) as pending",
			"SUM(CASE WHEN status = 'processing' THEN 1 ELSE 0 END) as processing",
			"SUM(CASE WHEN status = 'sent' THEN 1 ELSE 0 END) as sent",
			"SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed").
		Scan(&stats).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get email stats: %w", err)
	}

	return map[string]int64{
		"pending":    stats.Pending,
		"processing": stats.Processing,
		"sent":       stats.Sent,
		"failed":     stats.Failed,
	}, nil
}
