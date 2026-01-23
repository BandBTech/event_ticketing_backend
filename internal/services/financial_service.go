package services

import (
	"errors"
	"fmt"
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

// UpdateEventSales updates event sales when a ticket is purchased
func (fs *FinancialService) UpdateEventSales(eventID uuid.UUID, ticketPrice float64, quantity int, commissionRate float64) error {
	// Get organizer ID from event
	var event models.Event
	if err := fs.db.First(&event, eventID).Error; err != nil {
		return utils.NewNotFoundError("event")
	}

	grossAmount := ticketPrice * float64(quantity)
	commissionAmount := grossAmount * (commissionRate / 100)
	organizerShare := grossAmount - commissionAmount

	// Find or create event sales record
	var eventSales models.EventSales
	err := fs.db.Where("event_id = ?", eventID).First(&eventSales).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Create new event sales record
		eventSales = models.EventSales{
			EventID:          eventID,
			OrganizerID:      event.OrganizerID,
			TotalTicketsSold: quantity,
			GrossRevenue:     grossAmount,
			CommissionRate:   commissionRate,
			CommissionAmount: commissionAmount,
			OrganizerShare:   organizerShare,
			PaidAmount:       0,
			DueAmount:        organizerShare,
		}

		if err := fs.db.Create(&eventSales).Error; err != nil {
			return utils.NewDatabaseError("Failed to create event sales.", err)
		}
	} else if err != nil {
		return utils.NewDatabaseError("Failed to query event sales.", err)
	} else {
		// Update existing record
		eventSales.TotalTicketsSold += quantity
		eventSales.GrossRevenue += grossAmount
		eventSales.CommissionAmount += commissionAmount
		eventSales.OrganizerShare += organizerShare
		eventSales.DueAmount = eventSales.OrganizerShare - eventSales.PaidAmount

		if err := fs.db.Save(&eventSales).Error; err != nil {
			return utils.NewDatabaseError("Failed to update event sales.", err)
		}
	}

	return nil
}

// GetEventSalesList returns paginated list of event sales for admin
func (fs *FinancialService) GetEventSalesList(page, limit int, organizerID *uuid.UUID) ([]models.EventSalesResponse, int64, error) {
	var eventSales []models.EventSales
	var total int64

	query := fs.db.Model(&models.EventSales{}).Preload("Event").Preload("Organizer").Preload("Organizer.OrganizerOnboarding")

	// Filter by organizer if specified
	if organizerID != nil {
		query = query.Where("organizer_id = ?", *organizerID)
	}

	// Count total records
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to count event sales.", err)
	}

	// Get paginated results
	offset := (page - 1) * limit
	if err := query.Order("updated_at DESC").Offset(offset).Limit(limit).Find(&eventSales).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to get event sales.", err)
	}

	// Convert to response format
	var responses []models.EventSalesResponse
	for _, sale := range eventSales {
		responses = append(responses, sale.ToResponse())
	}

	return responses, total, nil
}

// GetOrganizerSales returns sales data for a specific organizer
func (fs *FinancialService) GetOrganizerSales(organizerID uuid.UUID) ([]models.EventSalesResponse, error) {
	var eventSales []models.EventSales

	err := fs.db.Where("organizer_id = ?", organizerID).
		Preload("Event").
		Preload("Organizer").
		Preload("Organizer.OrganizerOnboarding").
		Order("updated_at DESC").
		Find(&eventSales).Error

	if err != nil {
		return nil, utils.NewDatabaseError("Failed to get organizer sales.", err)
	}

	var responses []models.EventSalesResponse
	for _, sale := range eventSales {
		responses = append(responses, sale.ToResponse())
	}

	return responses, nil
}

