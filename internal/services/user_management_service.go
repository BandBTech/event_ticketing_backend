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
			"LOWER(first_name) LIKE ? OR LOWER(last_name) LIKE ? OR LOWER(email) LIKE ?",
			searchTerm, searchTerm, searchTerm,
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
	if req.Sort == "" {
		req.Sort = "-created_at"
	}

	// Parse sort parameter for field and direction
	validSortFields := map[string]bool{
		"first_name": true, "last_name": true, "email": true, "created_at": true,
		"account_status": true, "organizer_status": true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(req.Sort, validSortFields, "created_at", "desc")

	orderClause := fmt.Sprintf("%s %s", sortBy, sortOrder)
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
			return fmt.Errorf("user not found: %w", err)
		}

		oldRoles := make([]string, len(user.Roles))
		for i, role := range user.Roles {
			oldRoles[i] = role.Name
		}

		// Get the new role
		var newRole models.Role
		if err := tx.Where("name = ?", newRoleName).First(&newRole).Error; err != nil {
			return fmt.Errorf("role not found: %w", err)
		}

		// Clear existing roles and assign new role
		if err := tx.Model(&user).Association("Roles").Clear(); err != nil {
			return fmt.Errorf("failed to clear existing roles: %w", err)
		}

		if err := tx.Model(&user).Association("Roles").Append(&newRole); err != nil {
			return fmt.Errorf("failed to assign new role: %w", err)
		}

		// Special handling for organizer promotion
		if newRoleName == "organizer" {
			user.OrganizerStatus = "approved"
		}

		// Update user record
		if err := tx.Save(&user).Error; err != nil {
			return fmt.Errorf("failed to update user: %w", err)
		}

		return nil
	})
}

// UpdateAccountStatus updates a user's account status
func (s *UserManagementService) UpdateAccountStatus(userID uuid.UUID, req *models.UpdateAccountStatusRequest, adminID uuid.UUID) error {
	var user models.User
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", userID).First(&user).Error; err != nil {
		return fmt.Errorf("user not found: %w", err)
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
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", userID).First(&user).Error; err != nil {
		return fmt.Errorf("user not found: %w", err)
	}

	return database.DB.Delete(&user).Error
}

// HardDeleteUser permanently deletes a user and all associated data
func (s *UserManagementService) HardDeleteUser(userID uuid.UUID, adminID uuid.UUID) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Unscoped().Where("id = ?", userID).First(&user).Error; err != nil {
			return fmt.Errorf("user not found: %w", err)
		}

		// Delete user roles associations
		if err := tx.Where("user_id = ?", userID).Delete(&models.UserRole{}).Error; err != nil {
			return fmt.Errorf("failed to delete user roles: %w", err)
		}

		// Delete tokens associated with the user
		if err := tx.Where("user_id = ?", userID).Delete(&models.Token{}).Error; err != nil {
			return fmt.Errorf("failed to delete user tokens: %w", err)
		}

		// Delete OTP records associated with the user
		if err := tx.Where("identifier = ?", user.Email).Delete(&models.OTP{}).Error; err != nil {
			return fmt.Errorf("failed to delete user OTPs: %w", err)
		}

		// Permanently delete the user
		if err := tx.Unscoped().Delete(&user).Error; err != nil {
			return fmt.Errorf("failed to hard delete user: %w", err)
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
		return fmt.Errorf("invalid delete type: %s", deleteType)
	}
}

// RestoreUser restores a soft-deleted user
func (s *UserManagementService) RestoreUser(userID uuid.UUID, adminID uuid.UUID) error {
	var user models.User
	if err := database.DB.Unscoped().Where("id = ?", userID).First(&user).Error; err != nil {
		return fmt.Errorf("user not found: %w", err)
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
				return fmt.Errorf("invalid user ID format: %s", idStr)
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
			return tx.Where("id IN ?", userIDs).Delete(&models.User{}).Error

		case "hard_delete":
			// For hard delete, we need to delete associated data for each user
			for _, userID := range userIDs {
				if err := s.HardDeleteUser(userID, adminID); err != nil {
					return fmt.Errorf("failed to hard delete user %s: %w", userID, err)
				}
			}
			return nil

		case "promote":
			if req.Role == "" {
				return fmt.Errorf("role is required for promote action")
			}

			// Get the new role
			var newRole models.Role
			if err := tx.Where("name = ?", req.Role).First(&newRole).Error; err != nil {
				return fmt.Errorf("role not found: %w", err)
			}

			// Update each user's roles
			for _, userID := range userIDs {
				var user models.User
				if err := tx.Preload("Roles").Where("id = ?", userID).First(&user).Error; err != nil {
					continue // Skip invalid users
				}

				// Clear existing roles and assign new role
				if err := tx.Model(&user).Association("Roles").Clear(); err != nil {
					return fmt.Errorf("failed to clear roles for user %s: %w", userID, err)
				}

				if err := tx.Model(&user).Association("Roles").Append(&newRole); err != nil {
					return fmt.Errorf("failed to assign role to user %s: %w", userID, err)
				}

				// Special handling for organizer promotion
				if req.Role == "organizer" {
					user.OrganizerStatus = "approved"
					if err := tx.Save(&user).Error; err != nil {
						return fmt.Errorf("failed to update organizer status for user %s: %w", userID, err)
					}
				}
			}

		default:
			return fmt.Errorf("unsupported action: %s", req.Action)
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
