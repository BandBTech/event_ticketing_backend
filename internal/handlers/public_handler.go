package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/helpers"
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
// @Router /api/v1/public/company-info [get]
func (h *PublicHandler) GetCompanyInfo(c *gin.Context) {
	var companyInfo models.CompanyInfo

	if err := h.db.First(&companyInfo).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Return empty company info instead of error when no record exists
			utils.SuccessResponse(c, http.StatusOK, "Company information retrieved successfully", models.CompanyInfoResponse{})
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
	if err := h.db.Select("id, title, banner_image, category, event_type, country, currency, start_date, end_date, status, sales_status, is_featured, venue_name, organizer_id, created_at, updated_at").
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
		utils.HandleError(c, utils.NewBusinessLogicError("checkout token required"))
		return
	}

	// 1. Get payment intent
	var paymentIntent models.PaymentIntent

	if err := h.db.
		Where("checkout_token = ?", checkoutToken).
		First(&paymentIntent).Error; err != nil {

		utils.HandleError(c, utils.NewNotFoundError("payment intent not found"))
		return
	}

	// 2. Expiry handling
	if paymentIntent.ExpiresAt != nil &&
		paymentIntent.ExpiresAt.Before(time.Now()) &&
		(paymentIntent.Status == models.PaymentIntentRequiresPaymentMethod ||
			paymentIntent.Status == models.PaymentIntentRequiresConfirmation ||
			paymentIntent.Status == models.PaymentIntentProcessing) {

		paymentIntent.Status = models.PaymentIntentExpired
		_ = h.db.Save(&paymentIntent).Error
	}

	// 3. Normalize status
	status := "pending"

	switch paymentIntent.Status {

	case models.PaymentIntentSucceeded:
		status = "completed"

	case models.PaymentIntentProcessing,
		models.PaymentIntentRequiresConfirmation:
		status = "processing"

	case models.PaymentIntentRequiresPaymentMethod:
		status = "pending"

	case models.PaymentIntentExpired:
		status = "expired"

	case models.PaymentIntentCanceled:
		status = "failed"

	default:
		status = "unknown"
	}

	// 4. Base response
	response := map[string]interface{}{
		"success": status == "completed",
		"status":  status,
		"message": helpers.GetPaymentIntentStatusMessage(paymentIntent.Status),
	}

	// 5. Completed → generate JWT ticket
	if status == "completed" {

		// load event safely
		var event models.Event
		if err := h.db.
			Where("id = ?", paymentIntent.EventID).
			First(&event).Error; err != nil {

			utils.HandleError(c, utils.NewNotFoundError("event not found"))
			return
		}

		jwtService := utils.NewJWTService(&h.config.JWT)

		token, err := jwtService.GenerateTicketToken(
			event,
			paymentIntent.ActorID,
			paymentIntent.CheckoutToken,
		)

		if err == nil {
			response["ticket"] = map[string]interface{}{
				"count": paymentIntent.Quantity,
				"token": token,
				"url":   h.config.URLs.UserBaseURL + "/tickets/view?token=" + token,
			}
		}
	}

	log.Printf("[CHECKOUT_RESPONSE] %+v", response)

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

