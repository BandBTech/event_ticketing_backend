package services

import (
	"context"
	"strings"

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

// GetUserTransactions returns paginated list of user transactions with detailed information
func (ts *TransactionService) GetUserTransactions(userID uuid.UUID, page, limit int, filters models.UserTransactionFilters) ([]models.UserTransactionListingResponse, int64, error) {
	var transactions []models.Transaction
	var total int64

	// Base query for user's transactions (filter by actor_id and actor_type for logged-in users)
	query := ts.db.Model(&models.Transaction{}).
		Preload("Event").
		Preload("User").
		Where("actor_id = ? AND actor_type = ?", userID, models.ActorUser)

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
	for _, txn := range transactions {
		// Build user info
		userInfo := models.UserTransactionUserInfo{
			TransactionDetails: txn.ProviderChargeID,
		}
		if txn.User != nil {
			userInfo.ID = &txn.User.ID
			userInfo.Name = strings.TrimSpace(txn.User.FirstName + " " + txn.User.LastName)
		}

		// Build event info
		eventInfo := models.UserTransactionEventInfo{}
		if txn.Event != nil {
			eventInfo.ID = txn.Event.ID
			eventInfo.Title = txn.Event.Title
			eventInfo.BannerImage = txn.Event.BannerImage
		}

		// Build tiers info (no tier details in transaction, just quantity)
		tiers := make([]models.UserTransactionTierInfo, 0)

		// Build response - AmountTotal is in cents, convert to decimal
		displayAmount := float64(txn.AmountTotal) / 100.0
		responses = append(responses, models.UserTransactionListingResponse{
			ID:              txn.ID,
			Event:           eventInfo,
			Tiers:           tiers,
			Price:           displayAmount,
			Status:          string(txn.Status),
			Date:            txn.CreatedAt,
			PaymentMethod:   string(txn.PaymentGateway),
			PaymentIntentID: txn.PaymentIntentID.String(),
			TransactionRef:  txn.ProviderChargeID,
			User:            userInfo,
		})
	}

	return responses, total, nil
}

// logAudit creates audit log entries for transaction operations
func (ts *TransactionService) logAudit(ctx context.Context, action, entityType string, entityID uuid.UUID, actorID *uuid.UUID, actorType string, eventID *uuid.UUID, changes map[string]interface{}) {
	LogPaymentAuditAsync(ts.db, action, entityType, entityID, actorID, actorType, eventID, changes)
}
