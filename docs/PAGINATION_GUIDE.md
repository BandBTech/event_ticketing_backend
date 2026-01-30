# Pagination Guide - Single Source of Truth

## Overview

This system uses a **centralized pagination utility** located in [`pkg/utils/handlers.go`](../pkg/utils/handlers.go). All pagination logic across the entire backend is managed from this single file, making it easy to maintain and update pagination behavior system-wide.

## Single Source of Truth

**File**: `pkg/utils/handlers.go`

This file contains:

- `PaginationConfig` - Global configuration for pagination limits
- `DefaultPaginationConfig` - System-wide default values
- `GetPaginationParams()` - Extracts and validates pagination from query parameters
- `BuildPaginatedResponse()` - Creates standardized pagination response structure

## How to Change Pagination Behavior

### Changing Default Limits

To change pagination defaults across the **entire system**, modify `DefaultPaginationConfig` in `pkg/utils/handlers.go`:

```go
// DefaultPaginationConfig is the system-wide pagination configuration
// CHANGE THIS TO MODIFY PAGINATION BEHAVIOR ACROSS THE ENTIRE SYSTEM
var DefaultPaginationConfig = PaginationConfig{
	DefaultLimit: 10,   // Change this to modify default page size
	MaxLimit:     100,  // Change this to modify maximum allowed page size
}
```

### Changing Validation Rules

To change how pagination parameters are validated (e.g., minimum page, maximum limit), modify the `GetPaginationParams()` function:

```go
func GetPaginationParams(c *gin.Context, defaultLimit int) PaginationParams {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(defaultLimit)))

	// Modify validation logic here
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > DefaultPaginationConfig.MaxLimit {
		limit = defaultLimit
	}

	return PaginationParams{
		Page:  page,
		Limit: limit,
	}
}
```

### Changing Response Structure

To change the pagination response format (e.g., add new fields, rename fields), modify `BuildPaginatedResponse()`:

```go
func BuildPaginatedResponse(data interface{}, total int64, page, limit int) map[string]interface{} {
	totalPages := (total + int64(limit) - 1) / int64(limit)
	return map[string]interface{}{
		"data":        data,         // Change field names here
		"total":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": totalPages,   // Add new fields here
	}
}
```

## Usage in Handlers

All handlers use the same pattern:

```go
func (h *Handler) GetItems(c *gin.Context) {
	// Extract pagination with custom default limit (10, 20, etc.)
	pagination := utils.GetPaginationParams(c, 10)

	// Use pagination.Page and pagination.Limit in service calls
	items, total, err := h.service.GetItems(pagination.Page, pagination.Limit)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Build standardized response
	response := utils.BuildPaginatedResponse(items, total, pagination.Page, pagination.Limit)

	utils.SuccessResponse(c, http.StatusOK, "Items retrieved successfully", response)
}
```

## Files Using Centralized Pagination

✅ **Fully Migrated**:

- `internal/handlers/event_handler.go` - All event-related endpoints
- `internal/handlers/ticket_handler.go` - Ticket management endpoints
- `internal/handlers/financial_handler.go` - Financial and payment endpoints
- `internal/handlers/organizer_user_handler.go` - Organizer user management
- `internal/handlers/auth_handler.go` - Authentication and organizer management
- `internal/handlers/public_handler.go` - Public API endpoints (guest tickets)

⚠️ **Custom Validation** (intentionally different):

- Some public API endpoints in `public_handler.go` have stricter limits (10 max instead of 100)

## Benefits

1. **Single Point of Maintenance**: Change pagination logic in ONE place
2. **Consistency**: All endpoints use the same pagination behavior
3. **Easy Testing**: Pagination logic is centralized and testable
4. **Quick Updates**: System-wide changes require editing only one file
5. **Standardized Responses**: All paginated responses have the same structure

## API Query Parameters

All paginated endpoints accept:

- `page` (default: 1) - Page number (1-based)
- `limit` (default: varies by endpoint) - Items per page (max: 100)

Example:

```
GET /api/v1/events?page=2&limit=20
```

## Response Structure

All paginated responses follow this structure:

```json
{
  "success": true,
  "message": "Items retrieved successfully",
  "data": {
    "data": [...],        // Array of items
    "total": 150,         // Total number of items
    "page": 2,            // Current page
    "limit": 20,          // Items per page
    "total_pages": 8      // Total number of pages
  }
}
```

Some endpoints add additional fields (like `has_next`, `has_prev`) by extending the base response.

## Migration Complete

As of this documentation, **ALL** pagination logic has been centralized. No manual pagination logic (`strconv.Atoi`, manual `total_pages` calculation) exists outside of `pkg/utils/handlers.go`.

---

**Last Updated**: 2024
**Maintained By**: Backend Team
