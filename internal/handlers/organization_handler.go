package handlers

import (
	"net/http"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type OrganizationHandler struct {
	orgService  *services.OrganizationService
	authService *services.AuthService
}

func NewOrganizationHandler(cfg *config.Config, authService *services.AuthService) *OrganizationHandler {
	emailService := services.NewEmailService(cfg)
	return &OrganizationHandler{
		orgService:  services.NewOrganizationService(emailService),
		authService: authService,
	}
}

// CreateOrganization godoc
// @Summary Create a new organizer account (Admin only)
// @Description Admin can directly create an organizer account with pre-approved status
// @Tags Admin
// @Accept json
// @Produce json
// @Param request body models.AdminCreateOrganizerRequest true "Organizer account data with password"
// @Security ApiKeyAuth
// @Success 201 {object} utils.Response{data=models.UserResponse} "Organizer created successfully"
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response "Insufficient permissions"
// @Failure 409 {object} utils.Response "User already exists"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/organizer [post]
func (h *OrganizationHandler) CreateOrganization(c *gin.Context) {
	// Get admin ID from context (set by auth middleware)
	adminIDInterface, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	adminID, ok := adminIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}

	// Parse request body
	var req models.AdminCreateOrganizerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request body", err)
		return
	}

	// Create organizer account using auth service
	user, err := h.authService.AdminCreateOrganizer(adminID, &req)
	if err != nil {
		// Handle specific error types
		if err.Error() == "user with this email already exists" {
			utils.ConflictErrorResponse(c, "User with this email already exists", err)
			return
		}
		if err.Error() == "insufficient permissions: only admin or subadmin can create organizers" {
			utils.ForbiddenErrorResponse(c, "Insufficient permissions", err)
			return
		}
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to create organizer", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Organizer created and approved successfully", user.ToResponse())
}

// CreateOrganizationUser godoc
// @Summary Create a new user in organization
// @Description Creates a new user with staff or manager role within the organization
// @Tags Organizer
// @Accept json
// @Produce json
// @Param id path string true "Organization ID"
// @Param request body models.CreateOrgUserRequest true "User data"
// @Security ApiKeyAuth
// @Success 201 {object} utils.Response{data=models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/organizations/{id}/users [post]
func (h *OrganizationHandler) CreateOrganizationUser(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "Unauthorized", nil)
		return
	}

	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid organization ID", err)
		return
	}

	// Parse request body
	var req models.CreateOrgUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequestErrorResponse(c, "Invalid request data", err)
		return
	}

	// Create user
	err = h.orgService.CreateOrgUser(userID.(uuid.UUID), orgID, &req)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to create user", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Organization user created successfully", nil)
}

// This duplicate GetUserOrganizations method has been removed to fix compilation errors

// CreateOrganizationUser godoc

// GetOrganizationUsers godoc
// @Summary Get users in an organization
// @Description Retrieves all users associated with the specified organization
// @Tags Organizer
// @Accept json
// @Produce json
// @Param id path string true "Organization ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=[]models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/organizations/{id}/users [get]
func (h *OrganizationHandler) GetOrganizationUsers(c *gin.Context) {
	// Check if user is authenticated (auth middleware already handles this)
	if _, exists := c.Get("userID"); !exists {
		utils.UnauthorizedErrorResponse(c, "Unauthorized", nil)
		return
	}

	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid organization ID", err)
		return
	}

	// Get users in organization
	users, err := h.orgService.GetOrganizationUsers(orgID)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to get organization users", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization users retrieved successfully", users)
}

