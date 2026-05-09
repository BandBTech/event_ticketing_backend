package services

import (
	"context"
	"strings"
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
		entityCtx := fs.resolveAuditEntityContext(log.EntityType, log.EntityID)

		minimalLogs[i] = models.MinimalAuditLog{
			ID:         log.ID,
			Action:     log.Action,
			EntityType: log.EntityType,
			EntityID:   log.EntityID,
			Timestamp:  log.Timestamp,
			CreatedAt:  log.CreatedAt,
		}

		minimalLogs[i].Actor = fs.resolveMinimalAuditActor(log, entityCtx.ActorID)
		minimalLogs[i].Event = fs.resolveMinimalAuditEvent(log, entityCtx.EventID)
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

type auditEntityContext struct {
	ActorID *uuid.UUID
	EventID *uuid.UUID
}

func (fs *FinancialService) resolveAuditEntityContext(entityType string, entityID uuid.UUID) auditEntityContext {
	switch entityType {
	case "ticket":
		var row struct {
			ActorID uuid.UUID
			EventID uuid.UUID
		}
		if err := fs.db.Model(&models.Ticket{}).
			Select("actor_id, event_id").
			Where("id = ?", entityID).
			Take(&row).Error; err == nil {
			return auditEntityContext{
				ActorID: &row.ActorID,
				EventID: &row.EventID,
			}
		}
	case "transaction":
		var row struct {
			ActorID uuid.UUID
			EventID uuid.UUID
		}
		if err := fs.db.Model(&models.Transaction{}).
			Select("actor_id, event_id").
			Where("id = ?", entityID).
			Take(&row).Error; err == nil {
			return auditEntityContext{
				ActorID: &row.ActorID,
				EventID: &row.EventID,
			}
		}
	case "refund":
		var row struct {
			InitiatedBy uuid.UUID
			EventID     uuid.UUID
		}
		if err := fs.db.Model(&models.Refund{}).
			Select("initiated_by, event_id").
			Where("id = ?", entityID).
			Take(&row).Error; err == nil {
			return auditEntityContext{
				ActorID: &row.InitiatedBy,
				EventID: &row.EventID,
			}
		}
	}

	return auditEntityContext{}
}

func (fs *FinancialService) resolveMinimalAuditActor(log models.PaymentAuditLog, fallbackActorID *uuid.UUID) *models.MinimalUser {
	if log.Actor != nil {
		return &models.MinimalUser{
			ID:    log.Actor.ID,
			Name:  strings.TrimSpace(log.Actor.FirstName + " " + log.Actor.LastName),
			Email: log.Actor.Email,
		}
	}

	actorID := log.ActorID
	if actorID == nil {
		actorID = fallbackActorID
	}
	if actorID == nil {
		return nil
	}

	var user models.User
	if err := fs.db.Model(&models.User{}).Where("id = ?", *actorID).Take(&user).Error; err == nil {
		return &models.MinimalUser{
			ID:    user.ID,
			Name:  strings.TrimSpace(user.FirstName + " " + user.LastName),
			Email: user.Email,
		}
	}

	var guest models.GuestUser
	if err := fs.db.Model(&models.GuestUser{}).Where("id = ?", *actorID).Take(&guest).Error; err == nil {
		return &models.MinimalUser{
			ID:    guest.ID,
			Name:  strings.TrimSpace(guest.FirstName + " " + guest.LastName),
			Email: guest.Email,
		}
	}

	return nil
}

func (fs *FinancialService) resolveMinimalAuditEvent(log models.PaymentAuditLog, fallbackEventID *uuid.UUID) *models.MinimalEvent {
	if log.Event != nil {
		return &models.MinimalEvent{
			ID:          log.Event.ID,
			Title:       log.Event.Title,
			BannerImage: log.Event.BannerImage,
			OrganizerID: log.Event.OrganizerID,
		}
	}

	eventID := log.EventID
	if eventID == nil {
		eventID = fallbackEventID
	}
	if eventID == nil {
		return nil
	}

	var event models.Event
	if err := fs.db.Model(&models.Event{}).Where("id = ?", *eventID).Take(&event).Error; err != nil {
		return nil
	}

	return &models.MinimalEvent{
		ID:          event.ID,
		Title:       event.Title,
		BannerImage: event.BannerImage,
		OrganizerID: event.OrganizerID,
	}
}
