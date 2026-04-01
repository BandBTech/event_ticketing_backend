package models

import (
	"time"

	"github.com/google/uuid"
)

// EmailOutbox implements the outbox pattern for reliable email delivery
// CRITICAL: Ensures no emails are lost even if workers crash
type EmailOutbox struct {
	ID             uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	EventType      string     `gorm:"not null;size:50;index" json:"event_type"` // ticket_confirmation, payment_failed, etc.
	RecipientEmail string     `gorm:"not null;size:255;index" json:"recipient_email"`
	Subject        string     `gorm:"not null" json:"subject"`
	BodyHTML       string     `gorm:"type:text" json:"body_html,omitempty"`
	BodyText       string     `gorm:"type:text" json:"body_text,omitempty"`
	TemplateData   *JSONMap   `gorm:"type:jsonb" json:"template_data,omitempty"`     // Template variables
	Priority       int        `gorm:"default:1" json:"priority"`                     // 1=normal, 2=high, 3=critical
	Status         string     `gorm:"size:20;default:'pending';index" json:"status"` // pending, processing, sent, failed
	MaxRetries     int        `gorm:"default:3" json:"max_retries"`
	RetryCount     int        `gorm:"default:0" json:"retry_count"`
	LastAttemptAt  *time.Time `json:"last_attempt_at,omitempty"`
	NextAttemptAt  time.Time  `gorm:"default:now()" json:"next_attempt_at"`
	ErrorMessage   string     `gorm:"type:text" json:"error_message,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// EmailOutboxStatus constants
const (
	EmailStatusPending    = "pending"
	EmailStatusProcessing = "processing"
	EmailStatusSent       = "sent"
	EmailStatusFailed     = "failed"
)

// EmailEventType constants
const (
	EmailEventTicketConfirmation = "ticket_confirmation"
	EmailEventPaymentFailed      = "payment_failed"
	EmailEventPaymentCanceled    = "payment_canceled"
	EmailEventRefundProcessed    = "refund_processed"
	EmailEventEventCancellation  = "event_cancellation"
)
