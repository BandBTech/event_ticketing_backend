package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/currency"
	"event-ticketing-backend/pkg/utils"
)

// BillService handles all bill and payment operations
type BillService struct {
	db *gorm.DB
}

// NewBillService creates a new bill service instance
func NewBillService(db *gorm.DB) *BillService {
	return &BillService{db: db}
}

func resolveOrganizerDisplayName(user *models.User) string {
	if user == nil {
		return ""
	}

	if user.OrganizerOnboarding != nil {
		businessName := strings.TrimSpace(user.OrganizerOnboarding.BusinessName)
		if businessName != "" {
			return businessName
		}
	}

	fullName := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if fullName != "" {
		return fullName
	}

	if email := strings.TrimSpace(user.Email); email != "" {
		return email
	}

	return "Organizer"
}

func (bs *BillService) ensureBillOrganizerLoaded(bill *models.PaymentBill) {
	if bill == nil {
		return
	}

	if bill.Organizer != nil && resolveOrganizerDisplayName(bill.Organizer) != "Organizer" {
		return
	}

	var organizer models.User
	if err := bs.db.Preload("OrganizerOnboarding").First(&organizer, bill.OrganizerID).Error; err == nil {
		bill.Organizer = &organizer
	}
}

// CreatePaymentBill creates a payment bill
func (bs *BillService) CreatePaymentBill(adminID uuid.UUID, req models.CreatePaymentBillRequest) (*models.PaymentBillResponse, error) {
	var event models.Event
	if err := bs.db.First(&event, req.EventID).Error; err != nil {
		return nil, utils.NewNotFoundError("event")
	}

	var organizer models.User
	if err := bs.db.First(&organizer, req.OrganizerID).Error; err != nil {
		return nil, utils.NewNotFoundError("organizer")
	}

	if event.OrganizerID != req.OrganizerID {
		return nil, utils.NewValidationError("Organizer does not own this event", nil)
	}

	billType := req.Type
	if billType == "" {
		billType = models.BillTypePayout
	}

	billCurrency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if billCurrency == "" {
		billCurrency = strings.ToUpper(strings.TrimSpace(event.Currency))
	}
	if billCurrency == "" {
		billCurrency = "USD"
	}

	var amountSmallest int64
	if req.Amount != nil {
		v, err := currency.ToSmallestUnit(*req.Amount, billCurrency)
		if err != nil {
			return nil, utils.NewValidationError("Invalid bill currency/amount", nil)
		}
		if v <= 0 {
			return nil, utils.NewValidationError("Amount must be greater than zero", nil)
		}
		amountSmallest = v
	} else {
		var totalEarning int64
		if err := bs.db.Model(&models.Transaction{}).
			Where("event_id = ? AND status IN ?", req.EventID, []string{"succeeded", "completed"}).
			Select("COALESCE(SUM(organizer_share), 0)").
			Scan(&totalEarning).Error; err != nil {
			return nil, utils.NewDatabaseError("Failed to calculate organizer earnings.", err)
		}

		var totalOrganizerRefund int64
		if err := bs.db.Table("refunds r").
			Select("COALESCE(SUM(r.organizer_refund), 0)").
			Joins("JOIN transactions t ON r.transaction_id = t.id").
			Where("t.event_id = ? AND r.status IN ?", req.EventID, []string{"succeeded", "processing", "completed"}).
			Scan(&totalOrganizerRefund).Error; err != nil {
			return nil, utils.NewDatabaseError("Failed to calculate organizer refunds.", err)
		}

		var existingBilled int64
		if err := bs.db.Model(&models.PaymentBill{}).
			Where("event_id = ? AND bill_type = ? AND status != ?", req.EventID, models.BillTypePayout, models.PaymentBillCancelled).
			Select("COALESCE(SUM(amount), 0)").
			Scan(&existingBilled).Error; err != nil {
			return nil, utils.NewDatabaseError("Failed to calculate existing payout bills.", err)
		}

		amountSmallest = totalEarning - totalOrganizerRefund - existingBilled
		if amountSmallest <= 0 {
			return nil, utils.NewValidationError("No outstanding amount available for billing", nil)
		}
	}

	paymentBill := &models.PaymentBill{
		BillNumber:  bs.generateBillNumber(),
		EventID:     &req.EventID,
		OrganizerID: req.OrganizerID,
		CreatedByID: adminID,
		BillType:    billType,
		Status:      models.PaymentBillPending,
		Currency:    billCurrency,
		Amount:      float64(amountSmallest),
		PaidAmount:  0,
		DueDate:     nil,
		Notes:       req.Notes,
	}

	if err := bs.db.Create(paymentBill).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to create payment bill.", err)
	}

	bs.logAudit(context.Background(), "bill_created", "payment_bill", paymentBill.ID, &adminID, "admin", &req.EventID, map[string]interface{}{
		"bill_number": paymentBill.BillNumber,
		"amount":      paymentBill.Amount,
		"bill_type":   paymentBill.BillType,
		"currency":    paymentBill.Currency,
	})

	if err := bs.db.Preload("Event").Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("CreatedBy").First(paymentBill, paymentBill.ID).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to load payment bill associations.", err)
	}

	resp := paymentBill.ToResponse()
	return &resp, nil
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

	var paymentHistoryCount int64
	if err := bs.db.Model(&models.PaymentHistory{}).Where("payment_bill_id = ?", billID).Count(&paymentHistoryCount).Error; err != nil {
		return utils.NewDatabaseError("Failed to check payment history.", err)
	}
	if paymentHistoryCount > 0 || paymentBill.PaidAmount > 0 {
		return utils.NewValidationError("Cannot delete a bill that has payments", nil)
	}

	if paymentBill.Status != models.PaymentBillCancelled {
		return utils.NewValidationError("Can only delete cancelled bills", nil)
	}

	if err := bs.db.Where("payment_bill_id = ?", billID).Delete(&models.PaymentHistory{}).Error; err != nil {
		return utils.NewDatabaseError("Failed to delete payment history.", err)
	}

	if err := bs.db.Delete(&paymentBill).Error; err != nil {
		return utils.NewDatabaseError("Failed to delete payment bill.", err)
	}

	bs.logAudit(context.Background(), "bill_deleted", "payment_bill", billID, nil, "system", paymentBill.EventID, map[string]interface{}{
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
		Preload("CreatedBy")

	if organizerID != nil {
		query = query.Where("organizer_id = ?", *organizerID)
	}

	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to count payment bills.", err)
	}

	offset := (page - 1) * limit
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&paymentBills).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to get payment bills.", err)
	}

	for i := range paymentBills {
		bs.ensureBillOrganizerLoaded(&paymentBills[i])
	}

	responses := make([]models.PaymentBillResponse, 0, len(paymentBills))
	for i := range paymentBills {
		responses = append(responses, paymentBills[i].ToResponse())
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
		Preload("CreatedBy")

	if organizerID != nil {
		query = query.Where("organizer_id = ?", *organizerID)
	}

	if status != "" {
		query = query.Where("status = ?", status)
	}

	if search != "" {
		search = strings.TrimSpace(search)
		if search != "" {
			searchTerm := "%" + strings.ToLower(search) + "%"
			query = query.Joins("LEFT JOIN events ON payment_bills.event_id = events.id").
				Joins("LEFT JOIN users ON payment_bills.organizer_id = users.id").
				Joins("LEFT JOIN organizer_onboardings ON organizer_onboardings.organizer_id = users.id").
				Where("LOWER(NULLIF(TRIM(events.title), '')) LIKE ? OR LOWER(COALESCE(NULLIF(TRIM(organizer_onboardings.business_name), ''), NULLIF(TRIM(CONCAT(COALESCE(users.first_name, ''), ' ', COALESCE(users.last_name, ''))), ''))) LIKE ?", searchTerm, searchTerm)
		}
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to count payment bills.", err)
	}

	offset := (page - 1) * limit
	if err := query.Order("payment_bills.created_at DESC").Offset(offset).Limit(limit).Find(&paymentBills).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to get payment bills.", err)
	}

	for i := range paymentBills {
		bs.ensureBillOrganizerLoaded(&paymentBills[i])
	}

	responses := make([]models.PaymentBillResponse, 0, len(paymentBills))
	for i := range paymentBills {
		responses = append(responses, paymentBills[i].ToResponse())
	}

	return responses, total, nil
}

