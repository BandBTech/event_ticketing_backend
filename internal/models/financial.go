package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EventSales tracks total sales for each event with commission breakdown
type EventSales struct {
	ID               uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	EventID          uuid.UUID      `gorm:"type:uuid;not null;unique;index" json:"event_id"`
	Event            *Event         `gorm:"foreignKey:EventID" json:"event,omitempty"`
	OrganizerID      uuid.UUID      `gorm:"type:uuid;not null;index" json:"organizer_id"`
	Organizer        *User          `gorm:"foreignKey:OrganizerID" json:"organizer,omitempty"`
	TotalTicketsSold int            `gorm:"not null;default:0" json:"total_tickets_sold"`
	GrossRevenue     float64        `gorm:"not null;default:0" json:"gross_revenue"`     // Total ticket sales
	CommissionRate   float64        `gorm:"not null" json:"commission_rate"`             // Commission rate for this event
	CommissionAmount float64        `gorm:"not null;default:0" json:"commission_amount"` // Total commission earned by platform
	OrganizerShare   float64        `gorm:"not null;default:0" json:"organizer_share"`   // Amount due to organizer
	PaidAmount       float64        `gorm:"not null;default:0" json:"paid_amount"`       // Amount already paid to organizer
	DueAmount        float64        `gorm:"not null;default:0" json:"due_amount"`        // Amount still due to organizer
	LastPaymentDate  *time.Time     `json:"last_payment_date"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

// REMOVED: EventSales is redundant - we can calculate this from Transactions table
// Use these queries instead:
// - Total tickets sold: SELECT SUM(quantity) FROM transactions WHERE event_id = ?
// - Gross revenue: SELECT SUM(amount) FROM transactions WHERE event_id = ?
// - Commission amount: SELECT SUM(commission_amount) FROM transactions WHERE event_id = ?

// PaymentBill represents bills created by admin for organizer payments
type PaymentBill struct {
	ID            uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	BillNumber    string         `gorm:"unique;not null;size:50" json:"bill_number"` // Unique bill identifier
	EventID       uuid.UUID      `gorm:"type:uuid;not null;index" json:"event_id"`
	Event         *Event         `gorm:"foreignKey:EventID" json:"event,omitempty"`
	OrganizerID   uuid.UUID      `gorm:"type:uuid;not null;index" json:"organizer_id"`
	Organizer     *User          `gorm:"foreignKey:OrganizerID" json:"organizer,omitempty"`
	AdminID       uuid.UUID      `gorm:"type:uuid;not null;index" json:"admin_id"`
	Admin         *User          `gorm:"foreignKey:AdminID" json:"admin,omitempty"`
	BillAmount    float64        `gorm:"not null" json:"bill_amount"`              // Amount being billed/paid
	PaymentMethod PaymentMethod  `gorm:"not null" json:"payment_method"`           // bank_transfer, check, cash, etc.
	PaymentRef    string         `json:"payment_ref"`                              // Transaction reference
	Status        string         `gorm:"not null;default:'pending'" json:"status"` // pending, paid, cancelled
	Notes         string         `gorm:"type:text" json:"notes"`
	BillDate      time.Time      `json:"bill_date"`
	PaidDate      *time.Time     `json:"paid_date"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

// Transaction represents a complete transaction record for ticket purchases
type Transaction struct {
	ID               uuid.UUID              `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	TransactionID    string                 `gorm:"unique;not null;size:100" json:"transaction_id"` // Unique transaction identifier
	EventID          uuid.UUID              `gorm:"type:uuid;not null;index" json:"event_id"`
	Event            *Event                 `gorm:"foreignKey:EventID" json:"event,omitempty"`
	UserID           *uuid.UUID             `gorm:"type:uuid;index" json:"user_id,omitempty"` // Nullable for guest purchases
	User             *User                  `gorm:"foreignKey:UserID" json:"user,omitempty"`
	GuestUserID      *uuid.UUID             `gorm:"type:uuid;index" json:"guest_user_id,omitempty"` // For guest purchases
	GuestUser        *GuestUser             `gorm:"foreignKey:GuestUserID" json:"guest_user,omitempty"`
	TicketIDs        []uuid.UUID            `gorm:"type:uuid[];not null" json:"ticket_ids"`     // Array of ticket IDs in this transaction
	PaymentGateway   PaymentGateway         `gorm:"not null" json:"payment_gateway"`            // Payment method used
	Amount           float64                `gorm:"not null" json:"amount"`                     // Total transaction amount
	Currency         string                 `gorm:"not null;default:'USD'" json:"currency"`     // Currency used
	Quantity         int                    `gorm:"not null" json:"quantity"`                   // Number of tickets purchased
	Status           string                 `gorm:"not null;default:'completed'" json:"status"` // completed, pending, failed, refunded
	GatewayTxnID     string                 `json:"gateway_txn_id"`                             // Transaction ID from payment gateway
	GatewayData      map[string]interface{} `gorm:"type:jsonb" json:"gateway_data"`             // Additional gateway-specific data
	CommissionRate   float64                `gorm:"not null" json:"commission_rate"`            // Commission rate applied
	CommissionAmount float64                `gorm:"not null" json:"commission_amount"`          // Commission earned by platform
	OrganizerShare   float64                `gorm:"not null" json:"organizer_share"`            // Amount due to organizer
	ProcessedAt      *time.Time             `json:"processed_at"`                               // When payment was processed
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
	DeletedAt        gorm.DeletedAt         `gorm:"index" json:"-"`
}

// Request/Response models

// CreatePaymentBillRequest represents the request to create a payment bill
type CreatePaymentBillRequest struct {
	EventID       uuid.UUID     `json:"event_id" binding:"required"`
	OrganizerID   uuid.UUID     `json:"organizer_id" binding:"required"`
	BillAmount    float64       `json:"bill_amount" binding:"required,gt=0"`
	PaymentMethod PaymentMethod `json:"payment_method" binding:"required,payment_method"`
	PaymentRef    string        `json:"payment_ref,omitempty"`
	Notes         string        `json:"notes,omitempty"`
}

// UpdatePaymentBillRequest represents the request to update payment bill status
type UpdatePaymentBillRequest struct {
	Status     string `json:"status" binding:"required,oneof=paid cancelled"`
	PaymentRef string `json:"payment_ref,omitempty"`
	Notes      string `json:"notes,omitempty"`
}

// EventSalesResponse represents event sales data in API responses
type EventSalesResponse struct {
	ID               uuid.UUID  `json:"id"`
	EventID          uuid.UUID  `json:"event_id"`
	EventTitle       string     `json:"event_title"`
	OrganizerID      uuid.UUID  `json:"organizer_id"`
	OrganizerName    string     `json:"organizer_name"`
	TotalTicketsSold int        `json:"total_tickets_sold"`
	GrossRevenue     float64    `json:"gross_revenue"`
	CommissionRate   float64    `json:"commission_rate"`
	CommissionAmount float64    `json:"commission_amount"`
	OrganizerShare   float64    `json:"organizer_share"`
	PaidAmount       float64    `json:"paid_amount"`
	DueAmount        float64    `json:"due_amount"`
	LastPaymentDate  *time.Time `json:"last_payment_date"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// PaymentBillResponse represents payment bill data in API responses
type PaymentBillResponse struct {
	ID            uuid.UUID     `json:"id"`
	BillNumber    string        `json:"bill_number"`
	EventID       uuid.UUID     `json:"event_id"`
	EventTitle    string        `json:"event_title"`
	OrganizerID   uuid.UUID     `json:"organizer_id"`
	OrganizerName string        `json:"organizer_name"`
	AdminID       uuid.UUID     `json:"admin_id"`
	AdminName     string        `json:"admin_name"`
	BillAmount    float64       `json:"bill_amount"`
	PaymentMethod PaymentMethod `json:"payment_method"`
	PaymentRef    string        `json:"payment_ref"`
	Status        string        `json:"status"`
	Notes         string        `json:"notes"`
	BillDate      time.Time     `json:"bill_date"`
	PaidDate      *time.Time    `json:"paid_date"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// AdminFinancialSummary represents overall financial summary for admin
type AdminFinancialSummary struct {
	TotalGrossRevenue     float64 `json:"total_gross_revenue"`
	TotalCommissions      float64 `json:"total_commissions"`
	TotalOrganizerShare   float64 `json:"total_organizer_share"`
	TotalPaidOut          float64 `json:"total_paid_out"`
	TotalDue              float64 `json:"total_due"`
	ActiveEvents          int64   `json:"active_events"`
	TotalTicketsSold      int64   `json:"total_tickets_sold"`
	AverageCommissionRate float64 `json:"average_commission_rate"`
}

// OrganizerFinancialSummary represents financial summary for organizer
type OrganizerFinancialSummary struct {
	OrganizerID       uuid.UUID `json:"organizer_id"`
	OrganizerName     string    `json:"organizer_name"`
	TotalEvents       int64     `json:"total_events"`
	TotalTicketsSold  int64     `json:"total_tickets_sold"`
	TotalGrossRevenue float64   `json:"total_gross_revenue"`
	TotalEarnings     float64   `json:"total_earnings"` // After commission
	TotalReceived     float64   `json:"total_received"` // Amount received from admin
	AmountDue         float64   `json:"amount_due"`     // Still pending
}

// TransactionResponse represents transaction data in API responses
type TransactionResponse struct {
	ID               uuid.UUID      `json:"id"`
	TransactionID    string         `json:"transaction_id"`
	EventID          uuid.UUID      `json:"event_id"`
	EventTitle       string         `json:"event_title"`
	UserID           *uuid.UUID     `json:"user_id,omitempty"`
	UserName         *string        `json:"user_name,omitempty"`
	GuestUserID      *uuid.UUID     `json:"guest_user_id,omitempty"`
	GuestUserName    *string        `json:"guest_user_name,omitempty"`
	TicketCount      int            `json:"ticket_count"`
	PaymentGateway   PaymentGateway `json:"payment_gateway"`
	Amount           float64        `json:"amount"`
	Currency         string         `json:"currency"`
	Status           string         `json:"status"`
	GatewayTxnID     string         `json:"gateway_txn_id"`
	CommissionRate   float64        `json:"commission_rate"`
	CommissionAmount float64        `json:"commission_amount"`
	OrganizerShare   float64        `json:"organizer_share"`
	ProcessedAt      *time.Time     `json:"processed_at"`
	CreatedAt        time.Time      `json:"created_at"`
}

// BeforeCreate hooks
func (es *EventSales) BeforeCreate(tx *gorm.DB) error {
	// Calculate due amount initially
	es.DueAmount = es.OrganizerShare - es.PaidAmount
	return nil
}

func (pb *PaymentBill) BeforeCreate(tx *gorm.DB) error {
	if pb.BillNumber == "" {
		pb.BillNumber = generateBillNumber()
	}
	if pb.BillDate.IsZero() {
		pb.BillDate = time.Now()
	}
	return nil
}

func (t *Transaction) BeforeCreate(tx *gorm.DB) error {
	if t.TransactionID == "" {
		t.TransactionID = generateTransactionID()
	}
	if t.ProcessedAt == nil && t.Status == "completed" {
		now := time.Now()
		t.ProcessedAt = &now
	}
	return nil
}

// generateBillNumber creates a unique bill identifier
func generateBillNumber() string {
	now := time.Now()
	return "BILL-" + now.Format("20060102") + "-" + uuid.New().String()[:8]
}

// ToResponse methods
func (es *EventSales) ToResponse() EventSalesResponse {
	eventTitle := ""
	organizerName := ""

	if es.Event != nil {
		eventTitle = es.Event.Title
	}
	if es.Organizer != nil {
		if es.Organizer.OrganizerOnboarding != nil && es.Organizer.OrganizerOnboarding.BusinessName != "" {
			organizerName = es.Organizer.OrganizerOnboarding.BusinessName
		} else {
			organizerName = es.Organizer.FirstName + " " + es.Organizer.LastName
		}
	}

	return EventSalesResponse{
		ID:               es.ID,
		EventID:          es.EventID,
		EventTitle:       eventTitle,
		OrganizerID:      es.OrganizerID,
		OrganizerName:    organizerName,
		TotalTicketsSold: es.TotalTicketsSold,
		GrossRevenue:     es.GrossRevenue,
		CommissionRate:   es.CommissionRate,
		CommissionAmount: es.CommissionAmount,
		OrganizerShare:   es.OrganizerShare,
		PaidAmount:       es.PaidAmount,
		DueAmount:        es.DueAmount,
		LastPaymentDate:  es.LastPaymentDate,
		CreatedAt:        es.CreatedAt,
		UpdatedAt:        es.UpdatedAt,
	}
}

func (pb *PaymentBill) ToResponse() PaymentBillResponse {
	eventTitle := ""
	organizerName := ""
	adminName := ""

	if pb.Event != nil {
		eventTitle = pb.Event.Title
	}
	if pb.Organizer != nil {
		if pb.Organizer.OrganizerOnboarding != nil && pb.Organizer.OrganizerOnboarding.BusinessName != "" {
			organizerName = pb.Organizer.OrganizerOnboarding.BusinessName
		} else {
			organizerName = pb.Organizer.FirstName + " " + pb.Organizer.LastName
		}
	}
	if pb.Admin != nil {
		adminName = pb.Admin.FirstName + " " + pb.Admin.LastName
	}

	return PaymentBillResponse{
		ID:            pb.ID,
		BillNumber:    pb.BillNumber,
		EventID:       pb.EventID,
		EventTitle:    eventTitle,
		OrganizerID:   pb.OrganizerID,
		OrganizerName: organizerName,
		AdminID:       pb.AdminID,
		AdminName:     adminName,
		BillAmount:    pb.BillAmount,
		PaymentMethod: pb.PaymentMethod,
		PaymentRef:    pb.PaymentRef,
		Status:        pb.Status,
		Notes:         pb.Notes,
		BillDate:      pb.BillDate,
		PaidDate:      pb.PaidDate,
		CreatedAt:     pb.CreatedAt,
		UpdatedAt:     pb.UpdatedAt,
	}
}

// ToResponse converts a Transaction model to a TransactionResponse
func (t *Transaction) ToResponse() TransactionResponse {
	var eventTitle string
	if t.Event != nil {
		eventTitle = t.Event.Title
	}

	var userName *string
	if t.User != nil {
		name := t.User.FirstName + " " + t.User.LastName
		userName = &name
	}

	var guestUserName *string
	if t.GuestUser != nil {
		name := t.GuestUser.FirstName + " " + t.GuestUser.LastName
		guestUserName = &name
	}

	return TransactionResponse{
		ID:               t.ID,
		TransactionID:    t.TransactionID,
		EventID:          t.EventID,
		EventTitle:       eventTitle,
		UserID:           t.UserID,
		UserName:         userName,
		GuestUserID:      t.GuestUserID,
		GuestUserName:    guestUserName,
		TicketCount:      len(t.TicketIDs),
		PaymentGateway:   t.PaymentGateway,
		Amount:           t.Amount,
		Currency:         t.Currency,
		Status:           t.Status,
		GatewayTxnID:     t.GatewayTxnID,
		CommissionRate:   t.CommissionRate,
		CommissionAmount: t.CommissionAmount,
		OrganizerShare:   t.OrganizerShare,
		ProcessedAt:      t.ProcessedAt,
		CreatedAt:        t.CreatedAt,
	}
}

// generateTransactionID creates a unique transaction identifier
func generateTransactionID() string {
	// Generate a unique transaction ID like TXN-ABC12345-20240130
	return fmt.Sprintf("TXN-%s-%s", uuid.New().String()[:8], time.Now().Format("20060102"))
}
