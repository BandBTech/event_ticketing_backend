package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type EventHandler struct {
	service            *services.EventService
	fileStorageService *services.FileStorageService
	eventMgmtService   *services.EventManagementService
}

func NewEventHandler(service *services.EventService, fileStorageService *services.FileStorageService) *EventHandler {
	return &EventHandler{
		service:            service,
		fileStorageService: fileStorageService,
		eventMgmtService:   services.NewEventManagementService(),
	}
}

// AdminCreateEvent godoc
// @Summary Create a new event (Admin)
// @Description Create a new event with the provided details (Admin only)
// @Tags Admin
// @Security ApiKeyAuth
// @Accept multipart/form-data
// @Produce json
// @Param title formData string true "Event title"
// @Param description formData string false "Event description"
// @Param banner_image formData file false "Event banner image"
// @Param category formData string true "Event categories (comma-separated)"
// @Param venue_name formData string true "Venue name"
// @Param address formData string true "Event address"
// @Param start_date formData string true "Start date (RFC3339 format)"
// @Param end_date formData string true "End date (RFC3339 format)"
// @Param timezone formData string false "Timezone"
// @Param capacity formData int true "Event capacity"
// @Param price formData number true "Ticket price"
// @Param commission_rate formData number false "Commission rate for admin"
// @Param tiers formData string false "Event tiers as JSON string array of {tier_id, price, currency, quantity, gst, sales_start, sales_end, sort_order}"
// @Success 201 {object} utils.Response{data=models.Event}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events [post]
func (h *EventHandler) AdminCreateEvent(c *gin.Context) {
	h.createEvent(c)
}

// OrganizerCreateEvent godoc
// @Summary Create a new event (Organizer)
// @Description Create a new event with the provided details (Organizer only)
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept multipart/form-data
// @Produce json
// @Param title formData string true "Event title"
// @Param description formData string false "Event description"
// @Param banner_image formData file false "Event banner image"
// @Param category formData string true "Event categories (comma-separated)"
// @Param venue_name formData string true "Venue name"
// @Param address formData string true "Event address"
// @Param start_date formData string true "Start date (RFC3339 format)"
// @Param end_date formData string true "End date (RFC3339 format)"
// @Param timezone formData string false "Timezone"
// @Param capacity formData int true "Event capacity"
// @Param price formData number true "Ticket price"
// @Param tiers formData string false "Event tiers as JSON string array of {tier_id, price, currency, quantity, gst, sales_start, sales_end, sort_order}"
// @Success 201 {object} utils.Response{data=models.Event}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events [post]
func (h *EventHandler) OrganizerCreateEvent(c *gin.Context) {
	h.createEvent(c)
}

