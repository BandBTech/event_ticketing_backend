package services

import (
	"event-ticketing-backend/internal/models"
	"fmt"
	"time"

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

	// Central ticket cancellation policy.
	// Adjust this value here to change the user cancellation cutoff everywhere.
	UserCancellationWindowBeforeStart time.Duration
	BlockUserCancellationAfterEnd     bool
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

	UserCancellationWindowBeforeStart: 2 * time.Hour,
	BlockUserCancellationAfterEnd:     true,
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
	if baseRefund < rc.rules.Default.MinRefundAmount {
		return 0, fmt.Errorf("refund amount too small: %d cents", baseRefund)
	}

	return baseRefund, nil
}

// CalculateRefundForTicket calculates the refund amount for a single ticket
// based on the transaction's per-ticket price minus platform/gateway fees.
func (rc *RefundCalculator) CalculateRefundForTicket(txn *models.Transaction) (int64, error) {
	if txn.Quantity <= 0 {
		return 0, fmt.Errorf("transaction has invalid quantity: %d", txn.Quantity)
	}

	// Per-ticket base amount = (total - fees) / quantity
	netAmount := txn.AmountTotal - txn.PlatformFee - txn.GatewayFee
	perTicket := netAmount / int64(txn.Quantity)

	if perTicket < rc.rules.UserInitiated.MinRefundAmount {
		return 0, fmt.Errorf("per-ticket refund amount too small: %d cents", perTicket)
	}

	return perTicket, nil
}

func (rc *RefundCalculator) UpdateRules(newRules RefundRulesConfig) {
	rc.rules = newRules
}

// GetRules returns current refund rules
func (rc *RefundCalculator) GetRules() RefundRulesConfig {
	return rc.rules
}

// ValidateCancellationRequest applies the centralized ownership and time-window rules.
func (rc *RefundCalculator) ValidateCancellationRequest(ticket *models.Ticket, event *models.Event, initiatorID uuid.UUID, isAdmin bool, now time.Time) error {
	if ticket == nil {
		return fmt.Errorf("ticket is required for refund validation")
	}
	if event == nil {
		return fmt.Errorf("event is required for refund validation")
	}

	if !isAdmin && ticket.ActorID != initiatorID {
		return fmt.Errorf("ticket does not belong to this user")
	}

	if isAdmin {
		return nil
	}

	if rc.rules.BlockUserCancellationAfterEnd && !event.EndDate.IsZero() && event.EndDate.Before(now) {
		return fmt.Errorf("event has already ended")
	}

	window := rc.rules.UserCancellationWindowBeforeStart
	if window > 0 {
		cutoff := event.StartDate.Add(-window)
		if now.After(cutoff) {
			return fmt.Errorf("refund window has closed (must be more than %s before event start)", window)
		}
	}

	return nil
}

// ValidateCancellationState applies centralized ticket/refund state rules.
func (rc *RefundCalculator) ValidateCancellationState(ticketStatus models.TicketStatus, checkInCount int64, existingRefund *models.Refund, refundFound bool) error {
	switch ticketStatus {
	case models.TicketRefunded:
		return fmt.Errorf("ticket has already been refunded")
	case models.TicketCanceled:
		return fmt.Errorf("ticket is already cancelled")
	case models.TicketCheckedIn:
		return fmt.Errorf("ticket has already been checked in")
	case models.TicketPendingRefund:
		return fmt.Errorf("ticket has already been partially refunded")
	}

	if checkInCount > 0 {
		return fmt.Errorf("ticket has already been checked in")
	}

	if !refundFound || existingRefund == nil {
		return nil
	}

	switch existingRefund.Status {
	case models.RefundPending:
		return fmt.Errorf("A refund for this ticket is pending approval. Please wait for admin review or reject the existing refund to create a new one.")
	case models.RefundProcessing:
		return fmt.Errorf("A refund for this ticket is currently being processed. Please wait for completion.")
	case models.RefundSucceeded:
		return fmt.Errorf("A refund for this ticket has already been successfully processed.")
	case models.RefundRejected:
		return nil
	case models.RefundFailed:
		return fmt.Errorf("The previous refund for this ticket failed. Please use the retry function or contact support.")
	case models.RefundCancelled:
		return fmt.Errorf("The refund for this ticket has been cancelled. Contact support to request a new refund.")
	default:
		return fmt.Errorf("A refund for this ticket already exists with status: %s", existingRefund.Status)
	}
}
