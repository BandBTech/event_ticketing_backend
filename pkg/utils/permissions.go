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

// HasRole checks if a user has a specific role
func HasRole(user *models.User, roleName string) bool {
	if user == nil || len(user.Roles) == 0 {
		return false
	}

	for _, role := range user.Roles {
		if strings.ToLower(role.Name) == strings.ToLower(roleName) {
			return true
		}
	}

	return false
}

// HasAnyRole checks if a user has any of the specified roles
func HasAnyRole(user *models.User, roleNames []string) bool {
	if user == nil || len(user.Roles) == 0 || len(roleNames) == 0 {
		return false
	}

	for _, roleName := range roleNames {
		if HasRole(user, roleName) {
			return true
		}
	}

	return false
}
