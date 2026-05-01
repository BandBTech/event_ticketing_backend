package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TransactionService handles all transaction operations
type TransactionService struct {
	db *gorm.DB
}

// NewTransactionService creates a new transaction service instance
func NewTransactionService(db *gorm.DB) *TransactionService {
	return &TransactionService{
		db: db,
	}
}

// RecordTransaction records a transaction for completed purchases
func (ts *TransactionService) RecordTransaction(tickets []*models.Ticket, paymentGateway string, gatewayTxnID string, gatewayData map[string]interface{}, paymentIntentID *uuid.UUID) error {
	return ts.recordTransactionInTx(ts.db, tickets, paymentGateway, gatewayTxnID, gatewayData, "completed", paymentIntentID)
}

// extractGatewayIDs extracts gateway transaction ID and payment intent ID from gateway data
// This consolidates the duplicate extraction logic used in multiple payment paths
func (ts *TransactionService) extractGatewayIDs(gatewayData map[string]interface{}) (string, *uuid.UUID) {
	gatewayTxnID := ""
	var paymentIntentID *uuid.UUID

	if gatewayData != nil {
		// Extract transaction ID
		if txnID, ok := gatewayData["payment_intent_id"].(string); ok {
			gatewayTxnID = txnID
		} else if txnID, ok := gatewayData["txn_id"].(string); ok {
			gatewayTxnID = txnID
		}

		// Extract payment intent ID
		if piID, ok := gatewayData["payment_intent_id"].(string); ok && piID != "" {
			if parsedID, err := uuid.Parse(piID); err == nil {
				paymentIntentID = &parsedID
			}
		}
	}

	return gatewayTxnID, paymentIntentID
}

// recordTransactionInTx is an internal helper that allows recording transactions within an existing transaction
func (ts *TransactionService) recordTransactionInTx(db *gorm.DB, tickets []*models.Ticket, paymentGateway string, gatewayTxnID string, gatewayData map[string]interface{}, status string, paymentIntentID *uuid.UUID) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}
	if len(tickets) == 0 {
		return utils.NewBusinessLogicError("No tickets provided for transaction recording.")
	}
	if tickets[0] == nil {
		return fmt.Errorf("first ticket is nil")
	}

	// Get event details for commission calculation
	var event models.Event
	if err := db.First(&event, tickets[0].EventID).Error; err != nil {
		return fmt.Errorf("failed to get event details: %w", err)
	}

	// Get tier details for currency
	var tier models.EventTier
	if err := db.First(&tier, tickets[0].TierID).Error; err != nil {
		return fmt.Errorf("failed to get tier details: %w", err)
	}

	// Calculate total amount
	totalAmount := 0.0
	for _, ticket := range tickets {
		totalAmount += ticket.TotalAmount
	}

	// Calculate commission
	commissionAmount := totalAmount * (event.CommissionRate / 100)
	organizerShare := totalAmount - commissionAmount

	// Create transaction record (without specific tier_id for multi-tier purchases)
	transaction := &models.Transaction{
		EventID:          tickets[0].EventID,
		TierID:           tickets[0].TierID, // Use first ticket's tier
		UserID:           tickets[0].UserID,
		GuestUserID:      tickets[0].GuestUserID,
		PaymentIntentID:  *paymentIntentID, // Dereference the pointer
		PaymentGateway:   paymentGateway,
		Amount:           int64(totalAmount * 100), // Convert to cents
		Currency:         tier.Currency,
		Quantity:         len(tickets),
		Status:           status,
		GatewayTxnID:     gatewayTxnID,
		GatewayData:      gatewayData,
		CommissionRate:   event.CommissionRate,
		CommissionAmount: int64(commissionAmount * 100), // Convert to cents
		OrganizerShare:   int64(organizerShare * 100),   // Convert to cents
	}

	// Create transaction record
	if err := db.Create(transaction).Error; err != nil {
		return fmt.Errorf("failed to create transaction record: %w", err)
	}

	// Log audit for transaction creation
	ts.logAudit(context.Background(), "transaction_created", "transaction", transaction.ID, nil, "system", &transaction.EventID, map[string]interface{}{
		"amount":            transaction.Amount,
		"currency":          transaction.Currency,
		"quantity":          transaction.Quantity,
		"payment_gateway":   transaction.PaymentGateway,
		"gateway_txn_id":    transaction.GatewayTxnID,
		"commission_rate":   transaction.CommissionRate,
		"commission_amount": transaction.CommissionAmount,
		"organizer_share":   transaction.OrganizerShare,
		"status":            transaction.Status,
		"buyer_type":        "user",
	})

	// Update all tickets with the transaction ID (establishes the relationship)
	// Use individual updates to ensure transaction context is maintained
	for _, ticket := range tickets {
		ticket.TransactionID = &transaction.ID
		result := db.Model(&models.Ticket{}).Where("id = ?", ticket.ID).Update("transaction_id", transaction.ID)
		if result.Error != nil {
			return fmt.Errorf("failed to update ticket %s with transaction_id: %w", ticket.ID.String(), result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("no rows updated for ticket %s (ticket may not exist)", ticket.ID.String())
		}
		log.Printf("[TRANSACTION_LINKING_DETAIL] Updated ticket %s with transaction_id %s (rows affected: %d)", ticket.ID.String(), transaction.ID.String(), result.RowsAffected)
	}
	log.Printf("[TRANSACTION_LINKING] Linked %d tickets to transaction %s", len(tickets), transaction.ID.String())

	log.Printf("Transaction recorded: ID=%s, Amount=%.2f, Gateway=%s, Tickets=%d",
		transaction.ID.String(), totalAmount, paymentGateway, len(tickets))

	return nil
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
		TransactionDetails: transaction.ProviderTxnID,
	}

	// Processed by information (for refunds, this might be admin)
	if transaction.Status == "refunded" {
		// For refunded transactions, we might need to get who processed the refund
		// For now, we'll leave it as nil since we don't have refund tracking yet
		userInfo.ProcessedBy = nil
	}

	// Determine payment method string
	paymentMethod := transaction.Provider
	paymentIntentID := ""
	transactionRef := ""
	if transaction.ProviderData != nil {
		if method, ok := transaction.ProviderData["payment_method"].(string); ok {
			paymentMethod = method
		}
		if piID, ok := transaction.ProviderData["payment_intent_id"].(string); ok {
			paymentIntentID = piID
		}
		if ref, ok := transaction.ProviderData["txn_id"].(string); ok {
			transactionRef = ref
		} else if ref, ok := transaction.ProviderData["transaction_id"].(string); ok {
			transactionRef = ref
		}
	}

	response := &models.UserTransactionListingResponse{
		ID:              transaction.ID,
		Event:           eventInfo,
		Tiers:           tiers,
		Price:           float64(transaction.Amount) / 100, // Convert cents to dollars
		Status:          transaction.Status,
		Date:            transaction.CreatedAt,
		PaymentMethod:   paymentMethod,
		PaymentIntentID: paymentIntentID,
		TransactionRef:  transactionRef,
		User:            userInfo,
	}

	return response, nil
}

// logAudit creates audit log entries for transaction operations
func (ts *TransactionService) logAudit(ctx context.Context, action, entityType string, entityID uuid.UUID, actorID *uuid.UUID, actorType string, eventID *uuid.UUID, changes map[string]interface{}) {
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
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[TRANSACTION_SERVICE] Panic in async audit logging: %v", r)
			}
		}()
		ts.db.Create(audit)
	}()
}