// GetPaymentBillSummariesWithSearch returns paginated list of payment bill summaries with advanced search
func (bs *BillService) GetPaymentBillSummariesWithSearch(
	page,
	limit int,
	organizerIDs []uuid.UUID,
	statuses []string,
	search string,
	startDate,
	endDate *time.Time,
	sortBy,
	sortOrder string,
) ([]models.PaymentBillSummaryResponse, int64, error) {

	var paymentBills []models.PaymentBill
	var total int64

	query := bs.db.
		Model(&models.PaymentBill{}).
		Preload("Event").
		Preload("Organizer").
		Preload("Organizer.OrganizerOnboarding").
		Joins("LEFT JOIN events ON payment_bills.event_id = events.id").
		Joins("LEFT JOIN users ON payment_bills.organizer_id = users.id").
		Joins("LEFT JOIN organizer_onboardings ON organizer_onboardings.organizer_id = users.id")

	// =========================
	// Filters
	// =========================

	if len(organizerIDs) > 0 {
		query = query.Where("payment_bills.organizer_id IN ?", organizerIDs)
	}

	if len(statuses) > 0 {
		query = query.Where("payment_bills.status IN ?", statuses)
	}

	if search != "" {
		search = strings.TrimSpace(search)
		if search != "" {
			searchTerm := "%" + strings.ToLower(search) + "%"

			query = query.Where(`
				LOWER(NULLIF(TRIM(events.title), '')) LIKE ?
				OR LOWER(COALESCE(NULLIF(TRIM(organizer_onboardings.business_name), ''), NULLIF(TRIM(CONCAT(COALESCE(users.first_name, ''), ' ', COALESCE(users.last_name, ''))), ''))) LIKE ?
			`,
				searchTerm,
				searchTerm,
			)
		}
	}

	if startDate != nil {
		query = query.Where("payment_bills.created_at >= ?", *startDate)
	}

	if endDate != nil {
		query = query.Where("payment_bills.created_at <= ?", *endDate)
	}

	// =========================
	// Count
	// =========================

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, utils.NewDatabaseError(
			"Failed to count payment bills.",
			err,
		)
	}

	// =========================
	// Sorting
	// =========================

	if sortBy == "" {
		sortBy = "created_at"
	}

	if sortOrder == "" {
		sortOrder = "desc"
	}

	sortOrder = strings.ToUpper(sortOrder)

	if sortOrder != "ASC" && sortOrder != "DESC" {
		sortOrder = "DESC"
	}

	sortColumns := map[string]string{
		"created_at":     "payment_bills.created_at",
		"event_title":    "LOWER(NULLIF(TRIM(events.title), ''))",
		"organizer_name": "LOWER(COALESCE(NULLIF(TRIM(organizer_onboardings.business_name), ''), NULLIF(TRIM(CONCAT(COALESCE(users.first_name, ''), ' ', COALESCE(users.last_name, ''))), ''), NULLIF(TRIM(users.email), '')))",
		"amount":         "payment_bills.amount",
		"billed_amount":  "payment_bills.amount",
		"paid_amount":    "payment_bills.paid_amount",
		"status":         "payment_bills.status",
	}

	sortColumn, ok := sortColumns[sortBy]
	if !ok {
		sortColumn = "payment_bills.created_at"
	}

	orderClause := fmt.Sprintf("%s %s", sortColumn, sortOrder)
	if sortBy == "event_title" || sortBy == "organizer_name" {
		orderClause += " NULLS LAST"
	}
	query = query.Order(orderClause)

	// =========================
	// Pagination
	// =========================

	if page <= 0 {
		page = 1
	}

	if limit <= 0 {
		limit = 10
	}

	offset := (page - 1) * limit

	// =========================
	// Fetch
	// =========================

	if err := query.
		Offset(offset).
		Limit(limit).
		Find(&paymentBills).Error; err != nil {

		return nil, 0, utils.NewDatabaseError(
			"Failed to get payment bill summaries.",
			err,
		)
	}

	for i := range paymentBills {
		bs.ensureBillOrganizerLoaded(&paymentBills[i])
	}

	// =========================
	// Response Mapping
	// =========================

	summaries := make([]models.PaymentBillSummaryResponse, 0, len(paymentBills))

	for i := range paymentBills {
		summaries = append(
			summaries,
			paymentBills[i].ToSummaryResponse(),
		)
	}

	return summaries, total, nil
}

