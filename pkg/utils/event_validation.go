package utils

// ValidateEventPurchaseEligibility checks if an event allows ticket purchases for multiple tiers
// This function validates multiple conditions that could prevent ticket purchases:
// - Event must exist
// - Event must not be cancelled
// - Event sales must not be paused or stopped
// - Event must be approved
// - All specified tiers must exist and have available tickets
// - Current time must be within each tier's sales window (SalesStart to SalesEnd)
// func ValidateEventPurchaseEligibilityForTiers(db *gorm.DB, eventID string, tierSelections []models.TicketTierSelection) error {
// 	var event models.Event
// 	if err := db.First(&event, "id = ?", eventID).Error; err != nil {
// 		if err == gorm.ErrRecordNotFound {
// 			return NewNotFoundError("event")
// 		}
// 		return NewDatabaseError("Failed to retrieve event.", err)
// 	}

// 	// Check if event is cancelled
// 	if event.IsCancelled || event.Status == "cancelled" {
// 		return NewBusinessLogicError("Ticket purchases are not allowed for cancelled events.")
// 	}

// 	// Check sales status
// 	if event.SalesStatus == "paused" || event.SalesStatus == "stopped" {
// 		return NewBusinessLogicError("Ticket purchases are currently not available for this event.")
// 	}

// 	// Check if event status allows purchases (approved or on_sale events should allow purchases)
// 	// Scheduled events ARE viewable but NOT purchasable
// 	if event.Status == "scheduled" {
// 		return NewBusinessLogicError("This event is scheduled but ticket sales have not opened yet. Please check back soon!")
// 	}
// 	if event.Status != "approved" && event.Status != "on_sale" {
// 		return NewBusinessLogicError("Event is not available for ticket purchases.")
// 	}

// 	// Validate each tier selection
// 	for _, tierSelection := range tierSelections {
// 		var tier models.EventTier
// 		if err := db.First(&tier, "id = ? AND event_id = ?", tierSelection.TierID, eventID).Error; err != nil {
// 			if err == gorm.ErrRecordNotFound {
// 				return NewNotFoundError("ticket tier")
// 			}
// 			return NewDatabaseError("Failed to retrieve ticket tier.", err)
// 		}

// 		// Check if tier is active
// 		if !tier.IsActive {
// 			return NewBusinessLogicError("This ticket tier is not available for purchase.")
// 		}

// 		// Check if enough tickets are available for this tier
// 		if tier.Available < tierSelection.Quantity {
// 			return NewBusinessLogicError("Insufficient tickets available for the selected tier.")
// 		}

// 		// Check tier sales time window
// 		now := time.Now()
// 		if tier.SalesStart != nil && now.Before(*tier.SalesStart) {
// 			return NewBusinessLogicError("Ticket sales for this tier have not started yet.")
// 		}
// 		if tier.SalesEnd != nil && now.After(*tier.SalesEnd) {
// 			return NewBusinessLogicError("Ticket sales for this tier have ended.")
// 		}
// 	}

// 	return nil
// }

// ValidateEventPurchaseEligibility checks if an event allows ticket purchases (legacy single tier version)
// func ValidateEventPurchaseEligibility(db *gorm.DB, eventID string, tierID string) error {
// 	parsedTierID, err := uuid.Parse(tierID)
// 	if err != nil {
// 		return NewDatabaseError("Invalid tier ID format", err)
// 	}

// 	tierSelections := []models.TicketTierSelection{
// 		{TierID: parsedTierID, Quantity: 1}, // Use quantity 1 for backward compatibility
// 	}
// 	return ValidateEventPurchaseEligibilityForTiers(db, eventID, tierSelections)
// }
