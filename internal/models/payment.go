package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PaymentStatus represents the lifecycle status of a payment transaction
type PaymentStatus string

const (
	PaymentStatusPending   PaymentStatus = "PENDING"   // User started payment but not finished
	PaymentStatusSuccess   PaymentStatus = "SUCCESS"   // Payment successful, money captured
	PaymentStatusFailed    PaymentStatus = "FAILED"    // Payment failed (declined, insufficient funds, etc.)
	PaymentStatusCancelled PaymentStatus = "CANCELLED" // User intentionally cancelled
	PaymentStatusExpired   PaymentStatus = "EXPIRED"   // Payment expired/abandoned
)

// String returns the string representation of PaymentStatus
func (ps PaymentStatus) String() string {
	return string(ps)
}

// IsValid checks if the payment status is valid
func (ps PaymentStatus) IsValid() bool {
	switch ps {
	case PaymentStatusPending, PaymentStatusSuccess, PaymentStatusFailed, PaymentStatusCancelled, PaymentStatusExpired:
		return true
	default:
		return false
	}
}

// IsRevenueGenerating returns true if this status should count towards revenue
func (ps PaymentStatus) IsRevenueGenerating() bool {
	return ps == PaymentStatusSuccess
}

// IsFinal returns true if this status represents a terminal state
func (ps PaymentStatus) IsFinal() bool {
	switch ps {
	case PaymentStatusSuccess, PaymentStatusFailed, PaymentStatusCancelled, PaymentStatusExpired:
		return true
	default:
		return false
	}
}

// RefundStatus represents the refund state of a payment
type RefundStatus string

const (
	RefundStatusNone    RefundStatus = "NONE"    // No refund
	RefundStatusPartial RefundStatus = "PARTIAL" // Partially refunded
	RefundStatusFull    RefundStatus = "FULL"    // Fully refunded
)

// String returns the string representation of RefundStatus
func (rs RefundStatus) String() string {
	return string(rs)
}

// IsValid checks if the refund status is valid
func (rs RefundStatus) IsValid() bool {
	switch rs {
	case RefundStatusNone, RefundStatusPartial, RefundStatusFull:
		return true
	default:
		return false
	}
}

// Value implements the driver.Valuer interface for GORM
func (ps PaymentStatus) Value() (driver.Value, error) {
	return string(ps), nil
}

// Scan implements the sql.Scanner interface for GORM
func (ps *PaymentStatus) Scan(value interface{}) error {
	if value == nil {
		*ps = PaymentStatusPending
		return nil
	}

	var str string
	switch s := value.(type) {
	case string:
		str = s
	case []byte:
		str = string(s)
	default:
		return errors.New("invalid scan value for PaymentStatus")
	}

	// Convert to uppercase for case-insensitive matching
	*ps = PaymentStatus(strings.ToUpper(str))

	if !ps.IsValid() {
		return errors.New("invalid PaymentStatus value")
	}

	return nil
}

// MarshalJSON implements json.Marshaler
func (ps PaymentStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(ps))
}

// UnmarshalJSON implements json.Unmarshaler with case-insensitive parsing for backward compatibility
func (ps *PaymentStatus) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Convert to uppercase for case-insensitive matching
	upper := strings.ToUpper(s)
	*ps = PaymentStatus(upper)

	if !ps.IsValid() {
		return errors.New("invalid PaymentStatus value")
	}

	return nil
}

// Value implements the driver.Valuer interface for GORM
func (rs RefundStatus) Value() (driver.Value, error) {
	return string(rs), nil
}

// Scan implements the sql.Scanner interface for GORM
func (rs *RefundStatus) Scan(value interface{}) error {
	if value == nil {
		*rs = RefundStatusNone
		return nil
	}

	var str string
	switch s := value.(type) {
	case string:
		str = s
	case []byte:
		str = string(s)
	default:
		return errors.New("invalid scan value for RefundStatus")
	}

	// Convert to uppercase for case-insensitive matching
	*rs = RefundStatus(strings.ToUpper(str))

	if !rs.IsValid() {
		return errors.New("invalid RefundStatus value")
	}

	return nil
}

// MarshalJSON implements json.Marshaler
func (rs RefundStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(rs))
}

