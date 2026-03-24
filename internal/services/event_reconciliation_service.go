package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"event-ticketing-backend/internal/models"

	"gorm.io/gorm"
)

// EventReconciliationService handles Stripe event reconciliation
type EventReconciliationService struct {
	db *gorm.DB
}

// NewEventReconciliationService creates a new event reconciliation service
func NewEventReconciliationService(db *gorm.DB) *EventReconciliationService {
	return &EventReconciliationService{db: db}
}

// RecordStripeEvent records a Stripe event for reconciliation
func (s *EventReconciliationService) RecordStripeEvent(ctx context.Context, stripeEventID, eventType string, paymentIntentID string, rawEvent map[string]interface{}) (*models.StripeEventReconciliation, error) {
	reconciliation := &models.StripeEventReconciliation{
		StripeEventID:   stripeEventID,
		EventType:       eventType,
		PaymentIntentID: paymentIntentID,
		RawEvent:        rawEvent,
		Status:          models.ReconciliationStatusPending,
	}

	if err := s.db.Create(reconciliation).Error; err != nil {
		return nil, fmt.Errorf("failed to record Stripe event: %w", err)
	}

	log.Printf("✓ Recorded Stripe event for reconciliation: %s (%s)", stripeEventID, eventType)
	return reconciliation, nil
}

// MarkEventAsProcessed marks an event as processed
func (s *EventReconciliationService) MarkEventAsProcessed(ctx context.Context, stripeEventID string) error {
	now := time.Now()

	result := s.db.Model(&models.StripeEventReconciliation{}).
		Where("stripe_event_id = ?", stripeEventID).
		Updates(map[string]interface{}{
			"status":       models.ReconciliationStatusProcessed,
			"processed_at": &now,
			"updated_at":   now,
		})

	if result.Error != nil {
		return fmt.Errorf("failed to mark event as processed: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("event not found: %s", stripeEventID)
	}

	return nil
}

// MarkEventAsReconciled marks an event as successfully reconciled
func (s *EventReconciliationService) MarkEventAsReconciled(ctx context.Context, stripeEventID string) error {
	now := time.Now()

	result := s.db.Model(&models.StripeEventReconciliation{}).
		Where("stripe_event_id = ?", stripeEventID).
		Updates(map[string]interface{}{
			"status":        models.ReconciliationStatusReconciled,
			"reconciled_at": &now,
			"updated_at":    now,
		})

	if result.Error != nil {
		return fmt.Errorf("failed to mark event as reconciled: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("event not found: %s", stripeEventID)
	}

	return nil
}

// MarkEventAsFailed marks an event as failed with error message
func (s *EventReconciliationService) MarkEventAsFailed(ctx context.Context, stripeEventID, errorMessage string) error {
	result := s.db.Model(&models.StripeEventReconciliation{}).
		Where("stripe_event_id = ?", stripeEventID).
		Updates(map[string]interface{}{
			"status":        models.ReconciliationStatusFailed,
			"error_message": errorMessage,
			"updated_at":    time.Now(),
		})

	if result.Error != nil {
		return fmt.Errorf("failed to mark event as failed: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("event not found: %s", stripeEventID)
	}

	return nil
}

// ReconcileEvents performs reconciliation by checking for missed events
// This should be called periodically (e.g., every 5 minutes)
func (s *EventReconciliationService) ReconcileEvents(ctx context.Context, stripeGateway interface{}) error {
	// Find events that are pending or failed and older than 5 minutes
	cutoffTime := time.Now().Add(-5 * time.Minute)

	var eventsToReconcile []models.StripeEventReconciliation
	if err := s.db.Where("(status = ? OR status = ?) AND created_at < ?",
		models.ReconciliationStatusPending,
		models.ReconciliationStatusFailed,
		cutoffTime).
		Limit(100). // Process in batches
		Find(&eventsToReconcile).Error; err != nil {
		return fmt.Errorf("failed to find events for reconciliation: %w", err)
	}

	if len(eventsToReconcile) == 0 {
		return nil // Nothing to reconcile
	}

	reconciled := 0
	failed := 0

	for _, event := range eventsToReconcile {
		if err := s.reconcileSingleEvent(ctx, stripeGateway, &event); err != nil {
			failed++
			log.Printf("✗ Failed to reconcile event %s: %v", event.StripeEventID, err)

			// Update retry count and potentially mark as permanently failed
			event.RetryCount++
			if event.RetryCount >= event.MaxRetries {
				if markErr := s.MarkEventAsFailed(ctx, event.StripeEventID, err.Error()); markErr != nil {
					log.Printf("✗ Failed to mark event as failed: %v", markErr)
				}
			} else {
				// Save retry count
				s.db.Model(&event).Update("retry_count", event.RetryCount)
			}
		} else {
			reconciled++
			if err := s.MarkEventAsReconciled(ctx, event.StripeEventID); err != nil {
				log.Printf("✗ Failed to mark event as reconciled: %v", err)
			}
		}
	}

	log.Printf("✓ Reconciled %d events (%d successful, %d failed)", len(eventsToReconcile), reconciled, failed)
	return nil
}

// reconcileSingleEvent attempts to reconcile a single event
func (s *EventReconciliationService) reconcileSingleEvent(ctx context.Context, stripeGateway interface{}, event *models.StripeEventReconciliation) error {
	// Check if the payment intent exists and is in the expected state
	if event.PaymentIntentID == "" {
		return fmt.Errorf("no payment intent ID for event")
	}

	var paymentIntent models.PaymentIntent
	if err := s.db.Where("idempotency_key = ?", event.PaymentIntentID).First(&paymentIntent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("payment intent not found: %s", event.PaymentIntentID)
		}
		return fmt.Errorf("failed to lookup payment intent: %w", err)
	}

	// Check if this event type should have resulted in a state change
	switch event.EventType {
	case "payment_intent.succeeded":
		if paymentIntent.Status != "succeeded" && paymentIntent.Status != "completed" {
			// Event was missed - we should process it
			return fmt.Errorf("payment succeeded but status is %s - event was missed", paymentIntent.Status)
		}
	case "payment_intent.payment_failed":
		if paymentIntent.Status != "failed" {
			// Event was missed - we should process it
			return fmt.Errorf("payment failed but status is %s - event was missed", paymentIntent.Status)
		}
	case "payment_intent.canceled":
		if paymentIntent.Status != "cancelled" {
			// Event was missed - we should process it
			return fmt.Errorf("payment canceled but status is %s - event was missed", paymentIntent.Status)
		}
	}

	// If we get here, the event was already properly processed
	return nil
}

// GetReconciliationStats returns statistics about event reconciliation
func (s *EventReconciliationService) GetReconciliationStats(ctx context.Context) (map[string]int64, error) {
	var stats struct {
		Pending    int64
		Processed  int64
		Reconciled int64
		Failed     int64
	}

	err := s.db.Model(&models.StripeEventReconciliation{}).
		Select("COUNT(CASE WHEN status = ? THEN 1 END) as pending",
			"COUNT(CASE WHEN status = ? THEN 1 END) as processed",
			"COUNT(CASE WHEN status = ? THEN 1 END) as reconciled",
			"COUNT(CASE WHEN status = ? THEN 1 END) as failed").
		Scan(&stats).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get reconciliation stats: %w", err)
	}

	return map[string]int64{
		"pending":    stats.Pending,
		"processed":  stats.Processed,
		"reconciled": stats.Reconciled,
		"failed":     stats.Failed,
	}, nil
}
