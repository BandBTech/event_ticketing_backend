package helpers

import (
	"fmt"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/state"
)

// PaymentIntentStatusHelpers provides utility functions for working with PaymentIntentStatus enums
// This prevents accidental use of string literals and improves type safety

// IsTerminalStatus checks if a payment intent has reached a terminal state
func IsPaymentIntentTerminal(status models.PaymentIntentStatus) bool {
	switch status {
	case models.PaymentIntentSucceeded,
		models.PaymentIntentCanceled,
		models.PaymentIntentExpired,
		models.PaymentIntentFailed:
		return true
	default:
		return false
	}
}

// IsPaymentIntentPending checks if a payment intent is still awaiting action
func IsPaymentIntentPending(status models.PaymentIntentStatus) bool {
	switch status {
	case models.PaymentIntentRequiresPaymentMethod,
		models.PaymentIntentRequiresConfirmation,
		models.PaymentIntentProcessing:
		return true
	default:
		return false
	}
}

// GetPaymentIntentStatusMessage returns a human-readable message for the given status
func GetPaymentIntentStatusMessage(status models.PaymentIntentStatus) string {
	switch status {
	case models.PaymentIntentSucceeded:
		return "Tickets generated successfully"
	case models.PaymentIntentFailed:
		return "Payment failed"
	case models.PaymentIntentExpired:
		return "Payment intent has expired"
	case models.PaymentIntentRequiresPaymentMethod:
		return "Awaiting payment method"
	case models.PaymentIntentRequiresConfirmation:
		return "Awaiting payment confirmation"
	case models.PaymentIntentProcessing:
		return "Payment processing in progress"
	case models.PaymentIntentCanceled:
		return "Payment was canceled"
	default:
		return "Unknown status"
	}
}

// IsPaymentIntentSuccessful checks if payment was successfully completed
func IsPaymentIntentSuccessful(status models.PaymentIntentStatus) bool {
	return status == models.PaymentIntentSucceeded
}

// IsPaymentIntentFailed checks if payment failed or was canceled/expired
func IsPaymentIntentFailed(status models.PaymentIntentStatus) bool {
	switch status {
	case models.PaymentIntentFailed,
		models.PaymentIntentCanceled,
		models.PaymentIntentExpired:
		return true
	default:
		return false
	}
}

// ValidPaymentIntentStatuses returns all valid PaymentIntentStatus values
func ValidPaymentIntentStatuses() []models.PaymentIntentStatus {
	return []models.PaymentIntentStatus{
		models.PaymentIntentRequiresPaymentMethod,
		models.PaymentIntentRequiresConfirmation,
		models.PaymentIntentProcessing,
		models.PaymentIntentSucceeded,
		models.PaymentIntentCanceled,
		models.PaymentIntentExpired,
		models.PaymentIntentFailed,
	}
}

// ValidatePaymentIntentTransition checks if a transition from 'from' to 'to' is valid
// Returns nil if valid, error if invalid
func ValidatePaymentIntentTransition(from, to models.PaymentIntentStatus) error {
	validTransitions := state.PaymentIntentTransitions
	allowedDestinations, exists := validTransitions[from]

	if !exists {
		return fmt.Errorf("unknown payment intent status: %v", from)
	}

	for _, destination := range allowedDestinations {
		if destination == to {
			return nil // Valid transition
		}
	}

	return fmt.Errorf("invalid payment intent transition: %v → %v", from, to)
}

// GetAllowedTransitions returns all valid next states from a given status
func GetAllowedPaymentIntentTransitions(from models.PaymentIntentStatus) ([]models.PaymentIntentStatus, error) {
	validTransitions := state.PaymentIntentTransitions
	allowedDestinations, exists := validTransitions[from]

	if !exists {
		return nil, fmt.Errorf("unknown payment intent status: %v", from)
	}

	return allowedDestinations, nil
}