// createEvent is a private method to handle event creation logic
func (h *EventHandler) createEvent(c *gin.Context) {
	// Get user from context (set by auth middleware)
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userIDStr := userIDInterface.(string)
	userID, _ := uuid.Parse(userIDStr)

	// Parse multipart form
	_, err := c.MultipartForm()
	if err != nil {
		utils.BadRequestErrorResponse(c, "Failed to parse multipart form", err)
		return
	}

	// Extract basic event data from form
	var req models.EventCreateRequest
	req.Title = c.PostForm("title")
	req.Description = c.PostForm("description")
	req.VenueName = c.PostForm("venue_name")
	req.Address = c.PostForm("address")
	req.StartDate, _ = time.Parse(time.RFC3339, c.PostForm("start_date"))
	req.EndDate, _ = time.Parse(time.RFC3339, c.PostForm("end_date"))
	req.Timezone = c.PostForm("timezone")
	req.Capacity, _ = strconv.Atoi(c.PostForm("capacity"))
	req.Price, _ = strconv.ParseFloat(c.PostForm("price"), 64)

	// Check if user is admin to allow commission rate setting
	var user models.User
	if err := database.DB.Preload("Roles").Where("id = ?", userID).First(&user).Error; err != nil {
		utils.InternalServerErrorResponse(c, "Failed to verify user permissions", err)
		return
	}

	isAdmin := false
	for _, role := range user.Roles {
		if role.Name == "admin" {
			isAdmin = true
			break
		}
	}

	// Only parse commission rate for admins
	if isAdmin {
		req.CommissionRate, _ = strconv.ParseFloat(c.PostForm("commission_rate"), 64)
	} else {
		req.CommissionRate = 0 // Default for organizers
	}

	// Parse categories
	if categoriesStr := c.PostForm("category"); categoriesStr != "" {
		req.Category = strings.Split(categoriesStr, ",")
		// Trim spaces
		for i, cat := range req.Category {
			req.Category[i] = strings.TrimSpace(cat)
		}
	}

	// Parse tiers from JSON string
	if tiersStr := c.PostForm("tiers"); tiersStr != "" {
		if err := json.Unmarshal([]byte(tiersStr), &req.Tiers); err != nil {
			utils.ValidationErrorResponse(c, "Invalid tiers format", err)
			return
		}
	}

	// Handle banner image upload
	if bannerFile, header, err := c.Request.FormFile("banner_image"); err == nil {
		defer bannerFile.Close()

		// Upload banner image first (without event ID)
		bannerURL, err := h.fileStorageService.UploadFile(bannerFile, header, models.FileCategoryEventBanner, userID, &services.FileUploadOptions{
			AltText:     req.Title,
			Description: fmt.Sprintf("Banner image for event: %s", req.Title),
		})
		if err != nil {
			utils.InternalServerErrorResponse(c, "Failed to upload banner image", err)
			return
		}
		req.BannerImage = bannerURL
	}

	event, err := h.service.CreateEvent(&req, userIDStr)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to create event", err)
		return
	}

	// Create tiers if provided
	if len(req.Tiers) > 0 {
		for _, tierReq := range req.Tiers {
			_, err := h.eventMgmtService.CreateEventTier(event.ID, userID, &tierReq)
			if err != nil {
				// If tier creation fails, we should probably delete the event or handle the error
				// For now, we'll log the error but continue
				fmt.Printf("Warning: failed to create tier for event %s: %v\n", event.ID, err)
			}
		}
	}

	utils.SuccessResponse(c, http.StatusCreated, "Event created successfully", event)
}

// PublicGetAllEvents godoc
// @Summary Get all approved events (Public)
// @Description Get a list of all approved events with pagination, search, and filtering
// @Tags Public
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param search query string false "Search by event title or description"
// @Param location query string false "Filter by location"
// @Param start_date query string false "Filter by start date (YYYY-MM-DD)"
// @Param end_date query string false "Filter by end date (YYYY-MM-DD)"
// @Param min_price query number false "Filter by minimum price"
// @Param max_price query number false "Filter by maximum price"
// @Param sort query string false "Sort by field with optional '-' prefix for desc (e.g., '-created_at', 'title')" default("-created_at")
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/events [get]
func (h *EventHandler) PublicGetAllEvents(c *gin.Context) {
	// Pagination params
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	// Validation
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	// Filter params
	search := c.Query("search")
	location := c.Query("location")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	minPriceStr := c.Query("min_price")
	maxPriceStr := c.Query("max_price")
	sortParam := c.DefaultQuery("sort", "-created_at")

	// Parse price filters
	var minPrice, maxPrice *float64
	if minPriceStr != "" {
		if val, err := strconv.ParseFloat(minPriceStr, 64); err == nil {
			minPrice = &val
		}
	}
	if maxPriceStr != "" {
		if val, err := strconv.ParseFloat(maxPriceStr, 64); err == nil {
			maxPrice = &val
		}
	}

	// Validate and parse sort parameters
	validSortFields := map[string]bool{
		"title": true, "start_date": true, "price": true, "created_at": true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, validSortFields, "created_at", "desc")

	events, total, err := h.service.GetFilteredEvents("approved", page, limit, search, location, startDate, endDate, minPrice, maxPrice, sortBy, sortOrder)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to fetch events", err)
		return
	}

	response := map[string]interface{}{
		"events":      events,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": (total + int64(limit) - 1) / int64(limit), // Calculate total pages
	}
	utils.SuccessResponse(c, http.StatusOK, "Events fetched successfully", response)
}

