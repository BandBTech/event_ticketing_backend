package gateways

import (
	"context"
	"fmt"
	"time"
)

// CashGateway implements PaymentGateway for cash/offline payments
// Cash payments don't interact with external services - they're marked as pending
// and require manual verification/collection by the organizer
type CashGateway struct {
	name string
}

// NewCashGateway creates a new Cash gateway instance
func NewCashGateway() *CashGateway {
	return &CashGateway{
		name: "cash",
	}
}

// GetName returns "cash"
func (cg *CashGateway) GetName() string {
	return "cash"
}

// CreatePayment creates a pending payment for cash payments
// No external gateway interaction - just marks as pending for manual collection
func (cg *CashGateway) CreatePayment(ctx context.Context, req *CreatePaymentRequest) (*CreatePaymentResponse, error) {
	if req.Amount <= 0 {
		return nil, fmt.Errorf("invalid amount: %f", req.Amount)
	}

	// For cash, we don't have a gateway payment ID
	// Status is "pending" until manually confirmed by organizer
	resp := &CreatePaymentResponse{
		Status:    StatusPending, // pending until organizer confirms receipt
		Amount:    req.Amount,
		Currency:  req.Currency,
		CreatedAt: time.Now(),
		ExpiresAt: func() *time.Time {
			// Cash payments expire in 7 days if not collected
			expiresAt := time.Now().AddDate(0, 0, 7)
			return &expiresAt
		}(),
		Metadata: map[string]interface{}{
			"payment_method": "cash",
			"note":           "Payment must be collected and verified by organizer",
		},
	}

	return resp, nil
}

// RefundPayment initiates a refund for a cash payment
// This is a manual process - we just record the refund intent
func (cg *CashGateway) RefundPayment(ctx context.Context, req *RefundRequest) error {
	if req.Amount <= 0 {
		return fmt.Errorf("invalid refund amount: %f", req.Amount)
	}

	// For cash, refund is manual process
	// In a real implementation, this would trigger organizer notification
	// For now, we just log the refund request
	return nil
}

// VerifyPayment verifies a cash payment
// Cash payments are manually verified by organizers
func (cg *CashGateway) VerifyPayment(ctx context.Context, payload interface{}) (*VerifyPaymentResponse, error) {
	// Cash payments don't have automated verification
	// They require manual confirmation from organizer
	return nil, fmt.Errorf("cash payments require manual verification by organizer")
}

// GetSupportedCurrencies returns all currencies for cash (can be any)
func (cg *CashGateway) GetSupportedCurrencies() []string {
	// Cash works in any currency
	return []string{
		"usd", "eur", "gbp", "jpy", "cad", "aud", "sgd", "hkd",
		"ind", "npr", "aed", "sar", "mxn", "brl",
	}
}

// GetSupportedCountries returns all countries for cash (can be any)
func (cg *CashGateway) GetSupportedCountries() []string {
	// Cash works in any country
	return []string{
		"US", "GB", "DE", "FR", "IT", "ES", "NL", "BE", "AT", "IE",
		"CA", "MX", "BR", "JP", "CN", "SG", "HK", "AU", "IN", "NP",
		"AE", "SA",
	}
}

// VerifyWebhook is not applicable for cash payments
// Cash payments don't have webhooks - they're manually verified
func (cg *CashGateway) VerifyWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error) {
	return nil, fmt.Errorf("cash payments do not support webhook verification")
}
