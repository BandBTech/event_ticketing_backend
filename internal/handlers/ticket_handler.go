package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TicketHandler struct {
	ticketService   *services.TicketService
	secureQRService *services.SecureQRService
	cfg             *config.Config
}

func NewTicketHandler(ticketService *services.TicketService, cfg *config.Config, secureQRService *services.SecureQRService) *TicketHandler {
	return &TicketHandler{
		ticketService:   ticketService,
		secureQRService: secureQRService,
		cfg:             cfg,
	}
}

// validateEventPurchaseEligibility checks if an event allows ticket purchases
func (h *TicketHandler) validateEventPurchaseEligibility(eventID uuid.UUID, tierID uuid.UUID) error {
	return utils.ValidateEventPurchaseEligibility(database.GetDB(), eventID.String(), tierID.String())
}

// getOrganizerIDForUser returns the organizer ID for the given user
// For organizers: returns their user ID
// For staff/managers: returns their organizer_id
func (h *TicketHandler) getOrganizerIDForUser(userID uuid.UUID) (uuid.UUID, error) {
	// Get user with roles
	var user models.User
	if err := database.GetDB().Preload("Roles").Where("id = ?", userID).First(&user).Error; err != nil {
		return uuid.Nil, utils.NewNotFoundError("user")
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

	// For staff/managers, check if they have organizer_id
	if user.OrganizerID == nil {
		return uuid.Nil, utils.NewForbiddenError("Staff/manager does not belong to an organizer.")
	}

	return *user.OrganizerID, nil
}

// UserGetTicketByID godococ
// @Summary Scan ticket for check-in/check-out
// @Description Scan a ticket QR code to check-in or check-out attendee (Organizer API)
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body models.TicketCheckInRequest true "Ticket scan details"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/tickets/scan [post]
func (h *TicketHandler) OrganizerScanTicket(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewUnauthorizedError("User not authenticated."))
		return
	}

	// Get the organizer ID (for staff/managers, it's their organizer_id)
	organizerID, err := h.getOrganizerIDForUser(userID.(uuid.UUID))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	var req models.TicketCheckInRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate the secure QR code
	qrData, err := h.secureQRService.ValidateSecureQR(req.QRCode, req.EventID, organizerID)
	if err != nil {
		utils.HandleError(c, utils.NewBusinessLogicError("Invalid or expired QR code."))
		return
	}

	// Get ticket details using the ticket number from QR data
	ticket, err := h.ticketService.GetTicketByNumber(qrData.TicketNumber)
	if err != nil {
		utils.HandleError(c, utils.NewNotFoundError("ticket"))
		return
	}

	// Verify event matches
	if ticket.EventID != req.EventID {
		utils.HandleError(c, utils.NewBusinessLogicError("Ticket does not belong to this event."))
		return
	}

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, req.EventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate that the event is not in the future (allow scanning up to 24 hours before event)
	now := time.Now()
	if ticket.Event.StartDate.After(now.Add(24 * time.Hour)) {
		utils.HandleError(c, utils.NewBusinessLogicError("Cannot scan tickets for upcoming events."))
		return
	}

	// Validate that the event has not ended
	if ticket.Event.EndDate.Before(now) {
		utils.HandleError(c, utils.NewBusinessLogicError("Cannot scan tickets: event has already ended."))
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket scanned successfully.", nil)
}

