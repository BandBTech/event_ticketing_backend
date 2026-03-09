package handlers

import (
	"fmt"
	"net/http"
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

// validateEventPurchaseEligibility checks if an event allows ticket purchases for multiple tiers
func (h *TicketHandler) validateEventPurchaseEligibility(eventID uuid.UUID, tierSelections []models.TicketTierSelection) error {
	return utils.ValidateEventPurchaseEligibilityForTiers(database.GetDB(), eventID.String(), tierSelections)
}

// getOrganizerIDForUser uses centralized utility
func (h *TicketHandler) getOrganizerIDForUser(userID uuid.UUID) (uuid.UUID, error) {
	return utils.GetOrganizerIDForUser(database.GetDB(), userID)
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

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, req.EventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	// For scanning, we need to validate the ticket exists and belongs to the event
	// Get ticket details to validate
	ticketID, err := uuid.Parse(qrData.TicketID)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid ticket ID in QR code.", nil))
		return
	}
	ticket, err := h.ticketService.GetTicketByID(ticketID)
	if err != nil {
		utils.HandleError(c, utils.NewNotFoundError("ticket"))
		return
	}

	// Verify event matches
	if ticket.EventID != req.EventID {
		utils.HandleError(c, utils.NewBusinessLogicError("Ticket does not belong to this event."))
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

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, req.EventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Check-in the ticket (simplified: one ticket = one person)
	ticketID, err := uuid.Parse(qrData.TicketID)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid ticket ID in QR code.", nil))
		return
	}
	err = h.ticketService.CheckInTicket(ticketID, req.EventID, organizerID)
	if err != nil {
		utils.HandleError(c, utils.NewBusinessLogicError(err.Error()))
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket checked in successfully.", nil)
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
		utils.HandleError(c, utils.NewBusinessLogicError("Invalid or expired QR code."))
		return
	}

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, req.EventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Check-out the ticket (simplified: one ticket = one person)
	ticketID, err := uuid.Parse(qrData.TicketID)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid ticket ID in QR code.", nil))
		return
	}
	err = h.ticketService.CheckOutTicket(ticketID, req.EventID, organizerID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket checked out successfully", nil)
}

