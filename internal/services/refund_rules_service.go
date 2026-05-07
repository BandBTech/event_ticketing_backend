package services

import (
	"event-ticketing-backend/internal/models"
	"fmt"

	"github.com/google/uuid"
)

// Refund type constants
const (
	RefundEventCancelled = "event_cancelled"
	RefundUserRequested  = "user_requested"
	RefundSystemError    = "system_error"
	RefundPaymentFailed  = "payment_failed"
)

type RefundRule struct {
	// Fee refund policies
	RefundPlatformFee    bool    // Whether to refund platform fees
	RefundGatewayFee     bool    // Whether to refund gateway fees
	PlatformFeeRetention float64 // Percentage of platform fee to retain (0-100)
	GatewayFeeRetention  float64 // Percentage of gateway fee to retain (0-100)

	// Time-based rules
	MaxRefundDays     int     // Maximum days after purchase for full refund
	PartialRefundRate float64 // Refund percentage after MaxRefundDays

	// Event-based rules
	AllowEventCancellation bool    // Allow refunds for cancelled events
	EventCancellationRate  float64 // Refund percentage for event cancellation

	// Minimum refund amount
	MinRefundAmount int64 // Minimum refund amount in smallest units
}

// RefundRulesConfig contains rules for different scenarios
type RefundRulesConfig struct {
	Default         RefundRule
	EventCancelled  RefundRule
	UserInitiated   RefundRule
	SystemInitiated RefundRule
}

// DefaultRefundRules provides the centralized refund configuration
var DefaultRefundRules = RefundRulesConfig{
	Default: RefundRule{
		RefundPlatformFee:      false, // Platform keeps commission
		RefundGatewayFee:       false, // Gateway fees are non-refundable
		PlatformFeeRetention:   100,   // Keep 100% of platform fee
		GatewayFeeRetention:    100,   // Keep 100% of gateway fee
		MaxRefundDays:          7,     // 7 days for full refund
		PartialRefundRate:      50,    // 50% refund after 7 days
		AllowEventCancellation: true,
		EventCancellationRate:  100, // Full refund for cancelled events
		MinRefundAmount:        100, // Minimum $1.00 refund
	},

	EventCancelled: RefundRule{
		RefundPlatformFee:     true, // Refund platform fee for cancelled events
		RefundGatewayFee:      true, // Refund gateway fee for cancelled events
		PlatformFeeRetention:  0,    // Return 100% of platform fee
		GatewayFeeRetention:   0,    // Return 100% of gateway fee
		EventCancellationRate: 100,  // Full refund
		MinRefundAmount:       0,    // No minimum for event cancellation
	},

	UserInitiated: RefundRule{
		RefundPlatformFee:    false,
		RefundGatewayFee:     false,
		PlatformFeeRetention: 100,
		GatewayFeeRetention:  100,
		MaxRefundDays:        7,
		PartialRefundRate:    50,
		MinRefundAmount:      100,
	},

	SystemInitiated: RefundRule{
		RefundPlatformFee:    false,
		RefundGatewayFee:     false,
		PlatformFeeRetention: 100,
		GatewayFeeRetention:  100,
		MaxRefundDays:        30,  // More lenient for system refunds
		PartialRefundRate:    100, // Full refund for system issues
		MinRefundAmount:      0,
	},
}

// RefundCalculator calculates refund amounts based on rules
type RefundCalculator struct {
	rules RefundRulesConfig
}

// NewRefundCalculator creates a new refund calculator
func NewRefundCalculator() *RefundCalculator {
	return &RefundCalculator{
		rules: DefaultRefundRules,
	}
}

// CalculateRefund calculates the refund amount for a transaction
func (rc *RefundCalculator) CalculateRefund(
	txn *models.Transaction,
	ticketIDs []uuid.UUID,
) (int64, error) {
	// For now, use a simple rule: refund the full amount minus fees
	// This can be enhanced later with more sophisticated rules based on time, reason, etc.

	// Calculate base refund amount (total - fees)
	baseRefund := txn.AmountTotal - txn.PlatformFee - txn.GatewayFee

	// If specific tickets are being refunded, calculate proportionally
	if len(ticketIDs) > 0 && len(ticketIDs) < txn.Quantity {
		// Partial refund - calculate based on number of tickets
		ticketRatio := float64(len(ticketIDs)) / float64(txn.Quantity)
		baseRefund = int64(float64(baseRefund) * ticketRatio)
	}

	// Apply minimum refund rule (no refunds less than $1)
	if baseRefund < 100 { // 100 cents = $1
		return 0, fmt.Errorf("refund amount too small: %d cents", baseRefund)
	}

	return baseRefund, nil
}

// UpdateRules allows updating refund rules (for admin configuration)
func (rc *RefundCalculator) UpdateRules(newRules RefundRulesConfig) {
	rc.rules = newRules
}

// GetRules returns current refund rules
func (rc *RefundCalculator) GetRules() RefundRulesConfig {
	return rc.rules
}
