package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EventManagementHandler struct {
	eventMgmtService *services.EventManagementService
	payoutService    *services.PayoutService
}

func NewEventManagementHandler() *EventManagementHandler {
	return &EventManagementHandler{
		eventMgmtService: services.NewEventManagementService(),
		payoutService:    services.NewPayoutService(),
	}
}

// ControlEventSales godoc
// @Summary Control event sales (Organizer)
// @Description Pause, resume, or stop event sales
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
func (h *EventManagementHandler) ControlEventSales(c *gin.Context) {
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid event ID", err)
		return
	}

	userIDInterface, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}
	organizerID := userID

	var req models.EventSalesControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request body", err)
		return
	}

	err = h.eventMgmtService.ControlEventSales(eventID, organizerID, &req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Failed to control event sales", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event sales status updated successfully", nil)
}

// CancelEvent godoc
// @Summary Cancel an event (Organizer/Admin)
// @Description Cancel an event with reason. Admins can cancel any event, organizers can only cancel their own events.
// @Tags Organizer
// @Accept json
// @Produce json
// @Param id path string true "Event ID"
// @Param request body models.EventCancellationRequest true "Cancellation request"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.Event}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/{id}/cancel [put]
// @Router /api/v1/admin/events/{id}/cancel [put]
func (h *EventManagementHandler) CancelEvent(c *gin.Context) {
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid event ID", err)
		return
	}

	userIDInterface, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}

	// Check if user is admin/subadmin
	rolesInterface, rolesExists := c.Get("roles")
	isAdmin := false
	if rolesExists {
		if roles, ok := rolesInterface.([]string); ok {
			for _, role := range roles {
				if role == "admin" || role == "subadmin" {
					isAdmin = true
					break
				}
			}
		}
	}

	var organizerID uuid.UUID
	if isAdmin {
		// For admin, we need to find the event first to get the organizer ID for logging
		var event models.Event
		if err := h.eventMgmtService.GetDB().Where("id = ?", eventID).First(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				utils.NotFoundErrorResponse(c, "Event not found", nil)
				return
			}
			utils.InternalServerErrorResponse(c, "Failed to fetch event", err)
			return
		}
		organizerID = event.OrganizerID
	} else {
		// For organizer, use their own ID
		organizerID = userID
	}

	var req models.EventCancellationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request body", err)
		return
	}

	err = h.eventMgmtService.CancelEvent(eventID, organizerID, &req, isAdmin)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Failed to cancel event", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event cancelled successfully", nil)
}

// GetEventAnalytics godoc
// @Summary Get event analytics (Organizer)
// @Description Get comprehensive analytics for an event including tier breakdown
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
func (h *EventManagementHandler) GetEventAnalytics(c *gin.Context) {
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid event ID", err)
		return
	}

	userIDInterface, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}
	organizerID := userID

	analytics, err := h.eventMgmtService.GetEventAnalytics(eventID, organizerID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "Failed to get event analytics", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event analytics retrieved successfully", analytics)
}

// GetAllEventsAnalytics godoc
// @Summary Get all events analytics (Admin)
// @Description Get analytics for all events with pagination
// @Tags Admin
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(20)
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=[]models.EventAnalyticsResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/events/analytics [get]
func (h *EventManagementHandler) GetAllEventsAnalytics(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	analytics, total, err := h.eventMgmtService.GetAllEventsAnalytics(page, limit)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to get events analytics", err)
		return
	}

	response := map[string]interface{}{
		"analytics":   analytics,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": (total + int64(limit) - 1) / int64(limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Events analytics retrieved successfully", response)
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
func (h *EventManagementHandler) GetOrganizerTierTemplates(c *gin.Context) {
	userIDInterface, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}
	organizerID := userID

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
func (h *EventManagementHandler) CreateOrganizerTierTemplate(c *gin.Context) {
	userIDInterface, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}
	organizerID := userID

	var req models.CreateOrganizerTierTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request body", err)
		return
	}

	err := h.eventMgmtService.CreateOrganizerTierTemplate(organizerID, &req)
	if err != nil {
		if http.StatusText(http.StatusConflict) != "" { // Check for conflict error
			utils.ErrorResponse(c, http.StatusConflict, "Template name already exists", err)
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
func (h *EventManagementHandler) UpdateOrganizerTierTemplate(c *gin.Context) {
	templateID, err := uuid.Parse(c.Param("templateId"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid template ID", err)
		return
	}

	userIDInterface, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}
	organizerID := userID

	var req models.UpdateOrganizerTierTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request body", err)
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
func (h *EventManagementHandler) DeleteOrganizerTierTemplate(c *gin.Context) {
	templateID, err := uuid.Parse(c.Param("templateId"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid template ID", err)
		return
	}

	userIDInterface, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}
	organizerID := userID

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
// @Success 201 {object} utils.Response{data=models.PayoutRequest}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/payout-requests [post]
func (h *EventManagementHandler) CreatePayoutRequest(c *gin.Context) {
	userIDInterface, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}
	organizerID := userID

	var req models.PayoutRequestCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request body", err)
		return
	}

	err := h.payoutService.CreatePayoutRequest(organizerID, &req)
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
// @Param status query string false "Filter by status" Enums(pending, approved, rejected, paid)
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=[]models.PayoutRequest}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/payout-requests [get]
func (h *EventManagementHandler) GetOrganizerPayoutRequests(c *gin.Context) {
	userIDInterface, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}
	organizerID := userID

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	requests, total, err := h.payoutService.GetOrganizerPayoutRequests(organizerID, page, limit, status)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to get payout requests", err)
		return
	}

	response := map[string]interface{}{
		"requests":    requests,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": (total + int64(limit) - 1) / int64(limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Payout requests retrieved successfully", response)
}

// GetAllPayoutRequests godoc
// @Summary Get all payout requests (Admin)
// @Description Get all payout requests from all organizers
// @Tags Admin Payouts
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Page size" default(20)
// @Param status query string false "Filter by status" Enums(pending, approved, rejected, paid)
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=[]models.PayoutRequest}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payout-requests [get]
func (h *EventManagementHandler) GetAllPayoutRequests(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	requests, total, err := h.payoutService.GetAllPayoutRequests(page, limit, status)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to get payout requests", err)
		return
	}

	response := map[string]interface{}{
		"requests":    requests,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": (total + int64(limit) - 1) / int64(limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Payout requests retrieved successfully", response)
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
// @Success 200 {object} utils.Response{data=models.PayoutRequest}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payout-requests/{id} [put]
func (h *EventManagementHandler) UpdatePayoutRequestStatus(c *gin.Context) {
	requestID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid request ID", err)
		return
	}

	userIDInterface, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}
	adminID := userID

	var req models.PayoutRequestUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request body", err)
		return
	}

	err = h.payoutService.UpdatePayoutRequestStatus(requestID, adminID, &req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Failed to update payout request", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payout request updated successfully", nil)
}

// GetPayoutSummary godoc
// @Summary Get payout summary (Organizer)
// @Description Get payout summary including earnings, received amount, and pending requests
// @Tags Organizer
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/payout-summary [get]
func (h *EventManagementHandler) GetPayoutSummary(c *gin.Context) {
	userIDInterface, exists := c.Get("user_id")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}
	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}
	organizerID := userID

	summary, err := h.payoutService.GetOrganizerPayoutSummary(organizerID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to get payout summary", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payout summary retrieved successfully", summary)
}
