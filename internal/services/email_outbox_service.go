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
		TemplateData:   models.JSONMap(templateData),
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
	// Extract basic info for template selection
	var ticketCount int
	if v, ok := email.TemplateData["ticket_count"].(int); ok {
		ticketCount = v
	} else if v, ok := email.TemplateData["ticket_count"].(float64); ok {
		ticketCount = int(v)
	}

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

	// Build email data by copying all template data and ensuring all required fields exist
	// The payment_worker already prepared most fields; we just ensure critical ones are present
	templateData := make(map[string]interface{})

	// Copy all existing template data
	for k, v := range email.TemplateData {
		templateData[k] = v
	}

	// Ensure critical fields for template rendering
	if _, ok := templateData["total_tickets"]; !ok {
		templateData["total_tickets"] = ticketCount
	}
	if _, ok := templateData["year"]; !ok {
		templateData["year"] = time.Now().Year()
	}
	if _, ok := templateData["is_guest"]; !ok {
		templateData["is_guest"] = isGuest
	}

	// Verify tickets array is properly formatted (required for template access: (index .tickets 0).view_url)
	if tickets, ok := templateData["tickets"]; ok {
		if ticketList, ok := tickets.([]interface{}); ok && len(ticketList) > 0 {
			// Tickets array exists and is non-empty - template can render (index .tickets 0)
			log.Printf("[EMAIL_QUEUE] Tickets array present with %d items", len(ticketList))
		} else if ticketList, ok := tickets.([]map[string]interface{}); ok && len(ticketList) > 0 {
			// Alternative format - already good
			log.Printf("[EMAIL_QUEUE] Tickets array (map format) present with %d items", len(ticketList))
		} else {
			log.Printf("[EMAIL_QUEUE] WARNING: Tickets field exists but is empty or wrong format: %T", tickets)
		}
	} else {
		log.Printf("[EMAIL_QUEUE] WARNING: No tickets field in template data")
	}

	// Build email data wrapper
	data := EmailData{
		To:          email.RecipientEmail,
		Subject:     email.Subject,
		Title:       "Order Confirmation",
		Message:     "Your order has been confirmed! Your tickets are ready for use.",
		CurrentYear: time.Now().Year(),
		Data:        templateData,
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
