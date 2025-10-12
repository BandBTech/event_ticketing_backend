package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Role represents a role in the system
type Role struct {
	ID          uuid.UUID     `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	Name        string        `gorm:"unique;not null" json:"name"`
	Description string        `json:"description"`
	Users       []*User       `gorm:"many2many:user_roles;" json:"users,omitempty"`
	Permissions []*Permission `gorm:"many2many:role_permissions;" json:"permissions,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// RolePermission represents the many-to-many relationship between roles and permissions
type RolePermission struct {
	RoleID       uuid.UUID `gorm:"type:uuid;primaryKey" json:"role_id"`
	PermissionID uuid.UUID `gorm:"type:uuid;primaryKey" json:"permission_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// CreateRoleRequest is the request structure for creating a new role
type CreateRoleRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions" binding:"required"`
}

// UpdateRoleRequest is the request structure for updating a role
type UpdateRoleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// RoleResponse is the response structure for role data
type RoleResponse struct {
	ID          uuid.UUID            `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Permissions []PermissionResponse `json:"permissions,omitempty"`
}

// BeforeCreate is a GORM hook to set a UUID before creating a record
func (r *Role) BeforeCreate(tx *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}

// ToResponse converts a Role model to a RoleResponse
func (r *Role) ToResponse() RoleResponse {
	permissionResponses := []PermissionResponse{}
	if r.Permissions != nil {
		permissionResponses = make([]PermissionResponse, len(r.Permissions))
		for i, permission := range r.Permissions {
			permissionResponses[i] = permission.ToResponse()
		}
	}

	return RoleResponse{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		Permissions: permissionResponses,
	}
}
