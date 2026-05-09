package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/utils"
)

// BillService handles all bill and payment operations
type BillService struct {
	db *gorm.DB
}

// NewBillService creates a new bill service instance
func NewBillService(db *gorm.DB) *BillService {
	return &BillService{
		db: db,
	}
}

// CreatePaymentBill creates a payment bill for organizer payout (single event per bill)
func (bs *BillService) CreatePaymentBill(adminID uuid.UUID, req models.CreatePaymentBillRequest) (*models.PaymentBillResponse, error) {
	var err error

	// Get event details
	var event models.Event
	if err = bs.db.First(&event, req.EventID).Error; err != nil {
		return nil, utils.NewNotFoundError("event")
	}

	// Get organizer details
	var organizer models.User
	if err = bs.db.First(&organizer, req.OrganizerID).Error; err != nil {
		return nil, utils.NewNotFoundError("organizer")
	}

	// Verify organizer owns the event
	if event.OrganizerID != req.OrganizerID {
		return nil, utils.NewValidationError("Organizer does not own this event", nil)
	}

	// Check if there are any non-cancelled bills for this event
	var nonCancelledBillCount int64
	if err = bs.db.Model(&models.PaymentBill{}).
		Where("event_id = ? AND status != 'cancelled'", req.EventID).
		Count(&nonCancelledBillCount).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to check existing bills.", err)
	}
	if nonCancelledBillCount > 0 {
		return nil, utils.NewValidationError("Cannot create a new bill while there are active bills for this event. Please cancel all existing bills before creating a new one.", nil)
	}

	var totalRevenue, totalCommission, organizerEarnings int64

	// Auto-calculate from completed transactions for this event
	var transactions []models.Transaction
	if err = bs.db.Preload("Event").
		Where("event_id = ? AND status = 'completed'", req.EventID).
		Find(&transactions).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to fetch transactions.", err)
	}

	// Calculate totals from transactions
	for _, txn := range transactions {
		totalRevenue += txn.AmountTotal
		totalCommission += txn.PlatformFee
		organizerEarnings += txn.OrganizerEarning
	}

	// Check if already paid for this event (from existing bills that have payments)
	var alreadyPaid int64
	err = bs.db.Model(&models.PaymentBill{}).
		Where("event_id = ? AND paid_amount > 0", req.EventID).
		Select("COALESCE(SUM(paid_amount), 0)").
		Scan(&alreadyPaid).Error
	if err != nil {
		return nil, utils.NewDatabaseError("Failed to calculate already paid amount.", err)
	}

	organizerEarnings -= alreadyPaid
	if organizerEarnings <= 0 {
		return nil, utils.NewValidationError("No outstanding payments for this event", nil)
	}

	// Set default priority
	priority := "normal"

	// Generate bill number
	billNumber := bs.generateBillNumber()

	// Create payment bill
	paymentBill := &models.PaymentBill{
		BillNumber:        billNumber,
		EventID:           req.EventID,
		OrganizerID:       req.OrganizerID,
		AdminID:           adminID,
		TotalRevenue:      float64(totalRevenue) / 100,      // Convert cents to dollars
		TotalCommission:   float64(totalCommission) / 100,   // Convert cents to dollars
		OrganizerEarnings: float64(organizerEarnings) / 100, // Convert cents to dollars
		BilledAmount:      float64(organizerEarnings) / 100, // Convert cents to dollars
		PaidAmount:        0,
		RemainingAmount:   float64(organizerEarnings) / 100, // Convert cents to dollars
		PaymentMethod:     req.PaymentMethod,
		Status:            "pending",
		BillType:          "auto_calculated",
		Priority:          priority,
		BillDate:          time.Now(),
	}

	if err := bs.db.Create(paymentBill).Error; err != nil {
		// Log the failure for audit trail
		bs.logAudit(context.Background(), "bill_creation_failed", "payment_bill", uuid.UUID{}, &adminID, "admin", &req.EventID, map[string]interface{}{
			"error":          "database_error",
			"error_message":  err.Error(),
			"organizer_id":   req.OrganizerID,
			"payment_method": req.PaymentMethod,
		})
		return nil, utils.NewDatabaseError("Failed to create payment bill.", err)
	}

	// Log audit for bill creation
	bs.logAudit(context.Background(), "bill_created", "payment_bill", paymentBill.ID, &adminID, "admin", &req.EventID, map[string]interface{}{
		"bill_number":        billNumber,
		"billed_amount":      organizerEarnings,
		"payment_method":     req.PaymentMethod,
		"organizer_id":       req.OrganizerID,
		"total_revenue":      totalRevenue,
		"total_commission":   totalCommission,
		"organizer_earnings": organizerEarnings,
	})

	// Load associations for response
	if err := bs.db.Preload("Event").Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Admin").First(paymentBill, paymentBill.ID).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to load payment bill associations.", err)
	}

	response := paymentBill.ToResponse()
	return &response, nil
}

