package models

import (
	"event-ticketing-backend/pkg/currency"
	"strings"
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
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`

	BillNumber string `gorm:"uniqueIndex;size:50;not null" json:"bill_number"`

	// Relations
	EventID *uuid.UUID `gorm:"type:uuid;index" json:"event_id,omitempty"`
	Event   *Event     `gorm:"foreignKey:EventID" json:"event,omitempty"`

	OrganizerID uuid.UUID `gorm:"type:uuid;not null;index" json:"organizer_id"`
	Organizer   *User     `gorm:"foreignKey:OrganizerID" json:"organizer,omitempty"`

	CreatedByID uuid.UUID `gorm:"type:uuid;not null" json:"created_by_id"`
	CreatedBy   *User     `gorm:"foreignKey:CreatedByID" json:"created_by,omitempty"`

	// payout | refund | adjustment
	BillType BillType `gorm:"size:30;not null;index" json:"bill_type"`

	// pending | partially_paid | paid | cancelled
	Status PaymentBillStatus `gorm:"size:30;not null;default:'pending';index" json:"status"`

	// Financials
	Currency string  `gorm:"size:10;not null" json:"currency"`
	Amount   float64 `gorm:"not null" json:"amount"`

	PaidAmount float64 `gorm:"not null;default:0" json:"paid_amount"`

	// Optional
	DueDate *time.Time `json:"due_date"`
	Notes   string     `gorm:"type:text" json:"notes"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName specifies the table name for PaymentBill
func (PaymentBill) TableName() string {
	return "payment_bills"
}

// PaymentHistory tracks individual payments made against bills
type PaymentHistory struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`

	PaymentBillID uuid.UUID    `gorm:"type:uuid;not null;index" json:"payment_bill_id"`
	PaymentBill   *PaymentBill `gorm:"foreignKey:PaymentBillID" json:"payment_bill,omitempty"`

	ProcessedByID uuid.UUID `gorm:"type:uuid;not null" json:"processed_by_id"`
	ProcessedBy   *User     `gorm:"foreignKey:ProcessedByID" json:"processed_by,omitempty"`

	Amount float64 `gorm:"not null" json:"amount"`

	// bank_transfer | cash | khalti | stripe | esewa
	Method string `gorm:"size:30;not null" json:"method"`

	Reference string `gorm:"size:100" json:"reference"`

	ScreenshotURL string `gorm:"size:500" json:"screenshot_url"`

	Notes string `gorm:"type:text" json:"notes"`

	PaidAt time.Time `json:"paid_at"`

	CreatedAt time.Time `json:"created_at"`
}

// Transaction represents the SINGLE FINANCIAL TRUTH for all ticket purchases
// 🌍 GATEWAY-NEUTRAL DESIGN:
// - Works with ANY provider (Stripe, PayPal, eSewa, Khalti, Razorpay, etc.)
// - Provider field is UNIVERSAL abstraction
// - NO schema change needed when adding new gateways
// 💰 MONEY RULES:
// - Transaction is the ONLY place with financial calculations
// - PaymentIntent tracks orchestration (no money data)
// - PaymentAttempt tracks gateway interaction (no money data)
// - All financial queries use Transaction table
// - Commission, fees, and splits calculated here ONCE

type Transaction struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`

	PaymentIntentID  uuid.UUID `gorm:"not null;index"`
	PaymentAttemptID uuid.UUID `gorm:"not null;index"`

	EventID uuid.UUID `gorm:"not null;index"`
	Event   *Event    `gorm:"foreignKey:EventID"`

	ActorID   uuid.UUID `gorm:"not null;index"`
	ActorType ActorType `gorm:"not null"`

	User      *User      `gorm:"foreignKey:ActorID;references:ID" json:"user,omitempty"`       // Populated if ActorType is user
	GuestUser *GuestUser `gorm:"foreignKey:ActorID;references:ID" json:"guest_user,omitempty"` // Populated if ActorType is guest

	ProviderChargeID string         `gorm:"not null;index"`
	PaymentGateway   PaymentGateway `gorm:"not null"`

	AmountTotal int64  `gorm:"not null"`
	Currency    string `gorm:"not null"`

	PlatformFee      int64 `gorm:"not null"`
	GatewayFee       int64 `gorm:"not null"`
	OrganizerEarning int64 `gorm:"not null" json:"organizer_share"` // Total amount after fees, before commission

	Quantity int `gorm:"not null"`

	Status TransactionStatus `gorm:"not null;index"`

	IsPaidOut bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Request/Response models

// CreatePaymentBillRequest is the request body for POST /api/v1/admin/payments/bills.
//
// This creates a payment bill with auto-calculated amount from completed transactions.
// The bill will be created with default priority 'normal' and can be updated later
// for additional details like due date, payment reference, and notes.
type CreatePaymentBillRequest struct {
	// EventID is the UUID of the event this bill is for. Required.
	// example: fa50c770-6a8c-4f50-a9fc-84c9dce21fe9
	EventID uuid.UUID `binding:"required" example:"fa50c770-6a8c-4f50-a9fc-84c9dce21fe9"`

	// OrganizerID is the UUID of the organizer receiving the payout. Required.
	// example: dcf2dda4-a490-4898-a402-d301567c2cf6
	OrganizerID uuid.UUID `binding:"required" example:"dcf2dda4-a490-4898-a402-d301567c2cf6"`

	// Type of bill: payout | refund | adjustment
	Type BillType `binding:"omitempty,oneof=payout refund adjustment" example:"payout"`

	// Currency for the bill amount (defaults to event currency)
	Currency string `binding:"omitempty,len=3" example:"NPR"`

	// Optional manual amount override in display units
	Amount *float64 `binding:"omitempty,gte=0" example:"1000.50"`

	// Optional note
	Notes string `binding:"omitempty,max=2000" example:"Generated from admin approval flow"`
}

// UpdatePaymentBillRequest is the request body for PUT /api/v1/admin/payments/bills/{bill_id}.
//
// Use payment_amount to record a (partial) payment — the service will automatically
// increment paid_amount, decrement remaining_amount, and set the status to
// partially_paid or paid as appropriate. Alternatively send only status for a
// status-only transition (e.g. mark as cancelled or overdue).
type UpdatePaymentBillRequest struct {
	// Status to transition the bill to.
	// Allowed values: pending, partially_paid, paid, cancelled, overdue
	// example: partially_paid
	Status string `json:"status" binding:"required,oneof=pending partially_paid paid cancelled overdue" example:"partially_paid"`

	// PaymentRef is the external reference for the payment being recorded (optional).
	// example: BANK-TXN-98765
	PaymentRef string `json:"payment_ref,omitempty" example:"BANK-TXN-98765"`

	// PaymentAmount is the amount being paid in this update. Must be > 0 and ≤
	// remaining_amount. The bill status is derived automatically:
	//   remaining == 0  → paid
	//   remaining > 0   → partially_paid
	// Omit (or set to 0) for a status-only update.
	// example: 135
	PaymentAmount *float64 `json:"payment_amount,omitempty" example:"135"`

	// Notes are optional free-text remarks for this update.
	// example: Partial payment received via SWIFT
	Notes string `json:"notes,omitempty" example:"Partial payment received via SWIFT"`
}

// AddPaymentRequest is the request body for POST /api/v1/admin/payments/bills/{bill_id}/payments.
//
// Records a standalone payment entry against a bill and updates paid_amount /
// remaining_amount / status on the parent PaymentBill automatically.
type AddPaymentRequest struct {
	// Amount being paid. Must be > 0 and ≤ remaining_amount on the bill.
	// example: 270
	Amount float64 `json:"amount" binding:"required,gt=0" example:"270"`

	// PaymentMethod is how this payment was made.
	// Allowed values: bank_transfer, check, cash, mobile_payment, other
	// example: bank_transfer
	Method string `json:"method" binding:"required" example:"bank_transfer"`

	// PaymentRef is the external reference for this payment (optional).
	// example: SWIFT-20260220-001
	Reference string `json:"reference,omitempty" example:"SWIFT-20260220-001"`

	// PaymentDate overrides the timestamp of the payment. Defaults to now if omitted.
	// example: 2026-02-20T10:00:00Z
	PaidAt *time.Time `json:"paid_at,omitempty" example:"2026-02-20T10:00:00Z"`

	// Notes are optional remarks for this payment entry.
	// example: Bank transfer confirmed
	Notes string `json:"notes,omitempty" example:"Bank transfer confirmed"`
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
	ID            uuid.UUID               `json:"id"`
	BillNumber    string                  `json:"bill_number"`
	EventID       *uuid.UUID              `json:"event_id,omitempty"`
	EventTitle    string                  `json:"event_title"`
	Event         PaymentBillSummaryEvent `json:"event"`
	OrganizerID   uuid.UUID               `json:"organizer_id"`
	OrganizerName string                  `json:"organizer_name"`
	CreatedByID   uuid.UUID               `json:"created_by_id"`
	CreatedByName string                  `json:"created_by_name"`
	BillType      BillType                `json:"bill_type"`
	Status        PaymentBillStatus       `json:"status"`
	Currency      string                  `json:"currency"`
	Amount        float64                 `json:"amount"`
	PaidAmount    float64                 `json:"paid_amount"`
	Remaining     float64                 `json:"remaining_amount"`
	DueDate       *time.Time              `json:"due_date"`
	Notes         string                  `json:"notes"`
	CreatedAt     time.Time               `json:"created_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
}

// PaymentBillSummaryResponse represents simplified payment bill data for listings
type PaymentBillSummaryResponse struct {
	ID         uuid.UUID                   `json:"id"`
	Event      PaymentBillSummaryEvent     `json:"event"`
	Organizer  PaymentBillSummaryOrganizer `json:"organizer"`
	BillType   BillType                    `json:"bill_type"`
	Status     PaymentBillStatus           `json:"status"`
	Currency   string                      `json:"currency"`
	Amount     float64                     `json:"amount"`
	PaidAmount float64                     `json:"paid_amount"`
	Remaining  float64                     `json:"remaining_amount"`
	CreatedAt  time.Time                   `json:"created_at"`
	UpdatedAt  time.Time                   `json:"updated_at"`
}

// PaymentBillSummaryEvent represents event info in simplified bill response
type PaymentBillSummaryEvent struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	Currency string    `json:"currency,omitempty"`
	Country  string    `json:"country,omitempty"`
	Symbol   string    `json:"symbol,omitempty"`
}

// PaymentBillSummaryOrganizer represents organizer info in simplified bill response
type PaymentBillSummaryOrganizer struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// PaymentHistoryResponse represents payment history in API responses
type PaymentHistoryResponse struct {
	ID            uuid.UUID               `json:"id"`
	Event         PaymentBillSummaryEvent `json:"event"`
	Amount        float64                 `json:"amount"`
	Method        string                  `json:"method"`
	Reference     string                  `json:"reference"`
	PaidAt        time.Time               `json:"paid_at"`
	ProcessedBy   string                  `json:"processed_by"`
	Notes         string                  `json:"notes"`
	ScreenshotURL string                  `json:"screenshot_url"`
	CreatedAt     time.Time               `json:"created_at"`
}

// ToResponse converts PaymentHistory to PaymentHistoryResponse
func (ph *PaymentHistory) ToResponse() PaymentHistoryResponse {
	processedBy := ""
	event := PaymentBillSummaryEvent{}
	displayAmount := ph.Amount
	if ph.ProcessedBy != nil {
		processedBy = ph.ProcessedBy.FirstName + " " + ph.ProcessedBy.LastName
		if processedBy == " " {
			processedBy = ph.ProcessedBy.Email
		}
	}
	if ph.PaymentBill != nil && ph.PaymentBill.Event != nil {
		event = PaymentBillSummaryEvent{
			ID:       ph.PaymentBill.Event.ID,
			Title:    ph.PaymentBill.Event.Title,
			Currency: ph.PaymentBill.Event.Currency,
			Country:  ph.PaymentBill.Event.Country,
		}
		if cfg, err := currency.Get(event.Currency); err == nil {
			event.Symbol = cfg.Symbol
		}
	}
	if ph.PaymentBill != nil && ph.PaymentBill.Currency != "" {
		if v, err := currency.FromSmallestUnit(int64(ph.Amount), ph.PaymentBill.Currency); err == nil {
			displayAmount = v
		}
	}

	return PaymentHistoryResponse{
		ID:            ph.ID,
		Event:         event,
		Amount:        displayAmount,
		Method:        string(ph.Method),
		Reference:     ph.Reference,
		PaidAt:        ph.PaidAt,
		ProcessedBy:   processedBy,
		Notes:         ph.Notes,
		ScreenshotURL: ph.ScreenshotURL,
		CreatedAt:     ph.CreatedAt,
	}
}

// BillPaymentSummary represents payment summary for a bill
type BillPaymentSummary struct {
	TotalBilled     float64    `json:"total_billed"`                // Total amount in the bill
	TotalPaid       float64    `json:"total_paid"`                  // Total amount paid so far
	RemainingAmount float64    `json:"remaining_amount"`            // Amount still remaining to pay
	PendingAmount   float64    `json:"pending_amount"`              // Amount pending (same as remaining for active bills)
	PaymentCount    int        `json:"payment_count"`               // Number of payments made
	LastPaymentDate *time.Time `json:"last_payment_date,omitempty"` // Date of last payment
}

// DashboardSummary represents overall dashboard summary for payout requests
type DashboardSummary struct {
	TotalBilled    float64 `json:"total_billed"`    // Sum of all bill amounts
	TotalPaid      float64 `json:"total_paid"`      // Sum of all paid amounts
	TotalRemaining float64 `json:"total_remaining"` // Sum of all remaining amounts
	TotalPending   float64 `json:"total_pending"`   // Sum of all pending amounts
	BillCount      int     `json:"bill_count"`      // Number of bills
	PaymentCount   int     `json:"payment_count"`   // Total number of payments
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

// TransactionEventInfo is the event sub-object in transaction responses
type TransactionEventInfo struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

// TransactionUserInfo is the user sub-object in transaction responses
type TransactionUserInfo struct {
	ID    *uuid.UUID `json:"id,omitempty"`
	Name  string     `json:"name"`  // registered user full name or guest name
	Email string     `json:"email"` // registered user email or guest email
}

// TransactionScanRow is used for raw SQL scan — flat structure, converted to nested response
type TransactionScanRow struct {
	ID                uuid.UUID      `gorm:"column:id"`
	EventID           uuid.UUID      `gorm:"column:event_id"`
	EventTitle        string         `gorm:"column:event_title"`
	ActorID           *uuid.UUID     `gorm:"column:actor_id"`
	ActorType         ActorType      `gorm:"column:actor_type"`
	UserName          *string        `gorm:"column:user_name"`
	UserEmail         *string        `gorm:"column:user_email"`
	GuestUserName     *string        `gorm:"column:guest_user_name"`
	GuestUserEmail    *string        `gorm:"column:guest_user_email"`
	Quantity          int            `gorm:"column:quantity"`
	PaymentGateway    PaymentGateway `gorm:"column:payment_gateway"`
	Amount            int64          `gorm:"column:amount_total"`
	Currency          string         `gorm:"column:currency"`
	Status            string         `gorm:"column:status"`
	GatewayTxnID      string         `gorm:"column:provider_charge_id"`
	CommissionRate    float64        `gorm:"column:commission_rate"`
	PlatformFee       int64          `gorm:"column:platform_fee"`
	GatewayFee        int64          `gorm:"column:gateway_fee"`
	OrganizerShare    int64          `gorm:"column:organizer_share"`
	CreatedAt         time.Time      `gorm:"column:created_at"`
	UpdatedAt         time.Time      `gorm:"column:updated_at"`
	HasPaymentDetails bool           `gorm:"column:has_payment_details"`
}

// ToSummaryResponse converts a flat scan row to the nested summary response
// ToSummaryResponse converts scan row to API response
func (r TransactionScanRow) ToSummaryResponse() TransactionSummaryResponse {

	user := TransactionUserInfo{
		ID: r.ActorID,
	}

	// Resolve actor info based on actor type
	switch r.ActorType {

	case ActorUser:
		if r.UserName != nil {
			user.Name = *r.UserName
		}

		if r.UserEmail != nil {
			user.Email = *r.UserEmail
		}

	case ActorGuest:
		if r.GuestUserName != nil {
			user.Name = *r.GuestUserName
		}

		if r.GuestUserEmail != nil {
			user.Email = *r.GuestUserEmail
		}
	}

	// Convert amounts from smallest units
	amount, _ := currency.FromSmallestUnit(
		r.Amount,
		r.Currency,
	)

	platformFee, _ := currency.FromSmallestUnit(
		r.PlatformFee,
		r.Currency,
	)

	organizerEarning, _ := currency.FromSmallestUnit(
		r.OrganizerShare,
		r.Currency,
	)

	gatewayFee, _ := currency.FromSmallestUnit(
		r.GatewayFee,
		r.Currency,
	)

	return TransactionSummaryResponse{
		ID: r.ID,

		Event: TransactionEventInfo{
			ID:    r.EventID,
			Title: r.EventTitle,
		},

		User: user,

		Quantity:       r.Quantity,
		PaymentGateway: r.PaymentGateway,
		Currency:       r.Currency,
		Status:         TransactionStatus(r.Status),

		Amount:           amount,
		CommissionRate:   r.CommissionRate,
		CommissionAmount: platformFee,
		GatewayFee:       gatewayFee,
		OrganizerShare:   organizerEarning,

		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,

		HasPaymentDetails: r.HasPaymentDetails,
		GatewayTxnID:      r.GatewayTxnID,
	}
}

// TransactionSummaryResponse is the nested response for the transaction listing API
type TransactionSummaryResponse struct {
	ID                uuid.UUID            `json:"id"`
	Event             TransactionEventInfo `json:"event"`
	User              TransactionUserInfo  `json:"user"`
	Quantity          int                  `json:"quantity"`
	PaymentGateway    PaymentGateway       `json:"payment_gateway"`
	Currency          string               `json:"currency"`
	Status            TransactionStatus    `json:"status"`
	Amount            float64              `json:"amount"`
	CommissionRate    float64              `json:"commission_rate"`
	CommissionAmount  float64              `json:"commission_amount"`
	GatewayFee        float64              `json:"gateway_fee"`
	OrganizerShare    float64              `json:"organizer_share"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
	HasPaymentDetails bool                 `json:"has_payment_details"`
	GatewayTxnID      string               `json:"gateway_txn_id"`
}

// TransactionResponse is kept as an alias so existing usages still compile
type TransactionResponse = TransactionSummaryResponse

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
	createdByName := ""
	event := PaymentBillSummaryEvent{}
	if pb.EventID != nil {
		event.ID = *pb.EventID
	}

	if pb.Event != nil {
		eventTitle = pb.Event.Title
		event.Title = pb.Event.Title
		event.Currency = pb.Event.Currency
		event.Country = pb.Event.Country
		if cfg, err := currency.Get(pb.Event.Currency); err == nil {
			event.Symbol = cfg.Symbol
		}
	}
	if pb.Organizer != nil {
		if pb.Organizer.OrganizerOnboarding != nil && strings.TrimSpace(pb.Organizer.OrganizerOnboarding.BusinessName) != "" {
			organizerName = strings.TrimSpace(pb.Organizer.OrganizerOnboarding.BusinessName)
		} else {
			organizerName = strings.TrimSpace(pb.Organizer.FirstName + " " + pb.Organizer.LastName)
		}
		// Email fallback when first/last name and business name are all empty
		if organizerName == "" && pb.Organizer.Email != "" {
			organizerName = pb.Organizer.Email
		}
	}
	if pb.CreatedBy != nil {
		createdByName = strings.TrimSpace(pb.CreatedBy.FirstName + " " + pb.CreatedBy.LastName)
		if createdByName == "" {
			createdByName = pb.CreatedBy.Email
		}
	}

	amountDisplay := pb.Amount
	paidDisplay := pb.PaidAmount
	remainingDisplay := pb.Amount - pb.PaidAmount
	if remainingDisplay < 0 {
		remainingDisplay = 0
	}
	if pb.Currency != "" {
		if v, err := currency.FromSmallestUnit(int64(pb.Amount), pb.Currency); err == nil {
			amountDisplay = v
		}
		if v, err := currency.FromSmallestUnit(int64(pb.PaidAmount), pb.Currency); err == nil {
			paidDisplay = v
		}
		if v, err := currency.FromSmallestUnit(int64(remainingDisplay), pb.Currency); err == nil {
			remainingDisplay = v
		}
	}

	return PaymentBillResponse{
		ID:            pb.ID,
		BillNumber:    pb.BillNumber,
		EventID:       pb.EventID,
		EventTitle:    eventTitle,
		Event:         event,
		OrganizerID:   pb.OrganizerID,
		OrganizerName: organizerName,
		CreatedByID:   pb.CreatedByID,
		CreatedByName: createdByName,
		BillType:      pb.BillType,
		Status:        pb.Status,
		Currency:      pb.Currency,
		Amount:        amountDisplay,
		PaidAmount:    paidDisplay,
		Remaining:     remainingDisplay,
		DueDate:       pb.DueDate,
		Notes:         pb.Notes,
		CreatedAt:     pb.CreatedAt,
		UpdatedAt:     pb.UpdatedAt,
	}
}

