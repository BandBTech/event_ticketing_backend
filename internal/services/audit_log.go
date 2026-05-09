package services

import (
	"log"
	"time"

	"event-ticketing-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func LogPaymentAuditAsync(
	db *gorm.DB,
	action, entityType string,
	entityID uuid.UUID,
	actorID *uuid.UUID,
	actorType string,
	eventID *uuid.UUID,
	changes map[string]interface{},
) {
	if db == nil {
		return
	}

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

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[AUDIT_LOG] panic in async audit logging: %v", r)
			}
		}()
		_ = db.Create(audit).Error
	}()
}

func LogPaymentAuditTx(
	tx *gorm.DB,
	action, entityType string,
	entityID uuid.UUID,
	actorID *uuid.UUID,
	actorType string,
	eventID *uuid.UUID,
	changes map[string]interface{},
) error {
	if tx == nil {
		return nil
	}

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

	return tx.Create(audit).Error
}
