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

// CreatePaymentIntent creates a pending payment intent for cash payments
// No external gateway interaction - just marks as pending for manual collection
func (cg *CashGateway) CreatePaymentIntent(ctx context.Context, req *PaymentIntentRequest) (*PaymentIntentResponse, error) {
	if req.Amount <= 0 {
		return nil, fmt.Errorf("invalid amount: %f", req.Amount)
	}

	// For cash, we don't have a gateway payment ID
	// Status is "pending" until manually confirmed by organizer
	resp := &PaymentIntentResponse{
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

// CreateRefund creates a refund for a cash payment
// This is a manual process - we just record the refund intent
func (cg *CashGateway) CreateRefund(ctx context.Context, req *RefundRequest) (*RefundResponse, error) {
	if req.Amount <= 0 {
		return nil, fmt.Errorf("invalid refund amount: %f", req.Amount)
	}

	// For cash, refund is manual process
	// Generate a refund ID based on timestamp and reason
	refundID := fmt.Sprintf("CASH_REFUND_%d", time.Now().UnixNano())

	resp := &RefundResponse{
		GatewayRefundID: refundID,
		Status:          RefundStatusPending, // pending until organizer processes
		Amount:          req.Amount,
		Currency:        req.Currency,
		Reason:          req.Reason,
		CreatedAt:       time.Now(),
	}

	return resp, nil
}

// GetRefund retrieves a cash refund status
// Cache or database lookup would be needed in real implementation
func (cg *CashGateway) GetRefund(ctx context.Context, gatewayRefundID string) (*RefundResponse, error) {
	// In a real implementation, would query database for refund details
	// For now, return not found as cash refunds require manual lookups
	return nil, fmt.Errorf("cash refund lookup not implemented - must be checked manually")
}

// VerifyWebhook is not applicable for cash payments
// Cash payments don't have webhooks - they're manually verified
func (cg *CashGateway) VerifyWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error) {
	return nil, fmt.Errorf("cash payments do not support webhook verification")
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

// CalculateFees returns 0 fees for cash payments
// Cash payments may have collection fees handled separately by organizer
func (cg *CashGateway) CalculateFees(amount float64, currency string) float64 {
	// No gateway fees for cash - organizer covers collection costs
	return 0
}
