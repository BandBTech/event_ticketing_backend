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

// CreatePaymentBill creates a payment bill for organizer payout
func (fs *FinancialService) CreatePaymentBill(adminID uuid.UUID, req models.CreatePaymentBillRequest) (*models.PaymentBillResponse, error) {
	// Get event details to validate
	var event models.Event
	if err := fs.db.First(&event, req.EventID).Error; err != nil {
		return nil, utils.NewNotFoundError("event")
	}

	// Get organizer details
	var organizer models.User
	if err := fs.db.First(&organizer, req.OrganizerID).Error; err != nil {
		return nil, utils.NewNotFoundError("organizer")
	}

	// Create payment bill
	paymentBill := &models.PaymentBill{
		EventID:       req.EventID,
		OrganizerID:   req.OrganizerID,
		AdminID:       adminID,
		BillAmount:    req.BillAmount,
		PaymentMethod: req.PaymentMethod,
		PaymentRef:    req.PaymentRef,
		Status:        "pending",
		Notes:         req.Notes,
		BillDate:      time.Now(),
	}

	if err := fs.db.Create(paymentBill).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to create payment bill.", err)
	}

	// Load associations for response
	if err := fs.db.Preload("Event").Preload("Organizer").Preload("Admin").First(paymentBill, paymentBill.ID).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to load payment bill associations.", err)
	}

	response := paymentBill.ToResponse()
	return &response, nil
}

// UpdatePaymentBill updates payment bill status
func (fs *FinancialService) UpdatePaymentBill(billID uint, req models.UpdatePaymentBillRequest) (*models.PaymentBillResponse, error) {
	var paymentBill models.PaymentBill
	if err := fs.db.First(&paymentBill, billID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payment bill")
		}
		return nil, utils.NewDatabaseError("Failed to find payment bill.", err)
	}

	// Update fields
	if req.Status != "" {
		paymentBill.Status = req.Status
		if req.Status == "paid" {
			now := time.Now()
			paymentBill.PaidDate = &now
		}
	}
	if req.PaymentRef != "" {
		paymentBill.PaymentRef = req.PaymentRef
	}
	if req.Notes != "" {
		paymentBill.Notes = req.Notes
	}

	if err := fs.db.Save(&paymentBill).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to update payment bill.", err)
	}

	// Load associations for response
	if err := fs.db.Preload("Event").Preload("Organizer").Preload("Admin").First(&paymentBill, paymentBill.ID).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to load payment bill associations.", err)
	}

	response := paymentBill.ToResponse()
	return &response, nil
}

// GetPaymentBills returns paginated list of payment bills
func (fs *FinancialService) GetPaymentBills(page, limit int, organizerID *uuid.UUID, status string) ([]models.PaymentBillResponse, int64, error) {
	var paymentBills []models.PaymentBill
	var total int64

	query := fs.db.Model(&models.PaymentBill{}).Preload("Event").Preload("Organizer").Preload("Admin")

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
	var responses []models.PaymentBillResponse
	for _, bill := range paymentBills {
		responses = append(responses, bill.ToResponse())
	}

	return responses, total, nil
}

// GetPaymentBillByID returns a specific payment bill by ID
func (fs *FinancialService) GetPaymentBillByID(billID uint) (*models.PaymentBillResponse, error) {
	var paymentBill models.PaymentBill
	if err := fs.db.Preload("Event").Preload("Organizer").Preload("Admin").First(&paymentBill, billID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payment bill")
		}
		return nil, utils.NewDatabaseError("Failed to get payment bill.", err)
	}

	response := paymentBill.ToResponse()
	return &response, nil
}
