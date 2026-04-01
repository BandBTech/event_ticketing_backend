package handlers

import (
	"encoding/json"
	"fmt"
	"io"
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
// @Description Mark a ticket as checked-in for an event using QR code or ticket number (Organizer, Manager, or Staff API)
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

	// Validate that either QR code or ticket number is provided
	if req.QRCode == "" && req.TicketNumber == "" {
		utils.HandleError(c, utils.NewValidationError("Either qr_code or ticket_number must be provided.", nil))
		return
	}

	if req.QRCode != "" && req.TicketNumber != "" {
		utils.HandleError(c, utils.NewValidationError("Provide either qr_code OR ticket_number, not both.", nil))
		return
	}

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, req.EventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	var ticketID uuid.UUID

	// Handle QR code validation
	if req.QRCode != "" {
		qrData, err := h.secureQRService.ValidateSecureQR(req.QRCode, req.EventID, organizerID)
		if err != nil {
			utils.HandleError(c, utils.NewBusinessLogicError("Invalid or expired QR code."))
			return
		}

		ticketID, err = uuid.Parse(qrData.TicketID)
		if err != nil {
			utils.HandleError(c, utils.NewValidationError("Invalid ticket ID in QR code.", nil))
			return
		}
	} else {
		// Handle ticket number validation
		validationResult, err := h.ticketService.ValidateTicketForCheckInByNumber(req.TicketNumber, req.EventID, organizerID)
		if err != nil {
			utils.HandleError(c, utils.NewBusinessLogicError("Validation failed"))
			return
		}

		if !validationResult["valid"].(bool) || !validationResult["can_checkin"].(bool) {
			utils.HandleError(c, utils.NewBusinessLogicError(validationResult["message"].(string)))
			return
		}

		// Extract ticket ID from validation result
		ticketInfo := validationResult["ticket_info"].(map[string]interface{})
		ticketID, err = uuid.Parse(ticketInfo["ticket_id"].(string))
		if err != nil {
			utils.HandleError(c, utils.NewValidationError("Invalid ticket ID from validation.", nil))
			return
		}
	}

	// Check-in the ticket
	err = h.ticketService.CheckInTicket(ticketID, req.EventID, userID.(uuid.UUID))
	if err != nil {
		utils.HandleError(c, utils.NewBusinessLogicError(err.Error()))
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket checked in successfully.", nil)
}

// OrganizerCheckOutTicket godoc
// @Summary Check-out ticket
// @Description Mark a ticket as checked-out from an event using QR code or ticket number (Organizer, Manager, or Staff API)
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

	// Validate that either QR code or ticket number is provided
	if req.QRCode == "" && req.TicketNumber == "" {
		utils.HandleError(c, utils.NewValidationError("Either qr_code or ticket_number must be provided.", nil))
		return
	}

	if req.QRCode != "" && req.TicketNumber != "" {
		utils.HandleError(c, utils.NewValidationError("Provide either qr_code OR ticket_number, not both.", nil))
		return
	}

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, req.EventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	var ticketID uuid.UUID

	// Handle QR code validation
	if req.QRCode != "" {
		qrData, err := h.secureQRService.ValidateSecureQR(req.QRCode, req.EventID, organizerID)
		if err != nil {
			utils.HandleError(c, utils.NewBusinessLogicError("Invalid or expired QR code."))
			return
		}

		ticketID, err = uuid.Parse(qrData.TicketID)
		if err != nil {
			utils.HandleError(c, utils.NewValidationError("Invalid ticket ID in QR code.", nil))
			return
		}
	} else {
		// Handle ticket number validation
		validationResult, err := h.ticketService.ValidateTicketForCheckOutByNumber(req.TicketNumber, req.EventID, organizerID)
		if err != nil {
			utils.HandleError(c, utils.NewBusinessLogicError("Validation failed"))
			return
		}

		if !validationResult["valid"].(bool) || !validationResult["can_checkout"].(bool) {
			utils.HandleError(c, utils.NewBusinessLogicError(validationResult["message"].(string)))
			return
		}

		// Extract ticket ID from validation result
		ticketInfo := validationResult["ticket_info"].(map[string]interface{})
		ticketID, err = uuid.Parse(ticketInfo["ticket_id"].(string))
		if err != nil {
			utils.HandleError(c, utils.NewValidationError("Invalid ticket ID from validation.", nil))
			return
		}
	}

	// Check-out the ticket
	err = h.ticketService.CheckOutTicket(ticketID, req.EventID, userID.(uuid.UUID))
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
	results, err := h.ticketService.BulkCheckInTickets(req.QRCodes, req.EventID, userID.(uuid.UUID))
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
	results, err := h.ticketService.BulkCheckOutTickets(req.QRCodes, req.EventID, userID.(uuid.UUID))
	if err != nil {
		utils.HandleError(c, utils.NewBusinessLogicError("Bulk check-out failed"))
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Bulk check-out completed", results)
}

// OrganizerValidateTicketForCheckIn godoc
// @Summary Validate a single ticket for check-in
// @Description Validate a ticket for check-in without actually checking it in using QR code or ticket number (Organizer, Manager, or Staff API)
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

	// Validate that either QR code or ticket number is provided
	if req.QRCode == "" && req.TicketNumber == "" {
		utils.HandleError(c, utils.NewValidationError("Either qr_code or ticket_number must be provided.", nil))
		return
	}

	if req.QRCode != "" && req.TicketNumber != "" {
		utils.HandleError(c, utils.NewValidationError("Provide either qr_code OR ticket_number, not both.", nil))
		return
	}

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, req.EventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	var result map[string]interface{}

	// Validate ticket for check-in
	if req.QRCode != "" {
		result, err = h.ticketService.ValidateTicketForCheckIn(req.QRCode, req.EventID, organizerID)
	} else {
		result, err = h.ticketService.ValidateTicketForCheckInByNumber(req.TicketNumber, req.EventID, organizerID)
	}

	if err != nil {
		utils.HandleError(c, utils.NewBusinessLogicError("Validation failed"))
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket validation completed", result)
}

// OrganizerValidateTicketForCheckOut godoc
// @Summary Validate a single ticket for check-out
// @Description Validate a ticket for check-out without actually checking it out using QR code or ticket number (Organizer, Manager, or Staff API)
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

	// Validate that either QR code or ticket number is provided
	if req.QRCode == "" && req.TicketNumber == "" {
		utils.HandleError(c, utils.NewValidationError("Either qr_code or ticket_number must be provided.", nil))
		return
	}

	if req.QRCode != "" && req.TicketNumber != "" {
		utils.HandleError(c, utils.NewValidationError("Provide either qr_code OR ticket_number, not both.", nil))
		return
	}

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, req.EventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	var result map[string]interface{}

	// Validate ticket for check-out
	if req.QRCode != "" {
		result, err = h.ticketService.ValidateTicketForCheckOut(req.QRCode, req.EventID, organizerID)
	} else {
		result, err = h.ticketService.ValidateTicketForCheckOutByNumber(req.TicketNumber, req.EventID, organizerID)
	}

	if err != nil {
		utils.HandleError(c, utils.NewBusinessLogicError("Validation failed"))
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket validation completed", result)
}

// OrganizerSearchTickets godoc
// @Summary Search tickets by number
// @Description Perform real-time search for tickets by partial ticket number (Organizer, Manager, or Staff API)
// @Tags Organizer
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param event_id query string true "Event ID"
// @Param q query string true "Search query (partial ticket number)"
// @Param limit query int false "Maximum results (default: 10, max: 50)"
// @Success 200 {object} utils.Response{data=[]map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/tickets/search [get]
func (h *TicketHandler) OrganizerSearchTickets(c *gin.Context) {
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

	eventIDStr := c.Query("event_id")
	if eventIDStr == "" {
		utils.HandleError(c, utils.NewValidationError("event_id is required", nil))
		return
	}

	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid event_id format", nil))
		return
	}

	searchQuery := c.Query("q")
	if searchQuery == "" {
		utils.HandleError(c, utils.NewValidationError("q (search query) is required", nil))
		return
	}

	// Validate staff access to this event
	if err := h.ticketService.ValidateStaffAccessToEvent(organizerID, eventID); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Parse limit parameter
	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		limit = 10
	}

	// Search tickets
	results, err := h.ticketService.SearchTicketsByNumber(eventID, searchQuery, limit)
	if err != nil {
		utils.HandleError(c, utils.NewBusinessLogicError("Search failed"))
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Search completed", results)
}

