package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/currency"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PayoutService handles payout request operations
type PayoutService struct {
	db *gorm.DB
}

// NewPayoutService creates a new payout service
func NewPayoutService() *PayoutService {
	return &PayoutService{
		db: database.DB,
	}
}

// logAudit creates audit log entries for payout operations
func (s *PayoutService) logAudit(ctx context.Context, action, entityType string, entityID uuid.UUID, actorID *uuid.UUID, actorType string, eventID *uuid.UUID, changes map[string]interface{}) {
	LogPaymentAuditAsync(s.db, action, entityType, entityID, actorID, actorType, eventID, changes)
}

// generateBillNumber creates a unique bill identifier
func (s *PayoutService) generateBillNumber() string {
	now := time.Now()
	return "BILL-" + now.Format("20060102") + "-" + uuid.New().String()[:8]
}

// CreatePayoutRequest creates a new payout request from organizer
func (s *PayoutService) CreatePayoutRequest(organizerID uuid.UUID, req *models.PayoutRequestCreate) error {
	// Validate organizer
	var organizer models.User
	if err := s.db.Where("id = ?", organizerID).First(&organizer).Error; err != nil {
		return utils.NewNotFoundError("organizer")
	}

	// Check if there are any non-cancelled payout requests for this event
	var nonCancelledRequestCount int64
	if err := s.db.Model(&models.PayoutRequest{}).
		Where("organizer_id = ? AND event_id = ? AND status != 'cancelled'", organizerID, req.EventID).
		Count(&nonCancelledRequestCount).Error; err != nil {
		return utils.NewDatabaseError("Failed to check existing payout requests.", err)
	}
	if nonCancelledRequestCount > 0 {
		return utils.NewBusinessLogicError("You have active payout requests for this event. Please cancel all existing requests before submitting a new one.")
	}

	// Verify event ownership and calculate available amount
	var event models.Event
	if err := s.db.Where("id = ? AND organizer_id = ?", req.EventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewNotFoundError("event")
		}
		return utils.NewDatabaseError("Failed to retrieve event.", err)
	}

	// Validate that payout can only be requested for completed events after the end date
	now := time.Now()
	eventHasEnded := event.EndDate.Before(now) || event.EndDate.Equal(now)

	// Check if event status is completed and event has ended
	if event.Status != "completed" {
		return utils.NewBusinessLogicError("Payout requests can only be made for events with status 'completed'.")
	}

	if !eventHasEnded {
		return utils.NewBusinessLogicError("Payout requests can only be made after the event end date has passed.")
	}

	// Calculate available amount from transactions
	var totalEarnings float64
	var totalPaid float64

	// Get total earnings for this event
	s.db.Model(&models.Transaction{}).
		Where("event_id = ? AND status IN ?", req.EventID, []string{"succeeded", "completed"}).
		Select("COALESCE(SUM(organizer_share), 0)").
		Scan(&totalEarnings)

	// Get total paid for this event from existing bills
	s.db.Model(&models.PaymentBill{}).
		Where("event_id = ? AND status IN ('paid', 'partially_paid')", req.EventID).
		Select("COALESCE(SUM(paid_amount), 0)").
		Scan(&totalPaid)

	availableAmount := totalEarnings - totalPaid

	requestedSmallest, convErr := currency.ToSmallestUnit(req.Amount, event.Currency)
	if convErr != nil {
		return utils.NewValidationError("Invalid payout amount for event currency.", nil)
	}
	requestedAmount := float64(requestedSmallest)

	// Check if requested amount is available
	if requestedAmount > availableAmount {
		return utils.NewBusinessLogicError(fmt.Sprintf("Requested amount exceeds available amount for this event."))
	}

	payoutRequest := &models.PayoutRequest{
		OrganizerID: organizerID,
		EventID:     req.EventID,
		Amount:      requestedAmount,
		RequestType: req.RequestType,
		Description: req.Description,
		Status:      "pending",
	}

	if err := s.db.Create(payoutRequest).Error; err != nil {
		return utils.NewDatabaseError("Failed to create payout request.", err)
	}

	// Log audit for payout request creation
	s.logAudit(context.Background(), "payout_requested", "payout_request", payoutRequest.ID, &organizerID, "organizer", &req.EventID, map[string]interface{}{
		"amount":       requestedAmount,
		"request_type": req.RequestType,
		"description":  req.Description,
		"organizer_id": organizerID,
	})

	return nil
}

