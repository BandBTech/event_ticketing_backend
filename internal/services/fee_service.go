package services

import (
	"fmt"
	"log"
	"math"

	"event-ticketing-backend/internal/models"

	"github.com/stripe/stripe-go/v83"
)

// FeeSnapshot represents actual gateway fee
type FeeSnapshot struct {
	GatewayFee int64
	Currency   string
	Gateway    models.PaymentGateway
}

// FeeExtractor extracts fees safely
type FeeExtractor struct{}

func NewFeeExtractor() *FeeExtractor {
	return &FeeExtractor{}
}

// ================================
// MAIN ENTRY
// ================================

func (fe *FeeExtractor) ExtractFromWebhook(eventType string, eventData interface{}) (*FeeSnapshot, error) {

	switch eventType {

	case "charge.succeeded":
		return fe.extractFromCharge(eventData)

	default:
		return nil, nil
	}
}

// ================================
// STRIPE CHARGE (REAL FEE SOURCE)
// ================================

func (fe *FeeExtractor) extractFromCharge(eventData interface{}) (*FeeSnapshot, error) {

	charge, ok := eventData.(stripe.Charge)
	if !ok {
		return nil, fmt.Errorf("invalid charge data")
	}

	var gatewayFee int64

	// ✔ BEST SOURCE: Stripe balance transaction
	if charge.BalanceTransaction != nil {
		//log this for debugging but DO NOT use for fee calculation (use Net and Amount for accuracy)
		log.Printf("Stripe Charge: Amount=%d, BalanceTransaction.Net=%d, BalanceTransaction.Fee=%d\n",
			charge.Amount, charge.BalanceTransaction.Net, charge.BalanceTransaction.Fee)
		gatewayFee = charge.Amount - charge.BalanceTransaction.Net
	} else {
		// fallback estimation (NEVER preferred)
		gatewayFee = fe.estimateStripeFee(charge.Amount)
	}

	return &FeeSnapshot{
		GatewayFee: gatewayFee,
		Currency:   string(charge.Currency),
		Gateway:    models.PaymentGatewayStripe,
	}, nil
}

// ================================
// FALLBACK STRIPE ESTIMATION
// ================================

func (fe *FeeExtractor) estimateStripeFee(amount int64) int64 {

	// Stripe: 2.9% + 30 cents
	percent := float64(amount) * 0.029
	fixed := int64(30)

	// No rounding — direct cast for precision
	log.Printf("Estimated Stripe Fee: %d\n", int64(percent)+fixed)
	return int64(percent) + fixed
}

// ================================
// PLATFORM FEE (FIXED)
// ================================

func (fe *FeeExtractor) CalculatePlatformFee(amount int64, currencyCode string, commissionRate float64) (int64, error) {

	fee := float64(amount) * (commissionRate / 100.0)

	// already in smallest unit → NO rounding, direct cast for precision
	return int64(math.Round(fee)), nil
}

// ================================
// GATEWAY FALLBACK
// ================================

func (fe *FeeExtractor) EstimateGatewayFee(amount int64, currencyCode string, gateway models.PaymentGateway) (int64, error) {

	switch gateway {

	case models.PaymentGatewayStripe:
		return int64(float64(amount) * 0.036), nil

	case models.PaymentGatewayKonbini:
		// Japan convenience store fixed fee example: 3.6%
		return int64(float64(amount) * 0.036), nil

	case models.PaymentGatewayPayPay:
		// PayPay ~3.98%
		return int64(float64(amount) * 0.0398), nil

	default:
		// safe default fallback
		return int64(float64(amount) * 0.025), nil
	}
}
