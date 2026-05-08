package services

import (
	"context"
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

// GetUserTransactions returns paginated list of user transactions with detailed information
func (ts *TransactionService) GetUserTransactions(userID uuid.UUID, page, limit int, filters models.UserTransactionFilters) ([]models.UserTransactionListingResponse, int64, error) {
	var transactions []models.Transaction
	var total int64

	// Base query for user's transactions (both regular user and guest purchases)
	query := ts.db.Model(&models.Transaction{}).
		Preload("Event").
		Preload("Tier").
		Preload("User").
		Preload("Tickets").
		Preload("Tickets.Tier").
		Where("actor_id = ?", userID)

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

	return responses, total, nil
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
