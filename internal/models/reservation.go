package models

import (
	"time"

	"github.com/google/uuid"
)

// TicketReservation is a time-limited hold on tier inventory.
// Created during checkout initiation, confirmed on payment success,
// expired/released on failure or timeout.
type TicketReservation struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`

	// 🔑 PRIMARY LINK (replaces payment_intent_id)
	PaymentIntentID uuid.UUID `gorm:"type:uuid;not null;index"`
	CheckoutToken   string    `gorm:"index"`
	EventID         uuid.UUID `gorm:"type:uuid;not null;index"`
	TierID          uuid.UUID `gorm:"type:uuid;not null"`

	ActorID   uuid.UUID `gorm:"type:uuid;not null"`
	ActorType ActorType `gorm:"not null"`

	CustomerEmail string `gorm:"not null"`

	Quantity int `gorm:"not null"`

	// lifecycle
	Status ReservationStatus `gorm:"not null;default:'reserved';index"`

	ExpiresAt time.Time `gorm:"not null;index"`

	ConfirmedAt *time.Time `gorm:"index"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsExpired checks if reservation has expired
func (r *TicketReservation) IsExpired() bool {
	return time.Now().After(r.ExpiresAt)
}

// IsActive checks if reservation is still valid
func (r *TicketReservation) IsActive() bool {
	return r.Status == ReservationReserved && !r.IsExpired()
}
