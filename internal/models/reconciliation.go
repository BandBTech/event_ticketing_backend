package models

import (
	"time"

	"github.com/google/uuid"
)

// StripeEventReconciliation tracks Stripe events for reconciliation
// CRITICAL: Ensures no webhook events are missed or processed out of order
type StripeEventReconciliation struct {
	ID              uuid.UUID              `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	StripeEventID   string                 `gorm:"unique;not null;size:255;index" json:"stripe_event_id"`
	EventType       string                 `gorm:"not null;size:100" json:"event_type"`
	PaymentIntentID string                 `gorm:"size:255;index" json:"payment_intent_id,omitempty"`
	Status          string                 `gorm:"size:20;default:'pending';index" json:"status"` // pending, processed, reconciled, failed
	RawEvent        map[string]interface{} `gorm:"type:jsonb;not null" json:"raw_event"`
	ProcessedAt     *time.Time             `json:"processed_at,omitempty"`
	ReconciledAt    *time.Time             `json:"reconciled_at,omitempty"`
	ErrorMessage    string                 `gorm:"type:text" json:"error_message,omitempty"`
	RetryCount      int                    `gorm:"default:0" json:"retry_count"`
	MaxRetries      int                    `gorm:"default:3" json:"max_retries"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

// TableName specifies the table name for StripeEventReconciliation
func (StripeEventReconciliation) TableName() string {
	return "stripe_event_reconciliation"
}

// ReconciliationStatus constants
const (
	ReconciliationStatusPending    = "pending"
	ReconciliationStatusProcessed  = "processed"
	ReconciliationStatusReconciled = "reconciled"
	ReconciliationStatusFailed     = "failed"
)
