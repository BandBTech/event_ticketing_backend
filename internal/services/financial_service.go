package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/utils"
)

type FinancialService struct {
	db *gorm.DB
}

func NewFinancialService(db *gorm.DB) *FinancialService {
	return &FinancialService{
		db: db,
	}
}

// logAudit creates audit log entries for financial operations
func (fs *FinancialService) logAudit(ctx context.Context, action, entityType string, entityID uuid.UUID, actorID *uuid.UUID, actorType string, eventID *uuid.UUID, changes map[string]interface{}) {
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
		fs.db.Create(audit)
	}()
}

// REMOVED: All EventSales methods - now calculate from Transactions table
// Use these SQL queries instead:
// - Total tickets sold per event: SELECT SUM(quantity) FROM transactions WHERE event_id = ? AND status = 'completed'
// - Gross revenue per event: SELECT SUM(amount) FROM transactions WHERE event_id = ? AND status = 'completed'
// - Commission earned: SELECT SUM(commission_amount) FROM transactions WHERE event_id = ? AND status = 'completed'
// - Organizer earnings: SELECT SUM(organizer_share) FROM transactions WHERE event_id = ? AND status = 'completed'

// CreatePaymentBill creates a payment bill for organizer payout (single event per bill)
func (fs *FinancialService) CreatePaymentBill(adminID uuid.UUID, req models.CreatePaymentBillRequest) (*models.PaymentBillResponse, error) {
	var err error

	// Get event details
	var event models.Event
	if err = fs.db.First(&event, req.EventID).Error; err != nil {
		return nil, utils.NewNotFoundError("event")
	}

	// Get organizer details
	var organizer models.User
	if err = fs.db.First(&organizer, req.OrganizerID).Error; err != nil {
		return nil, utils.NewNotFoundError("organizer")
	}

	// Verify organizer owns the event
	if event.OrganizerID != req.OrganizerID {
		return nil, utils.NewValidationError("Organizer does not own this event", nil)
	}

	// Check if there are any non-cancelled bills for this event
	var nonCancelledBillCount int64
	if err = fs.db.Model(&models.PaymentBill{}).
		Where("event_id = ? AND status != 'cancelled'", req.EventID).
		Count(&nonCancelledBillCount).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to check existing bills.", err)
	}
	if nonCancelledBillCount > 0 {
		return nil, utils.NewValidationError("Cannot create a new bill while there are active bills for this event. Please cancel all existing bills before creating a new one.", nil)
	}

	var totalRevenue, totalCommission, organizerEarnings float64

	// Auto-calculate from completed transactions for this event
	var transactions []models.Transaction
	if err = fs.db.Preload("Event").
		Where("event_id = ? AND status = 'completed'", req.EventID).
		Find(&transactions).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to fetch transactions.", err)
	}

	// Calculate totals from transactions
	for _, txn := range transactions {
		totalRevenue += txn.Amount
		totalCommission += txn.CommissionAmount
		organizerEarnings += txn.OrganizerShare
	}

	// Check if already paid for this event (from existing bills that have payments)
	var alreadyPaid float64
	err = fs.db.Model(&models.PaymentBill{}).
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
	billNumber := fs.generateBillNumber()

	// Create payment bill
	paymentBill := &models.PaymentBill{
		BillNumber:        billNumber,
		EventID:           req.EventID,
		OrganizerID:       req.OrganizerID,
		AdminID:           adminID,
		TotalRevenue:      totalRevenue,
		TotalCommission:   totalCommission,
		OrganizerEarnings: organizerEarnings,
		BilledAmount:      organizerEarnings,
		PaidAmount:        0,
		RemainingAmount:   organizerEarnings,
		PaymentMethod:     req.PaymentMethod,
		Status:            "pending",
		BillType:          "auto_calculated",
		Priority:          priority,
		BillDate:          time.Now(),
	}

	if err := fs.db.Create(paymentBill).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to create payment bill.", err)
	}

	// Log audit for bill creation
	fs.logAudit(context.Background(), "bill_created", "payment_bill", paymentBill.ID, &adminID, "admin", &req.EventID, map[string]interface{}{
		"bill_number":        billNumber,
		"billed_amount":      organizerEarnings,
		"payment_method":     req.PaymentMethod,
		"organizer_id":       req.OrganizerID,
		"total_revenue":      totalRevenue,
		"total_commission":   totalCommission,
		"organizer_earnings": organizerEarnings,
	})

	// Load associations for response
	if err := fs.db.Preload("Event").Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Admin").First(paymentBill, paymentBill.ID).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to load payment bill associations.", err)
	}

	response := paymentBill.ToResponse()
	return &response, nil
}

