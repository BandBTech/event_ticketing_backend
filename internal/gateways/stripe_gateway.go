package gateways

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v74"
	"github.com/stripe/stripe-go/v74/paymentintent"
	"github.com/stripe/stripe-go/v74/refund"
	"github.com/stripe/stripe-go/v74/webhook"
)

// StripeProvider implements PaymentProvider for Stripe
type StripeProvider struct {
	apiKey        string
	webhookSecret string
	successURL    string
	cancelURL     string
}

// NewStripeProvider creates a new Stripe provider instance
func NewStripeProvider(apiKey, webhookSecret, successURL, cancelURL string) *StripeProvider {
	stripe.Key = apiKey
	return &StripeProvider{
		apiKey:        apiKey,
		webhookSecret: webhookSecret,
		successURL:    successURL,
		cancelURL:     cancelURL,
	}
}

// GetName returns "STRIPE"
func (sp *StripeProvider) GetName() string {
	return "STRIPE"
}

// GetSupportedCurrencies returns currencies supported by Stripe
func (sp *StripeProvider) GetSupportedCurrencies() []string {
	return []string{
		"USD", "EUR", "GBP", "JPY", "CAD", "AUD", "CHF", "NOK", "SEK", "DKK",
		"PLN", "CZK", "HUF", "SGD", "HKD", "NZD", "MXN", "BRL", "ZAR", "THB",
		"MYR", "PHP", "TWD", "TRY", "INR", "RUB", "AED", "SAR", "ILS", "EGP",
		"KES", "MAD", "TND", "UGX", "XAF", "XOF", "BWP", "GHS", "MUR", "SCR",
		"CVE", "BSD", "BBD", "BZD", "BND", "FJD", "GYD", "JMD", "LRD", "NAD",
		"SBD", "SRD", "TTD", "VND", "AMD", "AZN", "BAM", "BGN", "BYN", "GEL",
		"HRK", "ISK", "KZT", "MKD", "MDL", "RON", "RSD", "UAH", "UZS",
	}
}

// GetSupportedCountries returns countries supported by Stripe
func (sp *StripeProvider) GetSupportedCountries() []string {
	return []string{
		"US", "CA", "GB", "AU", "DE", "FR", "IT", "ES", "NL", "BE", "AT", "CH",
		"SE", "NO", "DK", "FI", "IE", "PT", "GR", "SI", "HR", "SK", "CZ", "HU",
		"PL", "EE", "LV", "LT", "MT", "CY", "LU", "BG", "RO", "JP", "SG", "HK",
		"NZ", "MX", "BR", "AR", "CL", "CO", "PE", "UY", "ZA", "AE", "SA", "IL",
		"EG", "KE", "MA", "TN", "UG", "GH", "MU", "SC", "CV", "BS", "BB", "BZ",
		"BN", "FJ", "GY", "JM", "LR", "NA", "SB", "SR", "TT", "VN", "AM", "AZ",
		"BA", "GE", "IS", "KZ", "MD", "RS", "UA", "UZ",
	}
}

// CreatePayment creates a Stripe payment intent
func (sp *StripeProvider) CreatePayment(ctx context.Context, req *CreatePaymentRequest) (*CreatePaymentResponse, error) {
	if req.Amount <= 0 {
		return nil, fmt.Errorf("invalid amount: %f", req.Amount)
	}

	// Convert amount to cents
	amountCents := int64(req.Amount * 100)

	// Create Stripe PaymentIntent
	params := &stripe.PaymentIntentParams{
		Amount:   &amountCents,
		Currency: stripe.String(strings.ToLower(req.Currency)),
	}

	// Only set non-empty fields
	if req.CustomerEmail != "" {
		params.ReceiptEmail = stripe.String(req.CustomerEmail)
	}

	if req.Description != "" {
		params.Description = stripe.String(req.Description)
	}

	pi, err := paymentintent.New(params)
	if err != nil {
		log.Printf("[STRIPE] Failed to create payment intent: %v", err)
		return nil, fmt.Errorf("failed to create payment intent: %w", err)
	}

	resp := &CreatePaymentResponse{
		ClientSecret:  pi.ClientSecret,
		Status:        string(pi.Status),
		Amount:        req.Amount,
		Currency:      req.Currency,
		ProviderTxnID: pi.ID,
		CreatedAt:     time.Now(),
		Metadata: map[string]interface{}{
			"stripe_payment_intent_id": pi.ID,
		},
	}

	return resp, nil
}

