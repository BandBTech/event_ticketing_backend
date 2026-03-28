package utils

import "strings"

// SortConfig defines sorting configuration for an API endpoint
type SortConfig struct {
	DefaultField string
	DefaultOrder string
	ValidFields  map[string]bool
}

// Predefined sorting configurations for different API contexts
var (
	// AdminTransactionsSortConfig for admin transactions listing
	AdminTransactionsSortConfig = SortConfig{
		DefaultField: "created_at",
		DefaultOrder: "desc",
		ValidFields: map[string]bool{
			"created_at":        true,
			"amount":            true,
			"commission_amount": true,
			"organizer_share":   true,
			"quantity":          true,
			"event_title":       true,
			"user_name":         true,
			"payment_gateway":   true,
			"status":            true,
		},
	}

	// PayoutRequestsSortConfig for payout requests listing (admin and organizer)
	PayoutRequestsSortConfig = SortConfig{
		DefaultField: "created_at",
		DefaultOrder: "desc",
		ValidFields: map[string]bool{
			"created_at":   true,
			"amount":       true,
			"event_title":  true,
			"event_status": true,
			"status":       true,
			"request_type": true,
		},
	}

	// PaymentBillsSortConfig for payment bills listing
	PaymentBillsSortConfig = SortConfig{
		DefaultField: "created_at",
		DefaultOrder: "desc",
		ValidFields: map[string]bool{
			"created_at":     true,
			"event_title":    true,
			"organizer_name": true,
			"billed_amount":  true,
			"status":         true,
		},
	}

	// EventsSortConfig for events listing
	EventsSortConfig = SortConfig{
		DefaultField: "created_at",
		DefaultOrder: "desc",
		ValidFields: map[string]bool{
			"created_at":  true,
			"title":       true,
			"start_date":  true,
			"end_date":    true,
			"status":      true,
			"ticket_sold": true,
			"revenue":     true,
		},
	}

	// UsersSortConfig for users listing
	UsersSortConfig = SortConfig{
		DefaultField: "created_at",
		DefaultOrder: "desc",
		ValidFields: map[string]bool{
			"created_at":       true,
			"name":             true,
			"email":            true,
			"account_status":   true,
			"organizer_status": true,
		},
	}

	// TicketsSortConfig for tickets listing
	TicketsSortConfig = SortConfig{
		DefaultField: "created_at",
		DefaultOrder: "desc",
		ValidFields: map[string]bool{
			"created_at":    true,
			"ticket_number": true,
			"total_amount":  true,
			"status":        true,
			"event_title":   true,
			"user_name":     true,
		},
	}

	// RefundsSortConfig for refunds listing
	RefundsSortConfig = SortConfig{
		DefaultField: "created_at",
		DefaultOrder: "desc",
		ValidFields: map[string]bool{
			"created_at":    true,
			"amount":        true,
			"status":        true,
			"refund_reason": true,
			"processed_at":  true,
		},
	}

	// AuditLogsSortConfig for audit logs listing
	AuditLogsSortConfig = SortConfig{
		DefaultField: "created_at",
		DefaultOrder: "desc",
		ValidFields: map[string]bool{
			"created_at":  true,
			"action":      true,
			"entity_type": true,
			"actor_type":  true,
		},
	}

	// CheckoutSessionsSortConfig for checkout sessions listing
	CheckoutSessionsSortConfig = SortConfig{
		DefaultField: "created_at",
		DefaultOrder: "desc",
		ValidFields: map[string]bool{
			"created_at":      true,
			"status":          true,
			"payment_gateway": true,
			"total_amount":    true,
			"expires_at":      true,
		},
	}
)

// ParseSortParam parses a sort parameter that can include a `-` prefix for descending order
// Examples:
//   - "created_at" -> ("created_at", "asc")
//   - "-created_at" -> ("created_at", "desc")
//   - "-title" -> ("title", "desc")
//   - "title" -> ("title", "asc")
func ParseSortParam(sortParam string, defaultField, defaultOrder string) (string, string) {
	if sortParam == "" {
		return defaultField, defaultOrder
	}

	if strings.HasPrefix(sortParam, "-") {
		// Remove the `-` prefix and return DESC order
		field := strings.TrimPrefix(sortParam, "-")
		if field == "" {
			// If only `-` was provided, use default
			return defaultField, defaultOrder
		}
		return field, "desc"
	}

	// No prefix means ascending order
	return sortParam, "asc"
}

