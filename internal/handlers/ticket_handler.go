package handlers

import (
	"net/http"
	"strconv"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TicketHandler struct {
	ticketService *services.TicketService
	cfg           *config.Config
}

func NewTicketHandler(ticketService *services.TicketService, cfg *config.Config) *TicketHandler {
	return &TicketHandler{
		ticketService: ticketService,
		cfg:           cfg,
	}
}

// UserGetTicketByID godococ
// @Summary Scan ticket for check-in/check-out
// @Description Scan a ticket QR code to check-in or check-out attendee (Organizer API)
// @Tags Organizer
// @Accept json
// @Produce json
// @Param request body models.TicketCheckInRequest true "Ticket scan details"
// @Success 200 {object} utils.Response{data=models.TicketScanResponse}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/tickets/scan [post]
func (h *TicketHandler) OrganizerScanTicket(c *gin.Context) {
	staffID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "Staff not authenticated", nil)
		return
	}

	var req models.TicketCheckInRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Get ticket details
	ticket, err := h.ticketService.GetTicketByNumber(req.TicketNumber)
	if err != nil {
		utils.NotFoundErrorResponse(c, "Ticket not found", nil)
		return
	}

	// Verify event matches
	if ticket.EventID != req.EventID {
		utils.BadRequestErrorResponse(c, "Ticket does not belong to this event", nil)
		return
	}

	// Get staff user details for response
	staffResp := models.UserResponse{
		ID: staffID.(uuid.UUID),
	}

	response := models.TicketScanResponse{
		TicketNumber: ticket.TicketNumber,
		User:         ticket.User.ToResponse(),
		Event:        *ticket.Event,
		Status:       ticket.Status,
		CheckInTime:  ticket.CheckInTime,
		CheckOutTime: ticket.CheckOutTime,
		ScannedBy:    staffResp,
		ScanTime:     utils.Now(),
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket scanned successfully", response)
}

// OrganizerCheckInTicket godoc
// @Summary Check-in ticket
// @Description Mark a ticket as checked-in for an event (Organizer API)
// @Tags Organizer
// @Accept json
// @Produce json
// @Param request body models.TicketCheckInRequest true "Check-in details"
// @Success 200 {object} utils.Response{data=models.TicketResponse}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/tickets/checkin [post]
func (h *TicketHandler) OrganizerCheckInTicket(c *gin.Context) {
	staffID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "Staff not authenticated", nil)
		return
	}

	var req models.TicketCheckInRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	ticket, err := h.ticketService.CheckInTicket(req.TicketNumber, req.EventID, staffID.(uuid.UUID))
	if err != nil {
		utils.BadRequestErrorResponse(c, err.Error(), nil)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket checked in successfully", ticket.ToResponse())
}

// OrganizerCheckOutTicket godoc
// @Summary Check-out ticket
// @Description Mark a ticket as checked-out from an event (Organizer API)
// @Tags Organizer
// @Accept json
// @Produce json
// @Param request body models.TicketCheckOutRequest true "Check-out details"
// @Success 200 {object} utils.Response{data=models.TicketResponse}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/tickets/checkout [post]
func (h *TicketHandler) OrganizerCheckOutTicket(c *gin.Context) {
	staffID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "Staff not authenticated", nil)
		return
	}

	var req models.TicketCheckOutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	ticket, err := h.ticketService.CheckOutTicket(req.TicketNumber, req.EventID, staffID.(uuid.UUID))
	if err != nil {
		utils.BadRequestErrorResponse(c, err.Error(), nil)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket checked out successfully", ticket.ToResponse())
}

// OrganizerGetEventTickets godoc
// @Summary Get event tickets
// @Description Get all tickets purchased for a specific event (Organizer API)
// @Tags Organizer
// @Produce json
// @Param eventId path int true "Event ID"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} utils.Response{data=[]models.TicketResponse}
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/{id}/tickets [get]
func (h *TicketHandler) OrganizerGetEventTickets(c *gin.Context) {
	organizerID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "Organizer not authenticated", nil)
		return
	}

	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid event ID", err)
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

	tickets, total, err := h.ticketService.GetEventTickets(eventID, organizerID.(uuid.UUID), page, limit)
	if err != nil {
		utils.BadRequestErrorResponse(c, err.Error(), nil)
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
// @Produce json
// @Param eventId path int true "Event ID"
// @Success 200 {object} utils.Response{data=object}
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/events/{id}/tickets/stats [get]
func (h *TicketHandler) OrganizerGetTicketStats(c *gin.Context) {
	organizerID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "Organizer not authenticated", nil)
		return
	}

	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid event ID", err)
		return
	}

	stats, err := h.ticketService.GetTicketStats(eventID, organizerID.(uuid.UUID))
	if err != nil {
		utils.BadRequestErrorResponse(c, err.Error(), nil)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket statistics retrieved successfully", stats)
}

// === USER TICKET MANAGEMENT ===

// UserGetTickets godoc
// @Summary Get user's purchased tickets
// @Description Get list of tickets purchased by the authenticated user
// @Tags User Tickets
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param status query string false "Filter by ticket status (active, used, cancelled)" enum(active,used,cancelled)
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/tickets [get]
func (h *TicketHandler) UserGetTickets(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
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

	status := c.Query("status") // Optional filter

	tickets, total, err := h.ticketService.GetUserTickets(userID.(uuid.UUID), page, limit)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to fetch tickets", err)
		return
	}

	// Apply status filter if provided
	var filteredTickets []models.Ticket
	if status != "" {
		for _, ticket := range tickets {
			if ticket.Status == status {
				filteredTickets = append(filteredTickets, ticket)
			}
		}
		tickets = filteredTickets
	}

	response := map[string]interface{}{
		"tickets": tickets,
		"total":   total,
		"page":    page,
		"limit":   limit,
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
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	ticketIDStr := c.Param("id")
	ticketID, err := uuid.Parse(ticketIDStr)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid ticket ID", err)
		return
	}

	// Get ticket by ID and verify ownership
	ticket, err := h.ticketService.GetTicketByID(ticketID)
	if err != nil {
		utils.NotFoundErrorResponse(c, "Ticket not found", nil)
		return
	}

	// Verify the ticket belongs to the authenticated user
	if ticket.UserID != userID.(uuid.UUID) {
		utils.ForbiddenErrorResponse(c, "Access denied: Ticket does not belong to user", nil)
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
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	ticketIDStr := c.Param("id")
	ticketID, err := uuid.Parse(ticketIDStr)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid ticket ID", err)
		return
	}

	// Get ticket by ID and verify ownership
	ticket, err := h.ticketService.GetTicketByID(ticketID)
	if err != nil {
		utils.NotFoundErrorResponse(c, "Ticket not found", nil)
		return
	}

	// Verify the ticket belongs to the authenticated user
	if ticket.UserID != userID.(uuid.UUID) {
		utils.ForbiddenErrorResponse(c, "Access denied: Ticket does not belong to user", nil)
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
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	eventIDStr := c.Param("event_id")
	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid event ID", err)
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
		utils.InternalServerErrorResponse(c, "Failed to fetch event tickets", err)
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
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	stats, err := h.ticketService.GetUserTicketStats(userID.(uuid.UUID))
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to fetch ticket statistics", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket statistics retrieved successfully", stats)
}