// OrganizerCheckInTicket godoc
// @Summary Check-in ticket
// @Description Mark a ticket as checked-in for an event (Organizer, Manager, or Staff API)
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body models.TicketCheckInRequest true "Check-in details"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/tickets/checkin [post]
func (h *TicketHandler) OrganizerCheckInTicket(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewUnauthorizedError("User not authenticated."))
		return
	}

	// Get the organizer ID
	organizerID, err := h.getOrganizerIDForUser(userID.(uuid.UUID))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	var req models.TicketCheckInRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate the secure QR code
	qrData, err := h.secureQRService.ValidateSecureQR(req.QRCode, req.EventID, organizerID)
	if err != nil {
		utils.HandleError(c, utils.NewBusinessLogicError("Invalid or expired QR code."))
		return
	}

	// Get ticket details to validate organizer ownership and event timing
	ticket, err := h.ticketService.GetTicketByNumber(qrData.TicketNumber)
	if err != nil {
		utils.HandleError(c, utils.NewNotFoundError("ticket"))
		return
	}

	// Verify event matches
	if ticket.EventID != req.EventID {
		utils.HandleError(c, utils.NewBusinessLogicError("Ticket does not belong to this event."))
		return
	}

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, req.EventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate that the event is not in the future (allow check-in up to 24 hours before event)
	now := time.Now()
	if ticket.Event.StartDate.After(now.Add(24 * time.Hour)) {
		utils.HandleError(c, utils.NewBusinessLogicError("Cannot check in tickets for upcoming events."))
		return
	}

	// Check if it's an individual ticket (ITKT-) or parent ticket (TKT-)
	if strings.HasPrefix(qrData.TicketNumber, "ITKT-") {
		// Handle individual ticket check-in (for guest purchases with multiple quantities)
		err := h.ticketService.CheckInIndividualTicket(qrData.TicketNumber, req.EventID, organizerID)
		if err != nil {
			utils.HandleError(c, utils.NewBusinessLogicError(err.Error()))
			return
		}

		utils.SuccessResponse(c, http.StatusOK, "Individual ticket checked in successfully.", nil)
	} else {
		// Handle regular ticket check-in with partial quantity support
		checkInCount := 1 // Default to 1 for backward compatibility
		if req.CheckInCount > 0 {
			checkInCount = req.CheckInCount
		}

		err := h.ticketService.CheckInTicketPartial(qrData.TicketNumber, req.EventID, organizerID, checkInCount)
		if err != nil {
			utils.HandleError(c, utils.NewBusinessLogicError(err.Error()))
			return
		}

		utils.SuccessResponse(c, http.StatusOK, "Ticket checked in successfully.", nil)
	}
}