// GetOrganizerPayoutRequests gets payout requests for an organizer with sorting
func (s *PayoutService) GetOrganizerPayoutRequests(organizerID uuid.UUID, page, limit int, status, sortBy, sortOrder string) ([]models.OrganizerPayoutRequestListResponse, int64, error) {
	var requests []models.PayoutRequest
	var total int64

	// Normalize sort order to lowercase to handle both lowercase and uppercase values from handler
	sortOrder = strings.ToLower(sortOrder)

	query := s.db.Model(&models.PayoutRequest{}).Where("payout_requests.organizer_id = ?", organizerID)

	if status != "" {
		query = query.Where("payout_requests.status = ?", status)
	}

	// Get total count
	query.Count(&total)

	// Validate sort parameters - use the same as admin
	validSortFields := map[string]bool{
		"date":           true, // maps to created_at
		"created_at":     true,
		"amount":         true,
		"event_title":    true,
		"status":         true,
		"request_number": true,
	}
	if !validSortFields[sortBy] {
		sortBy = "created_at"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	// Map "date" to "created_at"
	if sortBy == "date" {
		sortBy = "created_at"
	}

	// Apply sorting - handle related fields and case-insensitive sorting
	var orderClause string
	switch sortBy {
	case "event_title":
		orderClause = fmt.Sprintf("LOWER(events.title) %s", sortOrder)
		// Join with events table for sorting
		query = query.Joins("LEFT JOIN events ON payout_requests.event_id = events.id")
	case "status":
		orderClause = utils.GenerateOrderByClause("payout_requests.status", sortOrder)
	case "amount":
		// Amount is numeric, no LOWER needed
		orderClause = "payout_requests.amount " + sortOrder
	case "created_at":
		// Use table prefix for created_at
		orderClause = "payout_requests.created_at " + sortOrder
	case "request_number":
		orderClause = utils.GenerateOrderByClause("payout_requests.request_number", sortOrder)
	default:
		orderClause = utils.GenerateOrderByClause(sortBy, sortOrder)
	}

	// Get paginated results with preloaded relations
	offset := (page - 1) * limit
	if err := query.Order(orderClause).Preload("Event").
		Offset(offset).Limit(limit).Find(&requests).Error; err != nil {
		return nil, 0, err
	}

	// Convert to response format
	var responses []models.OrganizerPayoutRequestListResponse
	for _, req := range requests {
		responses = append(responses, req.ToOrganizerListResponse())
	}

	return responses, total, nil
}

// GetAllPayoutRequests gets all payout requests (admin only) with sorting
func (s *PayoutService) GetAllPayoutRequests(page, limit int, status, sortBy, sortOrder string) ([]models.AdminPayoutRequestListResponse, int64, error) {
	var requests []models.PayoutRequest
	var total int64

	// Normalize sort order to lowercase to handle both lowercase and uppercase values from handler
	sortOrder = strings.ToLower(sortOrder)

	query := s.db.Model(&models.PayoutRequest{})

	if status != "" {
		query = query.Where("payout_requests.status = ?", status)
	}

	// Get total count
	query.Count(&total)

	// Validate sort parameters - include date, event_title, amount, status, request_number
	validSortFields := map[string]bool{
		"date":           true, // maps to created_at
		"created_at":     true,
		"amount":         true,
		"event_title":    true,
		"status":         true,
		"request_number": true,
	}
	if !validSortFields[sortBy] {
		sortBy = "created_at"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	// Map "date" to "created_at"
	if sortBy == "date" {
		sortBy = "created_at"
	}

	// Apply sorting - handle related fields and case-insensitive sorting
	var orderClause string
	switch sortBy {
	case "event_title":
		orderClause = fmt.Sprintf("LOWER(events.title) %s", sortOrder)
		// Join with events table for sorting
		query = query.Joins("LEFT JOIN events ON payout_requests.event_id = events.id")
	case "status":
		orderClause = utils.GenerateOrderByClause("payout_requests.status", sortOrder)
	case "amount":
		// Amount is numeric, no LOWER needed
		orderClause = "payout_requests.amount " + sortOrder
	case "created_at":
		// Use table prefix for created_at
		orderClause = "payout_requests.created_at " + sortOrder
	case "request_number":
		orderClause = utils.GenerateOrderByClause("payout_requests.request_number", sortOrder)
	default:
		orderClause = utils.GenerateOrderByClause(sortBy, sortOrder)
	}

	// Get paginated results with preloaded relations
	offset := (page - 1) * limit
	if err := query.Order(orderClause).Preload("Event").
		Offset(offset).Limit(limit).Find(&requests).Error; err != nil {
		return nil, 0, err
	}

	// Convert to response format
	var responses []models.AdminPayoutRequestListResponse
	for _, req := range requests {
		responses = append(responses, req.ToAdminListResponse())
	}

	return responses, total, nil
}

// GetPayoutRequestByID gets a specific payout request
func (s *PayoutService) GetPayoutRequestByID(requestID uuid.UUID, organizerID *uuid.UUID) (*models.PayoutRequestResponse, error) {
	var payoutRequest models.PayoutRequest

	query := s.db.Preload("Event").Preload("Organizer").Preload("PaymentBill")

	// If organizerID is provided, restrict to that organizer
	if organizerID != nil {
		query = query.Where("id = ? AND organizer_id = ?", requestID, *organizerID)
	} else {
		query = query.Where("id = ?", requestID)
	}

	if err := query.First(&payoutRequest).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payout request")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve payout request.", err)
	}

	// Get event and organizer for response
	var event *models.EventSummaryResponse
	if payoutRequest.Event != nil {
		summary := payoutRequest.Event.ToSummaryResponse()
		event = &summary
	}

	var organizer *models.User
	if payoutRequest.Organizer != nil {
		organizer = payoutRequest.Organizer
	}

	// Get payment history if there's a bill
	var paymentHistory []models.PaymentHistory
	var billSummary *models.BillPaymentSummary

	if payoutRequest.PaymentBillID != nil && payoutRequest.PaymentBill != nil {
		if err := s.db.Where("payment_bill_id = ?", *payoutRequest.PaymentBillID).
			Preload("ProcessedBy").
			Order("paid_at DESC").
			Find(&paymentHistory).Error; err != nil {
			// Log error but don't fail the request
			fmt.Printf("[ERROR] Failed to load payment history for bill %s: %v\n", *payoutRequest.PaymentBillID, err)
		} else {
			// Calculate bill summary
			summary := s.calculateBillPaymentSummary(payoutRequest.PaymentBill, paymentHistory)
			billSummary = &summary
		}
	}

	// Convert payment history to response format
	paymentHistoryResponses := make([]models.PaymentHistoryResponse, len(paymentHistory))
	for i, payment := range paymentHistory {
		paymentHistoryResponses[i] = payment.ToResponse()
	}

	// Populate response
	response := models.PayoutRequestResponse{
		ID:              payoutRequest.ID,
		RequestNumber:   payoutRequest.RequestNumber,
		OrganizerID:     payoutRequest.OrganizerID,
		Organizer:       organizer,
		EventID:         payoutRequest.EventID,
		Event:           event,
		RequestedAmount: payoutRequest.Amount,
		Status:          payoutRequest.Status,
		RequestType:     payoutRequest.RequestType,
		RequestDate:     payoutRequest.CreatedAt,
		Description:     payoutRequest.Description,
		AdminNotes:      payoutRequest.AdminNotes,
		ProcessedBy:     payoutRequest.ProcessedBy,
		ProcessedDate:   payoutRequest.ProcessedAt,
		BillSummary:     billSummary,
		PaymentHistory:  paymentHistoryResponses,
		CreatedAt:       payoutRequest.CreatedAt,
		UpdatedAt:       payoutRequest.UpdatedAt,
	}

	return &response, nil
}