func (pb *PaymentBill) ToSummaryResponse() PaymentBillSummaryResponse {
	event := PaymentBillSummaryEvent{}
	if pb.EventID != nil {
		event.ID = *pb.EventID
	}
	if pb.Event != nil {
		event.Title = pb.Event.Title
		event.Currency = pb.Event.Currency
		event.Country = pb.Event.Country
		if cfg, err := currency.Get(pb.Event.Currency); err == nil {
			event.Symbol = cfg.Symbol
		}
	}

	organizer := PaymentBillSummaryOrganizer{
		ID:   pb.OrganizerID,
		Name: "",
	}
	if pb.Organizer != nil {
		// Priority: Business Name > Personal Name (as account name)
		if pb.Organizer.OrganizerOnboarding != nil && strings.TrimSpace(pb.Organizer.OrganizerOnboarding.BusinessName) != "" {
			organizer.Name = strings.TrimSpace(pb.Organizer.OrganizerOnboarding.BusinessName)
		} else {
			// Use personal name as account name
			organizer.Name = strings.TrimSpace(pb.Organizer.FirstName + " " + pb.Organizer.LastName)
		}
		// If still empty, try individual name parts
		if organizer.Name == "" {
			if pb.Organizer.FirstName != "" {
				organizer.Name = pb.Organizer.FirstName
			}
			if pb.Organizer.LastName != "" {
				if organizer.Name != "" {
					organizer.Name += " " + pb.Organizer.LastName
				} else {
					organizer.Name = pb.Organizer.LastName
				}
			}
		}
		// Final fallback to email
		if organizer.Name == "" && pb.Organizer.Email != "" {
			organizer.Name = pb.Organizer.Email
		}
	}

	amountDisplay := pb.Amount
	paidDisplay := pb.PaidAmount
	remainingDisplay := pb.Amount - pb.PaidAmount
	if remainingDisplay < 0 {
		remainingDisplay = 0
	}
	if pb.Currency != "" {
		if v, err := currency.FromSmallestUnit(int64(pb.Amount), pb.Currency); err == nil {
			amountDisplay = v
		}
		if v, err := currency.FromSmallestUnit(int64(pb.PaidAmount), pb.Currency); err == nil {
			paidDisplay = v
		}
		if v, err := currency.FromSmallestUnit(int64(remainingDisplay), pb.Currency); err == nil {
			remainingDisplay = v
		}
	}

	return PaymentBillSummaryResponse{
		ID:         pb.ID,
		Event:      event,
		Organizer:  organizer,
		BillType:   pb.BillType,
		Status:     pb.Status,
		Currency:   pb.Currency,
		Amount:     amountDisplay,
		PaidAmount: paidDisplay,
		Remaining:  remainingDisplay,
		CreatedAt:  pb.CreatedAt,
		UpdatedAt:  pb.UpdatedAt,
	}
}

