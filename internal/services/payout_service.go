package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
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
	audit := &models.PaymentAuditLog{
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		ActorID:    actorID,
		ActorType:  actorType,
		EventID:    eventID,
		Timestamp:  time.Now(),
	}

	if changes != nil {
		audit.ChangesAfter = changes
	}

	// Log async to avoid blocking
	go func() {
		s.db.Create(audit)
	}()
}

// CreatePayoutRequest creates a new payout request from organizer
func (s *PayoutService) CreatePayoutRequest(organizerID uuid.UUID, req *models.PayoutRequestCreate) error {
	// Validate organizer
	var organizer models.User
	if err := s.db.Where("id = ?", organizerID).First(&organizer).Error; err != nil {
		return utils.NewNotFoundError("organizer")
	}

	// If event-specific payout, verify event ownership and calculate available amount
	if req.EventID != nil {
		var event models.Event
		if err := s.db.Where("id = ? AND organizer_id = ?", *req.EventID, organizerID).First(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return utils.NewNotFoundError("event")
			}
			return utils.NewDatabaseError("Failed to retrieve event.", err)
		}

		// Validate that payout can only be requested when event is completed or sales have ended
		now := time.Now()
		eventCompleted := event.EndDate.Before(now) || event.EndDate.Equal(now)
		salesEnded := event.SalesStatus != "active"

		if !eventCompleted && !salesEnded {
			return utils.NewBusinessLogicError("Payout requests can only be made after the event has completed or when ticket sales have ended.")
		}

		// Calculate available amount from transactions
		var totalEarnings float64
		var totalPaid float64

		// Get total earnings for this event
		s.db.Model(&models.Transaction{}).
			Where("event_id = ? AND status = ?", *req.EventID, "completed").
			Select("COALESCE(SUM(organizer_share), 0)").
			Scan(&totalEarnings)

		// Get total paid for this event from EventSales
		s.db.Model(&models.EventSales{}).
			Where("event_id = ?", *req.EventID).
			Select("COALESCE(SUM(paid_amount), 0)").
			Scan(&totalPaid)

		availableAmount := totalEarnings - totalPaid

		// Check if requested amount is available
		if req.Amount > availableAmount {
			return utils.NewBusinessLogicError(fmt.Sprintf("Requested amount (%.2f) exceeds available amount (%.2f) for this event.", req.Amount, availableAmount))
		}
	} else {
		// For bulk payouts, validate that there are events that have completed or sales have ended
		var completedOrEndedEventsCount int64
		s.db.Model(&models.Event{}).
			Where("organizer_id = ? AND (end_date <= ? OR sales_status != ?)", organizerID, time.Now(), "active").
			Count(&completedOrEndedEventsCount)

		if completedOrEndedEventsCount == 0 {
			return utils.NewBusinessLogicError("Bulk payout requests can only be made when you have events that have completed or ticket sales have ended.")
		}

		// For bulk payouts, check total available earnings across all events
		var totalEarnings float64
		var totalPaid float64
		var totalPending float64

		// Get total earnings across all events for this organizer
		totalEarningsQuery := `
			SELECT COALESCE(SUM(t.organizer_share), 0) as total_earnings
			FROM events
			LEFT JOIN transactions t ON t.event_id = events.id AND t.status = 'completed'
			WHERE events.organizer_id = ?
		`
		s.db.Raw(totalEarningsQuery, organizerID).Scan(&totalEarnings)

		// Get total paid across all events
		totalPaidQuery := `
			SELECT COALESCE(SUM(es.paid_amount), 0) as total_paid
			FROM events
			LEFT JOIN event_sales es ON es.event_id = events.id
			WHERE events.organizer_id = ?
		`
		s.db.Raw(totalPaidQuery, organizerID).Scan(&totalPaid)

		// Get total pending payout amounts
		s.db.Model(&models.PayoutRequest{}).
			Where("organizer_id = ? AND status IN ?", organizerID, []string{"pending", "approved"}).
			Select("COALESCE(SUM(amount), 0)").
			Scan(&totalPending)

		availableAmount := totalEarnings - totalPaid - totalPending

		// Check if requested amount is available
		if req.Amount > availableAmount {
			return utils.NewBusinessLogicError(fmt.Sprintf("Requested amount (%.2f) exceeds available amount (%.2f). Total earnings: %.2f, Total paid: %.2f, Total pending: %.2f.",
				req.Amount, availableAmount, totalEarnings, totalPaid, totalPending))
		}
	}

	payoutRequest := &models.PayoutRequest{
		OrganizerID: organizerID,
		EventID:     req.EventID,
		Amount:      req.Amount,
		RequestType: req.RequestType,
		Description: req.Description,
		Status:      "pending",
	}

	if err := s.db.Create(payoutRequest).Error; err != nil {
		return utils.NewDatabaseError("Failed to create payout request.", err)
	}

	// Log audit for payout request creation
	s.logAudit(context.Background(), "payout_requested", "payout_request", payoutRequest.ID, &organizerID, "organizer", req.EventID, map[string]interface{}{
		"amount":       req.Amount,
		"request_type": req.RequestType,
		"description":  req.Description,
		"organizer_id": organizerID,
	})

	return nil
}