// calculateBillPaymentSummary calculates payment summary for a bill
func (s *PayoutService) calculateBillPaymentSummary(bill *models.PaymentBill, paymentHistory []models.PaymentHistory) models.BillPaymentSummary {
	totalBilled := bill.Amount
	totalPaid := bill.PaidAmount
	remaining := bill.Amount - bill.PaidAmount
	if remaining < 0 {
		remaining = 0
	}
	if bill.Currency != "" {
		if v, err := currency.FromSmallestUnit(int64(totalBilled), bill.Currency); err == nil {
			totalBilled = v
		}
		if v, err := currency.FromSmallestUnit(int64(totalPaid), bill.Currency); err == nil {
			totalPaid = v
		}
		if v, err := currency.FromSmallestUnit(int64(remaining), bill.Currency); err == nil {
			remaining = v
		}
	}

	summary := models.BillPaymentSummary{
		TotalBilled:     totalBilled,
		TotalPaid:       totalPaid,
		RemainingAmount: remaining,
		PendingAmount:   remaining, // For active bills, pending = remaining
		PaymentCount:    len(paymentHistory),
	}

	// If bill is fully paid, pending amount is 0
	if bill.Status == "paid" {
		summary.PendingAmount = 0
	}

	// Find last payment date
	if len(paymentHistory) > 0 {
		lastPayment := paymentHistory[0] // Already ordered by paid_at DESC
		summary.LastPaymentDate = &lastPayment.PaidAt
	}

	return summary
}