// ValidateAndParseSortParam validates the sort field against allowed fields and parses direction
func ValidateAndParseSortParam(sortParam string, validFields map[string]bool, defaultField, defaultOrder string) (string, string) {
	field, order := ParseSortParam(sortParam, defaultField, defaultOrder)

	// Validate field is allowed
	if !validFields[field] {
		return defaultField, defaultOrder
	}

	return field, order
}

// ValidateSortForAdminTransactions validates sorting for admin transactions API
func ValidateSortForAdminTransactions(sortBy, sortOrder string) (string, string) {
	return ValidateAndParseSortParam(sortBy, AdminTransactionsSortConfig.ValidFields, AdminTransactionsSortConfig.DefaultField, AdminTransactionsSortConfig.DefaultOrder)
}

// ValidateSortForPayoutRequests validates sorting for payout requests API
func ValidateSortForPayoutRequests(sortBy, sortOrder string) (string, string) {
	return ValidateAndParseSortParam(sortBy, PayoutRequestsSortConfig.ValidFields, PayoutRequestsSortConfig.DefaultField, PayoutRequestsSortConfig.DefaultOrder)
}

// ValidateSortForPaymentBills validates sorting for payment bills API
func ValidateSortForPaymentBills(sortBy, sortOrder string) (string, string) {
	return ValidateAndParseSortParam(sortBy, PaymentBillsSortConfig.ValidFields, PaymentBillsSortConfig.DefaultField, PaymentBillsSortConfig.DefaultOrder)
}

// ValidateSortForEvents validates sorting for events API
func ValidateSortForEvents(sortBy, sortOrder string) (string, string) {
	return ValidateAndParseSortParam(sortBy, EventsSortConfig.ValidFields, EventsSortConfig.DefaultField, EventsSortConfig.DefaultOrder)
}

// ValidateSortForUsers validates sorting for users API
func ValidateSortForUsers(sortBy, sortOrder string) (string, string) {
	return ValidateAndParseSortParam(sortBy, UsersSortConfig.ValidFields, UsersSortConfig.DefaultField, UsersSortConfig.DefaultOrder)
}

// ValidateSortForTickets validates sorting for tickets API
func ValidateSortForTickets(sortBy, sortOrder string) (string, string) {
	return ValidateAndParseSortParam(sortBy, TicketsSortConfig.ValidFields, TicketsSortConfig.DefaultField, TicketsSortConfig.DefaultOrder)
}

// ValidateSortForRefunds validates sorting for refunds API
func ValidateSortForRefunds(sortBy, sortOrder string) (string, string) {
	return ValidateAndParseSortParam(sortBy, RefundsSortConfig.ValidFields, RefundsSortConfig.DefaultField, RefundsSortConfig.DefaultOrder)
}

// ValidateSortForAuditLogs validates sorting for audit logs API
func ValidateSortForAuditLogs(sortBy, sortOrder string) (string, string) {
	return ValidateAndParseSortParam(sortBy, AuditLogsSortConfig.ValidFields, AuditLogsSortConfig.DefaultField, AuditLogsSortConfig.DefaultOrder)
}

// ValidateSortForCheckoutSessions validates sorting for checkout sessions API
func ValidateSortForCheckoutSessions(sortBy, sortOrder string) (string, string) {
	return ValidateAndParseSortParam(sortBy, CheckoutSessionsSortConfig.ValidFields, CheckoutSessionsSortConfig.DefaultField, CheckoutSessionsSortConfig.DefaultOrder)
}

// GetSortConfig returns the sort configuration for a given API context
func GetSortConfig(context string) SortConfig {
	switch context {
	case "admin_transactions":
		return AdminTransactionsSortConfig
	case "payout_requests":
		return PayoutRequestsSortConfig
	case "payment_bills":
		return PaymentBillsSortConfig
	case "events":
		return EventsSortConfig
	case "users":
		return UsersSortConfig
	case "tickets":
		return TicketsSortConfig
	case "refunds":
		return RefundsSortConfig
	case "audit_logs":
		return AuditLogsSortConfig
	case "checkout_sessions":
		return CheckoutSessionsSortConfig
	default:
		// Return a default configuration
		return SortConfig{
			DefaultField: "created_at",
			DefaultOrder: "desc",
			ValidFields: map[string]bool{
				"created_at": true,
			},
		}
	}
}

// ValidateSortByContext validates sorting parameters for a specific API context
func ValidateSortByContext(context, sortBy, sortOrder string) (string, string) {
	config := GetSortConfig(context)
	return ValidateAndParseSortParam(sortBy, config.ValidFields, config.DefaultField, config.DefaultOrder)
}