// PurchaseTickets godoc
// @Summary Purchase tickets (Unified endpoint for both guest and authenticated users)
// @Description Create a payment intent and reserve tickets for purchase. Handles both guest and authenticated users seamlessly.
// @Tags Public
// @Accept json
// @Produce json
// @Param request body models.TicketPurchaseRequest true "Purchase details"
// @Header 201 {string} Idempotency-Key "Unique key to ensure idempotent requests (generate a new UUID for each purchase attempt)" "e.g., 550e8400-e29b-41d4-a716-446655440000"
// @Success 200 {object} utils.Response{data=services.CheckoutResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/purchase [post]
func (h *PublicHandler) PurchaseTickets(c *gin.Context) {
	var req models.TicketPurchaseRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[PURCHASE] Request binding error: %v", err)
		utils.HandleError(c, err)
		return
	}

	// ✅ FIX: Read idempotency key from HEADER
	idempotencyKey := c.GetHeader("Idempotency-Key")

	// Validate request
	if req.EventID == uuid.Nil {
		log.Printf("[PURCHASE] Validation failed: event_id is required")
		utils.HandleError(c, utils.NewValidationError("event_id is required", nil))
		return
	}
	if len(req.Tiers) == 0 {
		log.Printf("[PURCHASE] Validation failed: at least one tier must be specified")
		utils.HandleError(c, utils.NewValidationError("at least one tier must be specified", nil))
		return
	}
	if req.Currency == "" {
		log.Printf("[PURCHASE] Validation failed: currency is required")
		utils.HandleError(c, utils.NewValidationError("currency is required", nil))
		return
	}
	if req.PaymentGateway == "" {
		log.Printf("[PURCHASE] Validation failed: payment_gateway is required")
		utils.HandleError(c, utils.NewValidationError("payment_gateway is required", nil))
		return
	}
	if req.CustomerEmail == "" {
		log.Printf("[PURCHASE] Validation failed: customer_email is required")
		utils.HandleError(c, utils.NewValidationError("customer_email is required", nil))
		return
	}

	log.Printf("[PURCHASE] Validating event: %s", req.EventID.String())

	// Check if event exists and is purchasable
	var event models.Event
	if err := h.db.Preload("Tiers").Where("id = ?", req.EventID).First(&event).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Printf("[PURCHASE] Event not found: %s", req.EventID.String())
			utils.HandleError(c, utils.NewNotFoundError("event not found"))
			return
		}
		log.Printf("[PURCHASE] Database error checking event: %v", err)
		utils.HandleError(c, utils.NewDatabaseError("Failed to check event", err))
		return
	}

	// Check event status is purchasable
	purchasableStatuses := map[string]bool{"on_sale": true, "hold": true, "live": true}
	if !purchasableStatuses[event.Status] {
		log.Printf("[PURCHASE] Event status not purchasable: event_id=%s, status=%s", req.EventID.String(), event.Status)
		utils.HandleError(c, utils.NewBusinessLogicError(fmt.Sprintf("Event cannot be purchased in status '%s'. Purchasable statuses: on_sale, hold, live", event.Status)))
		return
	}

	// Check currency matches
	if event.Currency != req.Currency {
		log.Printf("[PURCHASE] Currency mismatch: event_id=%s, event_currency=%s, request_currency=%s", req.EventID.String(), event.Currency, req.Currency)
		utils.HandleError(c, utils.NewBusinessLogicError(fmt.Sprintf("Currency mismatch: event requires '%s' but request specified '%s'", event.Currency, req.Currency)))
		return
	}

	// Validate all tiers exist and belong to this event
	tierMap := make(map[uuid.UUID]models.EventTier)
	for _, tier := range event.Tiers {
		tierMap[tier.ID] = tier
	}

	totalTicketsRequested := 0
	for _, reqTier := range req.Tiers {
		tier, exists := tierMap[reqTier.TierID]
		if !exists {
			log.Printf("[PURCHASE] Tier not found: tier_id=%s, event_id=%s", reqTier.TierID.String(), req.EventID.String())
			utils.HandleError(c, utils.NewNotFoundError(fmt.Sprintf("tier '%s' not found for this event", reqTier.TierID.String())))
			return
		}

		// ✅ NEW: Validate ticket quantity
		if reqTier.Quantity <= 0 {
			log.Printf("[PURCHASE] Invalid quantity: tier_id=%s, quantity=%d", reqTier.TierID.String(), reqTier.Quantity)
			utils.HandleError(c, utils.NewValidationError(
				fmt.Sprintf("Invalid ticket quantity for tier. Must be at least 1 ticket, but got %d", reqTier.Quantity),
				map[string]interface{}{"tier_id": reqTier.TierID.String()},
			))
			return
		}

		// ✅ NEW: Check tier has capacity for requested quantity
		availableInTier := tier.Quantity - tier.Sold - tier.Reserved
		if availableInTier < reqTier.Quantity {
			log.Printf("[PURCHASE] Insufficient inventory: tier_id=%s, requested=%d, available=%d", tier.ID.String(), reqTier.Quantity, availableInTier)
			utils.HandleError(c, utils.NewBusinessLogicError(
				fmt.Sprintf(
					"Insufficient tickets in '%s' tier. You requested %d ticket(s) but only %d available. Please select fewer tickets or choose a different tier.",
					tier.TierName,
					reqTier.Quantity,
					availableInTier,
				),
			))
			return
		}

		totalTicketsRequested += reqTier.Quantity
	}

	// ✅ NEW: Validate total quantity across all tiers
	if totalTicketsRequested > 500 {
		log.Printf("[PURCHASE] Total tickets exceed limit: total=%d", totalTicketsRequested)
		utils.HandleError(c, utils.NewValidationError(
			fmt.Sprintf("You cannot purchase more than 500 tickets in a single order. You requested %d tickets.", totalTicketsRequested),
			map[string]interface{}{"total_requested": totalTicketsRequested},
		))
		return
	}

	log.Printf("[PURCHASE] Event and tier validation passed, checking authentication")

	// Actor resolution - check for JWT token to determine if user is authenticated
	var actorType models.ActorType
	var actorID uuid.UUID

	// Try to validate JWT token (optional for this endpoint - no error response written if not present/invalid)
	claims := utils.OptionalValidateAuthToken(c, h.config)
	if claims != nil {
		actorType = models.ActorUser
		actorID = claims.UserID
		log.Printf("[PURCHASE] Authenticated user: %s", claims.UserID.String())
	} else {
		// For unauthenticated users, create or find guest user
		var guestUser models.GuestUser

		err := h.db.Where("email = ?", req.CustomerEmail).First(&guestUser).Error
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				guestUser = models.GuestUser{
					ID:        uuid.New(),
					Email:     req.CustomerEmail,
					FirstName: "Guest",
					LastName:  "User",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
				if err := h.db.Create(&guestUser).Error; err != nil {
					log.Printf("[PURCHASE] Failed to create guest user: %v", err)
					utils.HandleError(c, utils.NewInternalServerError("Failed to create guest user", err))
					return
				}
				log.Printf("[PURCHASE] Created new guest user: %s", guestUser.ID.String())
			} else {
				log.Printf("[PURCHASE] Database error finding guest user: %v", err)
				utils.HandleError(c, utils.NewInternalServerError("Failed to find guest user", err))
				return
			}
		}

		actorType = models.ActorGuest
		actorID = guestUser.ID
		log.Printf("[PURCHASE] Guest user: %s", guestUser.ID.String())
	}

	// Convert tiers
	tiers := make([]types.TierSelection, len(req.Tiers))
	for i, tier := range req.Tiers {
		tiers[i] = types.TierSelection{
			TierID:   tier.TierID,
			Quantity: tier.Quantity,
		}
	}

	log.Printf("[PURCHASE] Building checkout request: event_id=%s, actor_id=%s, actor_type=%s, tiers=%d", req.EventID.String(), actorID.String(), actorType, len(tiers))

	// Build orchestrator request
	checkoutReq := &services.CheckoutRequest{
		ActorID:        actorID,
		ActorType:      actorType,
		EventID:        req.EventID,
		Tiers:          tiers,
		Currency:       req.Currency,
		PaymentGateway: models.PaymentGateway(req.PaymentGateway),
		CustomerEmail:  req.CustomerEmail,
		Timezone:       req.Timezone,

		// ✅ FIXED: always from header
		IdempotencyKey: idempotencyKey,
	}

	log.Printf("[PURCHASE] Calling purchaseOrchestrator.Checkout()")
	response, err := h.purchaseOrchestrator.Checkout(c.Request.Context(), checkoutReq)
	if err != nil {
		log.Printf("[PURCHASE] Checkout error: %v", err)
		log.Printf("[PURCHASE] Error type: %T", err)
		log.Printf("[PURCHASE] Error message: %s", err.Error())
		utils.HandleError(c, err)
		return
	}

	log.Printf("[PURCHASE] Checkout successful: checkout_token=%s", response.CheckoutToken)

	utils.SuccessResponse(c, http.StatusOK,
		"Payment initiated successfully, please proceed with the payment",
		response,
	)
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
		utils.HandleError(c, utils.NewValidationError("token is required", nil))
		return
	}

	// JWT validation
	jwtService := utils.NewJWTService(&h.config.JWT)
	claims, err := jwtService.ParseTicketToken(token)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Load payment intent
	var paymentIntent models.PaymentIntent
	if err := h.db.
		Where("checkout_token = ?", claims.CheckoutToken).
		First(&paymentIntent).Error; err != nil {
		utils.HandleError(c, utils.NewNotFoundError("payment not found"))
		return
	}

	// Load event (no organizer preload needed here anymore)
	var event models.Event
	if err := h.db.
		Where("id = ?", paymentIntent.EventID).
		First(&event).Error; err != nil {
		utils.HandleError(c, utils.NewNotFoundError("event not found"))
		return
	}

	// Event ended check
	if !event.EndDate.IsZero() && event.EndDate.Before(time.Now()) {
		utils.HandleError(c, utils.NewBusinessLogicError("event already ended"))
		return
	}

	// Resolve organizer info via utility (handles onboarding → user fallback)
	organizerInfo, err := helpers.GetOrganizerInfo(h.db, event.OrganizerID.String())
	if err != nil {
		// Non-fatal: log and continue with empty organizer info
		log.Printf("failed to resolve organizer info for event %s: %v", event.ID, err)
		organizerInfo = &helpers.OrganizerInfo{ID: event.OrganizerID.String()}
	}

	// Load tickets
	var tickets []models.Ticket
	if err := h.db.
		Preload("Tier").
		Preload("CheckIns").
		Where("checkout_token = ?", paymentIntent.CheckoutToken).
		Find(&tickets).Error; err != nil {
		utils.HandleError(c, utils.NewInternalServerError("failed to load tickets", err))
		return
	}
	if len(tickets) == 0 {
		utils.HandleError(c, utils.NewNotFoundError("no tickets found"))
		return
	}

	// QR generation
	secureQrService := services.NewSecureQRService(h.config)
	ticketResponses := make([]map[string]interface{}, 0, len(tickets))
	for _, t := range tickets {
		qrToken, err := secureQrService.GenerateSecureQRPayload(&t, &event)
		if err != nil {
			utils.HandleError(c, err)
			return
		}

		tierName := ""
		if t.Tier != nil {
			tierName = t.Tier.TierName
		}

		ticketResponses = append(ticketResponses, map[string]interface{}{
			"ticket_id":     t.ID,
			"ticket_number": t.TicketNumber,
			"tier_name":     tierName,
			"tier": map[string]interface{}{
				"id":   t.TierID,
				"name": tierName,
			},
			"price":      t.UnitPrice,
			"qr_data":    qrToken,
			"checked_in": len(t.CheckIns) > 0,
			"status":     t.Status,
		})
	}

	// Company info
	companyInfo, err := helpers.GetCompanyInfo(h.db)
	if err != nil {
		log.Printf("failed to load company info for ticket view: %v", err)
		companyInfo = &helpers.CompanyInfo{}
	}
	companyResponse := map[string]interface{}{
		"id":       companyInfo.ID,
		"name":     companyInfo.Name,
		"logo_url": companyInfo.LogoURL,
		"email":    companyInfo.Email,
	}

	response := map[string]interface{}{
		"order_id": paymentIntent.CheckoutToken,
		"event": map[string]interface{}{
			"id":           event.ID,
			"title":        event.Title,
			"banner_image": event.BannerImage,
			"venue_name":   event.VenueName,
			"address":      event.Address,
			"start_date":   event.StartDate,
			"timezone":     event.Timezone,
			"end_date":     event.EndDate,
			"organizer": map[string]interface{}{
				"id":                organizerInfo.ID,
				"business_name":     organizerInfo.BusinessName,
				"business_logo_url": organizerInfo.BusinessLogoURL,
			},
		},
		"ticket_count":       len(tickets),
		"transaction_status": string(paymentIntent.Status),
		"total_amount":       paymentIntent.AmountTotal,
		"currency":           paymentIntent.Currency,
		"purchase_date":      paymentIntent.CreatedAt,
		"is_guest_purchase":  paymentIntent.ActorType == models.ActorGuest,
		"tickets":            ticketResponses,
		"company":            companyResponse,
	}

	utils.SuccessResponse(c, http.StatusOK, "Ticket retrieved successfully", response)
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
		utils.HandleError(c, utils.NewValidationError("token required", nil))
		return
	}

	jwtService := utils.NewJWTService(&h.config.JWT)

	claims, err := jwtService.ParseTicketToken(token)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Check payment intent still valid
	var paymentIntent models.PaymentIntent
	if err := h.db.
		Where("checkout_token = ?", claims.CheckoutToken).
		First(&paymentIntent).Error; err != nil {

		utils.HandleError(c, utils.NewNotFoundError("payment not found"))
		return
	}

	if paymentIntent.Status != models.PaymentIntentSucceeded {
		utils.HandleError(c, utils.NewBusinessLogicError("payment not completed"))
		return
	}

	response := map[string]interface{}{
		"valid":          true,
		"event_id":       claims.EventID,
		"actor_id":       claims.ActorID,
		"checkout_token": claims.CheckoutToken,
		"quantity":       paymentIntent.Quantity,
	}

	utils.SuccessResponse(c, http.StatusOK, "token valid", response)
}
