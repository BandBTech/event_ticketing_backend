package services

import (
	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type UserManagementService struct {
	emailQueueService *EmailQueueService
}

func NewUserManagementService(cfg *config.Config) *UserManagementService {
	return &UserManagementService{
		emailQueueService: NewEmailQueueService(cfg),
	}
}

// GetAllUsers returns paginated list of users with filtering and sorting
func (s *UserManagementService) GetAllUsers(req *models.UserSearchRequest) ([]models.User, int64, error) {
	query := database.DB.Preload("Roles").Where("deleted_at IS NULL")

	// Apply search filter
	if req.Search != "" {
		searchTerm := "%" + strings.ToLower(req.Search) + "%"
		query = query.Where(
			"LOWER(first_name) LIKE ? OR LOWER(last_name) LIKE ? OR LOWER(email) LIKE ? OR LOWER(COALESCE(first_name, '') || ' ' || COALESCE(last_name, '')) LIKE ?",
			searchTerm, searchTerm, searchTerm, searchTerm,
		)
	}

	// Apply status filter
	if req.Status != "" {
		query = query.Where("account_status = ?", req.Status)
	}

	// Apply organizer status filter
	if req.OrgStatus != "" {
		query = query.Where("organizer_status = ?", req.OrgStatus)
	}

	// Apply role filter
	if req.Role != "" {
		query = query.Joins("JOIN user_roles ON users.id = user_roles.user_id").
			Joins("JOIN roles ON user_roles.role_id = roles.id").
			Where("roles.name = ?", req.Role)
	}

	// Count total records
	var total int64
	countQuery := query.Session(&gorm.Session{})
	if err := countQuery.Model(&models.User{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Apply sorting
	// Use sortBy and sortOrder directly from request (already validated in handler)
	sortBy := req.SortBy
	if sortBy == "" {
		sortBy = "created_at"
	}
	sortOrder := req.SortOrder
	if sortOrder == "" {
		sortOrder = "desc"
	}

	// Validate sort field (order already validated in handler)
	validatedSortBy, _ := utils.ValidateAndParseSortParam(sortBy, utils.UsersSortConfig.ValidFields, utils.UsersSortConfig.DefaultField, utils.UsersSortConfig.DefaultOrder)
	sortOrder = utils.ValidateSortOrder(sortOrder)

	// Handle special sorting cases
	var orderClause string
	if validatedSortBy == "name" {
		orderClause = fmt.Sprintf("LOWER(CONCAT(COALESCE(first_name, ''), ' ', COALESCE(last_name, ''))) %s", sortOrder)
	} else if validatedSortBy == "role" {
		// Sort by role name using a subquery to get the first role alphabetically (case insensitive)
		// Use COALESCE to handle users without roles (they'll be sorted as empty string)
		orderClause = fmt.Sprintf("COALESCE((SELECT MIN(LOWER(r.name)) FROM user_roles ur JOIN roles r ON ur.role_id = r.id WHERE ur.user_id = users.id), '') %s", sortOrder)
	} else {
		orderClause = utils.GenerateOrderByClause(validatedSortBy, sortOrder)
	}
	query = query.Order(orderClause)

	// Apply pagination
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit < 1 {
		req.Limit = 10
	}

	offset := (req.Page - 1) * req.Limit
	query = query.Offset(offset).Limit(req.Limit)

	var users []models.User
	err := query.Find(&users).Error
	return users, total, err
}

// GetUserByID returns a user by ID with all relationships
func (s *UserManagementService) GetUserByID(userID uuid.UUID) (*models.User, error) {
	var user models.User
	err := database.DB.Preload("Roles.Permissions").
		Where("id = ? AND deleted_at IS NULL", userID).First(&user).Error
	return &user, err
}

// PromoteUser changes a user's role
func (s *UserManagementService) PromoteUser(userID uuid.UUID, newRoleName string, adminID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// Get the user
		var user models.User
		if err := tx.Preload("Roles").Where("id = ? AND deleted_at IS NULL", userID).First(&user).Error; err != nil {
			return utils.NewNotFoundError("user")
		}

		oldRoles := make([]string, len(user.Roles))
		for i, role := range user.Roles {
			oldRoles[i] = role.Name
		}

		// Get the new role
		var newRole models.Role
		if err := tx.Where("name = ?", newRoleName).First(&newRole).Error; err != nil {
			return utils.NewNotFoundError("role")
		}

		// Clear existing roles and assign new role
		if err := tx.Model(&user).Association("Roles").Clear(); err != nil {
			return utils.NewDatabaseError("Failed to clear existing roles.", err)
		}

		if err := tx.Model(&user).Association("Roles").Append(&newRole); err != nil {
			return utils.NewDatabaseError("Failed to assign new role.", err)
		}

		// Special handling for organizer promotion
		if newRoleName == "organizer" {
			user.OrganizerStatus = "approved"
			user.AccountStatus = "active" // Ensure account is active when promoted to organizer
		}

		// Update user record
		if err := tx.Save(&user).Error; err != nil {
			return utils.NewDatabaseError("Failed to update user.", err)
		}

		return nil
	})
}

// UpdateAccountStatus updates a user's account status
func (s *UserManagementService) UpdateAccountStatus(userID uuid.UUID, req *models.UpdateAccountStatusRequest, adminID uuid.UUID) error {
	var user models.User
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", userID).First(&user).Error; err != nil {
		return utils.NewNotFoundError("user")
	}

	user.AccountStatus = req.Status
	if req.AdminRemark != "" {
		user.AdminRemark = req.AdminRemark
	}

	return database.DB.Save(&user).Error
}

