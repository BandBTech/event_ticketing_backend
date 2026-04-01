package utils

import (
	"crypto/rand"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// GenerateEventTicketNumber creates a unique random ticket code for an event
// Format: TT{YY}-{TIER_NAME}-{8_CHAR_RANDOM_TOKEN}
// Example: TT26-Early-Bird-AB7X9KQ2, TT25-VIP-3F9P2M8L
// Uses random generation to avoid race conditions and provide better security
func GenerateEventTicketNumber(tx *gorm.DB, tierName string, year int) (string, error) {
	// Format tier name: replace spaces with hyphens and convert to uppercase
	formattedTierName := strings.ReplaceAll(strings.TrimSpace(tierName), " ", "-")
	if formattedTierName == "" {
		formattedTierName = "GENERAL" // Default tier name
	}
	formattedTierName = strings.ToUpper(formattedTierName)

	// Ensure tier name doesn't make ticket number exceed 50 characters
	// Format: TT{YY}-{TIER_NAME}-{8_CHAR_TOKEN} = 2 + 2 + 1 + len(TIER_NAME) + 1 + 8 = 14 + len(TIER_NAME)
	// Max tier name length: 50 - 14 = 36 characters
	const maxTierNameLength = 36
	if len(formattedTierName) > maxTierNameLength {
		formattedTierName = formattedTierName[:maxTierNameLength]
	}

	// Generate unique random token until we find one that doesn't exist
	const maxAttempts = 10
	const tokenLength = 8

	for attempts := 0; attempts < maxAttempts; attempts++ {
		// Generate random 8-character token (alphanumeric)
		token, err := generateRandomToken(tokenLength)
		if err != nil {
			return "", fmt.Errorf("failed to generate random token: %w", err)
		}

		// Format: TT{year}-{tier_name}-{token}
		// Use last 2 digits of year for cleaner format
		yearShort := year % 100
		ticketNumber := fmt.Sprintf("TT%02d-%s-%s", yearShort, formattedTierName, token)

		// Double-check length constraint (should not exceed due to truncation above)
		if len(ticketNumber) > 50 {
			return "", fmt.Errorf("generated ticket number too long: %s (%d chars)", ticketNumber, len(ticketNumber))
		}

		// Check if this ticket number already exists
		var count int64
		err = tx.Model(&struct{ TicketNumber string }{}).
			Table("tickets").
			Where("ticket_number = ?", ticketNumber).
			Count(&count).Error

		if err != nil {
			return "", fmt.Errorf("failed to check ticket number uniqueness: %w", err)
		}

		// If unique, return it
		if count == 0 {
			return ticketNumber, nil
		}
	}

	return "", fmt.Errorf("failed to generate unique ticket number after %d attempts", maxAttempts)
}

// generateRandomToken creates a random alphanumeric string of specified length
func generateRandomToken(length int) (string, error) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	bytes := make([]byte, length)

	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	for i, b := range bytes {
		bytes[i] = charset[b%byte(len(charset))]
	}

	return string(bytes), nil
}