// OrganizerBulkCheckInTickets godoc
// @Summary Bulk check-in multiple tickets
// @Description Check-in multiple tickets at once using QR codes (Organizer, Manager, or Staff API)
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body models.TicketBulkCheckInRequest true "Bulk check-in details"
// @Success 200 {object} utils.Response{data=[]map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/tickets/bulk-checkin [post]
func (h *TicketHandler) OrganizerBulkCheckInTickets(c *gin.Context) {
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

	var req models.TicketBulkCheckInRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, req.EventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Bulk check-in tickets
	results, err := h.ticketService.BulkCheckInTickets(req.QRCodes, req.EventID, organizerID)
	if err != nil {
		utils.HandleError(c, utils.NewBusinessLogicError("Bulk check-in failed"))
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Bulk check-in completed", results)
}

// OrganizerBulkCheckOutTickets godoc
// @Summary Bulk check-out multiple tickets
// @Description Check-out multiple tickets at once using QR codes (Organizer, Manager, or Staff API)
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body models.TicketBulkCheckOutRequest true "Bulk check-out details"
// @Success 200 {object} utils.Response{data=[]map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/tickets/bulk-checkout [post]
func (h *TicketHandler) OrganizerBulkCheckOutTickets(c *gin.Context) {
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

	var req models.TicketBulkCheckOutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, req.EventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Bulk check-out tickets
	results, err := h.ticketService.BulkCheckOutTickets(req.QRCodes, req.EventID, organizerID)
	if err != nil {
		utils.HandleError(c, utils.NewBusinessLogicError("Bulk check-out failed"))
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Bulk check-out completed", results)
}

// OrganizerValidateTicketForCheckIn godoc
// @Summary Validate a single ticket for check-in
// @Description Validate a ticket for check-in without actually checking it in (Organizer, Manager, or Staff API)
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body models.TicketCheckInRequest true "Check-in validation details"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/tickets/validate-checkin [post]
func (h *TicketHandler) OrganizerValidateTicketForCheckIn(c *gin.Context) {
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

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, req.EventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate ticket for check-in
	result, err := h.ticketService.ValidateTicketForCheckIn(req.QRCode, req.EventID, organizerID)
	if err != nil {
		utils.HandleError(c, utils.NewBusinessLogicError("Validation failed"))
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket validation completed", result)
}

// OrganizerValidateTicketForCheckOut godoc
// @Summary Validate a single ticket for check-out
// @Description Validate a ticket for check-out without actually checking it out (Organizer, Manager, or Staff API)
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body models.TicketCheckOutRequest true "Check-out validation details"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/tickets/validate-checkout [post]
func (h *TicketHandler) OrganizerValidateTicketForCheckOut(c *gin.Context) {
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

	var req models.TicketCheckOutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, req.EventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate ticket for check-out
	result, err := h.ticketService.ValidateTicketForCheckOut(req.QRCode, req.EventID, organizerID)
	if err != nil {
		utils.HandleError(c, utils.NewBusinessLogicError("Validation failed"))
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket validation completed", result)
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

	pagination := utils.GetPaginationParams(c, 10)

	tickets, total, err := h.ticketService.GetEventTickets(eventID, organizerID, pagination.Page, pagination.Limit)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"tickets":    tickets,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
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
// @Description Purchase multiple individual tickets for a logged-in user. Logged-in users can purchase up to 10 tickets. Requires event_id, tier_id, quantity, and payment_gateway.
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

	// Validate event purchase eligibility for all tiers
	if err := h.validateEventPurchaseEligibility(req.EventID, req.Tiers); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get user details for email
	var user models.User
	if err := database.GetDB().Where("id = ?", userID).First(&user).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Check if the payment gateway is cash - if so, purchase immediately
	if req.PaymentGateway == models.PaymentGatewayCash {
		// Purchase tickets (returns multiple individual tickets)
		tickets, err := h.ticketService.PurchaseTicket(userID.(uuid.UUID), &req)
		if err != nil {
			utils.HandleError(c, err)
			return
		}

		utils.SuccessResponse(c, http.StatusCreated, fmt.Sprintf("Successfully purchased %d tickets! Confirmation emails have been sent.", len(tickets)), nil)
		return
	}

	// For payment gateways (stripe, paypal, esewa, khalti, imepay), create checkout session
	checkoutSession, _, err := h.ticketService.InitiateUserPaymentGatewayPurchase(userID.(uuid.UUID), &req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Payment initiated successfully. Please complete payment using the provided gateway data.", checkoutSession.ToResponse())
}

// UserGetTickets godoc
// @Summary Get user's purchased tickets grouped by transaction
// @Description Get a list of tickets purchased by the authenticated user, grouped by transaction with pagination and filtering
// @Tags User Tickets
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param filter query string false "Filter by event timing (all, upcoming, past)" enum(all,upcoming,past)
// @Param search query string false "Search by ticket number, event name, venue, address, or location"
// @Param event_id query string false "Filter by event ID"
// @Param start_date query string false "Filter tickets purchased after this date (YYYY-MM-DD)"
// @Param end_date query string false "Filter tickets purchased before this date (YYYY-MM-DD)"
// @Param sort query string false "Sort by field with optional '-' prefix for desc (e.g., '-created_at', 'ticket_count')" default(-created_at)
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
	pagination := utils.GetPaginationParams(c, 10)

	// Filter params
	filter := c.Query("filter") // all, upcoming, past
	search := c.Query("search") // search by ticket number, event name, venue, address, location
	eventID := c.Query("event_id")
	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")
	sortParam := c.DefaultQuery("sort", "-created_at")

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
		"created_at":   true,
		"ticket_count": true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, validSortFields, "created_at", "desc")

	ticketSummaries, total, err := h.ticketService.GetUserTicketSummaries(userID.(uuid.UUID), pagination.Page, pagination.Limit, filter, search, eventID, sortBy, sortOrder, startDate, endDate)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"tickets":    ticketSummaries,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Tickets retrieved successfully", response)
}

// UserGetTicketByID godoc
// @Summary Get transaction details with tickets
// @Description Get detailed information about a specific transaction with all its tickets
// @Tags User Tickets
// @Produce json
// @Param id path string true "Transaction ID"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.UserTransactionWithTicketsResponse}
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

	transactionIDStr := c.Param("id")
	transactionID, err := uuid.Parse(transactionIDStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get transaction details with tickets
	transactionDetails, err := h.ticketService.GetUserTransactionDetails(userID.(uuid.UUID), transactionID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Transaction details retrieved successfully", transactionDetails)
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

	pagination := utils.GetPaginationParams(c, 10)

	tickets, total, err := h.ticketService.GetUserEventTickets(userID.(uuid.UUID), eventID, pagination.Page, pagination.Limit)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"tickets":    tickets,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
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
