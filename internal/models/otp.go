package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OTP represents an OTP record for database fallback storage
type OTP struct {
	ID         uuid.UUID `json:"id" gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	Identifier string    `json:"identifier" gorm:"not null;index:idx_otp_identifier_type_role"`
	Type       string    `json:"type" gorm:"not null;index:idx_otp_identifier_type_role"`
	Role       string    `json:"role" gorm:"not null;index:idx_otp_identifier_type_role;default:'user'"`
	Code       string    `json:"code" gorm:"not null"`
	ExpiresAt  time.Time `json:"expires_at" gorm:"not null;index"`
	Attempts   int       `json:"attempts" gorm:"default:0"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// TableName specifies the table name for OTP model
func (OTP) TableName() string {
	return "otps"
}

// BeforeCreate hook to set UUID
func (o *OTP) BeforeCreate(tx *gorm.DB) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	return nil
}

// OTPVerifyRequest is the request structure for verifying an OTP
type OTPVerifyRequest struct {
	Identifier string `json:"identifier" binding:"required" example:"user@example.com"` // Email, phone, or user ID
	OTPCode    string `json:"otp_code" binding:"required" example:"123456"`             // The OTP code
	OTPType    string `json:"otp_type" binding:"required" example:"registration"`       // The purpose of OTP
}

// OTPSendRequest is the request structure for sending an OTP
type OTPSendRequest struct {
	Identifier string `json:"identifier" binding:"required" example:"user@example.com"` // Email, phone, or user ID
	OTPType    string `json:"otp_type" binding:"required" example:"registration"`       // The purpose of OTP
}

// OTPJob represents an OTP job for queue processing
type OTPJob struct {
	ID           string    `json:"id"`
	Identifier   string    `json:"identifier"`
	OTP          string    `json:"otp"`
	Type         string    `json:"type"`
	Role         string    `json:"role"`
	Attempts     int       `json:"attempts"`
	MaxRetries   int       `json:"max_retries"`
	ProcessAfter time.Time `json:"process_after,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}