// OrganizerGetEventTickets godoc
// @Summary Get event tickets
// @Description Get all tickets purchased for a specific event (Organizer API) with search, filter, and sorting
// @Tags Organizer
// @Security ApiKeyAuth
// @Produce json
// @Param id path string true "Event ID (UUID)"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param search query string false "Search by ticket number, attendee name, or email"
// @Param status query string false "Filter by ticket status (active, used, cancelled, refunded)"
// @Param tier_id query string false "Filter by tier ID (UUID)"
// @Param checkin_status query string false "Filter by check-in status (checked_in, not_checked_in, checked_out)"
// @Param sort_by query string false "Sort by field (created_at, ticket_number, total_amount, status, tier, check_in_time, checked_in_by, purchase_date, purchased_by)" default(created_at)
// @Param sort_order query string false "Sort order (asc, desc)" default(desc)
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
	search := c.Query("search")
	status := c.Query("status")
	tierIDStr := c.Query("tier_id")
	checkinStatus := c.Query("checkin_status")
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	// Parse tier ID if provided
	var tierID *uuid.UUID
	if tierIDStr != "" {
		if parsedID, err := uuid.Parse(tierIDStr); err == nil {
			tierID = &parsedID
		}
	}

	// Validate sort parameters using centralized utility
	sortBy, sortOrder = utils.ValidateSortForTickets(sortBy, sortOrder)

	tickets, total, err := h.ticketService.GetEventTicketsWithFilters(eventID, organizerID, search, status, tierID, checkinStatus, pagination.Page, pagination.Limit, sortBy, sortOrder)
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

