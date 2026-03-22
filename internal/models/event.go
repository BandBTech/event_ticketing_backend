package models

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// StringArray represents a PostgreSQL text array that can be used with Swagger
type StringArray []string

// Value implements the driver.Valuer interface for database storage
func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	return "{" + strings.Join([]string(a), ",") + "}", nil
}

// Scan implements the sql.Scanner interface for database retrieval
func (a *StringArray) Scan(value interface{}) error {
	if value == nil {
		*a = nil
		return nil
	}

	str, ok := value.(string)
	if !ok {
		return nil
	}

	// Remove PostgreSQL array brackets and split
	str = strings.Trim(str, "{}")
	if str == "" {
		*a = StringArray{}
		return nil
	}

	parts := strings.Split(str, ",")
	*a = make(StringArray, len(parts))
	for i, part := range parts {
		// Remove surrounding quotes if present
		part = strings.Trim(part, `"`)
		(*a)[i] = part
	}
	return nil
}

// MarshalJSON implements json.Marshaler for JSON serialization
func (a StringArray) MarshalJSON() ([]byte, error) {
	return json.Marshal([]string(a))
}

// UnmarshalJSON implements json.Unmarshaler for JSON deserialization
func (a *StringArray) UnmarshalJSON(data []byte) error {
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	*a = StringArray(arr)
	return nil
}