// UpdatePaymentBill updates payment bill status and handles partial payments
func (fs *FinancialService) UpdatePaymentBill(billID uuid.UUID, req models.UpdatePaymentBillRequest) (*models.PaymentBillResponse, error) {
	var paymentBill models.PaymentBill
	if err := fs.db.Where("id = ?", billID).First(&paymentBill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payment bill")
		}
		return nil, utils.NewDatabaseError("Failed to find payment bill.", err)
	}

	// Handle partial payments
	if req.PaymentAmount != nil && *req.PaymentAmount > 0 {
		paymentAmount := *req.PaymentAmount

		// Validate payment amount
		if paymentAmount > paymentBill.RemainingAmount {
			return nil, utils.NewValidationError("Payment amount cannot exceed remaining amount", nil)
		}

		// Update bill amounts
		paymentBill.PaidAmount += paymentAmount
		paymentBill.RemainingAmount -= paymentAmount

		// Update status based on payment
		if paymentBill.RemainingAmount <= 0 {
			paymentBill.Status = "paid"
			now := time.Now()
			paymentBill.PaidDate = &now
		} else {
			paymentBill.Status = "partially_paid"
		}
	} else {
		// Status-only update
		if req.Status != "" {
			paymentBill.Status = req.Status
			if req.Status == "paid" && paymentBill.PaidDate == nil {
				now := time.Now()
				paymentBill.PaidDate = &now
			}
		}
	}
	// If bill is being cancelled, update related payout request status to cancelled
	if req.Status == "cancelled" || paymentBill.Status == "cancelled" {
		var payoutRequest models.PayoutRequest
		if err := fs.db.Where("payment_bill_id = ?", billID).First(&payoutRequest).Error; err == nil {
			// Update payout request status to cancelled
			payoutRequest.Status = "cancelled"
			if err := fs.db.Save(&payoutRequest).Error; err != nil {
				return nil, utils.NewDatabaseError("Failed to update related payout request status.", err)
			}

			// Log audit for payout request update
			fs.logAudit(context.Background(), "payout_request_cancelled", "payout_request", payoutRequest.ID, nil, "admin", &paymentBill.EventID, map[string]interface{}{
				"reason":  "bill_cancelled",
				"bill_id": billID,
			})
		}
	}
	// If bill is being paid, update related payout request status to paid
	if req.Status == "paid" || paymentBill.Status == "paid" {
		var payoutRequest models.PayoutRequest
		if err := fs.db.Where("payment_bill_id = ?", billID).First(&payoutRequest).Error; err == nil {
			// Update payout request status to paid
			payoutRequest.Status = "paid"
			if err := fs.db.Save(&payoutRequest).Error; err != nil {
				return nil, utils.NewDatabaseError("Failed to update related payout request status.", err)
			}

			// Log audit for payout request update
			fs.logAudit(context.Background(), "payout_request_paid", "payout_request", payoutRequest.ID, nil, "admin", &paymentBill.EventID, map[string]interface{}{
				"reason":  "bill_paid",
				"bill_id": billID,
			})
		}
	}
	// Update other fields
	if req.PaymentRef != "" {
		paymentBill.PaymentRef = req.PaymentRef
	}
	if req.Notes != "" {
		paymentBill.Notes = req.Notes
	}

	// Save changes
	if err := fs.db.Save(&paymentBill).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to update payment bill.", err)
	}

	// Log audit for bill update
	action := "bill_updated"
	changes := map[string]interface{}{
		"status": paymentBill.Status,
	}

	if req.PaymentAmount != nil && *req.PaymentAmount > 0 {
		action = "bill_payment_added"
		changes["payment_amount"] = *req.PaymentAmount
		changes["paid_amount"] = paymentBill.PaidAmount
		changes["remaining_amount"] = paymentBill.RemainingAmount
	}

	if req.PaymentRef != "" {
		changes["payment_ref"] = req.PaymentRef
	}
	if req.Notes != "" {
		changes["notes"] = req.Notes
	}

	fs.logAudit(context.Background(), action, "payment_bill", billID, nil, "admin", &paymentBill.EventID, changes)

	// Load associations for response
	if err := fs.db.Preload("Event").Preload("Organizer.OrganizerOnboarding").Preload("Admin").First(&paymentBill, paymentBill.ID).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to load payment bill associations.", err)
	}

	response := paymentBill.ToResponse()
	return &response, nil
}

// DeletePaymentBill deletes a payment bill only if no payments have been made against it
func (fs *FinancialService) DeletePaymentBill(billID uuid.UUID) error {
	var paymentBill models.PaymentBill
	if err := fs.db.Where("id = ?", billID).First(&paymentBill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewNotFoundError("payment bill")
		}
		return utils.NewDatabaseError("Failed to find payment bill.", err)
	}

	// Check if any payments have been made against this bill
	var paymentCount int64
	if err := fs.db.Model(&models.PaymentHistory{}).Where("payment_bill_id = ?", billID).Count(&paymentCount).Error; err != nil {
		return utils.NewDatabaseError("Failed to check payment history.", err)
	}

	// Only allow deletion if no payments have been made
	if paymentCount > 0 {
		return utils.NewBusinessLogicError("Cannot delete bill that has payments associated with it. Please cancel the bill instead.")
	}

	// Store event ID for audit logging before deletion
	eventID := paymentBill.EventID

	// Delete the bill
	if err := fs.db.Delete(&paymentBill).Error; err != nil {
		return utils.NewDatabaseError("Failed to delete payment bill.", err)
	}

	// Log audit for bill deletion
	fs.logAudit(context.Background(), "bill_deleted", "payment_bill", billID, nil, "admin", &eventID, map[string]interface{}{
		"bill_number":   paymentBill.BillNumber,
		"organizer_id":  paymentBill.OrganizerID,
		"billed_amount": paymentBill.BilledAmount,
	})

	return nil
}

