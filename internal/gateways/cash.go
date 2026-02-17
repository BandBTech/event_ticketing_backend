package gateways

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CashGateway implements the PaymentGateway interface for cash payments
type CashGateway struct {
	name string
}

// NewCashGateway creates a new cash payment gateway instance
func NewCashGateway() PaymentGateway {
	return &CashGateway{
		name: "cash",
	}
}

// CreatePaymentIntent creates a payment intent for cash payment
func (c *CashGateway) CreatePaymentIntent(ctx context.Context, req *PaymentIntentRequest) (*PaymentIntentResponse, error) {
	// For cash payments, we immediately mark as succeeded since payment is collected manually
	paymentID := fmt.Sprintf("cash-%s-%d", uuid.New().String()[:8], time.Now().Unix())

	return &PaymentIntentResponse{
		GatewayPaymentID: paymentID,
		ClientSecret:     "", // No client secret needed for cash
		Status:           "succeeded",
		Amount:           req.Amount,
		Currency:         req.Currency,
		ExpiresAt:        nil, // Cash payments don't expire
	}, nil
}

// GetPaymentIntent retrieves payment intent status (always succeeded for cash)
func (c *CashGateway) GetPaymentIntent(ctx context.Context, gatewayPaymentID string) (*PaymentIntentResponse, error) {
	return &PaymentIntentResponse{
		GatewayPaymentID: gatewayPaymentID,
		ClientSecret:     "",
		Status:           "succeeded",
		ExpiresAt:        nil,
	}, nil
}

// CancelPaymentIntent cancels a cash payment intent (no-op for cash)
func (c *CashGateway) CancelPaymentIntent(ctx context.Context, gatewayPaymentID string) error {
	// Cash payments can't be cancelled once initiated
	return fmt.Errorf("cash payments cannot be cancelled")
}

// CreateRefund creates a refund for cash payment
func (c *CashGateway) CreateRefund(ctx context.Context, req *RefundRequest) (*RefundResponse, error) {
	refundID := fmt.Sprintf("cash-refund-%s-%d", uuid.New().String()[:8], time.Now().Unix())

	return &RefundResponse{
		GatewayRefundID: refundID,
		Status:          "succeeded", // Cash refunds are immediate
		Amount:          req.Amount,
		Currency:        req.Currency,
		Reason:          req.Reason,
		CreatedAt:       time.Now(),
	}, nil
}

// GetRefund retrieves refund status (always succeeded for cash)
func (c *CashGateway) GetRefund(ctx context.Context, gatewayRefundID string) (*RefundResponse, error) {
	return &RefundResponse{
		GatewayRefundID: gatewayRefundID,
		Status:          "succeeded",
		CreatedAt:       time.Now(),
	}, nil
}

// VerifyWebhook verifies webhook (not applicable for cash)
func (c *CashGateway) VerifyWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error) {
	return nil, fmt.Errorf("webhooks not supported for cash payments")
}

// GetSupportedCurrencies returns supported currencies for cash payments
func (c *CashGateway) GetSupportedCurrencies() []string {
	// Cash supports all currencies
	return []string{"USD", "EUR", "GBP", "NPR", "INR", "CAD", "AUD"}
}

// GetSupportedCountries returns supported countries for cash payments
func (c *CashGateway) GetSupportedCountries() []string {
	// Cash is available everywhere
	return []string{} // Empty means all countries
}

// GetName returns the gateway name
func (c *CashGateway) GetName() string {
	return c.name
}

// CalculateFees calculates fees for cash payments (always 0)
func (c *CashGateway) CalculateFees(amount float64, currency string) float64 {
	// No fees for cash payments
	return 0
}