// UpdateOrganizationUser godoc
// @Summary Update a user in organization
// @Description Updates role or status of a user within the organization
// @Tags Organizer
// @Accept json
// @Produce json
// @Param id path string true "Organization ID"
// @Param userId path string true "User ID"
// @Param request body models.UpdateOrgUserRequest true "User data"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/organizations/{id}/users/{userId} [put]
func (h *OrganizationHandler) UpdateOrganizationUser(c *gin.Context) {
	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid organization ID", err)
		return
	}

	// Parse user ID
	userID, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid user ID", err)
		return
	}

	// Parse request body
	var req models.UpdateOrgUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequestErrorResponse(c, "Invalid request data", err)
		return
	}

	// Update user
	if err := h.orgService.UpdateOrganizationUser(orgID, userID, &req); err != nil {
		utils.InternalServerErrorResponse(c, "Failed to update organization user", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization user updated successfully", nil)
}

// DeleteOrganizationUser godoc
// @Summary Delete a user from organization
// @Description Removes a user from the organization
// @Tags Organizer
// @Accept json
// @Produce json
// @Param id path string true "Organization ID"
// @Param userId path string true "User ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/organizations/{id}/users/{userId} [delete]
func (h *OrganizationHandler) DeleteOrganizationUser(c *gin.Context) {
	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid organization ID", err)
		return
	}

	// Parse user ID
	userID, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid user ID", err)
		return
	}

	// Delete user from organization
	if err := h.orgService.DeleteOrganizationUser(orgID, userID); err != nil {
		utils.InternalServerErrorResponse(c, "Failed to delete organization user", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization user deleted successfully", nil)
}

// UpdateOrganization godoc
// @Summary Update an organization
// @Description Updates details of an organization
// @Tags Admin
// @Accept json
// @Produce json
// @Param id path string true "Organization ID"
// @Param request body models.UpdateOrganizationRequest true "Organization data"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.OrganizationResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/organizer/{id} [put]
func (h *OrganizationHandler) UpdateOrganization(c *gin.Context) {
	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid organization ID", err)
		return
	}

	// Parse request body
	var req models.UpdateOrganizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequestErrorResponse(c, "Invalid request data", err)
		return
	}

	// Update organization
	if err := h.orgService.UpdateOrganization(orgID, &req); err != nil {
		utils.InternalServerErrorResponse(c, "Failed to update organization", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization updated successfully", nil)
}

// DeleteOrganization godoc
// @Summary Delete an organization
// @Description Deletes an organization and all associated data
// @Tags Admin
// @Accept json
// @Produce json
// @Param id path string true "Organization ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/organizer/{id} [delete]
func (h *OrganizationHandler) DeleteOrganization(c *gin.Context) {
	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid organization ID", err)
		return
	}

	// Delete organization
	if err := h.orgService.DeleteOrganization(orgID); err != nil {
		utils.InternalServerErrorResponse(c, "Failed to delete organization", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization deleted successfully", nil)
}

// UpdateUserRole godoc
// @Summary Update a user's role in organization
// @Description Updates a user's role within the organization
// @Tags Organizer
// @Accept json
// @Produce json
// @Param orgId path string true "Organization ID"
// @Param request body models.UpdateUserRoleRequest true "Role update data"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/organizations/{orgId}/users/role [put]
func (h *OrganizationHandler) UpdateUserRole(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userIDValue, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "Unauthorized", nil)
		return
	}
	userID := userIDValue.(uuid.UUID)

	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid organization ID", err)
		return
	}

	// Parse request body
	var req models.UpdateUserRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequestErrorResponse(c, "Invalid request data", err)
		return
	}

	// Update role
	err = h.orgService.UpdateOrgUserRole(userID, orgID, &req)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to update user role", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "User role updated successfully", nil)
}

// GetOrgUsers godoc
// @Summary Get all users in organization
// @Description Gets all users belonging to the organization
// @Tags Organizer
// @Produce json
// @Param orgId path string true "Organization ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=[]models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/organizations/{orgId}/users [get]
func (h *OrganizationHandler) GetOrgUsers(c *gin.Context) {
	// Check if user is authenticated
	if _, exists := c.Get("userID"); !exists {
		utils.UnauthorizedErrorResponse(c, "Unauthorized", nil)
		return
	}

	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid organization ID", err)
		return
	}

	// Get users
	users, err := h.orgService.GetOrganizationUsers(orgID)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to get users", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Users retrieved successfully", users)
}

// CreateOrganization godoc
