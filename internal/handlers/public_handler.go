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
	db                          *gorm.DB
	ticketService               *services.TicketService
	unifiedPurchaseOrchestrator *services.UnifiedPurchaseOrchestrator
	config                      *config.Config
}

func NewPublicHandler(
	ticketService *services.TicketService,
	unifiedOrchestrator *services.UnifiedPurchaseOrchestrator,
	cfg *config.Config,
) *PublicHandler {
	return &PublicHandler{
		db:                          database.GetDB(),
		ticketService:               ticketService,
		unifiedPurchaseOrchestrator: unifiedOrchestrator,
		config:                      cfg,
	}
}

// getBaseURL returns the base URL for the application from config
func (h *PublicHandler) getBaseURL() string {
	if h.config != nil && h.config.URLs.FrontendBaseURL != "" {
		return h.config.URLs.FrontendBaseURL
	}
	return "https://user.timroticket.com" // fallback
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

	// Get events with pagination - featured events first, then by ascending order (soonest first)
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

	// Get events with pagination - featured events first, then by ascending order (soonest first)
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

	// Unified purchase flow for BOTH guests and logged-in users via centralized orchestrator
	unifiedReq := &services.UnifiedPurchaseRequest{
		Email:          req.Email,
		FirstName:      req.FirstName,
		LastName:       req.LastName,
		Phone:          req.Phone,
		CountryCode:    req.CountryCode,
		EventID:        req.EventID,
		Tiers:          req.Tiers,
		PaymentGateway: req.PaymentGateway,
	}

	// Process via unified orchestrator
	unifiedResp, err := h.unifiedPurchaseOrchestrator.ProcessUnifiedPurchase(c.Request.Context(), unifiedReq)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Handle immediate completion for cash payments
	if unifiedResp.ImmediateCompletion {
		utils.SuccessResponse(c, http.StatusCreated,
			fmt.Sprintf("Successfully purchased %d tickets! Confirmation email sent to: %s", unifiedResp.TicketCount, unifiedResp.Email),
			nil)
		return
	}

	// For gateway payments, return checkout data for frontend to complete payment
	utils.SuccessResponse(c, http.StatusCreated, "Payment initiated successfully via centralized system. Please complete payment using the provided gateway data.", unifiedResp)
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
// @Summary Handle payment gateway success callback (GET for testing, POST for production)
// @Description Acknowledge successful Stripe payment. IMPORTANT: Actual ticket creation happens in webhook handlers only.
//
//	GET: For testing/debugging - just need checkout_token
//	POST: For production - can include optional payment metadata
//
//	This endpoint DOES NOT create tickets - it just acknowledges the payment and returns status.
//	Webhooks are the source of truth for all ticket creation.
//
// @Tags Public
// @Accept json
// @Produce json
// @Param checkout_token query string true "Checkout token"
// @Param request body models.PaymentCallbackRequest false "Payment callback data (optional)"
// @Success 200 {object} utils.Response{data=map[string]interface{}} "Payment acknowledged. Check webhook status for tickets."
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/payment/success [get]
// @Router /api/v1/public/payment/success [post]
func (h *PublicHandler) PaymentSuccessCallback(c *gin.Context) {
	checkoutToken := c.Query("checkout_token")
	if checkoutToken == "" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// For POST requests, try to bind JSON body (but don't fail if missing)
	// For GET requests, skip JSON parsing
	if c.Request.Method == http.MethodPost {
		var req models.PaymentCallbackRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			// Log if there's a body, but don't fail - JSON is optional
			log.Printf("[PAYMENT_SUCCESS_CALLBACK] Note: JSON body error (optional): %v", err)
		}
	}

	// ============================================
	// ARCHITECTURE: Browser Callback is Lightweight
	// ============================================
	// IMPORTANT: This is a browser callback route, NOT a webhook handler.
	// - We do NOT create tickets here
	// - We do NOT process payments here
	// - We only ACKNOWLEDGE the callback
	//
	// REASON: Webhooks are the SOURCE OF TRUTH for payment processing.
	// The webhook (payment_intent.succeeded) will:
	// 1. Confirm the reservation
	// 2. Create tickets
	// 3. Update inventory
	// 4. Mark transaction as completed
	//
	// Browser callback just comes AFTER checkout redirect, it's not reliable
	// for critical operations. Webhooks are guaranteed to arrive from Stripe
	// backend and processed reliably by asynq workers.
	// ============================================

	// Step 1: Verify checkout session exists
	var checkoutSession models.CheckoutSession
	if err := h.db.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
		log.Printf("[PAYMENT_SUCCESS_CALLBACK] Checkout session not found: %s", checkoutToken)
		utils.HandleError(c, utils.NewInternalServerError("Checkout session not found", nil))
		return
	}

	log.Printf("[PAYMENT_SUCCESS_CALLBACK] ✓ Checkout session acknowledged: token=%s, status=%s", checkoutToken, checkoutSession.Status)

	// Step 2: Return immediate response
	// The webhook (payment_intent.succeeded) will handle ticket creation asynchronously
	// Frontend should poll /api/v1/public/checkout/:checkout_token to check when tickets are ready
	response := map[string]interface{}{
		"success":        true,
		"message":        "Payment received. Tickets will be created shortly.",
		"status":         "processing",
		"checkout_token": checkoutToken,
		"poll_endpoint":  fmt.Sprintf("/api/v1/public/checkout/%s", checkoutToken),
		"poll_interval":  2000,  // milliseconds - frontend should poll every 2 seconds
		"max_wait_time":  30000, // milliseconds - give webhook up to 30 seconds to process
		"note":           "Tickets are created by the payment webhook (guaranteed to arrive from Stripe). This callback just acknowledges receipt.",
	}

	log.Printf("[PAYMENT_SUCCESS_CALLBACK] ✓ Returning processing status, webhook will create tickets. Token: %s", checkoutToken)
	utils.SuccessResponse(c, http.StatusOK, "Payment acknowledged. Tickets processing in background.", response)
}

