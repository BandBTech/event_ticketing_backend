package utils

import (
	"fmt"
	"sort"
	"strings"

	"event-ticketing-backend/pkg/types"
)

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
		DefaultOrder: "DESC",
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
		DefaultOrder: "DESC",
		ValidFields: map[string]bool{
			"date":           true, // alias for created_at
			"created_at":     true,
			"amount":         true,
			"event_title":    true,
			"status":         true,
			"request_number": true,
		},
	}

	// PaymentBillsSortConfig for payment bills listing
	PaymentBillsSortConfig = SortConfig{
		DefaultField: "created_at",
		DefaultOrder: "DESC",
		ValidFields: map[string]bool{
			"created_at":     true,
			"event_title":    true,
			"organizer_name": true,
			"billed_amount":  true,
			"status":         true,
		},
	}

	// PaymentHistorySortConfig for payment history listing
	PaymentHistorySortConfig = SortConfig{
		DefaultField: "payment_date",
		DefaultOrder: "DESC",
		ValidFields: map[string]bool{
			"payment_date":   true,
			"amount":         true,
			"payment_method": true,
			"payment_ref":    true,
			"processed_by":   true,
			"notes":          true,
			"created_at":     true,
		},
	}

	// EventsSortConfig for events listing
	EventsSortConfig = SortConfig{
		DefaultField: "created_at",
		DefaultOrder: "DESC",
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
		DefaultOrder: "DESC",
		ValidFields: map[string]bool{
			"created_at":     true,
			"name":           true,
			"email":          true,
			"account_status": true,
			"role":           true,
		},
	}

	// TicketsSortConfig for tickets listing
	TicketsSortConfig = SortConfig{
		DefaultField: "created_at",
		DefaultOrder: "DESC",
		ValidFields: map[string]bool{
			"created_at":    true,
			"ticket_number": true,
			"total_amount":  true,
			"status":        true,
			"event_title":   true,
			"user_name":     true,
			"tier":          true,
			"check_in_time": true,
			"checked_in_by": true,
			"purchase_date": true,
			"purchased_by":  true,
		},
	}

	// RefundsSortConfig for refunds listing
	RefundsSortConfig = SortConfig{
		DefaultField: "created_at",
		DefaultOrder: "DESC",
		ValidFields: map[string]bool{
			"created_at":    true,
			"amount":        true,
			"status":        true,
			"refund_number": true,
			"processed_at":  true,
			"initiated_by":  true,
			"refund_type":   true,
		},
	}

	// AuditLogsSortConfig for audit logs listing
	AuditLogsSortConfig = SortConfig{
		DefaultField: "created_at",
		DefaultOrder: "DESC",
		ValidFields: map[string]bool{
			"created_at":  true,
			"action":      true,
			"entity_type": true,
			"actor_type":  true,
		},
	}

	// REMOVED: CheckoutSessionsSortConfig - CheckoutSession model removed per clean architecture
)

// TextFieldsForCaseInsensitiveSorting defines which fields should be sorted case insensitively
var TextFieldsForCaseInsensitiveSorting = map[string]bool{
	// Admin Transactions
	"event_title": true,
	"user_name":   true,

	// Payout Requests - event_title already covered above
	"status": true,

	// Payment Bills
	"organizer_name": true,

	// Events
	"title": true,

	// Users
	"name":           true,
	"email":          true,
	"role":           true,
	"account_status": true,

	// Tickets
	"ticket_number": true,
	// event_title and user_name already covered above
	"tier":          true,
	"purchased_by":  true,
	"checked_in_by": true,

	// Refunds
	"refund_number": true,
	"refund_type":   true,
	"initiated_by":  true,

	// Audit Logs
	"action":      true,
	"entity_type": true,
	"actor_type":  true,

	// Checkout Sessions
	"payment_gateway": true,

	// Payment History
	"payment_method": true,
	"payment_ref":    true,
	"notes":          true,
}

// GenerateOrderByClause generates the appropriate ORDER BY clause for a field
// Uses LOWER() for case insensitive sorting on text fields
func GenerateOrderByClause(field, order string) string {
	if TextFieldsForCaseInsensitiveSorting[field] {
		return fmt.Sprintf("LOWER(%s) %s", field, order)
	}
	return fmt.Sprintf("%s %s", field, order)
}

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

// ValidateSortOrder ensures sort order is either "asc" or "desc" and returns uppercase for SQL
func ValidateSortOrder(sortOrder string) string {
	if sortOrder == "asc" || sortOrder == "ASC" {
		return "ASC"
	}
	// Default to DESC (latest first) for any other value including "desc"
	return "DESC"
}

// ValidateSortForAdminTransactions validates sorting for admin transactions API
func ValidateSortForAdminTransactions(sortBy, sortOrder string) (string, string) {
	field, _ := ValidateAndParseSortParam(sortBy, AdminTransactionsSortConfig.ValidFields, AdminTransactionsSortConfig.DefaultField, AdminTransactionsSortConfig.DefaultOrder)
	// If sortOrder is explicitly provided, use it; otherwise use default
	order := AdminTransactionsSortConfig.DefaultOrder
	if sortOrder != "" {
		order = ValidateSortOrder(sortOrder)
	}
	return field, order
}