// UserTransactionListingResponse represents transaction data for user transaction listing API
type UserTransactionListingResponse struct {
	ID              uuid.UUID                 `json:"id"`
	Event           UserTransactionEventInfo  `json:"event"`
	Tiers           []UserTransactionTierInfo `json:"tiers"`
	Price           float64                   `json:"price"`
	Status          string                    `json:"status"`
	Date            time.Time                 `json:"date"`
	PaymentMethod   string                    `json:"payment_method"`
	PaymentIntentID string                    `json:"payment_intent_id,omitempty"`
	TransactionRef  string                    `json:"transaction_ref,omitempty"`
	User            UserTransactionUserInfo   `json:"user"`
}

// UserTransactionEventInfo represents event information in user transaction listing
type UserTransactionEventInfo struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	BannerImage string    `json:"banner_image"`
}

// UserTransactionTierInfo represents tier information in user transaction listing
type UserTransactionTierInfo struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Quantity int       `json:"quantity"`
	Price    float64   `json:"price"`
}

// UserTransactionUserInfo represents user information in user transaction listing
type UserTransactionUserInfo struct {
	ID                 *uuid.UUID `json:"id,omitempty"`
	Name               string     `json:"name,omitempty"`
	ProcessedBy        *string    `json:"processed_by,omitempty"`
	TransactionDetails string     `json:"transaction_details"`
}

