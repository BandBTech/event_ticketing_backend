package models

import (
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// User represents a system user
type User struct {
	ID                  uuid.UUID            `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Email               string               `gorm:"unique;not null" json:"email"`
	PasswordHash        string               `gorm:"not null" json:"-"`
	FirstName           string               `json:"first_name"`
	LastName            string               `json:"last_name"`
	Phone               string               `json:"phone"`
	CountryCode         string               `json:"country_code"`
	IsEmailVerified     bool                 `gorm:"default:false" json:"is_email_verified"`
	VerificationCode    string               `gorm:"default:null" json:"-"`
	OrganizerStatus     string               `gorm:"default:'inactive'" json:"organizer_status"` // inactive, pending, approved, rejected
	AccountStatus       string               `gorm:"default:'active'" json:"account_status"`     // active, inactive, suspended
	AdminRemark         string               `gorm:"type:text" json:"admin_remark"`
	ApprovedAt          *time.Time           `gorm:"default:null" json:"approved_at"`
	RejectedAt          *time.Time           `gorm:"default:null" json:"rejected_at"`
	OrganizerID         *uuid.UUID           `gorm:"type:uuid;index" json:"organizer_id"`
	CreatedBy           *uuid.UUID           `gorm:"type:uuid" json:"created_by"`
	Roles               []*Role              `gorm:"many2many:user_roles;" json:"roles"`
	OrganizerOnboarding *OrganizerOnboarding `gorm:"foreignKey:OrganizerID;references:ID" json:"organizer_onboarding,omitempty"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
	DeletedAt           *time.Time           `gorm:"index" json:"-"`
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
	Status      string `json:"status" binding:"required,oneof=inactive pending approved rejected"`
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

// OrganizerListItemResponse represents a simplified organizer response for admin listing
type OrganizerListItemResponse struct {
	ID              uuid.UUID `json:"id"`
	Email           string    `json:"email"`
	Name            string    `json:"name"` // Business name or first_name + last_name
	Logo            string    `json:"logo"`
	Phone           string    `json:"phone"`
	CountryCode     string    `json:"country_code"`
	IsEmailVerified bool      `json:"is_email_verified"`
	OrganizerStatus string    `json:"organizer_status"`
	AccountStatus   string    `json:"account_status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// OrganizerSimpleResponse represents a simplified organizer response for single organizer view
type OrganizerSimpleResponse struct {
	ID                   uuid.UUID `json:"id"`
	Email                string    `json:"email"`
	Name                 string    `json:"name"` // Business name or first_name + last_name
	Logo                 string    `json:"logo"`
	Description          string    `json:"description"`
	Phone                string    `json:"phone"`
	CountryCode          string    `json:"country_code"`
	IsEmailVerified      bool      `json:"is_email_verified"`
	OrganizerStatus      string    `json:"organizer_status"`
	AccountStatus        string    `json:"account_status"`
	IsOnboardingComplete bool      `json:"is_onboarding_complete"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// OrganizerSimpleListResponse represents the response structure for simplified organizer lists
type OrganizerSimpleListResponse struct {
	Organizers []OrganizerListItemResponse `json:"organizers"`
	Pagination map[string]interface{}      `json:"pagination"`
}

// UserResponse is the response structure for user data
type UserResponse struct {
	ID              uuid.UUID      `json:"id"`
	Email           string         `json:"email"`
	FirstName       string         `json:"first_name"`
	LastName        string         `json:"last_name"`
	Phone           string         `json:"phone"`
	CountryCode     string         `json:"country_code"`
	IsEmailVerified bool           `json:"is_email_verified"`
	OrganizerStatus string         `json:"organizer_status,omitempty"`
	AccountStatus   string         `json:"account_status"`
	AdminRemark     string         `json:"admin_remark,omitempty"`
	OrganizerID     *uuid.UUID     `json:"organizer_id,omitempty"`
	CreatedBy       *uuid.UUID     `json:"created_by,omitempty"`
	Roles           []RoleResponse `json:"roles"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// OrganizerInfoResponse represents organizer info for staff/manager users
type OrganizerInfoResponse struct {
	ID              uuid.UUID  `json:"id"`
	BusinessName    string     `json:"business_name"`
	BusinessLogoURL string     `json:"business_logo_url"`
	Status          string     `json:"status"`
	Remark          string     `json:"remark"`
	ApprovedAt      *time.Time `json:"approved_at"`
	RejectedAt      *time.Time `json:"rejected_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// UserProfileResponse is the response structure for user profile data
type UserProfileResponse struct {
	ID              uuid.UUID              `json:"id"`
	Email           string                 `json:"email"`
	FirstName       string                 `json:"first_name"`
	LastName        string                 `json:"last_name"`
	Phone           string                 `json:"phone"`
	CountryCode     string                 `json:"country_code"`
	IsEmailVerified bool                   `json:"is_email_verified"`
	OrganizerStatus string                 `json:"organizer_status"`         // inactive, pending, approved, rejected
	AccountStatus   string                 `json:"account_status"`           // active, inactive, suspended
	OrganizerID     *uuid.UUID             `json:"organizer_id"`             // Always show organizer_id field
	OrganizerInfo   *OrganizerInfoResponse `json:"organizer_info,omitempty"` // For staff/manager/organizer: full organizer details
	Roles           []string               `json:"roles"`
	Permissions     []string               `json:"permissions"` // Array of permission names for UI adjustments
	CreatedBy       *uuid.UUID             `json:"created_by,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

// OrganizerDetailResponse represents comprehensive organizer information for admin
type OrganizerDetailResponse struct {
	// User information
	ID              uuid.UUID      `json:"id"`
	Email           string         `json:"email"`
	FirstName       string         `json:"first_name"`
	LastName        string         `json:"last_name"`
	Phone           string         `json:"phone"`
	CountryCode     string         `json:"country_code"`
	IsEmailVerified bool           `json:"is_email_verified"`
	OrganizerStatus string         `json:"organizer_status"`
	AccountStatus   string         `json:"account_status"`
	AdminRemark     string         `json:"admin_remark,omitempty"`
	ApprovedAt      *time.Time     `json:"approved_at,omitempty"`
	RejectedAt      *time.Time     `json:"rejected_at,omitempty"`
	Roles           []RoleResponse `json:"roles"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`

	// Business/Onboarding information
	Onboarding *OrganizerOnboardingResponse `json:"onboarding,omitempty"`
}

// OrganizerOnboardingResponse represents organizer onboarding/business information
type OrganizerOnboardingResponse struct {
	ID                  uuid.UUID `json:"id"`
	IsComplete          bool      `json:"is_complete"`
	BusinessName        string    `json:"business_name"`
	BusinessDescription string    `json:"business_description"`
	BusinessLogoURL     string    `json:"business_logo_url"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
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
		OrganizerID:     u.OrganizerID,
		CreatedBy:       u.CreatedBy,
		Roles:           roleResponses,
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

// ToOrganizerDetailResponse converts a User model to a comprehensive OrganizerDetailResponse
func (u *User) ToOrganizerDetailResponse() OrganizerDetailResponse {
	roleResponses := make([]RoleResponse, len(u.Roles))
	for i, role := range u.Roles {
		roleResponses[i] = role.ToResponse()
	}

	var onboardingResponse *OrganizerOnboardingResponse
	if u.OrganizerOnboarding != nil {
		onboardingResponse = &OrganizerOnboardingResponse{
			ID:                  u.OrganizerOnboarding.ID,
			IsComplete:          u.OrganizerOnboarding.IsComplete,
			BusinessName:        u.OrganizerOnboarding.BusinessName,
			BusinessDescription: u.OrganizerOnboarding.BusinessDescription,
			BusinessLogoURL:     u.OrganizerOnboarding.BusinessLogoURL,
			CreatedAt:           u.OrganizerOnboarding.CreatedAt,
			UpdatedAt:           u.OrganizerOnboarding.UpdatedAt,
		}
	}

	return OrganizerDetailResponse{
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
		ApprovedAt:      u.ApprovedAt,
		RejectedAt:      u.RejectedAt,
		Roles:           roleResponses,
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
		Onboarding:      onboardingResponse,
	}
}

// ToOrganizerListItemResponse converts a User model to a simplified OrganizerListItemResponse
func (u *User) ToOrganizerListItemResponse() OrganizerListItemResponse {
	name := u.FirstName + " " + u.LastName
	logo := ""

	// Use business name if onboarding exists and has business name
	if u.OrganizerOnboarding != nil && u.OrganizerOnboarding.BusinessName != "" {
		name = u.OrganizerOnboarding.BusinessName
		logo = u.OrganizerOnboarding.BusinessLogoURL
	}

	return OrganizerListItemResponse{
		ID:              u.ID,
		Email:           u.Email,
		Name:            name,
		Logo:            logo,
		Phone:           u.Phone,
		CountryCode:     u.CountryCode,
		IsEmailVerified: u.IsEmailVerified,
		OrganizerStatus: u.OrganizerStatus,
		AccountStatus:   u.AccountStatus,
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

// ToOrganizerSimpleResponse converts a User model to a simplified OrganizerSimpleResponse
func (u *User) ToOrganizerSimpleResponse() OrganizerSimpleResponse {
	name := u.FirstName + " " + u.LastName
	logo := ""
	businessDescription := ""
	isOnboardingComplete := false

	// Check if onboarding exists and determine completion status based on business name and logo
	if u.OrganizerOnboarding != nil {
		businessDescription = u.OrganizerOnboarding.BusinessDescription
		// Consider onboarding complete if business name and logo are both provided
		if u.OrganizerOnboarding.BusinessName != "" && u.OrganizerOnboarding.BusinessLogoURL != "" {
			isOnboardingComplete = true
		}
	}

	// Use business name and logo if onboarding exists and has business name
	if u.OrganizerOnboarding != nil && u.OrganizerOnboarding.BusinessName != "" {
		name = u.OrganizerOnboarding.BusinessName
		logo = u.OrganizerOnboarding.BusinessLogoURL
	}

	return OrganizerSimpleResponse{
		ID:                   u.ID,
		Email:                u.Email,
		Name:                 name,
		Logo:                 logo,
		Description:          businessDescription,
		Phone:                u.Phone,
		CountryCode:          u.CountryCode,
		IsEmailVerified:      u.IsEmailVerified,
		OrganizerStatus:      u.OrganizerStatus,
		AccountStatus:        u.AccountStatus,
		IsOnboardingComplete: isOnboardingComplete,
		CreatedAt:            u.CreatedAt,
		UpdatedAt:            u.UpdatedAt,
	}
}

// ToProfileResponse converts a User model to a UserProfileResponse
func (u *User) ToProfileResponse(permissions []string, orgInfo *OrganizerInfoResponse) UserProfileResponse {
	roleNames := make([]string, len(u.Roles))
	for i, role := range u.Roles {
		roleNames[i] = role.Name
	}

	return UserProfileResponse{
		ID:              u.ID,
		Email:           u.Email,
		FirstName:       u.FirstName,
		LastName:        u.LastName,
		Phone:           u.Phone,
		CountryCode:     u.CountryCode,
		IsEmailVerified: u.IsEmailVerified,
		OrganizerStatus: u.OrganizerStatus,
		AccountStatus:   u.AccountStatus,
		OrganizerID:     u.OrganizerID,
		OrganizerInfo:   orgInfo,
		Roles:           roleNames,
		Permissions:     permissions,
		CreatedBy:       u.CreatedBy,
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

// ToOrganizerPublicResponse converts User to OrganizerPublicResponse for public event details
func (u *User) ToOrganizerPublicResponse() *OrganizerPublicResponse {
	if u == nil {
		return nil
	}

	response := &OrganizerPublicResponse{
		ID: u.ID,
	}

	// Add business information if onboarding exists
	if u.OrganizerOnboarding != nil {
		response.BusinessName = u.OrganizerOnboarding.BusinessName
		response.BusinessLogoURL = u.OrganizerOnboarding.BusinessLogoURL
	}

	return response
}

// Query Scopes - Reusable database query patterns for User

// WithRoles preloads user roles
func WithRoles(db *gorm.DB) *gorm.DB {
	return db.Preload("Roles")
}

// WithRolesAndPermissions preloads user roles with their permissions
func WithRolesAndPermissions(db *gorm.DB) *gorm.DB {
	return db.Preload("Roles.Permissions")
}

// WithOrganizerOnboarding preloads organizer onboarding information
func WithOrganizerOnboarding(db *gorm.DB) *gorm.DB {
	return db.Preload("OrganizerOnboarding")
}

// ActiveUsersOnly filters only active (non-deleted) users
func ActiveUsersOnly(db *gorm.DB) *gorm.DB {
	return db.Where("deleted_at IS NULL")
}
