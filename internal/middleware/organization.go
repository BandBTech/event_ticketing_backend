package middleware

import (
	"net/http"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
)

// IsOrganizerOfOrganization returns a middleware that checks if the user is an organizer
// and if they're associated with the organization specified in the URL parameter
func IsOrganizerOfOrganization() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user from context (set by AuthMiddleware)
		userInterface, exists := c.Get("user")
		if !exists {
			utils.ErrorResponse(c, http.StatusUnauthorized, "User not authenticated", nil)
			c.Abort()
			return
		}

		user, ok := userInterface.(*models.User)
		if !ok {
			utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to get user information", nil)
			c.Abort()
			return
		}

		// Check if user has organizer or admin role
		hasOrganizerRole := false
		hasAdminRole := false
		for _, role := range user.Roles {
			if role.Name == "organizer" {
				hasOrganizerRole = true
			}
			if role.Name == "admin" {
				hasAdminRole = true
			}
		}

		// Check if user is an organizer or admin
		if !hasOrganizerRole && !hasAdminRole {
			utils.ErrorResponse(c, http.StatusForbidden, "Access denied: requires organizer role", nil)
			c.Abort()
			return
		}

		// Get organization ID from URL parameters
		orgID := c.Param("id")
		if orgID == "" {
			utils.ErrorResponse(c, http.StatusBadRequest, "Organization ID is required", nil)
			c.Abort()
			return
		}

		// If user is admin, allow access to any organization
		if hasAdminRole {
			c.Next()
			return
		}

		// For organizers, check if they're associated with this organization
		var organization models.Organization

		// Use the database connection from the service layer
		db := database.DB

		result := db.First(&organization, "id = ?", orgID)
		if result.Error != nil {
			utils.ErrorResponse(c, http.StatusNotFound, "Organization not found", result.Error)
			c.Abort()
			return
		}

		// Check if user is the organizer for this organization
		if organization.OrganizerID != user.ID {
			utils.ErrorResponse(c, http.StatusForbidden, "Access denied: you are not the organizer of this organization", nil)
			c.Abort()
			return
		}

		// Set organization in context for handlers to use
		c.Set("organization", organization)
		c.Next()
	}
}
