package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/currency"
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

// AdminApproveForStripe godoc
// @Summary Approve a Stripe refund (Admin)
// @Description Approve a pending refund for a Stripe payment. Calls the Stripe API which moves
// @Description the refund to "processing". Completion is confirmed via the charge.refunded webhook.
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Param refund_id path string true "Refund ID"
// @Success 200 {object} utils.Response{data=models.Refund}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/refunds/{refund_id}/approve/stripe [post]
func (h *PaymentHandler) AdminApproveForStripe(c *gin.Context) {
	refundID, err := uuid.Parse(c.Param("refund_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid refund ID", err)
		return
	}

	adminIDInterface, _ := c.Get("userID")
	adminID := adminIDInterface.(uuid.UUID)

	refund, err := h.refundService.ApproveForStripe(c.Request.Context(), refundID, adminID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund submitted to Stripe — processing will complete via webhook", refund)
}

// AdminApproveForBillings godoc
// @Summary Approve a billing (konbini) refund (Admin)
// @Description Approve a pending refund for a konbini/cash payment. Creates a RefundBill with
// @Description bank-transfer details and immediately marks the refund as succeeded.
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param refund_id path string true "Refund ID"
// @Param request body services.ApproveForBillingsRequest true "Bank transfer details"
// @Success 200 {object} utils.Response{data=models.Refund}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/refunds/{refund_id}/approve/billings [post]
func (h *PaymentHandler) AdminApproveForBillings(c *gin.Context) {
	refundID, err := uuid.Parse(c.Param("refund_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid refund ID", err)
		return
	}

	var req services.ApproveForBillingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request", err)
		return
	}

	adminIDInterface, _ := c.Get("userID")
	adminID := adminIDInterface.(uuid.UUID)

	refund, err := h.refundService.ApproveForBillings(c.Request.Context(), refundID, adminID, req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund approved and billing record created", refund)
}

// AdminCreateRefund godoc
// @Summary Create a refund for a ticket (Admin)
// @Description Admin cancels a ticket by ticket ID or ticket number and creates a pending refund.
// @Description The refund must then be approved (stripe or billing) or rejected.
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body services.AdminCancelTicketRequest true "Ticket identifier and reason"
// @Success 201 {object} utils.Response{data=models.Refund}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/refunds [post]
func (h *PaymentHandler) AdminCreateRefund(c *gin.Context) {
	var req services.AdminCancelTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request", err)
		return
	}

	adminIDInterface, _ := c.Get("userID")
	adminID := adminIDInterface.(uuid.UUID)

	refund, err := h.refundService.AdminCancelTicket(c.Request.Context(), req, adminID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Ticket cancelled and refund created — awaiting admin approval", refund)
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

	refundResponses, err := h.buildRefundListResponses(c.Request.Context(), refunds)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to format refund list", err)
		return
	}

	response := map[string]interface{}{
		"refunds":    refundResponses,
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
// @Success 200 {object} utils.Response{data=models.Refund}
// @Failure 401 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Router /api/v1/admin/payments/refunds/{refund_id} [get]
func (h *PaymentHandler) AdminGetRefund(c *gin.Context) {
	refundID, err := uuid.Parse(c.Param("refund_id"))
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

	refundResponse, err := h.buildRefundDetailResponse(c.Request.Context(), refund)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to format refund details", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund retrieved successfully", refundResponse)
}

// AdminGetRefundStatusHistory godoc
// @Summary Get refund status history (Admin)
// @Description Get status change history for any refund
// @Tags Admin - Payments
// @Security ApiKeyAuth
// @Produce json
// @Param refund_id path string true "Refund ID"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/refunds/{refund_id}/status-history [get]
func (h *PaymentHandler) AdminGetRefundStatusHistory(c *gin.Context) {
	refundID, err := uuid.Parse(c.Param("refund_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid refund ID format", err)
		return
	}

	history, err := h.refundService.AdminGetRefundStatusHistory(c.Request.Context(), refundID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve refund status history", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund status history retrieved successfully", map[string]interface{}{
		"status_history": history,
	})
}

// UserGetRefunds godoc
// @Summary Get user's refund history
// @Description Get all refunds for the authenticated user
// @Tags User - Payments
// @Security ApiKeyAuth
// @Param page query int false "Page number (default: 1)"
// @Param limit query int false "Items per page (default: 10)"
// @Produce json
// @Success 200 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/payments/refunds [get]
func (h *PaymentHandler) UserGetRefunds(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewUnauthorizedError("User not authenticated."))
		return
	}

	pagination := utils.GetPaginationParams(c, 10)
	userIDValue := userID.(uuid.UUID)

	refunds, total, err := h.refundService.GetUserRefunds(c.Request.Context(), userIDValue, pagination.Page, pagination.Limit)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve refunds", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Refunds retrieved successfully", map[string]interface{}{
		"refunds":    refunds,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	})
}

// GetUserRefundStatusHistory godoc
// @Summary Get refund status history (User)
// @Description Get status change history for a specific refund belonging to the authenticated user
// @Tags User - Payments
// @Security ApiKeyAuth
// @Produce json
// @Param refund_id path string true "Refund ID"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/payments/refunds/{refund_id}/status-history [get]
func (h *PaymentHandler) GetUserRefundStatusHistory(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewUnauthorizedError("User not authenticated."))
		return
	}

	refundID, err := uuid.Parse(c.Param("refund_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid refund ID format", err)
		return
	}

	history, err := h.refundService.GetUserRefundStatusHistory(c.Request.Context(), userID.(uuid.UUID), refundID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Refund status history retrieved successfully", map[string]interface{}{
		"status_history": history,
	})
}

