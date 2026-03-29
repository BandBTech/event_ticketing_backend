package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GuestUser represents a guest user who can purchase tickets without full registration
type GuestUser struct {
	ID                uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
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
	EventID        uuid.UUID             `json:"event_id" binding:"required"`
	Tiers          []TicketTierSelection `json:"tiers" binding:"required,min=1,dive"` // Array of tier selections
	Email          string                `json:"email" binding:"required,email"`
	FirstName      string                `json:"first_name,omitempty"` // Optional, defaults to "Guest"
	LastName       string                `json:"last_name,omitempty"`  // Optional, defaults to "User"
	Phone          string                `json:"phone,omitempty"`
	CountryCode    string                `json:"country_code,omitempty"`
	PaymentGateway PaymentGateway        `json:"payment_gateway" binding:"required,purchase_payment_gateway"` // Required for payment processing - only stripe and cash allowed
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

// CheckoutSession represents a payment gateway checkout session
type CheckoutSession struct {
	ID              uuid.UUID              `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	TicketID        uuid.UUID              `json:"ticket_id" gorm:"type:uuid;not null"`
	GuestUserID     *uuid.UUID             `json:"guest_user_id,omitempty" gorm:"type:uuid"`           // Nullable for logged-in users
	UserID          *uuid.UUID             `json:"user_id,omitempty" gorm:"type:uuid"`                 // For logged-in users
	PaymentIntentID *uuid.UUID             `json:"payment_intent_id,omitempty" gorm:"type:uuid;index"` // Link to PaymentIntent record
	CheckoutToken   string                 `json:"checkout_token" gorm:"uniqueIndex;not null"`         // Unique token for security
	PaymentGateway  PaymentGateway         `json:"payment_gateway" gorm:"not null"`                    // stripe, paypal, esewa, etc.
	Amount          float64                `json:"amount" gorm:"not null"`
	Currency        string                 `json:"currency" gorm:"default:'NPR'"`                  // Default to NPR
	Status          string                 `json:"status" gorm:"default:'pending'"`                // pending, processing, completed, failed, expired
	GatewayData     map[string]interface{} `json:"gateway_data" gorm:"type:jsonb;serializer:json"` // Store gateway-specific data (session_id, payment_intent_id, etc.)
	StripeSessionID string                 `json:"stripe_session_id,omitempty" gorm:"index"`       // Stripe checkout session ID for webhook lookup
	ExpiresAt       time.Time              `json:"expires_at" gorm:"not null"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`

	// Relations
	Ticket        Ticket        `json:"-" gorm:"foreignKey:TicketID"`
	GuestUser     GuestUser     `json:"-" gorm:"foreignKey:GuestUserID"`
	User          User          `json:"-" gorm:"foreignKey:UserID"`
	PaymentIntent PaymentIntent `json:"-" gorm:"foreignKey:PaymentIntentID"`
}

// CheckoutSessionResponse represents checkout session data for API responses
type CheckoutSessionResponse struct {
	ID             uuid.UUID              `json:"id"`
	CheckoutToken  string                 `json:"checkout_token"`
	PaymentGateway PaymentGateway         `json:"payment_gateway"`
	Amount         float64                `json:"amount"`
	Currency       string                 `json:"currency"`
	Status         string                 `json:"status"`
	GatewayData    map[string]interface{} `json:"gateway_data,omitempty"`
	ExpiresAt      time.Time              `json:"expires_at"`
	CreatedAt      time.Time              `json:"created_at"`
}

func (cs *CheckoutSession) ToResponse() CheckoutSessionResponse {
	if cs == nil {
		return CheckoutSessionResponse{}
	}
	return CheckoutSessionResponse{
		ID:             cs.ID,
		CheckoutToken:  cs.CheckoutToken,
		PaymentGateway: cs.PaymentGateway,
		Amount:         cs.Amount,
		Currency:       cs.Currency,
		Status:         cs.Status,
		GatewayData:    cs.GatewayData,
		ExpiresAt:      cs.ExpiresAt,
		CreatedAt:      cs.CreatedAt,
	}
}

// PaymentCallbackRequest represents the callback request from payment gateways
type PaymentCallbackRequest struct {
	CheckoutToken string                 `json:"checkout_token" binding:"required"`
	GatewayData   map[string]interface{} `json:"gateway_data"` // Gateway-specific callback data
}

// AdminProcessCheckoutSessionRequest represents the admin request payload
// to manually process a checkout session.
type AdminProcessCheckoutSessionRequest struct {
	CheckoutToken string `json:"checkout_token" binding:"required"`
}
