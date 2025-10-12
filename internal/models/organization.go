package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Organization represents a group/company that organizes events
type Organization struct {
	ID          uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	Name        string     `gorm:"not null" json:"name"`
	Description string     `json:"description"`
	LogoURL     string     `json:"logo_url"`
	WebsiteURL  string     `json:"website_url"`
	OrganizerID uuid.UUID  `gorm:"type:uuid" json:"organizer_id"`
	Organizer   *User      `gorm:"foreignKey:OrganizerID" json:"organizer,omitempty"`
	Members     []*User    `gorm:"foreignKey:OrganizationID" json:"members,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `gorm:"index" json:"-"`
}

// CreateOrganizationRequest is the request structure for creating a new organization
type CreateOrganizationRequest struct {
	Name        string `json:"name" binding:"required,min=3,max=100" example:"Acme Events"`
	Description string `json:"description" binding:"omitempty,max=1000" example:"Event management company for corporate events"`
	WebsiteURL  string `json:"website_url" binding:"omitempty,url" example:"https://acme-events.com"`
	LogoURL     string `json:"logo_url" binding:"omitempty,url" example:"https://acme-events.com/logo.png"`
}

// OrganizationResponse is the response structure for organization data
type OrganizationResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	LogoURL     string    `json:"logo_url"`
	WebsiteURL  string    `json:"website_url"`
	OrganizerID uuid.UUID `json:"organizer_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// BeforeCreate is a GORM hook to set a UUID before creating a record
func (o *Organization) BeforeCreate(tx *gorm.DB) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	return nil
}

// ToResponse converts an Organization model to an OrganizationResponse
func (o *Organization) ToResponse() OrganizationResponse {
	return OrganizationResponse{
		ID:          o.ID,
		Name:        o.Name,
		Description: o.Description,
		LogoURL:     o.LogoURL,
		WebsiteURL:  o.WebsiteURL,
		OrganizerID: o.OrganizerID,
		CreatedAt:   o.CreatedAt,
		UpdatedAt:   o.UpdatedAt,
	}
}