type Event struct {
	ID             uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id" swaggerignore:"true"`
	Title          string     `gorm:"not null;size:200" json:"title" binding:"required"`
	Description    string     `gorm:"type:text" json:"description"` // HTML content
	BannerImage    string     `gorm:"size:500" json:"banner_image"`
	Category       string     `gorm:"type:text" json:"category"` // Single category tag
	VenueName      string     `gorm:"size:200" json:"venue_name"`
	Address        string     `gorm:"type:text" json:"address"`
	Location       string     `gorm:"size:200" json:"location"` // Keep for backward compatibility
	StartDate      time.Time  `gorm:"not null" json:"start_date" binding:"required"`
	EndDate        time.Time  `gorm:"not null" json:"end_date" binding:"required"`
	Timezone       string     `gorm:"size:50;default:'UTC'" json:"timezone"`
	Capacity       int        `gorm:"not null" json:"capacity" binding:"required,min=1"`
	Available      int        `gorm:"not null" json:"available"`
	Price          float64    `gorm:"not null" json:"price" binding:"required,min=0"` // Base price for backward compatibility
	Currency       string     `gorm:"size:3;default:'USD'" json:"currency"`           // ISO 4217 currency code
	CommissionRate float64    `gorm:"not null;default:10" json:"commission_rate"`     // Platform commission percentage (0-100)
	Status         string     `gorm:"not null;default:'draft'" json:"status"`         // draft, pending, approved, on_sale, live, completed, scheduled, hold, held, rejected, cancelled
	SalesStatus    string     `gorm:"not null;default:'active'" json:"sales_status"`  // active, paused, stopped
	IsFeatured     bool       `gorm:"not null;default:false" json:"is_featured"`      // Featured event flag
	IsCancelled    bool       `gorm:"not null;default:false" json:"is_cancelled"`
	CancelledAt    *time.Time `json:"cancelled_at,omitempty"`
	CancelReason   string     `gorm:"type:text" json:"cancel_reason,omitempty"`
	OrganizerID    uuid.UUID  `gorm:"type:uuid;index" json:"organizer_id"`
	Organizer      *User      `gorm:"foreignKey:OrganizerID" json:"organizer,omitempty"`
	AdminRemark    string     `gorm:"type:text" json:"admin_remark"`

	// Relations
	Tiers         []EventTier          `gorm:"foreignKey:EventID;constraint:OnDelete:CASCADE" json:"tiers,omitempty"`
	StatusHistory []EventStatusHistory `gorm:"foreignKey:EventID;constraint:OnDelete:CASCADE" json:"status_history,omitempty"`
	// Discounts  []Discount  `gorm:"foreignKey:EventID;constraint:OnDelete:CASCADE" json:"discounts,omitempty"`  // Temporarily disabled - tables don't exist
	// Promocodes []Promocode `gorm:"foreignKey:EventID;constraint:OnDelete:CASCADE" json:"promocodes,omitempty"` // Temporarily disabled - tables don't exist

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// OrganizerPublicResponse represents public organizer information for events
type OrganizerPublicResponse struct {
	ID              uuid.UUID `json:"id"`
	BusinessName    string    `json:"business_name"`     // business_name from onboarding
	BusinessLogoURL string    `json:"business_logo_url"` // business_logo_url from onboarding
}

// EventPublicResponse represents the public-facing event data
type EventPublicResponse struct {
	ID          uuid.UUID                 `json:"id"`
	Title       string                    `json:"title"`
	Description string                    `json:"description"`
	BannerImage string                    `json:"banner_image"`
	Category    string                    `json:"category"`
	VenueName   string                    `json:"venue_name"`
	Address     string                    `json:"address"`
	Location    string                    `json:"location"`
	StartDate   time.Time                 `json:"start_date"`
	EndDate     time.Time                 `json:"end_date"`
	Timezone    string                    `json:"timezone"`
	Capacity    int                       `json:"capacity"`
	Available   int                       `json:"available"`
	Price       float64                   `json:"price"`
	Currency    string                    `json:"currency"`
	Status      string                    `json:"status"`
	SalesStatus string                    `json:"sales_status"`
	IsFeatured  bool                      `json:"is_featured"`
	IsCancelled bool                      `json:"is_cancelled"`
	Organizer   *OrganizerPublicResponse  `json:"organizer,omitempty"`
	Tiers       []EventTierPublicResponse `json:"tiers,omitempty"`
	CreatedAt   time.Time                 `json:"created_at"`
}

type EventPublicSummaryResponse struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	BannerImage string    `json:"banner_image"`
	Category    string    `json:"category"`
	StartDate   time.Time `json:"start_date"`
	EndDate     time.Time `json:"end_date"`
	Status      string    `json:"status"`
	SalesStatus string    `json:"sales_status"`
	IsFeatured  bool      `json:"is_featured"`
	VenueName   string    `json:"venue_name"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type EventSummaryResponse struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	BannerImage string    `json:"banner_image"`
	Status      string    `json:"status"`
}

// ToPublicResponse converts Event to EventPublicResponse with filtered tiers data
func (e *Event) ToPublicResponse() EventPublicResponse {
	var publicTiers []EventTierPublicResponse
	for _, tier := range e.Tiers {
		publicTiers = append(publicTiers, tier.ToPublicResponse())
	}

	var publicOrganizer *OrganizerPublicResponse
	if e.Organizer != nil {
		publicOrganizer = e.Organizer.ToOrganizerPublicResponse()
	}

	return EventPublicResponse{
		ID:          e.ID,
		Title:       e.Title,
		Description: e.Description,
		BannerImage: e.BannerImage,
		Category:    e.Category,
		VenueName:   e.VenueName,
		Address:     e.Address,
		Location:    e.Location,
		StartDate:   e.StartDate,
		EndDate:     e.EndDate,
		Timezone:    e.Timezone,
		Capacity:    e.Capacity,
		Available:   e.Available,
		Price:       e.Price,
		Currency:    e.Currency,
		Status:      e.Status,
		SalesStatus: e.SalesStatus,
		IsFeatured:  e.IsFeatured,
		IsCancelled: e.IsCancelled,
		Organizer:   publicOrganizer,
		Tiers:       publicTiers,
		CreatedAt:   e.CreatedAt,
	}
}

// ToPublicSummaryResponse converts Event to EventPublicSummaryResponse with minimal data for lists
func (e *Event) ToPublicSummaryResponse() EventPublicSummaryResponse {
	return EventPublicSummaryResponse{
		ID:          e.ID,
		Title:       e.Title,
		BannerImage: e.BannerImage,
		Category:    e.Category,
		StartDate:   e.StartDate,
		EndDate:     e.EndDate,
		Status:      e.Status,
		SalesStatus: e.SalesStatus,
		IsFeatured:  e.IsFeatured,
		VenueName:   e.VenueName,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}
}

// ToSummaryResponse converts Event to EventSummaryResponse with only essential fields
func (e *Event) ToSummaryResponse() EventSummaryResponse {
	return EventSummaryResponse{
		ID:          e.ID,
		Title:       e.Title,
		BannerImage: e.BannerImage,
		Status:      e.Status,
	}
}

// ToMinimalResponse converts Event to EventMinimalResponse for admin listing
func (e *Event) ToMinimalResponse() EventMinimalResponse {
	return EventMinimalResponse{
		ID:          e.ID,
		Title:       e.Title,
		Category:    e.Category,
		Address:     e.Address,
		StartDate:   e.StartDate,
		EndDate:     e.EndDate,
		BannerImage: e.BannerImage,
		Status:      e.Status,
		SalesStatus: e.SalesStatus,
		IsFeatured:  e.IsFeatured,
		Capacity:    e.Capacity,
		Available:   e.Available,
		Price:       e.Price,
		CreatedAt:   e.CreatedAt,
	}
}

type EventCreateRequest struct {
	Title       string `json:"title" binding:"required,min=3,max=200"`
	Description string `json:"description" binding:"max=10000"`
	BannerImage string `json:"banner_image" binding:"required,url"`
	// Accept single category string in requests; stored on Event as StringArray
	Category       string                   `json:"category" binding:"required"`
	VenueName      string                   `json:"venue_name" binding:"required,min=3,max=200"`
	Address        string                   `json:"address" binding:"required,min=10,max=500"`
	StartDate      time.Time                `json:"start_date" binding:"required"`
	EndDate        time.Time                `json:"end_date" binding:"required,gtfield=StartDate"`
	Timezone       string                   `json:"timezone" binding:"omitempty"`
	Capacity       int                      `json:"capacity" binding:"required,min=1,max=100000"`
	Price          float64                  `json:"price" binding:"required,min=0,max=100000"`
	Currency       string                   `json:"currency" binding:"omitempty,len=3"`                // ISO 4217 currency code (3 letters)
	CommissionRate float64                  `json:"commission_rate" binding:"omitempty,min=0,max=100"` // Optional, only for admin
	Tiers          []CreateEventTierRequest `json:"tiers" binding:"omitempty,dive"`
}

type EventUpdateRequest struct {
	Title          string                   `json:"title" binding:"omitempty,min=3,max=200"`
	Description    string                   `json:"description" binding:"max=10000"`
	BannerImage    string                   `json:"banner_image" binding:"omitempty,url"`
	Category       string                   `json:"category" binding:"omitempty"`
	VenueName      string                   `json:"venue_name" binding:"omitempty,min=3,max=200"`
	Address        string                   `json:"address" binding:"omitempty,min=10,max=500"`
	StartDate      time.Time                `json:"start_date"`
	EndDate        time.Time                `json:"end_date"`
	Timezone       string                   `json:"timezone"`
	Capacity       int                      `json:"capacity" binding:"omitempty,min=1,max=100000"`
	Price          float64                  `json:"price" binding:"omitempty,min=0,max=100000"`
	Currency       string                   `json:"currency" binding:"omitempty,len=3"`                // ISO 4217 currency code (3 letters)
	CommissionRate float64                  `json:"commission_rate" binding:"omitempty,min=0,max=100"` // Only admin can update
	Status         string                   `json:"status" binding:"omitempty,oneof=draft pending approved held rejected"`
	Tiers          []CreateEventTierRequest `json:"tiers" binding:"omitempty,dive"`
}

// EventStatusUpdateRequest represents the request payload for updating event status by admin
type EventStatusUpdateRequest struct {
	Status         string      `json:"status" binding:"required,oneof=pending approved rejected on_sale live hold scheduled cancelled draft completed" example:"approved"`
	CommissionRate interface{} `json:"commission_rate,omitempty" binding:"omitempty" example:"15.5"` // Optional: Admin can set commission rate during status update (accepts string or number, 0-100)
	AdminRemark    string      `json:"admin_remark,omitempty" binding:"omitempty,max=500" example:"Event approved with standard commission rate"`
}

func (e *Event) BeforeCreate(tx *gorm.DB) error {
	e.Available = e.Capacity
	if e.Status == "" {
		e.Status = "pending" // Events start as pending until approved by admin/subadmin
	}
	return nil
}

// EventMinimalResponse represents minimal event data for list views
type EventMinimalResponse struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Category    string    `json:"category"`
	Address     string    `json:"address"`
	StartDate   time.Time `json:"start_date"`
	EndDate     time.Time `json:"end_date"`
	BannerImage string    `json:"banner_image"`
	Status      string    `json:"status"`
	SalesStatus string    `json:"sales_status"`
	IsFeatured  bool      `json:"is_featured"`
	Capacity    int       `json:"capacity"`
	Available   int       `json:"available"`
	Price       float64   `json:"price"`
	CreatedAt   time.Time `json:"created_at"`
}

// EventDetailResponse represents full event data for single event view
type EventDetailResponse struct {
	ID             uuid.UUID   `json:"id"`
	Title          string      `json:"title"`
	Description    string      `json:"description"`
	BannerImage    string      `json:"banner_image"`
	Category       string      `json:"category"`
	VenueName      string      `json:"venue_name"`
	Address        string      `json:"address"`
	Location       string      `json:"location"`
	StartDate      time.Time   `json:"start_date"`
	EndDate        time.Time   `json:"end_date"`
	Timezone       string      `json:"timezone"`
	Capacity       int         `json:"capacity"`
	Available      int         `json:"available"`
	Price          float64     `json:"price"`
	CommissionRate float64     `json:"commission_rate"`
	Status         string      `json:"status"`
	SalesStatus    string      `json:"sales_status"`
	IsFeatured     bool        `json:"is_featured"`
	IsCancelled    bool        `json:"is_cancelled"`
	CancelledAt    *time.Time  `json:"cancelled_at,omitempty"`
	CancelReason   string      `json:"cancel_reason,omitempty"`
	OrganizerID    uuid.UUID   `json:"organizer_id"`
	AdminRemark    string      `json:"admin_remark"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	Tiers          []EventTier `json:"tiers,omitempty"`
	// Discounts      []Discount  `json:"discounts,omitempty"`  // Temporarily disabled - tables don't exist
	// Promocodes     []Promocode `json:"promocodes,omitempty"` // Temporarily disabled - tables don't exist
}