// SoftDeleteUser soft deletes a user
func (s *UserManagementService) SoftDeleteUser(userID uuid.UUID, adminID uuid.UUID) error {
	var user models.User
	if err := database.DB.Preload("Roles").Where("id = ? AND deleted_at IS NULL", userID).First(&user).Error; err != nil {
		return utils.NewNotFoundError("user")
	}

	// Check if user is admin - prevent deletion
	for _, role := range user.Roles {
		if role.Name == "admin" {
			return utils.NewBusinessLogicError("Cannot delete admin accounts.")
		}
	}

	// Check if user is organizer and has events - prevent deletion
	isOrganizer := false
	for _, role := range user.Roles {
		if role.Name == "organizer" {
			isOrganizer = true
			break
		}
	}

	if isOrganizer {
		var eventCount int64
		if err := database.DB.Model(&models.Event{}).Where("organizer_id = ?", userID).Count(&eventCount).Error; err != nil {
			return utils.NewDatabaseError("Failed to check for associated events.", err)
		}
		if eventCount > 0 {
			return utils.NewBusinessLogicError(fmt.Sprintf("Cannot delete organizer account with existing events (found %d events).", eventCount))
		}
	}

	return database.DB.Delete(&user).Error
}

// HardDeleteUser permanently deletes a user and all associated data
func (s *UserManagementService) HardDeleteUser(userID uuid.UUID, adminID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Unscoped().Preload("Roles").Where("id = ?", userID).First(&user).Error; err != nil {
			return utils.NewNotFoundError("user")
		}

		// Check if user is admin - prevent deletion
		for _, role := range user.Roles {
			if role.Name == "admin" {
				return utils.NewBusinessLogicError("Cannot delete admin accounts.")
			}
		}

		// Check if user is organizer and has events - prevent deletion
		isOrganizer := false
		for _, role := range user.Roles {
			if role.Name == "organizer" {
				isOrganizer = true
				break
			}
		}

		if isOrganizer {
			var eventCount int64
			if err := tx.Model(&models.Event{}).Where("organizer_id = ?", userID).Count(&eventCount).Error; err != nil {
				return utils.NewDatabaseError("Failed to check for associated events.", err)
			}
			if eventCount > 0 {
				return utils.NewBusinessLogicError(fmt.Sprintf("Cannot delete organizer account with existing events (found %d events).", eventCount))
			}
		}

		// Delete user roles associations
		if err := tx.Where("user_id = ?", userID).Delete(&models.UserRole{}).Error; err != nil {
			return utils.NewDatabaseError("Failed to delete user roles.", err)
		}

		// Delete tokens associated with the user
		if err := tx.Where("user_id = ?", userID).Delete(&models.Token{}).Error; err != nil {
			return utils.NewDatabaseError("Failed to delete user tokens.", err)
		}

		// Delete OTP records associated with the user
		if err := tx.Where("identifier = ?", user.Email).Delete(&models.OTP{}).Error; err != nil {
			return utils.NewDatabaseError("Failed to delete user OTPs.", err)
		}

		// Permanently delete the user
		if err := tx.Unscoped().Delete(&user).Error; err != nil {
			return utils.NewDatabaseError("Failed to hard delete user.", err)
		}

		return nil
	})
}