// GetPaymentBillByID returns a single payment bill by ID
func (bs *BillService) GetPaymentBillByID(billID uuid.UUID) (*models.PaymentBillResponse, error) {
	var paymentBill models.PaymentBill
	if err := bs.db.Preload("Event").Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("CreatedBy").First(&paymentBill, billID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payment bill")
		}
		return nil, utils.NewDatabaseError("Failed to find payment bill.", err)
	}

	bs.ensureBillOrganizerLoaded(&paymentBill)

	resp := paymentBill.ToResponse()

	// Compute settlements from transactions for the event (if event linked)
	if paymentBill.EventID != nil {
		var grossRaw int64
		var platformRaw int64
		var gatewayRaw int64
		var organizerRaw int64

		// Sum relevant fields from transactions for this event
		// Note: amount_total, platform_fee, gateway_fee, organizer_share are stored in smallest units
		row := bs.db.Model(&models.Transaction{}).
			Where("event_id = ? AND status IN ?", *paymentBill.EventID, []string{"succeeded", "completed"}).
			Select("COALESCE(SUM(amount_total),0) as gross, COALESCE(SUM(platform_fee),0) as platform, COALESCE(SUM(gateway_fee),0) as gateway, COALESCE(SUM(organizer_share),0) as organizer").
			Row()
		_ = row.Scan(&grossRaw, &platformRaw, &gatewayRaw, &organizerRaw)

		gross, _ := currency.FromSmallestUnit(grossRaw, paymentBill.Event.Currency)
		platform, _ := currency.FromSmallestUnit(platformRaw, paymentBill.Event.Currency)
		gateway, _ := currency.FromSmallestUnit(gatewayRaw, paymentBill.Event.Currency)
		organizerAmt, _ := currency.FromSmallestUnit(organizerRaw, paymentBill.Event.Currency)
		totalAmount, _ := currency.FromSmallestUnit(int64(paymentBill.Amount), paymentBill.Currency)
		paidAmount, _ := currency.FromSmallestUnit(int64(paymentBill.PaidAmount), paymentBill.Currency)
		remainingBalance := totalAmount - paidAmount

		net := gross - platform - gateway

		resp.Settlements = &models.PaymentBillSettlements{
			TotalAmount:        totalAmount,
			PaidAmount:         paidAmount,
			RemainingBalance:   remainingBalance,
			GrossRevenue:       gross,
			NetRevenue:         net,
			PlatformCommission: platform,
			OrganizerEarnings:  organizerAmt,
		}
	}

	return &resp, nil
}