// GetPaymentBills returns paginated list of payment bills
func (fs *FinancialService) GetPaymentBills(page, limit int, organizerID *uuid.UUID, status string) ([]models.PaymentBillResponse, int64, error) {
	var paymentBills []models.PaymentBill
	var total int64

	query := fs.db.Model(&models.PaymentBill{}).Preload("Event").Preload("Organizer", func(db *gorm.DB) *gorm.DB {
		return db.Unscoped()
	}).Preload("Organizer.OrganizerOnboarding", func(db *gorm.DB) *gorm.DB {
		return db.Unscoped()
	}).Preload("Admin")

	// Filter by organizer if specified
	if organizerID != nil {
		query = query.Where("organizer_id = ?", *organizerID)
	}

	// Filter by status if specified
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
	responses := make([]models.PaymentBillResponse, 0)
	for _, bill := range paymentBills {
		responses = append(responses, bill.ToResponse())
	}

	return responses, total, nil
}

// GetPaymentBillsWithSearch returns paginated list of payment bills with search functionality
func (fs *FinancialService) GetPaymentBillsWithSearch(page, limit int, organizerID *uuid.UUID, status, search string) ([]models.PaymentBillResponse, int64, error) {
	var paymentBills []models.PaymentBill
	var total int64

	// Build count query (no preloads, no order)
	countQuery := fs.db.Model(&models.PaymentBill{})
	if organizerID != nil {
		countQuery = countQuery.Where("organizer_id = ?", *organizerID)
	}
	if status != "" {
		countQuery = countQuery.Where("status = ?", status)
	}
	if search != "" {
		searchTerm := "%" + search + "%"
		countQuery = countQuery.Joins("LEFT JOIN events ON payment_bills.event_id = events.id").
			Joins("LEFT JOIN users ON payment_bills.organizer_id = users.id").
			Where(`
					payment_bills.id::text ILIKE ? OR
					payment_bills.payment_ref ILIKE ? OR
					events.title ILIKE ? OR
					users.first_name ILIKE ? OR
					users.last_name ILIKE ? OR
					CONCAT(users.first_name, ' ', users.last_name) ILIKE ?
				`, searchTerm, searchTerm, searchTerm, searchTerm, searchTerm, searchTerm)
	}
	if err := countQuery.Select("COUNT(DISTINCT payment_bills.id)").Scan(&total).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to count payment bills.", err)
	}

	// Build data query (with preloads, order, pagination)
	dataQuery := fs.db.Model(&models.PaymentBill{}).
		Preload("Event").
		Preload("Organizer", func(db *gorm.DB) *gorm.DB { return db.Unscoped() }).
		Preload("Organizer.OrganizerOnboarding", func(db *gorm.DB) *gorm.DB { return db.Unscoped() }).
		Preload("Admin")
	if organizerID != nil {
		dataQuery = dataQuery.Where("organizer_id = ?", *organizerID)
	}
	if status != "" {
		dataQuery = dataQuery.Where("status = ?", status)
	}
	if search != "" {
		searchTerm := "%" + search + "%"
		dataQuery = dataQuery.Joins("LEFT JOIN events ON payment_bills.event_id = events.id").
			Joins("LEFT JOIN users ON payment_bills.organizer_id = users.id").
			Where(`
					payment_bills.id::text ILIKE ? OR
					payment_bills.payment_ref ILIKE ? OR
					events.title ILIKE ? OR
					users.first_name ILIKE ? OR
					users.last_name ILIKE ? OR
					CONCAT(users.first_name, ' ', users.last_name) ILIKE ?
				`, searchTerm, searchTerm, searchTerm, searchTerm, searchTerm, searchTerm)
	}
	offset := (page - 1) * limit
	if err := dataQuery.Order("created_at DESC").Offset(offset).Limit(limit).Find(&paymentBills).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to get payment bills.", err)
	}

	responses := make([]models.PaymentBillResponse, 0)
	for _, bill := range paymentBills {
		responses = append(responses, bill.ToResponse())
	}
	return responses, total, nil
}

