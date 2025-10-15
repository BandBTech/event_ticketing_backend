package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TokenType defines the type of JWT token
type TokenType string

const (
	// AccessToken is a short-lived token used for API access
	AccessToken TokenType = "access"
	// RefreshToken is a long-lived token used to get new access tokens
	RefreshToken TokenType = "refresh"
)

// Token represents a JWT token in the database
type Token struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;index" json:"user_id"`
	TokenHash string    `gorm:"not null" json:"-"` // Hashed token for security
	Type      TokenType `gorm:"not null" json:"type"`
	ExpiresAt time.Time `gorm:"not null" json:"expires_at"`
	Revoked   bool      `gorm:"default:false" json:"revoked"`
	Device    string    `json:"device"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TokenResponse is the response structure for token data
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// BeforeCreate is a GORM hook to set a UUID before creating a record
func (t *Token) BeforeCreate(tx *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return nil
}