// AddPaymentToBill adds payment to existing bill
func (bs *BillService) AddPaymentToBill(billID uuid.UUID, payment *models.PaymentHistory) (*models.PaymentBillResponse, error) {
	tx := bs.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var paymentBill models.PaymentBill
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", billID).First(&paymentBill).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payment bill")
		}
		return nil, utils.NewDatabaseError("Failed to find payment bill.", err)
	}

	if paymentBill.Status == models.PaymentBillCancelled {
		tx.Rollback()
		return nil, utils.NewValidationError("Cannot add payment to a cancelled bill", nil)
	}

	if payment.Amount <= 0 {
		tx.Rollback()
		return nil, utils.NewValidationError("Payment amount must be greater than zero", nil)
	}

	paymentSmallest, err := currency.ToSmallestUnit(payment.Amount, paymentBill.Currency)
	if err != nil {
		tx.Rollback()
		return nil, utils.NewValidationError("Invalid payment amount/currency", nil)
	}
	if paymentSmallest <= 0 {
		tx.Rollback()
		return nil, utils.NewValidationError("Payment amount must be greater than zero", nil)
	}

	remaining := int64(paymentBill.Amount - paymentBill.PaidAmount)
	if remaining < 0 {
		remaining = 0
	}
	if paymentSmallest > remaining {
		tx.Rollback()
		return nil, utils.NewValidationError("Payment amount cannot exceed remaining amount", nil)
	}

	now := time.Now()
	payment.PaymentBillID = billID
	payment.Amount = float64(paymentSmallest)
	if payment.PaidAt.IsZero() {
		payment.PaidAt = now
	}

	if err := tx.Create(payment).Error; err != nil {
		tx.Rollback()
		return nil, utils.NewDatabaseError("Failed to create payment history.", err)
	}

	paymentBill.PaidAmount += float64(paymentSmallest)
	if paymentBill.PaidAmount >= paymentBill.Amount {
		paymentBill.PaidAmount = paymentBill.Amount
		paymentBill.Status = models.PaymentBillPaid
	} else {
		paymentBill.Status = models.PaymentBillPartiallyPaid
	}
	paymentBill.UpdatedAt = now

	if err := tx.Save(&paymentBill).Error; err != nil {
		tx.Rollback()
		return nil, utils.NewDatabaseError("Failed to update payment bill.", err)
	}

	if paymentBill.BillType == models.BillTypeRefund && paymentBill.Status == models.PaymentBillPaid {
		if err := bs.finalizeRefundsForPaidBill(tx, paymentBill.ID, payment.ProcessedByID, now); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	if err := tx.Commit().Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to commit transaction.", err)
	}

	bs.logAudit(context.Background(), "payment_added", "payment_bill", billID, &payment.ProcessedByID, "admin", paymentBill.EventID, map[string]interface{}{
		"payment_amount": payment.Amount,
		"method":         payment.Method,
		"reference":      payment.Reference,
	})

	if err := bs.db.Preload("Event").Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("CreatedBy").First(&paymentBill, paymentBill.ID).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to load payment bill associations.", err)
	}

	resp := paymentBill.ToResponse()
	return &resp, nil
}

