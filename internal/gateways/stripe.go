package gateways

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/checkout/session"
	"github.com/stripe/stripe-go/v83/refund"
	"github.com/stripe/stripe-go/v83/webhook"
)

type stripeGateway struct {
	apiKey        string
	webhookSecret string
}

// NewStripe creates a configured Stripe gateway.
func NewStripe(apiKey, webhookSecret string) Gateway {
	return &stripeGateway{
		apiKey:        apiKey,
		webhookSecret: webhookSecret,
	}
}

func (g *stripeGateway) Name() string { return "stripe" }

// ─────────────────────────────────────────────────────────────────────────────
// InitSession — creates a Stripe Checkout Session
// ─────────────────────────────────────────────────────────────────────────────

func (g *stripeGateway) InitSession(_ context.Context, req *SessionRequest) (*SessionResponse, error) {
	stripe.Key = g.apiKey

	lineItems := make([]*stripe.CheckoutSessionLineItemParams, len(req.LineItems))
	for i, item := range req.LineItems {
		lineItems[i] = &stripe.CheckoutSessionLineItemParams{
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency: stripe.String(strings.ToLower(item.Currency)),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name:        stripe.String(item.Name),
					Description: stripe.String(item.Description),
				},
				UnitAmount: stripe.Int64(item.UnitAmount),
			},
			Quantity: stripe.Int64(int64(item.Quantity)),
		}
	}

	params := &stripe.CheckoutSessionParams{
		Mode:          stripe.String(string(stripe.CheckoutSessionModePayment)),
		CustomerEmail: stripe.String(req.CustomerEmail),
		LineItems:     lineItems,
		SuccessURL:    stripe.String(req.SuccessURL),
		CancelURL:     stripe.String(req.CancelURL),
		PaymentIntentData: &stripe.CheckoutSessionPaymentIntentDataParams{
			// Capture charge immediately. Change to "manual" for auth-only.
			CaptureMethod: stripe.String("automatic"),
		},
	}

	// Add metadata to both CheckoutSession and PaymentIntent
	for k, v := range req.Metadata {
		params.AddMetadata(k, v)
		params.PaymentIntentData.AddMetadata(k, v)
	}

	sess, err := session.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe: init session: %w", err)
	}

	resp := &SessionResponse{
		GatewaySessionID: sess.ID,
		RedirectURL:      sess.URL,
	}
	if sess.PaymentIntent != nil {
		resp.GatewayPaymentIntentID = sess.PaymentIntent.ID
	}
	return resp, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// CreateRefund
// ─────────────────────────────────────────────────────────────────────────────

func (g *stripeGateway) CreateRefund(_ context.Context, req *RefundRequest) (*RefundResponse, error) {
	stripe.Key = g.apiKey

	params := &stripe.RefundParams{
		Reason: stripe.String(MapRefundReason(req.Reason)),
	}
	gatewayID := strings.TrimSpace(req.GatewayChargeID)
	if gatewayID == "" {
		return nil, fmt.Errorf("stripe: create refund: gateway charge/payment intent id is required and cannot be empty")
	}
	if strings.HasPrefix(gatewayID, "pi_") {
		params.PaymentIntent = stripe.String(gatewayID)
	} else {
		params.Charge = stripe.String(gatewayID)
	}
	if req.Amount > 0 {
		params.Amount = stripe.Int64(req.Amount)
	}
	for k, v := range req.Metadata {
		params.AddMetadata(k, v)
	}

	r, err := refund.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe: create refund: %w", err)
	}

	return &RefundResponse{
		GatewayRefundID: r.ID,
		Status:          string(r.Status),
		Amount:          r.Amount,
		Currency:        string(r.Currency),
		CreatedAt:       time.Unix(r.Created, 0),
	}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// VerifyWebhookSignature
// ─────────────────────────────────────────────────────────────────────────────

func (g *stripeGateway) VerifyWebhookSignature(body []byte, header string) (*WebhookEvent, error) {
	event, err := webhook.ConstructEvent(body, header, g.webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("stripe: invalid webhook signature: %w", err)
	}

	// Extract checkout_token from wherever Stripe puts it for this event type.
	checkoutToken, err := extractStripeCheckoutToken(event)
	if err != nil {
		return nil, err
	}

	return &WebhookEvent{
		ProviderEventID: event.ID,
		EventType:       string(event.Type),
		CheckoutToken:   checkoutToken,
		RawObject:       event.Data.Raw,
	}, nil
}

// extractStripeCheckoutToken pulls our checkout_token out of the Stripe event
// metadata, which differs by event type.
func extractStripeCheckoutToken(event stripe.Event) (string, error) {
	var meta map[string]string

	switch event.Type {
	case "payment_intent.succeeded",
		"payment_intent.payment_failed",
		"payment_intent.canceled":
		var pi stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
			return "", fmt.Errorf("stripe: unmarshal payment_intent: %w", err)
		}
		meta = pi.Metadata

	case "checkout.session.completed",
		"checkout.session.expired":
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
			return "", fmt.Errorf("stripe: unmarshal checkout.session: %w", err)
		}
		meta = sess.Metadata

	case "charge.refunded":
		// Charge events don't carry our metadata directly.
		// The worker handles charge lookups via payment_intent ID.
		return "", nil

	default:
		// Unknown event — return empty token; worker will ignore it.
		return "", nil
	}

	if meta == nil {
		return "", fmt.Errorf("stripe: event %s has no metadata", event.Type)
	}
	token, ok := meta["checkout_token"]
	if !ok || token == "" {
		return "", fmt.Errorf("stripe: event %s metadata missing checkout_token", event.Type)
	}
	return token, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// MapRefundReason converts our internal reason to Stripe's accepted values.
// Stripe only accepts: duplicate | fraudulent | requested_by_customer
func MapRefundReason(internal string) string {
	// Map refund type to Stripe reason
	switch strings.ToLower(internal) {
	case "duplicate":
		return "duplicate"
	case "fraudulent":
		return "fraudulent"
	case "customer_request", "customer_initiated":
		return "requested_by_customer"
	case "event_cancellation":
		return "requested_by_customer" // Event cancellation is customer request from platform perspective
	case "partial_refund":
		return "requested_by_customer"
	default:
		return "requested_by_customer" // Default to customer request for unknown reasons
	}
}

// ExtractStripeChargeID pulls the charge ID from a PaymentIntent's raw JSON.
// Stripe embeds it as latest_charge.
func ExtractStripeChargeID(raw []byte) string {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(raw, &pi); err != nil {
		return ""
	}
	if pi.LatestCharge != nil {
		return pi.LatestCharge.ID
	}
	return ""
}

// ExtractStripePaymentMethodDetails returns a map of card/konbini details safe to store.
// Never includes PAN, CVV, or any PCI-sensitive field.
func ExtractStripePaymentMethodDetails(pi *stripe.PaymentIntent) (methodType string, details map[string]interface{}) {
	details = make(map[string]interface{})

	if pi.PaymentMethod != nil {
		details["payment_method_id"] = pi.PaymentMethod.ID
	}

	if len(pi.PaymentMethodTypes) > 0 {
		methodType = pi.PaymentMethodTypes[0]
	}

	return methodType, details
}