// AdminGetAllEvents godoc
// @Summary Get all events (Admin)
// @Description Get a list of all events with pagination, search, and filtering (Admin only)
// @Tags Admin
// @Security ApiKeyAuth
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param search query string false "Search by event title or description"
// @Param location query string false "Filter by location"
// @Param status query string false "Filter by status (draft, pending, approved, held, rejected)"
// @Param organizer_id query string false "Filter by organizer ID"
// @Param start_date query string false "Filter by start date (YYYY-MM-DD)"
// @Param end_date query string false "Filter by end date (YYYY-MM-DD)"
// @Param min_price query number false "Filter by minimum price"
// @Param max_price query number false "Filter by maximum price"
// @Param sort query string false "Sort by field with optional '-' prefix for desc (e.g., '-created_at', 'title', '-status')" default("-created_at")
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events [get]
func (h *EventHandler) AdminGetAllEvents(c *gin.Context) {
	// Pagination params
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	// Validation
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	// Filter params
	search := c.Query("search")
	location := c.Query("location")
	status := c.DefaultQuery("status", "") // Empty means all statuses for admin
	c.Query("organizer_id")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	minPriceStr := c.Query("min_price")
	maxPriceStr := c.Query("max_price")
	sortParam := c.DefaultQuery("sort", "-created_at")

	// Parse price filters
	var minPrice, maxPrice *float64
	if minPriceStr != "" {
		if val, err := strconv.ParseFloat(minPriceStr, 64); err == nil {
			minPrice = &val
		}
	}
	if maxPriceStr != "" {
		if val, err := strconv.ParseFloat(maxPriceStr, 64); err == nil {
			maxPrice = &val
		}
	}

	// Validate and parse sort parameters
	validSortFields := map[string]bool{
		"title": true, "start_date": true, "price": true, "created_at": true, "status": true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, validSortFields, "created_at", "desc")

	events, total, err := h.service.GetFilteredEvents(status, page, limit, search, location, startDate, endDate, minPrice, maxPrice, sortBy, sortOrder)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to fetch events", err)
		return
	}

	response := map[string]interface{}{
		"events":      events,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": (total + int64(limit) - 1) / int64(limit),
	}
	utils.SuccessResponse(c, http.StatusOK, "Events fetched successfully", response)
}

// PublicGetEventByID godoc
// @Summary Get event by ID (Public)
// @Description Get details of a specific event by ID
// @Tags Public
// @Produce json
// @Param id path int true "Event ID"
// @Success 200 {object} utils.Response{data=models.Event}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Router /api/v1/public/events/{id} [get]
func (h *EventHandler) PublicGetEventByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid event ID", err)
		return
	}

	event, err := h.service.GetEventByID(id)
	if err != nil {
		utils.NotFoundErrorResponse(c, "Event not found", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event fetched successfully", event)
}

// AdminUpdateEvent godoc
// @Summary Update an event (Admin)
// @Description Update event details by ID (Admin only)
// @Tags Admin
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param id path int true "Event ID"
// @Param event body models.EventUpdateRequest true "Updated event details"
// @Success 200 {object} utils.Response{data=models.Event}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events/{id} [put]
func (h *EventHandler) AdminUpdateEvent(c *gin.Context) {
	h.updateEvent(c, true)
}

// OrganizerUpdateEvent godoc
// @Summary Update an event (Organizer)
// @Description Update event details by ID (Organizer only)
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param id path int true "Event ID"
// @Param event body models.EventUpdateRequest true "Updated event details"
// @Success 200 {object} utils.Response{data=models.Event}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/{id} [put]
func (h *EventHandler) OrganizerUpdateEvent(c *gin.Context) {
	h.updateEvent(c, false)
}

// updateEvent is a private method to handle event update logic
func (h *EventHandler) updateEvent(c *gin.Context, isAdmin bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid event ID", err)
		return
	}

	var req models.EventUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request body", err)
		return
	}

	// Get user from context (set by auth middleware)
	userID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	// Get the event to check ownership
	event, err := h.service.GetEventByID(id)
	if err != nil {
		utils.NotFoundErrorResponse(c, "Event not found", err)
		return
	}

	// If not admin, check if user is the organizer of the event
	if !isAdmin {
		userUUID := userID.(string)
		if event.OrganizerID.String() != userUUID {
			utils.ForbiddenErrorResponse(c, "You don't have permission to update this event", nil)
			return
		}
	}

	event, err = h.service.UpdateEvent(id, &req)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to update event", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event updated successfully", event)
}