// UnmarshalJSON implements json.Unmarshaler
func (rs *RefundStatus) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	*rs = RefundStatus(s)
	if !rs.IsValid() {
		return errors.New("invalid RefundStatus value")
	}

	return nil
}

// EventStatus represents the status of an event
type EventStatus string

const (
	EventStatusDraft         EventStatus = "DRAFT"
	EventStatusPending       EventStatus = "PENDING"
	EventStatusApproved      EventStatus = "APPROVED"
	EventStatusOnSale        EventStatus = "ON_SALE"
	EventStatusLive          EventStatus = "LIVE"
	EventStatusCompleted     EventStatus = "COMPLETED"
	EventStatusScheduled     EventStatus = "SCHEDULED"
	EventStatusHold          EventStatus = "HOLD"
	EventStatusHeld          EventStatus = "HELD"
	EventStatusRejected      EventStatus = "REJECTED"
	EventStatusCancelled     EventStatus = "CANCELLED"
	EventStatusSalesEnd      EventStatus = "SALES_END"
	EventStatusSalesUpcoming EventStatus = "SALES_UPCOMING"
)

// String returns the string representation of EventStatus
func (es EventStatus) String() string {
	return string(es)
}

// IsValid checks if the event status is valid
func (es EventStatus) IsValid() bool {
	switch es {
	case EventStatusDraft, EventStatusPending, EventStatusApproved, EventStatusOnSale,
		EventStatusLive, EventStatusCompleted, EventStatusScheduled, EventStatusHold,
		EventStatusHeld, EventStatusRejected, EventStatusCancelled, EventStatusSalesEnd,
		EventStatusSalesUpcoming:
		return true
	default:
		return false
	}
}

// Value implements the driver.Valuer interface for GORM
func (es EventStatus) Value() (driver.Value, error) {
	return string(es), nil
}

// Scan implements the sql.Scanner interface for GORM
func (es *EventStatus) Scan(value interface{}) error {
	if value == nil {
		*es = EventStatusDraft
		return nil
	}

	var str string
	switch s := value.(type) {
	case string:
		str = s
	case []byte:
		str = string(s)
	default:
		return errors.New("invalid scan value for EventStatus")
	}

	// Convert to uppercase for case-insensitive matching
	*es = EventStatus(strings.ToUpper(str))

	if !es.IsValid() {
		return errors.New("invalid EventStatus value")
	}

	return nil
}

// MarshalJSON implements json.Marshaler
func (es EventStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(es))
}

// UnmarshalJSON implements json.Unmarshaler with case-insensitive parsing for backward compatibility
func (es *EventStatus) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Convert to uppercase for case-insensitive matching
	upper := strings.ToUpper(s)
	*es = EventStatus(upper)

	if !es.IsValid() {
		return errors.New("invalid EventStatus value")
	}

	return nil
}

// SalesStatus represents the sales status of an event
type SalesStatus string

const (
	SalesStatusActive  SalesStatus = "ACTIVE"
	SalesStatusPaused  SalesStatus = "PAUSED"
	SalesStatusStopped SalesStatus = "STOPPED"
)

// String returns the string representation of SalesStatus
func (ss SalesStatus) String() string {
	return string(ss)
}

// IsValid checks if the sales status is valid
func (ss SalesStatus) IsValid() bool {
	switch ss {
	case SalesStatusActive, SalesStatusPaused, SalesStatusStopped:
		return true
	default:
		return false
	}
}

// Value implements the driver.Valuer interface for GORM
func (ss SalesStatus) Value() (driver.Value, error) {
	return string(ss), nil
}

// Scan implements the sql.Scanner interface for GORM
func (ss *SalesStatus) Scan(value interface{}) error {
	if value == nil {
		*ss = SalesStatusActive
		return nil
	}

	var str string
	switch s := value.(type) {
	case string:
		str = s
	case []byte:
		str = string(s)
	default:
		return errors.New("invalid scan value for SalesStatus")
	}

	// Convert to uppercase for case-insensitive matching
	*ss = SalesStatus(strings.ToUpper(str))

	if !ss.IsValid() {
		return errors.New("invalid SalesStatus value")
	}

	return nil
}

