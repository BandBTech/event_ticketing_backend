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

	if err := h.db.Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Tiers").
		Where("is_featured = ? AND status IN (?) AND start_date > ?", true, []string{"on_sale", "completed", "approved"}, utils.Now()).
		Order("created_at DESC").
		Limit(limit).
		Find(&events).Error; err != nil {
		utils.HandleError(c, err)
		return
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

	query := h.db.Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Tiers").
		Where("status IN (?) AND start_date > ?", []string{"on_sale", "completed", "approved"}, utils.Now())

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

	// Get events with pagination
	if err := query.Order("start_date ASC").
		Offset(offset).
		Limit(limit).
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
		"events": publicEvents,
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
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
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

	query := h.db.Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Tiers").
		Where("status IN (?) AND start_date > ? AND category = ?", []string{"on_sale", "completed", "approved"}, utils.Now(), category)

	// Get total count
	if err := query.Model(&models.Event{}).Count(&total).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get events with pagination
	if err := query.Order("start_date ASC").
		Offset(offset).
		Limit(limit).
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
		"category": category,
		"events":   publicEvents,
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
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
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

	query := h.db.Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Tiers").
		Where("status IN (?) AND start_date > ? AND (title ILIKE ? OR description ILIKE ?)",
			[]string{"on_sale", "completed", "approved"}, utils.Now(), "%"+searchQuery+"%", "%"+searchQuery+"%")

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

	// Get events with pagination
	if err := query.Order("start_date ASC").
		Offset(offset).
		Limit(limit).
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
		"query":  searchQuery,
		"events": publicEvents,
		"pagination": map[string]interface{}{
			"current_page": page,
			"total_pages":  (total + int64(limit) - 1) / int64(limit),
			"total_items":  total,
			"limit":        limit,
		},
	}

	utils.SuccessResponse(c, http.StatusOK, "Search results retrieved successfully", response)
}