// billSummaryRow is used for direct SQL scan in GetPaymentBillSummariesWithSearch
type billSummaryRow struct {
	ID              uuid.UUID             `gorm:"column:id"`
	EventID         uuid.UUID             `gorm:"column:event_id"`
	EventTitle      string                `gorm:"column:event_title"`
	OrganizerID     uuid.UUID             `gorm:"column:organizer_id"`
	OrganizerName   string                `gorm:"column:organizer_name"`
	BilledAmount    float64               `gorm:"column:billed_amount"`
	PaidAmount      float64               `gorm:"column:paid_amount"`
	RemainingAmount float64               `gorm:"column:remaining_amount"`
	PaymentMethod   *models.PaymentMethod `gorm:"column:payment_method"`
	Status          string                `gorm:"column:status"`
	CreatedAt       time.Time             `gorm:"column:created_at"`
	UpdatedAt       time.Time             `gorm:"column:updated_at"`
}

// GetPaymentBillSummariesWithSearch returns paginated list of payment bill summaries with search functionality
func (fs *FinancialService) GetPaymentBillSummariesWithSearch(page, limit int, organizerIDs []uuid.UUID, status, search string, startDate, endDate *time.Time, sortBy, sortOrder string) ([]models.PaymentBillSummaryResponse, int64, error) {
	var total int64

	// Base WHERE clause for counts and data
	baseWhere := "pb.deleted_at IS NULL"
	args := []interface{}{}

	if len(organizerIDs) > 0 {
		placeholders := make([]string, len(organizerIDs))
		for i := range placeholders {
			placeholders[i] = "?"
		}
		baseWhere += " AND pb.organizer_id IN (" + strings.Join(placeholders, ",") + ")"
		for _, id := range organizerIDs {
			args = append(args, id)
		}
	}
	if status != "" {
		baseWhere += " AND pb.status = ?"
		args = append(args, status)
	}
	if startDate != nil {
		baseWhere += " AND pb.created_at >= ?"
		args = append(args, *startDate)
	}
	if endDate != nil {
		baseWhere += " AND pb.created_at <= ?"
		args = append(args, *endDate)
	}
	if search != "" {
		searchTerm := "%" + search + "%"
		baseWhere += ` AND (
			pb.id::text ILIKE ? OR
			pb.payment_ref ILIKE ? OR
			e.title ILIKE ? OR
			u.first_name ILIKE ? OR
			u.last_name ILIKE ? OR
			CONCAT(u.first_name, ' ', u.last_name) ILIKE ? OR
			oo.business_name ILIKE ?
		)`
		args = append(args, searchTerm, searchTerm, searchTerm, searchTerm, searchTerm, searchTerm, searchTerm)
	}

	// Count query
	countSQL := `
		SELECT COUNT(DISTINCT pb.id)
		FROM payment_bills pb
		LEFT JOIN events e ON pb.event_id = e.id
		LEFT JOIN users u ON pb.organizer_id = u.id
		LEFT JOIN organizer_onboardings oo ON oo.organizer_id = u.id
		WHERE ` + baseWhere

	if err := fs.db.Raw(countSQL, args...).Scan(&total).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to count payment bills.", err)
	}

	// Generate ORDER BY clause with special handling for computed columns
	var orderByClause string
	if sortBy == "organizer_name" {
		// For organizer_name alias, repeat the expression with LOWER for case-insensitive sorting
		orderByClause = fmt.Sprintf("LOWER(COALESCE(NULLIF(TRIM(oo.business_name), ''), NULLIF(TRIM(CONCAT(u.first_name, ' ', u.last_name)), ''), u.email, '')) %s", sortOrder)
	} else if sortBy == "event_title" {
		// For event_title, use COALESCE with LOWER for case-insensitive sorting
		orderByClause = fmt.Sprintf("COALESCE(LOWER(e.title), '') %s", sortOrder)
	} else {
		orderByClause = utils.GenerateOrderByClause(sortBy, sortOrder)
	}

	dataSQL := `
		SELECT
			pb.id,
			pb.event_id,
			COALESCE(e.title, '')    AS event_title,
			pb.organizer_id,
			COALESCE(
				NULLIF(TRIM(oo.business_name), ''),
				NULLIF(TRIM(CONCAT(u.first_name, ' ', u.last_name)), ''),
				u.email,
				''
			) AS organizer_name,
			pb.billed_amount,
			pb.paid_amount,
			pb.remaining_amount,
			pb.payment_method,
			pb.status,
			pb.created_at,
			pb.updated_at
		FROM payment_bills pb
		LEFT JOIN events e ON pb.event_id = e.id
		LEFT JOIN users u ON pb.organizer_id = u.id
		LEFT JOIN organizer_onboardings oo ON oo.organizer_id = u.id
		WHERE ` + baseWhere + `
		ORDER BY ` + orderByClause + `
		LIMIT ? OFFSET ?`

	var rows []billSummaryRow
	dataArgs := append(args, limit, (page-1)*limit)
	if err := fs.db.Raw(dataSQL, dataArgs...).Scan(&rows).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to get payment bills.", err)
	}

	summaries := make([]models.PaymentBillSummaryResponse, 0, len(rows))
	for _, r := range rows {
		summaries = append(summaries, models.PaymentBillSummaryResponse{
			ID: r.ID,
			Event: models.PaymentBillSummaryEvent{
				ID:    r.EventID,
				Title: r.EventTitle,
			},
			Organizer: models.PaymentBillSummaryOrganizer{
				ID:   r.OrganizerID,
				Name: r.OrganizerName,
			},
			BilledAmount:    r.BilledAmount,
			PaidAmount:      r.PaidAmount,
			RemainingAmount: r.RemainingAmount,
			PaymentMethod:   r.PaymentMethod,
			Status:          r.Status,
			CreatedAt:       r.CreatedAt,
			UpdatedAt:       r.UpdatedAt,
		})
	}
	return summaries, total, nil
}

