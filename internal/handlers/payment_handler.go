package handlers

import (
	"net/http"
	"strings"
	"time"

	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// PaymentHandler handles all payment-related operations
type PaymentHandler struct {
	paymentService              *services.PaymentService
	refundService               *services.RefundService
	ticketService               *services.TicketService
	unifiedPurchaseOrchestrator *services.PurchaseOrchestrator
	cfg                         *config.Config
	stripeAPIKey                string
}

// NewPaymentHandler creates a new payment handler instance
func NewPaymentHandler(
	paymentService *services.PaymentService,
	refundService *services.RefundService,
	ticketService *services.TicketService,
	unifiedOrchestrator *services.PurchaseOrchestrator,
	cfg *config.Config,
) *PaymentHandler {
	return &PaymentHandler{
		paymentService:              paymentService,
		refundService:               refundService,
		ticketService:               ticketService,
		unifiedPurchaseOrchestrator: unifiedOrchestrator,
		cfg:                         cfg,
		stripeAPIKey:                cfg.Payment.Gateways.StripeAPIKey,
	}
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

	_, err = h.refundService.ApproveRefund(c.Request.Context(), refundID, adminID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to approve refund", err)
		return
	}

	utils.SuccessResponseWithoutData(c, http.StatusOK, "Refund approved and processed successfully")
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

	refund, err := h.refundService.RejectRefund(c.Request.Context(), refundID, adminID, req.Reason)
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

	refunds, total, err := h.refundService.AdminGetAllRefundsList(c.Request.Context(), status, search, refundType, startDate, endDate, pagination.Page, pagination.Limit, sortBy, sortOrder)
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

	refund, err := h.refundService.AdminGetRefund(c.Request.Context(), refundID)
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

// AdminGetRefundStatusHistory godoc
// @Summary Get refund status history (Admin)
// @Description Get status change history for any refund
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param refund_id path string true "Refund ID to get status history for"
// @Success 200 {object} utils.Response{data=object{status_history=[]models.RefundStatusHistoryResponse}}
// @Failure 400 {object} utils.Response "Invalid request"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 404 {object} utils.Response "Refund not found"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/payments/refunds/{refund_id}/status-history [get]
func (h *PaymentHandler) AdminGetRefundStatusHistory(c *gin.Context) {
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

	history, err := h.refundService.AdminGetRefundStatusHistory(c.Request.Context(), refundID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve refund status history", err)
		return
	}

	response := map[string]interface{}{
		"status_history": history,
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund status history retrieved successfully", response)
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

	// Get admin ID from context
	adminIDInterface, _ := c.Get("userID")
	adminID := adminIDInterface.(uuid.UUID)

	refund, err := h.refundService.RetryFailedRefund(c.Request.Context(), refundID, adminID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund retry initiated. Processing async...", refund)
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

	eligible, reason, err := h.refundService.CheckRefundEligibility(req.TicketIDs)
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
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	// Validate sort parameters
	sortBy, sortOrder = utils.ValidateSortForRefunds(sortBy, sortOrder)

	userIDValue := userID.(uuid.UUID)
	refunds, total, err := h.refundService.GetUserRefunds(c.Request.Context(), userIDValue, pagination.Page, pagination.Limit)
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
	history, err := h.refundService.GetUserRefundStatusHistory(c.Request.Context(), userUUID, refundID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve refund status history", err)
		return
	}

	response := map[string]interface{}{
		"status_history": history,
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund status history retrieved successfully", response)
}
