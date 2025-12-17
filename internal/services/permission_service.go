package services

import (
	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PermissionService struct{}

func NewPermissionService() *PermissionService {
	return &PermissionService{}
}

// Resource and Action constants for the system
const (
	// Resources
	ResourceEvent        = "event"
	ResourceUser         = "user"
	ResourceOrganization = "organization"
	ResourceTicket       = "ticket"
	ResourceFinancial    = "financial"
	ResourcePayout       = "payout"
	ResourcePermission   = "permission"
	ResourceRole         = "role"
	ResourceAnalytics    = "analytics"

	// Actions
	ActionCreate  = "create"
	ActionRead    = "read"
	ActionUpdate  = "update"
	ActionDelete  = "delete"
	ActionApprove = "approve"
	ActionReject  = "reject"
	ActionManage  = "manage"
	ActionView    = "view"
	ActionControl = "control"
	ActionPromote = "promote"
	ActionSuspend = "suspend"
	ActionExport  = "export"
)

// Predefined system permissions - MATCHING EXISTING DATABASE FORMAT
var SystemPermissions = []models.Permission{
	// Profile Management
	{Name: "view:profile", Description: "View profile", Resource: "profile", Action: "view"},
	{Name: "update:profile", Description: "Update profile", Resource: "profile", Action: "update"},

	// Event Management
	{Name: "read:event", Description: "View events", Resource: "events", Action: "read"},
	{Name: "create:event", Description: "Create events", Resource: "events", Action: "create"},
	{Name: "update:event", Description: "Update events", Resource: "events", Action: "update"},
	{Name: "delete:event", Description: "Delete events", Resource: "events", Action: "delete"},
	{Name: "approve:event", Description: "Approve events", Resource: "events", Action: "approve"},
	{Name: "reject:event", Description: "Reject events", Resource: "events", Action: "reject"},
	{Name: "hold:event", Description: "Hold events", Resource: "events", Action: "hold"},

	// User Management
	{Name: "read:user", Description: "View users", Resource: "users", Action: "read"},
	{Name: "create:user", Description: "Create users", Resource: "users", Action: "create"},
	{Name: "update:user", Description: "Update users", Resource: "users", Action: "update"},
	{Name: "delete:user", Description: "Delete users", Resource: "users", Action: "delete"},
	{Name: "approve:organizer", Description: "Approve organizers", Resource: "organizers", Action: "approve"},
	{Name: "reject:organizer", Description: "Reject organizers", Resource: "organizers", Action: "reject"},

	// Ticket Management
	{Name: "create:ticket", Description: "Purchase tickets", Resource: "tickets", Action: "create"},
	{Name: "read:ticket", Description: "View tickets", Resource: "tickets", Action: "read"},
	{Name: "scan:ticket", Description: "Scan tickets for check-in/check-out", Resource: "tickets", Action: "scan"},
	{Name: "checkin:ticket", Description: "Check-in tickets", Resource: "tickets", Action: "checkin"},
	{Name: "checkout:ticket", Description: "Check-out tickets", Resource: "tickets", Action: "checkout"},

	// Staff Management
	{Name: "manage:staff", Description: "Manage staff members", Resource: "staff", Action: "manage"},

	// Payout Management
	{Name: "create:payout", Description: "Create payout requests", Resource: "payouts", Action: "create"},
	{Name: "read:payout", Description: "View payout requests", Resource: "payouts", Action: "read"},
	{Name: "update:payout", Description: "Update payout requests", Resource: "payouts", Action: "update"},

	// Financial Management
	{Name: "read:financial", Description: "View financial data", Resource: "financial", Action: "read"},
	{Name: "create:financial", Description: "Create financial records", Resource: "financial", Action: "create"},
	{Name: "update:financial", Description: "Update financial records", Resource: "financial", Action: "update"},
	{Name: "summary:financial", Description: "View financial summaries", Resource: "financial", Action: "summary"},
	{Name: "sales:financial", Description: "View sales data", Resource: "financial", Action: "sales"},
	{Name: "bills:financial", Description: "View payment bills", Resource: "financial", Action: "bills"},

	// Analytics
	{Name: "read:analytics", Description: "View analytics", Resource: "analytics", Action: "read"},

	// Admin Only
	{Name: "admin:full", Description: "Full admin access", Resource: "admin", Action: "full"},
}

// InitializeSystemPermissions creates predefined system permissions
func (s *PermissionService) InitializeSystemPermissions() error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		for _, permission := range SystemPermissions {
			var existingPermission models.Permission
			err := tx.Where("name = ?", permission.Name).First(&existingPermission).Error
			if err == gorm.ErrRecordNotFound {
				// Permission doesn't exist, create it
				permission.ID = uuid.New()
				if err := tx.Create(&permission).Error; err != nil {
					return fmt.Errorf("failed to create permission %s: %w", permission.Name, err)
				}
			}
		}
		return nil
	})
}

