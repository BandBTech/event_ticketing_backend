package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CompanyInfo represents the website/company information
type CompanyInfo struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	Name        string    `gorm:"not null;size:200" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	LogoURL     string    `gorm:"size:500" json:"logo_url"`
	Email       string    `gorm:"size:200" json:"email"`
	Phone       string    `gorm:"size:50" json:"phone"`
	Address     string    `gorm:"type:text" json:"address"`
	WebsiteURL  string    `gorm:"size:500" json:"website_url"`

	// Social Links
	FacebookURL  string `gorm:"size:500" json:"facebook_url"`
	TwitterURL   string `gorm:"size:500" json:"twitter_url"`
	InstagramURL string `gorm:"size:500" json:"instagram_url"`
	LinkedInURL  string `gorm:"size:500" json:"linkedin_url"`
	YouTubeURL   string `gorm:"size:500" json:"youtube_url"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"-"`
}

// Category represents event categories
type Category struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	Name        string    `gorm:"not null;size:100;unique" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	IconURL     string    `gorm:"size:500" json:"icon_url"`
	IsActive    bool      `gorm:"not null;default:true" json:"is_active"`
	SortOrder   int       `gorm:"not null;default:0" json:"sort_order"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"-"`
}

// OrganizerOnboarding represents the onboarding status and data for organizers
type OrganizerOnboarding struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	OrganizerID uuid.UUID `gorm:"type:uuid;unique;index" json:"organizer_id"`
	Organizer   *User     `gorm:"foreignKey:OrganizerID" json:"organizer,omitempty"`

	// Onboarding Status
	IsComplete bool `gorm:"not null;default:false" json:"is_complete"`

	// Business Information
	BusinessName        string `gorm:"size:200" json:"business_name"`
	BusinessDescription string `gorm:"type:text" json:"business_description"`
	BusinessLogoURL     string `gorm:"size:500" json:"business_logo_url"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"-"`
}

// BeforeCreate hooks
func (c *CompanyInfo) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

func (c *Category) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

func (o *OrganizerOnboarding) BeforeCreate(tx *gorm.DB) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	return nil
}

// Request/Response structs for CompanyInfo
type UpdateCompanyInfoRequest struct {
	Name        string `json:"name" binding:"required,min=2,max=200"`
	Description string `json:"description" binding:"max=2000"`
	LogoURL     string `json:"logo_url" binding:"omitempty,url"`
	Email       string `json:"email" binding:"required,email"`
	Phone       string `json:"phone" binding:"omitempty,min=7,max=20"`
	Address     string `json:"address" binding:"max=500"`
	WebsiteURL  string `json:"website_url" binding:"omitempty,url"`

	FacebookURL  string `json:"facebook_url" binding:"omitempty,url"`
	TwitterURL   string `json:"twitter_url" binding:"omitempty,url"`
	InstagramURL string `json:"instagram_url" binding:"omitempty,url"`
	LinkedInURL  string `json:"linkedin_url" binding:"omitempty,url"`
	YouTubeURL   string `json:"youtube_url" binding:"omitempty,url"`
}

// Request/Response structs for Category
type CreateCategoryRequest struct {
	Name        string `json:"name" binding:"required,min=2,max=100"`
	Description string `json:"description" binding:"max=500"`
	IconURL     string `json:"icon_url" binding:"omitempty,url"`
	IsActive    bool   `json:"is_active"`
	SortOrder   int    `json:"sort_order"`
}

type UpdateCategoryRequest struct {
	Name        string `json:"name" binding:"omitempty,min=2,max=100"`
	Description string `json:"description" binding:"max=500"`
	IconURL     string `json:"icon_url" binding:"omitempty,url"`
	IsActive    *bool  `json:"is_active"`
	SortOrder   int    `json:"sort_order"`
}

// Request/Response structs for OrganizerOnboarding
type UpdateOrganizerProfileRequest struct {
	BusinessName        string `json:"business_name" binding:"required,min=2,max=200"`
	BusinessDescription string `json:"business_description" binding:"max=1000"`
	BusinessLogoURL     string `json:"business_logo_url" binding:"omitempty,url"`
}

type OrganizerOnboardingStatusResponse struct {
	IsComplete bool `json:"is_complete"`
}

// Response structs
type CompanyInfoResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	LogoURL     string    `json:"logo_url"`
	Email       string    `json:"email"`
	Phone       string    `json:"phone"`
	Address     string    `json:"address"`
	WebsiteURL  string    `json:"website_url"`

	FacebookURL  string `json:"facebook_url"`
	TwitterURL   string `json:"twitter_url"`
	InstagramURL string `json:"instagram_url"`
	LinkedInURL  string `json:"linkedin_url"`
	YouTubeURL   string `json:"youtube_url"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CategoryResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IconURL     string    `json:"icon_url"`
	IsActive    bool      `json:"is_active"`
	SortOrder   int       `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ToResponse methods
func (c *CompanyInfo) ToResponse() CompanyInfoResponse {
	return CompanyInfoResponse{
		ID:           c.ID,
		Name:         c.Name,
		Description:  c.Description,
		LogoURL:      c.LogoURL,
		Email:        c.Email,
		Phone:        c.Phone,
		Address:      c.Address,
		WebsiteURL:   c.WebsiteURL,
		FacebookURL:  c.FacebookURL,
		TwitterURL:   c.TwitterURL,
		InstagramURL: c.InstagramURL,
		LinkedInURL:  c.LinkedInURL,
		YouTubeURL:   c.YouTubeURL,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}
}

func (c *Category) ToResponse() CategoryResponse {
	return CategoryResponse{
		ID:          c.ID,
		Name:        c.Name,
		Description: c.Description,
		IconURL:     c.IconURL,
		IsActive:    c.IsActive,
		SortOrder:   c.SortOrder,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}

func (o *OrganizerOnboarding) GetStatusResponse() OrganizerOnboardingStatusResponse {
	// Check if onboarding is complete based on business name, logo, and description
	isComplete := o.BusinessName != "" && o.BusinessLogoURL != "" && o.BusinessDescription != ""

	return OrganizerOnboardingStatusResponse{
		IsComplete: isComplete,
	}
}
