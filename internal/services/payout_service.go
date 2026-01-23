package services

import (
	"errors"
	"fmt"

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

		// Get event sales to check available amount
		var eventSales models.EventSales
		if err := s.db.Where("event_id = ?", *req.EventID).First(&eventSales).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return utils.NewNotFoundError("sales data for this event")
			}
			return utils.NewDatabaseError("Failed to retrieve event sales.", err)
		}

		// Check if requested amount is available
		if req.Amount > eventSales.DueAmount {
			return utils.NewBusinessLogicError(fmt.Sprintf("Requested amount (%.2f) exceeds available amount (%.2f).", req.Amount, eventSales.DueAmount))
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

	return nil
}

// GetOrganizerPayoutRequests gets all payout requests for an organizer
func (s *PayoutService) GetOrganizerPayoutRequests(organizerID uuid.UUID, page, limit int, status string) ([]models.PayoutRequest, int64, error) {
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

	return requests, total, nil
}

// GetAllPayoutRequests gets all payout requests (admin only)
func (s *PayoutService) GetAllPayoutRequests(page, limit int, status string) ([]models.PayoutRequest, int64, error) {
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

	return requests, total, nil
}

// GetPayoutRequestByID gets a specific payout request
func (s *PayoutService) GetPayoutRequestByID(requestID uuid.UUID, organizerID *uuid.UUID) (*models.PayoutRequest, error) {
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

	return &request, nil
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

// GetOrganizerPayoutSummary gets payout summary for an organizer
func (s *PayoutService) GetOrganizerPayoutSummary(organizerID uuid.UUID) (map[string]interface{}, error) {
	var summary struct {
		TotalEarnings    float64
		TotalReceived    float64
		TotalPending     float64
		PendingRequests  int64
		ApprovedRequests int64
		PaidRequests     int64
	}

	// Get organizer's total earnings from event sales
	s.db.Model(&models.EventSales{}).
		Where("organizer_id = ?", organizerID).
		Select("COALESCE(SUM(organizer_share), 0) as total_earnings, COALESCE(SUM(paid_amount), 0) as total_received").
		Scan(&summary)

	// Get payout request counts
	s.db.Model(&models.PayoutRequest{}).
		Where("organizer_id = ? AND status = ?", organizerID, "pending").
		Count(&summary.PendingRequests)

	s.db.Model(&models.PayoutRequest{}).
		Where("organizer_id = ? AND status = ?", organizerID, "approved").
		Count(&summary.ApprovedRequests)

	s.db.Model(&models.PayoutRequest{}).
		Where("organizer_id = ? AND status = ?", organizerID, "paid").
		Count(&summary.PaidRequests)

	// Get total pending payout amount
	s.db.Model(&models.PayoutRequest{}).
		Where("organizer_id = ? AND status IN ?", organizerID, []string{"pending", "approved"}).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&summary.TotalPending)

	result := map[string]interface{}{
		"total_earnings":    summary.TotalEarnings,
		"total_received":    summary.TotalReceived,
		"available_amount":  summary.TotalEarnings - summary.TotalReceived,
		"pending_amount":    summary.TotalPending,
		"pending_requests":  summary.PendingRequests,
		"approved_requests": summary.ApprovedRequests,
		"paid_requests":     summary.PaidRequests,
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