// AdminDeleteEvent godoc
// @Summary Delete an event (Admin)
// @Description Delete an event by ID (Admin only)
// @Tags Admin
// @Security ApiKeyAuth
// @Produce json
// @Param id path int true "Event ID"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events/{id} [delete]
func (h *EventHandler) AdminDeleteEvent(c *gin.Context) {
	h.deleteEvent(c, true)
}

// OrganizerDeleteEvent godoc
// @Summary Delete an event (Organizer)
// @Description Delete an event by ID (Organizer only)
// @Tags Organizer
// @Security ApiKeyAuth
// @Produce json
// @Param id path int true "Event ID"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/{id} [delete]
func (h *EventHandler) OrganizerDeleteEvent(c *gin.Context) {
	h.deleteEvent(c, false)
}

// deleteEvent is a private method to handle event deletion logic
func (h *EventHandler) deleteEvent(c *gin.Context, isAdmin bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid event ID", err)
		return
	}

	// Get user from context (set by auth middleware)
	userID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	// Get the event to check ownership
	event, err := h.service.GetEventByID(id)
	if err != nil {
		utils.NotFoundErrorResponse(c, "Event not found", err)
		return
	}

	// If not admin, check if user is the organizer of the event
	if !isAdmin {
		userUUID := userID.(string)
		if event.OrganizerID.String() != userUUID {
			utils.ForbiddenErrorResponse(c, "You don't have permission to delete this event", nil)
			return
		}
	}

	// Delete associated files (hard delete)
	if err := h.fileStorageService.DeleteFilesByEntity("event", id); err != nil {
		// Log error but continue with event deletion
		fmt.Printf("Warning: failed to delete associated files: %v\n", err)
	}

	if err := h.service.DeleteEvent(id); err != nil {
		utils.InternalServerErrorResponse(c, "Failed to delete event", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event deleted successfully", nil)
}

// AdminApproveEvent godoc
// @Summary Approve, hold, or reject an event (Admin)
// @Description Allow admin/subadmin to approve, hold, or reject events with remarks
// @Tags Admin
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param id path int true "Event ID"
// @Param approval body models.EventApprovalRequest true "Approval details"
// @Success 200 {object} utils.Response{data=models.Event}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events/{id}/approval [put]
func (h *EventHandler) AdminApproveEvent(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid event ID", err)
		return
	}

	var req models.EventApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request body", err)
		return
	}

	// Get user from context (set by auth middleware)
	userID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	event, err := h.service.ApproveEvent(id, userID.(string), &req)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to process event approval", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event approval processed successfully", event)
}

// AdminGetEventsForApproval godoc
// @Summary Get events pending approval (Admin)
// @Description Get list of events that need admin/subadmin approval
// @Tags Admin
// @Security ApiKeyAuth
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param sort query string false "Sort by field with optional '-' prefix for desc (e.g., '-created_at', 'title')" default("-created_at")
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events/pending [get]
func (h *EventHandler) AdminGetEventsForApproval(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	sortParam := c.DefaultQuery("sort", "-created_at")

	events, total, err := h.service.GetEventsByStatus("pending", page, limit, sortParam)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to fetch pending events", err)
		return
	}

	response := map[string]interface{}{
		"events": events,
		"total":  total,
		"page":   page,
		"limit":  limit,
	}
	utils.SuccessResponse(c, http.StatusOK, "Pending events fetched successfully", response)
}

// OrganizerGetEvents godoc
// @Summary Get events for specific organizer (Organizer)
// @Description Get paginated list of events created by the authenticated organizer
// @Tags Organizer
// @Security ApiKeyAuth
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param sort query string false "Sort by field with optional '-' prefix for desc (e.g., '-created_at', 'title', '-status')" default("-created_at")
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events [get]
func (h *EventHandler) OrganizerGetEvents(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	// Get user from context (set by auth middleware)
	userID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	sortParam := c.DefaultQuery("sort", "-created_at")

	events, total, err := h.service.GetEventsByOrganizer(userID.(string), page, limit, sortParam)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to fetch organizer events", err)
		return
	}

	response := map[string]interface{}{
		"events": events,
		"total":  total,
		"page":   page,
		"limit":  limit,
	}
	utils.SuccessResponse(c, http.StatusOK, "Organizer events fetched successfully", response)
}
