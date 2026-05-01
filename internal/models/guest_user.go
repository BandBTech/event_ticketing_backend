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

// REMOVED: CheckoutSession model completely removed per clean architecture spec
// All checkout session functionality now handled by PaymentIntent + Transaction
// - PaymentIntent: Gateway lifecycle management
// - Transaction: Financial truth and calculations
// - No more CheckoutSession table needed

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
