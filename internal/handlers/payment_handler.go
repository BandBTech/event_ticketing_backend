package handlers

import (
	"net/http"
	"strings"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v74"
	"github.com/stripe/stripe-go/v74/paymentintent"
)

// PaymentHandler handles all payment-related operations
type PaymentHandler struct {
	paymentService              *services.PaymentService
	ticketService               *services.TicketService
	unifiedPurchaseOrchestrator *services.UnifiedPurchaseOrchestrator
	cfg                         *config.Config
	stripeAPIKey                string
}

// NewPaymentHandler creates a new payment handler instance
func NewPaymentHandler(
	paymentService *services.PaymentService,
	ticketService *services.TicketService,
	unifiedOrchestrator *services.UnifiedPurchaseOrchestrator,
	cfg *config.Config,
) *PaymentHandler {
	return &PaymentHandler{
		paymentService:              paymentService,
		ticketService:               ticketService,
		unifiedPurchaseOrchestrator: unifiedOrchestrator,
		cfg:                         cfg,
		stripeAPIKey:                cfg.Payment.Gateways.StripeAPIKey,
	}
}

// InitiatePayment godoc
// @Summary Initiate a payment
// @Description Create a payment intent, reserve tickets, and prepare payment with selected gateway. Works for both authenticated users and guests via unified centralized system.
// @Tags Payments
// @Accept json
// @Produce json
// @Param request body services.InitiatePaymentRequest true "Payment initiation details including event, tier, quantity, and customer info"
// @Success 200 {object} utils.Response{data=services.InitiatePaymentResponse} "Payment initiated successfully with gateway details"
// @Failure 400 {object} utils.Response "Invalid request payload, missing required fields, or insufficient ticket availability"
// @Failure 401 {object} utils.Response "Authentication required for user-specific payments"
// @Failure 404 {object} utils.Response "Event or tier not found"
// @Failure 500 {object} utils.Response "Internal server error during payment initiation"
// @Router /api/v1/payments/initiate [post]
func (h *PaymentHandler) InitiatePayment(c *gin.Context) {
	var req services.InitiatePaymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	// Get user ID from context if logged in (optional)
	userIDInterface, exists := c.Get("userID")
	if exists {
		if userID, ok := userIDInterface.(uuid.UUID); ok {
			req.UserID = &userID
		}
	}

	// For guests, ensure guest_user_id or email is provided
	if req.UserID == nil && req.GuestUserID == nil && req.CustomerEmail == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "Either user authentication or guest email is required", nil)
		return
	}

	// ===== CENTRALIZED PAYMENT ROUTING =====
	// Convert to unified request for centralized processing
	// Support both new multi-tier format and legacy single-tier format for backward compatibility

	var tierSelections []models.TicketTierSelection

	// If new multi-tier format is provided, use it
	if len(req.Tiers) > 0 {
		tierSelections = req.Tiers
	} else if req.TierID != uuid.Nil && req.Quantity > 0 {
		// Backward compatibility: convert single tier format to multi-tier
		tierSelections = make([]models.TicketTierSelection, 1)
		tierSelections[0] = models.TicketTierSelection{
			TierID:   req.TierID,
			Quantity: req.Quantity,
		}
	} else {
		// Neither new format nor legacy format provided
		utils.ErrorResponse(c, http.StatusBadRequest, "Either tiers array or tier_id+quantity must be provided", nil)
		return
	}

	unifiedReq := &services.UnifiedPurchaseRequest{
		UserID:         req.UserID,
		GuestUserID:    req.GuestUserID,
		Email:          req.CustomerEmail,
		FirstName:      req.CustomerName, // Use customer name as first name
		Phone:          req.CustomerPhone,
		CountryCode:    req.CountryCode,
		EventID:        req.EventID,
		Tiers:          tierSelections,
		PaymentGateway: models.PaymentGateway(req.PaymentGateway),
		Currency:       req.Currency,
	}

	// Process via unified orchestrator
	unifiedResp, err := h.unifiedPurchaseOrchestrator.ProcessUnifiedPurchase(c.Request.Context(), unifiedReq)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to initiate payment", err)
		return
	}

	// Convert unified response to PaymentResponse format for backward compatibility
	response := &services.InitiatePaymentResponse{
		Amount:         unifiedResp.Amount,
		Currency:       unifiedResp.Currency,
		Status:         unifiedResp.Status,
		PaymentGateway: unifiedResp.PaymentGateway,
		RedirectURL:    unifiedResp.RedirectURL,
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment initiated successfully via centralized system", response)
}

