package handlers

import (
	"fmt"
	"net/http"

	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// PaymentHandler handles all payment-related operations
type PaymentHandler struct {
	paymentService *services.PaymentService
	cfg            *config.Config
}

// NewPaymentHandler creates a new payment handler instance
func NewPaymentHandler(paymentService *services.PaymentService, cfg *config.Config) *PaymentHandler {
	return &PaymentHandler{
		paymentService: paymentService,
		cfg:            cfg,
	}
}

// GetAvailableGateways godoc
// @Summary Get available payment gateways
// @Description Get list of available and enabled payment gateways for a specific currency and optional country
// @Tags Payments
// @Produce json
// @Param country query string false "Country code (ISO 3166-1 alpha-2) for gateway filtering" example(US)
// @Param currency query string true "Currency code (ISO 4217) for gateway filtering" example(USD)
// @Success 200 {object} utils.Response{data=[]gateways.GatewayInfo} "List of available payment gateways"
// @Failure 400 {object} utils.Response "Missing or invalid currency parameter"
// @Failure 500 {object} utils.Response "Internal server error retrieving gateways"
// @Router /api/v1/payments/gateways [get]
func (h *PaymentHandler) GetAvailableGateways(c *gin.Context) {
	country := c.Query("country")
	currency := c.Query("currency")

	if currency == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "currency is required", nil)
		return
	}

	availableGateways, err := h.paymentService.GetAvailableGateways(c.Request.Context(), country, currency)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to get available gateways", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Available payment gateways retrieved successfully", availableGateways)
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

// HandleWebhook godoc
// @Summary Handle payment gateway webhooks (Dynamic)
// @Description Process webhook events from any configured payment gateway
// @Tags Payments
// @Accept json
// @Produce json
// @Param gateway path string true "Gateway name (stripe, paypal, esewa, khalti, etc.)"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/webhooks/{gateway} [post]
func (h *PaymentHandler) HandleWebhook(c *gin.Context) {
	// Get gateway name from URL path
	gatewayName := c.Param("gateway")
	if gatewayName == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "Gateway name is required", nil)
		return
	}

	// Read webhook payload
	payload, err := c.GetRawData()
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Failed to read request body", err)
		return
	}

	// Get signature from appropriate header based on gateway
	var signature string
	switch gatewayName {
	case "stripe":
		signature = c.GetHeader("Stripe-Signature")
	case "paypal":
		signature = c.GetHeader("PayPal-Transmission-Sig")
	case "esewa":
		signature = c.GetHeader("X-eSewa-Signature")
	case "khalti":
		signature = c.GetHeader("Khalti-Signature")
	case "razorpay":
		signature = c.GetHeader("X-Razorpay-Signature")
	default:
		// Try generic signature header
		signature = c.GetHeader("X-Webhook-Signature")
	}

	if signature == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, fmt.Sprintf("Missing signature header for %s webhook", gatewayName), nil)
		return
	}

	// Process webhook using the gateway service
	if err := h.paymentService.HandleWebhook(c.Request.Context(), gatewayName, payload, signature); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to process webhook", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Webhook processed successfully", nil)
}

// Legacy handlers for backward compatibility - can be removed after gateway migration
// HandleStripeWebhook is deprecated - use HandleWebhook instead
func (h *PaymentHandler) HandleStripeWebhook(c *gin.Context) {
	c.Params = append(c.Params, gin.Param{Key: "gateway", Value: "stripe"})
	h.HandleWebhook(c)
}