// InitializeSystemRolesSafely creates predefined system roles and assigns permissions WITHOUT clearing existing ones
func (s *PermissionService) InitializeSystemRolesSafely() error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// Define roles and their permissions - MATCHING EXISTING DATABASE FORMAT
		roleDefinitions := map[string][]string{
			"admin": {
				// Profile
				"view:profile", "update:profile",
				// Events
				"read:event", "create:event", "update:event", "delete:event", "approve:event", "reject:event", "hold:event",
				// Users
				"read:user", "create:user", "update:user", "delete:user", "approve:organizer", "reject:organizer",
				// Tickets
				"create:ticket", "read:ticket", "scan:ticket", "checkin:ticket", "checkout:ticket",
				// Staff
				"manage:staff",
				// Payouts
				"create:payout", "read:payout", "update:payout",
				// Financial
				"read:financial", "create:financial", "update:financial", "summary:financial", "sales:financial", "bills:financial",
				// Analytics
				"read:analytics",
				// Admin Only
				"admin:full",
			},
			"subadmin": {
				// Same as admin for now
				"view:profile", "update:profile",
				"read:event", "create:event", "update:event", "delete:event", "approve:event", "reject:event", "hold:event",
				"read:user", "create:user", "update:user", "delete:user", "approve:organizer", "reject:organizer",
				"create:ticket", "read:ticket", "scan:ticket", "checkin:ticket", "checkout:ticket",
				"manage:staff",
				"create:payout", "read:payout", "update:payout",
				"read:financial", "create:financial", "update:financial", "summary:financial", "sales:financial", "bills:financial",
				"read:analytics",
			},
			"organizer": {
				"view:profile", "update:profile",
				"read:event", "create:event", "update:event", "delete:event",
				"read:user", "create:user", "update:user", "delete:user",
				"manage:staff",
				"create:ticket", "read:ticket", "scan:ticket", "checkin:ticket", "checkout:ticket",
				"create:payout", "read:payout",
				"summary:financial", "sales:financial", "bills:financial",
				"read:analytics",
			},
			"manager": {
				"view:profile", "update:profile",
				"read:event", "update:event",
				"read:user",
				"create:ticket", "read:ticket", "scan:ticket", "checkin:ticket", "checkout:ticket",
			},
			"staff": {
				"read:ticket", "scan:ticket", "checkin:ticket", "checkout:ticket",
			},
			"user": {
				"view:profile", "update:profile",
				"create:ticket", "read:ticket",
			},
		}

		for roleName, permissionNames := range roleDefinitions {
			// Find or create role
			var role models.Role
			err := tx.Where("name = ?", roleName).First(&role).Error
			if err == gorm.ErrRecordNotFound {
				// Create role
				role = models.Role{
					ID:          uuid.New(),
					Name:        roleName,
					Description: fmt.Sprintf("%s role", roleName),
				}
				if err := tx.Create(&role).Error; err != nil {
					return fmt.Errorf("failed to create role %s: %w", roleName, err)
				}
			}

			// Instead of clearing, check existing permissions and only add missing ones
			var existingPermissions []models.Permission
			if err := tx.Model(&role).Association("Permissions").Find(&existingPermissions); err != nil {
				return fmt.Errorf("failed to get existing permissions for role %s: %w", roleName, err)
			}

			// Create a map of existing permission names for quick lookup
			existingPermMap := make(map[string]bool)
			for _, perm := range existingPermissions {
				existingPermMap[perm.Name] = true
			}

			// Assign new permissions that don't already exist
			for _, permName := range permissionNames {
				if !existingPermMap[permName] {
					// Permission not assigned yet, add it
					var permission models.Permission
					if err := tx.Where("name = ?", permName).First(&permission).Error; err != nil {
						return fmt.Errorf("permission %s not found for role %s: %w", permName, roleName, err)
					}

					// Create role-permission association
					rolePermission := models.RolePermission{
						RoleID:       role.ID,
						PermissionID: permission.ID,
					}
					if err := tx.Create(&rolePermission).Error; err != nil {
						return fmt.Errorf("failed to assign permission %s to role %s: %w", permName, roleName, err)
					}
				}
			}
		}

		return nil
	})
}

