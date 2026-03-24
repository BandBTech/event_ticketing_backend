package handlers

import (
	"net/http"
	"strings"

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
// @Param sort query string false "Sort by field with optional '-' prefix for desc (e.g., '-created_at', 'first_name')" default("-created_at")
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
	sortParam := c.DefaultQuery("sort", "-created_at")

	// Validate role parameter
	if role != "" && role != "staff" && role != "manager" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get users for this organizer's organization
	users, total, err := h.authService.GetOrganizerUsers(organizerID, pagination.Page, pagination.Limit, search, role, sortParam)
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
	user, isNewUser, err := h.authService.CreateOrganizerUser(organizerID, &req)
	if err != nil {
		if strings.Contains(err.Error(), "user_already_belongs_to_organizer") {
			utils.ConflictErrorResponse(c, "This user is already associated with an organization and cannot be added.", nil)
			return
		}
		utils.HandleError(c, err)
		return
	}

	// Return appropriate message based on whether user was newly created or attached
	if isNewUser {
		utils.SuccessResponse(c, http.StatusCreated, "Organizer user created successfully", user.ToResponse())
	} else {
		utils.SuccessResponse(c, http.StatusOK, "An account with this email already exists. The user has been added to your organizer.", user.ToResponse())
	}
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

// ListAllEntities godoc
// @Summary List all entities without pagination (Organizer only)
// @Description Get a list of all entities of a specific type without pagination. Supported types: events (organizer's own events), users (staff/managers in organizer's organization)
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param type query string true "Entity type to list" Enums(events,users)
// @Param search query string false "Search term for users (email, first name, last name, or full name)"
// @Success 200 {object} utils.Response{data=[]MinimalEventResponse} "List of events"
// @Success 200 {object} utils.Response{data=[]MinimalUserResponse} "List of users"
// @Failure 400 {object} utils.Response "Bad request - missing or invalid type parameter"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 403 {object} utils.Response "Forbidden - Organizer access required"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/organizer/list-all [get]
func (h *OrganizerUserHandler) ListAllEntities(c *gin.Context) {
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

	entityType := c.Query("type")
	search := c.Query("search")
	if entityType == "" {
		utils.BadRequestErrorResponse(c, "Entity type is required. Use ?type=events|users", nil)
		return
	}

	switch entityType {
	case "events":
		events, err := h.listAllOrganizerEvents(organizerID)
		if err != nil {
			utils.HandleError(c, err)
			return
		}
		utils.SuccessResponse(c, http.StatusOK, "Events retrieved successfully", events)

	case "users":
		users, err := h.listAllOrganizerUsers(organizerID, search)
		if err != nil {
			utils.HandleError(c, err)
			return
		}
		utils.SuccessResponse(c, http.StatusOK, "Users retrieved successfully", users)

	default:
		utils.BadRequestErrorResponse(c, "Invalid entity type. Supported types: events, users", nil)
		return
	}
}

// Helper methods for listing all entities

func (h *OrganizerUserHandler) listAllOrganizerEvents(organizerID uuid.UUID) ([]MinimalEventResponse, error) {
	var events []models.Event
	db := database.GetDB()

	// Get all events for this organizer sorted by created_at
	if err := db.Where("organizer_id = ?", organizerID).
		Order("created_at DESC").
		Find(&events).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to get events.", err)
	}

	var responses []MinimalEventResponse
	for _, event := range events {
		responses = append(responses, MinimalEventResponse{
			ID:    event.ID,
			Title: event.Title,
		})
	}

	return responses, nil
}

func (h *OrganizerUserHandler) listAllOrganizerUsers(organizerID uuid.UUID, search string) ([]MinimalUserResponse, error) {
	var users []models.User
	db := database.GetDB()

	// Get all staff and managers for this organizer
	query := db.
		Joins("JOIN user_roles ON users.id = user_roles.user_id").
		Joins("JOIN roles ON user_roles.role_id = roles.id").
		Where("roles.name IN ?", []string{"staff", "manager"}).
		Where("users.organizer_id = ?", organizerID).
		Where("users.deleted_at IS NULL")

	// Apply search filter
	if search != "" {
		searchTerm := "%" + search + "%"
		query = query.Where("users.email ILIKE ? OR users.first_name ILIKE ? OR users.last_name ILIKE ? OR (COALESCE(users.first_name, '') || ' ' || COALESCE(users.last_name, '')) ILIKE ?",
			searchTerm, searchTerm, searchTerm, searchTerm)
	}

	if err := query.Find(&users).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to get users.", err)
	}

	var responses []MinimalUserResponse
	for _, user := range users {
		responses = append(responses, MinimalUserResponse{
			ID:    user.ID,
			Name:  user.FirstName + " " + user.LastName,
			Email: user.Email,
		})
	}

	return responses, nil
}
