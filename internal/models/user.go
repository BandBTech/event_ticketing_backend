package models

import (
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// User represents a system user
type User struct {
	ID               uuid.UUID     `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	Email            string        `gorm:"unique;not null" json:"email"`
	PasswordHash     string        `gorm:"not null" json:"-"`
	FirstName        string        `json:"first_name"`
	LastName         string        `json:"last_name"`
	IsEmailVerified  bool          `gorm:"default:false" json:"is_email_verified"`
	VerificationCode string        `gorm:"default:null" json:"-"`
	OrganizationID   *uuid.UUID    `gorm:"type:uuid;index" json:"organization_id"`
	Organization     *Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
	CreatedBy        *uuid.UUID    `gorm:"type:uuid" json:"created_by"`
	Roles            []*Role       `gorm:"many2many:user_roles;" json:"roles"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
	DeletedAt        *time.Time    `gorm:"index" json:"-"`
}

// UserRole represents the many-to-many relationship between users and roles
type UserRole struct {
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey" json:"user_id"`
	RoleID    uuid.UUID `gorm:"type:uuid;primaryKey" json:"role_id"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateUserRequest is the request structure for creating a new user
type CreateUserRequest struct {
	Email     string `json:"email" binding:"required,email" example:"user@example.com"`
	Password  string `json:"password" binding:"required" example:"Password123!"`
	FirstName string `json:"first_name" binding:"required,min=2,max=50" example:"John"`
	LastName  string `json:"last_name" binding:"required,min=2,max=50" example:"Doe"`
	Phone     string `json:"phone" binding:"omitempty" example:"+12345678901"`
}

// LoginRequest is the request structure for user login
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email" example:"user@example.com"`
	Password string `json:"password" binding:"required" example:"Password123!"`
}

// RefreshTokenRequest is the request structure for refreshing an access token
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
}

// ResetPasswordRequest is the request structure for resetting a password
type ResetPasswordRequest struct {
	Email string `json:"email" binding:"required,email" example:"user@example.com"`
}

// UpdatePasswordRequest is the request structure for updating a password
type UpdatePasswordRequest struct {
	ResetToken      string `json:"reset_token" binding:"required" example:"abc123def456"`            // Can be a token or OTP
	EmailToken      string `json:"email_token" binding:"omitempty,email" example:"user@example.com"` // Email for OTP-based flow
	NewPassword     string `json:"new_password" binding:"required,strong_password" example:"NewPassword123!"`
	ConfirmPassword string `json:"confirm_password" binding:"required,eqfield=NewPassword" example:"NewPassword123!"`
}

// VerifyEmailRequest is the request structure for verifying an email
type VerifyEmailRequest struct {
	VerificationCode string `json:"verification_code" binding:"required" example:"abc123def456"`
}

// UserResponse is the response structure for user data
type UserResponse struct {
	ID              uuid.UUID             `json:"id"`
	Email           string                `json:"email"`
	FirstName       string                `json:"first_name"`
	LastName        string                `json:"last_name"`
	IsEmailVerified bool                  `json:"is_email_verified"`
	OrganizationID  *uuid.UUID            `json:"organization_id,omitempty"`
	Organization    *OrganizationResponse `json:"organization,omitempty"`
	CreatedBy       *uuid.UUID            `json:"created_by,omitempty"`
	Roles           []RoleResponse        `json:"roles"`
	CreatedAt       time.Time             `json:"created_at"`
	UpdatedAt       time.Time             `json:"updated_at"`
}

// HashPassword creates a password hash from a plain-text password
func (u *User) HashPassword(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	return nil
}

// CheckPassword compares a plain-text password with the user's password hash
func (u *User) CheckPassword(password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password))
	return err == nil
}

// BeforeCreate is a GORM hook to set a UUID before creating a record
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}

// ToResponse converts a User model to a UserResponse
func (u *User) ToResponse() UserResponse {
	roleResponses := make([]RoleResponse, len(u.Roles))
	for i, role := range u.Roles {
		roleResponses[i] = role.ToResponse()
	}

	var orgResponse *OrganizationResponse
	if u.Organization != nil {
		resp := u.Organization.ToResponse()
		orgResponse = &resp
	}

	return UserResponse{
		ID:              u.ID,
		Email:           u.Email,
		FirstName:       u.FirstName,
		LastName:        u.LastName,
		IsEmailVerified: u.IsEmailVerified,
		OrganizationID:  u.OrganizationID,
		Organization:    orgResponse,
		CreatedBy:       u.CreatedBy,
		Roles:           roleResponses,
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}
