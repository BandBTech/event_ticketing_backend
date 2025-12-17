package models

import (
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// User represents a system user
type User struct {
	ID               uuid.UUID     `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Email            string        `gorm:"unique;not null" json:"email"`
	PasswordHash     string        `gorm:"not null" json:"-"`
	FirstName        string        `json:"first_name"`
	LastName         string        `json:"last_name"`
	Phone            string        `json:"phone"`
	CountryCode      string        `json:"country_code"`
	IsEmailVerified  bool          `gorm:"default:false" json:"is_email_verified"`
	VerificationCode string        `gorm:"default:null" json:"-"`
	OrganizerStatus  string        `gorm:"default:'inactive'" json:"organizer_status"` // inactive, pending, approved, rejected
	AccountStatus    string        `gorm:"default:'active'" json:"account_status"`     // active, inactive, suspended
	AdminRemark      string        `gorm:"type:text" json:"admin_remark"`
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
	UserID uuid.UUID `gorm:"type:uuid;primaryKey" json:"user_id"`
	RoleID uuid.UUID `gorm:"type:uuid;primaryKey" json:"role_id"`
}

// CreateUserRequest is the request structure for creating a new user
type CreateUserRequest struct {
	Email       string `json:"email" binding:"required,email" example:"user@example.com"`
	FirstName   string `json:"first_name" binding:"required,min=2,max=50" example:"John"`
	LastName    string `json:"last_name" binding:"required,min=2,max=50" example:"Doe"`
	Phone       string `json:"phone" binding:"omitempty" example:"8765432109"`
	CountryCode string `json:"country_code" binding:"omitempty" example:"+1"`
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
	EmailToken      string `json:"email_token" binding:"omitempty,email" example:"user@example.com"` // Email for OTP-based flow
	OTP             string `json:"otp" binding:"required" example:"123456"`
	NewPassword     string `json:"new_password" binding:"required" example:"NewPassword123!"`
	ConfirmPassword string `json:"confirm_password" binding:"required,eqfield=NewPassword" example:"NewPassword123!"`
}

// SetPasswordRequest is the request structure for setting password after registration
type SetPasswordRequest struct {
	Email    string `json:"email" binding:"required,email" example:"user@example.com"`
	Password string `json:"password" binding:"required,min=8" example:"Password123!"`
}

// UpdateProfileRequest is the request structure for updating user profile
type UpdateProfileRequest struct {
	FirstName   string `json:"first_name" binding:"required,min=2,max=50" example:"John"`
	LastName    string `json:"last_name" binding:"required,min=2,max=50" example:"Doe"`
	Phone       string `json:"phone" binding:"omitempty" example:"8765432109"`
	CountryCode string `json:"country_code" binding:"omitempty" example:"+1"`
}

// ChangePasswordRequest is the request structure for changing password (authenticated user)
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required" example:"CurrentPassword123!"`
	NewPassword     string `json:"new_password" binding:"required,min=8" example:"NewPassword123!"`
	ConfirmPassword string `json:"confirm_password" binding:"required,eqfield=NewPassword" example:"NewPassword123!"`
}

// VerifyEmailRequest is the request structure for verifying an email
type VerifyEmailRequest struct {
	VerificationCode string `json:"verification_code" binding:"required" example:"abc123def456"`
}

// OrganizerRegistrationRequest is the request structure for organizer registration
type OrganizerRegistrationRequest struct {
	Email       string `json:"email" binding:"required,email" example:"organizer@example.com"`
	FirstName   string `json:"first_name" binding:"required,min=2,max=50" example:"John"`
	LastName    string `json:"last_name" binding:"required,min=2,max=50" example:"Doe"`
	Phone       string `json:"phone" binding:"omitempty" example:"8765432109"`
	CountryCode string `json:"country_code" binding:"omitempty" example:"+1"`
}

// OrganizerApprovalRequest is the request structure for approving/rejecting organizers
type OrganizerApprovalRequest struct {
	Status      string `json:"status" binding:"required,oneof=approved rejected"`
	AdminRemark string `json:"admin_remark,omitempty"`
}

// AdminCreateOrganizerRequest is the request structure for admin creating organizers
type AdminCreateOrganizerRequest struct {
	Email       string `json:"email" binding:"required,email" example:"organizer@example.com"`
	FirstName   string `json:"first_name" binding:"required,min=2,max=50" example:"John"`
	LastName    string `json:"last_name" binding:"required,min=2,max=50" example:"Doe"`
	Phone       string `json:"phone" binding:"omitempty" example:"8765432109"`
	CountryCode string `json:"country_code" binding:"omitempty" example:"+1"`
	Password    string `json:"password" binding:"required,min=8" example:"SecurePass123!"`
}

// UserSearchRequest is the request structure for searching users
type UserSearchRequest struct {
	Search    string `json:"search" form:"search"`         // Search by name or email
	Status    string `json:"status" form:"status"`         // Filter by account status
	Role      string `json:"role" form:"role"`             // Filter by role
	OrgStatus string `json:"org_status" form:"org_status"` // Filter by organizer status
	Page      int    `json:"page" form:"page,default=1"`
	Limit     int    `json:"limit" form:"limit,default=10"`
	Sort      string `json:"sort" form:"sort,default=-created_at"` // Sort field with optional `-` prefix for desc (e.g., "-created_at", "email")
}

// PromoteUserRequest is the request structure for promoting user roles
type PromoteUserRequest struct {
	Role string `json:"role" binding:"required,oneof=user organizer subadmin"`
}

// UpdateAccountStatusRequest is the request structure for updating account status
type UpdateAccountStatusRequest struct {
	Status      string `json:"status" binding:"required,oneof=active inactive suspended"`
	AdminRemark string `json:"admin_remark,omitempty"`
}

// BulkUserActionRequest is the request structure for bulk user actions
type BulkUserActionRequest struct {
	UserIDs []string `json:"user_ids" binding:"required,min=1"`
	Action  string   `json:"action" binding:"required,oneof=activate deactivate suspend soft_delete hard_delete promote"`
	Role    string   `json:"role" binding:"omitempty"` // Required for promote action
	Reason  string   `json:"reason,omitempty"`         // Optional reason for action
}

// DeleteUserRequest is the request structure for deleting a user
type DeleteUserRequest struct {
	DeleteType string `json:"delete_type" binding:"required,oneof=soft hard" example:"soft"` // Type of deletion: soft or hard
	Reason     string `json:"reason,omitempty" example:"User requested account deletion"`    // Optional reason for deletion
}

// UserResponse is the response structure for user data
type UserResponse struct {
	ID              uuid.UUID             `json:"id"`
	Email           string                `json:"email"`
	FirstName       string                `json:"first_name"`
	LastName        string                `json:"last_name"`
	Phone           string                `json:"phone"`
	CountryCode     string                `json:"country_code"`
	IsEmailVerified bool                  `json:"is_email_verified"`
	OrganizerStatus string                `json:"organizer_status,omitempty"`
	AccountStatus   string                `json:"account_status"`
	AdminRemark     string                `json:"admin_remark,omitempty"`
	OrganizationID  *uuid.UUID            `json:"organization_id,omitempty"`
	Organization    *OrganizationResponse `json:"organization,omitempty"`
	CreatedBy       *uuid.UUID            `json:"created_by,omitempty"`
	Roles           []RoleResponse        `json:"roles"`
	CreatedAt       time.Time             `json:"created_at"`
	UpdatedAt       time.Time             `json:"updated_at"`
}

// UserProfileResponse is the response structure for user profile data
type UserProfileResponse struct {
	ID              uuid.UUID             `json:"id"`
	Email           string                `json:"email"`
	FirstName       string                `json:"first_name"`
	LastName        string                `json:"last_name"`
	Phone           string                `json:"phone"`
	CountryCode     string                `json:"country_code"`
	IsEmailVerified bool                  `json:"is_email_verified"`
	OrganizationID  *uuid.UUID            `json:"organization_id,omitempty"`
	Organization    *OrganizationResponse `json:"organization,omitempty"`
	Roles           []RoleResponse        `json:"roles"`
	Permissions     []string              `json:"permissions"` // Array of permission names for UI adjustments
	CreatedBy       *uuid.UUID            `json:"created_by,omitempty"`
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
		Phone:           u.Phone,
		CountryCode:     u.CountryCode,
		IsEmailVerified: u.IsEmailVerified,
		OrganizerStatus: u.OrganizerStatus,
		AccountStatus:   u.AccountStatus,
		AdminRemark:     u.AdminRemark,
		OrganizationID:  u.OrganizationID,
		Organization:    orgResponse,
		CreatedBy:       u.CreatedBy,
		Roles:           roleResponses,
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

// ToProfileResponse converts a User model to a UserProfileResponse
func (u *User) ToProfileResponse(permissions []string) UserProfileResponse {
	roleResponses := make([]RoleResponse, len(u.Roles))
	for i, role := range u.Roles {
		roleResponses[i] = role.ToResponse()
	}

	var orgResponse *OrganizationResponse
	if u.Organization != nil {
		resp := u.Organization.ToResponse()
		orgResponse = &resp
	}

	return UserProfileResponse{
		ID:              u.ID,
		Email:           u.Email,
		FirstName:       u.FirstName,
		LastName:        u.LastName,
		Phone:           u.Phone,
		CountryCode:     u.CountryCode,
		IsEmailVerified: u.IsEmailVerified,
		OrganizationID:  u.OrganizationID,
		Organization:    orgResponse,
		Roles:           roleResponses,
		Permissions:     permissions,
		CreatedBy:       u.CreatedBy,
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}
