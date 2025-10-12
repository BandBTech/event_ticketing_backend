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
	Title       string    `json:"title" binding:"required"`
	Description string    `json:"description"`
	Location    string    `json:"location"`
	StartDate   time.Time `json:"start_date" binding:"required"`
	EndDate     time.Time `json:"end_date" binding:"required"`
	Price       float64   `json:"price" binding:"required,min=0"`
	Capacity    int       `json:"capacity" binding:"required,min=1"`
}

type EventUpdateRequest struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Location    string    `json:"location"`
	StartDate   time.Time `json:"start_date"`
	EndDate     time.Time `json:"end_date"`
	Price       float64   `json:"price" binding:"omitempty,min=0"`
	Capacity    int       `json:"capacity" binding:"omitempty,min=1"`
	Status      string    `json:"status"`
}

func (e *Event) BeforeCreate(tx *gorm.DB) error {
	e.Available = e.Capacity
	if e.Status == "" {
		e.Status = "active"
	}
	return nil
}
