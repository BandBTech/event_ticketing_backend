package handlers

import (
	"net/http"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// PaymentHandler handles all payment-related operations
type PaymentHandler struct {
	paymentService *services.PaymentService
	ticketService  *services.TicketService
	cfg            *config.Config
}

// NewPaymentHandler creates a new payment handler instance
func NewPaymentHandler(paymentService *services.PaymentService, ticketService *services.TicketService, cfg *config.Config) *PaymentHandler {
	return &PaymentHandler{
		paymentService: paymentService,
		ticketService:  ticketService,
		cfg:            cfg,
	}
}

// InitiatePayment godoc
// @Summary Initiate a payment
// @Description Create a payment intent, reserve tickets, and prepare payment with selected gateway. Works for both authenticated users and guests.
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

	// Initiate payment
	response, err := h.paymentService.InitiatePayment(c.Request.Context(), &req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to initiate payment", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment initiated successfully", response)
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
// @Description Retrieve all refund requests with filters
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param status query string false "Filter by status (pending, succeeded, failed, canceled)"
// @Param sort_by query string false "Sort by field (created_at, amount, status, refund_reason, processed_at)" default(created_at)
// @Param sort_order query string false "Sort order (asc, desc)" default(desc)
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/refunds [get]
// @Router /api/v1/admin/refunds [get]
func (h *PaymentHandler) AdminGetAllRefunds(c *gin.Context) {
	pagination := utils.GetPaginationParams(c, 10)
	status := c.Query("status")
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	// Validate sort parameters using centralized utility
	sortBy, sortOrder = utils.ValidateSortForRefunds(sortBy, sortOrder)

	refunds, total, err := h.paymentService.AdminGetAllRefunds(c.Request.Context(), status, pagination.Page, pagination.Limit, sortBy, sortOrder)
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
