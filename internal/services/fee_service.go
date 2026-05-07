package services

import (
	"fmt"
	"math"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/currency"

	"github.com/stripe/stripe-go/v83"
)

// FeeSnapshot represents the actual fees charged by a payment gateway
type FeeSnapshot struct {
	GatewayFee  int64  // Actual fee charged by gateway (in smallest units)
	PlatformFee int64  // Platform commission (in smallest units)
	Currency    string // Currency of the fees
	Gateway     models.PaymentGateway
}

// FeeExtractor extracts actual fees from webhook events
type FeeExtractor struct{}

// NewFeeExtractor creates a new fee extractor
func NewFeeExtractor() *FeeExtractor {
	return &FeeExtractor{}
}

// ExtractFromWebhook extracts actual fees from webhook event data
func (fe *FeeExtractor) ExtractFromWebhook(eventType string, eventData interface{}) (*FeeSnapshot, error) {
	switch eventType {
	case "charge.succeeded":
		return fe.extractFromCharge(eventData)
	case "checkout.session.completed":
		return fe.extractFromCheckoutSession(eventData)
	case "payment_intent.succeeded":
		return fe.extractFromPaymentIntent(eventData)
	default:
		return nil, fmt.Errorf("unsupported event type for fee extraction: %s", eventType)
	}
}

// extractFromCharge extracts fees from Stripe charge object
func (fe *FeeExtractor) extractFromCharge(eventData interface{}) (*FeeSnapshot, error) {
	charge, ok := eventData.(stripe.Charge)
	if !ok {
		return nil, fmt.Errorf("invalid charge data")
	}

	// Stripe provides fee in the balance_transaction
	// The fee is the difference between amount and net
	var gatewayFee int64
	if charge.BalanceTransaction != nil {
		gatewayFee = charge.Amount - charge.BalanceTransaction.Net
	} else {
		// If no balance transaction, estimate based on typical Stripe fees
		gatewayFee = int64(float64(charge.Amount)*0.029) + 30 // 2.9% + 30¢
	}

	return &FeeSnapshot{
		GatewayFee: gatewayFee,
		Currency:   string(charge.Currency),
		Gateway:    models.PaymentGatewayStripe,
	}, nil
}

// extractFromCheckoutSession extracts fees from checkout session
func (fe *FeeExtractor) extractFromCheckoutSession(eventData interface{}) (*FeeSnapshot, error) {
	_, ok := eventData.(stripe.CheckoutSession)
	if !ok {
		return nil, fmt.Errorf("invalid checkout session data")
	}

	// For checkout sessions, we might not have direct fee info
	// Return nil to indicate fee should be calculated later from charge event
	return nil, nil
}

// extractFromPaymentIntent extracts fees from payment intent
func (fe *FeeExtractor) extractFromPaymentIntent(eventData interface{}) (*FeeSnapshot, error) {
	_, ok := eventData.(stripe.PaymentIntent)
	if !ok {
		return nil, fmt.Errorf("invalid payment intent data")
	}

	// Payment intents don't contain fee info directly
	// Return nil to indicate fee should be calculated later
	return nil, nil
}

// CalculatePlatformFee calculates platform commission
func (fe *FeeExtractor) CalculatePlatformFee(amount int64, currencyCode string, commissionRate float64) (int64, error) {
	commissionRateDecimal := commissionRate / 100.0
	platformFeeFloat := float64(amount) * commissionRateDecimal

	// Convert to smallest currency unit
	platformFee, err := currency.ToSmallestUnit(platformFeeFloat, currencyCode)
	if err != nil {
		return 0, fmt.Errorf("failed to convert platform fee to smallest unit: %w", err)
	}

	return platformFee, nil
}

// EstimateGatewayFee provides fallback fee estimation when actual fees aren't available
func (fe *FeeExtractor) EstimateGatewayFee(amount int64, currencyCode string, gateway models.PaymentGateway) (int64, error) {
	switch gateway {
	case models.PaymentGatewayStripe:
		// Stripe: 2.9% + 30¢
		feePercent := float64(amount) * 0.029
		feeFixed := float64(30) // 30¢ in USD
		feeFixedSmallest, _ := currency.ToSmallestUnit(feeFixed, "USD")
		return int64(math.Round(feePercent)) + feeFixedSmallest, nil

	case models.PaymentGatewayKonbini:
		// Konbini: Fixed fee (example: ¥100)
		feeFixed := float64(100) // 100 JPY
		feeFixedSmallest, _ := currency.ToSmallestUnit(feeFixed, "JPY")
		return feeFixedSmallest, nil

	default:
		// Default: 2.5% fee
		feePercent := float64(amount) * 0.025
		return int64(math.Round(feePercent)), nil
	}
}
