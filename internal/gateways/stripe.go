package gateways

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/stripe/stripe-go/v74"
	"github.com/stripe/stripe-go/v74/paymentintent"
	"github.com/stripe/stripe-go/v74/refund"
	"github.com/stripe/stripe-go/v74/webhook"
)

// StripeGateway implements the PaymentGateway interface for Stripe
type StripeGateway struct {
	apiKey        string
	webhookSecret string
	isTestMode    bool
}

// NewStripeGateway creates a new Stripe gateway instance
func NewStripeGateway(apiKey, webhookSecret string, isTestMode bool) *StripeGateway {
	stripe.Key = apiKey
	return &StripeGateway{
		apiKey:        apiKey,
		webhookSecret: webhookSecret,
		isTestMode:    isTestMode,
	}
}

// CreatePaymentIntent creates a new Stripe payment intent
func (g *StripeGateway) CreatePaymentIntent(ctx context.Context, req *PaymentIntentRequest) (*PaymentIntentResponse, error) {
	// Convert amount to cents (Stripe uses smallest currency unit)
	amountInCents := int64(req.Amount * 100)

	params := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(amountInCents),
		Currency: stripe.String(req.Currency),
		Params: stripe.Params{
			Context: ctx,
		},
	}

	// Set idempotency key for retry safety
	params.SetIdempotencyKey(req.IdempotencyKey)

	// Add customer information
	if req.CustomerEmail != "" {
		params.ReceiptEmail = stripe.String(req.CustomerEmail)
	}
	if req.Description != "" {
		params.Description = stripe.String(req.Description)
	}

	// Add metadata
	if req.Metadata != nil {
		params.Metadata = req.Metadata
	}

	// Create the payment intent
	pi, err := paymentintent.New(params)
	if err != nil {
		return nil, g.wrapStripeError(err)
	}

	// Build response
	resp := &PaymentIntentResponse{
		GatewayPaymentID: pi.ID,
		ClientSecret:     pi.ClientSecret,
		Status:           g.mapStripeStatus(string(pi.Status)),
		Amount:           req.Amount,
		Currency:         req.Currency,
		CreatedAt:        time.Unix(pi.Created, 0),
		Metadata:         make(map[string]interface{}),
	}

	// Add expiration if applicable
	if pi.CanceledAt > 0 {
		canceledAt := time.Unix(pi.CanceledAt, 0)
		resp.ExpiresAt = &canceledAt
	}

	// Extract payment method details (NON-SENSITIVE metadata only)
	if pi.PaymentMethod != nil {
		resp.PaymentMethodDetails = g.extractPaymentMethodDetails(pi.PaymentMethod)
	}

	return resp, nil
}

// GetPaymentIntent retrieves a Stripe payment intent by ID
func (g *StripeGateway) GetPaymentIntent(ctx context.Context, gatewayPaymentID string) (*PaymentIntentResponse, error) {
	pi, err := paymentintent.Get(gatewayPaymentID, &stripe.PaymentIntentParams{
		Params: stripe.Params{
			Context: ctx,
		},
	})
	if err != nil {
		return nil, g.wrapStripeError(err)
	}

	resp := &PaymentIntentResponse{
		GatewayPaymentID: pi.ID,
		ClientSecret:     pi.ClientSecret,
		Status:           g.mapStripeStatus(string(pi.Status)),
		Amount:           float64(pi.Amount) / 100,
		Currency:         string(pi.Currency),
		CreatedAt:        time.Unix(pi.Created, 0),
		Metadata:         make(map[string]interface{}),
	}

	// Extract payment method details if available
	if pi.PaymentMethod != nil {
		resp.PaymentMethodDetails = g.extractPaymentMethodDetails(pi.PaymentMethod)
	}

	return resp, nil
}

// CancelPaymentIntent cancels a pending Stripe payment intent
func (g *StripeGateway) CancelPaymentIntent(ctx context.Context, gatewayPaymentID string) error {
	_, err := paymentintent.Cancel(gatewayPaymentID, &stripe.PaymentIntentCancelParams{
		Params: stripe.Params{
			Context: ctx,
		},
	})
	if err != nil {
		return g.wrapStripeError(err)
	}
	return nil
}

