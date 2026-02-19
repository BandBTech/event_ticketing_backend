package utils

import (
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GenerateEventTicketNumber creates a sequential ticket number for an event
// Format: {TIER_NAME}-{YEAR}-{5_DIGIT_SEQUENCE}
// Example: VIP-2026-00001, GENERAL-2026-00002
// Sequence starts from 1 for each event and increments sequentially
// This function MUST be called within a transaction to ensure uniqueness
func GenerateEventTicketNumber(tx *gorm.DB, eventID uuid.UUID, tierName string, year int) (string, error) {
	// Count existing tickets for this event within the transaction
	// This provides an atomic, sequential number per event
	var count int64
	if err := tx.Model(&struct {
		ID uuid.UUID `gorm:"column:id"`
	}{}).
		Table("tickets").
		Where("event_id = ?", eventID).
		Count(&count).Error; err != nil {
		return "", fmt.Errorf("failed to count tickets: %w", err)
	}

	// Increment to get the next sequence number (starts from 1)
	sequence := count + 1

	// Format: TIERNAME-YEAR-SEQUENCE (5 digits, zero-padded)
	ticketNumber := fmt.Sprintf("%s-%d-%05d", tierName, year, sequence)

	return ticketNumber, nil
}