// GetOrganizerPayoutRequestDetail gets detailed payout request for organizer
func (s *PayoutService) GetOrganizerPayoutRequestDetail(requestID uuid.UUID, organizerID uuid.UUID) (*models.OrganizerPayoutRequestDetailResponse, error) {
	var payoutRequest models.PayoutRequest

	query := s.db.Preload("Event").Preload("PaymentBill").
		Where("id = ? AND organizer_id = ?", requestID, organizerID)

	if err := query.First(&payoutRequest).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payout request")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve payout request.", err)
	}

	// Get payment history if there's a bill
	var paymentHistory []models.PaymentHistory
	var billSummary *models.BillPaymentSummary

	if payoutRequest.PaymentBillID != nil && payoutRequest.PaymentBill != nil {
		if err := s.db.Where("payment_bill_id = ?", *payoutRequest.PaymentBillID).
			Preload("ProcessedBy").
			Order("paid_at DESC").
			Find(&paymentHistory).Error; err != nil {
			// Log error but don't fail the request
			fmt.Printf("[ERROR] Failed to load payment history for bill %s: %v\n", *payoutRequest.PaymentBillID, err)
		} else {
			// Calculate bill summary
			summary := s.calculateBillPaymentSummary(payoutRequest.PaymentBill, paymentHistory)
			billSummary = &summary
		}
	}

	// Convert payment history to response format
	paymentHistoryResponses := make([]models.PaymentHistoryResponse, len(paymentHistory))
	for i, payment := range paymentHistory {
		paymentHistoryResponses[i] = payment.ToResponse()
	}

	response := payoutRequest.ToOrganizerDetailResponse(billSummary, paymentHistoryResponses)
	return &response, nil
}

// GetAdminPayoutRequestDetail gets detailed payout request for admin
func (s *PayoutService) GetAdminPayoutRequestDetail(requestID uuid.UUID) (*models.AdminPayoutRequestDetailResponse, error) {
	var payoutRequest models.PayoutRequest

	query := s.db.Preload("Event").Preload("Organizer").Preload("Organizer.OrganizerOnboarding").
		Where("id = ?", requestID)

	if err := query.First(&payoutRequest).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payout request")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve payout request.", err)
	}

	response := payoutRequest.ToAdminDetailResponse()
	return &response, nil
}

