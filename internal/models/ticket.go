package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Ticket represents a purchased event ticket (one ticket = one person)
type Ticket struct {
	ID              uuid.UUID      `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	TicketNumber    string         `gorm:"unique;not null;size:50" json:"ticket_number"` // Unique ticket identifier
	UserID          *uuid.UUID     `gorm:"type:uuid;index" json:"user_id,omitempty"`     // Nullable for guest purchases
	User            *User          `gorm:"foreignKey:UserID" json:"user,omitempty"`
	GuestUserID     *uuid.UUID     `gorm:"type:uuid;index" json:"guest_user_id,omitempty"` // For guest purchases
	GuestUser       *GuestUser     `gorm:"foreignKey:GuestUserID" json:"guest_user,omitempty"`
	EventID         uuid.UUID      `gorm:"type:uuid;not null;index" json:"event_id"`
	Event           *Event         `gorm:"foreignKey:EventID" json:"event,omitempty"`
	TierID          uuid.UUID      `gorm:"type:uuid;not null;index" json:"tier_id"`
	Tier            *EventTier     `gorm:"foreignKey:TierID" json:"tier,omitempty"`
	TotalAmount     float64        `gorm:"not null" json:"total_amount"`
	PaymentGateway  PaymentGateway `gorm:"not null" json:"payment_gateway" binding:"payment_gateway"` // Payment method used (stripe, paypal, etc.)
	Status          string         `gorm:"not null;default:'active'" json:"status"`                   // active, pending_verification, used, cancelled, refunded
	IsGuestPurchase bool           `gorm:"default:false" json:"is_guest_purchase"`
	CheckInTime     *time.Time     `json:"check_in_time,omitempty"`
	CheckOutTime    *time.Time     `json:"check_out_time,omitempty"`
	CheckedInBy     *uuid.UUID     `gorm:"type:uuid" json:"checked_in_by,omitempty"`
	CheckedOutBy    *uuid.UUID     `gorm:"type:uuid" json:"checked_out_by,omitempty"`
	PurchaseDate    time.Time      `json:"purchase_date"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

// TicketPurchaseRequest represents the request to purchase tickets
type TicketPurchaseRequest struct {
	EventID        uuid.UUID      `json:"event_id" binding:"required"`
	TierID         uuid.UUID      `json:"tier_id" binding:"required"`
	Quantity       int            `json:"quantity" binding:"required,min=1,max=10"`           // Required, minimum 1, maximum 10 tickets for logged-in users
	PaymentGateway PaymentGateway `json:"payment_gateway" binding:"required,payment_gateway"` // Required for payment processing
}

// TicketCheckInRequest represents the request to check-in a ticket
type TicketCheckInRequest struct {
	QRCode  string    `json:"qr_code" binding:"required"` // Secure QR code containing ticket data
	EventID uuid.UUID `json:"event_id" binding:"required"`
}

// TicketCheckOutRequest represents the request to check-out a ticket
type TicketCheckOutRequest struct {
	QRCode  string    `json:"qr_code" binding:"required"` // Secure QR code containing ticket data
	EventID uuid.UUID `json:"event_id" binding:"required"`
}

// TicketBulkCheckInRequest represents the request to check-in multiple tickets
type TicketBulkCheckInRequest struct {
	QRCodes []string  `json:"qr_codes" binding:"required,min=1,max=50"` // Array of secure QR codes
	EventID uuid.UUID `json:"event_id" binding:"required"`
}

// TicketBulkCheckOutRequest represents the request to check-out multiple tickets
type TicketBulkCheckOutRequest struct {
	QRCodes []string  `json:"qr_codes" binding:"required,min=1,max=50"` // Array of secure QR codes
	EventID uuid.UUID `json:"event_id" binding:"required"`
}

// AttendeeResponse represents attendee information for tickets
type AttendeeResponse struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Type  string `json:"type"` // "user" or "guest"
}