// GetPaymentStatus godoc
// @Summary Get payment intent status
// @Description Retrieve the current status of a payment intent
// @Tags Payments
// @Produce json
// @Param payment_intent_id path string true "Payment Intent ID"
// @Success 200 {object} utils.Response{data=models.PaymentIntent}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/payments/{payment_intent_id} [get]
func (h *PaymentHandler) GetPaymentStatus(c *gin.Context) {
	paymentIntentIDStr := c.Param("payment_intent_id")
	paymentIntentID, err := uuid.Parse(paymentIntentIDStr)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid payment intent ID", err)
		return
	}

	paymentIntent, err := h.paymentService.GetPaymentIntentByID(c.Request.Context(), paymentIntentID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "Payment intent not found", err)
		return
	}

	// Check authorization - user can only view their own payment intents
	userIDInterface, exists := c.Get("userID")
	if exists {
		if userID, ok := userIDInterface.(uuid.UUID); ok {
			if paymentIntent.UserID != nil && *paymentIntent.UserID != userID {
				utils.ErrorResponse(c, http.StatusForbidden, "You don't have permission to view this payment intent", nil)
				return
			}
		}
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment intent retrieved successfully", paymentIntent)
}

// CancelPayment godoc
// @Summary Cancel a pending payment
// @Description Cancel a payment intent that hasn't been completed yet
// @Tags Payments
// @Security ApiKeyAuth
// @Produce json
// @Param payment_intent_id path string true "Payment Intent ID"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/payments/{payment_intent_id}/cancel [post]
func (h *PaymentHandler) CancelPayment(c *gin.Context) {
	paymentIntentIDStr := c.Param("payment_intent_id")
	paymentIntentID, err := uuid.Parse(paymentIntentIDStr)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid payment intent ID", err)
		return
	}

	// Get user ID from context
	userIDInterface, _ := c.Get("userID")
	userID := userIDInterface.(uuid.UUID)

	if err := h.paymentService.CancelPayment(c.Request.Context(), paymentIntentID, userID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to cancel payment", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment cancelled successfully", nil)
}

// GetUserPayments godoc
// @Summary Get user's payment history
// @Description Retrieve payment history for the authenticated user
// @Tags Payments
// @Security ApiKeyAuth
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param status query string false "Filter by status (pending, succeeded, failed, canceled)"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/payments [get]
func (h *PaymentHandler) GetUserPayments(c *gin.Context) {
	// Get user ID from context
	userIDInterface, _ := c.Get("userID")
	userID := userIDInterface.(uuid.UUID)

	// Parse pagination
	pagination := utils.GetPaginationParams(c, 10)
	status := c.Query("status")

	payments, total, err := h.paymentService.GetUserPayments(c.Request.Context(), userID, status, pagination.Page, pagination.Limit)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve payments", err)
		return
	}

	response := map[string]interface{}{
		"payments":   payments,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Payments retrieved successfully", response)
}

// AdminGetAllPayments godoc
// @Summary Get all payments (Admin)
// @Description Retrieve all payment intents with filters (admin only)
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param status query string false "Filter by status"
// @Param gateway query string false "Filter by payment gateway"
// @Param event_id query string false "Filter by event ID"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments [get]
func (h *PaymentHandler) AdminGetAllPayments(c *gin.Context) {
	// Parse pagination and filters
	pagination := utils.GetPaginationParams(c, 10)
	status := c.Query("status")
	gateway := c.Query("gateway")
	eventIDStr := c.Query("event_id")

	var eventID *uuid.UUID
	if eventIDStr != "" {
		id, err := uuid.Parse(eventIDStr)
		if err == nil {
			eventID = &id
		}
	}

	payments, total, err := h.paymentService.AdminGetAllPayments(c.Request.Context(), status, gateway, eventID, pagination.Page, pagination.Limit)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve payments", err)
		return
	}

	response := map[string]interface{}{
		"payments":   payments,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Payments retrieved successfully", response)
}

