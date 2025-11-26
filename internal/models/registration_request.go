package models

import (
	"time"

	"github.com/google/uuid"
)

type RegistrationRequest struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Email       string    `gorm:"uniqueIndex;not null"`
	FirstName   string    `gorm:"not null"`
	LastName    string    `gorm:"not null"`
	Phone       string    `gorm:"not null"`
	CountryCode string    `gorm:"not null"`
	Password    string    `gorm:"not null"` // Plain text temporarily
	UserType    string    `gorm:"not null"` // "user" or "organizer"
	IsVerified  bool      `gorm:"default:false"`
	ExpiresAt   time.Time `gorm:"not null" json:"expires_at"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TableName specifies the table name for RegistrationRequest
func (RegistrationRequest) TableName() string {
	return "registration_requests"
}
