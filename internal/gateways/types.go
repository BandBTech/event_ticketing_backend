package gateways

import (
	"context"
	"time"
)

// PaymentGateway defines the interface that all payment gateways must implement
// This abstraction allows supporting multiple payment providers (Stripe, PayPal, eSewa, etc.)
// without changing core business logic
type PaymentGateway interface {
	// CreatePaymentIntent creates a new payment intent with the gateway
	CreatePaymentIntent(ctx context.Context, req *PaymentIntentRequest) (*PaymentIntentResponse, error)

	// CreateRefund initiates a refund for a completed payment
	CreateRefund(ctx context.Context, req *RefundRequest) (*RefundResponse, error)

	// GetRefund retrieves the status of a refund
	GetRefund(ctx context.Context, gatewayRefundID string) (*RefundResponse, error)

	// VerifyWebhook verifies the authenticity of a webhook and parses the event
	VerifyWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error)

	// GetSupportedCurrencies returns list of currencies supported by this gateway
	GetSupportedCurrencies() []string

	// GetSupportedCountries returns list of countries supported by this gateway
	GetSupportedCountries() []string

	// GetName returns the unique identifier of this gateway (e.g., "stripe", "paypal")
	GetName() string

	// CalculateFees calculates the gateway's processing fees for a given amount
	CalculateFees(amount float64, currency string) float64
}

// PaymentIntentRequest represents a request to create a payment intent
type PaymentIntentRequest struct {
	Amount         float64                `json:"amount"`
	Currency       string                 `json:"currency"`
	IdempotencyKey string                 `json:"idempotency_key"`
	Metadata       map[string]string      `json:"metadata"`
	CustomerEmail  string                 `json:"customer_email"`
	CustomerName   string                 `json:"customer_name"`
	Description    string                 `json:"description"`
	SuccessURL     string                 `json:"success_url,omitempty"`
	CancelURL      string                 `json:"cancel_url,omitempty"`
	ReturnURL      string                 `json:"return_url,omitempty"`
	ExtraData      map[string]interface{} `json:"extra_data,omitempty"` // Gateway-specific data
}

// PaymentIntentResponse represents the response from creating/fetching a payment intent
type PaymentIntentResponse struct {
	ClientSecret         string                 `json:"client_secret,omitempty"`
	Status               string                 `json:"status"` // pending, processing, succeeded, failed, canceled
	Amount               float64                `json:"amount"`
	Currency             string                 `json:"currency"`
	RedirectURL          string                 `json:"redirect_url,omitempty"` // For redirect-based gateways (eSewa, Khalti)
	PaymentMethodDetails *PaymentMethodDetails  `json:"payment_method_details,omitempty"`
	CreatedAt            time.Time              `json:"created_at"`
	ExpiresAt            *time.Time             `json:"expires_at,omitempty"`
	Metadata             map[string]interface{} `json:"metadata,omitempty"`
}

// PaymentMethodDetails contains NON-SENSITIVE payment method metadata
// SECURITY: Never contains full card numbers, CVV, PINs, or passwords
type PaymentMethodDetails struct {
	Type         string `json:"type"`                  // card, wallet, bank_transfer, upi
	CardBrand    string `json:"card_brand,omitempty"`  // visa, mastercard, amex
	CardType     string `json:"card_type,omitempty"`   // credit, debit, prepaid
	Last4        string `json:"last4,omitempty"`       // Last 4 digits only
	ExpMonth     int    `json:"exp_month,omitempty"`   // For display only
	ExpYear      int    `json:"exp_year,omitempty"`    // For display only
	Country      string `json:"country,omitempty"`     // Card issuing country
	Funding      string `json:"funding,omitempty"`     // credit, debit, prepaid
	WalletType   string `json:"wallet_type,omitempty"` // esewa, khalti, paypal
	WalletID     string `json:"wallet_id,omitempty"`   // Masked wallet identifier
	BankName     string `json:"bank_name,omitempty"`
	AccountLast4 string `json:"account_last4,omitempty"` // Last 4 digits only
}

// RefundRequest represents a request to create a refund
type RefundRequest struct {
	ChargeID string            `json:"charge_id,omitempty"` // Stripe Charge ID (ch_xxx) - required for Stripe refunds
	Amount   float64           `json:"amount"`
	Currency string            `json:"currency"`
	Reason   string            `json:"reason"` // event_canceled, customer_request, duplicate, fraudulent
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RefundResponse represents the response from creating/fetching a refund
type RefundResponse struct {
	GatewayRefundID string    `json:"gateway_refund_id"`
	Status          string    `json:"status"` // pending, processing, succeeded, failed, canceled
	Amount          float64   `json:"amount"`
	Currency        string    `json:"currency"`
	Reason          string    `json:"reason"`
	CreatedAt       time.Time `json:"created_at"`
}

// WebhookEvent represents a parsed webhook event from any gateway
type WebhookEvent struct {
	Gateway         string                 `json:"gateway"`  // stripe, paypal, esewa, etc.
	EventID         string                 `json:"event_id"` // Gateway's event ID
	Type            string                 `json:"type"`     // payment_intent.succeeded, etc.
	Data            map[string]interface{} `json:"data"`     // Event data
	CreatedAt       time.Time              `json:"created_at"`
	PaymentIntentID string                 `json:"payment_intent_id,omitempty"`
	RefundID        string                 `json:"refund_id,omitempty"`
}

// GatewayError represents a standardized error from any gateway
type GatewayError struct {
	Code            string `json:"code"`
	Message         string `json:"message"`
	DeclineCode     string `json:"decline_code,omitempty"`
	Param           string `json:"param,omitempty"`
	Type            string `json:"type"`
	PaymentIntentID string `json:"payment_intent_id,omitempty"`
}

func (e *GatewayError) Error() string {
	return e.Message
}

// Payment status constants
const (
	StatusPending           = "pending"
	StatusProcessing        = "processing"
	StatusRequiresAction    = "requires_action"
	StatusSucceeded         = "succeeded"
	StatusFailed            = "failed"
	StatusCanceled          = "canceled"
	StatusRefunded          = "refunded"
	StatusPartiallyRefunded = "partially_refunded"
)

// Refund status constants
const (
	RefundStatusPending    = "pending"
	RefundStatusProcessing = "processing"
	RefundStatusSucceeded  = "succeeded"
	RefundStatusFailed     = "failed"
	RefundStatusCanceled   = "canceled"
)

// Payment method types
const (
	PaymentMethodCard         = "card"
	PaymentMethodWallet       = "wallet"
	PaymentMethodBankTransfer = "bank_transfer"
	PaymentMethodUPI          = "upi"
)