// AdminApproveRefund godoc
// @Summary Approve a refund request (Admin)
// @Description Approve a pending refund request and process the refund
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Param refund_id path string true "Refund ID"
// @Success 200 {object} utils.Response{data=models.Refund}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/refunds/{refund_id}/approve [post]
func (h *PaymentHandler) AdminApproveRefund(c *gin.Context) {
	refundIDStr := c.Param("refund_id")
	refundID, err := uuid.Parse(refundIDStr)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid refund ID", err)
		return
	}

	// Get admin ID from context
	adminIDInterface, _ := c.Get("userID")
	adminID := adminIDInterface.(uuid.UUID)

	refund, err := h.paymentService.ApproveRefund(c.Request.Context(), refundID, adminID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to approve refund", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund approved and processed successfully", refund)
}

// AdminRejectRefund godoc
// @Summary Reject a refund request (Admin)
// @Description Reject a pending refund request
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param refund_id path string true "Refund ID"
// @Param request body map[string]interface{} true "Rejection details"
// @Success 200 {object} utils.Response{data=models.Refund}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/refunds/{refund_id}/reject [post]
func (h *PaymentHandler) AdminRejectRefund(c *gin.Context) {
	refundIDStr := c.Param("refund_id")
	refundID, err := uuid.Parse(refundIDStr)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid refund ID", err)
		return
	}

	var req struct {
		Reason string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	// Get admin ID from context
	adminIDInterface, _ := c.Get("userID")
	adminID := adminIDInterface.(uuid.UUID)

	refund, err := h.paymentService.RejectRefund(c.Request.Context(), refundID, adminID, req.Reason)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to reject refund", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund rejected successfully", refund)
}

// AdminGetAllRefunds godoc
// @Summary Get all refunds (Admin)
// @Description Retrieve all refund requests with filters and search
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param status query string false "Filter by status (pending, succeeded, failed, canceled)"
// @Param search query string false "Search by refund number, reason, initiator name/email, or transaction ID"
// @Param refund_type query string false "Filter by refund type (full, partial, event_cancellation, customer_request, admin_action)"
// @Param start_date query string false "Filter refunds from this date (YYYY-MM-DD)"
// @Param end_date query string false "Filter refunds to this date (YYYY-MM-DD)"
// @Param sort_by query string false "Sort by field (created_at, amount, status, refund_number, processed_at)" default(created_at)
// @Param sort_order query string false "Sort order (asc, desc)" default(desc)
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/refunds [get]
// @Router /api/v1/admin/refunds [get]
func (h *PaymentHandler) AdminGetAllRefunds(c *gin.Context) {
	pagination := utils.GetPaginationParams(c, 10)
	status := c.Query("status")
	search := c.Query("search")
	refundType := c.Query("refund_type")
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	// Parse date filters
	var startDate, endDate *time.Time
	if startDateStr := c.Query("start_date"); startDateStr != "" {
		if parsedDate, err := time.Parse("2006-01-02", startDateStr); err == nil {
			startDate = &parsedDate
		}
	}
	if endDateStr := c.Query("end_date"); endDateStr != "" {
		if parsedDate, err := time.Parse("2006-01-02", endDateStr); err == nil {
			// Set end date to end of day
			endOfDay := parsedDate.Add(24*time.Hour - time.Second)
			endDate = &endOfDay
		}
	}

	// Validate sort parameters using centralized utility
	sortBy, sortOrder = utils.ValidateSortForRefunds(sortBy, sortOrder)

	refunds, total, err := h.paymentService.AdminGetAllRefundsList(c.Request.Context(), status, search, refundType, startDate, endDate, pagination.Page, pagination.Limit, sortBy, sortOrder)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve refunds", err)
		return
	}

	response := map[string]interface{}{
		"refunds":    refunds,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Refunds retrieved successfully", response)
}

// AdminGetRefund godoc
// @Summary Get single refund details (Admin)
// @Description Retrieve detailed information for a specific refund
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Param refund_id path string true "Refund ID"
// @Success 200 {object} utils.Response{data=models.RefundDetailResponse}
// @Failure 401 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Router /api/v1/admin/payments/refunds/{refund_id} [get]
func (h *PaymentHandler) AdminGetRefund(c *gin.Context) {
	refundIDStr := c.Param("refund_id")
	refundID, err := uuid.Parse(refundIDStr)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid refund ID", nil))
		return
	}

	refund, err := h.paymentService.AdminGetRefund(c.Request.Context(), refundID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			utils.HandleError(c, utils.NewNotFoundError("Refund not found"))
			return
		}
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve refund", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund retrieved successfully", refund)
}