// UserTransactionFilters represents filters for user transaction listing
type UserTransactionFilters struct {
	PaymentMethod string     `json:"payment_method,omitempty"`
	DateFrom      *time.Time `json:"date_from,omitempty"`
	DateTo        *time.Time `json:"date_to,omitempty"`
	Search        string     `json:"search,omitempty"` // Search by event title (partial match, case-insensitive)
}

// UserTransactionInvoiceInfo represents invoice data for user transaction listing
type UserTransactionInvoiceInfo struct {
	CompanyName    string    `json:"company_name"`
	CompanyAddress string    `json:"company_address"`
	CompanyPhone   string    `json:"company_phone"`
	CompanyEmail   string    `json:"company_email"`
	TaxNumber      string    `json:"tax_number"`
	InvoiceNumber  string    `json:"invoice_number"`
	TransactionRef string    `json:"transaction_ref"`
	PaymentGateway string    `json:"payment_gateway"`
	Currency       string    `json:"currency"`
	Subtotal       float64   `json:"subtotal"`
	TaxAmount      float64   `json:"tax_amount"`
	TotalAmount    float64   `json:"total_amount"`
	IssueDate      time.Time `json:"issue_date"`
	// Item breakdown
	Items []UserTransactionInvoiceItem `json:"items"`
}