// EventListResponse represents paginated event list response
type EventListResponse struct {
	Events      []EventMinimalResponse `json:"events"`
	Total       int64                  `json:"total"`
	Page        int                    `json:"page"`
	Limit       int                    `json:"limit"`
	TotalPages  int                    `json:"total_pages"`
	HasNext     bool                   `json:"has_next"`
	HasPrevious bool                   `json:"has_previous"`
}

// EventSearchRequest represents search and filter parameters
type EventSearchRequest struct {
	Page     int    `form:"page" binding:"omitempty,min=1"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Search   string `form:"search" binding:"omitempty,max=255"`
	Status   string `form:"status" binding:"omitempty,oneof=draft pending approved held rejected cancelled on_sale live completed scheduled hold"`
	Category string `form:"category" binding:"omitempty,max=100"`
	SortBy   string `form:"sort_by" binding:"omitempty,oneof=created_at title start_date end_date status"`
	SortDir  string `form:"sort_dir" binding:"omitempty,oneof=asc desc"`
}

// EventStatusHistory tracks all status changes for events
type EventStatusHistory struct {
	ID            uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	EventID       uuid.UUID `gorm:"type:uuid;index" json:"event_id"`
	Event         *Event    `gorm:"foreignKey:EventID" json:"event,omitempty"`
	OldStatus     string    `gorm:"size:50" json:"old_status"`
	NewStatus     string    `gorm:"size:50" json:"new_status"`
	StatusType    string    `gorm:"size:20;check:status_type IN ('approval','sales')" json:"status_type"` // 'approval' or 'sales'
	ChangedBy     uuid.UUID `gorm:"type:uuid;index" json:"changed_by"`
	ChangedByUser *User     `gorm:"foreignKey:ChangedBy" json:"changed_by_user,omitempty"`
	Remark        string    `gorm:"type:text" json:"remark"`
	CreatedAt     time.Time `json:"created_at"`

	// Relations
	EventTitle    string `gorm:"-" json:"event_title,omitempty"`     // For display purposes
	ChangedByName string `gorm:"-" json:"changed_by_name,omitempty"` // For display purposes
}

// EventStatusHistoryResponse for API responses
type EventStatusHistoryResponse struct {
	ID            uuid.UUID `json:"id"`
	EventID       uuid.UUID `json:"event_id"`
	EventTitle    string    `json:"event_title"`
	OldStatus     string    `json:"old_status"`
	NewStatus     string    `json:"new_status"`
	StatusType    string    `json:"status_type"`
	ChangedBy     uuid.UUID `json:"changed_by"`
	ChangedByName string    `json:"changed_by_name"`
	Remark        string    `json:"remark"`
	CreatedAt     time.Time `json:"created_at"`
}

// Query Scopes - Reusable database query patterns

// WithTiers preloads event tiers
func WithTiers(db *gorm.DB) *gorm.DB {
	return db.Preload("Tiers")
}

// WithOrganizer preloads organizer information including onboarding details
func WithOrganizer(db *gorm.DB) *gorm.DB {
	return db.Preload("Organizer").Preload("Organizer.OrganizerOnboarding")
}

// WithPublicRelations preloads all public event relations (Tiers, Organizer with Onboarding)
func WithPublicRelations(db *gorm.DB) *gorm.DB {
	return db.Preload("Tiers").Preload("Organizer").Preload("Organizer.OrganizerOnboarding")
}
