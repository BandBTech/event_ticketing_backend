package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Event struct {
	ID             uuid.UUID  `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	Title          string     `gorm:"not null;size:200" json:"title" binding:"required"`
	Description    string     `gorm:"type:text" json:"description"` // HTML content
	BannerImage    string     `gorm:"size:500" json:"banner_image"`
	Category       []string   `gorm:"type:text[]" json:"category"` // Array of category tags
	VenueName      string     `gorm:"size:200" json:"venue_name"`
	Address        string     `gorm:"type:text" json:"address"`
	Location       string     `gorm:"size:200" json:"location"` // Keep for backward compatibility
	StartDate      time.Time  `gorm:"not null" json:"start_date" binding:"required"`
	EndDate        time.Time  `gorm:"not null" json:"end_date" binding:"required"`
	Timezone       string     `gorm:"size:50;default:'UTC'" json:"timezone"`
	Capacity       int        `gorm:"not null" json:"capacity" binding:"required,min=1"`
	Available      int        `gorm:"not null" json:"available"`
	Price          float64    `gorm:"not null" json:"price" binding:"required,min=0"` // Base price for backward compatibility
	CommissionRate float64    `gorm:"not null;default:10" json:"commission_rate"`     // Platform commission percentage (0-100)
	Status         string     `gorm:"not null;default:'draft'" json:"status"`         // draft, pending, approved, held, rejected, cancelled
	SalesStatus    string     `gorm:"not null;default:'active'" json:"sales_status"`  // active, paused, stopped
	IsFeatured     bool       `gorm:"not null;default:false" json:"is_featured"`      // Featured event flag
	IsCancelled    bool       `gorm:"not null;default:false" json:"is_cancelled"`
	CancelledAt    *time.Time `json:"cancelled_at,omitempty"`
	CancelReason   string     `gorm:"type:text" json:"cancel_reason,omitempty"`
	OrganizerID    uuid.UUID  `gorm:"type:uuid;index" json:"organizer_id"`
	Organizer      *User      `gorm:"foreignKey:OrganizerID" json:"organizer,omitempty"`
	AdminRemark    string     `gorm:"type:text" json:"admin_remark"`

	// Relations
	Tiers      []EventTier `gorm:"foreignKey:EventID;constraint:OnDelete:CASCADE" json:"tiers,omitempty"`
	Discounts  []Discount  `gorm:"foreignKey:EventID;constraint:OnDelete:CASCADE" json:"discounts,omitempty"`
	Promocodes []Promocode `gorm:"foreignKey:EventID;constraint:OnDelete:CASCADE" json:"promocodes,omitempty"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

type EventCreateRequest struct {
	Title          string                   `json:"title" binding:"required,min=3,max=200"`
	Description    string                   `json:"description" binding:"max=10000"`
	BannerImage    string                   `json:"banner_image" binding:"omitempty,url"`
	Category       []string                 `json:"category" binding:"required,min=1"`
	VenueName      string                   `json:"venue_name" binding:"required,min=3,max=200"`
	Address        string                   `json:"address" binding:"required,min=10,max=500"`
	StartDate      time.Time                `json:"start_date" binding:"required"`
	EndDate        time.Time                `json:"end_date" binding:"required,gtfield=StartDate"`
	Timezone       string                   `json:"timezone" binding:"omitempty"`
	Capacity       int                      `json:"capacity" binding:"required,min=1,max=100000"`
	Price          float64                  `json:"price" binding:"required,min=0,max=100000"`
	CommissionRate float64                  `json:"commission_rate" binding:"omitempty,min=0,max=50"` // Optional, only for admin
	Tiers          []CreateEventTierRequest `json:"tiers" binding:"omitempty,dive"`
}

type EventUpdateRequest struct {
	Title          string                   `json:"title" binding:"omitempty,min=3,max=200"`
	Description    string                   `json:"description" binding:"max=10000"`
	BannerImage    string                   `json:"banner_image" binding:"omitempty,url"`
	Category       []string                 `json:"category" binding:"omitempty,min=1"`
	VenueName      string                   `json:"venue_name" binding:"omitempty,min=3,max=200"`
	Address        string                   `json:"address" binding:"omitempty,min=10,max=500"`
	StartDate      time.Time                `json:"start_date"`
	EndDate        time.Time                `json:"end_date"`
	Timezone       string                   `json:"timezone"`
	Capacity       int                      `json:"capacity" binding:"omitempty,min=1,max=100000"`
	Price          float64                  `json:"price" binding:"omitempty,min=0,max=100000"`
	CommissionRate float64                  `json:"commission_rate" binding:"omitempty,min=0,max=50"` // Only admin can update
	Status         string                   `json:"status" binding:"omitempty,oneof=draft pending approved held rejected"`
	Tiers          []CreateEventTierRequest `json:"tiers" binding:"omitempty,dive"`
}

type EventApprovalRequest struct {
	Status         string  `json:"status" binding:"required,oneof=approved held rejected"`
	CommissionRate float64 `json:"commission_rate" binding:"omitempty,min=0,max=50"` // Admin sets commission during approval
	AdminRemark    string  `json:"admin_remark,omitempty"`
}

func (e *Event) BeforeCreate(tx *gorm.DB) error {
	e.Available = e.Capacity
	if e.Status == "" {
		e.Status = "pending" // Events start as pending until approved by admin/subadmin
	}
	return nil
}
