package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// // Ticket represents a purchased event ticket (one ticket = one person)
type Ticket struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey"`

	TicketNumber string `gorm:"uniqueIndex"`

	// 👤 OWNER
	ActorID   uuid.UUID
	ActorType string // user | guest

	// 🎟️ EVENT
	EventID uuid.UUID
	Event   *Event `gorm:"foreignKey:EventID"`

	TierID uuid.UUID
	Tier   *EventTier `gorm:"foreignKey:TierID"`

	// 💳 TRANSACTION LINK
	TransactionID uuid.UUID
	Transaction   *Transaction `gorm:"foreignKey:TransactionID"`

	// 💰 PRICE SNAPSHOT (PER TICKET)
	UnitPrice int64
	Currency  string

	// 🎫 STATE (usage state only)
	Status TicketStatus // active, used, invalid

	// 💸 REFUND STATE (separate!)
	RefundStatus TicketRefundStatus // none, partial, full

	// ⏱️ PAYMENT
	PaidAt *time.Time

	// 💡 REFUND SUPPORT (IMPORTANT)
	RefundID     *uuid.UUID
	RefundedAt   *time.Time
	RefundAmount int64
	RefundType   string // partial | full | event_cancel

	// ✅ CHECK-IN
	CheckedInBy *uuid.UUID
	CheckedInAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt
}

// TicketPurchaseRequest represents the request to purchase tickets (unified for both guest and authenticated users)
type TicketPurchaseRequest struct {
	EventID        uuid.UUID             `json:"event_id" binding:"required"`
	Tiers          []TicketTierSelection `json:"tiers" binding:"required,min=1,dive"`
	PaymentGateway string                `json:"payment_gateway" binding:"required"`
	Currency       string                `json:"currency" binding:"required"`
	CustomerEmail  string                `json:"customer_email" binding:"required,email"`
	FirstName      string                `json:"first_name,omitempty"`
	LastName       string                `json:"last_name,omitempty"`
	IdempotencyKey string                `json:"idempotency_key,omitempty"`
	Timezone       string                `json:"timezone,omitempty"`
}

// type GuestPurchaseRequest struct {
// 	EventID        uuid.UUID             `json:"event_id" binding:"required"`
// 	Tiers          []TicketTierSelection `json:"tiers" binding:"required,min=1,dive"`                         // Array of tier selections
// 	PaymentGateway PaymentGateway        `json:"payment_gateway" binding:"required,purchase_payment_gateway"` // Required for payment processing - only stripe and cash allowed
// 	Currency       string                `json:"currency" binding:"required,iso4217_currency_code"`           // ISO 4217 currency code (e.g. USD, EUR)``

// 	// Guest user details (for email receipt and potential account conversion)
// 	Email string `json:"email" binding:"required,email"`
// }

// // TicketTierSelection represents a single tier selection with quantity
// TicketTierSelection represents a single tier selection with quantity
type TicketTierSelection struct {
	TierID   uuid.UUID `json:"tier_id" binding:"required"`
	Quantity int       `json:"quantity" binding:"required,min=1,max=10"`
}

// TicketCheckInRequest represents the request to check-in a ticket
type TicketCheckInRequest struct {
	QRCode       string    `json:"qr_code,omitempty"`       // Secure QR code containing ticket data (optional)
	TicketNumber string    `json:"ticket_number,omitempty"` // Ticket number as backup (optional)
	EventID      uuid.UUID `json:"event_id" binding:"required"`
}

// TicketBulkCheckInRequest represents the request to check-in multiple tickets
type TicketBulkCheckInRequest struct {
	QRCodes       []string  `json:"qr_codes,omitempty"`       // Array of secure QR codes (optional)
	TicketNumbers []string  `json:"ticket_numbers,omitempty"` // Array of ticket numbers as backup (optional)
	EventID       uuid.UUID `json:"event_id" binding:"required"`
}

// CancelTicketRequest represents the request to cancel a purchased ticket
type CancelTicketRequest struct {
	Reason string `json:"reason" binding:"required,max=500"` // Cancellation reason (max 500 chars)
}

// AttendeeResponse represents attendee information for tickets
type AttendeeResponse struct {
	ID    *uuid.UUID `json:"id,omitempty"`
	Name  string     `json:"name"`
	Email string     `json:"email"`
	Type  string     `json:"type"` // "user" or "guest"
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
	CheckedInBy     *uuid.UUID         `json:"checked_in_by,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

// BeforeCreate hook - ticket number should be set before calling Create
// If not set, it will fail validation (ticket_number is required)
func (t *Ticket) BeforeCreate(tx *gorm.DB) error {
	// Ticket number must be generated by the service layer using utils.GenerateEventTicketNumber
	// This ensures centralized, unique random code generation per event
	if t.TicketNumber == "" {
		return fmt.Errorf("ticket_number is required: use utils.GenerateEventTicketNumber to generate it")
	}
	return nil
}

// SimpleTierResponse represents a simplified tier with only essential fields
type SimpleTierResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// OrganizerTicketResponse represents ticket data for organizers (excludes User object)
type OrganizerTicketResponse struct {
	ID              uuid.UUID           `json:"id"`
	TicketNumber    string              `json:"ticket_number"`
	EventID         uuid.UUID           `json:"event_id"`
	Tier            *SimpleTierResponse `json:"tier,omitempty"`
	TotalAmount     float64             `json:"total_amount"`
	PaymentGateway  PaymentGateway      `json:"payment_gateway"`
	Status          string              `json:"status"`
	IsGuestPurchase bool                `json:"is_guest_purchase"`
	CheckInTime     *time.Time          `json:"check_in_time,omitempty"`

	// Attendee information (either registered user or guest)
	Attendee        *AttendeeResponse `json:"attendee,omitempty"`
	CheckedInByName string            `json:"checked_in_by_name,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
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
	QRData          string                   `json:"qr_data"` // Data to generate QR code dynamically
	Tier            *EventTierPublicResponse `json:"tier,omitempty"`
}

