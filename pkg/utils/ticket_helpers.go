package utils

import (
	"fmt"
	"strings"
)

// GenerateTicketNumber creates a structured ticket number using tier name, event year, and sequential number
// Format: {TIER_NAME}-{YEAR}-{SEQUENTIAL_NUMBER} (zero padded to 5 digits for consistency)
// Example: VIP-2026-00001
func GenerateTicketNumber(tierName string, eventYear int, soldCount int, sequenceOffset int) string {
	// Sanitize tier name to alphanumeric uppercase (keep letters and digits)
	sanitize := func(s string) string {
		s = strings.ToUpper(strings.ReplaceAll(s, " ", ""))
		// keep only alnum
		out := make([]rune, 0, len(s))
		for _, r := range s {
			if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				out = append(out, r)
			}
		}
		if len(out) == 0 {
			return "T"
		}
		return string(out)
	}

	abbr := sanitize(tierName)
	// Use fixed width of 5 digits to handle large numbers (up to 99999 tickets per tier)
	// This provides consistency and handles cases where more tickets are sold than event capacity
	width := 5
	seq := soldCount + sequenceOffset + 1
	padded := fmt.Sprintf("%0*d", width, seq)
	return fmt.Sprintf("%s-%d-%s", abbr, eventYear, padded)
}
