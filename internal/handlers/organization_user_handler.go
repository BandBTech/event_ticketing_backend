package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type OrganizationUserHandler struct {
	authService *services.AuthService
}

func NewOrganizationUserHandler(authService *services.AuthService) *OrganizationUserHandler {
	return &OrganizationUserHandler{
		authService: authService,
	}
}

// getOrganizerIDForUser returns the organizer ID for the given user
// For organizers: returns their user ID
// For staff/managers: returns their organization_id
func (h *OrganizationUserHandler) getOrganizerIDForUser(userID uuid.UUID) (uuid.UUID, error) {
	// Import database and models
	var user models.User
	if err := database.GetDB().Preload("Roles").Where("id = ?", userID).First(&user).Error; err != nil {
		return uuid.Nil, fmt.Errorf("user not found")
	}

	// Check if user is organizer
	isOrganizer := false
	for _, role := range user.Roles {
		if role.Name == "organizer" {
			isOrganizer = true
			break
		}
	}

	if isOrganizer {
		return userID, nil
	}

	// For staff/managers, check if they have organization_id
	if user.OrganizerID == nil {
		return uuid.Nil, fmt.Errorf("staff/manager does not belong to an organization")
	}

	return *user.OrganizerID, nil
}

// GetOrganizationUsers godoc
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
func (h *OrganizationUserHandler) GetOrganizationUsers(c *gin.Context) {
	// Get user ID from context
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}

	// Get the organizer ID (handles scoping for staff/managers)
	organizerID, err := h.getOrganizerIDForUser(userID)
	if err != nil {
		utils.ForbiddenErrorResponse(c, err.Error(), nil)
		return
	}

	// Parse query parameters
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	search := c.DefaultQuery("search", "")
	role := c.DefaultQuery("role", "")

	// Validate role parameter
	if role != "" && role != "staff" && role != "manager" {
		utils.BadRequestErrorResponse(c, "Invalid role parameter. Must be 'staff' or 'manager'", nil)
		return
	}

	// Get users for this organizer's organization
	users, total, err := h.authService.GetOrganizationUsers(organizerID, page, limit, search, role)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to fetch organization users", err)
		return
	}

	response := map[string]interface{}{
		"users": users,
		"total": total,
		"page":  page,
		"limit": limit,
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization users fetched successfully", response)
}

// CreateOrganizationUser godoc
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
func (h *OrganizationUserHandler) CreateOrganizationUser(c *gin.Context) {
	// Get user ID from context
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}

	// Get the organizer ID (handles scoping for staff/managers)
	organizerID, err := h.getOrganizerIDForUser(userID)
	if err != nil {
		utils.ForbiddenErrorResponse(c, err.Error(), nil)
		return
	}

	var req models.CreateOrgUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request body", err)
		return
	}

	// Create user in the organizer's organization
	user, err := h.authService.CreateOrganizationUser(organizerID, &req)
	if err != nil {
		if err.Error() == "user already exists" {
			utils.ConflictErrorResponse(c, "User with this email already exists", err)
			return
		}
		utils.InternalServerErrorResponse(c, "Failed to create organization user", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Organization user created successfully", user.ToResponse())
}

// UpdateOrganizationUser godoc
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
func (h *OrganizationUserHandler) UpdateOrganizationUser(c *gin.Context) {
	// Get user ID from context
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	currentUserID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}

	// Get the organizer ID (handles scoping for staff/managers)
	organizerID, err := h.getOrganizerIDForUser(currentUserID)
	if err != nil {
		utils.ForbiddenErrorResponse(c, err.Error(), nil)
		return
	}

	// Parse user ID
	userIDStr := c.Param("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid user ID format", err)
		return
	}

	var req models.UpdateOrgUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request body", err)
		return
	}

	// Update user in the organizer's organization
	user, err := h.authService.UpdateOrganizationUser(organizerID, userID, &req)
	if err != nil {
		if err.Error() == "user not found in organization" {
			utils.NotFoundErrorResponse(c, "User not found in your organization", nil)
			return
		}
		utils.InternalServerErrorResponse(c, "Failed to update organization user", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization user updated successfully", user.ToResponse())
}

// DeleteOrganizationUser godoc
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
func (h *OrganizationUserHandler) DeleteOrganizationUser(c *gin.Context) {
	// Get user ID from context
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	currentUserID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}

	// Get the organizer ID (handles scoping for staff/managers)
	organizerID, err := h.getOrganizerIDForUser(currentUserID)
	if err != nil {
		utils.ForbiddenErrorResponse(c, err.Error(), nil)
		return
	}

	// Parse user ID
	userIDStr := c.Param("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid user ID format", err)
		return
	}

	// Delete user from the organizer's organization
	err = h.authService.DeleteOrganizationUser(organizerID, userID)
	if err != nil {
		if err.Error() == "user not found in organization" {
			utils.NotFoundErrorResponse(c, "User not found in your organization", nil)
			return
		}
		utils.InternalServerErrorResponse(c, "Failed to delete organization user", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organization user deleted successfully", nil)
}
