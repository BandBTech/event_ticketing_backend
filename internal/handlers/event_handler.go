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
	"gorm.io/gorm"
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
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}
	userIDStr := userID.String()

	// Parse multipart form with size limit
	fmt.Printf("[DEBUG] Parsing multipart form for user: %s\n", userIDStr)
	_, err := c.MultipartForm()
	if err != nil {
		fmt.Printf("[ERROR] Failed to parse multipart form: %v\n", err)
		if strings.Contains(err.Error(), "request body too large") {
			utils.BadRequestErrorResponse(c, "Request body too large. Maximum size allowed is 32MB", err)
			return
		}
		utils.BadRequestErrorResponse(c, "Failed to parse multipart form", err)
		return
	}

	// Extract and validate basic event data from form
	var req models.EventCreateRequest
	req.Title = strings.TrimSpace(c.PostForm("title"))
	req.Description = strings.TrimSpace(c.PostForm("description"))
	req.VenueName = strings.TrimSpace(c.PostForm("venue_name"))
	req.Address = strings.TrimSpace(c.PostForm("address"))
	req.Timezone = strings.TrimSpace(c.PostForm("timezone"))

	// Validate required fields
	if req.Title == "" {
		utils.BadRequestErrorResponse(c, "Event title is required", nil)
		return
	}
	if req.VenueName == "" {
		utils.BadRequestErrorResponse(c, "Venue name is required", nil)
		return
	}
	if req.Address == "" {
		utils.BadRequestErrorResponse(c, "Event address is required", nil)
		return
	}

	// Parse and validate dates
	startDateStr := c.PostForm("start_date")
	endDateStr := c.PostForm("end_date")
	if startDateStr == "" {
		utils.BadRequestErrorResponse(c, "Start date is required", nil)
		return
	}
	if endDateStr == "" {
		utils.BadRequestErrorResponse(c, "End date is required", nil)
		return
	}

	req.StartDate, err = time.Parse(time.RFC3339, startDateStr)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid start date format. Use RFC3339 format (e.g., 2024-12-25T18:00:00Z)", err)
		return
	}
	req.EndDate, err = time.Parse(time.RFC3339, endDateStr)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid end date format. Use RFC3339 format (e.g., 2024-12-25T22:00:00Z)", err)
		return
	}

	// Validate date logic
	if req.EndDate.Before(req.StartDate) {
		utils.BadRequestErrorResponse(c, "End date must be after start date", nil)
		return
	}
	if req.StartDate.Before(time.Now().Add(-24 * time.Hour)) {
		utils.BadRequestErrorResponse(c, "Start date cannot be more than 24 hours in the past", nil)
		return
	}

	// Parse and validate numeric fields
	capacityStr := c.PostForm("capacity")
	priceStr := c.PostForm("price")
	if capacityStr == "" {
		utils.BadRequestErrorResponse(c, "Event capacity is required", nil)
		return
	}
	if priceStr == "" {
		utils.BadRequestErrorResponse(c, "Ticket price is required", nil)
		return
	}

	req.Capacity, err = strconv.Atoi(capacityStr)
	if err != nil || req.Capacity <= 0 {
		utils.BadRequestErrorResponse(c, "Event capacity must be a positive integer", err)
		return
	}
	if req.Capacity > 100000 {
		utils.BadRequestErrorResponse(c, "Event capacity cannot exceed 100,000", nil)
		return
	}

	req.Price, err = strconv.ParseFloat(priceStr, 64)
	if err != nil || req.Price < 0 {
		utils.BadRequestErrorResponse(c, "Ticket price must be a valid non-negative number", err)
		return
	}
	if req.Price > 10000 {
		utils.BadRequestErrorResponse(c, "Ticket price cannot exceed 10,000", nil)
		return
	}

	fmt.Printf("[DEBUG] Event data validated: title=%s, venue=%s, capacity=%d, price=%.2f\n", req.Title, req.VenueName, req.Capacity, req.Price)

	// Start database transaction
	tx := database.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()

	// Check if user is admin to allow commission rate setting
	fmt.Printf("[DEBUG] Verifying user permissions for user: %s\n", userIDStr)
	var user models.User
	if err := tx.Preload("Roles").Where("id = ? AND deleted_at IS NULL", userIDStr).First(&user).Error; err != nil {
		tx.Rollback()
		fmt.Printf("[ERROR] Failed to verify user: %v\n", err)
		if err == gorm.ErrRecordNotFound {
			utils.NotFoundErrorResponse(c, "User not found", err)
			return
		}
		if strings.Contains(err.Error(), "connection") {
			utils.InternalServerErrorResponse(c, "Database connection error", err)
			return
		}
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
		commissionStr := c.PostForm("commission_rate")
		if commissionStr != "" {
			req.CommissionRate, err = strconv.ParseFloat(commissionStr, 64)
			if err != nil || req.CommissionRate < 0 || req.CommissionRate > 100 {
				tx.Rollback()
				utils.BadRequestErrorResponse(c, "Commission rate must be a valid percentage between 0 and 100", err)
				return
			}
		} else {
			req.CommissionRate = 0 // Default for admins if not specified
		}
	} else {
		req.CommissionRate = 0 // Default for organizers
	}

	// Parse and validate categories
	categoriesStr := strings.TrimSpace(c.PostForm("category"))
	if categoriesStr == "" {
		tx.Rollback()
		utils.BadRequestErrorResponse(c, "Event category is required", nil)
		return
	}
	req.Category = categoriesStr
	fmt.Printf("[DEBUG] User role check - isAdmin: %v, commissionRate: %.2f\n", isAdmin, req.CommissionRate)

	// Parse tiers from JSON string
	if tiersStr := c.PostForm("tiers"); tiersStr != "" {
		if err := json.Unmarshal([]byte(tiersStr), &req.Tiers); err != nil {
			tx.Rollback()
			utils.ValidationErrorResponse(c, "Invalid tiers format", err)
			return
		}
	}

	// Handle banner image upload
	fmt.Printf("[DEBUG] Checking for banner_image in form data...\n")
	if bannerFile, header, err := c.Request.FormFile("banner_image"); err == nil {
		defer bannerFile.Close()
		fmt.Printf("[DEBUG] Banner image found: %s, size: %d bytes\n", header.Filename, header.Size)

		// Validate file before upload (same as organizer profile validation)
		if header.Size == 0 {
			tx.Rollback()
			utils.BadRequestErrorResponse(c, "Banner image file is empty", nil)
			return
		}

		// Check file size (10MB limit for banners)
		if header.Size > 10*1024*1024 {
			tx.Rollback()
			utils.BadRequestErrorResponse(c, "Banner image file size must be less than 10MB", nil)
			return
		}

		// Check file type
		contentType := header.Header.Get("Content-Type")
		allowedTypes := []string{"image/jpeg", "image/jpg", "image/png", "image/webp"}
		isValidType := false
		for _, t := range allowedTypes {
			if contentType == t {
				isValidType = true
				break
			}
		}
		if !isValidType {
			tx.Rollback()
			utils.BadRequestErrorResponse(c, "Banner image must be a JPEG, PNG, or WebP image", nil)
			return
		}

		// Upload banner image first (without event ID)
		fmt.Printf("[DEBUG] Starting banner image upload for user: %s\n", userIDStr)
		bannerURL, err := h.fileStorageService.UploadFile(bannerFile, header, models.FileCategoryEventBanner, userID, &services.FileUploadOptions{
			AltText:     req.Title,
			Description: fmt.Sprintf("Banner image for event: %s", req.Title),
		})
		if err != nil {
			fmt.Printf("[ERROR] Banner image upload failed: %v\n", err)
			tx.Rollback()

			// Detailed error handling like in organizer profile
			if strings.Contains(err.Error(), "NoCredentialsProvided") {
				utils.InternalServerErrorResponse(c, "S3 credentials not configured", nil)
				return
			}
			if strings.Contains(err.Error(), "NoSuchBucket") {
				utils.InternalServerErrorResponse(c, "S3 bucket not found", nil)
				return
			}
			if strings.Contains(err.Error(), "AccessDenied") {
				utils.InternalServerErrorResponse(c, "S3 access denied", nil)
				return
			}
			if strings.Contains(err.Error(), "InvalidAccessKeyId") {
				utils.InternalServerErrorResponse(c, "Invalid S3 access key", nil)
				return
			}
			if strings.Contains(err.Error(), "image dimensions") {
				utils.BadRequestErrorResponse(c, "Banner image dimensions must be between 800x400 and 2000x1000 pixels", err)
				return
			}
			if strings.Contains(err.Error(), "file type") {
				utils.BadRequestErrorResponse(c, err.Error(), nil)
				return
			}
			if strings.Contains(err.Error(), "file size") {
				utils.BadRequestErrorResponse(c, err.Error(), nil)
				return
			}

			// Generic error
			utils.InternalServerErrorResponse(c, fmt.Sprintf("Failed to upload banner image: %s", err.Error()), err)
			return
		}
		fmt.Printf("[DEBUG] Banner image uploaded successfully: %s\n", bannerURL)
		req.BannerImage = bannerURL
	} else if err != http.ErrMissingFile {
		// Only return error if it's not a "missing file" error
		fmt.Printf("[DEBUG] Banner image form file error (not missing file): %v\n", err)
		tx.Rollback()
		utils.BadRequestErrorResponse(c, "Invalid banner image file", err)
		return
	} else {
		fmt.Printf("[DEBUG] No banner_image found in form data (missing file)\n")
	}

	fmt.Printf("[DEBUG] Creating event with title: %s\n", req.Title)
	event, err := h.service.CreateEventWithTx(&req, userIDStr, tx)
	if err != nil {
		tx.Rollback()
		fmt.Printf("[ERROR] Event creation failed: %v\n", err)

		// Handle specific database errors
		if strings.Contains(err.Error(), "duplicate key") {
			utils.ConflictErrorResponse(c, "Event with this title already exists for this organizer", err)
			return
		}
		if strings.Contains(err.Error(), "violates foreign key constraint") {
			utils.BadRequestErrorResponse(c, "Invalid organizer or related data", err)
			return
		}
		if strings.Contains(err.Error(), "value too long") {
			utils.BadRequestErrorResponse(c, "One or more fields exceed maximum length", err)
			return
		}
		if strings.Contains(err.Error(), "connection") {
			utils.InternalServerErrorResponse(c, "Database connection error", err)
			return
		}
		if strings.Contains(err.Error(), "check constraint") {
			utils.BadRequestErrorResponse(c, "Event data violates business rules", err)
			return
		}

		// Generic database error
		utils.InternalServerErrorResponse(c, "Failed to create event", err)
		return
	}
	fmt.Printf("[DEBUG] Event created successfully with ID: %s\n", event.ID)

	// Create tiers if provided
	if len(req.Tiers) > 0 {
		fmt.Printf("[DEBUG] Creating %d event tiers\n", len(req.Tiers))
		for i, tierReq := range req.Tiers {
			fmt.Printf("[DEBUG] Creating tier %d/%d\n", i+1, len(req.Tiers))
			tier, err := h.eventMgmtService.CreateEventTierWithTx(event.ID, userID, &tierReq, tx)
			if err != nil {
				tx.Rollback()
				fmt.Printf("[ERROR] Tier creation failed for tier %d: %v\n", i+1, err)

				// Handle specific tier creation errors
				if strings.Contains(err.Error(), "duplicate key") {
					utils.ConflictErrorResponse(c, fmt.Sprintf("Tier with similar properties already exists for this event (tier %d)", i+1), err)
					return
				}
				if strings.Contains(err.Error(), "violates foreign key constraint") {
					utils.BadRequestErrorResponse(c, fmt.Sprintf("Invalid tier data for tier %d", i+1), err)
					return
				}
				if strings.Contains(err.Error(), "check constraint") {
					utils.BadRequestErrorResponse(c, fmt.Sprintf("Tier %d data violates business rules (e.g., invalid price or quantity)", i+1), err)
					return
				}
				if strings.Contains(err.Error(), "value too long") {
					utils.BadRequestErrorResponse(c, fmt.Sprintf("One or more fields in tier %d exceed maximum length", i+1), err)
					return
				}

				// Generic tier creation error
				utils.InternalServerErrorResponse(c, fmt.Sprintf("Failed to create event tier %d", i+1), err)
				return
			}
			fmt.Printf("[DEBUG] Tier %d created successfully with ID: %s\n", i+1, tier.ID)
		}
		fmt.Printf("[DEBUG] All %d tiers created successfully\n", len(req.Tiers))
	} else {
		fmt.Printf("[DEBUG] No tiers provided for this event\n")
	}

	// Commit transaction
	fmt.Printf("[DEBUG] Committing transaction for event creation\n")
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		fmt.Printf("[ERROR] Transaction commit failed: %v\n", err)

		// Handle specific commit errors
		if strings.Contains(err.Error(), "connection") {
			utils.InternalServerErrorResponse(c, "Database connection lost during transaction commit", err)
			return
		}
		if strings.Contains(err.Error(), "deadlock") {
			utils.InternalServerErrorResponse(c, "Database deadlock detected. Please try again", err)
			return
		}
		if strings.Contains(err.Error(), "constraint") {
			utils.BadRequestErrorResponse(c, "Data constraint violation detected during commit", err)
			return
		}

		// Generic commit error
		utils.DatabaseErrorResponse(c, "Failed to commit event creation", err)
		return
	}

	fmt.Printf("[DEBUG] Event creation completed successfully for event ID: %s\n", event.ID)

	// Return the created event data in response
	responseData := map[string]interface{}{
		"event":   event,
		"message": "Event created successfully",
	}

	utils.SuccessResponse(c, http.StatusCreated, "Event created successfully", responseData)
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
	userIDStr := userID.String()

	// Get the event to check ownership
	event, err := h.service.GetEventByID(id)
	if err != nil {
		utils.NotFoundErrorResponse(c, "Event not found", err)
		return
	}

	// If not admin, check if user is the organizer of the event
	if !isAdmin {
		if event.OrganizerID.String() != userIDStr {
			utils.ForbiddenErrorResponse(c, "You don't have permission to update this event", nil)
			return
		}
	}

	event, err = h.service.UpdateEvent(id, &req)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to update event", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event updated successfully", nil)
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
	userIDStr := userID.String()

	// Get the event to check ownership
	event, err := h.service.GetEventByID(id)
	if err != nil {
		utils.NotFoundErrorResponse(c, "Event not found", err)
		return
	}

	// If not admin, check if user is the organizer of the event
	if !isAdmin {
		if event.OrganizerID.String() != userIDStr {
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
	userIDStr := userID.String()

	if err := h.service.ApproveEvent(id, userIDStr, &req); err != nil {
		utils.InternalServerErrorResponse(c, "Failed to process event approval", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event approval processed successfully", nil)
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
	userIDStr := userID.String()

	sortParam := c.DefaultQuery("sort", "-created_at")

	events, total, err := h.service.GetEventsByOrganizer(userIDStr, page, limit, sortParam)
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
