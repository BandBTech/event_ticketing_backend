package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// PaymentAttempt - UNIVERSAL GATEWAY ABSTRACTION LAYER
// 🌍 This is the KEY to multi-gateway future-proofing
// Every attempt (regardless of provider) uses this structure
// NO database migration needed when adding new gateways
// SECURITY: This table NEVER stores sensitive card data (no CVV, full card numbers, PINs)
type PaymentAttempt struct {
	ID uuid.UUID

	PaymentIntentID uuid.UUID

	Provider            string
	ProviderReferenceID string

	Amount   int64
	Currency string

	Status string

	PaymentMethodType string

	ProviderData JSONMap

	FailureReason string

	CreatedAt time.Time
	UpdatedAt time.Time
}

type PaymentStatus string
type TicketStatus string

const (
	PaymentPending      PaymentStatus = "pending"
	PaymentProcessing   PaymentStatus = "processing"
	PaymentSucceeded    PaymentStatus = "succeeded"
	PaymentFailed       PaymentStatus = "failed"
	PaymentCanceled     PaymentStatus = "cancelled"
	PaymentExpired      PaymentStatus = "expired"
	PaymentRefunded     PaymentStatus = "refunded"
	TicketReserved      TicketStatus  = "reserved"
	TicketConfirmed     TicketStatus  = "confirmed"
	TicketPendingRefund TicketStatus  = "pending_refund"
	TicketUsed          TicketStatus  = "used"
	TicketActive        TicketStatus  = "active"
	TicketCancelled     TicketStatus  = "cancelled"
)

// PaymentIntent - NOW PURE ORCHESTRATION LAYER
// 🧠 Orchestrates payment flow, NOT gateway-specific
// All gateway details moved to PaymentAttempt
// SECURITY: This table NEVER stores sensitive card data (no CVV, full card numbers, PINs)
type PaymentIntent struct {
	ID uuid.UUID

	ActorID   uuid.UUID
	ActorType string // user | guest

	EventID uuid.UUID
	TierID  uuid.UUID

	Quantity int

	AmountTotal int64
	Currency    string

	Status PaymentStatus

	SessionID      string // for gateways that use sessions (e.g. Stripe)
	CheckoutToken  string
	IdempotencyKey string
	PaymentGateway PaymentGateway

	// Metadata for flexibility (stores any additional info needed by gateways or for auditing)
	GatewayMetadata JSONMap

	// Timestamps for tracking payment lifecycle
	ExpiresAt   *time.Time
	SucceededAt *time.Time
	CanceledAt  *time.Time
	FailedAt    *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Refund - CLEAN & GENERIC (works with ANY provider)
// 🌍 MULTI-GATEWAY READY
// NO provider-specific logic, just universal fields
type Refund struct {
	ID uuid.UUID

	TransactionID   uuid.UUID
	PaymentIntentID uuid.UUID

	EventID uuid.UUID

	ActorID   uuid.UUID // who requested refund
	ActorType string    // user | admin | system

	ApprovedByID   *uuid.UUID
	ApprovedByType string // admin | system

	Provider         string
	ProviderRefundID string

	Amount   int64
	Currency string

	Reason string

	Status PaymentStatus // requested, approved, rejected, processed

	IsFullRefund bool

	CreatedAt time.Time
}

// WebhookEvent logs all webhook events from payment gateways for debugging and replay
type WebhookEvent struct {
	ID uuid.UUID

	Provider  string
	EventID   string
	EventType string

	Status string // pending, processed, failed

	Payload JSONMap
	Headers JSONMap

	PaymentIntentID *uuid.UUID
	TransactionID   *uuid.UUID
	RefundID        *uuid.UUID

	ReceivedAt  time.Time
	ProcessedAt *time.Time

	CreatedAt time.Time
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
	OldStatus PaymentStatus `gorm:"size:50" json:"old_status,omitempty"`
	NewStatus PaymentStatus `gorm:"not null;size:50" json:"new_status"`

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
	OldStatus     PaymentStatus          `json:"old_status,omitempty"`
	NewStatus     PaymentStatus          `json:"new_status"`
	ChangedByID   *uuid.UUID             `json:"changed_by_id,omitempty"`
	ChangedBy     *UserSummary           `json:"changed_by,omitempty"`
	ChangedByType PaymentStatus          `json:"changed_by_type"`
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
