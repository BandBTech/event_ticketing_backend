package utils

import (
	"strings"

	"event-ticketing-backend/internal/models"
)

// HasPermission checks if a user has the specified permission
func HasPermission(user *models.User, resource, action string) bool {
	if user == nil || len(user.Roles) == 0 {
		return false
	}

	// Check if the user has the admin role, which grants all permissions
	for _, role := range user.Roles {
		if strings.ToLower(role.Name) == "admin" {
			return true
		}
	}

	// Check if any of the user's roles has the required permission
	for _, role := range user.Roles {
		for _, permission := range role.Permissions {
			// Check for exact match on resource and action
			if permission.Resource == resource && permission.Action == action {
				return true
			}

			// Check for wildcard permissions
			if permission.Resource == "*" && permission.Action == "*" {
				return true
			}

			if permission.Resource == resource && permission.Action == "*" {
				return true
			}

			if permission.Resource == "*" && permission.Action == action {
				return true
			}
		}
	}

	return false
}

// Note: UserHasRole and UserHasAnyRole moved to auth_helpers.go