// MarshalJSON implements json.Marshaler
func (ss SalesStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(ss))
}

// UnmarshalJSON implements json.Unmarshaler with case-insensitive parsing for backward compatibility
func (ss *SalesStatus) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Convert to uppercase for case-insensitive matching
	upper := strings.ToUpper(s)
	*ss = SalesStatus(upper)

	if !ss.IsValid() {
		return errors.New("invalid SalesStatus value")
	}

	return nil
}

// UserAccountStatus represents the account status of a user
type UserAccountStatus string

const (
	UserAccountStatusActive    UserAccountStatus = "ACTIVE"
	UserAccountStatusInactive  UserAccountStatus = "INACTIVE"
	UserAccountStatusSuspended UserAccountStatus = "SUSPENDED"
)

// String returns the string representation of UserAccountStatus
func (uas UserAccountStatus) String() string {
	return string(uas)
}

// IsValid checks if the user account status is valid
func (uas UserAccountStatus) IsValid() bool {
	switch uas {
	case UserAccountStatusActive, UserAccountStatusInactive, UserAccountStatusSuspended:
		return true
	default:
		return false
	}
}

// Value implements the driver.Valuer interface for GORM
func (uas UserAccountStatus) Value() (driver.Value, error) {
	return string(uas), nil
}

// Scan implements the sql.Scanner interface for GORM
func (uas *UserAccountStatus) Scan(value interface{}) error {
	if value == nil {
		*uas = UserAccountStatusActive
		return nil
	}

	var str string
	switch s := value.(type) {
	case string:
		str = s
	case []byte:
		str = string(s)
	default:
		return errors.New("invalid scan value for UserAccountStatus")
	}

	// Convert to uppercase for case-insensitive matching
	*uas = UserAccountStatus(strings.ToUpper(str))

	if !uas.IsValid() {
		return errors.New("invalid UserAccountStatus value")
	}

	return nil
}

// MarshalJSON implements json.Marshaler
func (uas UserAccountStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(uas))
}

// UnmarshalJSON implements json.Unmarshaler with case-insensitive parsing for backward compatibility
func (uas *UserAccountStatus) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Convert to uppercase for case-insensitive matching
	upper := strings.ToUpper(s)
	*uas = UserAccountStatus(upper)

	if !uas.IsValid() {
		return errors.New("invalid UserAccountStatus value")
	}

	return nil
}

// OrganizerStatus represents the organizer status of a user
type OrganizerStatus string

const (
	OrganizerStatusInactive OrganizerStatus = "INACTIVE"
	OrganizerStatusPending  OrganizerStatus = "PENDING"
	OrganizerStatusApproved OrganizerStatus = "APPROVED"
	OrganizerStatusRejected OrganizerStatus = "REJECTED"
)

// String returns the string representation of OrganizerStatus
func (os OrganizerStatus) String() string {
	return string(os)
}

// IsValid checks if the organizer status is valid
func (os OrganizerStatus) IsValid() bool {
	switch os {
	case OrganizerStatusInactive, OrganizerStatusPending, OrganizerStatusApproved, OrganizerStatusRejected:
		return true
	default:
		return false
	}
}

// Value implements the driver.Valuer interface for GORM
func (os OrganizerStatus) Value() (driver.Value, error) {
	return string(os), nil
}

// Scan implements the sql.Scanner interface for GORM
func (os *OrganizerStatus) Scan(value interface{}) error {
	if value == nil {
		*os = OrganizerStatusInactive
		return nil
	}

	var str string
	switch s := value.(type) {
	case string:
		str = s
	case []byte:
		str = string(s)
	default:
		return errors.New("invalid scan value for OrganizerStatus")
	}

	// Convert to uppercase for case-insensitive matching
	*os = OrganizerStatus(strings.ToUpper(str))

	if !os.IsValid() {
		return errors.New("invalid OrganizerStatus value")
	}

	return nil
}

// MarshalJSON implements json.Marshaler
func (os OrganizerStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(os))
}

// UnmarshalJSON implements json.Unmarshaler with case-insensitive parsing for backward compatibility
func (os *OrganizerStatus) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Convert to uppercase for case-insensitive matching
	upper := strings.ToUpper(s)
	*os = OrganizerStatus(upper)

	if !os.IsValid() {
		return errors.New("invalid OrganizerStatus value")
	}

	return nil
}

