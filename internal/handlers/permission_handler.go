package handlers

import (
	"net/http"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type PermissionHandler struct {
	permissionService *services.PermissionService
}

func NewPermissionHandler() *PermissionHandler {
	return &PermissionHandler{
		permissionService: services.NewPermissionService(),
	}
}

// GetAllPermissions godoc
// @Summary Get all system permissions (Admin Only)
// @Description Retrieve all available system permissions
// @Tags Admin Permissions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=[]models.Permission}
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/permissions [get]
func (h *PermissionHandler) GetAllPermissions(c *gin.Context) {
	permissions, err := h.permissionService.GetAllPermissions()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve permissions", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Permissions retrieved successfully", permissions)
}

// InitializeSystemPermissions godoc
// @Summary Initialize system permissions (Admin Only)
// @Description Initialize predefined system permissions
// @Tags Admin Permissions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/permissions/initialize [post]
func (h *PermissionHandler) InitializeSystemPermissions(c *gin.Context) {
	if err := h.permissionService.InitializeSystemPermissions(); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to initialize system permissions", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "System permissions initialized successfully", nil)
}

// CreatePermission godoc
// @Summary Create custom permission (Admin Only)
// @Description Create a new custom permission
// @Tags Admin Permissions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param permission body models.CreatePermissionRequest true "Permission details"
// @Success 201 {object} utils.Response{data=models.Permission}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/permissions [post]
func (h *PermissionHandler) CreatePermission(c *gin.Context) {
	var req models.CreatePermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	permission, err := h.permissionService.CreatePermission(&req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to create permission", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Permission created successfully", permission)
}

// UpdatePermission godoc
// @Summary Update permission (Admin Only)
// @Description Update an existing permission
// @Tags Admin Permissions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Permission ID"
// @Param permission body models.UpdatePermissionRequest true "Updated permission details"
// @Success 200 {object} utils.Response{data=models.Permission}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/permissions/{id} [put]
func (h *PermissionHandler) UpdatePermission(c *gin.Context) {
	idParam := c.Param("id")
	permissionID, err := uuid.Parse(idParam)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid permission ID", err)
		return
	}

	var req models.UpdatePermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	permission, err := h.permissionService.UpdatePermission(permissionID, &req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to update permission", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Permission updated successfully", permission)
}

// DeletePermission godoc
// @Summary Delete permission (Admin Only)
// @Description Delete a permission
// @Tags Admin Permissions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Permission ID"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/permissions/{id} [delete]
func (h *PermissionHandler) DeletePermission(c *gin.Context) {
	idParam := c.Param("id")
	permissionID, err := uuid.Parse(idParam)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid permission ID", err)
		return
	}

	if err := h.permissionService.DeletePermission(permissionID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to delete permission", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Permission deleted successfully", nil)
}

// GetRolePermissions godoc
// @Summary Get permissions for a role (Admin/SubAdmin)
// @Description Retrieve all permissions assigned to a specific role
// @Tags Admin Permissions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param roleId path string true "Role ID"
// @Success 200 {object} utils.Response{data=[]models.Permission}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/roles/{roleId}/permissions [get]
func (h *PermissionHandler) GetRolePermissions(c *gin.Context) {
	roleIDParam := c.Param("roleId")
	roleID, err := uuid.Parse(roleIDParam)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid role ID", err)
		return
	}

	permissions, err := h.permissionService.GetPermissionsByRole(roleID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve role permissions", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Role permissions retrieved successfully", permissions)
}

// AssignPermissionsToRole godoc
// @Summary Assign permissions to role (Admin Only)
// @Description Assign multiple permissions to a role
// @Tags Admin Permissions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param roleId path string true "Role ID"
// @Param permissions body object{permission_names=[]string} true "Permission names to assign"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/roles/{roleId}/permissions [post]
func (h *PermissionHandler) AssignPermissionsToRole(c *gin.Context) {
	roleIDParam := c.Param("roleId")
	roleID, err := uuid.Parse(roleIDParam)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid role ID", err)
		return
	}

	var req struct {
		PermissionNames []string `json:"permission_names" binding:"required,min=1"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	if err := h.permissionService.AssignPermissionsToRole(roleID, req.PermissionNames); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to assign permissions to role", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Permissions assigned to role successfully", nil)
}

// GetUserPermissions godoc
// @Summary Get user permissions (Admin/SubAdmin)
// @Description Retrieve all permissions for a specific user
// @Tags Admin Permissions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "User ID"
// @Success 200 {object} utils.Response{data=[]models.Permission}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/users/{id}/permissions [get]
func (h *PermissionHandler) GetUserPermissions(c *gin.Context) {
	userIDParam := c.Param("id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", err)
		return
	}

	permissions, err := h.permissionService.GetUserPermissions(userID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve user permissions", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "User permissions retrieved successfully", permissions)
}

// CheckUserPermission godoc
// @Summary Check user permission (Admin/SubAdmin)
// @Description Check if a user has a specific permission
// @Tags Admin Permissions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "User ID"
// @Param permission query string true "Permission name to check"
// @Success 200 {object} utils.Response{data=object{has_permission=bool}}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/users/{id}/permissions/check [get]
func (h *PermissionHandler) CheckUserPermission(c *gin.Context) {
	userIDParam := c.Param("id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", err)
		return
	}

	permissionName := c.Query("permission")
	if permissionName == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "Permission name is required", nil)
		return
	}

	hasPermission, err := h.permissionService.CheckUserPermission(userID, permissionName)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to check user permission", err)
		return
	}

	result := map[string]interface{}{
		"has_permission": hasPermission,
		"permission":     permissionName,
		"user_id":        userID,
	}

	utils.SuccessResponse(c, http.StatusOK, "Permission check completed", result)
}