// DeletePaymentBill deletes a payment bill (only if no payments have been made)
func (bs *BillService) DeletePaymentBill(billID uuid.UUID) error {
	var paymentBill models.PaymentBill
	if err := bs.db.Where("id = ?", billID).First(&paymentBill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewNotFoundError("payment bill")
		}
		return utils.NewDatabaseError("Failed to find payment bill.", err)
	}

	// Check if bill has any payments
	if paymentBill.PaidAmount > 0 {
		return utils.NewValidationError("Cannot delete a bill that has payments", nil)
	}

	// Check if bill is cancelled
	if paymentBill.Status != "cancelled" {
		return utils.NewValidationError("Can only delete cancelled bills", nil)
	}

	// Delete associated payment history
	if err := bs.db.Where("bill_id = ?", billID).Delete(&models.PaymentHistory{}).Error; err != nil {
		return utils.NewDatabaseError("Failed to delete payment history.", err)
	}

	// Delete the bill
	if err := bs.db.Delete(&paymentBill).Error; err != nil {
		return utils.NewDatabaseError("Failed to delete payment bill.", err)
	}

	// Log audit for bill deletion
	bs.logAudit(context.Background(), "bill_deleted", "payment_bill", billID, nil, "system", &paymentBill.EventID, map[string]interface{}{
		"bill_number": paymentBill.BillNumber,
	})

	return nil
}

// GetPaymentBills returns paginated list of payment bills
func (bs *BillService) GetPaymentBills(page, limit int, organizerID *uuid.UUID, status string) ([]models.PaymentBillResponse, int64, error) {
	var paymentBills []models.PaymentBill
	var total int64

	query := bs.db.Model(&models.PaymentBill{}).
		Preload("Event").
		Preload("Organizer").
		Preload("Admin")

	if organizerID != nil {
		query = query.Where("organizer_id = ?", *organizerID)
	}

	if status != "" {
		query = query.Where("status = ?", status)
	}

	// Count total records
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to count payment bills.", err)
	}

	// Get paginated results
	offset := (page - 1) * limit
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&paymentBills).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to get payment bills.", err)
	}

	// Convert to response format
	responses := make([]models.PaymentBillResponse, 0, len(paymentBills))
	for _, bill := range paymentBills {
		response := bill.ToResponse()
		responses = append(responses, response)
	}

	return responses, total, nil
}

// GetPaymentBillsWithSearch returns paginated list of payment bills with search functionality
func (bs *BillService) GetPaymentBillsWithSearch(page, limit int, organizerID *uuid.UUID, status, search string) ([]models.PaymentBillResponse, int64, error) {
	var paymentBills []models.PaymentBill
	var total int64

	query := bs.db.Model(&models.PaymentBill{}).
		Preload("Event").
		Preload("Organizer").
		Preload("Admin")

	if organizerID != nil {
		query = query.Where("organizer_id = ?", *organizerID)
	}

	if status != "" {
		query = query.Where("status = ?", status)
	}

	if search != "" {
		searchTerm := "%" + search + "%"
		query = query.Joins("LEFT JOIN events ON payment_bills.event_id = events.id").
			Joins("LEFT JOIN users ON payment_bills.organizer_id = users.id").
			Where("payment_bills.bill_number ILIKE ? OR events.title ILIKE ? OR users.first_name ILIKE ? OR users.last_name ILIKE ?",
				searchTerm, searchTerm, searchTerm, searchTerm)
	}

	// Count total records
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to count payment bills.", err)
	}

	// Get paginated results
	offset := (page - 1) * limit
	if err := query.Order("payment_bills.created_at DESC").Offset(offset).Limit(limit).Find(&paymentBills).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to get payment bills.", err)
	}

	// Convert to response format
	responses := make([]models.PaymentBillResponse, 0, len(paymentBills))
	for _, bill := range paymentBills {
		response := bill.ToResponse()
		responses = append(responses, response)
	}

	return responses, total, nil
}

