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

type EventCancellationRequestResponse struct {
	ID uuid.UUID `json:"id"`

	EventID uuid.UUID `json:"event_id"`
	Event   *struct {
		ID          uuid.UUID `json:"id"`
		Title       string    `json:"title"`
		BannerImage string    `json:"banner_image,omitempty"`
		Currency    string    `json:"currency"`
	} `json:"event,omitempty"`

	OrganizerID uuid.UUID `json:"organizer_id"`
	Organizer   *struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	} `json:"organizer,omitempty"`

	Reason      string     `json:"reason"`
	Status      string     `json:"status"`
	AdminRemark string     `json:"admin_remark,omitempty"`
	ReviewedBy  *uuid.UUID `json:"reviewed_by,omitempty"`
	Reviewer    *struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	} `json:"reviewer,omitempty"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewEventCancellationRequestResponse(ecr *EventCancellationRequest) EventCancellationRequestResponse {
	resp := EventCancellationRequestResponse{
		ID:          ecr.ID,
		EventID:     ecr.EventID,
		OrganizerID: ecr.OrganizerID,
		Reason:      ecr.Reason,
		Status:      ecr.Status,
		AdminRemark: ecr.AdminRemark,
		ReviewedBy:  ecr.ReviewedBy,
		ReviewedAt:  ecr.ReviewedAt,
		CreatedAt:   ecr.CreatedAt,
		UpdatedAt:   ecr.UpdatedAt,
	}

	if ecr.Event != nil {
		resp.Event = &struct {
			ID          uuid.UUID `json:"id"`
			Title       string    `json:"title"`
			BannerImage string    `json:"banner_image,omitempty"`
			Currency    string    `json:"currency"`
		}{
			ID:          ecr.Event.ID,
			Title:       ecr.Event.Title,
			BannerImage: ecr.Event.BannerImage,
			Currency:    ecr.Event.Currency,
		}
	}
	//show name from onboarding details if available, otherwise from user details
	if ecr.Organizer != nil {
		name := ecr.Organizer.FirstName + " " + ecr.Organizer.LastName
		if ecr.Organizer.OrganizerOnboarding != nil && ecr.Organizer.OrganizerOnboarding.BusinessName != "" {
			name = ecr.Organizer.OrganizerOnboarding.BusinessName
		}
		resp.Organizer = &struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		}{
			ID:   ecr.Organizer.ID,
			Name: name,
		}
	}

	if ecr.Reviewer != nil {
		resp.Reviewer = &struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		}{
			ID:   ecr.Reviewer.ID,
			Name: ecr.Reviewer.FirstName + " " + ecr.Reviewer.LastName,
		}
	}

	return resp
}