// OrganizerCheckOutTicket godoc
// @Summary Check-out ticket
// @Description Mark a ticket as checked-out from an event (Organizer, Manager, or Staff API)
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body models.TicketCheckOutRequest true "Check-out details"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/tickets/checkout [post]
func (h *TicketHandler) OrganizerCheckOutTicket(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get the organizer ID
	organizerID, err := h.getOrganizerIDForUser(userID.(uuid.UUID))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	var req models.TicketCheckOutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate the secure QR code
	qrData, err := h.secureQRService.ValidateSecureQR(req.QRCode, req.EventID, organizerID)
	if err != nil {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get ticket details to validate organizer ownership and event timing
	ticket, err := h.ticketService.GetTicketByNumber(qrData.TicketNumber)
	if err != nil {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Verify event matches
	if ticket.EventID != req.EventID {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, req.EventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	// For check-out, we need to find the individual ticket that was checked in
	// and check it out. This is more complex as we need to find the checked-in ticket.
	// For now, we'll assume the ticket number is for an individual ticket
	// since check-out typically happens after check-in

	// Check if it's an individual ticket (ITKT-) or parent ticket (TKT-)
	if strings.HasPrefix(qrData.TicketNumber, "ITKT-") {
		// Handle individual ticket check-out
		err := h.ticketService.CheckOutIndividualTicket(qrData.TicketNumber, req.EventID, organizerID)
		if err != nil {
			utils.HandleError(c, err)
			return
		}

		utils.SuccessResponse(c, http.StatusOK, "Individual ticket checked out successfully", nil)
	} else {
		// Handle regular ticket check-out (legacy single ticket system)
		err := h.ticketService.CheckOutTicket(qrData.TicketNumber, req.EventID, organizerID)
		if err != nil {
			utils.HandleError(c, err)
			return
		}

		utils.SuccessResponse(c, http.StatusOK, "Ticket checked out successfully", nil)
	}
}

// OrganizerGetEventTickets godoc
// @Summary Get event tickets
// @Description Get all tickets purchased for a specific event (Organizer API)
// @Tags Organizer
// @Security ApiKeyAuth
// @Produce json
// @Param id path string true "Event ID (UUID)"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} utils.Response{data=[]models.TicketResponse}
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/{id}/tickets [get]
func (h *TicketHandler) OrganizerGetEventTickets(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get the organizer ID
	organizerID, err := h.getOrganizerIDForUser(userID.(uuid.UUID))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}

	tickets, total, err := h.ticketService.GetEventTickets(eventID, organizerID, page, limit)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Convert to response
	ticketResponses := make([]models.TicketResponse, len(tickets))
	for i, ticket := range tickets {
		ticketResponses[i] = ticket.ToResponse()
	}

	response := map[string]interface{}{
		"tickets": ticketResponses,
		"pagination": map[string]interface{}{
			"page":  page,
			"limit": limit,
			"total": total,
			"pages": (total + int64(limit) - 1) / int64(limit),
		},
	}

	utils.SuccessResponse(c, http.StatusOK, "Event tickets retrieved successfully", response)
}

// OrganizerGetTicketStats godoc
// @Summary Get ticket statistics
// @Description Get ticket statistics for a specific event (Organizer API)
// @Tags Organizer
// @Security ApiKeyAuth
// @Produce json
// @Param id path string true "Event ID (UUID)"
// @Success 200 {object} utils.Response{data=object}
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/{id}/tickets/stats [get]
func (h *TicketHandler) OrganizerGetTicketStats(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get the organizer ID
	organizerID, err := h.getOrganizerIDForUser(userID.(uuid.UUID))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	stats, err := h.ticketService.GetTicketStats(eventID, organizerID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket statistics retrieved successfully", stats)
}

// === USER TICKET MANAGEMENT ===

// UserPurchaseTicket godoc
// @Summary Purchase tickets for logged-in user
// @Description Purchase multiple individual tickets for a logged-in user. Logged-in users can purchase up to 10 tickets.
// @Tags User
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body models.TicketPurchaseRequest true "Ticket purchase details"
// @Success 201 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/tickets/purchase [post]
func (h *TicketHandler) UserPurchaseTicket(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	var req models.TicketPurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate event purchase eligibility
	if err := h.validateEventPurchaseEligibility(req.EventID, req.TierID); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Purchase tickets (returns multiple individual tickets)
	tickets, err := h.ticketService.PurchaseTicket(userID.(uuid.UUID), &req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Convert tickets to response format
	var ticketResponses []models.TicketResponse
	for _, ticket := range tickets {
		ticketResponses = append(ticketResponses, ticket.ToResponse())
	}

	response := map[string]interface{}{
		"tickets": ticketResponses,
		"message": fmt.Sprintf("Purchase successful! %d ticket confirmation emails have been sent.", len(tickets)),
	}

	utils.SuccessResponse(c, http.StatusCreated, "Tickets purchased successfully", response)
}

// UserGetTickets godoc
// @Summary Get user's purchased tickets
// @Description Get a list of tickets purchased by the authenticated user with pagination and filtering
// @Tags User Tickets
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param status query string false "Filter by ticket status (active, used, cancelled, refunded)" enum(active,used,cancelled,refunded)
// @Param event_id query string false "Filter by event ID"
// @Param start_date query string false "Filter tickets purchased after this date (YYYY-MM-DD)"
// @Param end_date query string false "Filter tickets purchased before this date (YYYY-MM-DD)"
// @Param sort query string false "Sort by field with optional '-' prefix for desc (e.g., '-purchase_date', 'ticket_number')" default(-purchase_date)
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/tickets [get]
func (h *TicketHandler) UserGetTickets(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

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
	status := c.Query("status")
	eventID := c.Query("event_id")
	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")
	sortParam := c.DefaultQuery("sort", "-purchase_date")

	// Parse date filters
	var startDate, endDate *time.Time
	if startDateStr != "" {
		if parsed, err := time.Parse("2006-01-02", startDateStr); err == nil {
			startDate = &parsed
		}
	}
	if endDateStr != "" {
		if parsed, err := time.Parse("2006-01-02", endDateStr); err == nil {
			// Set to end of day
			endOfDay := parsed.Add(24*time.Hour - time.Second)
			endDate = &endOfDay
		}
	}

	// Validate and parse sort parameters
	validSortFields := map[string]bool{
		"purchase_date": true,
		"created_at":    true,
		"ticket_number": true,
		"price":         true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, validSortFields, "purchase_date", "desc")

	tickets, total, err := h.ticketService.GetUserTickets(userID.(uuid.UUID), page, limit, status, eventID, sortBy, sortOrder, startDate, endDate)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"tickets":     tickets,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": (total + int64(limit) - 1) / int64(limit),
		"has_next":    int64(page*limit) < total,
		"has_prev":    page > 1,
	}

	utils.SuccessResponse(c, http.StatusOK, "Tickets retrieved successfully", response)
}

// UserGetTicketByID godoc
// @Summary Get specific ticket details
// @Description Get detailed information about a specific ticket owned by the user
// @Tags User Tickets
// @Produce json
// @Param id path string true "Ticket ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.Ticket}
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/tickets/{id} [get]
func (h *TicketHandler) UserGetTicketByID(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	ticketIDStr := c.Param("id")
	ticketID, err := uuid.Parse(ticketIDStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get ticket by ID and verify ownership
	ticket, err := h.ticketService.GetTicketByID(ticketID)
	if err != nil {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Verify the ticket belongs to the authenticated user
	userIDValue := userID.(uuid.UUID)
	if ticket.UserID == nil || *ticket.UserID != userIDValue {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket details retrieved successfully", ticket)
}

// UserGetTicketQR godoc
// @Summary Get ticket QR code
// @Description Get QR code data for a specific ticket (for mobile apps)
// @Tags User Tickets
// @Produce json
// @Param id path string true "Ticket ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/tickets/{id}/qr [get]
func (h *TicketHandler) UserGetTicketQR(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	ticketIDStr := c.Param("id")
	ticketID, err := uuid.Parse(ticketIDStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get ticket by ID and verify ownership
	ticket, err := h.ticketService.GetTicketByID(ticketID)
	if err != nil {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Verify the ticket belongs to the authenticated user
	userIDValue := userID.(uuid.UUID)
	if ticket.UserID == nil || *ticket.UserID != userIDValue {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Check if the event has ended
	if ticket.Event != nil && !ticket.Event.EndDate.IsZero() && ticket.Event.EndDate.Before(time.Now()) {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Generate QR code data (ticket number for scanning)
	qrData := map[string]interface{}{
		"ticket_number": ticket.TicketNumber,
		"event_id":      ticket.EventID,
		"ticket_id":     ticket.ID,
		"user_id":       ticket.UserID,
		"event_title":   ticket.Event.Title,
		"valid_until":   ticket.Event.EndDate,
	}

	// If ticket service is configured with SecureQRService, include the signed QR payload
	if h.ticketService != nil {
		if qrPayload, err := h.ticketService.GenerateQRCodeForTicket(ticket.ID); err == nil {
			qrData["qr_payload"] = qrPayload // base64-encoded JSON payload (signed)
		}
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket QR code data retrieved successfully", qrData)
}

// UserGetEventTickets godoc
// @Summary Get user's tickets for a specific event
// @Description Get all tickets purchased by the user for a specific event
// @Tags User Tickets
// @Produce json
// @Param event_id path string true "Event ID"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 401 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/events/{event_id}/tickets [get]
func (h *TicketHandler) UserGetEventTickets(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	eventIDStr := c.Param("event_id")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	page := 1
	if pageParam := c.Query("page"); pageParam != "" {
		if p, err := strconv.Atoi(pageParam); err == nil && p > 0 {
			page = p
		}
	}

	limit := 10
	if limitParam := c.Query("limit"); limitParam != "" {
		if l, err := strconv.Atoi(limitParam); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	tickets, total, err := h.ticketService.GetUserEventTickets(userID.(uuid.UUID), eventID, page, limit)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"tickets": tickets,
		"total":   total,
		"page":    page,
		"limit":   limit,
	}

	utils.SuccessResponse(c, http.StatusOK, "Event tickets retrieved successfully", response)
}

// UserGetTicketStats godoc
// @Summary Get user's ticket statistics
// @Description Get statistics about user's ticket purchases and usage
// @Tags User Tickets
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/tickets/stats [get]
func (h *TicketHandler) UserGetTicketStats(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	stats, err := h.ticketService.GetUserTicketStats(userID.(uuid.UUID))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket statistics retrieved successfully", stats)
}
