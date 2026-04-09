package gateways

import (
	"context"
	"time"
)

// PaymentProvider defines the interface that all payment providers must implement
// This abstraction allows supporting multiple payment providers (Stripe, Khalti, Esewa)
// without changing core business logic
type PaymentProvider interface {
	// CreatePayment creates a new payment with the provider
	CreatePayment(ctx context.Context, req *CreatePaymentRequest) (*CreatePaymentResponse, error)

	// VerifyPayment verifies a payment and extracts transaction details
	VerifyPayment(ctx context.Context, payload interface{}) (*VerifyPaymentResponse, error)

	// RefundPayment initiates a refund for a completed payment
	RefundPayment(ctx context.Context, req *RefundRequest) error

	// VerifyWebhook verifies webhook signatures and extracts event data
	VerifyWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error)

	// GetName returns the unique identifier of this provider (e.g., "STRIPE", "KHALTI")
	GetName() string

	// GetSupportedCurrencies returns list of currencies supported by this provider
	GetSupportedCurrencies() []string

	// GetSupportedCountries returns list of countries supported by this provider
	GetSupportedCountries() []string
}

// CreatePaymentRequest represents a request to create a payment
type CreatePaymentRequest struct {
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
	ExtraData      map[string]interface{} `json:"extra_data,omitempty"` // Provider-specific data
}

// CreatePaymentResponse represents the response from creating a payment
type CreatePaymentResponse struct {
	ClientSecret         string                 `json:"client_secret,omitempty"`
	Status               string                 `json:"status"` // pending, processing, succeeded, failed, canceled
	Amount               float64                `json:"amount"`
	Currency             string                 `json:"currency"`
	RedirectURL          string                 `json:"redirect_url,omitempty"` // For redirect-based providers (Esewa, Khalti)
	PaymentMethodDetails *PaymentMethodDetails  `json:"payment_method_details,omitempty"`
	CreatedAt            time.Time              `json:"created_at"`
	ExpiresAt            *time.Time             `json:"expires_at,omitempty"`
	Metadata             map[string]interface{} `json:"metadata,omitempty"`
	ProviderTxnID        string                 `json:"provider_txn_id,omitempty"`
}

// VerifyPaymentResponse represents the response from verifying a payment
type VerifyPaymentResponse struct {
	ProviderTxnID string                 `json:"provider_txn_id"`
	Status        string                 `json:"status"` // success, failed, pending
	Amount        float64                `json:"amount"`
	Currency      string                 `json:"currency"`
	Fee           *float64               `json:"fee,omitempty"` // Processing fee (for Stripe)
	ProcessedAt   time.Time              `json:"processed_at"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
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
