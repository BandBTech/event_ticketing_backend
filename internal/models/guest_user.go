package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GuestUser represents a guest user who can purchase tickets without full registration
type GuestUser struct {
	ID                uuid.UUID      `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	Email             string         `gorm:"not null;size:255;index" json:"email"`
	FirstName         string         `gorm:"size:100" json:"first_name"`
	LastName          string         `gorm:"size:100" json:"last_name"`
	Phone             string         `gorm:"size:20" json:"phone,omitempty"`
	CountryCode       string         `gorm:"size:5" json:"country_code,omitempty"`
	EmailVerified     bool           `gorm:"default:false" json:"email_verified"`
	VerificationToken string         `gorm:"size:255;index" json:"-"`
	TokenExpiresAt    *time.Time     `json:"-"`
	ConvertedToUser   bool           `gorm:"default:false" json:"converted_to_user"`
	ConvertedUserID   *uuid.UUID     `gorm:"type:uuid;index" json:"converted_user_id,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

// GuestPurchaseRequest represents the request to purchase tickets as a guest
type GuestPurchaseRequest struct {
	EventID     uuid.UUID `json:"event_id" binding:"required"`
	Quantity    int       `json:"quantity" binding:"required,min=1,max=10"`
	Email       string    `json:"email" binding:"required,email"`
	FirstName   string    `json:"first_name" binding:"required,min=2,max=50"`
	LastName    string    `json:"last_name" binding:"required,min=2,max=50"`
	Phone       string    `json:"phone,omitempty"`
	CountryCode string    `json:"country_code,omitempty"`
}

// VerifyGuestEmailRequest represents the request to verify guest email
type VerifyGuestEmailRequest struct {
	Token string `json:"token" binding:"required"`
}

// GuestUserResponse represents guest user data in API responses
type GuestUserResponse struct {
	ID            uuid.UUID `json:"id"`
	Email         string    `json:"email"`
	FirstName     string    `json:"first_name"`
	LastName      string    `json:"last_name"`
	Phone         string    `json:"phone,omitempty"`
	CountryCode   string    `json:"country_code,omitempty"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
}

// IndividualTicket represents a single ticket instance for dynamic QR generation
type IndividualTicket struct {
	ID           uuid.UUID      `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	TicketID     uuid.UUID      `gorm:"type:uuid;not null;index" json:"ticket_id"` // Reference to parent ticket
	Ticket       *Ticket        `gorm:"foreignKey:TicketID" json:"ticket,omitempty"`
	TicketNumber string         `gorm:"unique;not null;size:50;index" json:"ticket_number"` // Unique individual ticket number
	QRCode       string         `gorm:"size:500" json:"qr_code,omitempty"`                  // Base64 encoded QR code
	Status       string         `gorm:"not null;default:'active'" json:"status"`            // active, used, cancelled
	CheckInTime  *time.Time     `json:"check_in_time,omitempty"`
	CheckOutTime *time.Time     `json:"check_out_time,omitempty"`
	CheckedInBy  *uuid.UUID     `gorm:"type:uuid" json:"checked_in_by,omitempty"`
	CheckedOutBy *uuid.UUID     `gorm:"type:uuid" json:"checked_out_by,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate generates a unique ticket number for individual tickets
func (it *IndividualTicket) BeforeCreate(tx *gorm.DB) error {
	if it.TicketNumber == "" {
		it.TicketNumber = generateIndividualTicketNumber()
	}
	return nil
}

// generateIndividualTicketNumber creates a unique individual ticket number
func generateIndividualTicketNumber() string {
	// Generate a ticket number like ITKT-20241117-ABC123
	now := time.Now()
	return "ITKT-" + now.Format("20060102") + "-" + uuid.New().String()[:8]
}

// ToResponse converts a GuestUser model to a GuestUserResponse
func (gu *GuestUser) ToResponse() GuestUserResponse {
	return GuestUserResponse{
		ID:            gu.ID,
		Email:         gu.Email,
		FirstName:     gu.FirstName,
		LastName:      gu.LastName,
		Phone:         gu.Phone,
		CountryCode:   gu.CountryCode,
		EmailVerified: gu.EmailVerified,
		CreatedAt:     gu.CreatedAt,
	}
}