// DeleteUser deletes a user based on the specified delete type
func (s *UserManagementService) DeleteUser(userID uuid.UUID, adminID uuid.UUID, deleteType string) error {
	switch deleteType {
	case "soft":
		return s.SoftDeleteUser(userID, adminID)
	case "hard":
		return s.HardDeleteUser(userID, adminID)
	default:
		return utils.NewBusinessLogicError(fmt.Sprintf("Invalid delete type: %s.", deleteType))
	}
}

// RestoreUser restores a soft-deleted user
func (s *UserManagementService) RestoreUser(userID uuid.UUID, adminID uuid.UUID) error {
	var user models.User
	if err := database.DB.Unscoped().Where("id = ?", userID).First(&user).Error; err != nil {
		return utils.NewNotFoundError("user")
	}

	user.DeletedAt = nil
	user.AccountStatus = "active"

	return database.DB.Unscoped().Save(&user).Error
}

// BulkUserAction performs bulk actions on multiple users
func (s *UserManagementService) BulkUserAction(req *models.BulkUserActionRequest, adminID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		userIDs := make([]uuid.UUID, len(req.UserIDs))
		for i, idStr := range req.UserIDs {
			userID, err := uuid.Parse(idStr)
			if err != nil {
				return utils.NewBusinessLogicError(fmt.Sprintf("Invalid user ID format: %s.", idStr))
			}
			userIDs[i] = userID
		}

		switch req.Action {
		case "activate":
			return tx.Model(&models.User{}).Where("id IN ?", userIDs).
				Update("account_status", "active").Error

		case "deactivate":
			return tx.Model(&models.User{}).Where("id IN ?", userIDs).
				Update("account_status", "inactive").Error

		case "suspend":
			return tx.Model(&models.User{}).Where("id IN ?", userIDs).
				Updates(map[string]interface{}{
					"account_status": "suspended",
					"admin_remark":   req.Reason,
				}).Error

		case "soft_delete":
			// Check each user for protection before bulk soft delete
			for _, userID := range userIDs {
				var user models.User
				if err := tx.Preload("Roles").Where("id = ? AND deleted_at IS NULL", userID).First(&user).Error; err != nil {
					return utils.NewNotFoundError(fmt.Sprintf("user %s", userID))
				}

				// Check if user is admin - prevent deletion
				for _, role := range user.Roles {
					if role.Name == "admin" {
						return utils.NewBusinessLogicError(fmt.Sprintf("Cannot delete admin accounts (user: %s).", userID))
					}
				}

				// Check if user is organizer and has events - prevent deletion
				isOrganizer := false
				for _, role := range user.Roles {
					if role.Name == "organizer" {
						isOrganizer = true
						break
					}
				}

				if isOrganizer {
					var eventCount int64
					if err := tx.Model(&models.Event{}).Where("organizer_id = ?", userID).Count(&eventCount).Error; err != nil {
						return utils.NewDatabaseError(fmt.Sprintf("Failed to check for associated events for user %s.", userID), err)
					}
					if eventCount > 0 {
						return utils.NewBusinessLogicError(fmt.Sprintf("Cannot delete organizer account with existing events (user: %s, events: %d).", userID, eventCount))
					}
				}
			}
			return tx.Where("id IN ?", userIDs).Delete(&models.User{}).Error

		case "hard_delete":
			// For hard delete, we need to delete associated data for each user
			for _, userID := range userIDs {
				if err := s.HardDeleteUser(userID, adminID); err != nil {
					return utils.NewDatabaseError(fmt.Sprintf("Failed to hard delete user %s.", userID), err)
				}
			}
			return nil

		case "promote":
			if req.Role == "" {
				return utils.NewBusinessLogicError("Role is required for promote action.")
			}

			// Get the new role
			var newRole models.Role
			if err := tx.Where("name = ?", req.Role).First(&newRole).Error; err != nil {
				return utils.NewNotFoundError("role")
			}

			// Update each user's roles
			for _, userID := range userIDs {
				var user models.User
				if err := tx.Preload("Roles").Where("id = ?", userID).First(&user).Error; err != nil {
					continue // Skip invalid users
				}

				// Clear existing roles and assign new role
				if err := tx.Model(&user).Association("Roles").Clear(); err != nil {
					return utils.NewDatabaseError(fmt.Sprintf("Failed to clear roles for user %s.", userID), err)
				}

				if err := tx.Model(&user).Association("Roles").Append(&newRole); err != nil {
					return utils.NewDatabaseError(fmt.Sprintf("Failed to assign role to user %s.", userID), err)
				}

				// Special handling for organizer promotion
				if req.Role == "organizer" {
					user.OrganizerStatus = "approved"
					user.AccountStatus = "active" // Ensure account is active when promoted to organizer
					if err := tx.Save(&user).Error; err != nil {
						return utils.NewDatabaseError(fmt.Sprintf("Failed to update organizer status for user %s.", userID), err)
					}
				}
			}

		default:
			return utils.NewBusinessLogicError(fmt.Sprintf("Unsupported action: %s.", req.Action))
		}

		return nil
	})
}

