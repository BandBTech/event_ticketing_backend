package services

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"event-ticketing-backend/internal/models"
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
	LogPaymentAuditAsync(fs.db, action, entityType, entityID, actorID, actorType, eventID, changes)
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
