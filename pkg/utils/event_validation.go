package utils

import (
	"time"

	"event-ticketing-backend/internal/models"

	"gorm.io/gorm"
)

// ValidateEventPurchaseEligibility checks if an event allows ticket purchases
// This function validates multiple conditions that could prevent ticket purchases:
// - Event must exist
// - Event must not be cancelled
// - Event sales must not be paused or stopped
// - Event must be approved
// - Specified tier must exist and have available tickets
// - Current time must be within the tier's sales window (SalesStart to SalesEnd)
func ValidateEventPurchaseEligibility(db *gorm.DB, eventID string, tierID string) error {
	var event models.Event
	if err := db.First(&event, "id = ?", eventID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return NewNotFoundError("event")
		}
		return NewDatabaseError("Failed to retrieve event.", err)
	}

	// Check if event is cancelled
	if event.IsCancelled || event.Status == "cancelled" {
		return NewBusinessLogicError("Ticket purchases are not allowed for cancelled events.")
	}

	// Check sales status
	if event.SalesStatus == "paused" || event.SalesStatus == "stopped" {
		return NewBusinessLogicError("Ticket purchases are currently not available for this event.")
	}

	// Check if event status allows purchases (only approved events should allow purchases)
	if event.Status != "approved" {
		return NewBusinessLogicError("Event is not available for ticket purchases.")
	}

	// Check if the specified tier exists and has available tickets
	var tier models.EventTier
	if err := db.First(&tier, "id = ? AND event_id = ?", tierID, eventID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return NewNotFoundError("ticket tier")
		}
		return NewDatabaseError("Failed to retrieve ticket tier.", err)
	}

	// Check if tier is active
	if !tier.IsActive {
		return NewBusinessLogicError("This ticket tier is not available for purchase.")
	}

	// Check if tickets are available for this tier
	if tier.Available <= 0 {
		return NewBusinessLogicError("No tickets are available for the selected tier.")
	}

	// Check tier sales time window
	now := time.Now()
	if tier.SalesStart != nil && now.Before(*tier.SalesStart) {
		return NewBusinessLogicError("Ticket sales for this tier have not started yet.")
	}
	if tier.SalesEnd != nil && now.After(*tier.SalesEnd) {
		return NewBusinessLogicError("Ticket sales for this tier have ended.")
	}

	return nil
}
