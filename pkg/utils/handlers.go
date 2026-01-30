package utils

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// PaginationConfig holds default pagination configuration
type PaginationConfig struct {
	DefaultLimit int
	MaxLimit     int
}

// DefaultPaginationConfig is the system-wide pagination configuration
// CHANGE THIS TO MODIFY PAGINATION BEHAVIOR ACROSS THE ENTIRE SYSTEM
var DefaultPaginationConfig = PaginationConfig{
	DefaultLimit: 10,
	MaxLimit:     100,
}

// PaginationParams represents pagination query parameters
type PaginationParams struct {
	Page  int
	Limit int
}

// GetPaginationParams extracts and validates pagination parameters from query string
// This is the SINGLE SOURCE OF TRUTH for pagination across the entire system
func GetPaginationParams(c *gin.Context, defaultLimit int) PaginationParams {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(defaultLimit)))

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

// GetUserIDFromContext extracts and validates userID from Gin context
// Returns (userID, exists, ok) where:
// - exists: whether the key was found in context
// - ok: whether the type assertion succeeded
func GetUserIDFromContext(c *gin.Context) (uuid.UUID, bool, bool) {
	userIDInterface, exists := c.Get("userID")
	if !exists {
		return uuid.Nil, false, false
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		return uuid.Nil, true, false
	}
	return userID, true, true
}

// HandleUserIDExtraction is a helper that extracts userID and handles errors
// Returns true if extraction was successful, false otherwise (and sends error response)
func HandleUserIDExtraction(c *gin.Context) (uuid.UUID, bool) {
	userID, exists, ok := GetUserIDFromContext(c)
	if !exists {
		HandleError(c, NewInternalServerError("User ID not found in context.", nil))
		return uuid.Nil, false
	}
	if !ok {
		HandleError(c, NewInternalServerError("Invalid user ID format.", nil))
		return uuid.Nil, false
	}
	return userID, true
}

// BuildPaginatedResponse creates a standardized paginated response structure
// SINGLE SOURCE OF TRUTH - All paginated responses use this
func BuildPaginatedResponse(data interface{}, total int64, page, limit int) map[string]interface{} {
	totalPages := (total + int64(limit) - 1) / int64(limit)
	hasNext := int64(page*limit) < total
	hasPrev := page > 1

	return map[string]interface{}{
		"pagination": map[string]interface{}{
			"has_next":    hasNext,
			"has_prev":    hasPrev,
			"limit":       limit,
			"page":        page,
			"total":       total,
			"total_pages": totalPages,
		},
		"data": data,
	}
}

// BuildPaginationInfo creates just the pagination metadata object
// Use this when you're building a custom response structure
func BuildPaginationInfo(total int64, page, limit int) map[string]interface{} {
	totalPages := (total + int64(limit) - 1) / int64(limit)
	hasNext := int64(page*limit) < total
	hasPrev := page > 1

	return map[string]interface{}{
		"has_next":    hasNext,
		"has_prev":    hasPrev,
		"limit":       limit,
		"page":        page,
		"total":       total,
		"total_pages": totalPages,
	}
}

// GetRolesFromContext extracts roles from Gin context
func GetRolesFromContext(c *gin.Context) ([]string, bool) {
	rolesInterface, exists := c.Get("roles")
	if !exists {
		return nil, false
	}
	roles, ok := rolesInterface.([]string)
	if !ok {
		return nil, false
	}
	return roles, true
}

// HasRole checks if user has a specific role
func HasRole(c *gin.Context, targetRole string) bool {
	roles, exists := GetRolesFromContext(c)
	if !exists {
		return false
	}
	for _, role := range roles {
		if role == targetRole {
			return true
		}
	}
	return false
}

// HasAnyRole checks if user has any of the specified roles
func HasAnyRole(c *gin.Context, targetRoles ...string) bool {
	roles, exists := GetRolesFromContext(c)
	if !exists {
		return false
	}
	for _, userRole := range roles {
		for _, targetRole := range targetRoles {
			if userRole == targetRole {
				return true
			}
		}
	}
	return false
}