// UpdatePayoutRequestStatus updates payout request status (admin only)
func (s *PayoutService) UpdatePayoutRequestStatus(requestID, adminID uuid.UUID, req *models.PayoutRequestUpdate) (*models.PayoutRequestResponse, error) {
	// Start transaction
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var request models.PayoutRequest

	if err := tx.Where("id = ?", requestID).First(&request).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payout request")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve payout request.", err)
	}

	// Check if request is already processed (allow cancelling approved requests)
	if request.Status != "pending" && req.Status != "cancelled" {
		tx.Rollback()
		return nil, utils.NewBusinessLogicError(fmt.Sprintf("Payout request is already %s.", request.Status))
	}

	// Validate that approving this request won't result in negative available amount
	if req.Status == "approved" {
		var totalEarnings float64
		var totalPaid float64
		var totalPending float64

		// Get total earnings for this event
		totalEarningsQuery := `
			SELECT COALESCE(SUM(t.organizer_share), 0) as total_earnings
			FROM transactions t
			WHERE t.event_id = ? AND t.status IN ('succeeded', 'completed')
		`
		tx.Raw(totalEarningsQuery, request.EventID).Scan(&totalEarnings)

		// Get total paid for this event
		totalPaidQuery := `
			SELECT COALESCE(SUM(pb.paid_amount), 0) as total_paid
			FROM payment_bills pb
			WHERE pb.event_id = ? AND pb.organizer_id = ?
		`
		tx.Raw(totalPaidQuery, request.EventID, request.OrganizerID).Scan(&totalPaid)

		// Get total pending payout amounts for this event (excluding current request)
		tx.Model(&models.PayoutRequest{}).
			Where("organizer_id = ? AND event_id = ? AND status IN ? AND id != ?", request.OrganizerID, request.EventID, []string{"pending", "approved"}, request.ID).
			Select("COALESCE(SUM(amount), 0)").
			Scan(&totalPending)

		availableAmount := totalEarnings - totalPaid - totalPending

		// Check if approving this request would result in negative available amount
		if request.Amount > availableAmount {
			tx.Rollback()
			return nil, utils.NewBusinessLogicError(fmt.Sprintf("Cannot approve payout request. Requested amount (%.2f) exceeds available amount (%.2f). Total earnings: %.2f, Total paid: %.2f, Other pending: %.2f.",
				request.Amount, availableAmount, totalEarnings, totalPaid, totalPending))
		}
	}

	// Validate that cancelling/rejecting an approved request doesn't have payment history
	if (req.Status == "cancelled" || req.Status == "rejected") && request.Status == "approved" && request.PaymentBillID != nil {
		var paymentHistoryCount int64
		if err := tx.Model(&models.PaymentHistory{}).
			Where("payment_bill_id = ?", request.PaymentBillID).
			Count(&paymentHistoryCount).Error; err != nil {
			tx.Rollback()
			return nil, utils.NewDatabaseError("Failed to check payment history.", err)
		}

		if paymentHistoryCount > 0 {
			tx.Rollback()
			return nil, utils.NewBusinessLogicError("Cannot cancel or reject payout request. Payment history already exists for the associated bill. Contact support if you need to reverse paid amounts.")
		}
	}

	// Update status and admin notes
	request.Status = req.Status
	request.AdminNotes = req.AdminNotes
	request.ProcessedBy = &adminID

	// Set processed timestamp for final statuses
	if req.Status == "approved" || req.Status == "rejected" || req.Status == "cancelled" {
		now := database.DB.NowFunc()
		request.ProcessedAt = &now
	}

	// If approved, create a payment bill
	if req.Status == "approved" {
		now := database.DB.NowFunc()
		request.ProcessedAt = &now

		// Create PaymentBill for the approved payout request
		var event models.Event
		if err := tx.Select("id, currency").Where("id = ?", request.EventID).First(&event).Error; err != nil {
			tx.Rollback()
			return nil, utils.NewDatabaseError("Failed to load event for payout bill creation.", err)
		}

		amountSmallest := int64(request.Amount)
		if amountSmallest <= 0 {
			tx.Rollback()
			return nil, utils.NewValidationError("Invalid payout amount for event currency", nil)
		}

		billNumber := s.generateBillNumber()
		bill := &models.PaymentBill{
			BillNumber:  billNumber,
			EventID:     &request.EventID,
			OrganizerID: request.OrganizerID,
			CreatedByID: adminID,
			BillType:    models.BillTypePayout,
			Status:      models.PaymentBillPending,
			Currency:    event.Currency,
			Amount:      float64(amountSmallest),
			PaidAmount:  0,
			Notes:       fmt.Sprintf("Generated from payout request #%s", request.RequestNumber),
		}

		if err := tx.Create(bill).Error; err != nil {
			tx.Rollback()
			return nil, utils.NewDatabaseError("Failed to create payment bill.", err)
		}

		// Link bill to payout request
		request.PaymentBillID = &bill.ID

		// Log bill creation
		s.logAudit(context.Background(), "bill_created", "payment_bill", bill.ID, &adminID, "admin", &request.EventID, map[string]interface{}{
			"bill_number":       bill.BillNumber,
			"amount":            bill.Amount,
			"payout_request_id": request.ID,
		})
	}

	if err := tx.Save(&request).Error; err != nil {
		tx.Rollback()
		return nil, utils.NewDatabaseError("Failed to update payout request.", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	// Load the updated request with associations for response
	if err := s.db.Preload("Event").Preload("Organizer").Preload("PaymentBill").First(&request, requestID).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to load updated payout request.", err)
	}

	response := request.ToResponse()
	return &response, nil
}