// GetPaymentBillByID returns a specific payment bill by ID
func (fs *FinancialService) GetPaymentBillByID(billID uuid.UUID) (*models.PaymentBillResponse, error) {
	var paymentBill models.PaymentBill
	if err := fs.db.
		Preload("Event").
		Preload("Organizer", func(db *gorm.DB) *gorm.DB { return db.Unscoped() }).
		Preload("Organizer.OrganizerOnboarding", func(db *gorm.DB) *gorm.DB { return db.Unscoped() }).
		Preload("Admin").
		Where("id = ?", billID).First(&paymentBill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payment bill")
		}
		return nil, utils.NewDatabaseError("Failed to get payment bill.", err)
	}

	// If organizer name is still empty after preload, resolve via direct SQL
	if paymentBill.Organizer == nil || (paymentBill.Organizer.FirstName == "" && paymentBill.Organizer.LastName == "") {
		var resolvedName string
		fs.db.Raw(`
			SELECT COALESCE(
				NULLIF(TRIM(oo.business_name), ''),
				NULLIF(TRIM(CONCAT(u.first_name, ' ', u.last_name)), ''),
				u.email, ''
			) AS organizer_name
			FROM users u
			LEFT JOIN organizer_onboardings oo ON oo.organizer_id = u.id
			WHERE u.id = ?`, paymentBill.OrganizerID).Scan(&resolvedName)
		if paymentBill.Organizer == nil {
			paymentBill.Organizer = &models.User{ID: paymentBill.OrganizerID, FirstName: resolvedName}
		} else {
			paymentBill.Organizer.FirstName = resolvedName
		}
	}

	response := paymentBill.ToResponse()
	return &response, nil
}

// AddPaymentToBill adds a payment record to an existing bill
func (fs *FinancialService) AddPaymentToBill(billID uuid.UUID, payment *models.PaymentHistory) (*models.PaymentBillResponse, error) {
	var paymentBill models.PaymentBill
	if err := fs.db.Where("id = ?", billID).First(&paymentBill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payment bill")
		}
		return nil, utils.NewDatabaseError("Failed to find payment bill.", err)
	}

	// Validate payment amount
	if payment.Amount > paymentBill.RemainingAmount {
		return nil, utils.NewValidationError("Payment amount cannot exceed remaining amount", nil)
	}

	// Create payment history record and update bill in transaction
	err := fs.db.Transaction(func(tx *gorm.DB) error {
		// Log payment details before creation
		fmt.Printf("[DEBUG] Creating payment with screenshot URL: %s\n", payment.ScreenshotURL)
		// Create payment history
		if err := tx.Create(payment).Error; err != nil {
			return err
		}
		fmt.Printf("[DEBUG] Payment created successfully with ID: %d\n", payment.ID)

		// Update bill amounts
		paymentBill.PaidAmount += payment.Amount
		paymentBill.RemainingAmount -= payment.Amount

		// Update bill status
		if paymentBill.RemainingAmount <= 0 {
			paymentBill.Status = "paid"
			now := time.Now()
			paymentBill.PaidDate = &now
		} else {
			paymentBill.Status = "partially_paid"
		}

		return tx.Save(&paymentBill).Error
	})

	if err != nil {
		return nil, utils.NewDatabaseError("Failed to add payment to bill.", err)
	}

	// Log audit for payment addition
	fs.logAudit(context.Background(), "bill_payment_recorded", "payment_bill", billID, &payment.ProcessedByID, "admin", &paymentBill.EventID, map[string]interface{}{
		"payment_amount":   payment.Amount,
		"payment_method":   payment.PaymentMethod,
		"payment_ref":      payment.PaymentRef,
		"new_paid_amount":  paymentBill.PaidAmount,
		"remaining_amount": paymentBill.RemainingAmount,
		"bill_status":      paymentBill.Status,
	})

	// Load associations for response
	if err := fs.db.Preload("Event").Preload("Organizer.OrganizerOnboarding").Preload("Admin").First(&paymentBill, paymentBill.ID).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to load payment bill associations.", err)
	}

	response := paymentBill.ToResponse()
	return &response, nil
}

