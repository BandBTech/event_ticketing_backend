package handlers

import (
	"net/http"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type OrganizerUserHandler struct {
	authService *services.AuthService
}

func NewOrganizerUserHandler(authService *services.AuthService) *OrganizerUserHandler {
	return &OrganizerUserHandler{
		authService: authService,
	}
}

// getOrganizerIDForUser uses centralized utility
func (h *OrganizerUserHandler) getOrganizerIDForUser(userID uuid.UUID) (uuid.UUID, error) {
	return utils.GetOrganizerIDForUser(database.GetDB(), userID)
}

// GetOrganizerUsers godoc
// @Summary Get all users in the organizer's organization
// @Description Get paginated list of users belonging to the authenticated organizer's organization
// @Tags Organizer
// @Accept json
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param search query string false "Search by email, first name, or last name"
// @Param role query string false "Filter by role (staff, manager)" Enums(staff,manager)
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/users [get]
func (h *OrganizerUserHandler) GetOrganizerUsers(c *gin.Context) {
	// Get user ID from context
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get the organizer ID (handles scoping for staff/managers)
	organizerID, err := h.getOrganizerIDForUser(userID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Parse query parameters
	pagination := utils.GetPaginationParams(c, 10)
	search := c.DefaultQuery("search", "")
	role := c.DefaultQuery("role", "")

	// Validate role parameter
	if role != "" && role != "staff" && role != "manager" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get users for this organizer's organization
	users, total, err := h.authService.GetOrganizerUsers(organizerID, pagination.Page, pagination.Limit, search, role)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"users":      users,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Organizer users fetched successfully", response)
}

// CreateOrganizerUser godoc
// @Summary Create a new user in the organizer's organization
// @Description Create a new staff or manager user within the authenticated organizer's organization
// @Tags Organizer
// @Accept json
// @Produce json
// @Param request body models.CreateOrgUserRequest true "User creation data"
// @Security ApiKeyAuth
// @Success 201 {object} utils.Response{data=models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 409 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/users [post]
func (h *OrganizerUserHandler) CreateOrganizerUser(c *gin.Context) {
	// Get user ID from context
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get the organizer ID (handles scoping for staff/managers)
	organizerID, err := h.getOrganizerIDForUser(userID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	var req models.CreateOrgUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Create user in the organizer's organization
	user, err := h.authService.CreateOrganizerUser(organizerID, &req)
	if err != nil {
		if err.Error() == "user already exists" {
			utils.HandleError(c, err)
			return
		}
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Organizer user created successfully", user.ToResponse())
}

// UpdateOrganizerUser godoc
// @Summary Update a user in the organizer's organization
// @Description Update role and status of a user within the authenticated organizer's organization
// @Tags Organizer
// @Accept json
// @Produce json
// @Param user_id path string true "User ID"
// @Param request body models.UpdateOrgUserRequest true "User update data"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/users/{user_id} [put]
func (h *OrganizerUserHandler) UpdateOrganizerUser(c *gin.Context) {
	// Get user ID from context
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}
	currentUserID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get the organizer ID (handles scoping for staff/managers)
	organizerID, err := h.getOrganizerIDForUser(currentUserID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Parse user ID
	userIDStr := c.Param("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	var req models.UpdateOrgUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Update user in the organizer's organization
	user, err := h.authService.UpdateOrganizerUser(organizerID, userID, &req)
	if err != nil {
		if err.Error() == "user not found in organization" {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organizer user updated successfully", user.ToResponse())
}

// DeleteOrganizerUser godoc
// @Summary Delete a user from the organizer's organization
// @Description Soft delete a user from the authenticated organizer's organization
// @Tags Organizer
// @Accept json
// @Produce json
// @Param user_id path string true "User ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/users/{user_id} [delete]
func (h *OrganizerUserHandler) DeleteOrganizerUser(c *gin.Context) {
	// Get user ID from context
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}
	currentUserID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get the organizer ID (handles scoping for staff/managers)
	organizerID, err := h.getOrganizerIDForUser(currentUserID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Parse user ID
	userIDStr := c.Param("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Delete user from the organizer's organization
	err = h.authService.DeleteOrganizerUser(organizerID, userID)
	if err != nil {
		if err.Error() == "user not found in organization" {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organizer user deleted successfully", nil)
}