// CreateRefund creates a refund for a Stripe payment
func (g *StripeGateway) CreateRefund(ctx context.Context, req *RefundRequest) (*RefundResponse, error) {
	amountInCents := int64(req.Amount * 100)

	params := &stripe.RefundParams{
		PaymentIntent: stripe.String(req.GatewayPaymentID),
		Amount:        stripe.Int64(amountInCents),
		Params: stripe.Params{
			Context: ctx,
		},
	}

	if req.Reason != "" {
		// Map our reason to Stripe's expected values
		stripeReason := g.mapRefundReason(req.Reason)
		params.Reason = stripe.String(stripeReason)
	}

	if req.Metadata != nil {
		params.Metadata = req.Metadata
	}

	r, err := refund.New(params)
	if err != nil {
		return nil, g.wrapStripeError(err)
	}

	return &RefundResponse{
		GatewayRefundID:  r.ID,
		GatewayPaymentID: req.GatewayPaymentID,
		Status:           g.mapStripeRefundStatus(string(r.Status)),
		Amount:           req.Amount,
		Currency:         req.Currency,
		Reason:           req.Reason,
		CreatedAt:        time.Unix(r.Created, 0),
	}, nil
}

// GetRefund retrieves a Stripe refund by ID
func (g *StripeGateway) GetRefund(ctx context.Context, gatewayRefundID string) (*RefundResponse, error) {
	r, err := refund.Get(gatewayRefundID, &stripe.RefundParams{
		Params: stripe.Params{
			Context: ctx,
		},
	})
	if err != nil {
		return nil, g.wrapStripeError(err)
	}

	return &RefundResponse{
		GatewayRefundID:  r.ID,
		GatewayPaymentID: r.PaymentIntent.ID,
		Status:           g.mapStripeRefundStatus(string(r.Status)),
		Amount:           float64(r.Amount) / 100,
		Currency:         string(r.Currency),
		CreatedAt:        time.Unix(r.Created, 0),
	}, nil
}

// VerifyWebhook verifies and parses a Stripe webhook event
func (g *StripeGateway) VerifyWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error) {
	event, err := webhook.ConstructEvent(payload, signature, g.webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("webhook signature verification failed: %w", err)
	}

	// Parse the event data
	var eventData map[string]interface{}
	if err := json.Unmarshal(event.Data.Raw, &eventData); err != nil {
		return nil, fmt.Errorf("failed to parse webhook event data: %w", err)
	}

	webhookEvent := &WebhookEvent{
		Gateway:   "stripe",
		EventID:   event.ID,
		Type:      event.Type,
		Data:      eventData,
		CreatedAt: time.Unix(event.Created, 0),
	}

	// Extract payment intent ID if present
	if obj, ok := eventData["object"].(map[string]interface{}); ok {
		if id, ok := obj["id"].(string); ok {
			webhookEvent.PaymentIntentID = id
		}
		// For refund events
		if paymentIntentID, ok := obj["payment_intent"].(string); ok {
			webhookEvent.PaymentIntentID = paymentIntentID
		}
		if refundID, ok := obj["id"].(string); ok && event.Type == "charge.refunded" {
			webhookEvent.RefundID = refundID
		}
	}

	return webhookEvent, nil
}

// GetSupportedCurrencies returns currencies supported by Stripe
func (g *StripeGateway) GetSupportedCurrencies() []string {
	return []string{
		"USD", "EUR", "GBP", "CAD", "AUD", "NZD", "JPY", "INR", "NPR", "SGD",
		"CHF", "SEK", "NOK", "DKK", "PLN", "CZK", "HUF", "RON", "BGN", "HRK",
	}
}

// GetSupportedCountries returns countries supported by Stripe
func (g *StripeGateway) GetSupportedCountries() []string {
	return []string{
		"US", "GB", "CA", "AU", "NZ", "JP", "IN", "NP", "SG", "CH", "SE", "NO",
		"DK", "PL", "CZ", "HU", "RO", "BG", "HR", "AT", "BE", "FI", "FR", "DE",
		"IE", "IT", "LU", "NL", "PT", "ES", "GR", "CY", "MT", "SI", "SK", "EE",
		"LV", "LT",
	}
}

// GetName returns the gateway identifier
func (g *StripeGateway) GetName() string {
	return "stripe"
}

