package handlers

import (
	"fmt"
	"log"
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
	"gorm.io/gorm"
)

type PublicHandler struct {
	db            *gorm.DB
	ticketService *services.TicketService
	config        *config.Config
}

func NewPublicHandler(ticketService *services.TicketService, cfg *config.Config) *PublicHandler {
	return &PublicHandler{
		db:            database.GetDB(),
		ticketService: ticketService,
		config:        cfg,
	}
}

// getBaseURL returns the base URL for the application from config
func (h *PublicHandler) getBaseURL() string {
	if h.config != nil && h.config.URLs.FrontendBaseURL != "" {
		return h.config.URLs.FrontendBaseURL
	}
	return "https://user.timroticket.com"
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
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
		utils.HandleError(c, err)
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
		utils.HandleError(c, err)
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
// @Success 200 {object} utils.Response{data=[]models.EventPublicSummaryResponse} "Featured events"
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

	// Optimized query: Select only necessary columns first, then load relations
	if err := h.db.Select("id, title, banner_image, category, start_date, end_date, status, sales_status, is_featured, venue_name, organizer_id, created_at, updated_at").
		Where("is_featured = ? AND status IN (?) AND start_date > ?", true, []string{"on_sale", "hold", "live"}, utils.Now()).
		Order("created_at DESC").
		Limit(limit).
		Find(&events).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Load relations only if events found
	if len(events) > 0 {
		var eventIDs []uuid.UUID
		for _, event := range events {
			eventIDs = append(eventIDs, event.ID)
		}
		// Load Organizers and Onboarding data in bulk
		h.db.Preload("OrganizerOnboarding").Where("id IN (SELECT DISTINCT organizer_id FROM events WHERE id IN (?))", eventIDs).Find(&[]models.User{})
		h.db.Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Tiers").Where("id IN (?)", eventIDs).Find(&events)
	}

	// Convert events to public summary response format
	var publicEvents []models.EventPublicSummaryResponse
	for _, event := range events {
		publicEvents = append(publicEvents, event.ToPublicSummaryResponse())
	}

	utils.SuccessResponse(c, http.StatusOK, "Featured events retrieved successfully", publicEvents)
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
	pagination := utils.GetPaginationParams(c, 10)
	offset := (pagination.Page - 1) * pagination.Limit

	query := h.db.Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Tiers").
		Where("status IN (?, ?) AND start_date > ?", "scheduled", "on_sale", utils.Now())

	// Filter by category if provided
	if category := c.Query("category"); category != "" {
		query = query.Where("category = ?", category)
	}

	var events []models.Event
	var total int64

	// Get total count
	if err := query.Model(&models.Event{}).Count(&total).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get events with pagination - featured events first, then by ascending date (soonest first)
	if err := query.Order("is_featured DESC, start_date ASC").
		Offset(offset).
		Limit(pagination.Limit).
		Find(&events).Error; err != nil {
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
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	pagination := utils.GetPaginationParams(c, 10)
	offset := (pagination.Page - 1) * pagination.Limit

	var events []models.Event
	var total int64

	query := h.db.Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Tiers").
		Where("status IN (?) AND start_date > ? AND category = ?", []string{"on_sale", "hold", "live"}, utils.Now(), category)

	// Get total count
	if err := query.Model(&models.Event{}).Count(&total).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get events with pagination - featured events first, then by descending order
	if err := query.Order("is_featured DESC, start_date DESC").
		Offset(offset).
		Limit(pagination.Limit).
		Find(&events).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Convert events to public summary response format
	var publicEvents []models.EventPublicSummaryResponse
	for _, event := range events {
		publicEvents = append(publicEvents, event.ToPublicSummaryResponse())
	}

	response := map[string]interface{}{
		"category":   category,
		"events":     publicEvents,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
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
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	pagination := utils.GetPaginationParams(c, 10)
	offset := (pagination.Page - 1) * pagination.Limit

	query := h.db.Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Tiers").
		Where("status IN (?) AND start_date > ? AND (title ILIKE ? OR description ILIKE ?)",
			[]string{"on_sale", "hold", "live"}, utils.Now(), "%"+searchQuery+"%", "%"+searchQuery+"%")

	// Filter by category if provided
	if category := c.Query("category"); category != "" {
		query = query.Where("? = ANY(category)", category)
	}

	var events []models.Event
	var total int64

	// Get total count
	if err := query.Model(&models.Event{}).Count(&total).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get events with pagination - featured events first, then by descending order
	if err := query.Order("is_featured DESC, start_date DESC").
		Offset(offset).
		Limit(pagination.Limit).
		Find(&events).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Convert events to public summary response format
	var publicEvents []models.EventPublicSummaryResponse
	for _, event := range events {
		publicEvents = append(publicEvents, event.ToPublicSummaryResponse())
	}

	response := map[string]interface{}{
		"query":      searchQuery,
		"events":     publicEvents,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Search results retrieved successfully", response)
}

// validateEventPurchaseEligibility checks if an event allows ticket purchases for multiple tiers
func (h *PublicHandler) validateEventPurchaseEligibility(eventID uuid.UUID, tierSelections []models.TicketTierSelection) error {
	return utils.ValidateEventPurchaseEligibilityForTiers(h.db, eventID.String(), tierSelections)
}

// PurchaseTicketAsGuest godoc
// @Summary Purchase ticket as guest
// @Description Create multiple individual ticket purchases for a guest with payment gateway integration. Email, event_id, tier_id, payment_gateway, and quantity are required. Guests can purchase up to 6 tickets. Other fields are optional with sensible defaults.
// @Tags Public
// @Accept json
// @Produce json
// @Param request body models.GuestPurchaseRequest true "Guest purchase details (email, event_id, tier_id, payment_gateway, quantity required)"
// @Success 201 {object} utils.Response{data=map[string]interface{}} "Purchase created successfully"
// @Failure 400 {object} utils.Response "Invalid request data or unauthorized cash payment"
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/tickets/guest-purchase [post]
func (h *PublicHandler) PurchaseTicketAsGuest(c *gin.Context) {
	var req models.GuestPurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate event purchase eligibility for all tiers
	if err := h.validateEventPurchaseEligibility(req.EventID, req.Tiers); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Set default values if not provided
	if req.FirstName == "" {
		req.FirstName = "Guest"
	}
	if req.LastName == "" {
		req.LastName = "User"
	}

	// For cash payment, validate that the email is in the allowed list (if configured)
	if req.PaymentGateway == models.PaymentGatewayCash {
		cfg, err := config.Load()
		if err != nil {
			utils.HandleError(c, err)
			return
		}

		// Only check allowed emails if the list is configured
		if len(cfg.Payment.CashAllowedEmails) > 0 {
			// Check if the email is in the allowed list for cash payments
			allowed := false
			for _, allowedEmail := range cfg.Payment.CashAllowedEmails {
				if strings.TrimSpace(allowedEmail) == req.Email {
					allowed = true
					break
				}
			}

			if !allowed {
				utils.HandleError(c, utils.NewBusinessLogicError("Cash payments are not available for this email address. Please contact support or use a different payment method."))
				return
			}
		}
	}

	// Unified purchase flow for both cash and gateway payments
	tickets, _, checkoutSession, err := h.ticketService.UnifiedGuestPurchase(&req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Handle immediate completion for cash payments
	if req.PaymentGateway == models.PaymentGatewayCash {
		utils.SuccessResponse(c, http.StatusCreated, fmt.Sprintf("Successfully purchased %d tickets! Confirmation email sent to: %s", len(tickets), req.Email), nil)
		return
	}

	// For gateway payments, return checkout session for frontend to complete payment
	if checkoutSession == nil {
		utils.HandleError(c, utils.NewInternalServerError("Failed to initialize payment session. Please try again.", nil))
		return
	}
	utils.SuccessResponse(c, http.StatusCreated, "Payment initiated successfully. Please complete payment using the provided gateway data.", checkoutSession.ToResponse())
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
		utils.HandleError(c, err)
		return
	}

	ticket, err := h.ticketService.VerifyGuestEmail(req.Token)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Email verified successfully", ticket)
}

// PaymentSuccessCallback godoc
// @Summary Handle payment gateway success callback
// @Description Process successful payment from gateway and activate tickets
// @Tags Public
// @Accept json
// @Produce json
// @Param checkout_token query string true "Checkout token"
// @Param request body models.PaymentCallbackRequest true "Payment callback data"
// @Success 200 {object} utils.Response{data=map[string]interface{}} "Payment processed successfully with ticket view token"
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/payment/success [post]
func (h *PublicHandler) PaymentSuccessCallback(c *gin.Context) {
	checkoutToken := c.Query("checkout_token")
	if checkoutToken == "" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	var req models.PaymentCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Override checkout token from URL param (more secure)
	req.CheckoutToken = checkoutToken

	// First check if checkout session exists and its status
	var checkoutSession models.CheckoutSession
	if err := h.db.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
		utils.HandleError(c, utils.NewInternalServerError("Checkout session not found", nil))
		return
	}

	// If checkout session is not completed, try to process the payment
	// This handles cases where webhook hasn't processed yet or failed
	if checkoutSession.Status != "completed" {
		log.Printf("[PAYMENT_SUCCESS] Processing payment for checkout token: %s, current status: %s", checkoutToken, checkoutSession.Status)
		// Process successful payment
		err := h.ticketService.ProcessPaymentSuccess(&req)
		if err != nil {
			// If payment is already processed, continue (webhook might have processed it)
			if !strings.Contains(err.Error(), "Payment already processed") {
				log.Printf("[PAYMENT_SUCCESS] Failed to process payment for checkout token: %s, error: %v", checkoutToken, err)
				utils.HandleError(c, err)
				return
			}
			// Continue if already processed
			log.Printf("[PAYMENT_SUCCESS] Payment already processed by webhook for checkout token: %s", checkoutToken)
		} else {
			log.Printf("[PAYMENT_SUCCESS] Successfully processed payment for checkout token: %s", checkoutToken)
		}
	} else {
		log.Printf("[PAYMENT_SUCCESS] Checkout session already completed for token: %s", checkoutToken)
	}

	// Re-fetch checkout session after potential processing
	if err := h.db.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
		utils.HandleError(c, utils.NewInternalServerError("Failed to find checkout session", nil))
		return
	}

	// Find tickets associated with this checkout session
	var tickets []models.Ticket
	query := h.db.Preload("Event")

	// Handle unified checkout session format (new) - check for ticket_ids in GatewayData
	if checkoutSession.GatewayData != nil {
		if ticketIDsData, ok := checkoutSession.GatewayData["ticket_ids"]; ok && ticketIDsData != nil {
			// Try to convert to []uuid.UUID
			var ticketIDs []uuid.UUID
			switch v := ticketIDsData.(type) {
			case []uuid.UUID:
				ticketIDs = v
			case []interface{}:
				// Handle case where it's stored as []interface{}
				for _, id := range v {
					if idStr, ok := id.(string); ok {
						if parsedID, err := uuid.Parse(idStr); err == nil {
							ticketIDs = append(ticketIDs, parsedID)
						}
					}
				}
			}

			if len(ticketIDs) > 0 {
				// New format: multiple tickets per checkout session
				query = query.Where("id IN ?", ticketIDs)
			} else {
				utils.HandleError(c, utils.NewInternalServerError("Invalid ticket_ids in checkout session", nil))
				return
			}
		} else {
			// Fallback: old format (single ticket per checkout session)
			if checkoutSession.TicketID == uuid.Nil {
				utils.HandleError(c, utils.NewInternalServerError("Invalid checkout session: missing ticket reference", nil))
				return
			}

			// First verify the ticket exists and get its event_id
			var ticket models.Ticket
			if err := h.db.Select("event_id").Where("id = ?", checkoutSession.TicketID).First(&ticket).Error; err != nil {
				utils.HandleError(c, utils.NewInternalServerError("Invalid ticket reference in checkout session", nil))
				return
			}

			if checkoutSession.GuestUserID != nil {
				// Guest purchase
				query = query.Where("guest_user_id = ? AND event_id = ?",
					checkoutSession.GuestUserID,
					ticket.EventID)
			} else if checkoutSession.UserID != nil {
				// Logged-in user purchase
				query = query.Where("user_id = ? AND event_id = ?",
					checkoutSession.UserID,
					ticket.EventID)
			} else {
				utils.HandleError(c, utils.NewInternalServerError("Invalid checkout session", nil))
				return
			}
		}
	} else {
		// Fallback: old format (single ticket per checkout session)
		if checkoutSession.TicketID == uuid.Nil {
			utils.HandleError(c, utils.NewInternalServerError("Invalid checkout session: missing ticket reference", nil))
			return
		}

		// First verify the ticket exists and get its event_id
		var ticket models.Ticket
		if err := h.db.Select("event_id").Where("id = ?", checkoutSession.TicketID).First(&ticket).Error; err != nil {
			utils.HandleError(c, utils.NewInternalServerError("Invalid ticket reference in checkout session", nil))
			return
		}

		if checkoutSession.GuestUserID != nil {
			// Guest purchase
			query = query.Where("guest_user_id = ? AND event_id = ?",
				checkoutSession.GuestUserID,
				ticket.EventID)
		} else if checkoutSession.UserID != nil {
			// Logged-in user purchase
			query = query.Where("user_id = ? AND event_id = ?",
				checkoutSession.UserID,
				ticket.EventID)
		} else {
			utils.HandleError(c, utils.NewInternalServerError("Invalid checkout session", nil))
			return
		}
	}

	if err := query.Find(&tickets).Error; err != nil {
		utils.HandleError(c, utils.NewInternalServerError("Failed to find tickets", nil))
		return
	}

	log.Printf("[PAYMENT_SUCCESS] Found %d tickets for checkout token: %s", len(tickets), checkoutToken)

	if len(tickets) == 0 {
		utils.HandleError(c, utils.NewInternalServerError("No tickets found", nil))
		return
	}

	// Check if tickets are active - if not, this might indicate processing hasn't completed
	activeTickets := 0
	for _, ticket := range tickets {
		log.Printf("[PAYMENT_SUCCESS] Ticket %s status: %s", ticket.ID, ticket.Status)
		if ticket.Status == "active" {
			activeTickets++
		}
	}

	log.Printf("[PAYMENT_SUCCESS] %d/%d tickets are active for checkout token: %s", activeTickets, len(tickets), checkoutToken)

	// If no active tickets and checkout session is completed, there might be an issue
	if activeTickets == 0 && checkoutSession.Status == "completed" {
		utils.HandleError(c, utils.NewInternalServerError("Payment processed but tickets not activated", nil))
		return
	}

	// If tickets are not active but checkout session is pending, return pending status
	if activeTickets == 0 && checkoutSession.Status == "pending" {
		log.Printf("[PAYMENT_SUCCESS] Returning pending status for checkout token: %s (checkout status: %s, active tickets: %d/%d)",
			checkoutToken, checkoutSession.Status, activeTickets, len(tickets))
		response := map[string]interface{}{
			"success":        false,
			"message":        "Payment processing in progress",
			"status":         "pending",
			"checkout_token": checkoutToken,
		}
		utils.SuccessResponse(c, http.StatusOK, "Payment processing in progress", response)
		return
	}

	// Generate JWT token using the first ticket as reference
	if h.config.JWT.Secret == "" {
		utils.HandleError(c, utils.NewInternalServerError("JWT configuration not available", nil))
		return
	}

	var token string
	var err error
	jwtService := utils.NewJWTService(&h.config.JWT)
	token, err = jwtService.GenerateTicketAccessToken(&tickets[0])
	if err != nil {
		utils.HandleError(c, utils.NewInternalServerError("Failed to generate access token", nil))
		return
	}

	// Extract payment information from checkout session
	paymentInfo := map[string]interface{}{
		"amount":          checkoutSession.Amount,
		"currency":        checkoutSession.Currency,
		"payment_gateway": checkoutSession.PaymentGateway,
		"status":          checkoutSession.Status,
	}

	// Get payment_intent_id from PaymentIntent relationship (single source of truth)
	var paymentIntentID string
	if checkoutSession.PaymentIntentID != nil {
		var paymentIntent models.PaymentIntent
		if err := h.db.Where("id = ?", *checkoutSession.PaymentIntentID).First(&paymentIntent).Error; err == nil {
			if paymentIntent.GatewayPaymentID != nil {
				paymentIntentID = *paymentIntent.GatewayPaymentID
			}
		}
	}
	if paymentIntentID != "" {
		paymentInfo["payment_intent_id"] = paymentIntentID
	}

	// Add gateway-specific information if available
	if checkoutSession.GatewayData != nil {
		if paymentMethod, ok := checkoutSession.GatewayData["payment_method"].(string); ok {
			paymentInfo["payment_method"] = paymentMethod
		}
		if amountReceived, ok := checkoutSession.GatewayData["amount_received"].(float64); ok {
			paymentInfo["amount_received"] = amountReceived
		}
		// Add any other gateway data that's safe to expose (skip payment_intent_id - from PaymentIntent relation)
		for k, v := range checkoutSession.GatewayData {
			if k != "client_secret" && k != "api_key" && k != "payment_intent_id" { // Don't expose sensitive data or duplicates
				paymentInfo[k] = v
			}
		}
	}

	// Add transaction ID from tickets
	transactionID := ""
	if len(tickets) > 0 && tickets[0].TransactionID != nil {
		transactionID = tickets[0].TransactionID.String()
	}
	paymentInfo["transaction_id"] = transactionID

	// Return success response with token, redirect URL, and payment info
	ticketViewURL := fmt.Sprintf("%s/tickets/view?token=%s", h.getBaseURL(), token)
	response := map[string]interface{}{
		"success":           true,
		"message":           "Payment processed successfully",
		"ticket_view_token": token,
		"ticket_view_url":   ticketViewURL,
		"ticket_count":      len(tickets),
		"checkout_token":    checkoutToken,
		"payment_info":      paymentInfo, // Payment details for frontend
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment processed successfully", response)
}

// PaymentFailureCallback godoc
// @Summary Handle payment gateway failure callback
// @Description Process failed payment from gateway
// @Tags Public
// @Accept json
// @Produce json
// @Param checkout_token query string true "Checkout token"
// @Param request body models.PaymentCallbackRequest true "Payment callback data"
// @Success 200 {object} utils.Response "Payment failure recorded"
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/payment/failure [post]
func (h *PublicHandler) PaymentFailureCallback(c *gin.Context) {
	checkoutToken := c.Query("checkout_token")
	if checkoutToken == "" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	var req models.PaymentCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Override checkout token from URL param (more secure)
	req.CheckoutToken = checkoutToken

	// Process failed payment
	err := h.ticketService.ProcessPaymentFailure(&req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment failure recorded", nil)
}

// GetCheckoutSession godoc
// @Summary Get checkout session details
// @Description Get checkout session details by token (for frontend polling)
// @Tags Public
// @Accept json
// @Produce json
// @Param checkout_token path string true "Checkout token"
// @Success 200 {object} utils.Response{data=models.CheckoutSessionResponse}
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/checkout/{checkout_token} [get]
func (h *PublicHandler) GetCheckoutSession(c *gin.Context) {
	checkoutToken := c.Param("checkout_token")
	if checkoutToken == "" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	checkoutSession, err := h.ticketService.GetCheckoutSessionByToken(checkoutToken)
	if err != nil {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Checkout session retrieved", checkoutSession.ToResponse())
}

// ViewTicket godoc
// @Summary View ticket with JWT token
// @Description View ticket details using a secure JWT token for frontend rendering
// @Tags Public
// @Accept json
// @Produce json
// @Param token query string true "JWT ticket access token"
// @Success 200 {object} utils.Response{data=models.OrderViewResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/tickets/view [get]
func (h *PublicHandler) ViewTicket(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Validate the JWT token
	jwtService := utils.NewJWTService(&h.config.JWT)
	claims, err := jwtService.ValidateTicketAccessToken(token)
	if err != nil {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get all tickets for this transaction (same transaction ID)
	var tickets []models.Ticket
	query := h.db.Preload("Event").Preload("Event.Tiers").Preload("Event.Organizer").Preload("Event.Organizer.OrganizerOnboarding").Preload("Tier").Preload("Transaction")

	// Only allow access if transaction ID is present in the token
	if claims.TransactionID == nil {
		utils.HandleError(c, utils.NewBusinessLogicError("Invalid ticket access token. Transaction information is required."))
		return
	}

	// Filter by transaction ID only
	query = query.Where("transaction_id = ?", *claims.TransactionID)

	if err := query.Where("status = ?", "active").Find(&tickets).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	log.Printf("ViewTicket: Found %d tickets for transaction %s", len(tickets), claims.TransactionID.String())

	if len(tickets) == 0 {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Check if the event has ended
	if tickets[0].Event != nil && !tickets[0].Event.EndDate.IsZero() && tickets[0].Event.EndDate.Before(time.Now()) {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get currency from the first ticket's tier
	currency := "USD" // default
	if len(tickets[0].Event.Tiers) > 0 {
		currency = tickets[0].Event.Tiers[0].Currency
	}

	// Convert tickets to minimal view responses
	var ticketResponses []models.TicketViewMinimalResponse
	totalAmount := 0.0

	for _, ticket := range tickets {
		// Get tier name and price
		tierName := "General"
		price := ticket.TotalAmount // Each ticket is for 1 person

		// Find the specific tier for this ticket
		if ticket.Tier != nil {
			tierName = ticket.Tier.TierName
			price = ticket.Tier.Price
		} else {
			// Fallback: find tier by ID in event tiers
			for _, tier := range ticket.Event.Tiers {
				if tier.ID == ticket.TierID {
					tierName = tier.TierName
					price = tier.Price
					break
				}
			}
		}

		// Generate secure QR payload for the ticket
		qrData := ""
		if qr, err := h.ticketService.GenerateQRCodeForTicket(ticket.ID); err == nil {
			qrData = qr
		}

		ticketResp := models.TicketViewMinimalResponse{
			ID:           ticket.ID,
			TicketNumber: ticket.TicketNumber,
			TierName:     tierName,
			Price:        price,
			QRData:       qrData,
			CheckedIn:    ticket.CheckInTime != nil,
		}

		ticketResponses = append(ticketResponses, ticketResp)
		totalAmount += ticket.TotalAmount
	}

	// Create minimal event response with organizer
	var organizerResp *models.OrganizerPublicResponse
	if tickets[0].Event.Organizer != nil {
		organizerResp = tickets[0].Event.Organizer.ToOrganizerPublicResponse()
	} else {
		// Fallback: try to load organizer directly from event's organizer_id
		if tickets[0].Event.OrganizerID != uuid.Nil {
			var organizer models.User
			if err := h.db.Preload("OrganizerOnboarding").First(&organizer, tickets[0].Event.OrganizerID).Error; err == nil {
				organizerResp = organizer.ToOrganizerPublicResponse()
			}
		}
	}

	// Load company information
	var companyResp *models.CompanyInfoMinimalResponse
	var companyInfo models.CompanyInfo
	if err := h.db.First(&companyInfo).Error; err == nil {
		companyResp = &models.CompanyInfoMinimalResponse{
			ID:         companyInfo.ID,
			Name:       companyInfo.Name,
			LogoURL:    companyInfo.LogoURL,
			Email:      companyInfo.Email,
			WebsiteURL: companyInfo.WebsiteURL,
		}
	}

	eventResp := &models.EventViewMinimalResponse{
		ID:          tickets[0].Event.ID,
		Title:       tickets[0].Event.Title,
		BannerImage: tickets[0].Event.BannerImage,
		VenueName:   tickets[0].Event.VenueName,
		Address:     tickets[0].Event.Address,
		StartDate:   tickets[0].Event.StartDate,
		Timezone:    tickets[0].Event.Timezone,
		Organizer:   organizerResp,
	}

	// Create minimal order response
	orderID := claims.TransactionID.String() // Use transaction ID from JWT token

	orderResponse := models.OrderViewMinimalResponse{
		OrderID:         orderID,
		Event:           eventResp,
		Tickets:         ticketResponses,
		TotalAmount:     totalAmount,
		Currency:        currency,
		IsGuestPurchase: tickets[0].IsGuestPurchase,
		Company:         companyResp,
	}

	// Add payment information from transaction if available
	if tickets[0].Transaction != nil && tickets[0].Transaction.GatewayData != nil {
		orderResponse.PaymentInfo = tickets[0].Transaction.GatewayData
	}

	utils.SuccessResponse(c, http.StatusOK, "Tickets retrieved successfully", orderResponse)
}

// @Summary Validate ticket access token
// @Description Validate a JWT ticket access token to check if it's valid for viewing tickets
// @Tags Public
// @Accept json
// @Produce json
// @Param token query string true "JWT ticket access token"
// @Success 200 {object} utils.Response{data=map[string]interface{}} "Token is valid"
// @Failure 400 {object} utils.Response "Token is required"
// @Failure 401 {object} utils.Response "Invalid or expired token"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/public/tickets/validate-token [get]
func (h *PublicHandler) ValidateTicketToken(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Validate the JWT token
	jwtService := utils.NewJWTService(&h.config.JWT)
	claims, err := jwtService.ValidateTicketAccessToken(token)
	if err != nil {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Check if tickets exist for this token
	var ticketCount int64
	query := h.db.Model(&models.Ticket{})

	if claims.UserID != nil {
		query = query.Where("user_id = ? AND event_id = ?", *claims.UserID, claims.EventID)
	} else if claims.GuestUserID != nil {
		query = query.Where("guest_user_id = ? AND event_id = ?", *claims.GuestUserID, claims.EventID)
	} else {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get tickets from the last 24 hours
	since := time.Now().Add(-24 * time.Hour)
	if err := query.Where("created_at > ? AND status = ?", since, "active").Count(&ticketCount).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	if ticketCount == 0 {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	response := map[string]interface{}{
		"valid":        true,
		"ticket_count": ticketCount,
		"event_id":     claims.EventID,
		"message":      "Token is valid and tickets are available",
	}

	utils.SuccessResponse(c, http.StatusOK, "Token validated successfully", response)
}

// prepareGuestOrderConfirmationData prepares email data for guest order confirmation
func (h *PublicHandler) prepareGuestOrderConfirmationData(guestUser *models.GuestUser, event *models.Event, tickets []*models.Ticket) (map[string]interface{}, error) {
	if guestUser == nil {
		return nil, utils.NewValidationError("Guest user is required.", nil)
	}
	if event == nil {
		return nil, utils.NewValidationError("Event is required.", nil)
	}

	totalAmount := 0.0

	for _, ticket := range tickets {
		totalAmount += ticket.TotalAmount
	}

	// Generate calendar data
	organizerName := "Event Organizer"
	if event.Organizer != nil {
		if event.Organizer.OrganizerOnboarding != nil && event.Organizer.OrganizerOnboarding.BusinessName != "" {
			organizerName = event.Organizer.OrganizerOnboarding.BusinessName
		} else {
			organizerName = event.Organizer.FirstName + " " + event.Organizer.LastName
		}
	}

	calendarEvent := utils.ICalendarEvent{
		UID:         event.ID.String(),
		Summary:     event.Title,
		Description: utils.FormatEventDescription(event.Title, tickets[0].TicketNumber, "", len(tickets)),
		Location:    fmt.Sprintf("%s, %s", event.VenueName, event.Address),
		StartTime:   event.StartDate,
		EndTime:     event.EndDate,
		Organizer:   organizerName,
		URL:         fmt.Sprintf("%s/events/%s", h.config.URLs.UserBaseURL, event.ID),
	}

	icsContent := utils.GenerateICS(calendarEvent)
	icsDataURL := utils.GenerateAddToCalendarURL(icsContent)
	googleCalURL := utils.GenerateGoogleCalendarURL(calendarEvent)
	calendarFilename := utils.GetCalendarFilename(event.Title)

	// Generate ticket data for email template
	var ticketData []map[string]interface{}
	for _, ticket := range tickets {
		// Generate secure view URL using JWT token
		// Format: {base_url}/tickets/view?token={jwt_token}
		jwtService := utils.NewJWTService(&h.config.JWT)
		token, err := jwtService.GenerateTicketAccessToken(ticket)
		if err != nil {
			log.Printf("Failed to generate JWT token for ticket %s: %v", ticket.ID, err)
			continue
		}

		viewURL := fmt.Sprintf("%s/tickets/view?token=%s", h.config.URLs.UserBaseURL, token)
		ticketData = append(ticketData, map[string]interface{}{
			"ticket_number": ticket.TicketNumber,
			"view_url":      viewURL,
		})
	}

	emailData := map[string]interface{}{
		"guest_name":          guestUser.FirstName + " " + guestUser.LastName,
		"guest_email":         guestUser.Email,
		"event_name":          event.Title,
		"event_date":          event.StartDate.Format("January 2, 2006"),
		"event_time":          event.StartDate.Format("3:04 PM"),
		"venue":               event.VenueName,
		"organizer_name":      organizerName,
		"tickets":             ticketData,
		"total_tickets":       len(tickets),
		"total_amount":        totalAmount,
		"payment_gateway":     string(tickets[0].PaymentGateway),
		"base_url":            h.config.URLs.UserBaseURL,
		"calendar_ics_url":    icsDataURL,
		"google_calendar_url": googleCalURL,
		"calendar_filename":   calendarFilename,
		"year":                time.Now().Year(),
	}

	return emailData, nil
}

// GuestGetTickets godoc
// @Summary Get guest user's purchased tickets
// @Description Get a list of tickets purchased by a guest user with pagination and filtering
// @Tags Public
// @Produce json
// @Param email query string true "Guest email address"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param status query string false "Filter by ticket status (active, used, cancelled, refunded)" enum(active,used,cancelled,refunded)
// @Param event_id query string false "Filter by event ID"
// @Param start_date query string false "Filter tickets purchased after this date (YYYY-MM-DD)"
// @Param end_date query string false "Filter tickets purchased before this date (YYYY-MM-DD)"
// @Param sort query string false "Sort by field with optional '-' prefix for desc (e.g., '-created_at', 'ticket_number')" default(-created_at)
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/guest/tickets [get]
func (h *PublicHandler) GuestGetTickets(c *gin.Context) {
	// Get email from query parameter
	guestEmail := c.Query("email")
	if guestEmail == "" {
		utils.HandleError(c, utils.NewValidationError("Email parameter is required", nil))
		return
	}

	// Basic email validation
	if !strings.Contains(guestEmail, "@") || !strings.Contains(guestEmail, ".") {
		utils.HandleError(c, utils.NewValidationError("Invalid email format", nil))
		return
	}

	// Pagination params
	pagination := utils.GetPaginationParams(c, 10)

	// Filter params
	status := c.Query("status")
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
		"created_at":    true,
		"ticket_number": true,
		"price":         true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, validSortFields, "created_at", "desc")

	tickets, total, err := h.ticketService.GetGuestTickets(guestEmail, pagination.Page, pagination.Limit, status, eventID, sortBy, sortOrder, startDate, endDate)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"tickets":    tickets,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Guest tickets retrieved successfully", response)
}
