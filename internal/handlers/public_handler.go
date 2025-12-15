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
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
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
			utils.NotFoundErrorResponse(c, "Company information not found", nil)
			return
		}
		utils.DatabaseErrorResponse(c, "Failed to get company information", err)
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
		utils.DatabaseErrorResponse(c, "Failed to get categories", err)
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
// @Success 200 {object} utils.Response{data=[]models.Event} "Featured events"
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

	if err := h.db.Preload("Organizer").Preload("Organizer.Organization").Preload("Tiers").
		Where("is_featured = ? AND status = ? AND start_date > ?", true, "approved", utils.Now()).
		Order("created_at DESC").
		Limit(limit).
		Find(&events).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to get featured events", err)
		return
	}

	// Convert events to public response format
	var publicEvents []models.EventPublicResponse
	for _, event := range events {
		publicEvents = append(publicEvents, event.ToPublicResponse())
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

	query := h.db.Preload("Organizer").Preload("Organizer.Organization").Preload("Tiers").
		Where("status = ? AND start_date > ?", "approved", utils.Now())

	// Filter by category if provided
	if category := c.Query("category"); category != "" {
		query = query.Where("category = ?", category)
	}

	var events []models.Event
	var total int64

	// Get total count
	if err := query.Model(&models.Event{}).Count(&total).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to count events", err)
		return
	}

	// Get events with pagination
	if err := query.Order("start_date ASC").
		Offset(offset).
		Limit(limit).
		Find(&events).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to get upcoming events", err)
		return
	}

	// Convert events to public response format
	var publicEvents []models.EventPublicResponse
	for _, event := range events {
		publicEvents = append(publicEvents, event.ToPublicResponse())
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
		utils.BadRequestErrorResponse(c, "Category parameter is required", nil)
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

	query := h.db.Preload("Organizer").Preload("Organizer.Organization").Preload("Tiers").
		Where("status = ? AND start_date > ? AND category = ?", "approved", utils.Now(), category)

	// Get total count
	if err := query.Model(&models.Event{}).Count(&total).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to count events", err)
		return
	}

	// Get events with pagination
	if err := query.Order("start_date ASC").
		Offset(offset).
		Limit(limit).
		Find(&events).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to get events by category", err)
		return
	}

	// Convert events to public response format
	var publicEvents []models.EventPublicResponse
	for _, event := range events {
		publicEvents = append(publicEvents, event.ToPublicResponse())
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
		utils.BadRequestErrorResponse(c, "Search query 'q' parameter is required", nil)
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

	query := h.db.Preload("Organizer").Preload("Organizer.Organization").Preload("Tiers").
		Where("status = ? AND start_date > ? AND (title ILIKE ? OR description ILIKE ?)",
			"approved", utils.Now(), "%"+searchQuery+"%", "%"+searchQuery+"%")

	// Filter by category if provided
	if category := c.Query("category"); category != "" {
		query = query.Where("? = ANY(category)", category)
	}

	var events []models.Event
	var total int64

	// Get total count
	if err := query.Model(&models.Event{}).Count(&total).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to count search results", err)
		return
	}

	// Get events with pagination
	if err := query.Order("start_date ASC").
		Offset(offset).
		Limit(limit).
		Find(&events).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to search events", err)
		return
	}

	// Convert events to public response format
	var publicEvents []models.EventPublicResponse
	for _, event := range events {
		publicEvents = append(publicEvents, event.ToPublicResponse())
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

// PurchaseTicketAsGuest godoc
// @Summary Purchase ticket as guest
// @Description Create multiple individual ticket purchases for a guest with payment gateway integration. First name defaults to "Guest", last name defaults to "User" if not provided.
// @Tags Public
// @Accept json
// @Produce json
// @Param request body models.GuestPurchaseRequest true "Guest purchase details (first_name and last_name optional)"
// @Success 201 {object} utils.Response{data=map[string]interface{}} "Purchase created successfully"
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/public/tickets/guest-purchase [post]
func (h *PublicHandler) PurchaseTicketAsGuest(c *gin.Context) {
	var req models.GuestPurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Set default values if not provided
	if req.FirstName == "" {
		req.FirstName = "Guest"
	}
	if req.LastName == "" {
		req.LastName = "User"
	}

	// For cash payment, assume payment is successful immediately
	if req.PaymentGateway == "cash" {
		// Purchase tickets as guest (returns multiple tickets)
		tickets, guestUser, err := h.ticketService.PurchaseTicketAsGuest(&req)
		if err != nil {
			utils.BadRequestErrorResponse(c, "Purchase failed", err)
			return
		}

		// Send single order confirmation email with ticket links
		if h.ticketService.GetEmailQueueService() != nil {
			// Collect all individual tickets
			var allIndividualTickets []models.IndividualTicket
			for _, ticket := range tickets {
				individualTickets, err := h.ticketService.GetIndividualTickets(ticket.ID)
				if err != nil {
					log.Printf("Failed to get individual tickets for ticket %s: %v", ticket.ID, err)
					continue
				}
				allIndividualTickets = append(allIndividualTickets, individualTickets...)
			}

			// Generate email data for order confirmation
			emailData, err := h.prepareGuestOrderConfirmationData(guestUser, tickets[0].Event, allIndividualTickets, tickets)
			if err != nil {
				log.Printf("Failed to prepare email data: %v", err)
				utils.BadRequestErrorResponse(c, "Failed to prepare confirmation email", err)
				return
			}

			// Send single email with all tickets
			if err := h.ticketService.GetEmailQueueService().QueueGuestOrderConfirmationEmail(req.Email, emailData); err != nil {
				log.Printf("Failed to queue order confirmation email: %v", err)
				utils.BadRequestErrorResponse(c, "Failed to queue confirmation email", err)
				return
			}
		}

		utils.SuccessResponse(c, http.StatusCreated, fmt.Sprintf("Tickets purchased successfully! Confirmation email sent to: %s", req.Email), nil)
		return
	}

	// For payment gateways (stripe, paypal, esewa), create checkout session
	checkoutSession, tickets, guestUser, err := h.ticketService.InitiatePaymentGatewayPurchase(&req)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Failed to initiate payment", err)
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
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	ticket, err := h.ticketService.VerifyGuestEmail(req.Token)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Verification failed", err)
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
		utils.BadRequestErrorResponse(c, "Checkout token is required", nil)
		return
	}

	var req models.PaymentCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid callback data", err)
		return
	}

	// Override checkout token from URL param (more secure)
	req.CheckoutToken = checkoutToken

	// Process successful payment
	err := h.ticketService.ProcessPaymentSuccess(&req)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Payment processing failed", err)
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
		utils.BadRequestErrorResponse(c, "Checkout token is required", nil)
		return
	}

	var req models.PaymentCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid callback data", err)
		return
	}

	// Override checkout token from URL param (more secure)
	req.CheckoutToken = checkoutToken

	// Process failed payment
	err := h.ticketService.ProcessPaymentFailure(&req)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Payment failure processing failed", err)
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
		utils.BadRequestErrorResponse(c, "Checkout token is required", nil)
		return
	}

	checkoutSession, err := h.ticketService.GetCheckoutSessionByToken(checkoutToken)
	if err != nil {
		utils.NotFoundErrorResponse(c, "Checkout session not found", nil)
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
		utils.BadRequestErrorResponse(c, "Ticket access token is required", nil)
		return
	}

	// Validate the JWT token
	jwtService := utils.NewJWTService(&h.config.JWT)
	claims, err := jwtService.ValidateTicketAccessToken(token)
	if err != nil {
		utils.UnauthorizedErrorResponse(c, "Invalid or expired ticket access token", nil)
		return
	}

	// Get all tickets for this order (same event, same user/guest, same purchase date)
	var tickets []models.Ticket
	query := h.db.Preload("Event").Preload("Event.Tiers").Preload("Event.Organizer").Preload("Event.Organizer.Organization")

	if claims.UserID != nil {
		query = query.Where("user_id = ? AND event_id = ?", *claims.UserID, claims.EventID)
	} else if claims.GuestUserID != nil {
		query = query.Where("guest_user_id = ? AND event_id = ?", *claims.GuestUserID, claims.EventID)
	} else {
		utils.UnauthorizedErrorResponse(c, "Invalid token claims", nil)
		return
	}

	// Get tickets from the last 24 hours to group them as an order
	since := time.Now().Add(-24 * time.Hour)
	if err := query.Where("created_at > ? AND status = ?", since, "active").Find(&tickets).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to retrieve tickets", err)
		return
	}

	if len(tickets) == 0 {
		utils.NotFoundErrorResponse(c, "No tickets found for this order", nil)
		return
	}

	// Convert tickets to view responses
	var ticketResponses []models.TicketViewResponse
	totalAmount := 0.0

	for _, ticket := range tickets {
		resp := ticket.ToViewResponse()

		// Generate secure QR payload for the ticket (first individual ticket)
		if qr, err := h.ticketService.GenerateQRCodeForTicket(ticket.ID); err == nil {
			resp.QRData = qr
		}

		ticketResponses = append(ticketResponses, resp)
		totalAmount += ticket.TotalAmount
	}

	// Create order response
	orderResponse := models.OrderViewResponse{
		OrderID:         tickets[0].ID.String(), // Use first ticket ID as order identifier
		Event:           ticketResponses[0].Event,
		Tickets:         ticketResponses,
		TotalAmount:     totalAmount,
		PurchaseDate:    tickets[0].PurchaseDate,
		IsGuestPurchase: tickets[0].IsGuestPurchase,
	}

	utils.SuccessResponse(c, http.StatusOK, "Order retrieved successfully", orderResponse)
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
		utils.BadRequestErrorResponse(c, "Ticket access token is required", nil)
		return
	}

	// Validate the JWT token
	jwtService := utils.NewJWTService(&h.config.JWT)
	claims, err := jwtService.ValidateTicketAccessToken(token)
	if err != nil {
		utils.UnauthorizedErrorResponse(c, "Invalid or expired ticket access token", nil)
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
		utils.UnauthorizedErrorResponse(c, "Invalid token claims", nil)
		return
	}

	// Get tickets from the last 24 hours
	since := time.Now().Add(-24 * time.Hour)
	if err := query.Where("created_at > ? AND status = ?", since, "active").Count(&ticketCount).Error; err != nil {
		utils.DatabaseErrorResponse(c, "Failed to validate token", err)
		return
	}

	if ticketCount == 0 {
		utils.NotFoundErrorResponse(c, "No valid tickets found for this token", nil)
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
func (h *PublicHandler) prepareGuestOrderConfirmationData(guestUser *models.GuestUser, event *models.Event, individualTickets []models.IndividualTicket, tickets []*models.Ticket) (map[string]interface{}, error) {
	var ticketData []map[string]interface{}
	totalAmount := 0.0

	for _, ticket := range individualTickets {
		// Generate JWT access token using the parent ticket
		jwtService := utils.NewJWTService(&h.config.JWT)
		token, err := jwtService.GenerateTicketAccessToken(ticket.Ticket)
		if err != nil {
			return nil, fmt.Errorf("failed to generate JWT token for ticket %s: %w", ticket.TicketNumber, err)
		}

		// Prepare ticket data for email template
		ticketData = append(ticketData, map[string]interface{}{
			"ticket_number": ticket.TicketNumber,
			"view_url":      fmt.Sprintf("%s/tickets/view?token=%s", h.config.URLs.UserBaseURL, token),
		})

		// Calculate total amount (price per individual ticket)
		if ticket.Ticket != nil && ticket.Ticket.Quantity > 0 {
			totalAmount += ticket.Ticket.TotalAmount / float64(ticket.Ticket.Quantity)
		}
	}

	// Prepare email data matching guest_order_confirmation.html template
	emailData := map[string]interface{}{
		"guest_name":     guestUser.FirstName + " " + guestUser.LastName,
		"event_name":     event.Title,
		"event_date":     event.StartDate.Format("January 2, 2006"),
		"event_time":     event.StartDate.Format("3:04 PM"),
		"venue":          event.VenueName,
		"organizer_name": event.Organizer.Organization.Name,
		"tickets":        ticketData,
		"total_tickets":  len(individualTickets),
		"total_amount":   totalAmount,
		"CurrentYear":    time.Now().Year(),
	}

	return emailData, nil
}