// OrderViewResponse represents an order with multiple tickets for frontend rendering
type OrderViewResponse struct {
	OrderID         string               `json:"order_id"`
	Event           *EventPublicResponse `json:"event"`
	Tickets         []TicketViewResponse `json:"tickets"`
	TotalAmount     float64              `json:"total_amount"`
	IsGuestPurchase bool                 `json:"is_guest_purchase"`
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
	EndDate     *time.Time               `json:"end_date,omitempty"`
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
	IsGuestPurchase bool                        `json:"is_guest_purchase"`
	Company         *CompanyInfoMinimalResponse `json:"company,omitempty"`
	PaymentInfo     map[string]interface{}      `json:"payment_info,omitempty"`
}

// User ticket listing response models
type UserTicketListingEventResponse struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	BannerImage string     `json:"banner_image"`
	VenueName   string     `json:"venue_name"`
	Address     string     `json:"address"`
	StartDate   time.Time  `json:"start_date"`
	EndDate     *time.Time `json:"end_date,omitempty"`
	Status      string     `json:"status"`
}

type UserTicketListingTierResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type UserTicketListingResponse struct {
	ID                uuid.UUID                      `json:"id"`
	TicketNumber      string                         `json:"ticket_number"`
	Event             UserTicketListingEventResponse `json:"event"`
	Tier              UserTicketListingTierResponse  `json:"tier"`
	TransactionStatus string                         `json:"transaction_status"`
	CreatedAt         time.Time                      `json:"created_at"`
	UpdatedAt         time.Time                      `json:"updated_at"`
}

type UserTransactionGroupResponse struct {
	TransactionID uuid.UUID                      `json:"transaction_id"`
	Event         UserTicketListingEventResponse `json:"event"`
	Tier          UserTicketListingTierResponse  `json:"tier"`
	Tickets       []UserTicketListingResponse    `json:"tickets"`
	TotalAmount   float64                        `json:"total_amount"`
	CreatedAt     time.Time                      `json:"created_at"`
	UpdatedAt     time.Time                      `json:"updated_at"`
}

type UserTicketSingleResponse struct {
	ID           uuid.UUID                      `json:"id"`
	TicketNumber string                         `json:"ticket_number"`
	Event        UserTicketListingEventResponse `json:"event"`
	Tier         UserTicketListingTierResponse  `json:"tier"`
	CreatedAt    time.Time                      `json:"created_at"`
	UpdatedAt    time.Time                      `json:"updated_at"`
	QRData       string                         `json:"qr_data"`
}

// New response models for flattened listing and transaction details
type UserTicketSummaryResponse struct {
	ID                uuid.UUID                      `json:"id"` // transaction_id
	Event             UserTicketListingEventResponse `json:"event"`
	TicketCount       int                            `json:"ticket_count"`
	TransactionStatus string                         `json:"transaction_status"`
	CreatedAt         time.Time                      `json:"created_at"`
	UpdatedAt         time.Time                      `json:"updated_at"`
}

// Transaction details response
type UserTransactionWithTicketsResponse struct {
	ID                uuid.UUID                       `json:"id"` // transaction_id
	Event             UserTicketListingEventResponse  `json:"event"`
	Tickets           []UserTransactionTicketResponse `json:"tickets"`
	TransactionStatus string                          `json:"transaction_status"`
	CreatedAt         time.Time                       `json:"created_at"`
	UpdatedAt         time.Time                       `json:"updated_at"`
}

type UserTransactionTicketResponse struct {
	ID           uuid.UUID                     `json:"id"`
	TicketNumber string                        `json:"ticket_number"`
	Status       string                        `json:"status"` // active, pending_refund, used, cancelled, refunded, expired
	Tier         UserTicketListingTierResponse `json:"tier"`
	QRData       string                        `json:"qr_data"`
	CheckInTime  *time.Time                    `json:"check_in_time,omitempty"`  // When ticket was scanned/checked-in
	CheckOutTime *time.Time                    `json:"check_out_time,omitempty"` // When ticket was checked-out
	CheckedInBy  *uuid.UUID                    `json:"checked_in_by,omitempty"`  // Staff member ID who checked in
	CheckedOutBy *uuid.UUID                    `json:"checked_out_by,omitempty"` // Staff member ID who checked out
	IsCheckedIn  bool                          `json:"is_checked_in"`            // true if CheckInTime is set (convenience field)
}
