package models

import (
	"time"

	"gorm.io/gorm"
)

type Event struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	Title       string         `gorm:"not null;size:200" json:"title" binding:"required"`
	Description string         `gorm:"type:text" json:"description"`
	Location    string         `gorm:"size:200" json:"location"`
	StartDate   time.Time      `gorm:"not null" json:"start_date" binding:"required"`
	EndDate     time.Time      `gorm:"not null" json:"end_date" binding:"required"`
	Price       float64        `gorm:"not null" json:"price" binding:"required,min=0"`
	Capacity    int            `gorm:"not null" json:"capacity" binding:"required,min=1"`
	Available   int            `gorm:"not null" json:"available"`
	Status      string         `gorm:"not null;default:'active'" json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

type EventCreateRequest struct {
	Title       string    `json:"title" binding:"required,min=3,max=200"`
	Description string    `json:"description" binding:"max=5000"`
	Location    string    `json:"location" binding:"required,min=3,max=200"`
	StartDate   time.Time `json:"start_date" binding:"required"`
	EndDate     time.Time `json:"end_date" binding:"required,gtfield=StartDate"`
	Price       float64   `json:"price" binding:"required,min=0,max=100000"`
	Capacity    int       `json:"capacity" binding:"required,min=1,max=100000"`
	Category    string    `json:"category" binding:"omitempty,oneof=conference workshop seminar party concert sports other"`
	IsPublic    bool      `json:"is_public" binding:"omitempty"`
	ImageURL    string    `json:"image_url" binding:"omitempty,url"`
	Tags        []string  `json:"tags" binding:"omitempty,dive,min=2,max=50"`
}

type EventUpdateRequest struct {
	Title       string    `json:"title" binding:"omitempty,min=3,max=200"`
	Description string    `json:"description" binding:"omitempty,max=5000"`
	Location    string    `json:"location" binding:"omitempty,min=3,max=200"`
	StartDate   time.Time `json:"start_date" binding:"omitempty"`
	EndDate     time.Time `json:"end_date" binding:"omitempty"`
	Price       float64   `json:"price" binding:"omitempty,min=0,max=100000"`
	Capacity    int       `json:"capacity" binding:"omitempty,min=1,max=100000"`
	Status      string    `json:"status" binding:"omitempty,oneof=active cancelled postponed completed draft"`
	Category    string    `json:"category" binding:"omitempty,oneof=conference workshop seminar party concert sports other"`
	IsPublic    bool      `json:"is_public" binding:"omitempty"`
	ImageURL    string    `json:"image_url" binding:"omitempty,url"`
	Tags        []string  `json:"tags" binding:"omitempty,dive,min=2,max=50"`
}

func (e *Event) BeforeCreate(tx *gorm.DB) error {
	e.Available = e.Capacity
	if e.Status == "" {
		e.Status = "active"
	}
	return nil
}
