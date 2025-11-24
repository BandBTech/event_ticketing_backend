package handlers

import (
	"fmt"
	"net/http"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type UserManagementHandler struct {
	userMgmtService   *services.UserManagementService
	permissionService *services.PermissionService
}

func NewUserManagementHandler() *UserManagementHandler {
	return &UserManagementHandler{
		userMgmtService:   services.NewUserManagementService(),
		permissionService: services.NewPermissionService(),
	}
}

// GetAllUsers godoc
// @Summary Get all users with comprehensive search, filtering and sorting (Admin/SubAdmin)
// @Description Retrieve paginated list of users with advanced search, filter, and sort options
// @Tags Admin Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param search query string false "Search by name or email"
// @Param status query string false "Filter by account status (active, inactive, suspended)"
// @Param role query string false "Filter by role (user, organizer, subadmin, admin)"
// @Param org_status query string false "Filter by organizer status (pending, approved, rejected)"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param sort query string false "Sort by field with optional '-' prefix for desc (e.g., '-created_at', 'email', '-role')" default("-created_at")
// @Success 200 {object} utils.Response{data=object{users=[]models.User,total=int64,page=int,limit=int,has_more=bool}}
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/users [get]
func (h *UserManagementHandler) GetAllUsers(c *gin.Context) {
	// Parse query parameters
	req := &models.UserSearchRequest{}
	if err := c.ShouldBindQuery(req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid query parameters", err)
		return
	}

	// Set defaults
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit < 1 {
		req.Limit = 10
	}
	if req.Sort == "" {
		req.Sort = "-created_at"
	}

	users, total, err := h.userMgmtService.GetAllUsers(req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve users", err)
		return
	}

	// Convert to response format
	userResponses := make([]models.UserResponse, len(users))
	for i, user := range users {
		userResponses[i] = user.ToResponse()
	}

	response := map[string]interface{}{
		"users":    userResponses,
		"total":    total,
		"page":     req.Page,
		"limit":    req.Limit,
		"has_more": (req.Page * req.Limit) < int(total),
	}

	utils.SuccessResponse(c, http.StatusOK, "Users retrieved successfully", response)
}

// GetUserByID godoc
// @Summary Get user by ID (Admin/SubAdmin)
// @Description Retrieve detailed information about a specific user
// @Tags Admin Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "User ID"
// @Success 200 {object} utils.Response{data=models.User}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/users/{id} [get]
func (h *UserManagementHandler) GetUserByID(c *gin.Context) {
	userIDParam := c.Param("id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", err)
		return
	}

	user, err := h.userMgmtService.GetUserByID(userID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "User not found", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "User retrieved successfully", user.ToResponse())
}

// PromoteUser godoc
// @Summary Promote user role (Admin/SubAdmin)
// @Description Promote a user to a different role
// @Tags Admin Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "User ID"
// @Param request body models.PromoteUserRequest true "Promotion details"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/users/{id}/promote [put]
func (h *UserManagementHandler) PromoteUser(c *gin.Context) {
	userIDParam := c.Param("id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", err)
		return
	}

	var req models.PromoteUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	// Get current admin user ID
	adminID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "Admin user ID not found", nil)
		return
	}

	adminUUID, ok := adminID.(uuid.UUID)
	if !ok {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Invalid admin user ID format", nil)
		return
	}

	if err := h.userMgmtService.PromoteUser(userID, req.Role, adminUUID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to promote user", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "User promoted successfully", nil)
}

// UpdateAccountStatus godoc
// @Summary Update account status (Admin/SubAdmin)
// @Description Update a user's account status (active, inactive, suspended)
// @Tags Admin Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "User ID"
// @Param request body models.UpdateAccountStatusRequest true "Status update details"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/users/{id}/status [put]
func (h *UserManagementHandler) UpdateAccountStatus(c *gin.Context) {
	userIDParam := c.Param("id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", err)
		return
	}

	var req models.UpdateAccountStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	// Get current admin user ID
	adminID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "Admin user ID not found", nil)
		return
	}

	adminUUID, ok := adminID.(uuid.UUID)
	if !ok {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Invalid admin user ID format", nil)
		return
	}

	if err := h.userMgmtService.UpdateAccountStatus(userID, &req, adminUUID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to update account status", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Account status updated successfully", nil)
}

