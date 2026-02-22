package services

import (
	"errors"
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

// REMOVED: All EventSales methods - now calculate from Transactions table
// Use these SQL queries instead:
// - Total tickets sold per event: SELECT SUM(quantity) FROM transactions WHERE event_id = ? AND status = 'completed'
// - Gross revenue per event: SELECT SUM(amount) FROM transactions WHERE event_id = ? AND status = 'completed'
// - Commission earned: SELECT SUM(commission_amount) FROM transactions WHERE event_id = ? AND status = 'completed'
// - Organizer earnings: SELECT SUM(organizer_share) FROM transactions WHERE event_id = ? AND status = 'completed'

// CreatePaymentBill creates a payment bill for organizer payout (single event per bill)
func (fs *FinancialService) CreatePaymentBill(adminID uuid.UUID, req models.CreatePaymentBillRequest) (*models.PaymentBillResponse, error) {
	// Get event details
	var event models.Event
	if err := fs.db.First(&event, req.EventID).Error; err != nil {
		return nil, utils.NewNotFoundError("event")
	}

	// Get organizer details
	var organizer models.User
	if err := fs.db.First(&organizer, req.OrganizerID).Error; err != nil {
		return nil, utils.NewNotFoundError("organizer")
	}

	// Verify organizer owns the event
	if event.OrganizerID != req.OrganizerID {
		return nil, utils.NewValidationError("Organizer does not own this event", nil)
	}

	var totalRevenue, totalCommission, organizerEarnings float64

	// Auto-calculate from completed transactions for this event
	var transactions []models.Transaction
	err := fs.db.Preload("Event").
		Where("event_id = ? AND status = 'completed'", req.EventID).
		Find(&transactions).Error
	if err != nil {
		return nil, utils.NewDatabaseError("Failed to fetch transactions.", err)
	}

	// Calculate totals from transactions
	for _, txn := range transactions {
		totalRevenue += txn.Amount
		totalCommission += txn.CommissionAmount
		organizerEarnings += txn.OrganizerShare
	}

	// Check if already paid for this event (from existing bills)
	var alreadyPaid float64
	err = fs.db.Model(&models.PaymentBill{}).
		Where("event_id = ? AND status IN ('paid', 'partially_paid')", req.EventID).
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

	// Load associations for response
	if err := fs.db.Preload("Event").Preload("Organizer.OrganizerOnboarding").Preload("Admin").First(&paymentBill, paymentBill.ID).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to load payment bill associations.", err)
	}

	response := paymentBill.ToResponse()
	return &response, nil
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

// GetPaymentBillByID returns a specific payment bill by ID
func (fs *FinancialService) GetPaymentBillByID(billID uuid.UUID) (*models.PaymentBillResponse, error) {
	var paymentBill models.PaymentBill
	if err := fs.db.Preload("Event").Preload("Organizer.OrganizerOnboarding").Preload("Admin").Where("id = ?", billID).First(&paymentBill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payment bill")
		}
		return nil, utils.NewDatabaseError("Failed to get payment bill.", err)
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
		// Create payment history
		if err := tx.Create(payment).Error; err != nil {
			return err
		}

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

	// Load associations for response
	if err := fs.db.Preload("Event").Preload("Organizer.OrganizerOnboarding").Preload("Admin").First(&paymentBill, paymentBill.ID).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to load payment bill associations.", err)
	}

	response := paymentBill.ToResponse()
	return &response, nil
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

// generateBillNumber creates a unique bill identifier
func (fs *FinancialService) generateBillNumber() string {
	now := time.Now()
	return "BILL-" + now.Format("20060102") + "-" + uuid.New().String()[:8]
}

// GetUserTransactions returns paginated list of user transactions with detailed information
func (fs *FinancialService) GetUserTransactions(userID uuid.UUID, page, limit int) ([]models.UserTransactionListingResponse, int64, error) {
	var transactions []models.Transaction
	var total int64

	// Base query for user's transactions (both regular user and guest purchases)
	query := fs.db.Model(&models.Transaction{}).
		Preload("Event").
		Preload("Tier").
		Preload("User").
		Preload("Tickets").
		Where("(user_id = ? OR guest_user_id = ?)", userID, userID)

	// Count total records
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to count user transactions.", err)
	}

	// Get paginated results
	offset := (page - 1) * limit
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&transactions).Error; err != nil {
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
	// Get event title
	eventTitle := ""
	if transaction.Event != nil {
		eventTitle = transaction.Event.Title
	}

	// Get tiers information from tickets
	tiersMap := make(map[uuid.UUID]models.UserTransactionTierInfo)
	for _, ticket := range transaction.Tickets {
		if ticket.Tier != nil {
			tiersMap[ticket.TierID] = models.UserTransactionTierInfo{
				ID:   ticket.TierID,
				Name: ticket.Tier.TierName,
			}
		}
	}

	// Convert map to slice
	tiers := make([]models.UserTransactionTierInfo, 0, len(tiersMap))
	for _, tier := range tiersMap {
		tiers = append(tiers, tier)
	}

	// User information
	userInfo := models.UserTransactionUserInfo{
		ID:                 transaction.UserID,
		TransactionDetails: transaction.GatewayTxnID,
	}

	// Processed by information (for refunds, this might be admin)
	if transaction.Status == "refunded" {
		// For refunded transactions, we might need to get who processed the refund
		// For now, we'll leave it as nil since we don't have refund tracking yet
		userInfo.ProcessedBy = nil
	}

	// Invoice information - get company info from config or database
	invoiceInfo := models.UserTransactionInvoiceInfo{
		CompanyName:    "Event Ticketing Platform", // This should come from config
		CompanyAddress: "Kathmandu, Nepal",         // This should come from config
		CompanyPhone:   "+977-1234567890",          // This should come from config
		CompanyEmail:   "support@timro.com",        // This should come from config
		TaxNumber:      "123456789",                // This should come from config
		InvoiceNumber:  "INV-" + transaction.ID.String()[:8],
		TransactionRef: transaction.GatewayTxnID,
		PaymentGateway: string(transaction.PaymentGateway),
		Currency:       transaction.Currency,
		Subtotal:       transaction.Amount - transaction.CommissionAmount, // Amount before commission
		TaxAmount:      0,                                                 // No tax calculation for now
		TotalAmount:    transaction.Amount,
		IssueDate:      transaction.CreatedAt,
	}

	// Determine payment method string
	paymentMethod := string(transaction.PaymentGateway)
	if transaction.GatewayData != nil {
		if method, ok := transaction.GatewayData["payment_method"].(string); ok {
			paymentMethod = method
		}
	}

	response := &models.UserTransactionListingResponse{
		ID:            transaction.ID,
		EventTitle:    eventTitle,
		Tiers:         tiers,
		Price:         transaction.Amount,
		Status:        transaction.Status,
		Date:          transaction.CreatedAt,
		PaymentMethod: paymentMethod,
		User:          userInfo,
		Invoice:       invoiceInfo,
	}

	return response, nil
}