// GetBillPaymentHistory returns payment history for a specific bill with filtering and sorting
func (fs *FinancialService) GetBillPaymentHistory(billID uuid.UUID, search, paymentMethod string, startDate, endDate *time.Time, sortBy, sortOrder string, limit int) ([]models.PaymentHistoryResponse, error) {
	query := fs.db.
		Preload("ProcessedBy").
		Where("payment_bill_id = ?", billID)

	// Add search filter if provided
	if search != "" {
		searchTerm := "%" + search + "%"
		query = query.Where("payment_ref ILIKE ? OR notes ILIKE ? OR payment_method ILIKE ?",
			searchTerm, searchTerm, searchTerm)
	}

	// Add payment method filter if provided
	if paymentMethod != "" {
		query = query.Where("payment_method = ?", paymentMethod)
	}

	// Add date range filters
	if startDate != nil {
		query = query.Where("payment_date >= ?", *startDate)
	}
	if endDate != nil {
		query = query.Where("payment_date <= ?", *endDate)
	}

	// Apply sorting (already validated by handler)
	// Handle special case for processed_by sorting (requires JOIN)
	if sortBy == "processed_by" {
		query = query.Joins("LEFT JOIN users u ON payment_histories.processed_by_id = u.id").
			Order("LOWER(COALESCE(NULLIF(TRIM(CONCAT(u.first_name, ' ', u.last_name)), ''), u.email, '')) " + sortOrder)
	} else {
		orderByClause := utils.GenerateOrderByClause(sortBy, sortOrder)
		query = query.Order(orderByClause)
	}

	// Apply limit if specified
	if limit > 0 {
		query = query.Limit(limit)
	}

	var payments []models.PaymentHistory
	if err := query.Find(&payments).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to get payment history.", err)
	}

	// Convert to response format
	var responses []models.PaymentHistoryResponse
	for _, payment := range payments {
		responses = append(responses, payment.ToResponse())
	}

	return responses, nil
}

// getBillType returns the bill type based on auto-calculation flag
func (fs *FinancialService) getBillType(autoCalculate bool) string {
	if autoCalculate {
		return "auto_calculated"
	}
	return "manual"
}

// SetBillScreenshot persists a payment proof screenshot URL on an existing bill.
func (fs *FinancialService) SetBillScreenshot(billID uuid.UUID, screenshotURL string) error {
	return fs.db.Model(&models.PaymentBill{}).Where("id = ?", billID).Update("payment_screenshot_url", screenshotURL).Error
}

// SetPaymentHistoryScreenshot persists a payment proof screenshot URL on an existing payment history record.
func (fs *FinancialService) SetPaymentHistoryScreenshot(paymentID uuid.UUID, screenshotURL string) error {
	return fs.db.Model(&models.PaymentHistory{}).Where("id = ?", paymentID).Update("screenshot_url", screenshotURL).Error
}

// generateBillNumber creates a unique bill identifier
func (fs *FinancialService) generateBillNumber() string {
	now := time.Now()
	return "BILL-" + now.Format("20060102") + "-" + uuid.New().String()[:8]
}

