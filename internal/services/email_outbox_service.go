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
	outbox := &models.EmailOutbox{
		EventType:      eventType,
		RecipientEmail: recipientEmail,
		Subject:        subject,
		TemplateData:   templateData,
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
	// Extract template data
	eventName, _ := email.TemplateData["event_name"].(string)
	totalAmount, _ := email.TemplateData["total_amount"].(float64)
	currency, _ := email.TemplateData["currency"].(string)

	// Extract ticket count - handle both int and float64
	var ticketCount int
	if v, ok := email.TemplateData["ticket_count"].(int); ok {
		ticketCount = v
	} else if v, ok := email.TemplateData["ticket_count"].(float64); ok {
		ticketCount = int(v)
	}

	transactionItems, _ := email.TemplateData["transaction_items"].([]*models.TransactionItem)

	// Check if this is a guest user purchase
	isGuest := false
	if v, ok := email.TemplateData["is_guest"].(bool); ok {
		isGuest = v
	}

	// Use different template based on user type
	var templateName string
	if isGuest {
		templateName = "guest_order_confirmation.html"
	} else {
		templateName = "order_confirmation.html"
	}

	// Build email data with all necessary fields
	data := EmailData{
		To:          email.RecipientEmail,
		Subject:     email.Subject,
		Title:       "Order Confirmation",
		Message:     "Your order has been confirmed! Your tickets are ready for use.",
		EventName:   eventName,
		TotalAmount: totalAmount,
		CurrentYear: time.Now().Year(),
		Data: map[string]interface{}{
			"event_name":          eventName,
			"total_amount":        totalAmount,
			"currency":            currency,
			"ticket_count":        ticketCount,
			"transaction_items":   transactionItems,
			"is_guest":            isGuest,
			"Google_calendar_url": email.TemplateData["google_calendar_url"],
			"tickets":             email.TemplateData["tickets"],
		},
	}

	// Populate guest/user specific fields
	if isGuest {
		// Guest confirmation fields
		if v, ok := email.TemplateData["guest_name"].(string); ok {
			data.GuestName = v
		}
		data.Data["guest_name"] = data.GuestName
	} else {
		// Registered user fields
		if v, ok := email.TemplateData["user_name"].(string); ok {
			data.RecipientName = v
		}
		data.Data["user_name"] = data.RecipientName
	}

	// Populate common event fields
	if v, ok := email.TemplateData["event_date"].(string); ok {
		data.EventDate = v
		data.Data["event_date"] = v
	}
	if v, ok := email.TemplateData["event_time"].(string); ok {
		data.EventTime = v
		data.Data["event_time"] = v
	}
	if v, ok := email.TemplateData["venue"].(string); ok {
		data.Venue = v
		data.Data["venue"] = v
	}
	if v, ok := email.TemplateData["organizer_name"].(string); ok {
		data.OrganizerName = v
		data.Data["organizer_name"] = v
	}
	if v, ok := email.TemplateData["payment_gateway"].(string); ok {
		data.Data["payment_gateway"] = v
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
	// Extract template data
	eventName, _ := email.TemplateData["event_name"].(string)
	amount, _ := email.TemplateData["amount"].(float64)
	currency, _ := email.TemplateData["currency"].(string)
	reason, _ := email.TemplateData["reason"].(string)

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
	// Extract template data
	eventName, _ := email.TemplateData["event_name"].(string)
	amount, _ := email.TemplateData["amount"].(float64)
	currency, _ := email.TemplateData["currency"].(string)

	return emailService.SendPaymentCanceledEmail(
		email.RecipientEmail,
		eventName,
		amount,
		currency,
	)
}

// sendRefundProcessedEmail sends refund processed notification
func (s *EmailOutboxService) sendRefundProcessedEmail(emailService *EmailService, email *models.EmailOutbox) error {
	// Extract template data
	eventName, _ := email.TemplateData["event_name"].(string)
	refundAmount, _ := email.TemplateData["refund_amount"].(float64)
	currency, _ := email.TemplateData["currency"].(string)
	ticketCount, _ := email.TemplateData["ticket_count"].(int)

	return emailService.SendRefundProcessedEmail(
		email.RecipientEmail,
		eventName,
		refundAmount,
		currency,
		ticketCount,
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

	err := s.db.Model(&models.EmailOutbox{}).
		Select("COUNT(CASE WHEN status = ? THEN 1 END) as pending",
			"COUNT(CASE WHEN status = ? THEN 1 END) as processing",
			"COUNT(CASE WHEN status = ? THEN 1 END) as sent",
			"COUNT(CASE WHEN status = ? THEN 1 END) as failed").
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
