package models

import (
	"time"

	"github.com/google/uuid"
)

// TicketReservation represents a temporary ticket reservation with expiry
// CRITICAL: Prevents overselling while allowing payment processing time
type TicketReservation struct {
	ID uuid.UUID

	CheckoutToken string

	EventID uuid.UUID
	Event   Event `gorm:"foreignKey:EventID"`
	TierID  uuid.UUID
	Tier    EventTier `gorm:"foreignKey:TierID"`

	ActorID   uuid.UUID
	ActorType string // user | guest

	CustomerEmail string
	Quantity      int

	Status string // reserved, confirmed, expired

	ExpiresAt   time.Time
	ConfirmedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ReservationStatus constants
const (
	ReservationStatusReserved  = "reserved"
	ReservationStatusConfirmed = "confirmed"
	ReservationStatusExpired   = "expired"
	ReservationStatusCancelled = "cancelled"
)

// IsExpired checks if reservation has expired
func (r *TicketReservation) IsExpired() bool {
	return time.Now().After(r.ExpiresAt)
}

// IsActive checks if reservation is still valid
func (r *TicketReservation) IsActive() bool {
	return r.Status == ReservationStatusReserved && !r.IsExpired()
}

// CanConfirm checks if reservation can be confirmed
func (r *TicketReservation) CanConfirm() bool {
	return r.Status == ReservationStatusReserved && !r.IsExpired()
}