// GetPaymentBillSummariesWithSearch returns paginated list of payment bill summaries with advanced search
func (bs *BillService) GetPaymentBillSummariesWithSearch(page, limit int, organizerIDs []uuid.UUID, statuses []string, search string, startDate, endDate *time.Time, sortBy, sortOrder string) ([]models.PaymentBillSummaryResponse, int64, error) {
	var summaries []models.PaymentBillSummaryResponse
	var total int64

	query := bs.db.Model(&models.PaymentBill{}).
		Select(`
			payment_bills.id,
			payment_bills.bill_number,
			payment_bills.event_id,
			payment_bills.organizer_id,
			payment_bills.total_revenue,
			payment_bills.total_commission,
			payment_bills.organizer_earnings,
			payment_bills.billed_amount,
			payment_bills.paid_amount,
			payment_bills.remaining_amount,
			payment_bills.payment_method,
			payment_bills.status,
			payment_bills.priority,
			payment_bills.bill_date,
			payment_bills.paid_date,
			payment_bills.cancelled_date,
			payment_bills.created_at,
			payment_bills.updated_at,
			events.title as event_title,
			CONCAT(users.first_name, ' ', users.last_name) as organizer_name
		`).
		Joins("LEFT JOIN events ON payment_bills.event_id = events.id").
		Joins("LEFT JOIN users ON payment_bills.organizer_id = users.id")

	if len(organizerIDs) > 0 {
		query = query.Where("payment_bills.organizer_id IN ?", organizerIDs)
	}

	if len(statuses) > 0 {
		query = query.Where("payment_bills.status IN ?", statuses)
	}

	if search != "" {
		searchTerm := "%" + search + "%"
		query = query.Where("payment_bills.bill_number ILIKE ? OR events.title ILIKE ? OR CONCAT(users.first_name, ' ', users.last_name) ILIKE ?",
			searchTerm, searchTerm, searchTerm)
	}

	if startDate != nil {
		query = query.Where("payment_bills.created_at >= ?", *startDate)
	}

	if endDate != nil {
		query = query.Where("payment_bills.created_at <= ?", *endDate)
	}

	// Count total records
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to count payment bills.", err)
	}

	// Apply sorting
	if sortBy == "" {
		sortBy = "created_at"
	}
	if sortOrder == "" {
		sortOrder = "desc"
	}

	orderClause := fmt.Sprintf("payment_bills.%s %s", sortBy, sortOrder)
	query = query.Order(orderClause)

	// Get paginated results
	offset := (page - 1) * limit
	if err := query.Offset(offset).Limit(limit).Scan(&summaries).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to get payment bill summaries.", err)
	}

	return summaries, total, nil
}

// GetPaymentBillByID returns a single payment bill by ID
func (bs *BillService) GetPaymentBillByID(billID uuid.UUID) (*models.PaymentBillResponse, error) {
	var paymentBill models.PaymentBill
	if err := bs.db.Preload("Event").Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Admin").First(&paymentBill, billID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payment bill")
		}
		return nil, utils.NewDatabaseError("Failed to find payment bill.", err)
	}

	response := paymentBill.ToResponse()
	return &response, nil
}

// AddPaymentToBill adds payment to existing bill
func (bs *BillService) AddPaymentToBill(
	billID uuid.UUID,
	payment *models.PaymentHistory,
) (*models.PaymentBillResponse, error) {

	tx := bs.db.Begin()

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var paymentBill models.PaymentBill

	if err := tx.
		Where("id = ?", billID).
		First(&paymentBill).Error; err != nil {

		tx.Rollback()

		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payment bill")
		}

		return nil, utils.NewDatabaseError(
			"Failed to find payment bill.",
			err,
		)
	}

	// Validate payment amount
	if payment.Amount <= 0 {
		tx.Rollback()
		return nil, utils.NewValidationError(
			"Payment amount must be greater than zero",
			nil,
		)
	}

	// Prevent overpayment
	if payment.Amount > paymentBill.RemainingAmount {
		tx.Rollback()
		return nil, utils.NewValidationError(
			"Payment amount cannot exceed remaining amount",
			nil,
		)
	}

	now := time.Now()

	// Attach bill
	payment.PaymentBillID = billID
	payment.PaymentDate = now

	// Create payment history
	if err := tx.Create(payment).Error; err != nil {
		tx.Rollback()

		return nil, utils.NewDatabaseError(
			"Failed to create payment history.",
			err,
		)
	}

	// Update bill financials
	paymentBill.PaidAmount += payment.Amount
	paymentBill.RemainingAmount -= payment.Amount

	// Fully paid
	if paymentBill.RemainingAmount <= 0 {

		paymentBill.RemainingAmount = 0
		paymentBill.Status = "paid"

		if paymentBill.PaidDate == nil {
			paymentBill.PaidDate = &now
		}

	} else {

		paymentBill.Status = "partially_paid"
	}

	paymentBill.UpdatedAt = now

	if err := tx.Save(&paymentBill).Error; err != nil {

		tx.Rollback()

		return nil, utils.NewDatabaseError(
			"Failed to update payment bill.",
			err,
		)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, utils.NewDatabaseError(
			"Failed to commit transaction.",
			err,
		)
	}

	// Audit log
	bs.logAudit(
		context.Background(),
		"payment_added",
		"payment_bill",
		billID,
		&payment.ProcessedByID,
		"admin",
		&paymentBill.EventID,
		map[string]interface{}{
			"payment_amount": payment.Amount,
			"payment_method": payment.PaymentMethod,
			"payment_ref":    payment.PaymentRef,
			"remaining":      paymentBill.RemainingAmount,
		},
	)

	// Reload with relations
	if err := bs.db.
		Preload("Event").
		Preload("Organizer").
		Preload("Organizer.OrganizerOnboarding").
		Preload("Admin").
		First(&paymentBill, paymentBill.ID).Error; err != nil {

		return nil, utils.NewDatabaseError(
			"Failed to load payment bill associations.",
			err,
		)
	}

	response := paymentBill.ToResponse()

	return &response, nil
}

