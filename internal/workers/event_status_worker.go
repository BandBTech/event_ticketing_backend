package workers

import (
	"context"
	"fmt"
	"log"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// EventStatusWorker automatically updates event statuses based on conditions
type EventStatusWorker struct {
	db            *gorm.DB
	eventService  *services.EventService
	cronScheduler *cron.Cron
	cfg           *config.Config
	running       bool
}

// NewEventStatusWorker creates a new event status worker
func NewEventStatusWorker(cfg *config.Config, eventService *services.EventService) *EventStatusWorker {
	return &EventStatusWorker{
		db:            database.GetDB(),
		eventService:  eventService,
		cronScheduler: cron.New(),
		cfg:           cfg,
		running:       false,
	}
}

// Start starts the event status worker with scheduled tasks
func (w *EventStatusWorker) Start() {
	if w.running {
		log.Println("[EventStatusWorker] Worker is already running")
		return
	}

	w.running = true
	log.Println("[EventStatusWorker] Starting event status worker...")

	// Schedule status updates daily at 1 AM (production-friendly)
	// For development/testing, you can change to "*/5 * * * *" for every 5 minutes
	// Cron format: "0 1 * * *" = At 01:00 every day
	_, err := w.cronScheduler.AddFunc("*/5 * * * *", w.updateEventStatuses)
	if err != nil {
		log.Printf("[EventStatusWorker] Failed to schedule status updates: %v", err)
		return
	}

	// Schedule live status updates every 15 minutes (less frequent than every minute)
	// For development/testing, you can change to "*/1 * * * *" for every minute
	// Cron format: "*/15 * * * *" = Every 15 minutes
	_, err = w.cronScheduler.AddFunc("*/1 * * * *", w.updateLiveEventStatuses)
	if err != nil {
		log.Printf("[EventStatusWorker] Failed to schedule live status updates: %v", err)
		return
	}

	w.cronScheduler.Start()
	log.Println("[EventStatusWorker] Event status worker started successfully - General updates: Daily at 1 AM, Live updates: Every 15 minutes")
}

// Stop stops the event status worker
func (w *EventStatusWorker) Stop() {
	if !w.running {
		return
	}

	log.Println("[EventStatusWorker] Stopping event status worker...")
	w.cronScheduler.Stop()
	w.running = false
	log.Println("[EventStatusWorker] Event status worker stopped")
}

// updateEventStatuses performs periodic status updates based on event conditions
func (w *EventStatusWorker) updateEventStatuses() {
	ctx := context.Background()
	log.Println("[EventStatusWorker] Running scheduled event status updates...")

	// 1. Update scheduled events based on tier sales periods
	if err := w.updateScheduledToSalesStatus(ctx); err != nil {
		log.Printf("[EventStatusWorker] Error updating scheduled events to sales status: %v", err)
	}

	// 2. Update pending events with expired sales dates to "cancelled"
	if err := w.updateExpiredPendingEvents(ctx); err != nil {
		log.Printf("[EventStatusWorker] Error updating expired pending events: %v", err)
	}

	// 3. Update events that have ended
	if err := w.updateEndedEvents(ctx); err != nil {
		log.Printf("[EventStatusWorker] Error updating ended events: %v", err)
	}

	log.Println("[EventStatusWorker] Event status updates completed")
}

// updateLiveEventStatuses handles more frequent status updates for active events
func (w *EventStatusWorker) updateLiveEventStatuses() {
	ctx := context.Background()

	// 1. Update tier-based sales statuses: on_sale ↔ sales_end based on tier sales periods
	if err := w.updateTierBasedSalesStatus(ctx); err != nil {
		log.Printf("[EventStatusWorker] Error updating tier-based sales status: %v", err)
	}
}

// updateScheduledToSalesStatus transitions scheduled events to on_sale or sales_end based on tier sales periods
func (w *EventStatusWorker) updateScheduledToSalesStatus(ctx context.Context) error {
	now := time.Now().UTC()

	// Find scheduled events
	var events []models.Event
	if err := w.db.Preload("Tiers").
		Where("status = ? AND is_cancelled = false", "scheduled").
		Find(&events).Error; err != nil {
		return fmt.Errorf("failed to fetch scheduled events: %w", err)
	}

	updatedCount := 0
	for _, event := range events {
		// Check if event has tiers
		if len(event.Tiers) == 0 {
			continue
		}

		// Determine event status based on tier sales periods
		anyTierActive := false
		allTiersEnded := true

		for _, tier := range event.Tiers {
			if tier.SalesStart != nil && tier.SalesEnd != nil {
				// Check if any tier is currently active (now between sales_start and sales_end)
				if now.After(*tier.SalesStart) && now.Before(*tier.SalesEnd) {
					anyTierActive = true
					allTiersEnded = false
					break
				}

				// Check if we're after this tier's end
				if now.Before(*tier.SalesEnd) {
					allTiersEnded = false
				}
			}
		}

		// Determine target status:
		// - If any tier is active: on_sale
		// - If all tiers have ended: sales_end
		// - Otherwise (before first tier starts): stay scheduled (don't auto-transition)
		targetStatus := event.Status
		if anyTierActive {
			targetStatus = "on_sale"
		} else if allTiersEnded {
			targetStatus = "sales_end"
		}
		// If before all tiers start, keep current status (scheduled)

		// Only update if status changed
		if event.Status != targetStatus {
			if err := w.db.Model(&event).Update("status", targetStatus).Error; err != nil {
				log.Printf("[EventStatusWorker] Failed to update event %s from scheduled to %s: %v", event.ID, targetStatus, err)
				continue
			}

			// Log the status change
			reason := fmt.Sprintf("Event automatically transitioned from scheduled to %s based on tier sales periods", targetStatus)
			if err := w.logStatusChange(event.ID, "scheduled", targetStatus, "automatic", "system", reason); err != nil {
				log.Printf("[EventStatusWorker] Failed to log status change for event %s: %v", event.ID, err)
			}

			updatedCount++
			log.Printf("[EventStatusWorker] Updated event %s (%s) from scheduled to %s", event.ID, event.Title, targetStatus)
		}
	}

	if updatedCount > 0 {
		log.Printf("[EventStatusWorker] Updated %d scheduled events to sales status", updatedCount)
	}

	return nil
}

// updateExpiredPendingEvents changes pending events to "cancelled" when all tiers' sales dates have expired
func (w *EventStatusWorker) updateExpiredPendingEvents(ctx context.Context) error {
	now := time.Now().UTC()

	// Find pending events with tiers
	var events []models.Event
	if err := w.db.Preload("Tiers").
		Where("status = ? AND is_cancelled = false", "pending").
		Find(&events).Error; err != nil {
		return fmt.Errorf("failed to fetch pending events: %w", err)
	}

	updatedCount := 0
	for _, event := range events {
		// Check if all tiers have expired sales_end dates
		allTiersExpired := true
		hasAnyTierWithSalesEnd := false

		for _, tier := range event.Tiers {
			if tier.SalesEnd != nil {
				hasAnyTierWithSalesEnd = true
				// If any tier's sales_end is still in the future, event is not expired
				if tier.SalesEnd.After(now) {
					allTiersExpired = false
					break
				}
			}
		}

		// If event has no tiers with sales_end dates, skip it
		if !hasAnyTierWithSalesEnd {
			continue
		}

		// If all tiers have expired sales dates, cancel the event
		if allTiersExpired {
			if err := w.db.Model(&event).Updates(map[string]interface{}{
				"status":        "cancelled",
				"is_cancelled":  true,
				"cancelled_at":  now,
				"cancel_reason": "Automatically cancelled due to expired sales period for all tiers",
			}).Error; err != nil {
				log.Printf("[EventStatusWorker] Failed to cancel expired pending event %s: %v", event.ID, err)
				continue
			}

			// Log the status change
			if err := w.logStatusChange(event.ID, "pending", "cancelled", "automatic", "system", "Event automatically cancelled as all tier sales periods have expired"); err != nil {
				log.Printf("[EventStatusWorker] Failed to log status change for event %s: %v", event.ID, err)
			}

			updatedCount++
			log.Printf("[EventStatusWorker] Cancelled expired pending event %s (%s)", event.ID, event.Title)
		}
	}

	if updatedCount > 0 {
		log.Printf("[EventStatusWorker] Cancelled %d expired pending events", updatedCount)
	}

	return nil
}

// updateEventsToLive changes on_sale events to "live" when start time is reached
func (w *EventStatusWorker) updateEventsToLive(ctx context.Context) error {
	now := time.Now().UTC()

	// Find events that should be live (on_sale events where start time has been reached)
	var events []models.Event
	if err := w.db.Where("status = ? AND start_date <= ? AND end_date > ?", "on_sale", now, now).Find(&events).Error; err != nil {
		return fmt.Errorf("failed to fetch events for live status: %w", err)
	}

	updatedCount := 0
	for _, event := range events {
		if err := w.db.Model(&event).Update("status", "live").Error; err != nil {
			log.Printf("[EventStatusWorker] Failed to update event %s to live: %v", event.ID, err)
			continue
		}

		// Log the status change
		if err := w.logStatusChange(event.ID, "on_sale", "live", "automatic", "system", "Event automatically set to live as start time has been reached"); err != nil {
			log.Printf("[EventStatusWorker] Failed to log status change for event %s: %v", event.ID, err)
		}

		updatedCount++
		log.Printf("[EventStatusWorker] Updated event %s (%s) from on_sale to live", event.ID, event.Title)
	}

	if updatedCount > 0 {
		log.Printf("[EventStatusWorker] Updated %d events to live status", updatedCount)
	}

	return nil
}

// updateEndedEvents handles events that have ended
func (w *EventStatusWorker) updateEndedEvents(ctx context.Context) error {
	now := time.Now().UTC()

	// Find events that have ended (approved, on_sale, or live status)
	var events []models.Event
	if err := w.db.Where("status IN (?) AND end_date <= ? AND is_cancelled = false",
		[]string{"approved", "on_sale", "live"}, now).Find(&events).Error; err != nil {
		return fmt.Errorf("failed to fetch ended events: %w", err)
	}

	updatedCount := 0
	for _, event := range events {
		// Determine the old status for logging
		oldStatus := event.Status
		newStatus := "completed"

		if err := w.db.Model(&event).Updates(map[string]interface{}{
			"status":       "completed",
			"sales_status": "stopped",
			"is_cancelled": false,
		}).Error; err != nil {
			log.Printf("[EventStatusWorker] Failed to update ended event %s: %v", event.ID, err)
			continue
		}

		// Log the status change
		if err := w.logStatusChange(event.ID, oldStatus, newStatus, "automatic", "system", "Event automatically completed as end time has passed"); err != nil {
			log.Printf("[EventStatusWorker] Failed to log status change for event %s: %v", event.ID, err)
		}

		updatedCount++
		log.Printf("[EventStatusWorker] Updated event %s (%s) from %s to completed", event.ID, event.Title, oldStatus)
	}

	if updatedCount > 0 {
		log.Printf("[EventStatusWorker] Updated %d events to completed status", updatedCount)
	}

	return nil
}

// updateTierBasedSalesStatus handles dynamic transitions between on_sale and sales_end based on tier sales periods
// If ANY tier is currently active (now between tier.sales_start and tier.sales_end): status = on_sale
// If NO tier is currently active:
//   - If we're BEFORE all tiers start: keep status as-is (don't force to sales_end)
//   - If we're AFTER all tiers end: status = sales_end
func (w *EventStatusWorker) updateTierBasedSalesStatus(ctx context.Context) error {
	now := time.Now().UTC()

	// Find events with status scheduled, on_sale, or sales_end (events that can have dynamic status changes)
	var events []models.Event
	if err := w.db.Preload("Tiers").
		Where("status IN ? AND is_cancelled = false", []string{"scheduled", "on_sale", "sales_end"}).
		Find(&events).Error; err != nil {
		return fmt.Errorf("failed to fetch events for tier-based status updates: %w", err)
	}

	updatedCount := 0
	for _, event := range events {
		// Skip events with no tiers - can't determine tier-based status transitions
		if len(event.Tiers) == 0 {
			continue
		}

		// Determine current tier sales status
		anyTierActive := false
		allTiersEnded := true

		for _, tier := range event.Tiers {
			if tier.SalesStart != nil && tier.SalesEnd != nil {
				// Check if any tier is currently active (now between sales_start and sales_end)
				if now.After(*tier.SalesStart) && now.Before(*tier.SalesEnd) {
					anyTierActive = true
					allTiersEnded = false
				}

				// Check if any tier has NOT ended yet (sales_end >= now)
				if now.Before(*tier.SalesEnd) || now.Equal(*tier.SalesEnd) {
					allTiersEnded = false
				}
			}
		}

		// Determine target status:
		// - If any tier is active (now between sales_start and sales_end): on_sale
		// - If ALL tiers have ended (all sales_end < now): sales_end
		// - Otherwise: keep current status (before all tiers start or mixed state)
		targetStatus := event.Status
		if anyTierActive {
			targetStatus = "on_sale"
		} else if allTiersEnded {
			targetStatus = "sales_end"
		}

		// Only update if status changed
		if event.Status != targetStatus {
			oldStatus := event.Status
			if err := w.db.Model(&event).Update("status", targetStatus).Error; err != nil {
				log.Printf("[EventStatusWorker] Failed to update event %s to %s: %v", event.ID, targetStatus, err)
				continue
			}

			// Log the status change
			reason := fmt.Sprintf("Event automatically set to %s based on tier sales periods", targetStatus)
			if err := w.logStatusChange(event.ID, oldStatus, targetStatus, "automatic", "system", reason); err != nil {
				log.Printf("[EventStatusWorker] Failed to log status change for event %s: %v", event.ID, err)
			}

			updatedCount++
			log.Printf("[EventStatusWorker] Updated event %s (%s) from %s to %s (tier-based)", event.ID, event.Title, oldStatus, targetStatus)
		}
	}

	if updatedCount > 0 {
		log.Printf("[EventStatusWorker] Updated %d events based on tier sales periods", updatedCount)
	}

	return nil
}

// logStatusChange logs a status change to the event status history
func (w *EventStatusWorker) logStatusChange(eventID uuid.UUID, oldStatus, newStatus, changeType, changedBy, remarks string) error {
	// For system changes, we use a system user ID or nil
	var changedByUUID uuid.UUID
	if changedBy == "system" {
		// Use a zero UUID for system changes
		changedByUUID = uuid.Nil
	} else {
		var err error
		changedByUUID, err = uuid.Parse(changedBy)
		if err != nil {
			return fmt.Errorf("invalid changed_by UUID: %w", err)
		}
	}

	statusHistory := models.EventStatusHistory{
		EventID:    eventID,
		OldStatus:  oldStatus,
		NewStatus:  newStatus,
		StatusType: changeType, // 'automatic' for system changes
		ChangedBy:  changedByUUID,
		Remark:     remarks,
		CreatedAt:  time.Now(),
	}

	return w.db.Create(&statusHistory).Error
}
