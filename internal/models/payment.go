package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PaymentIntent represents a gateway-agnostic payment intent with full lifecycle management
// SECURITY: This table NEVER stores sensitive card data (no CVV, full card numbers, PINs)
type PaymentIntent struct {
	ID uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`

	// Gateway Integration (Gateway-Agnostic)
	PaymentGateway   string  `gorm:"not null;size:50;index" json:"payment_gateway"` // stripe, paypal, esewa, khalti, etc.
	IdempotencyKey   string  `gorm:"unique;not null;size:255" json:"idempotency_key"`
	GatewayPaymentID *string `gorm:"size:255;index" json:"gateway_payment_id,omitempty"`    // Stripe PI ID, PayPal transaction ID, etc. (nil for cash)
	GatewayChargeID  *string `gorm:"size:255;index" json:"gateway_charge_id,omitempty"`     // Stripe Charge ID (ch_xxx) - needed for refunds
	CheckoutToken    string  `gorm:"unique;size:255;index" json:"checkout_token,omitempty"` // For fallback verification endpoints

	// Customer Info
	UserID        *uuid.UUID `gorm:"type:uuid;index" json:"user_id,omitempty"`
	User          *User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
	GuestUserID   *uuid.UUID `gorm:"type:uuid;index" json:"guest_user_id,omitempty"`
	GuestUser     *GuestUser `gorm:"foreignKey:GuestUserID" json:"guest_user,omitempty"`
	CustomerEmail string     `gorm:"not null;size:255" json:"customer_email"`
	CustomerName  string     `gorm:"size:255" json:"customer_name,omitempty"`
	CustomerPhone string     `gorm:"size:50" json:"customer_phone,omitempty"`

	// Event & Pricing
	EventID  uuid.UUID  `gorm:"type:uuid;not null;index" json:"event_id"`
	Event    *Event     `gorm:"foreignKey:EventID" json:"event,omitempty"`
	TierID   uuid.UUID  `gorm:"type:uuid;not null;index" json:"tier_id"`
	Tier     *EventTier `gorm:"foreignKey:TierID" json:"tier,omitempty"`
	Quantity int        `gorm:"not null" json:"quantity"`

	// Multi-Currency Support
	Currency           string  `gorm:"not null;size:3;index" json:"currency"` // USD, EUR, GBP, NPR, INR
	CurrencySymbol     string  `gorm:"size:10" json:"currency_symbol,omitempty"`
	ExchangeRate       float64 `gorm:"type:decimal(10,6);default:1.000000" json:"exchange_rate"`
	BaseCurrency       string  `gorm:"size:3;default:'USD'" json:"base_currency"`
	BaseCurrencyAmount float64 `gorm:"type:decimal(10,2)" json:"base_currency_amount,omitempty"`

	// Pricing Breakdown
	UnitPrice   float64 `gorm:"type:decimal(10,2);not null" json:"unit_price"`
	Subtotal    float64 `gorm:"type:decimal(10,2);not null" json:"subtotal"`
	PlatformFee float64 `gorm:"type:decimal(10,2);not null" json:"platform_fee"`
	GatewayFee  float64 `gorm:"type:decimal(10,2);default:0" json:"gateway_fee"`
	TaxAmount   float64 `gorm:"type:decimal(10,2);default:0" json:"tax_amount"`
	TotalAmount float64 `gorm:"type:decimal(10,2);not null" json:"total_amount"`

	// Status Management
	Status string `gorm:"not null;default:'pending';size:50;index" json:"status"`
	// pending, processing, requires_action, succeeded, failed, canceled, refunded, partially_refunded

	// Financial Tracking
	CommissionRate     float64 `gorm:"type:decimal(5,2);not null" json:"commission_rate"`
	CommissionAmount   float64 `gorm:"type:decimal(10,2);not null" json:"commission_amount"`
	OrganizerNetAmount float64 `gorm:"type:decimal(10,2);not null" json:"organizer_net_amount"`

	// Gateway-Specific Data (NON-SENSITIVE METADATA ONLY)
	PaymentMethodType    string                 `gorm:"size:50" json:"payment_method_type"`                       // card, wallet, bank_transfer, upi
	PaymentMethodDetails map[string]interface{} `gorm:"type:jsonb;serializer:json" json:"payment_method_details"` // {"brand":"visa","type":"credit","last4":"4242"}
	GatewayResponse      map[string]interface{} `gorm:"type:jsonb;serializer:json" json:"gateway_response"`
	GatewayMetadata      map[string]interface{} `gorm:"type:jsonb;serializer:json" json:"gateway_metadata"`
	CaptureMethod        string                 `gorm:"size:20;default:'automatic'" json:"capture_method"`

	// Region & Localization
	CountryCode string `gorm:"size:10" json:"country_code"` // +977, +1, +44, etc (with + prefix)
	Locale      string `gorm:"size:10" json:"locale"`       // en-US, ne-NP

	// Timestamps
	SucceededAt *time.Time     `json:"succeeded_at,omitempty"`
	FailedAt    *time.Time     `json:"failed_at,omitempty"`
	CanceledAt  *time.Time     `json:"canceled_at,omitempty"`
	ExpiresAt   *time.Time     `json:"expires_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// Refund represents a refund operation (gateway-agnostic)
type Refund struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	RefundNumber string    `gorm:"unique;not null;size:50" json:"refund_number"`

	// Links
	TransactionID   uuid.UUID      `gorm:"type:uuid;not null;index" json:"transaction_id"`
	Transaction     *Transaction   `gorm:"foreignKey:TransactionID" json:"transaction,omitempty"`
	PaymentIntentID uuid.UUID      `gorm:"type:uuid;not null;index" json:"payment_intent_id"`
	PaymentIntent   *PaymentIntent `gorm:"foreignKey:PaymentIntentID" json:"payment_intent,omitempty"`

	// Gateway Integration
	PaymentGateway  string `gorm:"not null;size:50;index" json:"payment_gateway"`
	GatewayRefundID string `gorm:"not null;size:255;index" json:"gateway_refund_id"`

	// Refund Details
	Amount             float64 `gorm:"type:decimal(10,2);not null" json:"amount"`
	Currency           string  `gorm:"not null;size:3;default:'USD'" json:"currency"`
	ExchangeRate       float64 `gorm:"type:decimal(10,6);default:1.000000" json:"exchange_rate"`
	BaseCurrency       string  `gorm:"size:3;default:'USD'" json:"base_currency"`
	BaseCurrencyAmount float64 `gorm:"type:decimal(10,2)" json:"base_currency_amount,omitempty"`

	Reason     string `gorm:"not null;size:255" json:"reason"`
	RefundType string `gorm:"not null;size:50" json:"refund_type"` // full, partial, event_cancellation, customer_request, etc.

	// Status
	Status string `gorm:"not null;default:'pending';size:50;index" json:"status"`

	// Ticket Impact
	AffectedTicketIDs       []string `gorm:"type:jsonb;serializer:json" json:"affected_ticket_ids"`
	TicketCount             int      `gorm:"not null" json:"ticket_count"`
	IsFullTransactionRefund bool     `gorm:"default:false" json:"is_full_transaction_refund"`

	// Financial Impact
	CommissionRefund float64 `gorm:"type:decimal(10,2)" json:"commission_refund,omitempty"`
	OrganizerRefund  float64 `gorm:"type:decimal(10,2)" json:"organizer_refund,omitempty"`
	GatewayFeeRefund float64 `gorm:"type:decimal(10,2)" json:"gateway_fee_refund,omitempty"`

	// Admin Control
	InitiatedBy     *uuid.UUID `gorm:"type:uuid" json:"initiated_by,omitempty"`
	Initiator       *User      `gorm:"foreignKey:InitiatedBy" json:"initiator,omitempty"`
	ApprovedBy      *uuid.UUID `gorm:"type:uuid" json:"approved_by,omitempty"`
	Approver        *User      `gorm:"foreignKey:ApprovedBy" json:"approver,omitempty"`
	RejectionReason string     `gorm:"type:text" json:"rejection_reason,omitempty"`
	FailureReason   string     `gorm:"type:text" json:"failure_reason,omitempty"` // User-friendly reason for refund failure

	// Gateway Data
	GatewayResponse map[string]interface{} `gorm:"type:jsonb;serializer:json" json:"gateway_response,omitempty"`
	GatewayMetadata map[string]interface{} `gorm:"type:jsonb;serializer:json" json:"gateway_metadata,omitempty"`
	Metadata        map[string]interface{} `gorm:"type:jsonb;serializer:json" json:"metadata,omitempty"`
	Notes           string                 `gorm:"type:text" json:"notes,omitempty"`

	// Timestamps
	RequestedAt *time.Time     `json:"requested_at"`
	ApprovedAt  *time.Time     `json:"approved_at,omitempty"`
	ProcessedAt *time.Time     `json:"processed_at,omitempty"`
	FailedAt    *time.Time     `json:"failed_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// WebhookEvent logs all webhook events from payment gateways for debugging and replay
type WebhookEvent struct {
	ID             uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	PaymentGateway string    `gorm:"not null;size:50;index" json:"payment_gateway"`
	GatewayEventID string    `gorm:"not null;size:255;index" json:"gateway_event_id"`
	EventType      string    `gorm:"not null;size:100;index" json:"event_type"`
	APIVersion     string    `gorm:"size:50" json:"api_version,omitempty"`

	// Processing Status
	Status         string `gorm:"not null;default:'pending';size:50;index" json:"status"`
	ProcessedCount int    `gorm:"default:0" json:"processed_count"`
	LastError      string `gorm:"type:text" json:"last_error,omitempty"`

	// Related Records
	PaymentIntentID *uuid.UUID     `gorm:"type:uuid" json:"payment_intent_id,omitempty"`
	PaymentIntent   *PaymentIntent `gorm:"foreignKey:PaymentIntentID" json:"payment_intent,omitempty"`
	TransactionID   *uuid.UUID     `gorm:"type:uuid" json:"transaction_id,omitempty"`
	Transaction     *Transaction   `gorm:"foreignKey:TransactionID" json:"transaction,omitempty"`
	RefundID        *uuid.UUID     `gorm:"type:uuid" json:"refund_id,omitempty"`
	Refund          *Refund        `gorm:"foreignKey:RefundID" json:"refund,omitempty"`

	// Raw Data (for replay)
	Payload map[string]interface{} `gorm:"type:jsonb;serializer:json;not null" json:"payload"`
	Headers map[string]interface{} `gorm:"type:jsonb;serializer:json" json:"headers,omitempty"`

	// Timestamps
	ReceivedAt  time.Time      `gorm:"not null;index" json:"received_at"`
	ProcessedAt *time.Time     `json:"processed_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// Invoice represents generated invoices for transactions
type Invoice struct {
	ID            uuid.UUID    `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	InvoiceNumber string       `gorm:"unique;not null;size:50" json:"invoice_number"`
	TransactionID uuid.UUID    `gorm:"type:uuid;not null;unique;index" json:"transaction_id"`
	Transaction   *Transaction `gorm:"foreignKey:TransactionID" json:"transaction,omitempty"`

	// Customer Info
	CustomerName  string `gorm:"not null;size:255" json:"customer_name"`
	CustomerEmail string `gorm:"not null;size:255" json:"customer_email"`
	CustomerPhone string `gorm:"size:50" json:"customer_phone,omitempty"`

	// Invoice Details
	Amount      float64 `gorm:"type:decimal(10,2);not null" json:"amount"`
	Currency    string  `gorm:"not null;size:3" json:"currency"`
	TaxAmount   float64 `gorm:"type:decimal(10,2);default:0" json:"tax_amount"`
	TotalAmount float64 `gorm:"type:decimal(10,2);not null" json:"total_amount"`

	// File Storage
	FileURL    string `gorm:"type:text" json:"file_url"` // S3/cloud storage URL
	FileKey    string `gorm:"type:text" json:"file_key"` // S3 key
	ReceiptURL string `gorm:"type:text" json:"receipt_url,omitempty"`

	// Status
	Status   string     `gorm:"not null;default:'generated';size:50" json:"status"` // generated, sent, viewed
	SentAt   *time.Time `json:"sent_at,omitempty"`
	ViewedAt *time.Time `json:"viewed_at,omitempty"`

	// Metadata
	Metadata map[string]interface{} `gorm:"type:jsonb;serializer:json" json:"metadata,omitempty"`

	// Timestamps
	IssuedAt  time.Time      `gorm:"not null" json:"issued_at"`
	DueDate   *time.Time     `json:"due_date,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// PaymentAuditLog tracks all financial operations for compliance and debugging
type PaymentAuditLog struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Action     string    `gorm:"not null;size:100;index" json:"action"`     // payment_created, refund_issued, etc.
	EntityType string    `gorm:"not null;size:50;index" json:"entity_type"` // payment_intent, transaction, refund
	EntityID   uuid.UUID `gorm:"type:uuid;not null;index" json:"entity_id"`

	// Actor Info
	ActorID   *uuid.UUID `gorm:"type:uuid;index" json:"actor_id,omitempty"` // Who performed the action
	Actor     *User      `gorm:"foreignKey:ActorID" json:"actor,omitempty"`
	ActorType string     `gorm:"size:50" json:"actor_type"` // user, admin, system, webhook

	// Event Details
	EventID *uuid.UUID `gorm:"type:uuid" json:"event_id,omitempty"`
	Event   *Event     `gorm:"foreignKey:EventID" json:"event,omitempty"`

	// Changes
	ChangesBefore JSONMap `gorm:"type:jsonb" json:"changes_before,omitempty"`
	ChangesAfter  JSONMap `gorm:"type:jsonb" json:"changes_after,omitempty"`

	// Context
	IPAddress string  `gorm:"size:45" json:"ip_address,omitempty"`
	UserAgent string  `gorm:"type:text" json:"user_agent,omitempty"`
	Metadata  JSONMap `gorm:"type:jsonb" json:"metadata,omitempty"`

	// Timestamps
	Timestamp time.Time `gorm:"not null;index" json:"timestamp"`
	CreatedAt time.Time `json:"created_at"`
}

// RefundStatusHistory tracks all status changes for refunds (similar to EventStatusHistory)
type RefundStatusHistory struct {
	ID       uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	RefundID uuid.UUID `gorm:"type:uuid;not null;index" json:"refund_id"`
	Refund   *Refund   `gorm:"foreignKey:RefundID" json:"refund,omitempty"`

	// Status Change
	OldStatus string `gorm:"size:50" json:"old_status,omitempty"`
	NewStatus string `gorm:"not null;size:50" json:"new_status"`

	// Actor Info
	ChangedByID   *uuid.UUID `gorm:"type:uuid;index" json:"changed_by_id,omitempty"`
	ChangedBy     *User      `gorm:"foreignKey:ChangedByID" json:"changed_by,omitempty"`
	ChangedByType string     `gorm:"size:50;default:'system'" json:"changed_by_type"` // user, admin, system, webhook

	// Context
	Remarks  string                 `gorm:"type:text" json:"remarks,omitempty"`
	Metadata map[string]interface{} `gorm:"type:jsonb;serializer:json" json:"metadata,omitempty"`

	// Timestamps
	ChangedAt time.Time `gorm:"not null;index" json:"changed_at"`
	CreatedAt time.Time `json:"created_at"`
}

// RefundStatusHistoryResponse is the API response format for refund status history
type RefundStatusHistoryResponse struct {
	ID            uuid.UUID              `json:"id"`
	RefundID      uuid.UUID              `json:"refund_id"`
	OldStatus     string                 `json:"old_status,omitempty"`
	NewStatus     string                 `json:"new_status"`
	ChangedByID   *uuid.UUID             `json:"changed_by_id,omitempty"`
	ChangedBy     *UserSummary           `json:"changed_by,omitempty"`
	ChangedByType string                 `json:"changed_by_type"`
	Remarks       string                 `json:"remarks,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	ChangedAt     time.Time              `json:"changed_at"`
}

// UserSummary is a simplified user representation for API responses
type UserSummary struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

// Table names
func (PaymentIntent) TableName() string       { return "payment_intents" }
func (Refund) TableName() string              { return "refunds" }
func (RefundStatusHistory) TableName() string { return "refund_status_history" }
func (WebhookEvent) TableName() string        { return "webhook_events" }
func (Invoice) TableName() string             { return "invoices" }
func (PaymentAuditLog) TableName() string     { return "payment_audit_logs" }

// JSONMap is a custom type for JSONB fields that implements sql.Scanner and driver.Valuer
type JSONMap map[string]interface{}

// Value implements the driver.Valuer interface
func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// Scan implements the sql.Scanner interface
func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}

	var m map[string]interface{}
	if err := json.Unmarshal(bytes, &m); err != nil {
		return err
	}

	*j = JSONMap(m)
	return nil
}

// MarshalJSON implements json.Marshaler
func (j JSONMap) MarshalJSON() ([]byte, error) {
	if j == nil {
		return []byte("null"), nil
	}
	return json.Marshal(map[string]interface{}(j))
}

// UnmarshalJSON implements json.Unmarshaler
func (j *JSONMap) UnmarshalJSON(data []byte) error {
	if j == nil {
		return errors.New("JSONMap: UnmarshalJSON on nil pointer")
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	*j = JSONMap(m)
	return nil
}