// GetBillPaymentHistory returns payment history for a bill
func (bs *BillService) GetBillPaymentHistory(billID uuid.UUID, search, paymentMethod string, startDate, endDate *time.Time, sortBy, sortOrder string, limit int) ([]models.PaymentHistoryResponse, error) {
	var payments []models.PaymentHistory

	query := bs.db.Model(&models.PaymentHistory{}).
		Preload("ProcessedBy").
		Preload("PaymentBill.Event").
		Where("payment_bill_id = ?", billID)

	if search != "" {
		searchTerm := "%" + search + "%"
		query = query.Where("reference ILIKE ? OR notes ILIKE ?", searchTerm, searchTerm)
	}
	if paymentMethod != "" {
		query = query.Where("method = ?", paymentMethod)
	}
	if startDate != nil {
		query = query.Where("paid_at >= ?", *startDate)
	}
	if endDate != nil {
		query = query.Where("paid_at <= ?", *endDate)
	}

	if sortBy == "" {
		sortBy = "created_at"
	}
	if sortOrder == "" {
		sortOrder = "desc"
	}
	sortColumns := map[string]string{
		"payment_date": "paid_at",
		"paid_at":      "paid_at",
		"amount":       "amount",
		"method":       "method",
		"reference":    "reference",
		"processed_by": "processed_by_id",
		"notes":        "notes",
		"created_at":   "created_at",
	}
	sortColumn, ok := sortColumns[sortBy]
	if !ok {
		sortColumn = "paid_at"
	}
	query = query.Order(fmt.Sprintf("%s %s", sortColumn, sortOrder))

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&payments).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to get payment history.", err)
	}

	responses := make([]models.PaymentHistoryResponse, 0, len(payments))
	for i := range payments {
		responses = append(responses, payments[i].ToResponse())
	}
	return responses, nil
}

