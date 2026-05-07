package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/types"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PublicHandler struct {
	db *gorm.DB
	// ticketService               *services.TicketService
	purchaseOrchestrator *services.PurchaseOrchestrator
	config               *config.Config
}

func NewPublicHandler(
	// ticketService *services.TicketService,
	purchaseOrchestrator *services.PurchaseOrchestrator,
	cfg *config.Config,
) *PublicHandler {
	return &PublicHandler{
		db: database.GetDB(),
		// ticketService:               ticketService,
		purchaseOrchestrator: purchaseOrchestrator,
		config:               cfg,
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

	// Step 1: Verify payment intent exists
	var paymentIntent models.PaymentIntent
	if err := h.db.Where("checkout_token = ?", checkoutToken).First(&paymentIntent).Error; err != nil {
		log.Printf("[PAYMENT_SUCCESS_CALLBACK] Payment intent not found: %s", checkoutToken)
		utils.HandleError(c, utils.NewInternalServerError("Payment intent not found", nil))
		return
	}

	log.Printf("[PAYMENT_SUCCESS_CALLBACK] ✓ Payment intent acknowledged: token=%s, status=%s", checkoutToken, paymentIntent.Status)

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

	// Load payment intent
	var paymentIntent models.PaymentIntent
	if err := h.db.Where("checkout_token = ?", checkoutToken).First(&paymentIntent).Error; err != nil {
		utils.HandleError(c, utils.NewInternalServerError("Payment intent not found", nil))
		return
	}

	// Check if payment intent has expired and update status if needed
	if paymentIntent.ExpiresAt != nil && paymentIntent.ExpiresAt.Before(time.Now()) && (paymentIntent.Status == "pending" || paymentIntent.Status == "processing") {
		paymentIntent.Status = "expired"
		if err := h.db.Save(&paymentIntent).Error; err != nil {
			log.Printf("[PAYMENT_EXPIRED] Warning: Failed to update expired payment intent %s: %v", checkoutToken, err)
		}
	}

	// Initialize consistent response structure
	response := map[string]interface{}{
		"success": false,
		"message": "Payment intent is still processing. Please wait and poll again shortly.",
		"status":  paymentIntent.Status,
	}

	// Set message based on status
	switch paymentIntent.Status {
	case "completed":
		response["success"] = true
		response["message"] = "Tickets generated successfully"
	case "failed":
		response["message"] = "Payment failed"
	case "expired":
		response["message"] = "Payment intent has expired"
	case "pending":
		response["message"] = "Payment pending"
	case "processing":
		response["message"] = "Payment processing in progress"
	default:
		response["message"] = "Unknown status"
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

	// Since CheckoutSession is removed and reservations auto-expire,
	// this endpoint now just acknowledges the cancellation
	log.Printf("[PAYMENT_CANCEL] Payment intent cancelled: %s", checkoutToken)

	utils.SuccessResponse(c, http.StatusOK, "Payment intent cancelled", map[string]interface{}{
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

	// Verify payment intent exists
	var paymentIntent models.PaymentIntent
	if err := h.db.Where("checkout_token = ?", checkoutToken).First(&paymentIntent).Error; err != nil {
		log.Printf("[PAYMENT_FAILURE_CALLBACK] Payment intent not found: %s", checkoutToken)
		utils.HandleError(c, utils.NewInternalServerError("Payment intent not found", nil))
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

// PurchaseTickets godoc
// @Summary Purchase tickets (Unified endpoint for both guest and authenticated users)
// @Description Create a payment intent and checkout session for ticket purchase
// @Tags Public
// @Accept json
// @Produce json
// @Param request body models.TicketPurchaseRequest true "Purchase details"
// @Success 200 {object} utils.Response{data=services.CheckoutResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/purchase [post]
func (h *PublicHandler) PurchaseTickets(c *gin.Context) {
	var req models.TicketPurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Validate request
	if req.EventID == uuid.Nil {
		utils.HandleError(c, utils.NewValidationError("event_id is required", nil))
		return
	}
	if len(req.Tiers) == 0 {
		utils.HandleError(c, utils.NewValidationError("at least one tier must be specified", nil))
		return
	}
	if req.Currency == "" {
		utils.HandleError(c, utils.NewValidationError("currency is required", nil))
		return
	}
	if req.PaymentGateway == "" {
		utils.HandleError(c, utils.NewValidationError("payment_gateway is required", nil))
		return
	}
	if req.CustomerEmail == "" {
		utils.HandleError(c, utils.NewValidationError("customer_email is required", nil))
		return
	}

	// Determine actor type and ID
	var actorType models.ActorType
	var actorID uuid.UUID

	// Check if user is authenticated
	userIDInterface, exists := c.Get("userID")
	if exists && userIDInterface != nil {
		// Authenticated user
		if userID, ok := userIDInterface.(uuid.UUID); ok {
			actorType = models.ActorUser
			actorID = userID
		} else {
			utils.HandleError(c, utils.NewValidationError("invalid user authentication", nil))
			return
		}
	} else {
		// Guest user - find or create by email
		var guestUser models.GuestUser
		err := h.db.Where("email = ?", req.CustomerEmail).First(&guestUser).Error
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				// Create new guest user
				guestUser = models.GuestUser{
					ID:        uuid.New(),
					Email:     req.CustomerEmail,
					FirstName: "Guest",
					LastName:  "User",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
				if err := h.db.Create(&guestUser).Error; err != nil {
					utils.HandleError(c, utils.NewInternalServerError("Failed to create guest user", err))
					return
				}
			} else {
				utils.HandleError(c, utils.NewInternalServerError("Failed to find guest user", err))
				return
			}
		}
		actorType = models.ActorGuest
		actorID = guestUser.ID
	}

	// Convert tiers to the format expected by orchestrator
	tiers := make([]types.TierSelection, len(req.Tiers))
	for i, tier := range req.Tiers {
		tiers[i] = types.TierSelection{
			TierID:   tier.TierID,
			Quantity: tier.Quantity,
		}
	}

	// Create checkout request for orchestrator
	checkoutReq := &services.CheckoutRequest{
		ActorID:        actorID,
		ActorType:      actorType,
		EventID:        req.EventID,
		Tiers:          tiers,
		Currency:       req.Currency,
		PaymentGateway: models.PaymentGateway(req.PaymentGateway),
		CustomerEmail:  req.CustomerEmail,
		Timezone:       req.Timezone,
	}

	// Set idempotency key if provided
	if req.IdempotencyKey != "" {
		checkoutReq.IdempotencyKey = req.IdempotencyKey
	}

	// Call the unified purchase orchestrator
	response, err := h.purchaseOrchestrator.Checkout(c.Request.Context(), checkoutReq)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Checkout session created successfully", response)
}
