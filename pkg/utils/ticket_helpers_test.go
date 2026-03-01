package utils

import (
	"strings"
	"testing"
)

// TestGenerateRandomToken tests the random token generation
func TestGenerateRandomToken(t *testing.T) {
	token1, err := generateRandomToken(8)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	if len(token1) != 8 {
		t.Errorf("Token length is %d, expected 8", len(token1))
	}

	// Check that token contains only valid characters
	validChars := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for _, char := range token1 {
		if !strings.ContainsRune(validChars, char) {
			t.Errorf("Token contains invalid character: %c", char)
		}
	}

	// Generate another token and ensure they're different (very high probability)
	token2, err := generateRandomToken(8)
	if err != nil {
		t.Fatalf("Failed to generate second token: %v", err)
	}

	if token1 == token2 {
		t.Errorf("Generated identical tokens: %s", token1)
	}

	t.Logf("Generated tokens: %s, %s", token1, token2)
}
