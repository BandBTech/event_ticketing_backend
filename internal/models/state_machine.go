package models

import (
	"fmt"
)

// PaymentStateMachine enforces strict state transitions for payments
// This prevents Stripe out-of-order events from corrupting payment state
//
// Valid transitions:
//
//	pending → succeeded (FINAL)
//	pending → failed (FINAL)
//	succeeded → IMMUTABLE (any attempt rejected)
//	failed → IMMUTABLE (any attempt rejected)
type PaymentStateMachine struct {
	currentStatus string
}

// NewPaymentStateMachine creates state machine for a payment status
func NewPaymentStateMachine(currentStatus string) *PaymentStateMachine {
	return &PaymentStateMachine{currentStatus: currentStatus}
}

// TransitionToSucceeded attempts transition to "succeeded"
// CRITICAL: This is the only valid path to mark payment as completed
func (sm *PaymentStateMachine) TransitionToSucceeded() error {
	switch sm.currentStatus {
	case "pending":
		// ✅ VALID: pending → succeeded (expected path)
		sm.currentStatus = "succeeded"
		return nil

	case "succeeded":
		// ⚠️ IDEMPOTENT: Already succeeded, no state change needed
		// This is OK - return early without error
		return ErrAlreadySucceeded

	case "failed", "cancelled", "refunded", "expired":
		// ❌ INVALID: Cannot go from terminal state back to succeeded
		// Likely Stripe out-of-order event or webhook replay issue
		return fmt.Errorf("invalid state transition: %s → succeeded (terminal state cannot go back)", sm.currentStatus)

	case "processing":
		// ❌ INVALID: Cannot transition from processing directly to succeeded
		// This indicates another worker is still processing
		return fmt.Errorf("invalid state transition: %s → succeeded (still processing by another worker)", sm.currentStatus)

	default:
		return fmt.Errorf("invalid state transition: unknown current state '%s'", sm.currentStatus)
	}
}

// TransitionToFailed attempts transition to "failed"
// This marks payment as permanently failed
func (sm *PaymentStateMachine) TransitionToFailed() error {
	switch sm.currentStatus {
	case "pending":
		// ✅ VALID: pending → failed (expected path)
		sm.currentStatus = "failed"
		return nil

	case "failed":
		// ⚠️ IDEMPOTENT: Already failed, no state change needed
		return ErrAlreadyFailed

	case "succeeded", "completed":
		// ❌ CRITICAL: Cannot fail an already-succeeded payment
		// This would orphan tickets and cause accounting mismatch
		return fmt.Errorf("CRITICAL: invalid state transition: %s → failed (payment already succeeded, tickets created)", sm.currentStatus)

	case "cancelled", "refunded", "expired":
		// ❌ INVALID: Cannot go from one terminal state to another
		return fmt.Errorf("invalid state transition: %s → failed (already in terminal state %s)", sm.currentStatus, sm.currentStatus)

	case "processing":
		// ❌ INVALID: Cannot fail while still being processed
		return fmt.Errorf("invalid state transition: %s → failed (still processing)", sm.currentStatus)

	default:
		return fmt.Errorf("invalid state transition: unknown current state '%s'", sm.currentStatus)
	}
}

// GetStatus returns the current state
func (sm *PaymentStateMachine) GetStatus() string {
	return sm.currentStatus
}

// IsTerminal returns true if state is immutable
func (sm *PaymentStateMachine) IsTerminal() bool {
	return sm.currentStatus == "succeeded" ||
		sm.currentStatus == "completed" ||
		sm.currentStatus == "failed" ||
		sm.currentStatus == "cancelled" ||
		sm.currentStatus == "refunded" ||
		sm.currentStatus == "expired"
}

// IsSucceeded returns true if payment succeeded
func (sm *PaymentStateMachine) IsSucceeded() bool {
	return sm.currentStatus == "succeeded" || sm.currentStatus == "completed"
}

// IsFailed returns true if payment failed
func (sm *PaymentStateMachine) IsFailed() bool {
	return sm.currentStatus == "failed" ||
		sm.currentStatus == "cancelled" ||
		sm.currentStatus == "refunded"
}

// Custom errors for idempotent behavior
var (
	ErrAlreadySucceeded = fmt.Errorf("payment already succeeded (idempotent)")
	ErrAlreadyFailed    = fmt.Errorf("payment already failed (idempotent)")
)