// UserCancelTicket godoc
// @Summary Cancel a purchased ticket
// @Description Cancel a ticket with automatic refund request. Only eligible tickets can be cancelled based on standard criteria:
// @Description - Event hasn't started (must be >24 hours away)
// @Description - Ticket hasn't been used/checked-in
// @Description - Ticket is not already cancelled/refunded
// @Description - Purchase was made >1 hour ago
// @Tags User Tickets
// @Accept json
// @Produce json
// @Param id path string true "Ticket ID"
// @Param request body models.CancelTicketRequest true "Cancellation reason"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response "Ticket not eligible for cancellation"
// @Failure 401 {object} utils.Response "User not authenticated"
// @Failure 403 {object} utils.Response "User does not own this ticket"
// @Failure 404 {object} utils.Response "Ticket not found"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/user/tickets/{id}/cancel [post]
func (h *TicketHandler) UserCancelTicket(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewUnauthorizedError("User not authenticated."))
		return
	}

	ticketIDStr := c.Param("id")
	ticketID, err := uuid.Parse(ticketIDStr)
	if err != nil {
		utils.HandleError(c, utils.NewBusinessLogicError("Invalid ticket ID format"))
		return
	}

	var req struct {
		Reason string `json:"reason" binding:"required,max=500"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request", err)
		return
	}

	userIDValue := userID.(uuid.UUID)

	// Get ticket and verify ownership
	ticket, err := h.ticketService.GetTicketByID(ticketID)
	if err != nil {
		utils.HandleError(c, utils.NewNotFoundError("Ticket not found"))
		return
	}

	// Verify ticket belongs to the user
	if ticket.UserID == nil || *ticket.UserID != userIDValue {
		utils.HandleError(c, utils.NewForbiddenError("You do not own this ticket"))
		return
	}

	// Check cancellation eligibility
	eligible, eligibilityReason, err := h.ticketService.CheckRefundEligibility([]uuid.UUID{ticketID})
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	if !eligible {
		utils.HandleError(c, utils.NewBusinessLogicError(eligibilityReason))
		return
	}

	// Mark ticket as cancelled and create refund request
	cancellationResult, err := h.ticketService.CancelTicketWithRefund(ticketID, userIDValue, req.Reason)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"ticket_id":         ticket.ID,
		"ticket_number":     ticket.TicketNumber,
		"refund_status":     cancellationResult["refund_status"],
		"refund_amount":     ticket.TotalAmount,
		"currency":          ticket.Event.Currency,
		"cancellation_date": time.Now(),
		"message":           "Ticket cancelled successfully. Refund will be processed within 3-5 business days.",
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket cancelled and refund requested", response)
}

// AdminProcessCheckoutSession godoc
// @Summary Manually process a checkout session (admin only)
// @Description Manually activate tickets and record transaction for a checkout session that failed to process automatically
// @Tags Admin Tickets
// @Accept json
// @Produce json
// @Param request body models.AdminProcessCheckoutSessionRequest true "Checkout token payload"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/tickets/process-checkout [post]
func (h *TicketHandler) AdminProcessCheckoutSession(c *gin.Context) {
	var req struct {
		CheckoutToken string `json:"checkout_token" binding:"required"`
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		utils.HandleError(c, utils.NewInternalServerError("Failed to read request body", err))
		return
	}

	rawBody := strings.TrimSpace(string(body))
	if rawBody == "" {
		utils.ValidationErrorResponse(c, "Invalid request body", fmt.Errorf("checkout_token is required"))
		return
	}

	if err := json.Unmarshal(body, &req); err != nil || strings.TrimSpace(req.CheckoutToken) == "" {
		var token string
		if err := json.Unmarshal(body, &token); err == nil && strings.TrimSpace(token) != "" {
			req.CheckoutToken = strings.TrimSpace(token)
		} else {
			req.CheckoutToken = strings.TrimSpace(strings.Trim(rawBody, `"`))
		}
	}

	if req.CheckoutToken == "" {
		utils.ValidationErrorResponse(c, "Invalid request body", fmt.Errorf("checkout_token is required"))
		return
	}

	adminID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	err = h.ticketService.AdminProcessCheckoutSession(req.CheckoutToken, adminID.(uuid.UUID))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Checkout session processed successfully", nil)
}

