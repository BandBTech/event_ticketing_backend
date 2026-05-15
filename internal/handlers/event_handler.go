package handlers

import (
	"encoding/json"
	"errors"
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
	payoutService      *services.PayoutService
}

func NewEventHandler(service *services.EventService, fileStorageService *services.FileStorageService, refundQueueService *services.RefundQueueService) *EventHandler {
	return &EventHandler{
		service:            service,
		fileStorageService: fileStorageService,
		eventMgmtService:   services.NewEventManagementService(refundQueueService),
		payoutService:      services.NewPayoutService(),
	}
}

// getOrganizerIDForUser uses centralized utility
func (h *EventHandler) getOrganizerIDForUser(userID uuid.UUID) (uuid.UUID, error) {
	return utils.GetOrganizerIDForUser(database.GetDB(), userID)
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
// @Param banner_image formData file true "Event banner image"
// @Param category formData string true "Event categories (comma-separated like \"Music,Art,Sports\")"
// @Param venue_name formData string true "Venue name"
// @Param address formData string true "Event address"
// @Param start_date formData string true "Start date (RFC3339 format)"
// @Param end_date formData string true "End date (RFC3339 format)"
// @Param timezone formData string false "Timezone"
// @Param capacity formData int true "Event capacity"
// @Param price formData number true "Ticket price"
// @Param tiers formData string false "Event tiers as JSON array: [{\"tier_template_id\":\"uuid\",\"price\":600,\"quantity\":500,\"sales_start\":\"2025-12-01T15:04:05Z\",\"sales_end\":\"2025-12-01T15:04:05Z\"}]"
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
	userID, ok := utils.HandleUserIDExtraction(c)
	if !ok {
		return
	}

	// Get the organizer ID (for staff/managers, it's their organizer_id)
	organizerID, err := h.getOrganizerIDForUser(userID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	userIDStr := userID.String()
	organizerIDStr := organizerID.String()

	// Parse multipart form with size limit
	fmt.Printf("[DEBUG] Parsing multipart form for user: %s\n", userIDStr)
	_, err = c.MultipartForm()
	if err != nil {
		fmt.Printf("[ERROR] Failed to parse multipart form: %v\n", err)
		if strings.Contains(err.Error(), "request body too large") {
			utils.HandleError(c, err)
			return
		}
		utils.HandleError(c, err)
		return
	}

	// Extract and validate basic event data from form
	var req models.EventCreateRequest
	req.Title = strings.TrimSpace(c.PostForm("title"))
	req.Description = strings.TrimSpace(c.PostForm("description"))
	req.VenueName = strings.TrimSpace(c.PostForm("venue_name"))
	req.Address = strings.TrimSpace(c.PostForm("address"))
	req.Country = strings.TrimSpace(c.PostForm("country"))
	req.EventType = strings.TrimSpace(c.PostForm("event_type"))
	req.Timezone = strings.TrimSpace(c.PostForm("timezone"))
	req.Currency = strings.ToUpper(strings.TrimSpace(c.PostForm("currency")))

	// Validate required fields
	if req.Title == "" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}
	if req.VenueName == "" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}
	if req.Address == "" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}
	if req.Currency == "" {
		utils.HandleError(c, utils.NewValidationError("Event currency is required", nil))
		return
	}
	if req.Country != "" && len(req.Country) < 2 {
		utils.HandleError(c, utils.NewValidationError("Country must be at least 2 characters", nil))
		return
	}
	if req.EventType != "" && len(req.EventType) < 2 {
		utils.HandleError(c, utils.NewValidationError("Event type must be at least 2 characters", nil))
		return
	}

	// Parse and validate dates
	startDateStr := c.PostForm("start_date")
	endDateStr := c.PostForm("end_date")
	if startDateStr == "" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}
	if endDateStr == "" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	req.StartDate, err = time.Parse(time.RFC3339, startDateStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	req.EndDate, err = time.Parse(time.RFC3339, endDateStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate date logic
	if req.EndDate.Before(req.StartDate) {
		utils.HandleError(c, utils.NewValidationError("Event end date must be after start date", nil))
		return
	}
	if req.StartDate.Before(time.Now().Add(-24 * time.Hour)) {
		utils.HandleError(c, utils.NewValidationError("Event start date cannot be in the past", nil))
		return
	}

	// Parse and validate numeric fields
	capacityStr := c.PostForm("capacity")
	priceStr := c.PostForm("price")
	if capacityStr == "" {
		utils.HandleError(c, utils.NewValidationError("Event capacity is required", nil))
		return
	}
	if priceStr == "" {
		utils.HandleError(c, utils.NewValidationError("Event price is required", nil))
		return
	}

	req.Capacity, err = strconv.Atoi(capacityStr)
	if err != nil || req.Capacity <= 0 {
		utils.HandleError(c, utils.NewValidationError("Event capacity must be a positive integer", nil))
		return
	}
	if req.Capacity > 1000000 {
		utils.HandleError(c, utils.NewValidationError("Event capacity cannot exceed 1,000,000 attendees", nil))
		return
	}

	req.Price, err = strconv.ParseFloat(priceStr, 64)
	if err != nil || req.Price < 0 {
		utils.HandleError(c, utils.NewValidationError("Event price must be a non-negative number", nil))
		return
	}
	if req.Price > 1000000 {
		utils.HandleError(c, utils.NewValidationError("Event price cannot exceed $1,000,000", nil))
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
	if err := tx.Preload("Roles").Preload("OrganizerOnboarding").Where("id = ? AND deleted_at IS NULL", userIDStr).First(&user).Error; err != nil {
		tx.Rollback()
		fmt.Printf("[ERROR] Failed to verify user: %v\n", err)
		if err == gorm.ErrRecordNotFound {
			utils.HandleError(c, err)
			return
		}
		if strings.Contains(err.Error(), "connection") {
			utils.HandleError(c, err)
			return
		}
		utils.HandleError(c, err)
		return
	}

	// Verify organizer onboarding is complete
	if user.OrganizerOnboarding == nil || !user.OrganizerOnboarding.IsComplete {
		tx.Rollback()
		fmt.Printf("[WARNING] Organizer onboarding not complete for user: %s\n", userIDStr)
		utils.HandleError(c, utils.NewBusinessLogicError("Event creation requires completed organizer onboarding. Please complete your business profile first."))
		return
	}

	// Set default commission rate for organizers (admins cannot create events)
	req.CommissionRate = 0

	// Parse and validate single category string (store as single-element array later)
	categoryStr := strings.TrimSpace(c.PostForm("category"))
	fmt.Printf("[DEBUG] Category string from form: '%s'\n", categoryStr)

	if categoryStr == "" {
		tx.Rollback()
		utils.HandleError(c, utils.NewValidationError("Event category is required", nil))
		return
	}

	req.Category = categoryStr
	fmt.Printf("[DEBUG] Commission rate set to: %.2f\n", req.CommissionRate)

	// Parse tiers from JSON string
	if tiersStr := c.PostForm("tiers"); tiersStr != "" {
		fmt.Printf("[DEBUG] Parsing tiers JSON: %s\n", tiersStr)
		if err := json.Unmarshal([]byte(tiersStr), &req.Tiers); err != nil {
			tx.Rollback()
			fmt.Printf("[ERROR] Failed to parse tiers JSON: %v\n", err)
			utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Invalid tiers format: %s", err.Error()), nil))
			return
		}

		// Validate tiers array (allow empty array but validate non-empty items)
		if len(req.Tiers) > 0 {
			// Check maximum number of tiers per event
			if len(req.Tiers) > 50 {
				tx.Rollback()
				utils.HandleError(c, utils.NewValidationError("Cannot create more than 50 tiers per event", nil))
				return
			}

			// Validate each tier
			for i, tier := range req.Tiers {
				fmt.Printf("[DEBUG] Validating tier %d: TierTemplateID=%s, Price=%.2f, Quantity=%d\n", i+1, tier.TierTemplateID, tier.Price, tier.Quantity)

				if tier.TierTemplateID == uuid.Nil {
					tx.Rollback()
					utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Tier %d: tier_template_id is required and must be a valid UUID", i+1), nil))
					return
				}

				if tier.Price < 0 {
					tx.Rollback()
					utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Tier %d: price must be non-negative", i+1), nil))
					return
				}

				if tier.Price > 1000000 {
					tx.Rollback()
					utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Tier %d: price cannot exceed $1,000,000", i+1), nil))
					return
				}

				if tier.Quantity <= 0 {
					tx.Rollback()
					utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Tier %d: quantity must be positive", i+1), nil))
					return
				}

				if tier.Quantity > 1000000 {
					tx.Rollback()
					utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Tier %d: quantity cannot exceed 1,000,000", i+1), nil))
					return
				}

				// Validate date logic if provided
				if tier.SalesStart != nil && tier.SalesEnd != nil {
					if tier.SalesEnd.Before(*tier.SalesStart) {
						tx.Rollback()
						utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Tier %d: sales_end must be after sales_start", i+1), nil))
						return
					}
				}

				// Validate GST percentage
				if tier.GST < 0 || tier.GST > 100 {
					tx.Rollback()
					utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Tier %d: GST must be between 0 and 100", i+1), nil))
					return
				}
			}

			fmt.Printf("[DEBUG] Successfully parsed and validated %d tiers\n", len(req.Tiers))
		} else {
			fmt.Printf("[DEBUG] Empty tiers array provided, proceeding without tiers\n")
		}
	} else {
		fmt.Printf("[DEBUG] No tiers provided, proceeding without tiers\n")
	}

	// Handle banner image upload (required)
	fmt.Printf("[DEBUG] Checking for banner_image in form data...\n")
	bannerFile, header, err := c.Request.FormFile("banner_image")
	if err != nil {
		if err == http.ErrMissingFile {
			tx.Rollback()
			utils.HandleError(c, utils.NewValidationError("Banner image is required", nil))
			return
		}
		tx.Rollback()
		utils.HandleError(c, utils.NewValidationError("Invalid banner image file", nil))
		return
	}
	defer bannerFile.Close()
	fmt.Printf("[DEBUG] Banner image found: %s, size: %d bytes\n", header.Filename, header.Size)

	// Validate file before upload (same as organizer profile validation)
	if header.Size == 0 {
		tx.Rollback()
		utils.HandleError(c, utils.NewValidationError("Banner image file cannot be empty", nil))
		return
	}

	// Check file size (10MB limit for banners)
	if header.Size > 10*1024*1024 {
		tx.Rollback()
		utils.HandleError(c, utils.NewValidationError("Banner image file size cannot exceed 10MB", nil))
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
		utils.HandleError(c, utils.NewValidationError("Banner image must be a valid image file (JPEG, PNG, or WebP)", nil))
		return
	}

	// Upload banner image first (without event ID)
	fmt.Printf("[DEBUG] Starting banner image upload for user: %s\n", userIDStr)
	bannerURL, err := h.fileStorageService.UploadFile(bannerFile, header, models.FileCategoryEventBanner, organizerID, &services.FileUploadOptions{
		AltText:     req.Title,
		Description: fmt.Sprintf("Banner image for event: %s", req.Title),
	})
	if err != nil {
		tx.Rollback()

		// Detailed error handling like in organizer profile
		if strings.Contains(err.Error(), "NoCredentialsProvided") {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		if strings.Contains(err.Error(), "NoSuchBucket") {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		if strings.Contains(err.Error(), "AccessDenied") {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		if strings.Contains(err.Error(), "InvalidAccessKeyId") {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		if strings.Contains(err.Error(), "image dimensions") {
			utils.HandleError(c, err)
			return
		}
		if strings.Contains(err.Error(), "file type") {
			utils.HandleError(c, err)
			return
		}
		if strings.Contains(err.Error(), "file size") {
			utils.HandleError(c, err)
			return
		}

		// Generic error
		utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Failed to upload banner image: %s", err.Error()), nil))
		return
	}
	fmt.Printf("[DEBUG] Banner image uploaded successfully: %s\n", bannerURL)
	req.BannerImage = bannerURL

	fmt.Printf("[DEBUG] Creating event with title: %s\n", req.Title)
	event, err := h.service.CreateEventWithTx(&req, organizerIDStr, tx)
	if err != nil {
		tx.Rollback()
		fmt.Printf("[ERROR] Event creation failed: %v\n", err)

		// Handle specific database errors
		if strings.Contains(err.Error(), "duplicate key") {
			utils.HandleError(c, err)
			return
		}
		if strings.Contains(err.Error(), "violates foreign key constraint") {
			utils.HandleError(c, err)
			return
		}
		if strings.Contains(err.Error(), "value too long") {
			utils.HandleError(c, err)
			return
		}
		if strings.Contains(err.Error(), "connection") {
			utils.HandleError(c, err)
			return
		}
		if strings.Contains(err.Error(), "check constraint") {
			utils.HandleError(c, err)
			return
		}

		// Generic database error
		utils.HandleError(c, err)
		return
	}
	fmt.Printf("[DEBUG] Event created successfully with ID: %s\n", event.ID)

	// Create tiers if provided
	if len(req.Tiers) > 0 {
		fmt.Printf("[DEBUG] Creating %d event tiers\n", len(req.Tiers))
		for i, tierReq := range req.Tiers {
			fmt.Printf("[DEBUG] Creating tier %d/%d\n", i+1, len(req.Tiers))
			tier, err := h.eventMgmtService.CreateEventTierWithTx(event.ID, organizerID, &tierReq, tx)
			if err != nil {
				tx.Rollback()
				fmt.Printf("[ERROR] Tier creation failed for tier %d: %v\n", i+1, err)

				// Handle specific tier creation errors
				if strings.Contains(err.Error(), "duplicate key") {
					utils.HandleError(c, utils.NewDatabaseError(fmt.Sprintf("Tier with similar properties already exists for this event (tier %d)", i+1), err))
					return
				}
				if strings.Contains(err.Error(), "violates foreign key constraint") {
					utils.HandleError(c, utils.NewDatabaseError(fmt.Sprintf("Invalid tier data for tier %d", i+1), err))
					return
				}
				if strings.Contains(err.Error(), "check constraint") {
					utils.HandleError(c, utils.NewDatabaseError(fmt.Sprintf("Tier %d data violates business rules (e.g., invalid price or quantity)", i+1), err))
					return
				}
				if strings.Contains(err.Error(), "value too long") {
					utils.HandleError(c, utils.NewDatabaseError(fmt.Sprintf("One or more fields in tier %d exceed maximum length", i+1), err))
					return
				}

				// Generic tier creation error
				utils.HandleError(c, utils.NewDatabaseError(fmt.Sprintf("Failed to create event tier %d", i+1), err))
				return
			}
			fmt.Printf("[DEBUG] Tier %d created successfully with ID: %s\n", i+1, tier.ID)
		}
		fmt.Printf("[DEBUG] All %d tiers created successfully\n", len(req.Tiers))

		// Note: Event capacity remains as set by organizer (venue capacity)
		// Available tickets will be calculated dynamically based on tier sales
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
			utils.HandleError(c, err)
			return
		}
		if strings.Contains(err.Error(), "deadlock") {
			utils.HandleError(c, err)
			return
		}
		if strings.Contains(err.Error(), "constraint") {
			utils.HandleError(c, err)
			return
		}

		// Generic commit error
		utils.HandleError(c, err)
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
// @Summary Get all public events (Public)
// @Description Get a list of all public events (scheduled, on_sale, sales_upcoming, hold) with pagination, search, and filtering. Only shows events with 'approved' status. Results are sorted with featured events first in alphabetical ascending order, then non-featured events in alphabetical ascending order.
// @Tags Public
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param search query string false "Search by event title or description"
// @Param location query string false "Filter by location"
// @Param status query string false "Filter by status (scheduled, on_sale, sales_upcoming, hold)" Enums(scheduled, on_sale, sales_upcoming, hold)
// @Param start_date query string false "Filter by start date (YYYY-MM-DD)"
// @Param end_date query string false "Filter by end date (YYYY-MM-DD)"
// @Param min_price query number false "Filter by minimum price"
// @Param max_price query number false "Filter by maximum price"
// @Param sort query string false "Sort parameter (currently fixed to show featured events first in alphabetical order, then non-featured events in alphabetical order)" default("featured_first_alpha")
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/events [get]
func (h *EventHandler) PublicGetAllEvents(c *gin.Context) {
	// Pagination params
	pagination := utils.GetPaginationParams(c, 10)

	// Filter params
	search := c.Query("search")
	location := c.Query("location")
	statusFilter := c.Query("status") // Allow status filtering for public events
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
		"title": true, "start_date": true, "price": true, "created_at": true, "is_featured": true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, validSortFields, "created_at", "desc")

	events, total, err := h.service.GetPublicEvents(pagination.Page, pagination.Limit, search, location, statusFilter, startDate, endDate, minPrice, maxPrice, sortBy, sortOrder)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Convert events to public summary response format
	var publicEvents []models.EventPublicSummaryResponse
	for _, event := range events {
		publicEvents = append(publicEvents, event.ToPublicSummaryResponse())
	}

	response := map[string]interface{}{
		"events":     publicEvents,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}
	utils.SuccessResponse(c, http.StatusOK, "Events fetched successfully", response)
}

// AdminGetAllEvents godoc
// @Summary Get all events (Admin)
// @Description Get a list of all events with pagination, search, and filtering (Admin only). Response includes essential fields only (title, venue, dates, status, event_type, country, currency). Fields excluded: description, timezone, location, organizer_id. Ticket sales data (available, total_sold_tickets, total_revenue) is calculated in real-time from all event tiers for all event statuses (draft, pending, approved, completed, cancelled, etc.).
// @Tags Admin
// @Security ApiKeyAuth
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param search query string false "Search by event title or description"
// @Param location query string false "Filter by location"
// @Param status query string false "Filter by status (draft, pending, approved, on_sale, live, completed, scheduled, hold, held, rejected, cancelled, sales_end, sales_upcoming)"
// @Param organizer_id query string false "Filter by organizer ID"
// @Param start_date query string false "Filter by start date (YYYY-MM-DD)"
// @Param end_date query string false "Filter by end date (YYYY-MM-DD)"
// @Param min_price query number false "Filter by minimum price"
// @Param max_price query number false "Filter by maximum price"
// @Param sort query string false "Sort by field with optional '-' prefix for desc (e.g., '-created_at', 'title', '-created_at')" default("-created_at")
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events [get]
func (h *EventHandler) AdminGetAllEvents(c *gin.Context) {
	// Pagination params
	pagination := utils.GetPaginationParams(c, 10)

	// Validation

	// Filter params
	search := c.Query("search")
	location := c.Query("location")
	statusFilter := c.DefaultQuery("status", "") // Empty means all statuses for admin
	organizerID := c.Query("organizer_id")
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

	// Parse sort parameter and validate using centralized utility
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, utils.EventsSortConfig.ValidFields, utils.EventsSortConfig.DefaultField, utils.EventsSortConfig.DefaultOrder)

	events, total, err := h.service.GetFilteredEvents(statusFilter, pagination.Page, pagination.Limit, search, location, startDate, endDate, minPrice, maxPrice, sortBy, sortOrder, organizerID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Transform events to admin list response (removes sensitive fields)
	adminEvents := make([]models.EventAdminListResponse, len(events))
	for i, event := range events {
		adminEvents[i] = event.ToAdminListResponse()
	}

	response := map[string]interface{}{
		"events":     adminEvents,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}
	utils.SuccessResponse(c, http.StatusOK, "Events fetched successfully", response)
}

// PublicGetEventByID godoc
// @Summary Get event by ID (Public)
// @Description Get details of a specific event by ID. Scheduled events are viewable but NOT purchasable. Users can view event details, tiers, and dates while waiting for ticket sales to open.
// @Tags Public
// @Produce json
// @Param id path string true "Event ID (UUID)"
// @Success 200 {object} utils.Response{data=models.EventPublicResponse}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Router /api/v1/public/events/{id} [get]
func (h *EventHandler) PublicGetEventByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	event, err := h.service.GetPublicEventByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	publicEvent := event.ToPublicResponse()
	utils.SuccessResponse(c, http.StatusOK, "Event fetched successfully", publicEvent)
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
func (h *EventHandler) OrganizerUpdateEvent(c *gin.Context) {
	h.updateEvent(c, false)
}

// updateEvent is a private method to handle event update logic
func (h *EventHandler) updateEvent(c *gin.Context, isAdmin bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	var req models.EventUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get user from context (set by auth middleware)
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

	// Get the event to check ownership
	event, err := h.service.GetEventByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// If not admin, check if user belongs to the same organization as the event organizer
	if !isAdmin {
		if event.OrganizerID.String() != organizerID.String() {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}

		// Validate that the event can still be updated (organizers only)
		// Prevent updates if the event has already started
		if time.Now().After(event.StartDate) {
			utils.HandleError(c, utils.NewBusinessLogicError("Cannot update event that has already started"))
			return
		}

		// Prevent updates if the event has sold tickets
		if event.Available < event.Capacity {
			utils.HandleError(c, utils.NewBusinessLogicError("Cannot update event that has sold tickets"))
			return
		}
	}

	event, err = h.service.UpdateEvent(id, &req)
	if err != nil {
		utils.HandleError(c, err)
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
func (h *EventHandler) OrganizerDeleteEvent(c *gin.Context) {
	h.deleteEvent(c, false)
}

// deleteEvent is a private method to handle event deletion logic
func (h *EventHandler) deleteEvent(c *gin.Context, isAdmin bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get user from context (set by auth middleware)
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

	// Get the event to check ownership
	event, err := h.service.GetEventByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// If not admin, check if user belongs to the same organization as the event organizer
	if !isAdmin {
		if event.OrganizerID.String() != organizerID.String() {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
	}

	// Check if event can be deleted (admin can delete any event except those with sold tickets, organizer only draft/pending/cancelled/rejected)
	if !isAdmin && event.Status != "draft" && event.Status != "pending" && event.Status != "cancelled" && event.Status != "rejected" {
		utils.HandleError(c, utils.NewBusinessLogicError(fmt.Sprintf("Cannot delete event with status '%s'. Only draft, pending, cancelled, and rejected events can be deleted.", event.Status)))
		return
	}

	// Delete associated files (hard delete)
	if err := h.fileStorageService.DeleteFilesByEntity("event", id); err != nil {
		// Log error but continue with event deletion
		fmt.Printf("Warning: failed to delete associated files: %v\n", err)
	}

	if err := h.service.DeleteEvent(id); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event deleted successfully", nil)
}

// AdminUpdateEventStatus godoc
// @Summary Update event status and commission (Admin)
// @Description Allow admin/subadmin to update event status, commission rate, and admin remarks. Available statuses: pending, approved, rejected, on_sale, live, hold, scheduled, cancelled, draft
// @Tags Admin
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param id path string true "Event ID (UUID)"
// @Param status body models.EventStatusUpdateRequest true "Event status update details with status, commission_rate (optional), and admin_remark"
// @Success 200 {object} utils.Response{data=models.Event}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events/{id}/status [put]
func (h *EventHandler) AdminUpdateEventStatus(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	var req models.EventStatusUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Parse commission rate from interface{} (can be string or number) to float64 if provided
	var commissionRate *float64
	if req.CommissionRate != nil {
		var rate float64
		var err error

		switch v := req.CommissionRate.(type) {
		case string:
			if v == "" {
				// Empty string, skip
			} else {
				rate, err = strconv.ParseFloat(v, 64)
			}
		case float64:
			rate = v
		case int:
			rate = float64(v)
		case int64:
			rate = float64(v)
		default:
			utils.HandleError(c, utils.NewValidationError("Invalid commission_rate format. Must be a valid number.", map[string]interface{}{"commission_rate": "invalid_format"}))
			return
		}

		if err != nil {
			utils.HandleError(c, utils.NewValidationError("Invalid commission_rate format. Must be a valid number.", map[string]interface{}{"commission_rate": "invalid_format"}))
			return
		}

		if rate < 0 || rate > 100 {
			utils.HandleError(c, utils.NewValidationError("commission_rate must be between 0 and 100", map[string]interface{}{"commission_rate": "out_of_range"}))
			return
		}
		commissionRate = &rate
	}

	// Get user from context (set by auth middleware)
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
	userIDStr := userID.String()

	updatedEvent, err := h.service.UpdateEventStatus(id, userIDStr, req.Status, commissionRate, req.AdminRemark)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event status updated successfully", updatedEvent)
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
	pagination := utils.GetPaginationParams(c, 10)

	sortParam := c.DefaultQuery("sort", "-created_at")

	events, total, err := h.service.GetEventsByStatus("pending", pagination.Page, pagination.Limit, sortParam)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"events":     events,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}
	utils.SuccessResponse(c, http.StatusOK, "Pending events fetched successfully", response)
}

// AdminGetEventByID godoc
// @Summary Get event by ID (Admin)
// @Description Get detailed event information by ID for admin
// @Tags Admin
// @Security ApiKeyAuth
// @Produce json
// @Param id path string true "Event ID (UUID)"
// @Success 200 {object} utils.Response{data=models.EventDetailResponse}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events/{id} [get]
func (h *EventHandler) AdminGetEventByID(c *gin.Context) {
	// Parse event ID
	eventIDStr := c.Param("id")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Fetch event with relations
	var event models.Event
	query := database.DB.Where("id = ? AND deleted_at IS NULL", eventID).
		Preload("Tiers")

	if err := query.First(&event).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.HandleError(c, utils.NewNotFoundError("event"))
			return
		}
		fmt.Printf("[ERROR] Failed to fetch event: %v\n", err)
		utils.HandleError(c, err)
		return
	}

	// Calculate total capacity and available from tiers
	totalCapacity := 0
	totalAvailable := 0
	for _, tier := range event.Tiers {
		totalCapacity += tier.Quantity
		totalAvailable += tier.Available
	}

	// Convert to detailed response
	response := models.EventDetailResponse{
		ID:             event.ID,
		Title:          event.Title,
		Description:    event.Description,
		BannerImage:    event.BannerImage,
		Category:       event.Category,
		EventType:      event.EventType,
		VenueName:      event.VenueName,
		Address:        event.Address,
		Location:       event.Location,
		Country:        event.Country,
		StartDate:      event.StartDate,
		EndDate:        event.EndDate,
		Timezone:       event.Timezone,
		Capacity:       event.Capacity, // Use actual venue capacity, not sum of tier quantities
		Available:      totalAvailable,
		Price:          event.Price,
		Currency:       event.Currency,
		CommissionRate: event.CommissionRate,
		Status:         event.Status,
		SalesStatus:    event.SalesStatus,
		IsFeatured:     event.IsFeatured,
		IsCancelled:    event.IsCancelled,
		CancelledAt:    event.CancelledAt,
		CancelReason:   event.CancelReason,
		OrganizerID:    event.OrganizerID,
		AdminRemark:    event.AdminRemark,
		CreatedAt:      event.CreatedAt,
		UpdatedAt:      event.UpdatedAt,
		Tiers:          event.Tiers,
	}

	utils.SuccessResponse(c, http.StatusOK, "Event fetched successfully", response)
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
func (h *EventHandler) OrganizerGetEvents(c *gin.Context) {
	pagination := utils.GetPaginationParams(c, 10)

	// Get user from context (set by auth middleware)
	userID, ok := utils.HandleUserIDExtraction(c)
	if !ok {
		return
	}

	// Get the organizer ID (for staff/managers, it's their organizer_id)
	organizerID, err := h.getOrganizerIDForUser(userID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	organizerIDStr := organizerID.String()

	sortParam := c.DefaultQuery("sort", "-created_at")

	events, total, err := h.service.GetEventsByOrganizer(organizerIDStr, pagination.Page, pagination.Limit, sortParam)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"events":     events,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}
	utils.SuccessResponse(c, http.StatusOK, "Organizer events fetched successfully", response)
}

// OrganizerGetAllEvents godoc
// @Summary Get all organizer events with pagination and search
// @Description Get paginated list of events for the authenticated organizer with search and filter capabilities
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param search query string false "Search by event title or description"
// @Param status query string false "Filter by status (draft, pending, approved, on_sale, live, completed, scheduled, hold, held, rejected, cancelled, sales_end, sales_upcoming)"
// @Param category query string false "Filter by category"
// @Param sort_by query string false "Sort by field (created_at, title, start_date, end_date, status)" default("created_at")
// @Param sort_dir query string false "Sort direction (asc, desc)" default("desc")
// @Success 200 {object} utils.Response{data=models.EventListResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events [get]
func (h *EventHandler) OrganizerGetAllEvents(c *gin.Context) {
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

	// Get the organizer ID (for staff/managers, it's their organizer_id)
	organizerID, err := h.getOrganizerIDForUser(userID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Pagination params
	pagination := utils.GetPaginationParams(c, 10)

	// Filter params
	search := c.Query("search")
	statusFilter := c.DefaultQuery("status", "") // Empty means all statuses for organizer
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	sortParam := c.DefaultQuery("sort", "-created_at")

	// Parse sort parameter
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, utils.EventsSortConfig.ValidFields, utils.EventsSortConfig.DefaultField, utils.EventsSortConfig.DefaultOrder)

	events, total, err := h.service.GetFilteredEvents(statusFilter, pagination.Page, pagination.Limit, search, "", startDate, endDate, nil, nil, sortBy, sortOrder, organizerID.String())
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Transform events to organizer list response
	organizerEvents := make([]models.EventMinimalResponse, len(events))
	for i, event := range events {
		organizerEvents[i] = event.ToMinimalResponse()
	}

	response := map[string]interface{}{
		"events":     organizerEvents,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}
	utils.SuccessResponse(c, http.StatusOK, "Events fetched successfully", response)
}

// OrganizerGetEventByID godoc
// @Summary Get event by ID with full details
// @Description Get detailed event information by ID for the authenticated organizer
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param id path string true "Event ID (UUID)"
// @Success 200 {object} utils.Response{data=models.EventDetailResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/{id} [get]
func (h *EventHandler) OrganizerGetEventByID(c *gin.Context) {
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

	// Parse event ID
	eventIDStr := c.Param("id")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Fetch event with relations
	var event models.Event
	query := database.DB.Where("id = ? AND organizer_id = ? AND deleted_at IS NULL", eventID, organizerID).
		Preload("Tiers")

	if err := query.First(&event).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		fmt.Printf("[ERROR] Failed to fetch event: %v\n", err)
		utils.HandleError(c, err)
		return
	}

	// Convert to detailed response
	response := models.EventDetailResponse{
		ID:             event.ID,
		Title:          event.Title,
		Description:    event.Description,
		BannerImage:    event.BannerImage,
		Category:       event.Category,
		EventType:      event.EventType,
		VenueName:      event.VenueName,
		Address:        event.Address,
		Location:       event.Location,
		Country:        event.Country,
		StartDate:      event.StartDate,
		EndDate:        event.EndDate,
		Timezone:       event.Timezone,
		Capacity:       event.Capacity,
		Available:      event.Available,
		Price:          event.Price,
		Currency:       event.Currency,
		CommissionRate: event.CommissionRate,
		Status:         event.Status,
		SalesStatus:    event.SalesStatus,
		IsFeatured:     event.IsFeatured,
		IsCancelled:    event.IsCancelled,
		CancelledAt:    event.CancelledAt,
		CancelReason:   event.CancelReason,
		OrganizerID:    event.OrganizerID,
		AdminRemark:    event.AdminRemark,
		CreatedAt:      event.CreatedAt,
		UpdatedAt:      event.UpdatedAt,
		Tiers:          event.Tiers,
	}

	fmt.Printf("[DEBUG] Fetched event %s for organizer %s\n", eventID, organizerID)
	utils.SuccessResponse(c, http.StatusOK, "Event fetched successfully", response)
}

// OrganizerUpdateEventByID godoc
// @Summary Update event by ID
// @Description Update event details by ID for the authenticated organizer (multipart form data)
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept multipart/form-data
// @Produce json
// @Param id path string true "Event ID (UUID)"
// @Param title formData string false "Event title"
// @Param description formData string false "Event description"
// @Param banner_image formData file false "Event banner image"
// @Param category formData string false "Event categories (comma-separated like \"Music,Art,Sports\")"
// @Param venue_name formData string false "Venue name"
// @Param address formData string false "Event address"
// @Param location formData string false "Event location"
// @Param start_date formData string false "Start date (RFC3339 format)"
// @Param end_date formData string false "End date (RFC3339 format)"
// @Param timezone formData string false "Timezone"
// @Param capacity formData int false "Event capacity"
// @Param price formData number false "Ticket price"
// @Param tiers formData string false "Event tiers as JSON array: [{\"tier_template_id\":\"uuid\",\"price\":600,\"quantity\":500,\"sales_start\":\"2025-12-01T15:04:05Z\",\"sales_end\":\"2025-12-01T15:04:05Z\"}]"
// @Success 200 {object} utils.Response{data=models.EventDetailResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/{id} [put]
func (h *EventHandler) OrganizerUpdateEventByID(c *gin.Context) {
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

	// Parse event ID
	eventIDStr := c.Param("id")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Parse multipart form with size limit
	fmt.Printf("[DEBUG] Parsing multipart form for event update: %s\n", eventIDStr)
	_, err = c.MultipartForm()
	if err != nil {
		fmt.Printf("[ERROR] Failed to parse multipart form: %v\n", err)
		if strings.Contains(err.Error(), "request body too large") {
			utils.HandleError(c, err)
			return
		}
		utils.HandleError(c, err)
		return
	}

	// Start transaction
	tx := database.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()

	// Check if event exists and belongs to organizer
	var existingEvent models.Event
	if err := tx.Where("id = ? AND organizer_id = ? AND deleted_at IS NULL", eventID, organizerID).First(&existingEvent).Error; err != nil {
		tx.Rollback()
		if err == gorm.ErrRecordNotFound {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		fmt.Printf("[ERROR] Failed to fetch event for update: %v\n", err)
		utils.HandleError(c, err)
		return
	}

	// Validate that the event can still be updated
	// Prevent updates if the event has already started
	if time.Now().After(existingEvent.StartDate) {
		tx.Rollback()
		utils.HandleError(c, utils.NewBusinessLogicError("Cannot update event that has already started"))
		return
	}

	// Prevent updates if the event has sold tickets
	if existingEvent.Available < existingEvent.Capacity {
		tx.Rollback()
		utils.HandleError(c, utils.NewBusinessLogicError("Cannot update event that has sold tickets"))
		return
	}

	// Check if event can be updated (only draft and pending events can be fully updated)
	// After update, event goes back to pending status for admin approval
	if existingEvent.Status != "draft" && existingEvent.Status != "pending" && existingEvent.Status != "rejected" {
		tx.Rollback()
		utils.HandleError(c, utils.NewBusinessLogicError(fmt.Sprintf("Event cannot be updated. Current status: %s. Only draft, pending, and rejected events can be updated", existingEvent.Status)))
		return
	}

	// Build update data from form
	updateData := make(map[string]interface{})

	// If event was approved/rejected, it goes back to pending for re-approval
	if existingEvent.Status == "approved" || existingEvent.Status == "rejected" {
		updateData["status"] = "pending"
		fmt.Printf("[DEBUG] Event %s status changed back to pending for re-approval\n", eventID)
	}

	if title := strings.TrimSpace(c.PostForm("title")); title != "" {
		if len(title) < 3 || len(title) > 200 {
			tx.Rollback()
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		updateData["title"] = title
	}

	if description := strings.TrimSpace(c.PostForm("description")); description != "" {
		if len(description) > 10000 {
			tx.Rollback()
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		updateData["description"] = description
	}

	if venueName := strings.TrimSpace(c.PostForm("venue_name")); venueName != "" {
		if len(venueName) < 3 || len(venueName) > 200 {
			tx.Rollback()
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		updateData["venue_name"] = venueName
	}

	if address := strings.TrimSpace(c.PostForm("address")); address != "" {
		if len(address) < 10 || len(address) > 500 {
			tx.Rollback()
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		updateData["address"] = address
	}

	if location := strings.TrimSpace(c.PostForm("location")); location != "" {
		if len(location) < 3 || len(location) > 200 {
			tx.Rollback()
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		updateData["location"] = location
	}

	if country := strings.TrimSpace(c.PostForm("country")); country != "" {
		if len(country) < 2 || len(country) > 100 {
			tx.Rollback()
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		updateData["country"] = country
	}

	if eventType := strings.TrimSpace(c.PostForm("event_type")); eventType != "" {
		if len(eventType) < 2 || len(eventType) > 50 {
			tx.Rollback()
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		updateData["event_type"] = eventType
	}

	if timezone := strings.TrimSpace(c.PostForm("timezone")); timezone != "" {
		updateData["timezone"] = timezone
	}

	// Handle category parsing for multipart/form-data
	fmt.Printf("[DEBUG] Checking for category field\n")
	categories := strings.TrimSpace(c.PostForm("category"))
	fmt.Printf("[DEBUG] Raw category string received: '%s'\n", categories)

	if categories != "" {
		// Single category string expected
		fmt.Printf("[DEBUG] Processing category string: '%s'\n", categories)
		updateData["category"] = categories
		fmt.Printf("[DEBUG] Setting updateData category to: %s\n", categories)
	} else {
		fmt.Printf("[DEBUG] No category field found in form data\n")
	}

	// Parse dates
	if startDateStr := c.PostForm("start_date"); startDateStr != "" {
		startDate, err := time.Parse(time.RFC3339, startDateStr)
		if err != nil {
			tx.Rollback()
			utils.HandleError(c, err)
			return
		}
		updateData["start_date"] = startDate
	}

	if endDateStr := c.PostForm("end_date"); endDateStr != "" {
		endDate, err := time.Parse(time.RFC3339, endDateStr)
		if err != nil {
			tx.Rollback()
			utils.HandleError(c, err)
			return
		}
		updateData["end_date"] = endDate
	}

	// Parse numeric fields
	if capacityStr := c.PostForm("capacity"); capacityStr != "" {
		capacity, err := strconv.Atoi(capacityStr)
		if err != nil || capacity <= 0 {
			tx.Rollback()
			utils.HandleError(c, err)
			return
		}
		if capacity > 100000 {
			tx.Rollback()
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		updateData["capacity"] = capacity
		// Update available count if capacity changed and no tickets sold yet
		if existingEvent.Available == existingEvent.Capacity {
			updateData["available"] = capacity
		}
	}

	if priceStr := c.PostForm("price"); priceStr != "" {
		price, err := strconv.ParseFloat(priceStr, 64)
		if err != nil || price < 0 {
			tx.Rollback()
			utils.HandleError(c, err)
			return
		}
		if price > 1000000 {
			tx.Rollback()
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		updateData["price"] = price
	}

	if currency := strings.TrimSpace(c.PostForm("currency")); currency != "" {
		updateData["currency"] = strings.ToUpper(currency)
	}

	// Handle banner image upload
	if bannerFile, header, err := c.Request.FormFile("banner_image"); err == nil {
		defer bannerFile.Close()
		fmt.Printf("[DEBUG] Banner image found for update: %s, size: %d bytes\n", header.Filename, header.Size)

		// Validate file
		if header.Size == 0 {
			tx.Rollback()
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		if header.Size > 10*1024*1024 {
			tx.Rollback()
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
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
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}

		// Upload new banner image
		bannerURL, err := h.fileStorageService.UploadFile(bannerFile, header, models.FileCategoryEventBanner, organizerID, &services.FileUploadOptions{
			AltText:     existingEvent.Title,
			Description: fmt.Sprintf("Banner image for event: %s", existingEvent.Title),
		})
		if err != nil {
			tx.Rollback()
			utils.HandleError(c, err)
			return
		}

		fmt.Printf("[DEBUG] New banner image uploaded successfully: %s\n", bannerURL)

		// Delete old banner image if exists (only after successful upload)
		if existingEvent.BannerImage != "" && existingEvent.BannerImage != bannerURL {
			fmt.Printf("[DEBUG] Deleting old banner image: %s\n", existingEvent.BannerImage)
			if err := h.fileStorageService.DeleteFileByURL(existingEvent.BannerImage); err != nil {
				fmt.Printf("[WARNING] Failed to delete old banner image %s: %v\n", existingEvent.BannerImage, err)
				// Continue with update - don't fail for cleanup issues
			} else {
				fmt.Printf("[DEBUG] Old banner image deleted successfully: %s\n", existingEvent.BannerImage)
			}
		}

		// Update database with new banner URL
		updateData["banner_image"] = bannerURL
		fmt.Printf("[DEBUG] Banner image updated in database for event %s: %s\n", eventID, bannerURL)
	} else if err != http.ErrMissingFile {
		tx.Rollback()
		utils.HandleError(c, err)
		return
	}

	// Handle tiers update
	var tiersToUpdate []models.CreateEventTierRequest
	if tiersStr := c.PostForm("tiers"); tiersStr != "" {
		fmt.Printf("[DEBUG] Parsing tiers JSON for update: %s\n", tiersStr)
		if err := json.Unmarshal([]byte(tiersStr), &tiersToUpdate); err != nil {
			tx.Rollback()
			fmt.Printf("[ERROR] Failed to parse tiers JSON: %v\n", err)
			utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Invalid tiers format: %s", err.Error()), nil))
			return
		}

		// Validate tiers array (allow empty array but validate non-empty items)
		if len(tiersToUpdate) > 0 {
			// Validate each tier
			for i, tier := range tiersToUpdate {
				fmt.Printf("[DEBUG] Validating tier %d: TierTemplateID=%s, Price=%.2f, Quantity=%d\n", i+1, tier.TierTemplateID, tier.Price, tier.Quantity)

				if tier.TierTemplateID == uuid.Nil {
					tx.Rollback()
					utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Tier %d: tier_template_id is required and must be a valid UUID", i+1), nil))
					return
				}

				if tier.Price < 0 {
					tx.Rollback()
					utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Tier %d: price must be non-negative", i+1), nil))
					return
				}

				if tier.Quantity <= 0 {
					tx.Rollback()
					utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Tier %d: quantity must be positive", i+1), nil))
					return
				}

				// Validate date logic if provided
				if tier.SalesStart != nil && tier.SalesEnd != nil {
					if tier.SalesEnd.Before(*tier.SalesStart) {
						tx.Rollback()
						utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Tier %d: sales_end must be after sales_start", i+1), nil))
						return
					}
				}

				// Validate GST percentage
				if tier.GST < 0 || tier.GST > 100 {
					tx.Rollback()
					utils.HandleError(c, utils.NewValidationError(fmt.Sprintf("Tier %d: GST must be between 0 and 100", i+1), nil))
					return
				}
			}

			fmt.Printf("[DEBUG] Successfully parsed and validated %d tiers for update\n", len(tiersToUpdate))
		} else {
			fmt.Printf("[DEBUG] Empty tiers array provided for update, will clear existing tiers\n")
		}
	} else {
		fmt.Printf("[DEBUG] No tiers provided for update, keeping existing tiers\n")
	}

	// If no fields to update and no tiers to update
	if len(updateData) == 0 && len(tiersToUpdate) == 0 {
		tx.Rollback()
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Update the event
	fmt.Printf("[DEBUG] Updating event %s with data: %+v\n", eventID, updateData)
	if err := tx.Model(&existingEvent).Updates(updateData).Error; err != nil {
		tx.Rollback()
		fmt.Printf("[ERROR] Failed to update event: %v\n", err)
		if strings.Contains(err.Error(), "duplicate key") {
			utils.HandleError(c, err)
			return
		}
		utils.HandleError(c, err)
		return
	}

	// Update tiers if provided
	if len(tiersToUpdate) > 0 {
		fmt.Printf("[DEBUG] Updating tiers for event %s\n", eventID)

		// Delete existing tiers
		if err := tx.Where("event_id = ?", eventID).Delete(&models.EventTier{}).Error; err != nil {
			tx.Rollback()
			fmt.Printf("[ERROR] Failed to delete existing tiers: %v\n", err)
			utils.HandleError(c, err)
			return
		}

		// Create new tiers
		for i, tierReq := range tiersToUpdate {
			// Fetch the tier template to get the name
			var template models.OrganizerTierTemplate
			if err := tx.Where("id = ? AND organizer_id = ? AND is_active = ?", tierReq.TierTemplateID, organizerID, true).First(&template).Error; err != nil {
				tx.Rollback()
				if errors.Is(err, gorm.ErrRecordNotFound) {
					utils.HandleError(c, utils.NewNotFoundError("tier template"))
					return
				}
				fmt.Printf("[ERROR] Failed to fetch tier template %s: %v\n", tierReq.TierTemplateID, err)
				utils.HandleError(c, utils.NewDatabaseError("Failed to retrieve tier template.", err))
				return
			}

			tier := models.EventTier{
				EventID:        eventID,
				TierTemplateID: tierReq.TierTemplateID,
				TierName:       template.TemplateName,
				Price:          tierReq.Price,
				Currency:       strings.ToUpper(tierReq.Currency),
				Quantity:       tierReq.Quantity,
				Available:      tierReq.Quantity, // Initially all are available
				GST:            tierReq.GST,
				SalesStart:     tierReq.SalesStart,
				SalesEnd:       tierReq.SalesEnd,
				SortOrder:      i + 1,
			}

			// Set default currency if not provided
			if tier.Currency == "" {
				tier.Currency = "USD"
			}

			if err := tx.Create(&tier).Error; err != nil {
				tx.Rollback()
				fmt.Printf("[ERROR] Failed to create tier %d: %v\n", i+1, err)
				utils.HandleError(c, err)
				return
			}
		}

		fmt.Printf("[DEBUG] Successfully updated %d tiers for event %s\n", len(tiersToUpdate), eventID)
	}

	// Commit transaction
	fmt.Printf("[DEBUG] Committing transaction for event update: %s\n", eventID)
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		fmt.Printf("[ERROR] Failed to commit event update: %v\n", err)
		utils.HandleError(c, err)
		return
	}

	// Log status change if event was moved back to pending
	if existingEvent.Status == "rejected" || existingEvent.Status == "approved" {
		oldStatus := existingEvent.Status
		if err := h.service.LogStatusChange(eventID, oldStatus, "pending", "manual", organizerID.String(), "Event updated by organizer and resubmitted for approval"); err != nil {
			fmt.Printf("[ERROR] Failed to log status change for %s->pending: %v\n", oldStatus, err)
			// Don't fail the request for logging errors
		}
	}

	// Fetch updated event with relations
	fmt.Printf("[DEBUG] Fetching updated event: %s\n", eventID)
	var updatedEvent models.Event
	if err := database.DB.Where("id = ?", eventID).
		Preload("Tiers").
		First(&updatedEvent).Error; err != nil {
		fmt.Printf("[ERROR] Failed to fetch updated event: %v\n", err)
		utils.HandleError(c, err)
		return
	}

	// Convert to detailed response
	response := models.EventDetailResponse{
		ID:             updatedEvent.ID,
		Title:          updatedEvent.Title,
		Description:    updatedEvent.Description,
		BannerImage:    updatedEvent.BannerImage,
		Category:       updatedEvent.Category,
		EventType:      updatedEvent.EventType,
		VenueName:      updatedEvent.VenueName,
		Address:        updatedEvent.Address,
		Location:       updatedEvent.Location,
		Country:        updatedEvent.Country,
		StartDate:      updatedEvent.StartDate,
		EndDate:        updatedEvent.EndDate,
		Timezone:       updatedEvent.Timezone,
		Capacity:       updatedEvent.Capacity,
		Available:      updatedEvent.Available,
		Price:          updatedEvent.Price,
		Currency:       updatedEvent.Currency,
		CommissionRate: updatedEvent.CommissionRate,
		Status:         updatedEvent.Status,
		SalesStatus:    updatedEvent.SalesStatus,
		IsFeatured:     updatedEvent.IsFeatured,
		IsCancelled:    updatedEvent.IsCancelled,
		CancelledAt:    updatedEvent.CancelledAt,
		CancelReason:   updatedEvent.CancelReason,
		OrganizerID:    updatedEvent.OrganizerID,
		AdminRemark:    updatedEvent.AdminRemark,
		CreatedAt:      updatedEvent.CreatedAt,
		UpdatedAt:      updatedEvent.UpdatedAt,
		Tiers:          updatedEvent.Tiers,
	}

	fmt.Printf("[DEBUG] Event %s updated successfully by organizer %s\n", eventID, organizerID)
	utils.SuccessResponse(c, http.StatusOK, "Event updated successfully", response)
}

// OrganizerDeleteEventByID godoc
// @Summary Delete event by ID
// @Description Soft delete event by ID for the authenticated organizer (only draft, pending, cancelled, and rejected events can be deleted)
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param id path string true "Event ID (UUID)"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/{id} [delete]
func (h *EventHandler) OrganizerDeleteEventByID(c *gin.Context) {
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

	// Parse event ID
	eventIDStr := c.Param("id")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Start transaction
	tx := database.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()

	// Check if event exists and belongs to organizer
	var event models.Event
	if err := tx.Where("id = ? AND organizer_id = ? AND deleted_at IS NULL", eventID, organizerID).First(&event).Error; err != nil {
		tx.Rollback()
		if err == gorm.ErrRecordNotFound {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		fmt.Printf("[ERROR] Failed to fetch event for deletion: %v\n", err)
		utils.HandleError(c, err)
		return
	}

	// Check if event can be deleted (business rule: draft, pending, cancelled, or rejected events)
	if event.Status != "draft" && event.Status != "pending" && event.Status != "cancelled" && event.Status != "rejected" {
		utils.HandleError(c, utils.NewBusinessLogicError(fmt.Sprintf("Cannot delete event with status '%s'. Only draft, pending, cancelled, and rejected events can be deleted.", event.Status)))
		return
	}

	// Check if there are any sold tickets
	var ticketCount int64
	if err := tx.Model(&models.Ticket{}).Where("event_id = ?", eventID).Count(&ticketCount).Error; err != nil {
		tx.Rollback()
		fmt.Printf("[ERROR] Failed to count tickets for event: %v\n", err)
		utils.HandleError(c, err)
		return
	}

	if ticketCount > 0 {
		tx.Rollback()
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Delete associated files (banner image)
	if event.BannerImage != "" {
		if err := h.fileStorageService.DeleteFileByURL(event.BannerImage); err != nil {
			fmt.Printf("[WARNING] Failed to delete banner image: %v\n", err)
		}
	}

	// Soft delete the event (GORM will handle cascading deletes for related data)
	if err := tx.Delete(&event).Error; err != nil {
		tx.Rollback()
		fmt.Printf("[ERROR] Failed to delete event: %v\n", err)
		if strings.Contains(err.Error(), "violates foreign key constraint") {
			utils.HandleError(c, err)
			return
		}
		utils.HandleError(c, err)
		return
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		fmt.Printf("[ERROR] Failed to commit event deletion: %v\n", err)
		utils.HandleError(c, err)
		return
	}

	fmt.Printf("[DEBUG] Event %s deleted successfully by organizer %s\n", eventID, organizerID)
	utils.SuccessResponse(c, http.StatusOK, "Event deleted successfully", nil)
}

// AdminGetEventStatusHistory godoc
// @Summary Get event status change history (Admin)
// @Description Get the complete status change history for an event (Admin only)
// @Tags Admin
// @Security ApiKeyAuth
// @Produce json
// @Param id path string true "Event ID (UUID)"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events/{id}/status-history [get]
func (h *EventHandler) AdminGetEventStatusHistory(c *gin.Context) {
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	history, err := h.service.GetEventStatusHistory(eventID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"history": history,
		"total":   len(history),
	}

	utils.SuccessResponse(c, http.StatusOK, "Status history retrieved successfully", response)
}

// OrganizerGetEventStatusHistory godoc
// @Summary Get event status change history (Organizer)
// @Description Get the status change history for an event owned by the organizer
// @Tags Organizer
// @Security ApiKeyAuth
// @Produce json
// @Param id path string true "Event ID (UUID)"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/{id}/status-history [get]
func (h *EventHandler) OrganizerGetEventStatusHistory(c *gin.Context) {
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

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

	// Verify the organizer owns this event
	event, err := h.service.GetEventByID(eventID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	if event.OrganizerID != organizerID {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	history, err := h.service.GetEventStatusHistory(eventID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"history": history,
		"total":   len(history),
	}

	utils.SuccessResponse(c, http.StatusOK, "Status history retrieved successfully", response)
}

// ControlEventSales godoc
// @Summary Control event sales (Organizer)
// @Description Pause, resume, or stop event sales. Pause/resume actions also update event status: pause sets status to 'hold', resume sets status to 'on_sale'
// @Tags Organizer
// @Accept json
// @Produce json
// @Param id path string true "Event ID"
// @Param request body models.EventSalesControlRequest true "Sales control request"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.Event}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/{id}/sales/control [put]
func (h *EventHandler) ControlEventSales(c *gin.Context) {
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

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

	var req models.EventSalesControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	err = h.eventMgmtService.ControlEventSales(eventID, organizerID, &req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Failed to control event sales", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event sales status updated successfully", nil)
}

// RequestEventCancellation godoc
// @Summary Request event cancellation (Organizer)
// @Description Submit a cancellation request for an event. The request will be reviewed by admins who can approve or reject it. Cancellation is only allowed for events that are 'pending' or 'approved' and have sold tickets.
// @Tags Organizer
// @Accept json
// @Produce json
// @Param id path string true "Event ID"
// @Param request body models.CreateEventCancellationRequest true "Cancellation request"
// @Security ApiKeyAuth
// @Success 201 {object} utils.Response{data=models.EventCancellationRequest}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/{id}/cancellation [post]
func (h *EventHandler) RequestEventCancellation(c *gin.Context) {
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	userID, ok := utils.HandleUserIDExtraction(c)
	if !ok {
		return
	}

	organizerID, err := h.getOrganizerIDForUser(userID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	var req models.CreateEventCancellationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	cancellationRequest, err := h.eventMgmtService.CreateCancellationRequest(eventID, organizerID, &req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Cancellation request submitted successfully", cancellationRequest)
}

// AdminListEventCancellationRequests godoc
// @Summary List event cancellation requests (Admin)
// @Description List all event cancellation requests with optional filtering by status. Admins can view all requests across all organizers.
// @Tags Admin
// @Produce json
// @Param status query string false "Filter by request status (pending, approved, rejected)"
// @Param page query int false "Page number for pagination"
// @Param limit query int false "Number of items per page"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events/cancellation-requests [get]
func (h *EventHandler) AdminListEventCancellationRequests(c *gin.Context) {
	pagination := utils.GetPaginationParams(c, 10)
	status := strings.TrimSpace(c.Query("status"))

	requests, total, err := h.eventMgmtService.ListCancellationRequests(status, pagination.Page, pagination.Limit)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve cancellation requests", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Cancellation requests retrieved successfully", map[string]interface{}{
		"requests":   requests,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	})
}

// AdminApproveEventCancellationRequest godoc
// @Summary Approve event cancellation request (Admin)
// @Description Approve a pending event cancellation request. This will cancel the associated event and trigger refunds for sold tickets. Only admins can perform this action.
// @Tags Admin
// @Accept json
// @Produce json
// @Param request_id path string true "Cancellation Request ID"
// @Param request body models.ReviewEventCancellationRequest true "Review cancellation request"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.EventCancellationRequest}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events/cancellation-requests/{request_id}/approve [put]
func (h *EventHandler) AdminApproveEventCancellationRequest(c *gin.Context) {
	requestID, err := uuid.Parse(c.Param("request_id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	adminID, ok := utils.HandleUserIDExtraction(c)
	if !ok {
		return
	}
	var req models.ReviewEventCancellationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	result, err := h.eventMgmtService.ReviewCancellationRequest(requestID, adminID, true, req.AdminRemark)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.SuccessResponse(c, http.StatusOK, "Cancellation request approved", result)
}

// AdminRejectEventCancellationRequest godoc
// @Summary Reject event cancellation request (Admin)
// @Description Reject a pending event cancellation request. The associated event will remain active. Only admins can perform this action.
// @Tags Admin
// @Accept json
// @Produce json
// @Param request_id path string true "Cancellation Request ID"
// @Param request body models.ReviewEventCancellationRequest true "Review cancellation request"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.EventCancellationRequest}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events/cancellation-requests/{request_id}/reject [put]
func (h *EventHandler) AdminRejectEventCancellationRequest(c *gin.Context) {
	requestID, err := uuid.Parse(c.Param("request_id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	adminID, ok := utils.HandleUserIDExtraction(c)
	if !ok {
		return
	}
	var req models.ReviewEventCancellationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	result, err := h.eventMgmtService.ReviewCancellationRequest(requestID, adminID, false, req.AdminRemark)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.SuccessResponse(c, http.StatusOK, "Cancellation request rejected", result)
}

// GetEventAnalytics godoc
// @Summary Get event analytics (Organizer)
// @Description Get comprehensive analytics for an event including tier breakdown, revenue totals, commission earnings, and organizer share
// @Tags Organizer
// @Produce json
// @Param id path string true "Event ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.EventAnalyticsResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/{id}/analytics [get]
func (h *EventHandler) GetEventAnalytics(c *gin.Context) {
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

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

	analytics, err := h.eventMgmtService.GetEventAnalytics(eventID, organizerID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "Failed to get event analytics", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event analytics retrieved successfully", analytics)
}

// AdminGetEventAnalytics godoc
// @Summary Get event analytics (Admin)
// @Description Get comprehensive analytics for an event including tier breakdown, revenue totals, commission earnings, and organizer share (Admin access - no organizer scoping)
// @Tags Admin
// @Produce json
// @Param id path string true "Event ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.EventAnalyticsResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events/{id}/analytics [get]
func (h *EventHandler) AdminGetEventAnalytics(c *gin.Context) {
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	analytics, err := h.eventMgmtService.AdminGetEventAnalytics(eventID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "Failed to get event analytics", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event analytics retrieved successfully", analytics)
}

// GetOrganizerTierTemplates godoc
// @Summary Get organizer tier templates (Organizer)
// @Description Get all tier name templates for the authenticated organizer
// @Tags Organizer
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=[]models.OrganizerTierTemplateResponse}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/tier-templates [get]
func (h *EventHandler) GetOrganizerTierTemplates(c *gin.Context) {
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

	templates, err := h.eventMgmtService.GetOrganizerTierTemplates(organizerID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to get tier templates", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Tier templates retrieved successfully", templates)
}

// CreateOrganizerTierTemplate godoc
// @Summary Create tier template (Organizer)
// @Description Create a new tier name template for reuse across events
// @Tags Organizer
// @Accept json
// @Produce json
// @Param request body models.CreateOrganizerTierTemplateRequest true "Template creation request"
// @Security ApiKeyAuth
// @Success 201 {object} utils.Response{data=models.OrganizerTierTemplateResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 409 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/tier-templates [post]
func (h *EventHandler) CreateOrganizerTierTemplate(c *gin.Context) {
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

	var req models.CreateOrganizerTierTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	err = h.eventMgmtService.CreateOrganizerTierTemplate(organizerID, &req)
	if err != nil {
		if http.StatusText(http.StatusConflict) != "" { // Check for conflict error
			utils.ErrorResponse(c, http.StatusConflict, "Template name already exists.", err)
			return
		}
		utils.ErrorResponse(c, http.StatusBadRequest, "Failed to create tier template", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Tier template created successfully", nil)
}

// UpdateOrganizerTierTemplate godoc
// @Summary Update tier template (Organizer)
// @Description Update an existing tier name template
// @Tags Organizer
// @Accept json
// @Produce json
// @Param templateId path string true "Template ID"
// @Param request body models.UpdateOrganizerTierTemplateRequest true "Template update request"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.OrganizerTierTemplateResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 409 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/tier-templates/{templateId} [put]
func (h *EventHandler) UpdateOrganizerTierTemplate(c *gin.Context) {
	templateID, err := uuid.Parse(c.Param("templateId"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

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

	var req models.UpdateOrganizerTierTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	err = h.eventMgmtService.UpdateOrganizerTierTemplate(templateID, organizerID, &req)
	if err != nil {
		if http.StatusText(http.StatusConflict) != "" { // Check for conflict error
			utils.ErrorResponse(c, http.StatusConflict, "Template name already exists", err)
			return
		}
		utils.ErrorResponse(c, http.StatusBadRequest, "Failed to update tier template", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Tier template updated successfully", nil)
}

// DeleteOrganizerTierTemplate godoc
// @Summary Delete tier template (Organizer)
// @Description Delete a tier name template (only if not used in active events)
// @Tags Organizer
// @Produce json
// @Param templateId path string true "Template ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/tier-templates/{templateId} [delete]
func (h *EventHandler) DeleteOrganizerTierTemplate(c *gin.Context) {
	templateID, err := uuid.Parse(c.Param("templateId"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

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

	if err := h.eventMgmtService.DeleteOrganizerTierTemplate(templateID, organizerID); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Failed to delete tier template", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Tier template deleted successfully", nil)
}

// === PAYOUT REQUEST HANDLERS ===

// CreatePayoutRequest godoc
// @Summary Create payout request (Organizer)
// @Description Create a new payout request for earned revenue
// @Tags Organizer
// @Accept json
// @Produce json
// @Param request body models.PayoutRequestCreate true "Payout request"
// @Security ApiKeyAuth
// @Success 201 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/payouts [post]
func (h *EventHandler) CreatePayoutRequest(c *gin.Context) {
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

	var req models.PayoutRequestCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	err = h.payoutService.CreatePayoutRequest(organizerID, &req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Failed to create payout request", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Payout request created successfully", nil)
}

// GetOrganizerPayoutRequests godoc
// @Summary Get organizer payout requests (Organizer)
// @Description Get all payout requests for the authenticated organizer
// @Tags Organizer
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(20)
// @Param status query string false "Filter by status" Enums(pending, approved, rejected, cancelled, paid)
// @Param sort_by query string false "Sort by field (created_at, amount, event_title, event_status, status, request_type)" default(created_at)
// @Param sort_order query string false "Sort order (asc, desc)" default(desc)
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=[]models.PayoutRequestResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/payouts [get]
func (h *EventHandler) GetOrganizerPayoutRequests(c *gin.Context) {
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

	pagination := utils.GetPaginationParams(c, 10)
	status := c.Query("status")
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	// Validate sort parameters using centralized utility
	sortBy, sortOrder = utils.ValidateSortForPayoutRequests(sortBy, sortOrder)

	requests, total, err := h.payoutService.GetOrganizerPayoutRequests(organizerID, pagination.Page, pagination.Limit, status, sortBy, sortOrder)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to get payout requests", err)
		return
	}

	response := map[string]interface{}{
		"requests":   requests,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Organizer Payout requests retrieved successfully", response)
}

// GetAllPayoutRequests godoc
// @Summary Get all payout requests (Admin)
// @Description Get all payout requests from all organizers
// @Tags Admin Payouts
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(20)
// @Param status query string false "Filter by status" Enums(pending, approved, rejected, cancelled, paid)
// @Param sort_by query string false "Sort by field (created_at, amount, event_title, event_status, status, request_type)" default(created_at)
// @Param sort_order query string false "Sort order (asc, desc)" default(desc)
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payouts [get]
func (h *EventHandler) GetAllPayoutRequests(c *gin.Context) {
	pagination := utils.GetPaginationParams(c, 10)
	status := c.Query("status")
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	// Validate sort parameters using centralized utility
	sortBy, sortOrder = utils.ValidateSortForPayoutRequests(sortBy, sortOrder)

	requests, total, err := h.payoutService.GetAllPayoutRequests(pagination.Page, pagination.Limit, status, sortBy, sortOrder)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to get payout requests", err)
		return
	}

	response := map[string]interface{}{
		"requests":   requests,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Admin Payout requests retrieved successfully", response)
}

// UpdatePayoutRequestStatus godoc
// @Summary Update payout request status (Admin)
// @Description Approve, reject, or mark payout request as paid
// @Tags Admin Payouts
// @Accept json
// @Produce json
// @Param id path string true "Payout Request ID"
// @Param request body models.PayoutRequestUpdate true "Status update"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.PayoutRequestResponse} "Payout request updated successfully with bill_id if approved"
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payouts/{id}/status [put]
func (h *EventHandler) UpdatePayoutRequestStatus(c *gin.Context) {
	requestID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

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
	adminID := userID

	var req models.PayoutRequestUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	updatedRequest, err := h.payoutService.UpdatePayoutRequestStatus(requestID, adminID, &req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Failed to update payout request", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payout request updated successfully", updatedRequest)
}

// GetPayoutSummary godoc
// @Summary Get payout summary (Organizer)
// @Description Get payout summary including earnings, received amount, and pending requests. Optional event_id query parameter to filter by specific event
// @Tags Organizer
// @Produce json
// @Security ApiKeyAuth
// @Param event_id query string false "Event ID to filter summary (optional)"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/payouts/summary [get]
func (h *EventHandler) GetPayoutSummary(c *gin.Context) {
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

	// Get optional event_id query parameter
	var eventID *uuid.UUID
	if eventIDStr := c.Query("event_id"); eventIDStr != "" {
		parsedEventID, err := uuid.Parse(eventIDStr)
		if err != nil {
			utils.HandleError(c, utils.NewValidationError("Invalid event_id format.", nil))
			return
		}
		eventID = &parsedEventID
	}

	summary, err := h.payoutService.GetOrganizerPayoutSummary(organizerID, eventID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to get payout summary", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payout summary retrieved successfully", summary)
}

// GetOrganizerPayoutRequest godoc
// @Summary Get single payout request (Organizer)
// @Description Get a specific payout request by ID for the authenticated organizer
// @Tags Organizer
// @Produce json
// @Param id path string true "Payout request ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.PayoutRequestResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/payouts/{id} [get]
func (h *EventHandler) GetOrganizerPayoutRequest(c *gin.Context) {
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

	// Parse payout request ID
	requestIDStr := c.Param("id")
	requestID, err := uuid.Parse(requestIDStr)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid payout request ID format.", nil))
		return
	}

	// Get the payout request (scoped to organizer)
	request, err := h.payoutService.GetOrganizerPayoutRequestDetail(requestID, organizerID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "Payout request not found", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payout request retrieved successfully", request)
}

// GetAdminPayoutRequest godoc
// @Summary Get single payout request (Admin)
// @Description Get a specific payout request by ID for admin review
// @Tags Admin
// @Produce json
// @Param id path string true "Payout request ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.PayoutRequestResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payouts/{id} [get]
func (h *EventHandler) GetAdminPayoutRequest(c *gin.Context) {
	// Parse payout request ID
	requestIDStr := c.Param("id")
	requestID, err := uuid.Parse(requestIDStr)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid payout request ID format.", nil))
		return
	}

	// Get the payout request (admin access - includes organizer details)
	request, err := h.payoutService.GetAdminPayoutRequestDetail(requestID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "Payout request not found", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payout request retrieved successfully", request)
}