// TicketResponse represents the ticket data in API responses
type TicketResponse struct {
	ID              uuid.UUID          `json:"id"`
	TicketNumber    string             `json:"ticket_number"`
	UserID          *uuid.UUID         `json:"user_id,omitempty"`
	User            *UserResponse      `json:"user,omitempty"`
	GuestUserID     *uuid.UUID         `json:"guest_user_id,omitempty"`
	GuestUser       *GuestUserResponse `json:"guest_user,omitempty"`
	Attendee        *AttendeeResponse  `json:"attendee,omitempty"`
	EventID         uuid.UUID          `json:"event_id"`
	Event           *Event             `json:"event,omitempty"`
	Quantity        int                `json:"quantity"`
	CheckedInCount  int                `json:"checked_in_count"`
	TotalAmount     float64            `json:"total_amount"`
	Status          string             `json:"status"`
	IsGuestPurchase bool               `json:"is_guest_purchase"`
	CheckInTime     *time.Time         `json:"check_in_time,omitempty"`
	CheckOutTime    *time.Time         `json:"check_out_time,omitempty"`
	CheckedInBy     *uuid.UUID         `json:"checked_in_by,omitempty"`
	CheckedOutBy    *uuid.UUID         `json:"checked_out_by,omitempty"`
	PurchaseDate    time.Time          `json:"purchase_date"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

// BeforeCreate generates a unique ticket number
func (t *Ticket) BeforeCreate(tx *gorm.DB) error {
	if t.TicketNumber == "" {
		t.TicketNumber = generateTicketNumber()
	}
	if t.PurchaseDate.IsZero() {
		t.PurchaseDate = time.Now()
	}
	return nil
}

// generateTicketNumber creates a unique ticket number
func generateTicketNumber() string {
	// Generate a short unique ticket number like TKT-ABC12345
	return "TKT-" + uuid.New().String()[:8]
}

// TicketViewResponse represents ticket data for frontend rendering
type TicketViewResponse struct {
	ID              uuid.UUID                `json:"id"`
	TicketNumber    string                   `json:"ticket_number"`
	Event           *EventPublicResponse     `json:"event"`
	Quantity        int                      `json:"quantity"`
	CheckedInCount  int                      `json:"checked_in_count"`
	TotalAmount     float64                  `json:"total_amount"`
	Status          string                   `json:"status"`
	IsGuestPurchase bool                     `json:"is_guest_purchase"`
	CheckInTime     *time.Time               `json:"check_in_time,omitempty"`
	PurchaseDate    time.Time                `json:"purchase_date"`
	QRData          string                   `json:"qr_data"` // Data to generate QR code dynamically
	Tier            *EventTierPublicResponse `json:"tier,omitempty"`
}

// OrderViewResponse represents an order with multiple tickets for frontend rendering
type OrderViewResponse struct {
	OrderID         string               `json:"order_id"`
	Event           *EventPublicResponse `json:"event"`
	Tickets         []TicketViewResponse `json:"tickets"`
	TotalAmount     float64              `json:"total_amount"`
	PurchaseDate    time.Time            `json:"purchase_date"`
	IsGuestPurchase bool                 `json:"is_guest_purchase"`
}

// ToViewResponse converts a Ticket model to a TicketViewResponse for frontend rendering
func (t *Ticket) ToViewResponse() TicketViewResponse {
	var eventResp *EventPublicResponse
	if t.Event != nil {
		resp := t.Event.ToPublicResponse()
		eventResp = &resp
	}

	var tierResp *EventTierPublicResponse
	if t.Tier != nil {
		resp := t.Tier.ToPublicResponse()
		tierResp = &resp
	} else if t.Event != nil && len(t.Event.Tiers) > 0 {
		// Fallback to first tier if specific tier not loaded
		resp := t.Event.Tiers[0].ToPublicResponse()
		tierResp = &resp
	}

	// Generate QR data - using ticket number as the data to encode
	qrData := t.TicketNumber

	return TicketViewResponse{
		ID:              t.ID,
		TicketNumber:    t.TicketNumber,
		Event:           eventResp,
		Quantity:        1, // Each ticket is for 1 person
		CheckedInCount:  0, // Not used in simplified system
		TotalAmount:     t.TotalAmount,
		Status:          t.Status,
		IsGuestPurchase: t.IsGuestPurchase,
		CheckInTime:     t.CheckInTime,
		PurchaseDate:    t.PurchaseDate,
		QRData:          qrData,
		Tier:            tierResp,
	}
}

// ToResponse converts a Ticket model to a TicketResponse for API responses
func (t *Ticket) ToResponse() TicketResponse {
	var userResp *UserResponse
	if t.User != nil {
		resp := t.User.ToResponse()
		userResp = &resp
	}

	var guestUserResp *GuestUserResponse
	if t.GuestUser != nil {
		resp := t.GuestUser.ToResponse()
		guestUserResp = &resp
	}

	// Create attendee info
	var attendeeResp *AttendeeResponse
	if t.User != nil {
		attendeeResp = &AttendeeResponse{
			Name:  t.User.FirstName + " " + t.User.LastName,
			Email: t.User.Email,
			Type:  "user",
		}
	} else if t.GuestUser != nil {
		attendeeResp = &AttendeeResponse{
			Name:  t.GuestUser.FirstName + " " + t.GuestUser.LastName,
			Email: t.GuestUser.Email,
			Type:  "guest",
		}
	}

	return TicketResponse{
		ID:              t.ID,
		TicketNumber:    t.TicketNumber,
		UserID:          t.UserID,
		User:            userResp,
		GuestUserID:     t.GuestUserID,
		GuestUser:       guestUserResp,
		Attendee:        attendeeResp,
		EventID:         t.EventID,
		Event:           t.Event,
		Quantity:        1, // Each ticket is for 1 person
		CheckedInCount:  0, // Not used in simplified system
		TotalAmount:     t.TotalAmount,
		Status:          t.Status,
		IsGuestPurchase: t.IsGuestPurchase,
		CheckInTime:     t.CheckInTime,
		CheckOutTime:    t.CheckOutTime,
		CheckedInBy:     t.CheckedInBy,
		CheckedOutBy:    t.CheckedOutBy,
		PurchaseDate:    t.PurchaseDate,
		CreatedAt:       t.CreatedAt,
		UpdatedAt:       t.UpdatedAt,
	}
}

// Minimal response structures for ticket viewing (focused on design needs)
type TicketViewMinimalResponse struct {
	ID           uuid.UUID `json:"ticket_id"`
	TicketNumber string    `json:"ticket_number"`
	TierName     string    `json:"tier_name"`
	Price        float64   `json:"price"`
	QRData       string    `json:"qr_data"`
	CheckedIn    bool      `json:"checked_in"`
}

type EventViewMinimalResponse struct {
	ID          uuid.UUID                `json:"id"`
	Title       string                   `json:"title"`
	BannerImage string                   `json:"banner_image"`
	VenueName   string                   `json:"venue_name"`
	Address     string                   `json:"address"`
	StartDate   time.Time                `json:"start_date"`
	Timezone    string                   `json:"timezone"`
	Organizer   *OrganizerPublicResponse `json:"organizer,omitempty"`
}

type CompanyInfoMinimalResponse struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	LogoURL    string    `json:"logo_url"`
	Email      string    `json:"email"`
	WebsiteURL string    `json:"website_url"`
}

type OrderViewMinimalResponse struct {
	OrderID         string                      `json:"order_id"`
	Event           *EventViewMinimalResponse   `json:"event"`
	Tickets         []TicketViewMinimalResponse `json:"tickets"`
	TotalAmount     float64                     `json:"total_amount"`
	Currency        string                      `json:"currency"`
	PurchaseDate    time.Time                   `json:"purchase_date"`
	IsGuestPurchase bool                        `json:"is_guest_purchase"`
	Company         *CompanyInfoMinimalResponse `json:"company,omitempty"`
}