// validateEventPurchaseEligibility checks if an event allows ticket purchases
func (h *PublicHandler) validateEventPurchaseEligibility(eventID uuid.UUID, tierID uuid.UUID) error {
	return utils.ValidateEventPurchaseEligibility(h.db, eventID.String(), tierID.String())
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

	// Validate event purchase eligibility
	if err := h.validateEventPurchaseEligibility(req.EventID, req.TierID); err != nil {
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

	// For cash payment, validate that the email is in the allowed list
	if req.PaymentGateway == models.PaymentGatewayCash {
		cfg, err := config.Load()
		if err != nil {
			utils.HandleError(c, err)
			return
		}

		// Check if the email is in the allowed list for cash payments
		allowed := false
		for _, allowedEmail := range cfg.Payment.CashAllowedEmails {
			if strings.TrimSpace(allowedEmail) == req.Email {
				allowed = true
				break
			}
		}

		if !allowed {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
	}

	// For cash payment, assume payment is successful immediately
	if req.PaymentGateway == "cash" {
		// Purchase tickets as guest (returns multiple tickets)
		tickets, guestUser, err := h.ticketService.PurchaseTicketAsGuest(&req)
		if err != nil {
			utils.HandleError(c, err)
			return
		}

		// Send single order confirmation email with ticket links
		if h.ticketService.GetEmailQueueService() != nil {
			// Get event details
			var event models.Event
			if len(tickets) > 0 {
				if err := h.db.Preload("Organizer").Preload("Organizer.OrganizerOnboarding").First(&event, tickets[0].EventID).Error; err != nil {
					log.Printf("Failed to get event details: %v", err)
					utils.HandleError(c, err)
					return
				}
			}

			// Generate email data for order confirmation
			emailData, err := h.prepareGuestOrderConfirmationData(guestUser, &event, tickets)
			if err != nil {
				log.Printf("Failed to prepare email data: %v", err)
				utils.HandleError(c, err)
				return
			}

			// Send single email with all tickets
			if err := h.ticketService.GetEmailQueueService().QueueGuestOrderConfirmationEmail(req.Email, emailData); err != nil {
				log.Printf("Failed to queue order confirmation email: %v", err)
				utils.HandleError(c, err)
				return
			}
		}

		utils.SuccessResponse(c, http.StatusCreated, fmt.Sprintf("Tickets purchased successfully! Confirmation email sent to: %s", req.Email), nil)
		return
	}

	// For payment gateways (stripe, paypal, esewa, khalti, imepay), create checkout session
	checkoutSession, tickets, guestUser, err := h.ticketService.InitiatePaymentGatewayPurchase(&req)
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
		"checkout_session": checkoutSession.ToResponse(),
		"tickets":          ticketResponses,
		"guest_user":       guestUser.ToResponse(),
		"message":          "Payment initiated successfully. Please complete payment using the provided gateway data.",
	}

	utils.SuccessResponse(c, http.StatusCreated, "Payment gateway purchase initiated", response)
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
// @Param checkout_token path string true "Checkout token"
// @Param request body models.PaymentCallbackRequest true "Payment callback data"
// @Success 200 {object} utils.Response "Payment processed successfully"
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/payment/success/{checkout_token} [post]
func (h *PublicHandler) PaymentSuccessCallback(c *gin.Context) {
	checkoutToken := c.Param("checkout_token")
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

	// Process successful payment
	err := h.ticketService.ProcessPaymentSuccess(&req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment processed successfully", nil)
}

// PaymentFailureCallback godoc
// @Summary Handle payment gateway failure callback
// @Description Process failed payment from gateway
// @Tags Public
// @Accept json
// @Produce json
// @Param checkout_token path string true "Checkout token"
// @Param request body models.PaymentCallbackRequest true "Payment callback data"
// @Success 200 {object} utils.Response "Payment failure recorded"
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/payment/failure/{checkout_token} [post]
func (h *PublicHandler) PaymentFailureCallback(c *gin.Context) {
	checkoutToken := c.Param("checkout_token")
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

	// Get all tickets for this order (same event, same user/guest, same purchase date)
	var tickets []models.Ticket
	query := h.db.Preload("Event").Preload("Event.Tiers").Preload("Event.Organizer").Preload("Event.Organizer.OrganizerOnboarding").Preload("Tier")

	if claims.UserID != nil {
		query = query.Where("user_id = ? AND event_id = ?", *claims.UserID, claims.EventID)
	} else if claims.GuestUserID != nil {
		query = query.Where("guest_user_id = ? AND event_id = ?", *claims.GuestUserID, claims.EventID)
	} else {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get tickets from the last 24 hours to group them as an order
	since := time.Now().Add(-24 * time.Hour)
	if err := query.Where("created_at > ? AND status = ?", since, "active").Find(&tickets).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

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
		var businessName, businessDescription, businessLogo string
		if tickets[0].Event.Organizer.OrganizerOnboarding != nil {
			businessName = tickets[0].Event.Organizer.OrganizerOnboarding.BusinessName
			businessDescription = tickets[0].Event.Organizer.OrganizerOnboarding.BusinessDescription
			businessLogo = tickets[0].Event.Organizer.OrganizerOnboarding.BusinessLogoURL
		}

		// Fallback to organizer's personal name if business name is not available
		if businessName == "" {
			businessName = strings.TrimSpace(tickets[0].Event.Organizer.FirstName + " " + tickets[0].Event.Organizer.LastName)
		}

		organizerResp = &models.OrganizerPublicResponse{
			ID:          tickets[0].Event.Organizer.ID,
			Name:        businessName,
			Description: businessDescription,
			Logo:        businessLogo,
			Status:      tickets[0].Event.Organizer.OrganizerStatus,
		}
	} else {
		// Fallback: try to load organizer directly from event's organizer_id
		if tickets[0].Event.OrganizerID != uuid.Nil {
			var organizer models.User
			if err := h.db.Preload("OrganizerOnboarding").First(&organizer, tickets[0].Event.OrganizerID).Error; err == nil {
				var businessName, businessDescription, businessLogo string
				if organizer.OrganizerOnboarding != nil {
					businessName = organizer.OrganizerOnboarding.BusinessName
					businessDescription = organizer.OrganizerOnboarding.BusinessDescription
					businessLogo = organizer.OrganizerOnboarding.BusinessLogoURL
				}

				// Fallback to organizer's personal name if business name is not available
				if businessName == "" {
					businessName = strings.TrimSpace(organizer.FirstName + " " + organizer.LastName)
				}

				organizerResp = &models.OrganizerPublicResponse{
					ID:          organizer.ID,
					Name:        businessName,
					Description: businessDescription,
					Logo:        businessLogo,
					Status:      organizer.OrganizerStatus,
				}
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
	orderResponse := models.OrderViewMinimalResponse{
		OrderID:         tickets[0].ID.String(),
		Event:           eventResp,
		Tickets:         ticketResponses,
		TotalAmount:     totalAmount,
		Currency:        currency,
		PurchaseDate:    tickets[0].PurchaseDate,
		IsGuestPurchase: tickets[0].IsGuestPurchase,
		Company:         companyResp,
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

	// Generate a single JWT token for the first ticket (will show all tickets in the order)
	var ticketURL string
	if len(tickets) > 0 {
		jwtService := utils.NewJWTService(&h.config.JWT)
		token, err := jwtService.GenerateTicketAccessToken(tickets[0])
		if err != nil {
			log.Printf("Failed to generate JWT token: %v", err)
		} else {
			ticketURL = fmt.Sprintf("%s/tickets/view?token=%s", h.config.URLs.UserBaseURL, token)
		}
	}

	emailData := map[string]interface{}{
		"event_name":    event.Title,
		"event_date":    event.StartDate.Format("January 2, 2006"),
		"venue":         event.VenueName,
		"total_tickets": len(tickets),
		"total_amount":  totalAmount,
		"ticket_url":    ticketURL,
		"CurrentYear":   time.Now().Year(),
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
// @Param sort query string false "Sort by field with optional '-' prefix for desc (e.g., '-purchase_date', 'ticket_number')" default(-purchase_date)
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

	tickets, total, err := h.ticketService.GetGuestTickets(guestEmail, page, limit, status, eventID, sortBy, sortOrder, startDate, endDate)
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

	utils.SuccessResponse(c, http.StatusOK, "Guest tickets retrieved successfully", response)
}