// GetBillPaymentHistory returns payment history for a bill
func (bs *BillService) GetBillPaymentHistory(billID uuid.UUID, search, paymentMethod string, startDate, endDate *time.Time, sortBy, sortOrder string, limit int) ([]models.PaymentHistoryResponse, error) {
	var payments []models.PaymentHistory

	query := bs.db.Model(&models.PaymentHistory{}).Where("bill_id = ?", billID)

	if search != "" {
		searchTerm := "%" + search + "%"
		query = query.Where("reference ILIKE ? OR notes ILIKE ?", searchTerm, searchTerm)
	}

	if paymentMethod != "" {
		query = query.Where("payment_method = ?", paymentMethod)
	}

	if startDate != nil {
		query = query.Where("payment_date >= ?", *startDate)
	}

	if endDate != nil {
		query = query.Where("payment_date <= ?", *endDate)
	}

	// Apply sorting
	if sortBy == "" {
		sortBy = "payment_date"
	}
	if sortOrder == "" {
		sortOrder = "desc"
	}

	orderClause := fmt.Sprintf("%s %s", sortBy, sortOrder)
	query = query.Order(orderClause)

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&payments).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to get payment history.", err)
	}

	// Convert to response format
	responses := make([]models.PaymentHistoryResponse, 0, len(payments))
	for _, payment := range payments {
		response := payment.ToResponse()
		responses = append(responses, response)
	}

	return responses, nil
}

// getBillType returns the bill type based on auto calculation flag
func (bs *BillService) getBillType(autoCalculate bool) string {
	if autoCalculate {
		return "auto_calculated"
	}
	return "manual"
}

// SetBillScreenshot sets the screenshot URL for a bill
func (bs *BillService) SetBillScreenshot(billID uuid.UUID, screenshotURL string) error {
	if err := bs.db.Model(&models.PaymentBill{}).Where("id = ?", billID).Update("screenshot_url", screenshotURL).Error; err != nil {
		return utils.NewDatabaseError("Failed to update bill screenshot.", err)
	}
	return nil
}

// SetPaymentHistoryScreenshot sets the screenshot URL for a payment history record
func (bs *BillService) SetPaymentHistoryScreenshot(paymentID uuid.UUID, screenshotURL string) error {
	if err := bs.db.Model(&models.PaymentHistory{}).Where("id = ?", paymentID).Update("screenshot_url", screenshotURL).Error; err != nil {
		return utils.NewDatabaseError("Failed to update payment history screenshot.", err)
	}
	return nil
}

// generateBillNumber generates a unique bill number
func (bs *BillService) generateBillNumber() string {
	return fmt.Sprintf("BILL-%d", time.Now().Unix())
}

// logAudit creates audit log entries for bill operations
func (bs *BillService) logAudit(ctx context.Context, action, entityType string, entityID uuid.UUID, actorID *uuid.UUID, actorType string, eventID *uuid.UUID, changes map[string]interface{}) {
	LogPaymentAuditAsync(bs.db, action, entityType, entityID, actorID, actorType, eventID, changes)
}