func (h *PaymentHandler) buildRefundListResponses(ctx context.Context, refunds []models.Refund) ([]models.RefundListResponse, error) {
	if len(refunds) == 0 {
		return []models.RefundListResponse{}, nil
	}

	initiatorIDs := make([]uuid.UUID, 0, len(refunds))
	seen := make(map[uuid.UUID]struct{}, len(refunds))
	for _, refund := range refunds {
		if _, ok := seen[refund.InitiatedBy]; ok {
			continue
		}
		seen[refund.InitiatedBy] = struct{}{}
		initiatorIDs = append(initiatorIDs, refund.InitiatedBy)
	}

	var initiators []models.User
	if err := database.GetDB().WithContext(ctx).
		Select("id", "first_name", "last_name", "email").
		Where("id IN ?", initiatorIDs).
		Find(&initiators).Error; err != nil {
		return nil, err
	}

	initiatorMap := make(map[uuid.UUID]models.User, len(initiators))
	for _, initiator := range initiators {
		initiatorMap[initiator.ID] = initiator
	}

	response := make([]models.RefundListResponse, 0, len(refunds))
	for _, refund := range refunds {
		amount, _ := currency.FromSmallestUnit(refund.Amount, refund.Currency)
		requestedAt := refund.CreatedAt
		ticketCount := 0
		if refund.TicketID != uuid.Nil {
			ticketCount = 1
		}

		item := models.RefundListResponse{
			ID:            refund.ID,
			RefundNumber:  refund.RefundNumber,
			TransactionID: refund.TransactionID,
			Amount:        amount,
			Currency:      refund.Currency,
			Reason:        refund.Reason,
			RefundType:    deriveRefundType(refund),
			Status:        string(refund.Status),
			TicketCount:   ticketCount,
			RequestedAt:   &requestedAt,
			CreatedAt:     refund.CreatedAt,
			UpdatedAt:     refund.UpdatedAt,
		}

		if initiator, ok := initiatorMap[refund.InitiatedBy]; ok {
			item.InitiatedBy = &models.RefundUserInfo{
				ID:    initiator.ID,
				Name:  strings.TrimSpace(initiator.FirstName + " " + initiator.LastName),
				Email: initiator.Email,
			}
		}

		response = append(response, item)
	}

	return response, nil
}

func (h *PaymentHandler) buildRefundDetailResponse(ctx context.Context, refund *models.Refund) (*models.RefundDetailResponse, error) {
	var transaction models.Transaction
	if err := database.GetDB().WithContext(ctx).First(&transaction, refund.TransactionID).Error; err != nil {
		return nil, err
	}

	event := refund.Event
	if event == nil {
		var loadedEvent models.Event
		if err := database.GetDB().WithContext(ctx).First(&loadedEvent, refund.EventID).Error; err != nil {
			return nil, err
		}
		event = &loadedEvent
	}

	var initiator models.User
	if err := database.GetDB().WithContext(ctx).
		Select("id", "first_name", "last_name", "email").
		First(&initiator, refund.InitiatedBy).Error; err != nil {
		return nil, err
	}

	var organizer models.User
	if event.OrganizerID != uuid.Nil {
		_ = database.GetDB().WithContext(ctx).
			Preload("OrganizerOnboarding").
			Select("id", "first_name", "last_name").
			First(&organizer, event.OrganizerID).Error
	}

	refundAmount, _ := currency.FromSmallestUnit(refund.Amount, refund.Currency)
	transactionAmount, _ := currency.FromSmallestUnit(transaction.AmountTotal, transaction.Currency)
	requestedAt := refund.CreatedAt
	affectedTicketIDs := make([]string, 0, 1)
	if refund.TicketID != uuid.Nil {
		affectedTicketIDs = append(affectedTicketIDs, refund.TicketID.String())
	}

	response := &models.RefundDetailResponse{
		ID:           refund.ID,
		RefundNumber: refund.RefundNumber,
		Transaction: models.RefundTransactionInfo{
			ID:        transaction.ID,
			Amount:    transactionAmount,
			Gateway:   string(transaction.PaymentGateway),
			Status:    string(transaction.Status),
			CreatedAt: transaction.CreatedAt,
		},
		Event: &models.RefundEventInfo{
			ID:          event.ID,
			Title:       event.Title,
			BannerImage: event.BannerImage,
		},
		InitiatedBy: &models.RefundUserInfo{
			ID:    initiator.ID,
			Name:  strings.TrimSpace(initiator.FirstName + " " + initiator.LastName),
			Email: initiator.Email,
		},
		Amount:            refundAmount,
		Currency:          refund.Currency,
		Reason:            refund.Reason,
		RefundType:        deriveRefundType(*refund),
		Status:            string(refund.Status),
		AffectedTicketIDs: affectedTicketIDs,
		TicketCount:       len(affectedTicketIDs),
		RequestedAt:       &requestedAt,
		CreatedAt:         refund.CreatedAt,
		UpdatedAt:         refund.UpdatedAt,
	}

	organizerName := strings.TrimSpace(organizer.FirstName + " " + organizer.LastName)
	if organizer.OrganizerOnboarding != nil && strings.TrimSpace(organizer.OrganizerOnboarding.BusinessName) != "" {
		organizerName = strings.TrimSpace(organizer.OrganizerOnboarding.BusinessName)
	}
	if organizer.ID != uuid.Nil {
		response.Organizer = &models.RefundOrganizerInfo{
			ID:   organizer.ID,
			Name: organizerName,
		}
	}

	return response, nil
}

func deriveRefundType(refund models.Refund) string {
	if refund.InitiatorType == "admin" {
		return "admin_action"
	}
	return "customer_request"
}