// DeletePayoutRequest deletes a payout request (organizer only, if pending)
func (s *PayoutService) DeletePayoutRequest(requestID, organizerID uuid.UUID) error {
	var request models.PayoutRequest

	if err := s.db.Where("id = ? AND organizer_id = ?", requestID, organizerID).First(&request).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewNotFoundError("payout request")
		}
		return utils.NewDatabaseError("Failed to retrieve payout request.", err)
	}

	// Only allow deletion of pending requests
	if request.Status != "pending" {
		return utils.NewBusinessLogicError(fmt.Sprintf("Cannot delete %s payout request.", request.Status))
	}

	if err := s.db.Delete(&request).Error; err != nil {
		return utils.NewDatabaseError("Failed to delete payout request.", err)
	}

	return nil
}

// GetOrganizerPayoutSummary gets payout summary for an organizer, optionally filtered by event
func (s *PayoutService) GetOrganizerPayoutSummary(organizerID uuid.UUID, eventID *uuid.UUID) (map[string]interface{}, error) {
	var summary struct {
		TotalEarnings    float64
		TotalReceived    float64
		TotalPending     float64
		PendingRequests  int64
		ApprovedRequests int64
		PaidRequests     int64
	}

	// Get organizer's total earnings from transactions (sum of organizer_share)
	// Use the same logic as event breakdown for consistency
	var totalEarnings float64
	totalEarningsQuery := `
		SELECT COALESCE(SUM(t.organizer_share), 0) as total_earnings
		FROM events
		LEFT JOIN transactions t ON t.event_id = events.id AND t.status = 'succeeded'
		WHERE events.organizer_id = ?
	`

	queryArgs := []interface{}{organizerID}
	if eventID != nil {
		totalEarningsQuery += " AND events.id = ?"
		queryArgs = append(queryArgs, *eventID)
	}

	s.db.Raw(totalEarningsQuery, queryArgs...).Scan(&totalEarnings)
	summary.TotalEarnings = totalEarnings

	// Get total received from actual payments made via payment bills
	// Sum all payment_histories amounts for this organizer's events
	var totalReceived float64
	totalReceivedQuery := `
		SELECT COALESCE(SUM(ph.amount), 0) as total_received
		FROM payment_histories ph
		JOIN payment_bills pb ON ph.payment_bill_id = pb.id
		WHERE pb.organizer_id = ?
	`

	receivedQueryArgs := []interface{}{organizerID}
	if eventID != nil {
		totalReceivedQuery += " AND pb.event_id = ?"
		receivedQueryArgs = append(receivedQueryArgs, *eventID)
	}

	s.db.Raw(totalReceivedQuery, receivedQueryArgs...).Scan(&totalReceived)
	summary.TotalReceived = totalReceived

	// Base query for payout requests
	payoutBaseQuery := s.db.Model(&models.PayoutRequest{}).Where("organizer_id = ?", organizerID)
	if eventID != nil {
		payoutBaseQuery = payoutBaseQuery.Where("event_id = ?", *eventID)
	}

	// Get payout request counts
	baseWhere := "organizer_id = ?"
	args := []interface{}{organizerID}
	if eventID != nil {
		baseWhere += " AND event_id = ?"
		args = append(args, *eventID)
	}

	s.db.Model(&models.PayoutRequest{}).Where(baseWhere, args...).Where("status = ?", "pending").Count(&summary.PendingRequests)
	s.db.Model(&models.PayoutRequest{}).Where(baseWhere, args...).Where("status = ?", "approved").Count(&summary.ApprovedRequests)
	s.db.Model(&models.PayoutRequest{}).Where(baseWhere, args...).Where("status = ?", "paid").Count(&summary.PaidRequests)

	// Get total pending payout amount
	s.db.Model(&models.PayoutRequest{}).
		Where("organizer_id = ? AND status IN ?", organizerID, []string{"pending", "approved"}).
		Scopes(func(db *gorm.DB) *gorm.DB {
			if eventID != nil {
				return db.Where("event_id = ?", *eventID)
			}
			return db
		}).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&summary.TotalPending)

	// Get per-event breakdown
	type EventBreakdown struct {
		EventID          uuid.UUID `json:"event_id"`
		EventTitle       string    `json:"event_title"`
		CommissionRate   float64   `json:"commission_rate"`
		TotalEarnings    float64   `json:"total_earnings"`
		PaidAmount       float64   `json:"paid_amount"`
		DueAmount        float64   `json:"due_amount"`
		PendingRequests  int64     `json:"pending_requests"`
		ApprovedRequests int64     `json:"approved_requests"`
		PaidRequests     int64     `json:"paid_requests"`
	}

	var eventBreakdowns []EventBreakdown

	// Build query for event breakdown - only include completed events that have ended
	// Use payment_histories to get actual paid amounts instead of event_sales
	breakdownQuery := `
		SELECT
			events.id as event_id,
			events.title as event_title,
			events.commission_rate as commission_rate,
			COALESCE(SUM(t.organizer_share), 0) as total_earnings,
			COALESCE((
				SELECT SUM(ph.amount)
				FROM payment_histories ph
				JOIN payment_bills pb ON ph.payment_bill_id = pb.id
				WHERE pb.event_id = events.id AND pb.organizer_id = events.organizer_id
			), 0) as paid_amount,
			COALESCE(SUM(t.organizer_share), 0) - COALESCE((
				SELECT SUM(ph.amount)
				FROM payment_histories ph
				JOIN payment_bills pb ON ph.payment_bill_id = pb.id
				WHERE pb.event_id = events.id AND pb.organizer_id = events.organizer_id
			), 0) as due_amount,
			(
				SELECT COUNT(*) FROM payout_requests pr
				WHERE pr.event_id = events.id AND pr.status = 'pending'
			) as pending_requests,
			(
				SELECT COUNT(*) FROM payout_requests pr
				WHERE pr.event_id = events.id AND pr.status = 'approved'
			) as approved_requests,
			(
				SELECT COUNT(*) FROM payout_requests pr
				WHERE pr.event_id = events.id AND pr.status = 'paid'
			) as paid_requests
		FROM events
		LEFT JOIN transactions t ON t.event_id = events.id AND t.status = 'succeeded'
		WHERE events.organizer_id = ? AND events.status = 'completed' AND events.end_date <= ?
	`

	queryArgs = []interface{}{organizerID, time.Now()}

	if eventID != nil {
		breakdownQuery += " AND events.id = ?"
		queryArgs = append(queryArgs, *eventID)
	}

	breakdownQuery += " GROUP BY events.id, events.title, events.commission_rate HAVING (COALESCE(SUM(t.organizer_share), 0) - COALESCE((\n\t\t\t\tSELECT SUM(ph.amount)\n\t\t\t\tFROM payment_histories ph\n\t\t\t\tJOIN payment_bills pb ON ph.payment_bill_id = pb.id\n\t\t\t\tWHERE pb.event_id = events.id AND pb.organizer_id = events.organizer_id\n\t\t\t), 0)) > 0 ORDER BY LOWER(events.title) ASC"

	if err := s.db.Raw(breakdownQuery, queryArgs...).Scan(&eventBreakdowns).Error; err != nil {
		return nil, err
	}

	// Recalculate pending_amount as sum of due_amount from events with approved payout requests
	// This ensures pending_amount accounts for bills and payment history
	var totalPendingFromApproved float64 = 0
	for _, eb := range eventBreakdowns {
		if eb.ApprovedRequests > 0 {
			totalPendingFromApproved += eb.DueAmount
		}
	}

	result := map[string]interface{}{
		"total_earnings":    summary.TotalEarnings,
		"total_received":    summary.TotalReceived,
		"available_amount":  summary.TotalEarnings - summary.TotalReceived - totalPendingFromApproved,
		"pending_amount":    totalPendingFromApproved,
		"pending_requests":  summary.PendingRequests,
		"approved_requests": summary.ApprovedRequests,
		"paid_requests":     summary.PaidRequests,
		"events":            eventBreakdowns,
	}

	// Add event-specific context if filtered
	if eventID != nil {
		result["filtered_by_event"] = true
		result["event_id"] = *eventID
	} else {
		result["filtered_by_event"] = false
	}

	return result, nil
}

// updateEventSalesPaidAmount updates the paid amount for event sales
func (s *PayoutService) updateEventSalesPaidAmount(eventID uuid.UUID, amount float64) error {
	return s.db.Model(&models.EventSales{}).
		Where("event_id = ?", eventID).
		UpdateColumn("paid_amount", gorm.Expr("paid_amount + ?", amount)).
		UpdateColumn("due_amount", gorm.Expr("organizer_share - paid_amount")).Error
}

func (s *PayoutService) updateEventSalesPaidAmountWithTx(tx *gorm.DB, eventID uuid.UUID, amount float64) error {
	return tx.Model(&models.EventSales{}).
		Where("event_id = ?", eventID).
		UpdateColumn("paid_amount", gorm.Expr("paid_amount + ?", amount)).
		UpdateColumn("due_amount", gorm.Expr("organizer_share - paid_amount")).Error
}