// GetCheckoutSession godoc
// @Summary Get checkout session and ticket status (for frontend polling)
// @Description Poll this endpoint to check when tickets have been created.
//
//	IMPORTANT: This endpoint is used by frontend to poll while waiting
//	for the webhook to process and create tickets.
//
// @Tags Public
// @Accept json
// @Produce json
// @Param checkout_token path string true "Checkout token"
// @Success 200 {object} utils.Response{data=map[string]interface{}} "Checkout session status"
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/checkout/{checkout_token} [get]
func (h *PublicHandler) GetCheckoutSession(c *gin.Context) {
	checkoutToken := c.Param("checkout_token")
	if checkoutToken == "" {
		utils.HandleError(c, utils.NewInternalServerError("Checkout token required", nil))
		return
	}

	// Load checkout session
	var checkoutSession models.CheckoutSession
	if err := h.db.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
		utils.HandleError(c, utils.NewInternalServerError("Checkout session not found", nil))
		return
	}

	// Check if checkout session has expired and update status if needed
	if checkoutSession.ExpiresAt.Before(time.Now()) && (checkoutSession.Status == "pending" || checkoutSession.Status == "processing") {
		checkoutSession.Status = "expired"
		if err := h.db.Save(&checkoutSession).Error; err != nil {
			log.Printf("[CHECKOUT_EXPIRED] Warning: Failed to update expired checkout session %s: %v", checkoutToken, err)
		}
	}

	// Initialize consistent response structure
	response := map[string]interface{}{
		"success": false,
		"message": "Checkout session is still processing. Please wait and poll again shortly.",
		"status":  checkoutSession.Status,
	}

	// Set message based on status
	switch checkoutSession.Status {
	case "completed":
		response["success"] = true
		response["message"] = "Tickets generated successfully"
	case "failed":
		response["message"] = "Payment failed"
	case "expired":
		response["message"] = "Checkout session has expired"
	case "pending":
		response["message"] = "Payment pending"
	case "processing":
		response["message"] = "Payment processing in progress"
	default:
		response["message"] = "Unknown status"
	}

	// Check if complete response is available from webhook processing (only for completed status)
	if checkoutSession.Status == "completed" && checkoutSession.GatewayData != nil {
		if completeResponse, ok := checkoutSession.GatewayData["complete_response"]; ok && completeResponse != nil {
			if responseMap, ok := completeResponse.(map[string]interface{}); ok {
				log.Printf("[CHECKOUT_DEBUG] Returning complete response directly for completed checkout %s", checkoutToken)
				c.JSON(http.StatusOK, responseMap)
				return
			}
		}
	}

	// Return flat response structure: {success, message, status, ticket}
	// ticket field only appears when status === "completed" AND has valid data
	log.Printf("[CHECKOUT_RESPONSE] Final response: %+v", response)
	c.JSON(http.StatusOK, response)
}