// SoftDeleteUser godoc
// @Summary Soft delete user (Admin/SubAdmin)
// @Description Soft delete a user account
// @Tags Admin Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "User ID"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/users/{id} [delete]
func (h *UserManagementHandler) SoftDeleteUser(c *gin.Context) {
	userIDParam := c.Param("id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", err)
		return
	}

	// Get current admin user ID
	adminID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "Admin user ID not found", nil)
		return
	}

	adminUUID, ok := adminID.(uuid.UUID)
	if !ok {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Invalid admin user ID format", nil)
		return
	}

	if err := h.userMgmtService.DeleteUser(userID, adminUUID, "soft"); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to delete user", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "User deleted successfully", nil)
}

// DeleteUser godoc
// @Summary Delete user with type (Admin/SubAdmin)
// @Description Delete a user account with specified delete type (soft or hard)
// @Tags Admin Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "User ID"
// @Param request body models.DeleteUserRequest true "Delete request with type"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/users/{id}/delete [delete]
func (h *UserManagementHandler) DeleteUser(c *gin.Context) {
	userIDParam := c.Param("id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", err)
		return
	}

	var req models.DeleteUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	// Get current admin user ID
	adminID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "Admin user ID not found", nil)
		return
	}

	adminUUID, ok := adminID.(uuid.UUID)
	if !ok {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Invalid admin user ID format", nil)
		return
	}

	if err := h.userMgmtService.DeleteUser(userID, adminUUID, req.DeleteType); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to delete user", err)
		return
	}

	deleteTypeMsg := "soft"
	if req.DeleteType == "hard" {
		deleteTypeMsg = "hard"
	}

	utils.SuccessResponse(c, http.StatusOK, fmt.Sprintf("User %s deleted successfully", deleteTypeMsg), nil)
}

// RestoreUser godoc
// @Summary Restore soft-deleted user (Admin Only)
// @Description Restore a soft-deleted user account
// @Tags Admin Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "User ID"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/users/{id}/restore [put]
func (h *UserManagementHandler) RestoreUser(c *gin.Context) {
	userIDParam := c.Param("id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", err)
		return
	}

	// Get current admin user ID
	adminID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "Admin user ID not found", nil)
		return
	}

	adminUUID, ok := adminID.(uuid.UUID)
	if !ok {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Invalid admin user ID format", nil)
		return
	}

	if err := h.userMgmtService.RestoreUser(userID, adminUUID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to restore user", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "User restored successfully", nil)
}

// BulkUserAction godoc
// @Summary Perform bulk actions on users (Admin/SubAdmin)
// @Description Perform bulk actions like activate, deactivate, suspend, delete, or promote multiple users
// @Tags Admin Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body models.BulkUserActionRequest true "Bulk action details"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/users/bulk-action [post]
func (h *UserManagementHandler) BulkUserAction(c *gin.Context) {
	var req models.BulkUserActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	// Get current admin user ID
	adminID, exists := c.Get("user_id")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "Admin user ID not found", nil)
		return
	}

	adminUUID, ok := adminID.(uuid.UUID)
	if !ok {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Invalid admin user ID format", nil)
		return
	}

	if err := h.userMgmtService.BulkUserAction(&req, adminUUID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to perform bulk action", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Bulk action completed successfully", nil)
}

// GetUserStatistics godoc
// @Summary Get user statistics (Admin/SubAdmin)
// @Description Get comprehensive user statistics including counts by status and role
// @Tags Admin Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=object}
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/users/statistics [get]
func (h *UserManagementHandler) GetUserStatistics(c *gin.Context) {
	stats, err := h.userMgmtService.GetUserStatistics()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve user statistics", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "User statistics retrieved successfully", stats)
}