// PayoutStatus represents the status of a payout request
type PayoutStatus string

const (
	PayoutStatusPending   PayoutStatus = "PENDING"
	PayoutStatusApproved  PayoutStatus = "APPROVED"
	PayoutStatusRejected  PayoutStatus = "REJECTED"
	PayoutStatusCancelled PayoutStatus = "CANCELLED"
	PayoutStatusPaid      PayoutStatus = "PAID"
)

// String returns the string representation of PayoutStatus
func (ps PayoutStatus) String() string {
	return string(ps)
}

// IsValid checks if the payout status is valid
func (ps PayoutStatus) IsValid() bool {
	switch ps {
	case PayoutStatusPending, PayoutStatusApproved, PayoutStatusRejected, PayoutStatusCancelled, PayoutStatusPaid:
		return true
	default:
		return false
	}
}

// Value implements the driver.Valuer interface for GORM
func (ps PayoutStatus) Value() (driver.Value, error) {
	return string(ps), nil
}

// Scan implements the sql.Scanner interface for GORM
func (ps *PayoutStatus) Scan(value interface{}) error {
	if value == nil {
		*ps = PayoutStatusPending
		return nil
	}

	var str string
	switch s := value.(type) {
	case string:
		str = s
	case []byte:
		str = string(s)
	default:
		return errors.New("invalid scan value for PayoutStatus")
	}

	// Convert to uppercase for case-insensitive matching
	*ps = PayoutStatus(strings.ToUpper(str))

	if !ps.IsValid() {
		return errors.New("invalid PayoutStatus value")
	}

	return nil
}

// MarshalJSON implements json.Marshaler
func (ps PayoutStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(ps))
}

// UnmarshalJSON implements json.Unmarshaler with case-insensitive parsing for backward compatibility
func (ps *PayoutStatus) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Convert to uppercase for case-insensitive matching
	upper := strings.ToUpper(s)
	*ps = PayoutStatus(upper)

	if !ps.IsValid() {
		return errors.New("invalid PayoutStatus value")
	}

	return nil
}

// BillingStatus represents the status of a billing/payment bill
type BillingStatus string

const (
	BillingStatusPending       BillingStatus = "PENDING"
	BillingStatusPartiallyPaid BillingStatus = "PARTIALLY_PAID"
	BillingStatusPaid          BillingStatus = "PAID"
	BillingStatusCancelled     BillingStatus = "CANCELLED"
	BillingStatusOverdue       BillingStatus = "OVERDUE"
)

// String returns the string representation of BillingStatus
func (bs BillingStatus) String() string {
	return string(bs)
}

// IsValid checks if the billing status is valid
func (bs BillingStatus) IsValid() bool {
	switch bs {
	case BillingStatusPending, BillingStatusPartiallyPaid, BillingStatusPaid, BillingStatusCancelled, BillingStatusOverdue:
		return true
	default:
		return false
	}
}

// Value implements the driver.Valuer interface for GORM
func (bs BillingStatus) Value() (driver.Value, error) {
	return string(bs), nil
}

// Scan implements the sql.Scanner interface for GORM
func (bs *BillingStatus) Scan(value interface{}) error {
	if value == nil {
		*bs = BillingStatusPending
		return nil
	}

	var str string
	switch s := value.(type) {
	case string:
		str = s
	case []byte:
		str = string(s)
	default:
		return errors.New("invalid scan value for BillingStatus")
	}

	// Convert to uppercase for case-insensitive matching
	*bs = BillingStatus(strings.ToUpper(str))

	if !bs.IsValid() {
		return errors.New("invalid BillingStatus value")
	}

	return nil
}

// MarshalJSON implements json.Marshaler
func (bs BillingStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(bs))
}

// UnmarshalJSON implements json.Unmarshaler with case-insensitive parsing for backward compatibility
func (bs *BillingStatus) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Convert to uppercase for case-insensitive matching
	upper := strings.ToUpper(s)
	*bs = BillingStatus(upper)

	if !bs.IsValid() {
		return errors.New("invalid BillingStatus value")
	}

	return nil
}

