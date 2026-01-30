package utils

import (
	"event-ticketing-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GetOrganizerIDForUser returns the organizer ID for the given user
// SINGLE SOURCE OF TRUTH for organizer ID resolution
// For organizers: returns their user ID
// For staff/managers: returns their organizer_id
func GetOrganizerIDForUser(db *gorm.DB, userID uuid.UUID) (uuid.UUID, error) {
	var user models.User
	if err := db.Preload("Roles").Where("id = ?", userID).First(&user).Error; err != nil {
		return uuid.Nil, NewNotFoundError("user")
	}

	// Check if user is organizer
	for _, role := range user.Roles {
		if role.Name == "organizer" {
			return userID, nil
		}
	}

	// For staff/managers, check if they have organizer_id
	if user.OrganizerID == nil {
		return uuid.Nil, NewForbiddenError("Staff/manager does not belong to an organizer.")
	}

	return *user.OrganizerID, nil
}

// GetUserWithRoles retrieves user with roles preloaded - optimized single query
func GetUserWithRoles(db *gorm.DB, userID uuid.UUID) (*models.User, error) {
	var user models.User
	if err := db.Preload("Roles").Where("id = ? AND deleted_at IS NULL", userID).First(&user).Error; err != nil {
		return nil, NewNotFoundError("user")
	}
	return &user, nil
}

// UserHasRole checks if user has a specific role (from preloaded Roles)
func UserHasRole(user *models.User, roleName string) bool {
	if user == nil {
		return false
	}
	for _, role := range user.Roles {
		if role.Name == roleName {
			return true
		}
	}
	return false
}

// UserHasAnyRole checks if user has any of the specified roles
func UserHasAnyRole(user *models.User, roleNames ...string) bool {
	if user == nil {
		return false
	}
	for _, role := range user.Roles {
		for _, roleName := range roleNames {
			if role.Name == roleName {
				return true
			}
		}
	}
	return false
}
