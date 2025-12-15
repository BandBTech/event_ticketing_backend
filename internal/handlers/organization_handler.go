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

// CreateOrganization godoc