// TicketStatus represents the status of a ticket
type TicketStatus string

const (
	TicketStatusActive              TicketStatus = "ACTIVE"
	TicketStatusPendingVerification TicketStatus = "PENDING_VERIFICATION"
	TicketStatusUsed                TicketStatus = "USED"
	TicketStatusCancelled           TicketStatus = "CANCELLED"
	TicketStatusRefunded            TicketStatus = "REFUNDED"
)

// String returns the string representation of TicketStatus
func (ts TicketStatus) String() string {
	return string(ts)
}

// IsValid checks if the ticket status is valid
func (ts TicketStatus) IsValid() bool {
	switch ts {
	case TicketStatusActive, TicketStatusPendingVerification, TicketStatusUsed, TicketStatusCancelled, TicketStatusRefunded:
		return true
	default:
		return false
	}
}

// Value implements the driver.Valuer interface for GORM
func (ts TicketStatus) Value() (driver.Value, error) {
	return string(ts), nil
}

// Scan implements the sql.Scanner interface for GORM
func (ts *TicketStatus) Scan(value interface{}) error {
	if value == nil {
		*ts = TicketStatusActive
		return nil
	}

	var str string
	switch s := value.(type) {
	case string:
		str = s
	case []byte:
		str = string(s)
	default:
		return errors.New("invalid scan value for TicketStatus")
	}

	// Convert to uppercase for case-insensitive matching
	*ts = TicketStatus(strings.ToUpper(str))

	if !ts.IsValid() {
		return errors.New("invalid TicketStatus value")
	}

	return nil
}

// MarshalJSON implements json.Marshaler
func (ts TicketStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(ts))
}

// UnmarshalJSON implements json.Unmarshaler with case-insensitive parsing for backward compatibility
func (ts *TicketStatus) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Convert to uppercase for case-insensitive matching
	upper := strings.ToUpper(s)
	*ts = TicketStatus(upper)

	if !ts.IsValid() {
		return errors.New("invalid TicketStatus value")
	}

	return nil
}

// TicketPaymentStatus represents the payment status of a ticket
type TicketPaymentStatus string

const (
	TicketPaymentStatusPending   TicketPaymentStatus = "PENDING"
	TicketPaymentStatusCompleted TicketPaymentStatus = "COMPLETED"
	TicketPaymentStatusFailed    TicketPaymentStatus = "FAILED"
	TicketPaymentStatusRefunded  TicketPaymentStatus = "REFUNDED"
)

// String returns the string representation of TicketPaymentStatus
func (tps TicketPaymentStatus) String() string {
	return string(tps)
}

// IsValid checks if the ticket payment status is valid
func (tps TicketPaymentStatus) IsValid() bool {
	switch tps {
	case TicketPaymentStatusPending, TicketPaymentStatusCompleted, TicketPaymentStatusFailed, TicketPaymentStatusRefunded:
		return true
	default:
		return false
	}
}

// Value implements the driver.Valuer interface for GORM
func (tps TicketPaymentStatus) Value() (driver.Value, error) {
	return string(tps), nil
}

// Scan implements the sql.Scanner interface for GORM
func (tps *TicketPaymentStatus) Scan(value interface{}) error {
	if value == nil {
		*tps = TicketPaymentStatusPending
		return nil
	}

	var str string
	switch s := value.(type) {
	case string:
		str = s
	case []byte:
		str = string(s)
	default:
		return errors.New("invalid scan value for TicketPaymentStatus")
	}

	// Convert to uppercase for case-insensitive matching
	*tps = TicketPaymentStatus(strings.ToUpper(str))

	if !tps.IsValid() {
		return errors.New("invalid TicketPaymentStatus value")
	}

	return nil
}

// MarshalJSON implements json.Marshaler
func (tps TicketPaymentStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(tps))
}

// UnmarshalJSON implements json.Unmarshaler with case-insensitive parsing for backward compatibility
func (tps *TicketPaymentStatus) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Convert to uppercase for case-insensitive matching
	upper := strings.ToUpper(s)
	*tps = TicketPaymentStatus(upper)

	if !tps.IsValid() {
		return errors.New("invalid TicketPaymentStatus value")
	}

	return nil
}