// GetAdminFinancialSummary returns overall financial summary for admin dashboard
func (fs *FinancialService) GetAdminFinancialSummary() (*models.AdminFinancialSummary, error) {
	var summary models.AdminFinancialSummary

	// Get total gross revenue and commission data
	var result struct {
		TotalGrossRevenue     float64
		TotalCommissions      float64
		TotalOrganizerShare   float64
		TotalPaidOut          float64
		TotalDue              float64
		TotalTicketsSold      int64
		ActiveEvents          int64
		AverageCommissionRate float64
	}

	err := fs.db.Model(&models.EventSales{}).
		Select(`
			COALESCE(SUM(gross_revenue), 0) as total_gross_revenue,
			COALESCE(SUM(commission_amount), 0) as total_commissions,
			COALESCE(SUM(organizer_share), 0) as total_organizer_share,
			COALESCE(SUM(paid_amount), 0) as total_paid_out,
			COALESCE(SUM(due_amount), 0) as total_due,
			COALESCE(SUM(total_tickets_sold), 0) as total_tickets_sold,
			COUNT(*) as active_events,
			COALESCE(AVG(commission_rate), 0) as average_commission_rate
		`).
		Scan(&result).Error

	if err != nil {
		return nil, utils.NewDatabaseError("Failed to get financial summary.", err)
	}

	summary.TotalGrossRevenue = result.TotalGrossRevenue
	summary.TotalCommissions = result.TotalCommissions
	summary.TotalOrganizerShare = result.TotalOrganizerShare
	summary.TotalPaidOut = result.TotalPaidOut
	summary.TotalDue = result.TotalDue
	summary.TotalTicketsSold = result.TotalTicketsSold
	summary.ActiveEvents = result.ActiveEvents
	summary.AverageCommissionRate = result.AverageCommissionRate

	return &summary, nil
}

// GetOrganizerFinancialSummary returns financial summary for a specific organizer
func (fs *FinancialService) GetOrganizerFinancialSummary(organizerID uuid.UUID) (*models.OrganizerFinancialSummary, error) {
	var summary models.OrganizerFinancialSummary

	// Get organizer info
	var organizer models.User
	if err := fs.db.Preload("OrganizerOnboarding").First(&organizer, "id = ?", organizerID).Error; err != nil {
		return nil, utils.NewNotFoundError("organizer")
	}

	// Get financial data
	var result struct {
		TotalEvents       int64
		TotalTicketsSold  int64
		TotalGrossRevenue float64
		TotalEarnings     float64
		TotalReceived     float64
		AmountDue         float64
	}

	err := fs.db.Model(&models.EventSales{}).
		Select(`
			COUNT(*) as total_events,
			COALESCE(SUM(total_tickets_sold), 0) as total_tickets_sold,
			COALESCE(SUM(gross_revenue), 0) as total_gross_revenue,
			COALESCE(SUM(organizer_share), 0) as total_earnings,
			COALESCE(SUM(paid_amount), 0) as total_received,
			COALESCE(SUM(due_amount), 0) as amount_due
		`).
		Where("organizer_id = ?", organizerID).
		Scan(&result).Error

	if err != nil {
		return nil, utils.NewDatabaseError("Failed to get organizer summary.", err)
	}

	summary.OrganizerID = organizerID
	if organizer.OrganizerOnboarding != nil && organizer.OrganizerOnboarding.BusinessName != "" {
		summary.OrganizerName = organizer.OrganizerOnboarding.BusinessName
	} else {
		summary.OrganizerName = organizer.FirstName + " " + organizer.LastName
	}
	summary.TotalEvents = result.TotalEvents
	summary.TotalTicketsSold = result.TotalTicketsSold
	summary.TotalGrossRevenue = result.TotalGrossRevenue
	summary.TotalEarnings = result.TotalEarnings
	summary.TotalReceived = result.TotalReceived
	summary.AmountDue = result.AmountDue

	return &summary, nil
}