// SetBillScreenshot stores bill screenshot as note metadata for backward compatibility
func (bs *BillService) SetBillScreenshot(billID uuid.UUID, screenshotURL string) error {
	var bill models.PaymentBill
	if err := bs.db.First(&bill, billID).Error; err != nil {
		return utils.NewDatabaseError("Failed to find bill.", err)
	}
	if strings.TrimSpace(screenshotURL) == "" {
		return nil
	}
	n := strings.TrimSpace(bill.Notes)
	if n != "" {
		n += "\n"
	}
	n += "Bill screenshot: " + screenshotURL
	if err := bs.db.Model(&models.PaymentBill{}).Where("id = ?", billID).Update("notes", n).Error; err != nil {
		return utils.NewDatabaseError("Failed to update bill notes.", err)
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

func (bs *BillService) finalizeRefundsForPaidBill(tx *gorm.DB, billID uuid.UUID, processedByID uuid.UUID, now time.Time) error {
	var refunds []models.Refund
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("refund_bill_id = ? AND status IN ?", billID, []models.RefundStatus{models.RefundPending, models.RefundProcessing}).
		Find(&refunds).Error; err != nil {
		return utils.NewDatabaseError("Failed to load bill refunds.", err)
	}

	for _, refund := range refunds {
		// Log intermediate transition: pending → processing (if still pending)
		if refund.Status == models.RefundPending {
			if err := tx.Create(&models.RefundStatusHistory{
				ID:            uuid.New(),
				RefundID:      refund.ID,
				OldStatus:     models.RefundPending,
				NewStatus:     models.RefundProcessing,
				ChangedAt:     now,
				ChangedByID:   &processedByID,
				ChangedByType: "admin",
				Remarks:       "auto-approved from pending when bill payment initiated",
			}).Error; err != nil {
				return utils.NewDatabaseError("Failed to create refund status history.", err)
			}
		}

		if err := tx.Model(&models.Refund{}).Where("id = ?", refund.ID).Updates(map[string]any{
			"status":       models.RefundSucceeded,
			"processed_at": now,
			"updated_at":   now,
		}).Error; err != nil {
			return utils.NewDatabaseError("Failed to update refund.", err)
		}

		history := &models.RefundStatusHistory{
			ID:            uuid.New(),
			RefundID:      refund.ID,
			OldStatus:     models.RefundProcessing, // Always transition FROM processing
			NewStatus:     models.RefundSucceeded,
			ChangedAt:     now,
			ChangedByID:   &processedByID,
			ChangedByType: "admin",
			Remarks:       "refund bill fully paid",
		}
		if err := tx.Create(history).Error; err != nil {
			return utils.NewDatabaseError("Failed to create refund status history.", err)
		}

		if refund.TicketID != uuid.Nil {
			if err := tx.Model(&models.Ticket{}).Where("id = ?", refund.TicketID).Updates(map[string]any{
				"status":        models.TicketRefunded,
				"refund_id":     refund.ID,
				"refunded_at":   now,
				"refund_amount": refund.Amount,
				"updated_at":    now,
			}).Error; err != nil {
				return utils.NewDatabaseError("Failed to update ticket refund status.", err)
			}

			if err := tx.Exec(`UPDATE event_tiers SET available = available + 1, sold = GREATEST(sold - 1, 0) WHERE id = (SELECT tier_id FROM tickets WHERE id = ?)`, refund.TicketID).Error; err != nil {
				return utils.NewDatabaseError("Failed to restore ticket inventory.", err)
			}
		} else if err := tx.Model(&models.Ticket{}).
			Where("transaction_id = ? AND status <> ?", refund.TransactionID, models.TicketRefunded).
			Updates(map[string]any{
				"status":      models.TicketRefunded,
				"refund_id":   refund.ID,
				"refunded_at": now,
				"updated_at":  now,
			}).Error; err != nil {
			return utils.NewDatabaseError("Failed to update transaction tickets refund status.", err)
		}

		if err := LogPaymentAuditTx(
			tx,
			"refund_succeeded",
			"refund",
			refund.ID,
			&processedByID,
			"admin",
			&refund.EventID,
			map[string]interface{}{
				"old_status": refund.Status,
				"new_status": models.RefundSucceeded,
				"source":     "refund_bill_payment",
				"bill_id":    billID,
			},
		); err != nil {
			return utils.NewDatabaseError("Failed to log refund audit.", err)
		}
	}

	return nil
}

// generateBillNumber generates a unique bill number
func (bs *BillService) generateBillNumber() string {
	return fmt.Sprintf("BILL-%d", time.Now().UnixNano())
}

// logAudit creates audit log entries for bill operations
func (bs *BillService) logAudit(ctx context.Context, action, entityType string, entityID uuid.UUID, actorID *uuid.UUID, actorType string, eventID *uuid.UUID, changes map[string]interface{}) {
	LogPaymentAuditAsync(bs.db, action, entityType, entityID, actorID, actorType, eventID, changes)
}