// UserTransactionInvoiceItem represents an item in the invoice
type UserTransactionInvoiceItem struct {
	EventTitle string  `json:"event_title"`
	TierName   string  `json:"tier_name"`
	Quantity   int     `json:"quantity"`
	UnitPrice  float64 `json:"unit_price"`
	TotalPrice float64 `json:"total_price"`
}

// RefundResponse represents refund data in API responses
type RefundResponse struct {
	ID              uuid.UUID  `json:"id"`
	TransactionID   uuid.UUID  `json:"transaction_id"`
	EventTitle      string     `json:"event_title"`
	RefundAmount    float64    `json:"refund_amount"`
	Currency        string     `json:"currency"`
	Status          string     `json:"status"`
	Reason          string     `json:"reason"`
	AdminNotes      string     `json:"admin_notes,omitempty"`
	ProcessedBy     string     `json:"processed_by,omitempty"`
	ProcessedAt     *time.Time `json:"processed_at,omitempty"`
	GatewayRefundID string     `json:"gateway_refund_id,omitempty"`
	RefundMethod    string     `json:"refund_method"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// RefundListResponse represents refund data for listing APIs with nested objects
type RefundListResponse struct {
	ID            uuid.UUID           `json:"id"`
	RefundNumber  string              `json:"refund_number"`
	TransactionID uuid.UUID           `json:"transaction_id"`
	InitiatedBy   *RefundUserInfo     `json:"initiated_by,omitempty"`
	Amount        float64             `json:"amount"`
	Currency      string              `json:"currency"`
	Reason        string              `json:"reason"`
	RefundType    RefundInitiatorType `json:"refund_type"`
	Status        string              `json:"status"`
	TicketCount   int                 `json:"ticket_count"`
	RequestedAt   *time.Time          `json:"requested_at"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

// RefundDetailResponse represents detailed refund data for single refund API
type RefundDetailResponse struct {
	ID                uuid.UUID             `json:"id"`
	RefundNumber      string                `json:"refund_number"`
	Transaction       RefundTransactionInfo `json:"transaction"`
	Event             *RefundEventInfo      `json:"event,omitempty"`
	Organizer         *RefundOrganizerInfo  `json:"organizer,omitempty"`
	InitiatedBy       *RefundUserInfo       `json:"initiated_by,omitempty"`
	Amount            float64               `json:"amount"`
	Currency          string                `json:"currency"`
	Reason            string                `json:"reason"`
	RefundType        RefundInitiatorType   `json:"refund_type"`
	Status            string                `json:"status"`
	AffectedTicketIDs []string              `json:"affected_ticket_ids"`
	TicketCount       int                   `json:"ticket_count"`
	RequestedAt       *time.Time            `json:"requested_at"`
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
}

// RefundTransactionInfo represents transaction info in refund responses
type RefundTransactionInfo struct {
	ID        uuid.UUID `json:"id"`
	Amount    float64   `json:"amount"`
	Gateway   string    `json:"gateway"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// RefundUserInfo represents user info in refund responses
type RefundUserInfo struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

// RefundEventInfo represents event info in refund responses
type RefundEventInfo struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	BannerImage string    `json:"banner_image"`
}

// RefundOrganizerInfo represents organizer info in refund responses
type RefundOrganizerInfo struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// UserTransactionDetailResponse represents detailed transaction data for user APIs (without sensitive financial data)
type UserTransactionDetailResponse struct {
	ID             uuid.UUID                         `json:"id"`
	Event          UserTransactionEventInfo          `json:"event"`
	User           UserTransactionUserDetailInfo     `json:"user"`
	TicketCount    int                               `json:"ticket_count"`
	PaymentGateway string                            `json:"payment_gateway"`
	Amount         float64                           `json:"amount"`
	Currency       string                            `json:"currency"`
	Status         string                            `json:"status"`
	CreatedAt      time.Time                         `json:"created_at"`
	UpdatedAt      time.Time                         `json:"updated_at"`
	ProcessedAt    *time.Time                        `json:"processed_at"`
	InvoiceInfo    *UserTransactionInvoiceDetailInfo `json:"invoice_info,omitempty"`
}

// UserTransactionUserDetailInfo represents user information in transaction details
type UserTransactionUserDetailInfo struct {
	ID    *uuid.UUID `json:"id,omitempty"`
	Name  string     `json:"name"`
	Email string     `json:"email"`
}

// UserTransactionInvoiceDetailInfo represents enhanced invoice data for user transaction details
type UserTransactionInvoiceDetailInfo struct {
	Organizer     UserTransactionOrganizerDetailInfo `json:"organizer"`
	Company       UserTransactionCompanyDetailInfo   `json:"company"`
	InvoiceNumber string                             `json:"invoice_number"`
	Total         float64                            `json:"total"`
	Subtotal      float64                            `json:"subtotal"`
	Tax           float64                            `json:"tax"`
	Discount      float64                            `json:"discount"`
	Items         []UserTransactionInvoiceItemDetail `json:"items"`
}

// UserTransactionOrganizerDetailInfo represents organizer information in invoice
type UserTransactionOrganizerDetailInfo struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Logo string    `json:"logo"`
}

// UserTransactionCompanyDetailInfo represents company information in invoice
type UserTransactionCompanyDetailInfo struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	Address   string    `json:"address"`
	TaxNumber string    `json:"tax_number"`
	Logo      string    `json:"logo"`
}

// UserTransactionInvoiceItemDetail represents an enhanced item in the invoice
type UserTransactionInvoiceItemDetail struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Quantity   int       `json:"quantity"`
	UnitPrice  float64   `json:"unit_price"`
	TotalPrice float64   `json:"total_price"`
}

// GetAuditLogsRequest represents the request for querying audit logs
type GetAuditLogsRequest struct {
	Page       int       `json:"page" form:"page" binding:"min=1"`
	Limit      int       `json:"limit" form:"limit" binding:"min=1,max=100"`
	Action     string    `json:"action" form:"action"`
	EntityType string    `json:"entity_type" form:"entity_type"`
	EntityID   uuid.UUID `json:"entity_id" form:"entity_id"`
	ActorID    uuid.UUID `json:"actor_id" form:"actor_id"`
	ActorType  string    `json:"actor_type" form:"actor_type"`
	EventID    uuid.UUID `json:"event_id" form:"event_id"`
	StartDate  time.Time `json:"start_date" form:"start_date" time_format:"2006-01-02"`
	EndDate    time.Time `json:"end_date" form:"end_date" time_format:"2006-01-02"`
	SortBy     string    `json:"sort_by" form:"sort_by"`
	SortOrder  string    `json:"sort_order" form:"sort_order"`
}

// GetAuditLogsResponse represents the response for audit logs query
type GetAuditLogsResponse struct {
	Logs       []MinimalAuditLog  `json:"logs"`
	Pagination PaginationResponse `json:"pagination"`
}

// MinimalAuditLog represents a simplified audit log entry for API responses
type MinimalAuditLog struct {
	ID         uuid.UUID     `json:"id"`
	Action     string        `json:"action"`
	EntityType string        `json:"entity_type"`
	EntityID   uuid.UUID     `json:"entity_id"`
	Actor      *MinimalUser  `json:"actor,omitempty"`
	Event      *MinimalEvent `json:"event,omitempty"`
	Timestamp  time.Time     `json:"timestamp"`
	CreatedAt  time.Time     `json:"created_at"`
}

// MinimalUser represents minimal user information for audit logs
type MinimalUser struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

// MinimalEvent represents minimal event information for audit logs
type MinimalEvent struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	BannerImage string    `json:"banner_image"`
	OrganizerID uuid.UUID `json:"organizer_id"`
}

// PaginationResponse represents pagination metadata
type PaginationResponse struct {
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	TotalPages int64 `json:"total_pages"`
}

// TransactionPaymentDetailsUserSummary represents minimal user details for payment detail views.
type TransactionPaymentDetailsUserSummary struct {
	ID    uuid.UUID `json:"id,omitempty"`
	Name  string    `json:"name"`
	Email string    `json:"email,omitempty"`
	Phone string    `json:"phone,omitempty"`
}

// TransactionPaymentDetailsEventSummary represents minimal event details.
type TransactionPaymentDetailsEventSummary struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Banner string    `json:"banner,omitempty"`
}

// TransactionPaymentDetailsTransactionSummary represents minimal transaction details.
type TransactionPaymentDetailsTransactionSummary struct {
	ID             uuid.UUID                             `json:"id"`
	Event          TransactionPaymentDetailsEventSummary `json:"event"`
	User           TransactionPaymentDetailsUserSummary  `json:"user"`
	PaymentGateway string                                `json:"payment_gateway"`
	Amount         float64                               `json:"amount"`
	Currency       string                                `json:"currency"`
	Quantity       int                                   `json:"quantity"`
	Status         string                                `json:"status"`
	CreatedAt      time.Time                             `json:"created_at"`
	UpdatedAt      time.Time                             `json:"updated_at"`
}

// TransactionPaymentDetailsTicketUserSummary represents minimal ticket owner details.
type TransactionPaymentDetailsTicketUserSummary struct {
	ID    uuid.UUID `json:"id,omitempty"`
	Name  string    `json:"name"`
	Email string    `json:"email,omitempty"`
}

// TransactionPaymentDetailsTicketTierSummary represents minimal tier details.
type TransactionPaymentDetailsTicketTierSummary struct {
	ID       uuid.UUID `json:"id"`
	TierName string    `json:"tier_name"`
}

// TransactionPaymentDetailsTicketSummary represents minimal ticket details.
type TransactionPaymentDetailsTicketSummary struct {
	ID              uuid.UUID                                  `json:"id"`
	TicketNumber    string                                     `json:"ticket_number"`
	User            TransactionPaymentDetailsTicketUserSummary `json:"user"`
	Tier            TransactionPaymentDetailsTicketTierSummary `json:"tier"`
	IsGuestPurchase bool                                       `json:"is_guest_purchase"`
	Currency        string                                     `json:"currency"`
	TotalAmount     float64                                    `json:"total_amount"`
	Status          string                                     `json:"status"`
	CreatedAt       time.Time                                  `json:"created_at"`
	UpdatedAt       time.Time                                  `json:"updated_at"`
}

// TransactionPaymentIntentSummary represents safe payment intent details for UI display.
// Note: only masked card information is exposed; full card numbers are never returned.
type TransactionPaymentIntentSummary struct {
	ID               uuid.UUID `json:"id"`
	Status           string    `json:"status"`
	PaymentGateway   string    `json:"payment_gateway"`
	PaymentMethod    string    `json:"payment_method,omitempty"`
	CardBrand        string    `json:"card_brand,omitempty"`
	CardLast4        string    `json:"card_last4,omitempty"`
	MaskedCardNumber string    `json:"masked_card_number,omitempty"`
	ExpMonth         int       `json:"exp_month,omitempty"`
	ExpYear          int       `json:"exp_year,omitempty"`
	CustomerEmail    string    `json:"customer_email,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// TransactionPaymentDetailsResponse represents minimal payment details response for admin UI.
type TransactionPaymentDetailsResponse struct {
	Transaction   TransactionPaymentDetailsTransactionSummary `json:"transaction"`
	PaymentIntent *TransactionPaymentIntentSummary            `json:"payment_intent"`
	Tickets       []TransactionPaymentDetailsTicketSummary    `json:"tickets"`
}