// AdminRetryFailedRefund godoc
// @Summary Retry a failed refund
// @Description Retry processing a refund that previously failed
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param refund_id path string true "Refund ID"
// @Success 200 {object} utils.Response{data=models.Refund}
// @Failure 400 {object} utils.Response "Not a failed refund"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 404 {object} utils.Response "Refund not found"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/payments/refunds/{refund_id}/retry [post]
func (h *PaymentHandler) AdminRetryFailedRefund(c *gin.Context) {
	refundIDStr := c.Param("refund_id")
	refundID, err := uuid.Parse(refundIDStr)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid refund ID", err)
		return
	}

	refund, err := h.paymentService.RetryFailedRefund(c.Request.Context(), refundID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund retry initiated. Processing async...", refund)
}

// AdminBulkApproveRefunds godoc
// @Summary Bulk approve multiple refunds
// @Description Approve multiple refunds at once (useful for event cancellations)
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body map[string]interface{} true "Refund IDs to approve"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response "Invalid request"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/payments/refunds/bulk-approve [post]
func (h *PaymentHandler) AdminBulkApproveRefunds(c *gin.Context) {
	var req struct {
		RefundIDs []string `json:"refund_ids" binding:"required,min=1,max=500"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	// Get admin ID from context
	adminIDInterface, _ := c.Get("userID")
	adminID := adminIDInterface.(uuid.UUID)

	// Convert string IDs to UUIDs
	refundIDs := make([]uuid.UUID, 0, len(req.RefundIDs))
	for _, idStr := range req.RefundIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			utils.ErrorResponse(c, http.StatusBadRequest, "Invalid refund ID format", err)
			return
		}
		refundIDs = append(refundIDs, id)
	}

	result, err := h.paymentService.BulkApproveRefunds(c.Request.Context(), refundIDs, adminID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Bulk refund approval initiated", result)
}

// AdminGetRefundAnalytics godoc
// @Summary Get refund analytics and statistics
// @Description Retrieve analytics about refunds (success rate, volume, breakdown by type)
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Param start_date query string false "Start date (RFC3339 format)"
// @Param end_date query string false "End date (RFC3339 format)"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response "Invalid date format"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/analytics/refunds [get]
func (h *PaymentHandler) AdminGetRefundAnalytics(c *gin.Context) {
	startDateStr := c.DefaultQuery("start_date", "")
	endDateStr := c.DefaultQuery("end_date", "")

	// Default to last 30 days if not provided
	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -30)

	// Parse provided dates if given
	if startDateStr != "" {
		if parsedStart, err := time.Parse(time.RFC3339, startDateStr); err == nil {
			startDate = parsedStart
		} else {
			utils.ErrorResponse(c, http.StatusBadRequest, "Invalid start_date format", nil)
			return
		}
	}

	if endDateStr != "" {
		if parsedEnd, err := time.Parse(time.RFC3339, endDateStr); err == nil {
			endDate = parsedEnd
		} else {
			utils.ErrorResponse(c, http.StatusBadRequest, "Invalid end_date format", nil)
			return
		}
	}

	analytics, err := h.paymentService.GetRefundAnalytics(c.Request.Context(), startDate, endDate)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve analytics", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund analytics retrieved successfully", analytics)
}

// RequestRefund godoc
// @Summary Request a refund for tickets
// @Description Create a refund request for specific tickets
// @Tags Payments
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body models.RefundRequest true "Refund request details"
// @Success 201 {object} utils.Response{data=models.RefundResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/payments/refund [post]
func (h *PaymentHandler) RequestRefund(c *gin.Context) {
	var req models.RefundRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	// Get user ID from context
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}

	// Request refund
	refund, err := h.ticketService.RequestRefund(&userID, nil, req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Refund request created successfully", refund)
}

// CheckRefundEligibility godoc
// @Summary Check if tickets are eligible for refund
// @Description Check refund eligibility for specific tickets without creating a refund request
// @Tags Payments
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body object{ticket_ids=[]string} true "Ticket IDs to check"
// @Success 200 {object} utils.Response{data=object{eligible=boolean,reason=string}}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/payments/check-refund-eligibility [post]
func (h *PaymentHandler) CheckRefundEligibility(c *gin.Context) {
	var req struct {
		TicketIDs []uuid.UUID `json:"ticket_ids" binding:"required,min=1"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	eligible, reason, err := h.ticketService.CheckRefundEligibility(req.TicketIDs)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to check refund eligibility", err)
		return
	}

	response := map[string]interface{}{
		"eligible": eligible,
		"reason":   reason,
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund eligibility checked successfully", response)
}

// AdminGetStripePaymentIntent godoc
// @Summary Get Stripe PaymentIntent details (Admin)
// @Description Retrieve detailed PaymentIntent information from Stripe API using gateway_txn_id
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Param gateway_txn_id path string true "Stripe PaymentIntent ID (gateway_txn_id)"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response "Invalid gateway_txn_id"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 404 {object} utils.Response "PaymentIntent not found in Stripe"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/payments/stripe/{gateway_txn_id} [get]
func (h *PaymentHandler) AdminGetStripePaymentIntent(c *gin.Context) {
	gatewayTxnID := c.Param("gateway_txn_id")
	if gatewayTxnID == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "gateway_txn_id is required", nil)
		return
	}

	// Set Stripe API key
	stripe.Key = h.stripeAPIKey

	// Fetch PaymentIntent from Stripe
	pi, err := paymentintent.Get(gatewayTxnID, nil)
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "PaymentIntent not found in Stripe", err)
		return
	}

	// Convert to response format
	response := map[string]interface{}{
		"id":                   pi.ID,
		"status":               string(pi.Status),
		"amount":               float64(pi.Amount) / 100, // Convert from cents
		"currency":             string(pi.Currency),
		"client_secret":        pi.ClientSecret,
		"receipt_email":        pi.ReceiptEmail,
		"description":          pi.Description,
		"payment_method":       pi.PaymentMethod,
		"customer":             pi.Customer,
		"metadata":             pi.Metadata,
		"created":              pi.Created,
		"last_payment_error":   pi.LastPaymentError,
		"capture_method":       pi.CaptureMethod,
		"confirmation_method":  pi.ConfirmationMethod,
		"payment_method_types": pi.PaymentMethodTypes,
	}

	utils.SuccessResponse(c, http.StatusOK, "Stripe PaymentIntent details retrieved successfully", response)
}

