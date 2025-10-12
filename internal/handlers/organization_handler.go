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
	orgService *services.OrganizationService
}

func NewOrganizationHandler(cfg *config.Config) *OrganizationHandler {
	emailService := services.NewEmailService(cfg)
	return &OrganizationHandler{
		orgService: services.NewOrganizationService(emailService),
	}
}

// CreateOrganization godoc
// @Summary Create a new organization
// @Description Creates a new organization with the current user as the organizer
// @Tags organizations
// @Accept json
// @Produce json
// @Param request body models.CreateOrganizationRequest true "Organization data"
// @Security ApiKeyAuth
// @Success 201 {object} utils.Response{data=models.OrganizationResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /organizations [post]
func (h *OrganizationHandler) CreateOrganization(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userID")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Parse request body
	var req models.CreateOrganizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request data", err)
		return
	}

	// Create organization
	org, err := h.orgService.CreateOrganization(userID.(uuid.UUID), &req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to create organization", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Organization created successfully", org)
}

// CreateOrganizationUser godoc
// @Summary Create a new user in organization
// @Description Creates a new user with staff or manager role within the organization
// @Tags organizations
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
// @Router /organizations/{id}/users [post]
func (h *OrganizationHandler) CreateOrganizationUser(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userID")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid organization ID", err)
		return
	}

	// Parse request body
	var req models.CreateOrgUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request data", err)
		return
	}

	// Create user
	user, err := h.orgService.CreateOrgUser(userID.(uuid.UUID), orgID, &req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to create user", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Organization user created successfully", user)
}

// This duplicate GetUserOrganizations method has been removed to fix compilation errors

// GetOrganizationByID godoc
// @Summary Get organization by ID
// @Description Retrieves organization details by ID
// @Tags organizations
// @Accept json
// @Produce json
// @Param id path string true "Organization ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.OrganizationResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /organizations/{id} [get]
func (h *OrganizationHandler) GetOrganizationByID(c *gin.Context) {
	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid organization ID", err)
		return
	}

	// Get organization
	org, err := h.orgService.GetOrganizationByID(orgID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "Organization not found", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization retrieved successfully", org)
}

// GetOrganizationUsers godoc
// @Summary Get users in an organization
// @Description Retrieves all users associated with the specified organization
// @Tags organizations
// @Accept json
// @Produce json
// @Param id path string true "Organization ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=[]models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /organizations/{id}/users [get]
func (h *OrganizationHandler) GetOrganizationUsers(c *gin.Context) {
	// Check if user is authenticated (auth middleware already handles this)
	if _, exists := c.Get("userID"); !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid organization ID", err)
		return
	}

	// Get users in organization
	users, err := h.orgService.GetOrganizationUsers(orgID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to get organization users", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization users retrieved successfully", users)
}

// UpdateOrganizationUser godoc
// @Summary Update a user in organization
// @Description Updates role or status of a user within the organization
// @Tags organizations
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
// @Router /organizations/{id}/users/{userId} [put]
func (h *OrganizationHandler) UpdateOrganizationUser(c *gin.Context) {
	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid organization ID", err)
		return
	}

	// Parse user ID
	userID, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", err)
		return
	}

	// Parse request body
	var req models.UpdateOrgUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request data", err)
		return
	}

	// Update user
	user, err := h.orgService.UpdateOrganizationUser(orgID, userID, &req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to update organization user", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization user updated successfully", user)
}

// DeleteOrganizationUser godoc
// @Summary Delete a user from organization
// @Description Removes a user from the organization
// @Tags organizations
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
// @Router /organizations/{id}/users/{userId} [delete]
func (h *OrganizationHandler) DeleteOrganizationUser(c *gin.Context) {
	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid organization ID", err)
		return
	}

	// Parse user ID
	userID, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid user ID", err)
		return
	}

	// Delete user from organization
	if err := h.orgService.DeleteOrganizationUser(orgID, userID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to delete organization user", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization user deleted successfully", nil)
}

// UpdateOrganization godoc
// @Summary Update an organization
// @Description Updates details of an organization
// @Tags organizations
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
// @Router /organizations/{id} [put]
func (h *OrganizationHandler) UpdateOrganization(c *gin.Context) {
	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid organization ID", err)
		return
	}

	// Parse request body
	var req models.UpdateOrganizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request data", err)
		return
	}

	// Update organization
	org, err := h.orgService.UpdateOrganization(orgID, &req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to update organization", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization updated successfully", org)
}

// DeleteOrganization godoc
// @Summary Delete an organization
// @Description Deletes an organization and all associated data
// @Tags organizations
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
// @Router /organizations/{id} [delete]
func (h *OrganizationHandler) DeleteOrganization(c *gin.Context) {
	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid organization ID", err)
		return
	}

	// Delete organization
	if err := h.orgService.DeleteOrganization(orgID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to delete organization", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization deleted successfully", nil)
}

// UpdateUserRole godoc
// @Summary Update a user's role in organization
// @Description Updates a user's role within the organization
// @Tags organizations
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
// @Router /organizations/{orgId}/users/role [put]
func (h *OrganizationHandler) UpdateUserRole(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userIDValue, exists := c.Get("userID")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	userID := userIDValue.(uuid.UUID)

	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid organization ID", err)
		return
	}

	// Parse request body
	var req models.UpdateUserRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request data", err)
		return
	}

	// Update role
	err = h.orgService.UpdateOrgUserRole(userID, orgID, &req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to update user role", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "User role updated successfully", nil)
}

// GetOrgUsers godoc
// @Summary Get all users in organization
// @Description Gets all users belonging to the organization
// @Tags organizations
// @Produce json
// @Param orgId path string true "Organization ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=[]models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /organizations/{orgId}/users [get]
func (h *OrganizationHandler) GetOrgUsers(c *gin.Context) {
	// Check if user is authenticated
	if _, exists := c.Get("userID"); !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid organization ID", err)
		return
	}

	// Get users
	users, err := h.orgService.GetOrganizationUsers(orgID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to get users", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Users retrieved successfully", users)
}

// GetUserOrganizations godoc
// @Summary Get user's organizations
// @Description Gets all organizations where the user is an organizer
// @Tags organizations
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=[]models.OrganizationResponse}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /organizations/mine [get]
func (h *OrganizationHandler) GetUserOrganizations(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userIDValue, exists := c.Get("userID")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	userID := userIDValue.(uuid.UUID)

	// Get organizations
	orgs, err := h.orgService.GetUserOrganizations(userID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to get organizations", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organizations retrieved successfully", orgs)
}

// GetOrganization godoc
// @Summary Get organization details
// @Description Gets details of a specific organization
// @Tags organizations
// @Produce json
// @Param orgId path string true "Organization ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.OrganizationResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /organizations/{orgId} [get]
func (h *OrganizationHandler) GetOrganization(c *gin.Context) {
	// Parse organization ID
	orgID, err := uuid.Parse(c.Param("orgId"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid organization ID", err)
		return
	}

	// Get organization
	org, err := h.orgService.GetOrganizationByID(orgID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to get organization", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization retrieved successfully", org)
}
