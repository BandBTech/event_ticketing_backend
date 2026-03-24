package models

import (
	"time"

	"github.com/google/uuid"
)

// ProcessingLock prevents double processing of the same event
// CRITICAL: Ensures only one worker processes a payment/webhook at a time
type ProcessingLock struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	LockKey   string    `gorm:"unique;not null;size:255;index" json:"lock_key"` // e.g., "payment:stripe:pi_123456"
	LockType  string    `gorm:"not null;size:50;index" json:"lock_type"`        // payment, webhook, reconciliation
	OwnerID   string    `gorm:"not null;size:255" json:"owner_id"`              // worker ID or instance ID
	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// LockType constants
const (
	LockTypePayment        = "payment"
	LockTypeWebhook        = "webhook"
	LockTypeReconciliation = "reconciliation"
)

// IsExpired checks if lock has expired
func (l *ProcessingLock) IsExpired() bool {
	return time.Now().After(l.ExpiresAt)
}