// UserGetRefunds godoc
// @Summary Get user's refund history
// @Description Get all refunds for the authenticated user
// @Tags User - Payments
// @Security ApiKeyAuth
// @Param status query string false "Filter by status: pending, approved, processing, completed, failed, cancelled"
// @Param page query int false "Page number (default: 1)"
// @Param limit query int false "Items per page (default: 10)"
// @Param sort_by query string false "Sort by: created_at, amount, status (default: created_at)"
// @Param sort_order query string false "Sort order: asc, desc (default: desc)"
// @Produce json
// @Success 200 {object} utils.Response{data=[]models.RefundListResponse}
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/user/payments/refunds [get]
func (h *PaymentHandler) UserGetRefunds(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewUnauthorizedError("User not authenticated."))
		return
	}

	pagination := utils.GetPaginationParams(c, 10)
	status := c.Query("status")
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	// Validate sort parameters
	sortBy, sortOrder = utils.ValidateSortForRefunds(sortBy, sortOrder)

	userIDValue := userID.(uuid.UUID)
	refunds, total, err := h.paymentService.UserGetRefundsList(c.Request.Context(), userIDValue, status, pagination.Page, pagination.Limit, sortBy, sortOrder)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve refunds", err)
		return
	}

	response := map[string]interface{}{
		"refunds":    refunds,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Refunds retrieved successfully", response)
}

// GetUserRefundStatusHistory godoc
// @Summary Get refund status history
// @Description Get status change history for a specific refund belonging to the authenticated user
// @Tags User - Payments
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param refund_id path string true "Refund ID to get status history for"
// @Success 200 {object} utils.Response{data=object{status_history=[]models.RefundStatusHistoryResponse}}
// @Failure 400 {object} utils.Response "Invalid request"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 404 {object} utils.Response "Refund not found or doesn't belong to user"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/user/payments/refunds/{refund_id}/status-history [get]
func (h *PaymentHandler) GetUserRefundStatusHistory(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewUnauthorizedError("User not authenticated."))
		return
	}

	// Get refund_id from path parameter (required)
	refundIDStr := c.Param("refund_id")
	if refundIDStr == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "Refund ID is required", nil)
		return
	}

	refundID, err := uuid.Parse(refundIDStr)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid refund ID format", err)
		return
	}

	userUUID := userID.(uuid.UUID)
	history, err := h.paymentService.GetUserRefundStatusHistory(c.Request.Context(), userUUID, &refundID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve refund status history", err)
		return
	}

	response := map[string]interface{}{
		"status_history": history,
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund status history retrieved successfully", response)
}

