package utils

import "strings"

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
