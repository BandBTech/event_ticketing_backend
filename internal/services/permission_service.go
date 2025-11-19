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

// Predefined system permissions
var SystemPermissions = []models.Permission{
	// Event Management
	{Name: "event.create", Description: "Create events", Resource: ResourceEvent, Action: ActionCreate},
	{Name: "event.read", Description: "View events", Resource: ResourceEvent, Action: ActionRead},
	{Name: "event.update", Description: "Update events", Resource: ResourceEvent, Action: ActionUpdate},
	{Name: "event.delete", Description: "Delete events", Resource: ResourceEvent, Action: ActionDelete},
	{Name: "event.approve", Description: "Approve events", Resource: ResourceEvent, Action: ActionApprove},
	{Name: "event.control", Description: "Control event sales", Resource: ResourceEvent, Action: ActionControl},

	// User Management
	{Name: "user.create", Description: "Create users", Resource: ResourceUser, Action: ActionCreate},
	{Name: "user.read", Description: "View users", Resource: ResourceUser, Action: ActionRead},
	{Name: "user.update", Description: "Update users", Resource: ResourceUser, Action: ActionUpdate},
	{Name: "user.delete", Description: "Delete users", Resource: ResourceUser, Action: ActionDelete},
	{Name: "user.promote", Description: "Promote user roles", Resource: ResourceUser, Action: ActionPromote},
	{Name: "user.suspend", Description: "Suspend users", Resource: ResourceUser, Action: ActionSuspend},
	{Name: "user.manage", Description: "Manage users", Resource: ResourceUser, Action: ActionManage},

	// Organization Management
	{Name: "organization.create", Description: "Create organizations", Resource: ResourceOrganization, Action: ActionCreate},
	{Name: "organization.read", Description: "View organizations", Resource: ResourceOrganization, Action: ActionRead},
	{Name: "organization.update", Description: "Update organizations", Resource: ResourceOrganization, Action: ActionUpdate},
	{Name: "organization.delete", Description: "Delete organizations", Resource: ResourceOrganization, Action: ActionDelete},
	{Name: "organization.manage", Description: "Manage organizations", Resource: ResourceOrganization, Action: ActionManage},

	// Financial Management
	{Name: "financial.read", Description: "View financial data", Resource: ResourceFinancial, Action: ActionRead},
	{Name: "financial.manage", Description: "Manage financial data", Resource: ResourceFinancial, Action: ActionManage},
	{Name: "financial.export", Description: "Export financial data", Resource: ResourceFinancial, Action: ActionExport},

	// Payout Management
	{Name: "payout.read", Description: "View payouts", Resource: ResourcePayout, Action: ActionRead},
	{Name: "payout.approve", Description: "Approve payouts", Resource: ResourcePayout, Action: ActionApprove},
	{Name: "payout.manage", Description: "Manage payouts", Resource: ResourcePayout, Action: ActionManage},

	// Permission Management
	{Name: "permission.read", Description: "View permissions", Resource: ResourcePermission, Action: ActionRead},
	{Name: "permission.manage", Description: "Manage permissions", Resource: ResourcePermission, Action: ActionManage},

	// Role Management
	{Name: "role.read", Description: "View roles", Resource: ResourceRole, Action: ActionRead},
	{Name: "role.create", Description: "Create roles", Resource: ResourceRole, Action: ActionCreate},
	{Name: "role.update", Description: "Update roles", Resource: ResourceRole, Action: ActionUpdate},
	{Name: "role.delete", Description: "Delete roles", Resource: ResourceRole, Action: ActionDelete},

	// Analytics
	{Name: "analytics.view", Description: "View analytics", Resource: ResourceAnalytics, Action: ActionView},
	{Name: "analytics.export", Description: "Export analytics", Resource: ResourceAnalytics, Action: ActionExport},

	// Ticket Management
	{Name: "ticket.read", Description: "View tickets", Resource: ResourceTicket, Action: ActionRead},
	{Name: "ticket.manage", Description: "Manage tickets", Resource: ResourceTicket, Action: ActionManage},
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
func (s *PermissionService) UpdatePermission(id uuid.UUID, req *models.UpdatePermissionRequest) (*models.Permission, error) {
	var permission models.Permission
	if err := database.DB.First(&permission, "id = ?", id).Error; err != nil {
		return nil, err
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

	err := database.DB.Save(&permission).Error
	return &permission, err
}

// DeletePermission deletes a permission
func (s *PermissionService) DeletePermission(id uuid.UUID) error {
	return database.DB.Delete(&models.Permission{}, "id = ?", id).Error
}