// AdminGetCheckoutSessions godoc
// @Summary Get all checkout sessions (admin only)
// @Description Get paginated list of checkout sessions with filters
// @Tags Admin Tickets
// @Produce json
// @Param status query string false "Filter by status (pending, completed, failed, expired)"
// @Param payment_gateway query string false "Filter by payment gateway"
// @Param event_id query string false "Filter by event ID"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(20)
// @Param sort_by query string false "Sort by field (created_at, status, payment_gateway, total_amount, expires_at)" default(created_at)
// @Param sort_order query string false "Sort order (asc, desc)" default(desc)
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/tickets/checkout-sessions [get]
func (h *TicketHandler) AdminGetCheckoutSessions(c *gin.Context) {
	// Parse query parameters
	status := c.Query("status")
	paymentGateway := c.Query("payment_gateway")
	var eventID *uuid.UUID
	if eventIDStr := c.Query("event_id"); eventIDStr != "" {
		if parsedID, err := uuid.Parse(eventIDStr); err == nil {
			eventID = &parsedID
		}
	}

	pagination := utils.GetPaginationParams(c, 20)
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	// Validate sort parameters using centralized utility
	sortBy, sortOrder = utils.ValidateSortForCheckoutSessions(sortBy, sortOrder)

	sessions, total, err := h.ticketService.GetCheckoutSessions(status, paymentGateway, eventID, pagination.Page, pagination.Limit, sortBy, sortOrder)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"sessions":   sessions,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Checkout sessions retrieved successfully", response)
}
