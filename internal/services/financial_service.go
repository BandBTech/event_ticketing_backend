package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
		PaymentGateway: models.PaymentGateway(transaction.Provider),
	}

	// Use the ticket service to create a new checkout session
	// We need to cast the interface back to the concrete type
	ts, ok := ticketService.(*TicketService)
	if !ok {
		return nil, utils.NewInternalServerError("Invalid ticket service type", nil)
	}

	paymentIntent, _, err := ts.InitiateUserPaymentGatewayPurchase(userID, retryReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create retry payment intent: %w", err)
	}

	// Return the payment intent information
	checkoutURL := ""
	if paymentAttempts, err := db.WithContext(ctx).Model(&models.PaymentAttempt{}).
		Where("payment_intent_id = ? AND status != ?", paymentIntent.ID, "failed").
		Order("created_at DESC").
		Find(&paymentAttempts).Error; err == nil && len(paymentAttempts) > 0 {
		if url, ok := paymentAttempts[0].ProviderData["url"].(string); ok {
			checkoutURL = url
		}
	}

	return map[string]interface{}{
		"checkout_url":   checkoutURL,
		"checkout_token": paymentIntent.CheckoutToken,
		"transaction_id": transactionID.String(),
		"amount":         transaction.Amount,
		"currency":       transaction.Currency,
		"ticket_count":   activeTickets,
	}, nil
}