// ReservationStatus represents the status of a reservation
type ReservationStatus string

const (
	ReservationStatusReserved  ReservationStatus = "RESERVED"
	ReservationStatusConfirmed ReservationStatus = "CONFIRMED"
	ReservationStatusExpired   ReservationStatus = "EXPIRED"
	ReservationStatusCancelled ReservationStatus = "CANCELLED"
)

// String returns the string representation of ReservationStatus
func (rs ReservationStatus) String() string {
	return string(rs)
}

// IsValid checks if the reservation status is valid
func (rs ReservationStatus) IsValid() bool {
	switch rs {
	case ReservationStatusReserved, ReservationStatusConfirmed, ReservationStatusExpired, ReservationStatusCancelled:
		return true
	default:
		return false
	}
}

// Value implements the driver.Valuer interface for GORM
func (rs ReservationStatus) Value() (driver.Value, error) {
	return string(rs), nil
}

// Scan implements the sql.Scanner interface for GORM
func (rs *ReservationStatus) Scan(value interface{}) error {
	if value == nil {
		*rs = ReservationStatusReserved
		return nil
	}

	var str string
	switch s := value.(type) {
	case string:
		str = s
	case []byte:
		str = string(s)
	default:
		return errors.New("invalid scan value for ReservationStatus")
	}

	// Convert to uppercase for case-insensitive matching
	*rs = ReservationStatus(strings.ToUpper(str))

	if !rs.IsValid() {
		return errors.New("invalid ReservationStatus value")
	}

	return nil
}

// MarshalJSON implements json.Marshaler
func (rs ReservationStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(rs))
}

// UnmarshalJSON implements json.Unmarshaler with case-insensitive parsing for backward compatibility
func (rs *ReservationStatus) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Convert to uppercase for case-insensitive matching
	upper := strings.ToUpper(s)
	*rs = ReservationStatus(upper)

	if !rs.IsValid() {
		return errors.New("invalid ReservationStatus value")
	}

	return nil
}

// EmailStatus represents the status of an email
type EmailStatus string

const (
	EmailStatusPending    EmailStatus = "PENDING"
	EmailStatusProcessing EmailStatus = "PROCESSING"
	EmailStatusSent       EmailStatus = "SENT"
	EmailStatusFailed     EmailStatus = "FAILED"
)

// String returns the string representation of EmailStatus
func (es EmailStatus) String() string {
	return string(es)
}

// IsValid checks if the email status is valid
func (es EmailStatus) IsValid() bool {
	switch es {
	case EmailStatusPending, EmailStatusProcessing, EmailStatusSent, EmailStatusFailed:
		return true
	default:
		return false
	}
}

// Value implements the driver.Valuer interface for GORM
func (es EmailStatus) Value() (driver.Value, error) {
	return string(es), nil
}

// Scan implements the sql.Scanner interface for GORM
func (es *EmailStatus) Scan(value interface{}) error {
	if value == nil {
		*es = EmailStatusPending
		return nil
	}

	var str string
	switch s := value.(type) {
	case string:
		str = s
	case []byte:
		str = string(s)
	default:
		return errors.New("invalid scan value for EmailStatus")
	}

	// Convert to uppercase for case-insensitive matching
	*es = EmailStatus(strings.ToUpper(str))

	if !es.IsValid() {
		return errors.New("invalid EmailStatus value")
	}

	return nil
}

// MarshalJSON implements json.Marshaler
func (es EmailStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(es))
}

// UnmarshalJSON implements json.Unmarshaler with case-insensitive parsing for backward compatibility
func (es *EmailStatus) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	// Convert to uppercase for case-insensitive matching
	upper := strings.ToUpper(s)
	*es = EmailStatus(upper)

	if !es.IsValid() {
		return errors.New("invalid EmailStatus value")
	}

	return nil
}

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
	Status       PaymentStatus `gorm:"not null;default:'PENDING';size:20;index" json:"status"`
	RefundStatus RefundStatus  `gorm:"not null;default:'NONE';size:20;index" json:"refund_status"`
	// Status transitions: PENDING → SUCCESS | FAILED | CANCELLED | EXPIRED
	// RefundStatus: NONE (default) → PARTIAL | FULL (only when refunded)

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
