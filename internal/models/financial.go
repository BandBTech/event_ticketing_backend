package models

import (
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

// PaymentBill represents bills created by admin for organizer payments (one bill per event)
type PaymentBill struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	BillNumber  string    `gorm:"unique;not null;size:50" json:"bill_number"` // Unique bill identifier
	EventID     uuid.UUID `gorm:"type:uuid;not null;index" json:"event_id"`   // Single event per bill
	Event       *Event    `gorm:"foreignKey:EventID" json:"event,omitempty"`
	OrganizerID uuid.UUID `gorm:"type:uuid;not null;index" json:"organizer_id"`
	Organizer   *User     `gorm:"foreignKey:OrganizerID" json:"organizer,omitempty"`
	AdminID     uuid.UUID `gorm:"type:uuid;not null;index" json:"admin_id"`
	Admin       *User     `gorm:"foreignKey:AdminID" json:"admin,omitempty"`

	// Financial tracking (organizer earnings after commission deduction)
	TotalRevenue      float64 `gorm:"not null;default:0" json:"total_revenue"`      // Total revenue from event transactions
	TotalCommission   float64 `gorm:"not null;default:0" json:"total_commission"`   // Total commission deducted
	OrganizerEarnings float64 `gorm:"not null;default:0" json:"organizer_earnings"` // Amount owed to organizer (after commission)
	BilledAmount      float64 `gorm:"not null;default:0" json:"billed_amount"`      // Amount included in this bill
	PaidAmount        float64 `gorm:"not null;default:0" json:"paid_amount"`        // Amount actually paid to organizer
	RemainingAmount   float64 `gorm:"not null;default:0" json:"remaining_amount"`   // Remaining amount to pay organizer

	// Payment details
	PaymentMethod PaymentMethod `gorm:"not null" json:"payment_method"`           // bank_transfer, check, cash, etc.
	PaymentRef    string        `json:"payment_ref"`                              // Transaction reference
	Status        string        `gorm:"not null;default:'pending'" json:"status"` // pending, partially_paid, paid, cancelled, overdue

	// Additional tracking
	BillType string     `gorm:"not null;default:'auto_calculated'" json:"bill_type"` // auto_calculated, manual
	Priority string     `gorm:"not null;default:'normal'" json:"priority"`           // low, normal, high, urgent
	DueDate  *time.Time `json:"due_date"`                                            // When payment is due
	Notes    string     `gorm:"type:text" json:"notes"`

	// Dates
	BillDate  time.Time      `json:"bill_date"`
	PaidDate  *time.Time     `json:"paid_date"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// PaymentHistory tracks individual payments made against bills
type PaymentHistory struct {
	ID            uint          `gorm:"primary_key" json:"id"`
	PaymentBillID uuid.UUID     `gorm:"type:uuid;not null;index" json:"payment_bill_id"`
	PaymentBill   *PaymentBill  `gorm:"foreignKey:PaymentBillID" json:"payment_bill,omitempty"`
	Amount        float64       `gorm:"not null" json:"amount"`                    // Payment amount
	PaymentMethod PaymentMethod `gorm:"not null" json:"payment_method"`            // How payment was made
	PaymentRef    string        `json:"payment_ref"`                               // Reference number
	PaymentDate   time.Time     `json:"payment_date"`                              // When payment was made
	ProcessedByID uuid.UUID     `gorm:"type:uuid;not null" json:"processed_by_id"` // Admin who processed payment
	ProcessedBy   *User         `gorm:"foreignKey:ProcessedByID" json:"processed_by,omitempty"`
	Notes         string        `gorm:"type:text" json:"notes"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// Transaction represents a complete transaction record for ticket purchases
type Transaction struct {
	ID               uuid.UUID              `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	EventID          uuid.UUID              `gorm:"type:uuid;not null;index" json:"event_id"`
	Event            *Event                 `gorm:"foreignKey:EventID" json:"event,omitempty"`
	TierID           *uuid.UUID             `gorm:"type:uuid;index" json:"tier_id,omitempty"` // Tier for this purchase
	Tier             *EventTier             `gorm:"foreignKey:TierID" json:"tier,omitempty"`
	UserID           *uuid.UUID             `gorm:"type:uuid;index" json:"user_id,omitempty"` // Nullable for guest purchases
	User             *User                  `gorm:"foreignKey:UserID" json:"user,omitempty"`
	GuestUserID      *uuid.UUID             `gorm:"type:uuid;index" json:"guest_user_id,omitempty"` // For guest purchases
	GuestUser        *GuestUser             `gorm:"foreignKey:GuestUserID" json:"guest_user,omitempty"`
	Tickets          []Ticket               `gorm:"foreignKey:TransactionID" json:"tickets,omitempty"` // Tickets in this transaction (reverse relationship)
	PaymentGateway   PaymentGateway         `gorm:"not null" json:"payment_gateway"`                   // Payment method used
	Amount           float64                `gorm:"not null" json:"amount"`                            // Total transaction amount
	Currency         string                 `gorm:"not null;default:'USD'" json:"currency"`            // Currency used
	Quantity         int                    `gorm:"not null" json:"quantity"`                          // Number of tickets purchased
	Status           string                 `gorm:"not null;default:'completed'" json:"status"`        // completed, pending, failed, refunded
	GatewayTxnID     string                 `json:"gateway_txn_id"`                                    // Transaction ID from payment gateway
	GatewayData      map[string]interface{} `gorm:"type:jsonb" json:"gateway_data"`                    // Additional gateway-specific data
	CommissionRate   float64                `gorm:"not null" json:"commission_rate"`                   // Commission rate applied
	CommissionAmount float64                `gorm:"not null" json:"commission_amount"`                 // Commission earned by platform
	OrganizerShare   float64                `gorm:"not null" json:"organizer_share"`                   // Amount due to organizer
	ProcessedAt      *time.Time             `json:"processed_at"`                                      // When payment was processed
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
	DeletedAt        gorm.DeletedAt         `gorm:"index" json:"-"`
}

// Request/Response models

// CreatePaymentBillRequest represents the request to create a payment bill
type CreatePaymentBillRequest struct {
	EventID       uuid.UUID     `json:"event_id" binding:"required"` // Single event per bill
	OrganizerID   uuid.UUID     `json:"organizer_id" binding:"required"`
	BilledAmount  float64       `json:"billed_amount,omitempty"`  // Manual amount to bill (if not auto-calculating)
	AutoCalculate bool          `json:"auto_calculate,omitempty"` // Auto-calculate owed amounts from transactions
	PaymentMethod PaymentMethod `json:"payment_method" binding:"required,payment_method"`
	Priority      string        `json:"priority,omitempty" binding:"omitempty,oneof=low normal high urgent"`
	DueDate       *time.Time    `json:"due_date,omitempty"`
	PaymentRef    string        `json:"payment_ref,omitempty"`
	Notes         string        `json:"notes,omitempty"`
}

// UpdatePaymentBillRequest represents the request to update payment bill status
type UpdatePaymentBillRequest struct {
	Status        string   `json:"status" binding:"required,oneof=pending partially_paid paid cancelled overdue"`
	PaymentRef    string   `json:"payment_ref,omitempty"`
	PaymentAmount *float64 `json:"payment_amount,omitempty"` // For partial payments
	Notes         string   `json:"notes,omitempty"`
}

// AddPaymentRequest represents adding a payment to an existing bill
type AddPaymentRequest struct {
	Amount        float64       `json:"amount" binding:"required,gt=0"`
	PaymentMethod PaymentMethod `json:"payment_method" binding:"required,payment_method"`
	PaymentRef    string        `json:"payment_ref,omitempty"`
	PaymentDate   *time.Time    `json:"payment_date,omitempty"`
	Notes         string        `json:"notes,omitempty"`
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
	ID            uuid.UUID `json:"id"`
	BillNumber    string    `json:"bill_number"`
	EventID       uuid.UUID `json:"event_id"`
	EventTitle    string    `json:"event_title"`
	OrganizerID   uuid.UUID `json:"organizer_id"`
	OrganizerName string    `json:"organizer_name"`
	AdminID       uuid.UUID `json:"admin_id"`
	AdminName     string    `json:"admin_name"`

	// Financial amounts (organizer earnings after commission)
	TotalRevenue      float64 `json:"total_revenue"`      // Total revenue from event
	TotalCommission   float64 `json:"total_commission"`   // Total commission deducted
	OrganizerEarnings float64 `json:"organizer_earnings"` // Amount owed to organizer
	BilledAmount      float64 `json:"billed_amount"`      // Amount included in this bill
	PaidAmount        float64 `json:"paid_amount"`        // Amount actually paid
	RemainingAmount   float64 `json:"remaining_amount"`   // Remaining amount to pay

	// Payment details
	PaymentMethod PaymentMethod `json:"payment_method"`
	PaymentRef    string        `json:"payment_ref"`
	Status        string        `json:"status"`
	BillType      string        `json:"bill_type"`
	Priority      string        `json:"priority"`
	DueDate       *time.Time    `json:"due_date"`

	// Additional info
	Notes     string     `json:"notes"`
	BillDate  time.Time  `json:"bill_date"`
	PaidDate  *time.Time `json:"paid_date"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// PaymentHistoryResponse represents payment history in API responses
type PaymentHistoryResponse struct {
	ID            uint          `json:"id"`
	Amount        float64       `json:"amount"`
	PaymentMethod PaymentMethod `json:"payment_method"`
	PaymentRef    string        `json:"payment_ref"`
	PaymentDate   time.Time     `json:"payment_date"`
	ProcessedBy   string        `json:"processed_by"`
	Notes         string        `json:"notes"`
	CreatedAt     time.Time     `json:"created_at"`
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
	ID                uuid.UUID      `json:"id"`
	EventID           uuid.UUID      `json:"event_id"`
	EventTitle        string         `json:"event_title"`
	UserID            *uuid.UUID     `json:"user_id,omitempty"`
	UserName          *string        `json:"user_name,omitempty"`
	GuestUserID       *uuid.UUID     `json:"guest_user_id,omitempty"`
	GuestUserName     *string        `json:"guest_user_name,omitempty"`
	TicketCount       int            `json:"ticket_count"`
	PaymentGateway    PaymentGateway `json:"payment_gateway"`
	Amount            float64        `json:"amount"`
	Currency          string         `json:"currency"`
	Status            string         `json:"status"`
	GatewayTxnID      string         `json:"gateway_txn_id"`
	CommissionRate    float64        `json:"commission_rate"`
	CommissionAmount  float64        `json:"commission_amount"`
	OrganizerShare    float64        `json:"organizer_share"`
	ProcessedAt       *time.Time     `json:"processed_at"`
	CreatedAt         time.Time      `json:"created_at"`
	HasPaymentDetails bool           `json:"has_payment_details"` // Whether payment intent details are available
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
		ID:                pb.ID,
		BillNumber:        pb.BillNumber,
		EventID:           pb.EventID,
		EventTitle:        eventTitle,
		OrganizerID:       pb.OrganizerID,
		OrganizerName:     organizerName,
		AdminID:           pb.AdminID,
		AdminName:         adminName,
		TotalRevenue:      pb.TotalRevenue,
		TotalCommission:   pb.TotalCommission,
		OrganizerEarnings: pb.OrganizerEarnings,
		BilledAmount:      pb.BilledAmount,
		PaidAmount:        pb.PaidAmount,
		RemainingAmount:   pb.RemainingAmount,
		PaymentMethod:     pb.PaymentMethod,
		PaymentRef:        pb.PaymentRef,
		Status:            pb.Status,
		BillType:          pb.BillType,
		Priority:          pb.Priority,
		DueDate:           pb.DueDate,
		Notes:             pb.Notes,
		BillDate:          pb.BillDate,
		PaidDate:          pb.PaidDate,
		CreatedAt:         pb.CreatedAt,
		UpdatedAt:         pb.UpdatedAt,
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
		EventID:          t.EventID,
		EventTitle:       eventTitle,
		UserID:           t.UserID,
		UserName:         userName,
		GuestUserID:      t.GuestUserID,
		GuestUserName:    guestUserName,
		TicketCount:      t.Quantity, // Use Quantity field instead of len(TicketIDs)
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