// CreatePaymentBill creates a new payment bill for an organizer
func (fs *FinancialService) CreatePaymentBill(adminID uuid.UUID, req models.CreatePaymentBillRequest) (*models.PaymentBillResponse, error) {
	// Validate event and organizer
	var eventSales models.EventSales
	err := fs.db.Where("event_id = ? AND organizer_id = ?", req.EventID, req.OrganizerID).
		First(&eventSales).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("no sales found for this event and organizer")
		}
		return nil, utils.NewDatabaseError("Failed to validate event sales.", err)
	}

	// Check if amount doesn't exceed due amount
	if req.BillAmount > eventSales.DueAmount {
		return nil, utils.NewBusinessLogicError(fmt.Sprintf("Bill amount (%.2f) exceeds due amount (%.2f).", req.BillAmount, eventSales.DueAmount))
	}

	// Create payment bill
	paymentBill := models.PaymentBill{
		EventID:       req.EventID,
		OrganizerID:   req.OrganizerID,
		AdminID:       adminID,
		BillAmount:    req.BillAmount,
		PaymentMethod: req.PaymentMethod,
		PaymentRef:    req.PaymentRef,
		Notes:         req.Notes,
		Status:        "pending",
		BillDate:      time.Now(),
	}

	if err := fs.db.Create(&paymentBill).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to create payment bill.", err)
	}

	// Load relations for response
	if err := fs.db.Preload("Event").Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Admin").First(&paymentBill, paymentBill.ID).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to load payment bill relations.", err)
	}

	response := paymentBill.ToResponse()
	return &response, nil
}

// UpdatePaymentBill updates the status of a payment bill
func (fs *FinancialService) UpdatePaymentBill(billID uint, req models.UpdatePaymentBillRequest) (*models.PaymentBillResponse, error) {
	var paymentBill models.PaymentBill
	if err := fs.db.First(&paymentBill, billID).Error; err != nil {
		return nil, utils.NewNotFoundError("payment bill")
	}

	// Update fields
	paymentBill.Status = req.Status
	if req.PaymentRef != "" {
		paymentBill.PaymentRef = req.PaymentRef
	}
	if req.Notes != "" {
		paymentBill.Notes = req.Notes
	}

	// Set paid date if marking as paid
	if req.Status == "paid" && paymentBill.PaidDate == nil {
		now := time.Now()
		paymentBill.PaidDate = &now

		// Update event sales paid amount
		err := fs.db.Transaction(func(tx *gorm.DB) error {
			var eventSales models.EventSales
			if err := tx.Where("event_id = ? AND organizer_id = ?", paymentBill.EventID, paymentBill.OrganizerID).
				First(&eventSales).Error; err != nil {
				return err
			}

			eventSales.PaidAmount += paymentBill.BillAmount
			eventSales.DueAmount = eventSales.OrganizerShare - eventSales.PaidAmount
			eventSales.LastPaymentDate = &now

			return tx.Save(&eventSales).Error
		})

		if err != nil {
			return nil, utils.NewDatabaseError("Failed to update event sales.", err)
		}
	}

	if err := fs.db.Save(&paymentBill).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to update payment bill.", err)
	}

	// Load relations for response
	if err := fs.db.Preload("Event").Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Admin").First(&paymentBill, paymentBill.ID).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to load payment bill relations.", err)
	}

	response := paymentBill.ToResponse()
	return &response, nil
}

// GetPaymentBills returns paginated list of payment bills
func (fs *FinancialService) GetPaymentBills(page, limit int, organizerID *uuid.UUID, status string) ([]models.PaymentBillResponse, int64, error) {
	var bills []models.PaymentBill
	var total int64

	query := fs.db.Model(&models.PaymentBill{}).Preload("Event").Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Admin")

	// Apply filters
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
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&bills).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to get payment bills.", err)
	}

	// Convert to response format
	var responses []models.PaymentBillResponse
	for _, bill := range bills {
		responses = append(responses, bill.ToResponse())
	}

	return responses, total, nil
}

// GetPaymentBillByID returns a specific payment bill
func (fs *FinancialService) GetPaymentBillByID(billID uint) (*models.PaymentBillResponse, error) {
	var paymentBill models.PaymentBill

	err := fs.db.Preload("Event").Preload("Organizer").Preload("Organizer.OrganizerOnboarding").Preload("Admin").
		First(&paymentBill, billID).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("payment bill")
		}
		return nil, utils.NewDatabaseError("Failed to get payment bill.", err)
	}

	response := paymentBill.ToResponse()
	return &response, nil
}
