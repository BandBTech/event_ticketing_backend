package handlers

import (
	"log"
	"net/http"
	"strconv"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type PublicHandler struct {
	db            *gorm.DB
	ticketService *services.TicketService
}

func NewPublicHandler(ticketService *services.TicketService) *PublicHandler {
	return &PublicHandler{
		db:            database.GetDB(),
		ticketService: ticketService,
	}
}

// @Summary Get company information
// @Description Get the website/company information for public display
// @Tags Public
// @Accept json
// @Produce json
// @Success 200 {object} utils.Response{data=models.CompanyInfoResponse} "Company information"
// @Failure 404 {object} utils.Response "Company information not found"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/public/company-info [get]
func (h *PublicHandler) GetCompanyInfo(c *gin.Context) {
	var companyInfo models.CompanyInfo

	if err := h.db.First(&companyInfo).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.NotFoundErrorResponse(c, "Company information not found", nil)
			return
		}
		utils.DatabaseErrorResponse(c, "Failed to get company information", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Company information retrieved successfully", companyInfo.ToResponse())
}

// @Summary Get all active categories
// @Description Get all active event categories for public display
// @Tags Public
// @Accept json
// @Produce json
// @Success 200 {object} utils.Response{data=[]models.CategoryResponse} "Categories list"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/public/categories [get]
func (h *PublicHandler) GetCategories(c *gin.Context) {
	var categories []models.Category

	if err := h.db.Where("is_active = ?", true).Order("sort_order ASC, name ASC").Find(&categories).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to get categories", err)
		return
	}

	var response []models.CategoryResponse
	for _, category := range categories {
		response = append(response, category.ToResponse())
	}

	utils.SuccessResponse(c, http.StatusOK, "Categories retrieved successfully", response)
}

// @Summary Get featured events
// @Description Get top 3 featured events for homepage display
// @Tags Public
// @Accept json
// @Produce json
// @Param limit query int false "Limit number of events (default 3, max 10)"
// @Success 200 {object} utils.Response{data=[]models.Event} "Featured events"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/public/events/featured [get]
func (h *PublicHandler) GetFeaturedEvents(c *gin.Context) {
	limit := 3
	if limitParam := c.Query("limit"); limitParam != "" {
		if parsedLimit, err := strconv.Atoi(limitParam); err == nil && parsedLimit > 0 && parsedLimit <= 10 {
			limit = parsedLimit
		}
	}

	var events []models.Event

	if err := h.db.Preload("Organizer").
		Where("is_featured = ? AND status = ? AND start_date > ?", true, "approved", utils.Now()).
		Order("created_at DESC").
		Limit(limit).
		Find(&events).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to get featured events", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Featured events retrieved successfully", events)
}

// @Summary Get upcoming events
// @Description Get upcoming events ordered by start date
// @Tags Public
// @Accept json
// @Produce json
// @Param limit query int false "Limit number of events (default 10, max 50)"
// @Param page query int false "Page number (default 1)"
// @Param category query string false "Filter by category"
// @Success 200 {object} utils.Response{data=map[string]interface{}} "Upcoming events with pagination"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/public/events/upcoming [get]
func (h *PublicHandler) GetUpcomingEvents(c *gin.Context) {
	page := 1
	limit := 10

	if pageParam := c.Query("page"); pageParam != "" {
		if parsedPage, err := strconv.Atoi(pageParam); err == nil && parsedPage > 0 {
			page = parsedPage
		}
	}

	if limitParam := c.Query("limit"); limitParam != "" {
		if parsedLimit, err := strconv.Atoi(limitParam); err == nil && parsedLimit > 0 && parsedLimit <= 50 {
			limit = parsedLimit
		}
	}

	offset := (page - 1) * limit

	query := h.db.Preload("Organizer").
		Where("status = ? AND start_date > ?", "approved", utils.Now())

	// Filter by category if provided
	if category := c.Query("category"); category != "" {
		query = query.Where("? = ANY(category)", category)
	}

	var events []models.Event
	var total int64

	// Get total count
	if err := query.Model(&models.Event{}).Count(&total).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to count events", err)
		return
	}

	// Get events with pagination
	if err := query.Order("start_date ASC").
		Offset(offset).
		Limit(limit).
		Find(&events).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to get upcoming events", err)
		return
	}

	response := map[string]interface{}{
		"events": events,
		"pagination": map[string]interface{}{
			"current_page": page,
			"total_pages":  (total + int64(limit) - 1) / int64(limit),
			"total_items":  total,
			"limit":        limit,
		},
	}

	utils.SuccessResponse(c, http.StatusOK, "Upcoming events retrieved successfully", response)
}

