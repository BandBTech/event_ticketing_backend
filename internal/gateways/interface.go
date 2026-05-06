package gateways

import (
	"context"
	"time"
)

// Gateway is the single interface every payment provider must implement.
//
// To add a new gateway (e.g. eSewa, Razorpay):
//  1. Create internal/gateways/esewa.go implementing this interface.
//  2. Add one case to Registry.Get() in registry.go.
//  3. Add the env key to config.
//     That's it — no other files change.
type Gateway interface {
	// Name returns the canonical identifier stored in DB records (e.g. "stripe").
	Name() string

	// InitSession creates a hosted payment session.
	// The checkoutToken MUST be embedded in session metadata so the webhook
	// can route back to the correct PaymentIntent.
	// Returns session ID and the URL to redirect the customer to.
	InitSession(ctx context.Context, req *SessionRequest) (*SessionResponse, error)

	// CreateRefund issues a refund on a previously captured charge.
	// Returns ErrNotRefundable for cash-based gateways (Konbini).
	CreateRefund(ctx context.Context, req *RefundRequest) (*RefundResponse, error)

	// VerifyWebhookSignature validates the raw body against the provider's
	// signature header. Returns the event type and parsed payload on success.
	VerifyWebhookSignature(body []byte, header string) (*WebhookEvent, error)
}

// ErrNotRefundable is returned by cash-based gateways (e.g. Konbini).
var ErrNotRefundable = &notRefundableError{}

type notRefundableError struct{}

func (e *notRefundableError) Error() string {
	return "this payment method cannot be refunded via gateway; issue a manual refund"
}

// ─────────────────────────────────────────────────────────────────────────────
// Session
// ─────────────────────────────────────────────────────────────────────────────

type LineItem struct {
	Name        string
	Description string
	UnitAmount  int64  // smallest currency unit
	Currency    string // ISO 4217
	Quantity    int
}

type SessionRequest struct {
	CheckoutToken string
	ActorID       string
	ActorType     string
	CustomerEmail string
	Currency      string
	LineItems     []LineItem
	SuccessURL    string
	CancelURL     string
	// Metadata is forwarded verbatim to the gateway. Must include checkout_token.
	Metadata map[string]string
}

type SessionResponse struct {
	// GatewaySessionID is the provider session reference (e.g. cs_xxx).
	GatewaySessionID string
	// GatewayPaymentIntentID is the provider-side PI id (e.g. pi_xxx), if available immediately.
	GatewayPaymentIntentID string
	// RedirectURL is where to send the customer. Empty for code-based flows (Konbini).
	RedirectURL string
}

// ─────────────────────────────────────────────────────────────────────────────
// Refund
// ─────────────────────────────────────────────────────────────────────────────

type RefundRequest struct {
	// GatewayChargeID is required for card refunds (e.g. Stripe ch_xxx).
	GatewayChargeID string
	Amount          int64  // 0 = full refund of the charge
	Currency        string // ISO 4217
	// Reason must use gateway-accepted values — use MapRefundReason() first.
	Reason   string
	Metadata map[string]string
}

type RefundResponse struct {
	GatewayRefundID string
	Status          string // "pending" | "succeeded" | "failed"
	Amount          int64
	Currency        string
	CreatedAt       time.Time
}

// ─────────────────────────────────────────────────────────────────────────────
// Webhook
// ─────────────────────────────────────────────────────────────────────────────

// WebhookEvent is the normalised, provider-agnostic representation of an
// inbound webhook. Each gateway's VerifyWebhookSignature returns one of these.
type WebhookEvent struct {
	// ProviderEventID is the provider's unique event ID (e.g. Stripe evt_xxx).
	// Used to deduplicate in webhook_events table.
	ProviderEventID string
	// EventType is the raw provider event string (e.g. "payment_intent.succeeded").
	EventType string
	// CheckoutToken extracted from the event's metadata. Used to look up our PaymentIntent.
	CheckoutToken string
	// RawObject is the unparsed JSON of the main data object (payment intent, charge, etc.)
	// The worker will unmarshal this into the appropriate Stripe struct.
	RawObject []byte
}
