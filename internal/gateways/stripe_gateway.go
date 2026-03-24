package gateways

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/stripe/stripe-go/v74"
	"github.com/stripe/stripe-go/v74/paymentintent"
	"github.com/stripe/stripe-go/v74/refund"
	"github.com/stripe/stripe-go/v74/webhook"
)

// StripeGateway implements PaymentGateway for Stripe
type StripeGateway struct {
	apiKey        string
	webhookSecret string
	successURL    string
	cancelURL     string
}

// NewStripeGateway creates a new Stripe gateway instance
func NewStripeGateway(apiKey, webhookSecret, successURL, cancelURL string) *StripeGateway {
	stripe.Key = apiKey
	return &StripeGateway{
		apiKey:        apiKey,
		webhookSecret: webhookSecret,
		successURL:    successURL,
		cancelURL:     cancelURL,
	}
}

// GetName returns "stripe"
func (sg *StripeGateway) GetName() string {
	return "stripe"
}

// CreatePaymentIntent creates a Stripe payment intent
// Returns a client secret for client-side payment handling
func (sg *StripeGateway) CreatePaymentIntent(ctx context.Context, req *PaymentIntentRequest) (*PaymentIntentResponse, error) {
	if req.Amount <= 0 {
		return nil, fmt.Errorf("invalid amount: %f", req.Amount)
	}

	// Convert amount to cents
	amountCents := int64(req.Amount * 100)

	// Create Stripe PaymentIntent
	params := &stripe.PaymentIntentParams{
		Amount:   &amountCents,
		Currency: stripe.String(req.Currency),
	}

	// Only set non-empty fields
	if req.CustomerEmail != "" {
		params.ReceiptEmail = stripe.String(req.CustomerEmail)
	}

	if req.Description != "" {
		params.Description = stripe.String(req.Description)
	}

	// Note: Metadata handling would need to be done via params.AddMetadata()
	// but for simplicity in this design, we skip it for now
	// params.AddMetadata("key", "value")

	pi, err := paymentintent.New(params)
	if err != nil {
		log.Printf("[STRIPE] Failed to create payment intent: %v", err)
		return nil, fmt.Errorf("failed to create payment intent: %w", err)
	}

	resp := &PaymentIntentResponse{
		ClientSecret: pi.ClientSecret,
		Status:       string(pi.Status),
		Amount:       req.Amount,
		Currency:     req.Currency,
		CreatedAt:    time.Now(),
		Metadata: map[string]interface{}{
			"stripe_payment_intent_id": pi.ID,
		},
	}

	return resp, nil
}

// CreateRefund refunds a Stripe payment
func (sg *StripeGateway) CreateRefund(ctx context.Context, req *RefundRequest) (*RefundResponse, error) {
	if req.Amount <= 0 {
		return nil, fmt.Errorf("invalid refund amount: %f", req.Amount)
	}

	amountCents := int64(req.Amount * 100)

	params := &stripe.RefundParams{
		Amount: &amountCents,
		Reason: stripe.String(req.Reason),
	}

	r, err := refund.New(params)
	if err != nil {
		log.Printf("[STRIPE] Failed to create refund: %v", err)
		return nil, fmt.Errorf("failed to create refund: %w", err)
	}

	return &RefundResponse{
		GatewayRefundID: r.ID,
		Status:          string(r.Status),
		Amount:          req.Amount,
		Currency:        req.Currency,
		Reason:          req.Reason,
		CreatedAt:       time.Now(),
	}, nil
}

// GetRefund retrieves a refund's current status
func (sg *StripeGateway) GetRefund(ctx context.Context, gatewayRefundID string) (*RefundResponse, error) {
	r, err := refund.Get(gatewayRefundID, nil)
	if err != nil {
		log.Printf("[STRIPE] Failed to get refund %s: %v", gatewayRefundID, err)
		return nil, fmt.Errorf("failed to get refund: %w", err)
	}

	return &RefundResponse{
		GatewayRefundID: r.ID,
		Status:          string(r.Status),
		Amount:          float64(r.Amount) / 100, // Convert from cents
		Currency:        string(r.Currency),
		Reason:          string(r.Reason),
		CreatedAt:       time.Unix(r.Created, 0),
	}, nil
}

// VerifyWebhook verifies Stripe webhook signature and parses the event
// SECURITY: This is critical - only process events with valid signatures
func (sg *StripeGateway) VerifyWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error) {
	if sg.webhookSecret == "" {
		//ERROR: Webhook secret not configured - this is a critical security issue
		log.Printf("[STRIPE_WEBHOOK] Webhook secret not configured - cannot verify signatures")
		return nil, fmt.Errorf("webhook secret not configured")
	}

	// Verify signature with options to ignore API version mismatch
	event, err := webhook.ConstructEventWithOptions(payload, signature, sg.webhookSecret, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})
	if err != nil {
		log.Printf("[STRIPE_WEBHOOK] Signature verification failed: %v", err)
		return nil, fmt.Errorf("invalid webhook signature: %w", err)
	}

	// Parse event data based on type
	webhookEvent := &WebhookEvent{
		Gateway:   "stripe",
		EventID:   event.ID,
		Type:      event.Type,
		CreatedAt: time.Unix(event.Created, 0),
	}

	// Extract relevant IDs from event
	switch event.Type {
	case "payment_intent.succeeded", "payment_intent.payment_failed", "payment_intent.canceled":
		var pi stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &pi); err == nil {
			webhookEvent.PaymentIntentID = pi.ID
		}

	case "charge.refunded":
		var charge stripe.Charge
		if err := json.Unmarshal(event.Data.Raw, &charge); err == nil {
			webhookEvent.RefundID = charge.ID
		}
	}

	return webhookEvent, nil
}

// GetSupportedCurrencies returns currencies Stripe supports
func (sg *StripeGateway) GetSupportedCurrencies() []string {
	return []string{
		"usd", "eur", "gbp", "jpy", "cad", "aud", "sgd", "hkd",
		"ind", "npr", // India, Nepal
		"aed", "sar", // Middle East
		"mxn", "brl", // Americas
	}
}

// GetSupportedCountries returns countries where Stripe operates
func (sg *StripeGateway) GetSupportedCountries() []string {
	return []string{
		"US", "GB", "DE", "FR", "IT", "ES", "NL", "BE", "AT", "IE", // Europe
		"CA", "MX", "BR", // Americas
		"JP", "CN", "SG", "HK", "AU", "IN", "NP", // Asia
		"AE", "SA", // Middle East
	}
}

// CalculateFees calculates Stripe's processing fees
// Stripe charges 2.9% + $0.30 for card payments (varies by country)
func (sg *StripeGateway) CalculateFees(amount float64, currency string) float64 {
	// Base rate: 2.9% + fixed fee (varies by currency)
	basePercentage := 0.029
	fixedFee := 0.30

	// Adjust for currency (some currencies have different rates)
	switch currency {
	case "jpy":
		fixedFee = 30 // JPY doesn't use decimals
	case "ind", "npr":
		basePercentage = 0.032 // Higher rate for Indian currencies
		fixedFee = 10
	}

	return (amount * basePercentage) + fixedFee
}