// GetAllPermissions returns all permissions
func (s *PermissionService) GetAllPermissions() ([]models.Permission, error) {
	var permissions []models.Permission
	err := database.DB.Find(&permissions).Error
	return permissions, err
}

// GetPermissionsByRole returns permissions for a specific role
func (s *PermissionService) GetPermissionsByRole(roleID uuid.UUID) ([]models.Permission, error) {
	var role models.Role
	err := database.DB.Preload("Permissions").Where("id = ?", roleID).First(&role).Error
	if err != nil {
		return nil, err
	}

	permissions := make([]models.Permission, len(role.Permissions))
	for i, perm := range role.Permissions {
		permissions[i] = *perm
	}

	return permissions, nil
}

// AssignPermissionsToRole assigns permissions to a role
func (s *PermissionService) AssignPermissionsToRole(roleID uuid.UUID, permissionNames []string) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// Get the role
		var role models.Role
		if err := tx.First(&role, "id = ?", roleID).Error; err != nil {
			return fmt.Errorf("role not found: %w", err)
		}

		// Get permissions by names
		var permissions []models.Permission
		if err := tx.Where("name IN ?", permissionNames).Find(&permissions).Error; err != nil {
			return fmt.Errorf("failed to find permissions: %w", err)
		}

		// Clear existing permissions and assign new ones
		if err := tx.Model(&role).Association("Permissions").Clear(); err != nil {
			return fmt.Errorf("failed to clear existing permissions: %w", err)
		}

		permissionPointers := make([]*models.Permission, len(permissions))
		for i := range permissions {
			permissionPointers[i] = &permissions[i]
		}

		if err := tx.Model(&role).Association("Permissions").Append(permissionPointers); err != nil {
			return fmt.Errorf("failed to assign permissions: %w", err)
		}

		return nil
	})
}

// CheckUserPermission checks if a user has a specific permission
func (s *PermissionService) CheckUserPermission(userID uuid.UUID, permissionName string) (bool, error) {
	var count int64
	err := database.DB.Table("users").
		Joins("JOIN user_roles ON users.id = user_roles.user_id").
		Joins("JOIN roles ON user_roles.role_id = roles.id").
		Joins("JOIN role_permissions ON roles.id = role_permissions.role_id").
		Joins("JOIN permissions ON role_permissions.permission_id = permissions.id").
		Where("users.id = ? AND permissions.name = ? AND users.deleted_at IS NULL AND users.account_status = ?", userID, permissionName, "active").
		Count(&count).Error

	return count > 0, err
}

// GetUserPermissions returns all permissions for a user
func (s *PermissionService) GetUserPermissions(userID uuid.UUID) ([]models.Permission, error) {
	var permissions []models.Permission
	err := database.DB.Table("permissions").
		Joins("JOIN role_permissions ON permissions.id = role_permissions.permission_id").
		Joins("JOIN roles ON role_permissions.role_id = roles.id").
		Joins("JOIN user_roles ON roles.id = user_roles.role_id").
		Joins("JOIN users ON user_roles.user_id = users.id").
		Where("users.id = ? AND users.deleted_at IS NULL AND users.account_status = ?", userID, "active").
		Distinct().
		Find(&permissions).Error

	return permissions, err
}

// CreatePermission creates a new custom permission
func (s *PermissionService) CreatePermission(req *models.CreatePermissionRequest) (*models.Permission, error) {
	permission := &models.Permission{
		ID:          uuid.New(),
		Name:        req.Name,
		Description: req.Description,
		Resource:    req.Resource,
		Action:      req.Action,
	}

	err := database.DB.Create(permission).Error
	return permission, err
}

// UpdatePermission updates an existing permission
func (s *PermissionService) UpdatePermission(id uuid.UUID, req *models.UpdatePermissionRequest) error {
	var permission models.Permission
	if err := database.DB.First(&permission, "id = ?", id).Error; err != nil {
		return err
	}

	if req.Name != "" {
		permission.Name = req.Name
	}
	if req.Description != "" {
		permission.Description = req.Description
	}
	if req.Resource != "" {
		permission.Resource = req.Resource
	}
	if req.Action != "" {
		permission.Action = req.Action
	}

	return database.DB.Save(&permission).Error
}

// DeletePermission deletes a permission
func (s *PermissionService) DeletePermission(id uuid.UUID) error {
	return database.DB.Delete(&models.Permission{}, "id = ?", id).Error
}