// GetOrganizerPayoutRequests gets all payout requests for an organizer
func (s *PayoutService) GetOrganizerPayoutRequests(organizerID uuid.UUID, page, limit int, status string) ([]models.PayoutRequestResponse, int64, error) {
	var requests []models.PayoutRequest
	var total int64

	query := s.db.Model(&models.PayoutRequest{}).Where("organizer_id = ?", organizerID)

	if status != "" {
		query = query.Where("status = ?", status)
	}

	// Get total count
	query.Count(&total)

	// Get paginated results with preloaded relations
	offset := (page - 1) * limit
	if err := query.Preload("Event").Preload("Organizer").
		Offset(offset).Limit(limit).Find(&requests).Error; err != nil {
		return nil, 0, err
	}

	// Convert to response format
	var responses []models.PayoutRequestResponse
	for _, req := range requests {
		responses = append(responses, req.ToResponse())
	}

	return responses, total, nil
}

// GetAllPayoutRequests gets all payout requests (admin only)
func (s *PayoutService) GetAllPayoutRequests(page, limit int, status string) ([]models.PayoutRequestResponse, int64, error) {
	var requests []models.PayoutRequest
	var total int64

	query := s.db.Model(&models.PayoutRequest{})

	if status != "" {
		query = query.Where("status = ?", status)
	}

	// Get total count
	query.Count(&total)

	// Get paginated results with preloaded relations
	offset := (page - 1) * limit
	if err := query.Preload("Event").Preload("Organizer").
		Offset(offset).Limit(limit).Find(&requests).Error; err != nil {
		return nil, 0, err
	}

	// Convert to response format
	var responses []models.PayoutRequestResponse
	for _, req := range requests {
		responses = append(responses, req.ToResponse())
	}

	return responses, total, nil
}

// GetPayoutRequestByID gets a specific payout request
func (s *PayoutService) GetPayoutRequestByID(requestID uuid.UUID, organizerID *uuid.UUID) (*models.PayoutRequestResponse, error) {
	var request models.PayoutRequest

	query := s.db.Preload("Event").Preload("Organizer")

	// If organizerID is provided, restrict to that organizer
	if organizerID != nil {
		query = query.Where("id = ? AND organizer_id = ?", requestID, *organizerID)
	} else {
		query = query.Where("id = ?", requestID)
	}

	if err := query.First(&request).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payout request")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve payout request.", err)
	}

	response := request.ToResponse()
	return &response, nil
}