// GetUserStatistics returns user statistics
func (s *UserManagementService) GetUserStatistics() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Total users (excluding deleted)
	var totalUsers int64
	if err := database.DB.Model(&models.User{}).Where("deleted_at IS NULL").Count(&totalUsers).Error; err != nil {
		return nil, err
	}
	stats["total_users"] = totalUsers

	// Active users
	var activeUsers int64
	if err := database.DB.Model(&models.User{}).Where("deleted_at IS NULL AND account_status = ?", "active").Count(&activeUsers).Error; err != nil {
		return nil, err
	}
	stats["active_users"] = activeUsers

	// Suspended users
	var suspendedUsers int64
	if err := database.DB.Model(&models.User{}).Where("deleted_at IS NULL AND account_status = ?", "suspended").Count(&suspendedUsers).Error; err != nil {
		return nil, err
	}
	stats["suspended_users"] = suspendedUsers

	// Organizers by status
	var pendingOrganizers, approvedOrganizers int64
	database.DB.Model(&models.User{}).Where("deleted_at IS NULL AND organizer_status = ?", "pending").Count(&pendingOrganizers)
	database.DB.Model(&models.User{}).Where("deleted_at IS NULL AND organizer_status = ?", "approved").Count(&approvedOrganizers)

	stats["pending_organizers"] = pendingOrganizers
	stats["approved_organizers"] = approvedOrganizers

	// Users by role
	roleStats := make(map[string]int64)
	roles := []string{"user", "organizer", "subadmin", "admin"}
	for _, role := range roles {
		var count int64
		database.DB.Table("users").
			Joins("JOIN user_roles ON users.id = user_roles.user_id").
			Joins("JOIN roles ON user_roles.role_id = roles.id").
			Where("users.deleted_at IS NULL AND roles.name = ?", role).
			Count(&count)
		roleStats[role] = count
	}
	stats["users_by_role"] = roleStats

	return stats, nil
}
