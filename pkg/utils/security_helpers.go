package utils

import (
	"crypto/rand"
	"encoding/base64"
	"regexp"
	"strings"
	"unicode"
)

// Security utility functions for validation and sanitization

// SanitizeInput removes potentially dangerous characters from user input
func SanitizeInput(input string) string {
	// Remove null bytes
	input = strings.ReplaceAll(input, "\x00", "")
	// Trim whitespace
	return strings.TrimSpace(input)
}

// ValidateEmail checks if email format is valid
func ValidateEmail(email string) bool {
	if email == "" {
		return false
	}
	// RFC 5322 compliant regex (simplified)
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}

// ValidatePhone checks basic phone number format
func ValidatePhone(phone string) bool {
	// Remove common separators
	cleaned := strings.ReplaceAll(phone, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "-", "")
	cleaned = strings.ReplaceAll(cleaned, "(", "")
	cleaned = strings.ReplaceAll(cleaned, ")", "")
	cleaned = strings.ReplaceAll(cleaned, "+", "")

	// Check if all remaining chars are digits and length is reasonable
	if len(cleaned) < 10 || len(cleaned) > 15 {
		return false
	}

	for _, char := range cleaned {
		if !unicode.IsDigit(char) {
			return false
		}
	}

	return true
}

// ValidatePassword checks password strength requirements
func ValidatePassword(password string) (bool, string) {
	if len(password) < 8 {
		return false, "Password must be at least 8 characters long"
	}

	hasUpper := false
	hasLower := false
	hasDigit := false
	hasSpecial := false

	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsDigit(char):
			hasDigit = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	if !hasUpper {
		return false, "Password must contain at least one uppercase letter"
	}
	if !hasLower {
		return false, "Password must contain at least one lowercase letter"
	}
	if !hasDigit {
		return false, "Password must contain at least one digit"
	}
	if !hasSpecial {
		return false, "Password must contain at least one special character"
	}

	return true, ""
}

// GenerateSecureToken generates a cryptographically secure random token
func GenerateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length], nil
}

// SanitizeFileName removes dangerous characters from filenames
func SanitizeFileName(filename string) string {
	// Remove path traversal attempts
	filename = strings.ReplaceAll(filename, "../", "")
	filename = strings.ReplaceAll(filename, "..\\", "")

	// Remove null bytes
	filename = strings.ReplaceAll(filename, "\x00", "")

	// Allow only alphanumeric, dash, underscore, and dot
	reg := regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	return reg.ReplaceAllString(filename, "_")
}

// ValidateUUID checks if string is valid UUID format (basic check)
func ValidateUUIDFormat(uuidStr string) bool {
	if len(uuidStr) != 36 {
		return false
	}

	uuidRegex := regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	return uuidRegex.MatchString(uuidStr)
}

// PreventXSS escapes HTML special characters
func PreventXSS(input string) string {
	replacements := map[string]string{
		"<":  "&lt;",
		">":  "&gt;",
		"&":  "&amp;",
		"\"": "&quot;",
		"'":  "&#39;",
	}

	for old, new := range replacements {
		input = strings.ReplaceAll(input, old, new)
	}

	return input
}

// ValidateNumericRange ensures numeric value is within acceptable range
func ValidateNumericRange(value, min, max int) bool {
	return value >= min && value <= max
}

// IsValidURL checks basic URL format
func IsValidURL(url string) bool {
	if url == "" {
		return false
	}
	urlRegex := regexp.MustCompile(`^https?://[a-zA-Z0-9\-._~:/?#\[\]@!$&'()*+,;=%]+$`)
	return urlRegex.MatchString(url)
}

// IsSQLInjectionAttempt performs basic SQL injection detection
func IsSQLInjectionAttempt(input string) bool {
	// Convert to lowercase for case-insensitive matching
	lower := strings.ToLower(input)

	// Common SQL injection patterns
	patterns := []string{
		"'", "\"", "--", ";", "/*", "*/",
		"xp_", "sp_", "exec", "execute",
		"select", "insert", "update", "delete",
		"drop", "create", "alter", "union",
		"||", "&&",
	}

	for _, pattern := range patterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// IsXSSAttempt performs basic XSS detection
func IsXSSAttempt(input string) bool {
	// Convert to lowercase for case-insensitive matching
	lower := strings.ToLower(input)

	// Common XSS patterns
	patterns := []string{
		"<script", "</script>", "javascript:",
		"onerror", "onload", "onclick",
		"<iframe", "<object", "<embed", "<applet",
	}

	for _, pattern := range patterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// SanitizeString removes potentially dangerous characters from user input (alias for SanitizeInput)
func SanitizeString(input string) string {
	return SanitizeInput(input)
}

// ValidatePasswordStrength checks if password meets security requirements (returns error instead of bool)
func ValidatePasswordStrength(password string) error {
	valid, msg := ValidatePassword(password)
	if !valid {
		return NewValidationError(msg, nil)
	}
	return nil
}

// ValidatePhoneNumber checks if phone number is valid (alias for ValidatePhone)
func ValidatePhoneNumber(phone string) bool {
	return ValidatePhone(phone)
}

// SanitizeFilename removes dangerous characters from filenames (alias for SanitizeFileName)
func SanitizeFilename(filename string) string {
	return SanitizeFileName(filename)
}

// ValidateURL checks if URL is valid and safe (alias for IsValidURL)
func ValidateURL(urlStr string) bool {
	return IsValidURL(urlStr)
}

// RateLimitKey generates a consistent key for rate limiting
func RateLimitKey(identifier string, action string) string {
	return "ratelimit:" + action + ":" + identifier
}