// UpdatePayoutRequestStatus updates payout request status (admin only)
func (s *PayoutService) UpdatePayoutRequestStatus(requestID, adminID uuid.UUID, req *models.PayoutRequestUpdate) error {
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
			return utils.NewNotFoundError("payout request")
		}
		return utils.NewDatabaseError("Failed to retrieve payout request.", err)
	}

	// Check if request is already processed
	if request.Status != "pending" {
		tx.Rollback()
		return utils.NewBusinessLogicError(fmt.Sprintf("Payout request is already %s.", request.Status))
	}

	// Validate that approving this request won't result in negative available amount
	if req.Status == "approved" || req.Status == "paid" {
		var totalEarnings float64
		var totalPaid float64
		var totalPending float64

		// Get total earnings for the organizer (or specific event)
		totalEarningsQuery := `
			SELECT COALESCE(SUM(t.organizer_share), 0) as total_earnings
			FROM events
			LEFT JOIN transactions t ON t.event_id = events.id AND t.status = 'completed'
			WHERE events.organizer_id = ?
		`

		queryArgs := []interface{}{request.OrganizerID}
		if request.EventID != nil {
			totalEarningsQuery += " AND events.id = ?"
			queryArgs = append(queryArgs, *request.EventID)
		}
		tx.Raw(totalEarningsQuery, queryArgs...).Scan(&totalEarnings)

		// Get total paid
		totalPaidQuery := `
			SELECT COALESCE(SUM(es.paid_amount), 0) as total_paid
			FROM events
			LEFT JOIN event_sales es ON es.event_id = events.id
			WHERE events.organizer_id = ?
		`

		paidQueryArgs := []interface{}{request.OrganizerID}
		if request.EventID != nil {
			totalPaidQuery += " AND events.id = ?"
			paidQueryArgs = append(paidQueryArgs, *request.EventID)
		}
		tx.Raw(totalPaidQuery, paidQueryArgs...).Scan(&totalPaid)

		// Get total pending (excluding current request)
		tx.Model(&models.PayoutRequest{}).
			Where("organizer_id = ? AND status IN ? AND id != ?",
				request.OrganizerID, []string{"pending", "approved"}, request.ID).
			Select("COALESCE(SUM(amount), 0)").
			Scan(&totalPending)

		availableAmount := totalEarnings - totalPaid - totalPending

		// Check if approving this request would result in negative available amount
		if request.Amount > availableAmount {
			tx.Rollback()
			return utils.NewBusinessLogicError(fmt.Sprintf("Cannot approve payout request. Requested amount (%.2f) exceeds available amount (%.2f). Total earnings: %.2f, Total paid: %.2f, Other pending: %.2f.",
				request.Amount, availableAmount, totalEarnings, totalPaid, totalPending))
		}
	}

	// Update status and admin notes
	request.Status = req.Status
	request.AdminNotes = req.AdminNotes
	request.ProcessedBy = &adminID

	// If approved or paid, update processed timestamp
	if req.Status == "approved" || req.Status == "paid" {
		now := database.DB.NowFunc()
		request.ProcessedAt = &now

		// If paid, update the event sales paid amount
		if req.Status == "paid" && request.EventID != nil {
			if err := s.updateEventSalesPaidAmountWithTx(tx, *request.EventID, request.Amount); err != nil {
				tx.Rollback()
				return utils.NewDatabaseError("Failed to update event sales.", err)
			}
		}
	}

	if err := tx.Save(&request).Error; err != nil {
		tx.Rollback()
		return utils.NewDatabaseError("Failed to update payout request.", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return err
	}

	return nil
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
		LEFT JOIN transactions t ON t.event_id = events.id AND t.status = 'completed'
		WHERE events.organizer_id = ?
	`

	queryArgs := []interface{}{organizerID}
	if eventID != nil {
		totalEarningsQuery += " AND events.id = ?"
		queryArgs = append(queryArgs, *eventID)
	}

	s.db.Raw(totalEarningsQuery, queryArgs...).Scan(&totalEarnings)
	summary.TotalEarnings = totalEarnings

	// Get total received from EventSales (paid_amount)
	// Use LEFT JOIN to handle cases where EventSales records don't exist
	var totalReceived float64
	totalReceivedQuery := `
		SELECT COALESCE(SUM(es.paid_amount), 0) as total_received
		FROM events
		LEFT JOIN event_sales es ON es.event_id = events.id
		WHERE events.organizer_id = ?
	`

	receivedQueryArgs := []interface{}{organizerID}
	if eventID != nil {
		totalReceivedQuery += " AND events.id = ?"
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
	payoutBaseQuery.Where("status = ?", "pending").Count(&summary.PendingRequests)
	payoutBaseQuery.Where("status = ?", "approved").Count(&summary.ApprovedRequests)
	payoutBaseQuery.Where("status = ?", "paid").Count(&summary.PaidRequests)

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
		TotalEarnings    float64   `json:"total_earnings"`
		PaidAmount       float64   `json:"paid_amount"`
		DueAmount        float64   `json:"due_amount"`
		PendingRequests  int64     `json:"pending_requests"`
		ApprovedRequests int64     `json:"approved_requests"`
		PaidRequests     int64     `json:"paid_requests"`
	}

	var eventBreakdowns []EventBreakdown

	// Build query for event breakdown
	breakdownQuery := `
		SELECT 
			events.id as event_id,
			events.title as event_title,
			COALESCE(SUM(t.organizer_share), 0) as total_earnings,
			COALESCE(es.paid_amount, 0) as paid_amount,
			COALESCE(SUM(t.organizer_share), 0) - COALESCE(es.paid_amount, 0) as due_amount,
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
		LEFT JOIN transactions t ON t.event_id = events.id AND t.status = 'completed'
		LEFT JOIN event_sales es ON es.event_id = events.id
		WHERE events.organizer_id = ?
	`

	queryArgs = []interface{}{organizerID}

	if eventID != nil {
		breakdownQuery += " AND events.id = ?"
		queryArgs = append(queryArgs, *eventID)
	}

	breakdownQuery += " GROUP BY events.id, events.title, es.paid_amount ORDER BY events.created_at DESC"

	if err := s.db.Raw(breakdownQuery, queryArgs...).Scan(&eventBreakdowns).Error; err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"total_earnings":    summary.TotalEarnings,
		"total_received":    summary.TotalReceived,
		"available_amount":  summary.TotalEarnings - summary.TotalReceived - summary.TotalPending,
		"pending_amount":    summary.TotalPending,
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
