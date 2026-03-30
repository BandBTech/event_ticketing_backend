package models

import (
	"time"

	"github.com/google/uuid"
)

// TicketReservation represents a temporary ticket reservation with expiry
// CRITICAL: Prevents overselling while allowing payment processing time
type TicketReservation struct {
	ID            uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	CheckoutToken string     `gorm:"not null;size:255;index" json:"checkout_token"`
	EventID       uuid.UUID  `gorm:"type:uuid;not null;index" json:"event_id"`
	Event         *Event     `gorm:"foreignKey:EventID" json:"event,omitempty"`
	TierID        uuid.UUID  `gorm:"type:uuid;not null;index" json:"tier_id"`
	Tier          *EventTier `gorm:"foreignKey:TierID" json:"tier,omitempty"`
	UserID        *uuid.UUID `gorm:"type:uuid;index" json:"user_id,omitempty"`
	User          *User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
	GuestUserID   *uuid.UUID `gorm:"type:uuid;index" json:"guest_user_id,omitempty"`
	GuestUser     *GuestUser `gorm:"foreignKey:GuestUserID" json:"guest_user,omitempty"`
	CustomerEmail string     `gorm:"not null;size:255" json:"customer_email"`
	Quantity      int        `gorm:"not null" json:"quantity"`
	Status        string     `gorm:"size:20;default:'reserved';index" json:"status"` // reserved, confirmed, expired, cancelled
	ExpiresAt     time.Time  `gorm:"not null;index" json:"expires_at"`
	ConfirmedAt   *time.Time `json:"confirmed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
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