// GetCompanyInfo fetches company information from the company info API
func (fs *FinancialService) GetCompanyInfo() (models.UserTransactionInvoiceInfo, error) {
	resp, err := http.Get("http://localhost:8080/api/v1/company/info")
	if err != nil {
		return models.UserTransactionInvoiceInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return models.UserTransactionInvoiceInfo{}, fmt.Errorf("company info API returned status %d", resp.StatusCode)
	}

	var company struct {
		Name      string `json:"name"`
		Address   string `json:"address"`
		Phone     string `json:"phone"`
		Email     string `json:"email"`
		TaxNumber string `json:"tax_number"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&company); err != nil {
		return models.UserTransactionInvoiceInfo{}, err
	}

	return models.UserTransactionInvoiceInfo{
		CompanyName:    company.Name,
		CompanyAddress: company.Address,
		CompanyPhone:   company.Phone,
		CompanyEmail:   company.Email,
		TaxNumber:      company.TaxNumber,
	}, nil
}

// GetUserTransactions returns paginated list of user transactions with detailed information
func (fs *FinancialService) GetUserTransactions(userID uuid.UUID, page, limit int, filters models.UserTransactionFilters) ([]models.UserTransactionListingResponse, int64, error) {
	var transactions []models.Transaction
	var total int64

	// Base query for user's transactions (both regular user and guest purchases)
	query := fs.db.Model(&models.Transaction{}).
		Preload("Event").
		Preload("Tier").
		Preload("User").
		Preload("Tickets").
		Preload("Tickets.Tier").
		Where("user_id = ?", userID)

	// Apply filters
	if filters.PaymentMethod != "" {
		query = query.Where("transactions.payment_gateway = ?", filters.PaymentMethod)
	}

	if filters.Search != "" {
		// Case-insensitive partial match on event title
		query = query.Joins("LEFT JOIN events ON transactions.event_id = events.id").
			Where("events.title ILIKE ?", "%"+filters.Search+"%")
	}

	if filters.DateFrom != nil {
		query = query.Where("transactions.created_at >= ?", *filters.DateFrom)
	}

	if filters.DateTo != nil {
		query = query.Where("transactions.created_at <= ?", *filters.DateTo)
	}

	// Count total records
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to count user transactions.", err)
	}

	// Get paginated results
	offset := (page - 1) * limit
	if err := query.Order("transactions.created_at DESC").Offset(offset).Limit(limit).Find(&transactions).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to get user transactions.", err)
	}

	// Convert to response format
	responses := make([]models.UserTransactionListingResponse, 0, len(transactions))
	for _, transaction := range transactions {
		response, err := fs.convertToUserTransactionListingResponse(transaction)
		if err != nil {
			continue // Skip transactions that can't be converted
		}
		responses = append(responses, *response)
	}

	return responses, total, nil
}

// convertToUserTransactionListingResponse converts a Transaction model to UserTransactionListingResponse
func (fs *FinancialService) convertToUserTransactionListingResponse(transaction models.Transaction) (*models.UserTransactionListingResponse, error) {
	// Get event information
	eventInfo := models.UserTransactionEventInfo{}
	if transaction.Event != nil {
		eventInfo = models.UserTransactionEventInfo{
			ID:          transaction.Event.ID,
			Title:       transaction.Event.Title,
			BannerImage: transaction.Event.BannerImage,
		}
	}

	// Get tiers information from tickets with quantities and prices
	tiersMap := make(map[uuid.UUID]*models.UserTransactionTierInfo)
	for _, ticket := range transaction.Tickets {
		if ticket.Tier != nil {
			if tierInfo, exists := tiersMap[ticket.TierID]; exists {
				// Increment quantity for existing tier
				tierInfo.Quantity++
			} else {
				// Create new tier entry
				tiersMap[ticket.TierID] = &models.UserTransactionTierInfo{
					ID:       ticket.TierID,
					Name:     ticket.Tier.TierName,
					Quantity: 1,
					Price:    ticket.TotalAmount, // Price per ticket for this tier
				}
			}
		}
	}

	// Convert map to slice
	tiers := make([]models.UserTransactionTierInfo, 0, len(tiersMap))
	for _, tier := range tiersMap {
		tiers = append(tiers, *tier)
	}

	// User information
	userName := ""
	if transaction.User != nil {
		userName = transaction.User.FirstName + " " + transaction.User.LastName
	}
	userInfo := models.UserTransactionUserInfo{
		ID:                 transaction.UserID,
		Name:               userName,
		TransactionDetails: transaction.GatewayTxnID,
	}

	// Processed by information (for refunds, this might be admin)
	if transaction.Status == "refunded" {
		// For refunded transactions, we might need to get who processed the refund
		// For now, we'll leave it as nil since we don't have refund tracking yet
		userInfo.ProcessedBy = nil
	}

	// Determine payment method string
	paymentMethod := string(transaction.PaymentGateway)
	paymentIntentID := ""
	transactionRef := ""
	if transaction.GatewayData != nil {
		if method, ok := transaction.GatewayData["payment_method"].(string); ok {
			paymentMethod = method
		}
		if piID, ok := transaction.GatewayData["payment_intent_id"].(string); ok {
			paymentIntentID = piID
		}
		if ref, ok := transaction.GatewayData["txn_id"].(string); ok {
			transactionRef = ref
		} else if ref, ok := transaction.GatewayData["transaction_id"].(string); ok {
			transactionRef = ref
		}
	}

	response := &models.UserTransactionListingResponse{
		ID:              transaction.ID,
		Event:           eventInfo,
		Tiers:           tiers,
		Price:           transaction.Amount,
		Status:          transaction.Status,
		Date:            transaction.CreatedAt,
		PaymentMethod:   paymentMethod,
		PaymentIntentID: paymentIntentID,
		TransactionRef:  transactionRef,
		User:            userInfo,
	}

	return response, nil
}

// GetAuditLogs retrieves audit logs with filtering and pagination
func (fs *FinancialService) GetAuditLogs(req models.GetAuditLogsRequest) (*models.GetAuditLogsResponse, error) {
	query := fs.db.Model(&models.PaymentAuditLog{}).Preload("Actor").Preload("Event")

	// Apply filters
	if req.Action != "" {
		query = query.Where("action = ?", req.Action)
	}
	if req.EntityType != "" {
		query = query.Where("entity_type = ?", req.EntityType)
	}
	if req.EntityID != uuid.Nil {
		query = query.Where("entity_id = ?", req.EntityID)
	}
	if req.ActorID != uuid.Nil {
		query = query.Where("actor_id = ?", req.ActorID)
	}
	if req.ActorType != "" {
		query = query.Where("actor_type = ?", req.ActorType)
	}
	if req.EventID != uuid.Nil {
		query = query.Where("event_id = ?", req.EventID)
	}

	// Date range filter
	if !req.StartDate.IsZero() {
		query = query.Where("timestamp >= ?", req.StartDate)
	}
	if !req.EndDate.IsZero() {
		query = query.Where("timestamp <= ?", req.EndDate)
	}

	// Get total count for pagination
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}

	// Apply pagination and ordering
	offset := (req.Page - 1) * req.Limit
	orderClause := req.SortBy + " " + req.SortOrder
	var logs []models.PaymentAuditLog
	if err := query.Order(orderClause).Offset(offset).Limit(req.Limit).Find(&logs).Error; err != nil {
		return nil, err
	}

	// Convert to minimal audit logs
	minimalLogs := make([]models.MinimalAuditLog, len(logs))
	for i, log := range logs {
		minimalLogs[i] = models.MinimalAuditLog{
			ID:         log.ID,
			Action:     log.Action,
			EntityType: log.EntityType,
			EntityID:   log.EntityID,
			Timestamp:  log.Timestamp,
			CreatedAt:  log.CreatedAt,
		}

		// Add minimal actor info if available
		if log.Actor != nil {
			minimalLogs[i].Actor = &models.MinimalUser{
				ID:    log.Actor.ID,
				Name:  log.Actor.FirstName + " " + log.Actor.LastName,
				Email: log.Actor.Email,
			}
		}

		// Add minimal event info if available
		if log.Event != nil {
			minimalLogs[i].Event = &models.MinimalEvent{
				ID:          log.Event.ID,
				Title:       log.Event.Title,
				BannerImage: log.Event.BannerImage,
				OrganizerID: log.Event.OrganizerID,
			}
		}
	}

	// Calculate total pages
	totalPages := (total + int64(req.Limit) - 1) / int64(req.Limit)

	return &models.GetAuditLogsResponse{
		Logs: minimalLogs,
		Pagination: models.PaginationResponse{
			Total:      total,
			Page:       req.Page,
			Limit:      req.Limit,
			TotalPages: totalPages,
		},
	}, nil
}

// RetryTransaction creates a new payment session for a failed transaction
func (fs *FinancialService) RetryTransaction(userID, transactionID uuid.UUID, ticketService interface{}) (map[string]interface{}, error) {
	// Find the transaction
	var transaction models.Transaction
	if err := fs.db.Preload("Tickets").Preload("Tickets.Tier").Preload("Event").
		Where("id = ? AND (user_id = ? OR guest_user_id = ?)", transactionID, userID, userID).
		First(&transaction).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewBusinessLogicError("Transaction not found or access denied.")
		}
		return nil, utils.NewDatabaseError("Failed to find transaction.", err)
	}

	// Check if transaction can be retried
	if transaction.Status == "completed" {
		return nil, utils.NewBusinessLogicError("Transaction is already completed and cannot be retried.")
	}

	if transaction.Status == "refunded" {
		return nil, utils.NewBusinessLogicError("Transaction has been refunded and cannot be retried.")
	}

	// Check if tickets are still available (not used/cancelled)
	var activeTickets int64
	for _, ticket := range transaction.Tickets {
		if ticket.Status == "active" || ticket.Status == "pending_payment" {
			activeTickets++
		}
	}

	if activeTickets == 0 {
		return nil, utils.NewBusinessLogicError("No active tickets found in this transaction to retry.")
	}

	// Group tickets by tier for the retry purchase
	tierSelections := make(map[uuid.UUID]int)
	for _, ticket := range transaction.Tickets {
		if ticket.Status == "active" || ticket.Status == "pending_payment" {
			tierSelections[ticket.TierID]++
		}
	}

	// Convert to the format expected by purchase API
	var selections []models.TicketTierSelection
	for tierID, quantity := range tierSelections {
		selections = append(selections, models.TicketTierSelection{
			TierID:   tierID,
			Quantity: quantity,
		})
	}

	// Create retry purchase request
	retryReq := &models.TicketPurchaseRequest{
		EventID:        transaction.EventID,
		Tiers:          selections,
		PaymentGateway: transaction.PaymentGateway,
	}

	// Use the ticket service to create a new checkout session
	// We need to cast the interface back to the concrete type
	ts, ok := ticketService.(*TicketService)
	if !ok {
		return nil, utils.NewInternalServerError("Invalid ticket service type", nil)
	}

	checkoutSession, _, err := ts.InitiateUserPaymentGatewayPurchase(userID, retryReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create retry checkout session: %w", err)
	}

	// Return the checkout information
	checkoutURL := ""
	if url, ok := checkoutSession.GatewayData["url"].(string); ok {
		checkoutURL = url
	}

	return map[string]interface{}{
		"checkout_url":   checkoutURL,
		"checkout_token": checkoutSession.CheckoutToken,
		"transaction_id": transactionID.String(),
		"amount":         transaction.Amount,
		"currency":       transaction.Currency,
		"ticket_count":   activeTickets,
	}, nil
}
