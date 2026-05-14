package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	EventCancellationRequestPending  = "pending"
	EventCancellationRequestApproved = "approved"
	EventCancellationRequestRejected = "rejected"
)

type EventCancellationRequest struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`

	EventID uuid.UUID `gorm:"type:uuid;not null;index" json:"event_id"`
	Event   *Event    `gorm:"foreignKey:EventID" json:"event,omitempty"`

	OrganizerID uuid.UUID `gorm:"type:uuid;not null;index" json:"organizer_id"`
	Organizer   *User     `gorm:"foreignKey:OrganizerID" json:"organizer,omitempty"`

	Reason string `gorm:"type:text;not null" json:"reason"`
	Status string `gorm:"type:varchar(20);not null;default:'pending';index" json:"status"`

	AdminRemark string     `gorm:"type:text" json:"admin_remark,omitempty"`
	ReviewedBy  *uuid.UUID `gorm:"type:uuid;index" json:"reviewed_by,omitempty"`
	Reviewer    *User      `gorm:"foreignKey:ReviewedBy" json:"reviewer,omitempty"`
	ReviewedAt  *time.Time `json:"reviewed_at,omitempty"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (EventCancellationRequest) TableName() string { return "event_cancellation_requests" }

type CreateEventCancellationRequest struct {
	Reason string `json:"reason" binding:"required,min=10,max=500"`
}

type ReviewEventCancellationRequest struct {
	AdminRemark string `json:"admin_remark" binding:"required,min=3,max=1000"`
}
