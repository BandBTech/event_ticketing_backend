package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EventTier represents pricing tiers for events
type EventTier struct {
	ID             uuid.UUID              `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	EventID        uuid.UUID              `gorm:"type:uuid;not null;index" json:"event_id"`
	Event          *Event                 `gorm:"foreignKey:EventID" json:"event,omitempty"`
	TierTemplateID uuid.UUID              `gorm:"type:uuid;not null;index" json:"tier_template_id"`
	TierTemplate   *OrganizerTierTemplate `gorm:"foreignKey:TierTemplateID" json:"tier_template,omitempty"`
	TierName       string                 `gorm:"not null;size:100" json:"tier_name"`
	Price          float64                `gorm:"not null" json:"price"`
	Currency       string                 `gorm:"not null;size:3;default:'USD'" json:"currency"`
	Quantity       int                    `gorm:"not null" json:"quantity"`
	Available      int                    `gorm:"not null" json:"available"`
	Sold           int                    `gorm:"not null;default:0" json:"sold"`
	Reserved       int                    `gorm:"not null;default:0" json:"reserved"` // Temporarily held tickets
	GST            float64                `gorm:"default:0" json:"gst"`               // GST percentage
	SalesStart     *time.Time             `json:"sales_start,omitempty"`
	SalesEnd       *time.Time             `json:"sales_end,omitempty"`
	IsActive       bool                   `gorm:"not null;default:true" json:"is_active"`
	SortOrder      int                    `gorm:"default:0" json:"sort_order"` // For ordering tiers
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	DeletedAt      gorm.DeletedAt         `gorm:"index" json:"-"`
}

// EventTierPublicResponse represents the public-facing tier data for events
type EventTierPublicResponse struct {
	ID         uuid.UUID  `json:"id"`
	TierName   string     `json:"tier_name"`
	Price      float64    `json:"price"`
	Currency   string     `json:"currency,omitempty"`
	Available  int        `json:"available"`
	Sold       int        `json:"sold"`
	SalesStart *time.Time `json:"sales_start,omitempty"`
	SalesEnd   *time.Time `json:"sales_end,omitempty"`
}

// ToPublicResponse converts EventTier to EventTierPublicResponse
func (et *EventTier) ToPublicResponse() EventTierPublicResponse {
	return EventTierPublicResponse{
		ID:         et.ID,
		TierName:   et.TierName,
		Price:      et.Price,
		Currency:   et.Currency,
		Available:  et.Available,
		Sold:       et.Sold,
		SalesStart: et.SalesStart,
		SalesEnd:   et.SalesEnd,
	}
}

// Discount represents discount configurations for events
type Discount struct {
	ID          uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	EventID     uuid.UUID      `gorm:"type:uuid;not null;index" json:"event_id"`
	Event       *Event         `gorm:"foreignKey:EventID" json:"event,omitempty"`
	TierID      *uuid.UUID     `gorm:"type:uuid;index" json:"tier_id,omitempty"` // If discount applies to specific tier
	Tier        *EventTier     `gorm:"foreignKey:TierID" json:"tier,omitempty"`
	Name        string         `gorm:"not null;size:100" json:"name"`
	Description string         `gorm:"type:text" json:"description,omitempty"`
	Type        string         `gorm:"not null;size:20" json:"type"`  // percentage, fixed_amount, buy_x_get_y
	Amount      float64        `gorm:"not null" json:"amount"`        // Percentage or fixed amount
	MinQuantity int            `gorm:"default:1" json:"min_quantity"` // Minimum quantity for discount
	MaxQuantity int            `gorm:"default:0" json:"max_quantity"` // Maximum quantity for discount (0 = unlimited)
	ValidFrom   *time.Time     `json:"valid_from,omitempty"`
	ValidTo     *time.Time     `json:"valid_to,omitempty"`
	IsActive    bool           `gorm:"not null;default:true" json:"is_active"`
	UsageLimit  int            `gorm:"default:0" json:"usage_limit"` // 0 = unlimited
	UsageCount  int            `gorm:"default:0" json:"usage_count"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// Promocode represents promocode configurations for events
type Promocode struct {
	ID          uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	EventID     uuid.UUID      `gorm:"type:uuid;not null;index" json:"event_id"`
	Event       *Event         `gorm:"foreignKey:EventID" json:"event,omitempty"`
	TierID      *uuid.UUID     `gorm:"type:uuid;index" json:"tier_id,omitempty"` // If promocode applies to specific tier
	Tier        *EventTier     `gorm:"foreignKey:TierID" json:"tier,omitempty"`
	Code        string         `gorm:"not null;unique;size:50" json:"code"`
	Name        string         `gorm:"not null;size:100" json:"name"`
	Description string         `gorm:"type:text" json:"description,omitempty"`
	Type        string         `gorm:"not null;size:20" json:"type"`  // percentage, fixed_amount
	Amount      float64        `gorm:"not null" json:"amount"`        // Percentage or fixed amount
	MinAmount   float64        `gorm:"default:0" json:"min_amount"`   // Minimum order amount
	MaxDiscount float64        `gorm:"default:0" json:"max_discount"` // Maximum discount amount (0 = unlimited)
	ValidFrom   *time.Time     `json:"valid_from,omitempty"`
	ValidTo     *time.Time     `json:"valid_to,omitempty"`
	IsActive    bool           `gorm:"not null;default:true" json:"is_active"`
	UsageLimit  int            `gorm:"default:0" json:"usage_limit"` // 0 = unlimited
	UsageCount  int            `gorm:"default:0" json:"usage_count"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// OrganizerTierTemplate represents reusable tier name templates for organizers
type OrganizerTierTemplate struct {
	ID           uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	OrganizerID  uuid.UUID      `gorm:"type:uuid;not null;uniqueIndex:idx_organizer_template_name" json:"organizer_id"`
	Organizer    *User          `gorm:"foreignKey:OrganizerID" json:"organizer,omitempty" swaggerignore:"true"`
	TemplateName string         `gorm:"not null;size:100;uniqueIndex:idx_organizer_template_name" json:"template_name"`
	Description  string         `gorm:"type:text" json:"description,omitempty"`
	IsActive     bool           `gorm:"not null;default:true" json:"is_active"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// PayoutRequest represents organizer payout requests
type PayoutRequest struct {
	ID            uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	RequestNumber string         `gorm:"unique;not null;size:50" json:"request_number"`
	OrganizerID   uuid.UUID      `gorm:"type:uuid;not null;index" json:"organizer_id"`
	Organizer     *User          `gorm:"foreignKey:OrganizerID" json:"organizer,omitempty"`
	EventID       uuid.UUID      `gorm:"type:uuid;not null;index" json:"event_id"` // Required - one event per payout request
	Event         *Event         `gorm:"foreignKey:EventID" json:"event,omitempty"`
	Amount        float64        `gorm:"not null" json:"amount"`
	Status        string         `gorm:"not null;default:'pending'" json:"status"`            // pending, approved, rejected
	RequestType   string         `gorm:"not null;default:'event_payout'" json:"request_type"` // event_payout (single event)
	Description   string         `gorm:"type:text" json:"description,omitempty"`
	AdminNotes    string         `gorm:"type:text" json:"admin_notes,omitempty"`
	ProcessedBy   *uuid.UUID     `gorm:"type:uuid" json:"processed_by,omitempty"`
	ProcessedAt   *time.Time     `json:"processed_at,omitempty"`
	PaymentBillID *uuid.UUID     `gorm:"type:uuid;index" json:"payment_bill_id,omitempty"` // Link to created bill when approved
	PaymentBill   *PaymentBill   `gorm:"foreignKey:PaymentBillID" json:"payment_bill,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

// Request/Response DTOs

// CreateEventTierRequest represents the request to create an event tier
type CreateEventTierRequest struct {
	TierTemplateID uuid.UUID  `json:"tier_template_id" binding:"required"`
	Price          float64    `json:"price" binding:"required,min=0"`
	Currency       string     `json:"currency" binding:"omitempty,len=3"`
	Quantity       int        `json:"quantity" binding:"required,min=1"`
	GST            float64    `json:"gst" binding:"omitempty,min=0,max=100"`
	SalesStart     *time.Time `json:"sales_start,omitempty"`
	SalesEnd       *time.Time `json:"sales_end,omitempty"`
	SortOrder      int        `json:"sort_order" binding:"omitempty,min=0"`
}

// UpdateEventTierRequest represents the request to update an event tier
type UpdateEventTierRequest struct {
	TierTemplateID *uuid.UUID `json:"tier_template_id,omitempty"`
	Price          float64    `json:"price" binding:"omitempty,min=0"`
	Currency       string     `json:"currency" binding:"omitempty,len=3"`
	Quantity       int        `json:"quantity" binding:"omitempty,min=1"`
	GST            float64    `json:"gst" binding:"omitempty,min=0,max=100"`
	SalesStart     *time.Time `json:"sales_start,omitempty"`
	SalesEnd       *time.Time `json:"sales_end,omitempty"`
	IsActive       *bool      `json:"is_active,omitempty"`
	SortOrder      int        `json:"sort_order" binding:"omitempty,min=0"`
}

// EventSalesControlRequest represents request to control event sales
type EventSalesControlRequest struct {
	Action string `json:"action" binding:"required,oneof=pause resume stop"`
	Reason string `json:"reason,omitempty"`
}

// EventCancellationRequest represents request to cancel an event
type EventCancellationRequest struct {
	Reason string `json:"reason" binding:"required,min=10,max=500"`
}

// PayoutRequestCreate represents request to create a payout request
type PayoutRequestCreate struct {
	EventID     uuid.UUID `json:"event_id" binding:"required"`
	Amount      float64   `json:"amount" binding:"required,min=0"`
	RequestType string    `json:"request_type" binding:"required,oneof=event_payout"`
	Description string    `json:"description,omitempty"`
}

// PayoutRequestUpdate represents request to update payout request status (Admin only)
type PayoutRequestUpdate struct {
	Status     string `json:"status" binding:"required,oneof=approved rejected"`
	AdminNotes string `json:"admin_notes,omitempty"`
}

// PayoutRequestResponse represents the response for payout requests with limited event data
type PayoutRequestResponse struct {
	ID            uuid.UUID             `json:"id"`
	RequestNumber string                `json:"request_number"`
	OrganizerID   uuid.UUID             `json:"organizer_id"`
	Organizer     *User                 `json:"organizer,omitempty"`
	EventID       uuid.UUID             `json:"event_id"`
	Event         *EventSummaryResponse `json:"event,omitempty"`
	Amount        float64               `json:"amount"`
	Status        string                `json:"status"`
	RequestType   string                `json:"request_type"`
	Description   string                `json:"description,omitempty"`
	AdminNotes    string                `json:"admin_notes,omitempty"`
	ProcessedBy   *uuid.UUID            `json:"processed_by,omitempty"`
	ProcessedAt   *time.Time            `json:"processed_at,omitempty"`
	PaymentBillID *uuid.UUID            `json:"payment_bill_id,omitempty"`
	PaymentBill   *PaymentBill          `json:"payment_bill,omitempty"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

// ToResponse converts PayoutRequest to PayoutRequestResponse with limited event data
func (pr *PayoutRequest) ToResponse() PayoutRequestResponse {
	var eventSummary *EventSummaryResponse
	if pr.Event != nil {
		summary := pr.Event.ToSummaryResponse()
		eventSummary = &summary
	}

	return PayoutRequestResponse{
		ID:            pr.ID,
		RequestNumber: pr.RequestNumber,
		OrganizerID:   pr.OrganizerID,
		Organizer:     pr.Organizer,
		EventID:       pr.EventID,
		Event:         eventSummary,
		Amount:        pr.Amount,
		Status:        pr.Status,
		RequestType:   pr.RequestType,
		Description:   pr.Description,
		AdminNotes:    pr.AdminNotes,
		ProcessedBy:   pr.ProcessedBy,
		ProcessedAt:   pr.ProcessedAt,
		PaymentBillID: pr.PaymentBillID,
		PaymentBill:   pr.PaymentBill,
		CreatedAt:     pr.CreatedAt,
		UpdatedAt:     pr.UpdatedAt,
	}
}

// OrganizerTierTemplateResponse represents the response for tier template
type OrganizerTierTemplateResponse struct {
	ID           uuid.UUID `json:"id"`
	OrganizerID  uuid.UUID `json:"organizer_id"`
	TemplateName string    `json:"template_name"`
	Description  string    `json:"description,omitempty"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CreateOrganizerTierTemplateRequest represents request to create a tier template
type CreateOrganizerTierTemplateRequest struct {
	TemplateName string `json:"template_name" binding:"required,min=1,max=100"`
	Description  string `json:"description,omitempty"`
}

// UpdateOrganizerTierTemplateRequest represents request to update a tier template
type UpdateOrganizerTierTemplateRequest struct {
	TemplateName string  `json:"template_name" binding:"omitempty,min=1,max=100"`
	Description  *string `json:"description,omitempty"`
	IsActive     *bool   `json:"is_active,omitempty"`
}

// EventTierAnalytics represents analytics for a single tier
type EventTierAnalytics struct {
	TierID     uuid.UUID  `json:"tier_id"`
	TierName   string     `json:"tier_name"`
	Price      float64    `json:"price"`
	Currency   string     `json:"currency,omitempty"`
	TotalSeats int        `json:"total_seats"`
	SoldSeats  int        `json:"sold_seats"`
	AvailSeats int        `json:"available_seats"`
	Revenue    float64    `json:"revenue"`
	SalesStart *time.Time `json:"sales_start,omitempty"`
	SalesEnd   *time.Time `json:"sales_end,omitempty"`
	IsActive   bool       `json:"is_active"`
}

// EventAnalyticsResponse represents complete event analytics
type EventAnalyticsResponse struct {
	EventID        uuid.UUID            `json:"event_id"`
	EventTitle     string               `json:"event_title"`
	EventStatus    string               `json:"event_status"`
	SalesStatus    string               `json:"sales_status"`
	TotalSeats     int                  `json:"total_seats"`
	SoldSeats      int                  `json:"sold_seats"`
	AvailSeats     int                  `json:"available_seats"`
	TotalRevenue   float64              `json:"total_revenue"`
	CommissionRate float64              `json:"commission_rate"`
	OrganizerShare float64              `json:"organizer_share"`
	TierCount      int                  `json:"tier_count"`
	Tiers          []EventTierAnalytics `json:"tiers"`
	CreatedAt      time.Time            `json:"created_at"`
}

// BeforeCreate hooks
func (et *EventTier) BeforeCreate(tx *gorm.DB) error {
	et.Available = et.Quantity
	return nil
}

func (pr *PayoutRequest) BeforeCreate(tx *gorm.DB) error {
	if pr.RequestNumber == "" {
		pr.RequestNumber = generatePayoutRequestNumber()
	}
	return nil
}

// generatePayoutRequestNumber creates a unique payout request number
func generatePayoutRequestNumber() string {
	now := time.Now()
	return "PAYOUT-" + now.Format("20060102") + "-" + uuid.New().String()[:8]
}

// ToAnalytics converts EventTier to EventTierAnalytics
func (et *EventTier) ToAnalytics() EventTierAnalytics {
	return EventTierAnalytics{
		TierID:     et.ID,
		TierName:   et.TierName,
		Price:      et.Price,
		Currency:   et.Currency,
		TotalSeats: et.Quantity,
		SoldSeats:  et.Sold,
		AvailSeats: et.Available,
		Revenue:    float64(et.Sold) * et.Price,
		SalesStart: et.SalesStart,
		SalesEnd:   et.SalesEnd,
		IsActive:   et.IsActive,
	}
}