// HandlePayPalWebhook is deprecated - use HandleWebhook instead
func (h *PaymentHandler) HandlePayPalWebhook(c *gin.Context) {
	c.Params = append(c.Params, gin.Param{Key: "gateway", Value: "paypal"})
	h.HandleWebhook(c)
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

// RequestRefund godoc
// @Summary Request a refund
// @Description Request a refund for a completed payment (requires admin approval)
// @Tags Payments
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body map[string]interface{} true "Refund request details"
// @Success 200 {object} utils.Response{data=models.Refund}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/payments/refund [post]
func (h *PaymentHandler) RequestRefund(c *gin.Context) {
	var req struct {
		PaymentIntentID uuid.UUID   `json:"payment_intent_id" binding:"required"`
		Reason          string      `json:"reason" binding:"required"`
		TicketIDs       []uuid.UUID `json:"ticket_ids" binding:"required,min=1"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	// Get user ID from context
	userIDInterface, _ := c.Get("userID")
	userID := userIDInterface.(uuid.UUID)

	refund, err := h.paymentService.RequestRefund(c.Request.Context(), req.PaymentIntentID, userID, req.Reason, req.TicketIDs)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to request refund", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund request submitted successfully", refund)
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
// @Router /api/v1/admin/refunds/{refund_id}/approve [post]
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
// @Router /api/v1/admin/refunds/{refund_id}/reject [post]
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
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/refunds [get]
func (h *PaymentHandler) AdminGetAllRefunds(c *gin.Context) {
	pagination := utils.GetPaginationParams(c, 10)
	status := c.Query("status")

	refunds, total, err := h.paymentService.AdminGetAllRefunds(c.Request.Context(), status, pagination.Page, pagination.Limit)
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

// AdminManageGateways godoc
// @Summary Manage payment gateway configurations (Admin)
// @Description Get, create, update, or delete payment gateway configurations
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} utils.Response{data=[]models.PaymentGatewayConfig}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payment-gateways [get]
func (h *PaymentHandler) AdminGetGatewayConfigs(c *gin.Context) {
	configs, err := h.paymentService.GetGatewayConfigs(c.Request.Context())
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve gateway configurations", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Gateway configurations retrieved successfully", configs)
}

// GetPaymentAnalytics godoc
// @Summary Get payment analytics (Admin)
// @Description Get payment statistics and analytics
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Param start_date query string false "Start date (YYYY-MM-DD)"
// @Param end_date query string false "End date (YYYY-MM-DD)"
// @Success 200 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/analytics [get]
func (h *PaymentHandler) GetPaymentAnalytics(c *gin.Context) {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")

	analytics, err := h.paymentService.GetPaymentAnalytics(c.Request.Context(), startDate, endDate)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve payment analytics", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment analytics retrieved successfully", analytics)
}

// GetAuditLogs godoc
// @Summary Get payment audit logs (Admin)
// @Description Query payment audit logs with filtering
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Param page query int false "Page number"
// @Param limit query int false "Items per page"
// @Param action query string false "Filter by action (e.g., payment_created, refund_approved)"
// @Param entity_type query string false "Filter by entity type (e.g., payment_intent, refund)"
// @Param entity_id query string false "Filter by entity ID"
// @Param actor_id query string false "Filter by actor ID"
// @Success 200 {object} utils.Response{data=[]models.PaymentAuditLog}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/audit-logs [get]
func (h *PaymentHandler) GetAuditLogs(c *gin.Context) {
	pagination := utils.GetPaginationParams(c, 50)

	// Build query filters
	filters := map[string]interface{}{}
	if action := c.Query("action"); action != "" {
		filters["action"] = action
	}
	if entityType := c.Query("entity_type"); entityType != "" {
		filters["entity_type"] = entityType
	}
	if entityID := c.Query("entity_id"); entityID != "" {
		if id, err := uuid.Parse(entityID); err == nil {
			filters["entity_id"] = id
		}
	}
	if actorID := c.Query("actor_id"); actorID != "" {
		if id, err := uuid.Parse(actorID); err == nil {
			filters["actor_id"] = id
		}
	}

	logs, total, err := h.paymentService.GetAuditLogs(c.Request.Context(), pagination.Page, pagination.Limit, filters)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve audit logs", err)
		return
	}

	response := map[string]interface{}{
		"logs":       logs,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Audit logs retrieved successfully", response)
}

// RetryWebhook godoc
// @Summary Retry failed webhook (Admin)
// @Description Retry processing a failed webhook event
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Param id path string true "Webhook Event ID"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/webhooks/{id}/retry [post]
func (h *PaymentHandler) RetryWebhook(c *gin.Context) {
	webhookID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid webhook ID", err)
		return
	}

	err = h.paymentService.RetryWebhook(c.Request.Context(), webhookID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retry webhook", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Webhook retry initiated successfully", nil)
}

// RetryTransaction godoc
// @Summary Retry failed transaction (Admin)
// @Description Retry processing a failed payment intent
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Param id path string true "Payment Intent ID"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/transactions/{id}/retry [post]
func (h *PaymentHandler) RetryTransaction(c *gin.Context) {
	paymentIntentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid payment intent ID", err)
		return
	}

	err = h.paymentService.RetryTransaction(c.Request.Context(), paymentIntentID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retry transaction", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Transaction retry initiated successfully", nil)
}

// ReencryptGatewayConfigs godoc
// @Summary Re-encrypt all gateway configs (Admin)
// @Description Re-encrypt all gateway credentials with the current encryption key
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/gateways/reencrypt [post]
func (h *PaymentHandler) ReencryptGatewayConfigs(c *gin.Context) {
	count, err := h.paymentService.ReencryptAllGatewayConfigs(c.Request.Context())
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to re-encrypt gateway configs", err)
		return
	}

	response := map[string]interface{}{
		"message":         "Gateway configurations re-encrypted successfully",
		"configs_updated": count,
		"new_key_version": h.cfg.Security.CurrentKeyID,
	}

	utils.SuccessResponse(c, http.StatusOK, fmt.Sprintf("Successfully re-encrypted %d gateway configurations", count), response)
}