// ReleaseCheckoutSession godoc
// @Summary Release/cancel checkout session and free reserved tickets
// @Description Call this when user navigates away from payment page to immediately release reserved tickets
// @Tags Public
// @Accept json
// @Produce json
// @Param checkout_token path string true "Checkout token"
// @Success 200 {object} utils.Response "Checkout session cancelled and reservations released"
// @Failure 400 {object} utils.Response "Invalid checkout token or already completed"
// @Failure 404 {object} utils.Response "Checkout session not found"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/public/checkout/{checkout_token} [delete]
func (h *PublicHandler) ReleaseCheckoutSession(c *gin.Context) {
	checkoutToken := c.Param("checkout_token")
	if checkoutToken == "" {
		utils.HandleError(c, utils.NewValidationError("Checkout token is required", nil))
		return
	}

	// Release the checkout session and reserved tickets
	if err := h.ticketService.ReleaseCheckoutSessionReservations(checkoutToken); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Checkout session cancelled and tickets released", map[string]interface{}{
		"checkout_token": checkoutToken,
		"status":         "cancelled",
	})
}

// SSEPaymentUpdates godoc
// @Summary Subscribe to real-time payment updates via Server-Sent Events (SSE)
// @Description Open a persistent SSE connection to receive real-time payment updates.
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

	// ============================================
	// ARCHITECTURE: Browser Callback is Lightweight
	// ============================================
	// IMPORTANT: This is a browser callback route, NOT a webhook handler.
	// - We do NOT release reservations here
	// - We do NOT mark tickets as cancelled here
	// - We only ACKNOWLEDGE the failure callback
	//
	// The webhook (payment_intent.payment_failed) will:
	// 1. Find the reservation
	// 2. Mark it as failed/expired
	// 3. Release reserved seats back to available
	// 4. Notify the user
	// ============================================

	// Verify checkout session exists
	var checkoutSession models.CheckoutSession
	if err := h.db.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
		log.Printf("[PAYMENT_FAILURE_CALLBACK] Checkout session not found: %s", checkoutToken)
		utils.HandleError(c, utils.NewInternalServerError("Checkout session not found", nil))
		return
	}

	log.Printf("[PAYMENT_FAILURE_CALLBACK] ✓ Payment failure acknowledged: token=%s", checkoutToken)

	// Return immediate response - webhook will handle the actual failure processing
	response := map[string]interface{}{
		"success":                  true,
		"message":                  "Payment failure recorded. Reservation will be released.",
		"status":                   "failed",
		"checkout_token":           checkoutToken,
		"note":                     "The payment_intent.payment_failed webhook will release your reservation and free up tickets.",
		"retry_checkout_available": true,
		"retry_url":                fmt.Sprintf("%s/checkout", h.getBaseURL()),
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment failure acknowledged", response)
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

	// Ensure event is loaded
	if tickets[0].Event == nil {
		utils.HandleError(c, utils.NewInternalServerError("Event information not available.", nil))
		return
	}

	// Check if the event has ended
	if !tickets[0].Event.EndDate.IsZero() && tickets[0].Event.EndDate.Before(time.Now()) {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get currency from the first ticket's tier
	currency := "USD" // default
	if tickets[0].Event.Tiers != nil && len(tickets[0].Event.Tiers) > 0 {
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
			if tickets[0].Event.Tiers != nil {
				for _, tier := range tickets[0].Event.Tiers {
					if tier.ID == ticket.TierID {
						tierName = tier.TierName
						price = tier.Price
						break
					}
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
		utils.HandleError(c, utils.NewValidationError("Token parameter is required.", nil))
		return
	}

	// Validate the JWT token
	jwtService := utils.NewJWTService(&h.config.JWT)
	claims, err := jwtService.ValidateTicketAccessToken(token)
	if err != nil {
		// Return the specific error from JWT validation (including expired token messages)
		utils.HandleError(c, err)
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
		utils.HandleError(c, utils.NewInternalServerError("Invalid token claims.", nil))
		return
	}

	// Get tickets from the last 24 hours
	since := time.Now().Add(-24 * time.Hour)
	if err := query.Where("created_at > ? AND status = ?", since, "active").Count(&ticketCount).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	if ticketCount == 0 {
		utils.HandleError(c, utils.NewNotFoundError("No active tickets found for this token."))
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
// @Param status query string false "Filter by ticket status (active, pending_refund, used, cancelled, refunded, expired)" enum(active,pending_refund,used,cancelled,refunded,expired)
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
