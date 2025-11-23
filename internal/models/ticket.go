package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Ticket represents a purchased event ticket
type Ticket struct {
	ID              uuid.UUID      `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	TicketNumber    string         `gorm:"unique;not null;size:50" json:"ticket_number"` // Unique ticket identifier
	UserID          *uuid.UUID     `gorm:"type:uuid;index" json:"user_id,omitempty"`     // Nullable for guest purchases
	User            *User          `gorm:"foreignKey:UserID" json:"user,omitempty"`
	GuestUserID     *uuid.UUID     `gorm:"type:uuid;index" json:"guest_user_id,omitempty"` // For guest purchases
	GuestUser       *GuestUser     `gorm:"foreignKey:GuestUserID" json:"guest_user,omitempty"`
	EventID         uuid.UUID      `gorm:"type:uuid;not null;index" json:"event_id"`
	Event           *Event         `gorm:"foreignKey:EventID" json:"event,omitempty"`
	Quantity        int            `gorm:"not null;default:1" json:"quantity"`
	CheckedInCount  int            `gorm:"default:0" json:"checked_in_count"` // Number of people checked in from this ticket
	TotalAmount     float64        `gorm:"not null" json:"total_amount"`
	Status          string         `gorm:"not null;default:'active'" json:"status"` // active, pending_verification, used, cancelled, refunded
	IsGuestPurchase bool           `gorm:"default:false" json:"is_guest_purchase"`
	CheckInTime     *time.Time     `json:"check_in_time,omitempty"`
	CheckOutTime    *time.Time     `json:"check_out_time,omitempty"`
	CheckedInBy     *uuid.UUID     `gorm:"type:uuid" json:"checked_in_by,omitempty"`
	CheckedOutBy    *uuid.UUID     `gorm:"type:uuid" json:"checked_out_by,omitempty"`
	PurchaseDate    time.Time      `json:"purchase_date"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

// TicketPurchaseRequest represents the request to purchase tickets
type TicketPurchaseRequest struct {
	EventID  uuid.UUID `json:"event_id" binding:"required"`
	Quantity int       `json:"quantity" binding:"required,min=1,max=10"`
}

// TicketCheckInRequest represents the request to check-in a ticket
type TicketCheckInRequest struct {
	TicketNumber string    `json:"ticket_number" binding:"required"`
	EventID      uuid.UUID `json:"event_id" binding:"required"`
	CheckInCount int       `json:"check_in_count,omitempty"` // Number of people checking in (for multiple quantity tickets)
}

// TicketCheckOutRequest represents the request to check-out a ticket
type TicketCheckOutRequest struct {
	TicketNumber string    `json:"ticket_number" binding:"required"`
	EventID      uuid.UUID `json:"event_id" binding:"required"`
}

// TicketResponse represents the ticket data in API responses
type TicketResponse struct {
	ID              uuid.UUID          `json:"id"`
	TicketNumber    string             `json:"ticket_number"`
	UserID          *uuid.UUID         `json:"user_id,omitempty"`
	User            *UserResponse      `json:"user,omitempty"`
	GuestUserID     *uuid.UUID         `json:"guest_user_id,omitempty"`
	GuestUser       *GuestUserResponse `json:"guest_user,omitempty"`
	EventID         uuid.UUID          `json:"event_id"`
	Event           *Event             `json:"event,omitempty"`
	Quantity        int                `json:"quantity"`
	CheckedInCount  int                `json:"checked_in_count"`
	TotalAmount     float64            `json:"total_amount"`
	Status          string             `json:"status"`
	IsGuestPurchase bool               `json:"is_guest_purchase"`
	CheckInTime     *time.Time         `json:"check_in_time,omitempty"`
	CheckOutTime    *time.Time         `json:"check_out_time,omitempty"`
	CheckedInBy     *uuid.UUID         `json:"checked_in_by,omitempty"`
	CheckedOutBy    *uuid.UUID         `json:"checked_out_by,omitempty"`
	PurchaseDate    time.Time          `json:"purchase_date"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

// BeforeCreate generates a unique ticket number
func (t *Ticket) BeforeCreate(tx *gorm.DB) error {
	if t.TicketNumber == "" {
		t.TicketNumber = generateTicketNumber()
	}
	if t.PurchaseDate.IsZero() {
		t.PurchaseDate = time.Now()
	}
	return nil
}

// generateTicketNumber creates a unique ticket number
func generateTicketNumber() string {
	// Generate a short unique ticket number like TKT-ABC12345
	return "TKT-" + uuid.New().String()[:8]
}

// ToResponse converts a Ticket model to a TicketResponse
func (t *Ticket) ToResponse() TicketResponse {
	var userResp *UserResponse
	if t.User != nil {
		resp := t.User.ToResponse()
		userResp = &resp
	}

	var guestUserResp *GuestUserResponse
	if t.GuestUser != nil {
		resp := t.GuestUser.ToResponse()
		guestUserResp = &resp
	}

	return TicketResponse{
		ID:              t.ID,
		TicketNumber:    t.TicketNumber,
		UserID:          t.UserID,
		User:            userResp,
		GuestUserID:     t.GuestUserID,
		GuestUser:       guestUserResp,
		EventID:         t.EventID,
		Event:           t.Event,
		Quantity:        t.Quantity,
		CheckedInCount:  t.CheckedInCount,
		TotalAmount:     t.TotalAmount,
		Status:          t.Status,
		IsGuestPurchase: t.IsGuestPurchase,
		CheckInTime:     t.CheckInTime,
		CheckOutTime:    t.CheckOutTime,
		CheckedInBy:     t.CheckedInBy,
		CheckedOutBy:    t.CheckedOutBy,
		PurchaseDate:    t.PurchaseDate,
		CreatedAt:       t.CreatedAt,
		UpdatedAt:       t.UpdatedAt,
	}
}