// ValidateSortForPayoutRequests validates sorting for payout requests API
func ValidateSortForPayoutRequests(sortBy, sortOrder string) (string, string) {
	field, _ := ValidateAndParseSortParam(sortBy, PayoutRequestsSortConfig.ValidFields, PayoutRequestsSortConfig.DefaultField, PayoutRequestsSortConfig.DefaultOrder)
	order := PayoutRequestsSortConfig.DefaultOrder
	if sortOrder != "" {
		order = ValidateSortOrder(sortOrder)
	}
	return field, order
}

// ValidateSortForPaymentBills validates sorting for payment bills API
func ValidateSortForPaymentBills(sortBy, sortOrder string) (string, string) {
	field, _ := ValidateAndParseSortParam(sortBy, PaymentBillsSortConfig.ValidFields, PaymentBillsSortConfig.DefaultField, PaymentBillsSortConfig.DefaultOrder)
	order := PaymentBillsSortConfig.DefaultOrder
	if sortOrder != "" {
		order = ValidateSortOrder(sortOrder)
	}
	return field, order
}

// ValidateSortForPaymentHistory validates sorting for payment history API
func ValidateSortForPaymentHistory(sortBy, sortOrder string) (string, string) {
	field, _ := ValidateAndParseSortParam(sortBy, PaymentHistorySortConfig.ValidFields, PaymentHistorySortConfig.DefaultField, PaymentHistorySortConfig.DefaultOrder)
	order := PaymentHistorySortConfig.DefaultOrder
	if sortOrder != "" {
		order = ValidateSortOrder(sortOrder)
	}
	return field, order
}

// ValidateSortForEvents validates sorting for events API
func ValidateSortForEvents(sortBy, sortOrder string) (string, string) {
	field, _ := ValidateAndParseSortParam(sortBy, EventsSortConfig.ValidFields, EventsSortConfig.DefaultField, EventsSortConfig.DefaultOrder)
	order := EventsSortConfig.DefaultOrder
	if sortOrder != "" {
		order = ValidateSortOrder(sortOrder)
	}
	return field, order
}

// ValidateSortForUsers validates sorting for users API
func ValidateSortForUsers(sortBy, sortOrder string) (string, string) {
	field, _ := ValidateAndParseSortParam(sortBy, UsersSortConfig.ValidFields, UsersSortConfig.DefaultField, UsersSortConfig.DefaultOrder)
	order := UsersSortConfig.DefaultOrder
	if sortOrder != "" {
		order = ValidateSortOrder(sortOrder)
	}
	return field, order
}

// ValidateSortForTickets validates sorting for tickets API
func ValidateSortForTickets(sortBy, sortOrder string) (string, string) {
	field, _ := ValidateAndParseSortParam(sortBy, TicketsSortConfig.ValidFields, TicketsSortConfig.DefaultField, TicketsSortConfig.DefaultOrder)
	order := TicketsSortConfig.DefaultOrder
	if sortOrder != "" {
		order = ValidateSortOrder(sortOrder)
	}
	return field, order
}

// ValidateSortForRefunds validates sorting for refunds API
func ValidateSortForRefunds(sortBy, sortOrder string) (string, string) {
	field, _ := ValidateAndParseSortParam(sortBy, RefundsSortConfig.ValidFields, RefundsSortConfig.DefaultField, RefundsSortConfig.DefaultOrder)
	order := RefundsSortConfig.DefaultOrder
	if sortOrder != "" {
		order = ValidateSortOrder(sortOrder)
	}
	return field, order
}

// ValidateSortForAuditLogs validates sorting for audit logs API
func ValidateSortForAuditLogs(sortBy, sortOrder string) (string, string) {
	field, _ := ValidateAndParseSortParam(sortBy, AuditLogsSortConfig.ValidFields, AuditLogsSortConfig.DefaultField, AuditLogsSortConfig.DefaultOrder)
	order := AuditLogsSortConfig.DefaultOrder
	if sortOrder != "" {
		order = ValidateSortOrder(sortOrder)
	}
	return field, order
}

// REMOVED: ValidateSortForCheckoutSessions - CheckoutSession model removed per clean architecture

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
	// REMOVED: checkout_sessions case - CheckoutSession model removed per clean architecture
	default:
		// Return a default configuration
		return SortConfig{
			DefaultField: "created_at",
			DefaultOrder: "DESC",
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

// SortTiers ensures deterministic DB locking order
// Prevents deadlocks when multiple tiers are reserved in same transaction
func SortTiers(tiers []types.TierSelection) {
	sort.SliceStable(tiers, func(i, j int) bool {
		return tiers[i].TierID.String() < tiers[j].TierID.String()
	})
}