// CalculateFees calculates Stripe's processing fees
func (g *StripeGateway) CalculateFees(amount float64, currency string) float64 {
	// Stripe standard fees: 2.9% + $0.30 for US cards
	// International cards have additional 1.5% fee
	// For simplicity, using base rate here
	percentageFee := amount * 0.029
	fixedFee := 0.30

	// For zero-decimal currencies (JPY, KRW, etc.), no fixed fee in cents
	zeroDecimalCurrencies := map[string]bool{
		"JPY": true, "KRW": true, "VND": true, "CLP": true,
	}
	if zeroDecimalCurrencies[currency] {
		fixedFee = 0
	}

	return percentageFee + fixedFee
}

// Helper methods

// extractPaymentMethodDetails extracts NON-SENSITIVE payment method metadata
// SECURITY: Never returns full card numbers, CVV, or PINs
func (g *StripeGateway) extractPaymentMethodDetails(pm interface{}) *PaymentMethodDetails {
	details := &PaymentMethodDetails{}

	// Type assertion to get payment method as map
	pmMap, ok := pm.(map[string]interface{})
	if !ok {
		return details
	}

	// Extract type
	if pmType, ok := pmMap["type"].(string); ok {
		details.Type = pmType
	}

	// Extract card details if it's a card payment
	if cardData, ok := pmMap["card"].(map[string]interface{}); ok {
		if brand, ok := cardData["brand"].(string); ok {
			details.CardBrand = brand
		}
		if last4, ok := cardData["last4"].(string); ok {
			details.Last4 = last4
		}
		if expMonth, ok := cardData["exp_month"].(float64); ok {
			details.ExpMonth = int(expMonth)
		}
		if expYear, ok := cardData["exp_year"].(float64); ok {
			details.ExpYear = int(expYear)
		}
		if country, ok := cardData["country"].(string); ok {
			details.Country = country
		}
		if funding, ok := cardData["funding"].(string); ok {
			details.Funding = funding
			details.CardType = funding // credit, debit, prepaid
		}
	}

	return details
}

// mapStripeStatus maps Stripe's payment intent status to our standard status
func (g *StripeGateway) mapStripeStatus(stripeStatus string) string {
	statusMap := map[string]string{
		"requires_payment_method": StatusPending,
		"requires_confirmation":   StatusPending,
		"requires_action":         StatusRequiresAction,
		"processing":              StatusProcessing,
		"requires_capture":        StatusProcessing,
		"canceled":                StatusCanceled,
		"succeeded":               StatusSucceeded,
	}

	if status, ok := statusMap[stripeStatus]; ok {
		return status
	}
	return stripeStatus
}

// mapStripeRefundStatus maps Stripe's refund status to our standard status
func (g *StripeGateway) mapStripeRefundStatus(stripeStatus string) string {
	statusMap := map[string]string{
		"pending":   RefundStatusPending,
		"succeeded": RefundStatusSucceeded,
		"failed":    RefundStatusFailed,
		"canceled":  RefundStatusCanceled,
	}

	if status, ok := statusMap[stripeStatus]; ok {
		return status
	}
	return stripeStatus
}

// mapRefundReason maps our refund reason to Stripe's expected values
func (g *StripeGateway) mapRefundReason(reason string) string {
	reasonMap := map[string]string{
		"duplicate":          "duplicate",
		"fraudulent":         "fraudulent",
		"customer_request":   "requested_by_customer",
		"event_canceled":     "requested_by_customer",
		"event_cancellation": "requested_by_customer",
	}

	if stripeReason, ok := reasonMap[reason]; ok {
		return stripeReason
	}
	return "requested_by_customer" // Default
}

// wrapStripeError wraps a Stripe error into our standard GatewayError
func (g *StripeGateway) wrapStripeError(err error) error {
	if stripeErr, ok := err.(*stripe.Error); ok {
		return &GatewayError{
			Code:            string(stripeErr.Code),
			Message:         stripeErr.Msg,
			DeclineCode:     string(stripeErr.DeclineCode),
			Param:           stripeErr.Param,
			Type:            string(stripeErr.Type),
			PaymentIntentID: stripeErr.PaymentIntent.ID,
		}
	}
	return err
}