// AdminInitiateRefund godoc
// @Summary Admin initiate refund
// @Description Admin can initiate refunds for transactions (single/multiple tickets)
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body object{transaction_id=string,amount=float64,ticket_ids=[]string,reason=string,refund_type=string} true "Refund details"
// @Success 200 {object} utils.Response{data=models.Refund}
// @Failure 400 {object} utils.Response "Invalid request"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 404 {object} utils.Response "Transaction not found"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/payments/refunds/initiate [post]
func (h *PaymentHandler) AdminInitiateRefund(c *gin.Context) {
	adminID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewUnauthorizedError("Admin not authenticated."))
		return
	}

	var req struct {
		PaymentIntentID string   `json:"payment_intent_id" binding:"required"`
		Amount          float64  `json:"amount,omitempty"` // Optional - calculated from tickets if not provided
		TicketIDs       []string `json:"ticket_ids" binding:"required,min=1"`
		Reason          string   `json:"reason" binding:"required,min=10,max=500"`
		RefundType      string   `json:"refund_type" binding:"required,oneof=full partial event_cancellation customer_request admin_action"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request payload", err)
		return
	}

	// Parse payment intent ID
	paymentIntentID, err := uuid.Parse(req.PaymentIntentID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid payment_intent_id format", err)
		return
	}

	// Parse ticket IDs
	ticketIDs := make([]uuid.UUID, 0, len(req.TicketIDs))
	for _, tidStr := range req.TicketIDs {
		tid, err := uuid.Parse(tidStr)
		if err != nil {
			utils.ErrorResponse(c, http.StatusBadRequest, "Invalid ticket_id format", err)
			return
		}
		ticketIDs = append(ticketIDs, tid)
	}

	adminIDValue := adminID.(uuid.UUID)
	refund, err := h.paymentService.AdminInitiateRefund(c.Request.Context(), paymentIntentID, adminIDValue, req.Amount, req.Reason, ticketIDs, req.RefundType)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund initiated successfully", refund)
}

// AdminRefundFullTransaction godoc
// @Summary Admin refund entire transaction
// @Description Admin can refund all tickets in a complete transaction (marks transaction and checkout as refunded)
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body object{transaction_id=string,reason=string,refund_type=string} true "Full transaction refund details"
// @Success 200 {object} utils.Response{data=models.Refund}
// @Failure 400 {object} utils.Response "Invalid request"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 404 {object} utils.Response "Transaction not found"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/payments/refunds/transaction [post]
func (h *PaymentHandler) AdminRefundFullTransaction(c *gin.Context) {
	adminID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewUnauthorizedError("Admin not authenticated."))
		return
	}

	var req struct {
		TransactionID string `json:"transaction_id" binding:"required"`
		Reason        string `json:"reason" binding:"required,min=10,max=500"`
		RefundType    string `json:"refund_type" binding:"required,oneof=full event_cancellation customer_request admin_action"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request payload", err)
		return
	}

	// Parse transaction ID
	transactionID, err := uuid.Parse(req.TransactionID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid transaction_id format", err)
		return
	}

	adminIDValue := adminID.(uuid.UUID)
	refund, err := h.paymentService.AdminRefundFullTransaction(c.Request.Context(), transactionID, adminIDValue, req.Reason, req.RefundType)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Full transaction refund initiated successfully", refund)
}

// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body object{event_id=string,reason=string,refund_type=string} true "Event refund details"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response "Invalid request"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 404 {object} utils.Response "Event not found"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/payments/refunds/event [post]
func (h *PaymentHandler) AdminRefundEventTickets(c *gin.Context) {
	adminID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewUnauthorizedError("Admin not authenticated."))
		return
	}

	var req struct {
		EventID    string `json:"event_id" binding:"required"`
		Reason     string `json:"reason" binding:"required,min=10,max=500"`
		RefundType string `json:"refund_type" binding:"required,oneof=event_cancellation admin_action"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request payload", err)
		return
	}

	// Parse event ID
	eventID, err := uuid.Parse(req.EventID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid event_id format", err)
		return
	}

	adminIDValue := adminID.(uuid.UUID)
	result, err := h.paymentService.AdminRefundEventTickets(c.Request.Context(), eventID, adminIDValue, req.Reason, req.RefundType)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event refunds processed successfully", result)
}