// @Summary Get events by category
// @Description Get events filtered by a specific category
// @Tags Public
// @Accept json
// @Produce json
// @Param category path string true "Category name"
// @Param limit query int false "Limit number of events (default 10, max 50)"
// @Param page query int false "Page number (default 1)"
// @Success 200 {object} utils.Response{data=map[string]interface{}} "Events by category with pagination"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/public/events/category/{category} [get]
func (h *PublicHandler) GetEventsByCategory(c *gin.Context) {
	category := c.Param("category")
	if category == "" {
		utils.BadRequestErrorResponse(c, "Category parameter is required", nil)
		return
	}

	page := 1
	limit := 10

	if pageParam := c.Query("page"); pageParam != "" {
		if parsedPage, err := strconv.Atoi(pageParam); err == nil && parsedPage > 0 {
			page = parsedPage
		}
	}

	if limitParam := c.Query("limit"); limitParam != "" {
		if parsedLimit, err := strconv.Atoi(limitParam); err == nil && parsedLimit > 0 && parsedLimit <= 50 {
			limit = parsedLimit
		}
	}

	offset := (page - 1) * limit

	var events []models.Event
	var total int64

	query := h.db.Preload("Organizer").
		Where("status = ? AND start_date > ? AND ? = ANY(category)", "approved", utils.Now(), category)

	// Get total count
	if err := query.Model(&models.Event{}).Count(&total).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to count events", err)
		return
	}

	// Get events with pagination
	if err := query.Order("start_date ASC").
		Offset(offset).
		Limit(limit).
		Find(&events).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to get events by category", err)
		return
	}

	response := map[string]interface{}{
		"category": category,
		"events":   events,
		"pagination": map[string]interface{}{
			"current_page": page,
			"total_pages":  (total + int64(limit) - 1) / int64(limit),
			"total_items":  total,
			"limit":        limit,
		},
	}

	utils.SuccessResponse(c, http.StatusOK, "Events by category retrieved successfully", response)
}

// @Summary Search events
// @Description Search events by title or description
// @Tags Public
// @Accept json
// @Produce json
// @Param q query string true "Search query"
// @Param category query string false "Filter by category"
// @Param limit query int false "Limit number of events (default 10, max 50)"
// @Param page query int false "Page number (default 1)"
// @Success 200 {object} utils.Response{data=map[string]interface{}} "Search results with pagination"
// @Failure 400 {object} utils.Response "Invalid search query"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/public/events/search [get]
func (h *PublicHandler) SearchEvents(c *gin.Context) {
	searchQuery := c.Query("q")
	if searchQuery == "" {
		utils.BadRequestErrorResponse(c, "Search query 'q' parameter is required", nil)
		return
	}

	page := 1
	limit := 10

	if pageParam := c.Query("page"); pageParam != "" {
		if parsedPage, err := strconv.Atoi(pageParam); err == nil && parsedPage > 0 {
			page = parsedPage
		}
	}

	if limitParam := c.Query("limit"); limitParam != "" {
		if parsedLimit, err := strconv.Atoi(limitParam); err == nil && parsedLimit > 0 && parsedLimit <= 50 {
			limit = parsedLimit
		}
	}

	offset := (page - 1) * limit

	query := h.db.Preload("Organizer").
		Where("status = ? AND start_date > ? AND (title ILIKE ? OR description ILIKE ?)",
			"approved", utils.Now(), "%"+searchQuery+"%", "%"+searchQuery+"%")

	// Filter by category if provided
	if category := c.Query("category"); category != "" {
		query = query.Where("? = ANY(category)", category)
	}

	var events []models.Event
	var total int64

	// Get total count
	if err := query.Model(&models.Event{}).Count(&total).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to count search results", err)
		return
	}

	// Get events with pagination
	if err := query.Order("start_date ASC").
		Offset(offset).
		Limit(limit).
		Find(&events).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to search events", err)
		return
	}

	response := map[string]interface{}{
		"query":  searchQuery,
		"events": events,
		"pagination": map[string]interface{}{
			"current_page": page,
			"total_pages":  (total + int64(limit) - 1) / int64(limit),
			"total_items":  total,
			"limit":        limit,
		},
	}

	utils.SuccessResponse(c, http.StatusOK, "Search results retrieved successfully", response)
}

// PurchaseTicketAsGuest godoc
// @Summary Purchase ticket as guest
// @Description Create a guest ticket purchase and send verification email
// @Tags Public
// @Accept json
// @Produce json
// @Param request body models.GuestPurchaseRequest true "Guest purchase details"
// @Success 201 {object} utils.Response{data=map[string]interface{}} "Purchase created successfully"
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/tickets/guest-purchase [post]
func (h *PublicHandler) PurchaseTicketAsGuest(c *gin.Context) {
	var req models.GuestPurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Purchase ticket as guest
	ticket, guestUser, err := h.ticketService.PurchaseTicketAsGuest(&req)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Purchase failed", err)
		return
	}

	// Send verification email
	if h.ticketService.GetEmailQueueService() != nil {
		if err := h.ticketService.GetEmailQueueService().QueueGuestVerificationEmail(guestUser); err != nil {
			// Log error but don't fail the purchase
			// In production, you might want to implement a retry mechanism
			log.Printf("Failed to queue guest verification email: %v", err)
		}
	}

	response := map[string]interface{}{
		"ticket":     ticket.ToResponse(),
		"guest_user": guestUser.ToResponse(),
		"message":    "Purchase created successfully. Please check your email for verification instructions.",
	}

	utils.SuccessResponse(c, http.StatusCreated, "Guest ticket purchase initiated", response)
}

// VerifyGuestEmail godoc
// @Summary Verify guest email
// @Description Verify guest email using verification token
// @Tags Public
// @Accept json
// @Produce json
// @Param request body models.VerifyGuestEmailRequest true "Verification token"
// @Success 200 {object} utils.Response{data=models.Ticket}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/verify-guest [post]
func (h *PublicHandler) VerifyGuestEmail(c *gin.Context) {
	var req models.VerifyGuestEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	ticket, err := h.ticketService.VerifyGuestEmail(req.Token)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Verification failed", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Email verified successfully", ticket)
}