// VerifyPayment verifies a Stripe payment from webhook data
func (sp *StripeProvider) VerifyPayment(ctx context.Context, payload interface{}) (*VerifyPaymentResponse, error) {
	// For Stripe, payload should be the webhook event data
	eventData, ok := payload.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid payload type for Stripe verification")
	}

	// Extract payment intent data
	data, ok := eventData["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid webhook data structure")
	}

	object, ok := data["object"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid payment intent object")
	}

	// Extract required fields
	id, _ := object["id"].(string)
	amount := int64(0)
	if amt, ok := object["amount"].(float64); ok {
		amount = int64(amt)
	}
	currency, _ := object["currency"].(string)
	status, _ := object["status"].(string)

	// Convert amount from cents to dollars
	amountFloat := float64(amount) / 100

	// Extract charge information for fee calculation
	var fee *float64
	if charges, ok := object["charges"].(map[string]interface{}); ok {
		if data, ok := charges["data"].([]interface{}); ok && len(data) > 0 {
			// In a real implementation, you'd fetch the balance transaction from the charge
			// For now, we'll use a placeholder fee calculation
			calculatedFee := amountFloat * 0.029 // 2.9% Stripe fee
			if strings.ToUpper(currency) == "USD" {
				calculatedFee += 0.30 // Add fixed fee for USD
			}
			fee = &calculatedFee
		}
	}

	// Map Stripe status to our status
	ourStatus := "pending"
	switch status {
	case "succeeded":
		ourStatus = "success"
	case "failed", "canceled":
		ourStatus = "failed"
	}

	return &VerifyPaymentResponse{
		ProviderTxnID: id,
		Status:        ourStatus,
		Amount:        amountFloat,
		Currency:      strings.ToUpper(currency),
		Fee:           fee,
		ProcessedAt:   time.Now(),
		Metadata:      eventData,
	}, nil
}

// RefundPayment refunds a Stripe payment using the charge ID
func (sp *StripeProvider) RefundPayment(ctx context.Context, req *RefundRequest) error {
	if req.ChargeID == "" {
		return fmt.Errorf("stripe charge ID is required for refunds")
	}

	amountCents := int64(req.Amount * 100)

	params := &stripe.RefundParams{
		Charge: stripe.String(req.ChargeID),
		Amount: &amountCents,
		Reason: stripe.String(req.Reason),
	}

	_, err := refund.New(params)
	if err != nil {
		log.Printf("[STRIPE] Failed to create refund: %v", err)
		return fmt.Errorf("failed to create refund: %w", err)
	}

	return nil
}

// VerifyWebhook verifies Stripe webhook signatures and extracts event data
func (sp *StripeProvider) VerifyWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error) {
	// Parse the event from the payload
	event, err := webhook.ConstructEvent(payload, signature, sp.webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to verify webhook signature: %w", err)
	}

	// Convert Stripe event data to map
	dataMap := map[string]interface{}{
		"object": event.Data.Object,
	}

	// Convert Stripe event to our WebhookEvent format
	webhookEvent := &WebhookEvent{
		Gateway:   "STRIPE",
		EventID:   event.ID,
		Type:      event.Type,
		Data:      dataMap,
		CreatedAt: time.Unix(event.Created, 0),
	}

	return webhookEvent, nil
}
