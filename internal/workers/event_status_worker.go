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

	// Schedule status updates every 5 minutes
	_, err := w.cronScheduler.AddFunc("*/5 * * * *", w.updateEventStatuses)
	if err != nil {
		log.Printf("[EventStatusWorker] Failed to schedule status updates: %v", err)
		return
	}

	// Schedule live status updates every 1 minute (more frequent for active events)
	_, err = w.cronScheduler.AddFunc("*/1 * * * *", w.updateLiveEventStatuses)
	if err != nil {
		log.Printf("[EventStatusWorker] Failed to schedule live status updates: %v", err)
		return
	}

	w.cronScheduler.Start()
	log.Println("[EventStatusWorker] Event status worker started successfully")
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

	// 1. Update approved events with tiers to "on_sale"
	if err := w.updateApprovedEventsToOnSale(ctx); err != nil {
		log.Printf("[EventStatusWorker] Error updating approved events to on_sale: %v", err)
	}

	// 2. Update events that have ended
	if err := w.updateEndedEvents(ctx); err != nil {
		log.Printf("[EventStatusWorker] Error updating ended events: %v", err)
	}

	log.Println("[EventStatusWorker] Event status updates completed")
}

// updateLiveEventStatuses handles more frequent status updates for active events
func (w *EventStatusWorker) updateLiveEventStatuses() {
	ctx := context.Background()

	// 1. Update events that should go live (start time reached)
	if err := w.updateEventsToLive(ctx); err != nil {
		log.Printf("[EventStatusWorker] Error updating events to live: %v", err)
	}
}

// updateApprovedEventsToOnSale changes approved events with tiers to "on_sale" status
func (w *EventStatusWorker) updateApprovedEventsToOnSale(ctx context.Context) error {
	// Find approved events that have tiers but are not yet on_sale
	var events []models.Event
	if err := w.db.Preload("Tiers").
		Where("status = ? AND (sales_status = ? OR sales_status IS NULL OR sales_status = '')", "approved", "active").
		Find(&events).Error; err != nil {
		return fmt.Errorf("failed to fetch approved events: %w", err)
	}

	updatedCount := 0
	for _, event := range events {
		// Check if event has tiers
		if len(event.Tiers) > 0 {
			// Check if any tier has available tickets
			hasAvailableTickets := false
			for _, tier := range event.Tiers {
				if tier.Quantity > tier.Sold {
					hasAvailableTickets = true
					break
				}
			}

			if hasAvailableTickets {
				// Update status to "on_sale"
				if err := w.db.Model(&event).Update("status", "on_sale").Error; err != nil {
					log.Printf("[EventStatusWorker] Failed to update event %s to on_sale: %v", event.ID, err)
					continue
				}

				// Log the status change
				if err := w.logStatusChange(event.ID, "approved", "on_sale", "automatic", "system", "Event automatically set to on_sale due to having available ticket tiers"); err != nil {
					log.Printf("[EventStatusWorker] Failed to log status change for event %s: %v", event.ID, err)
				}

				updatedCount++
				log.Printf("[EventStatusWorker] Updated event %s (%s) from approved to on_sale", event.ID, event.Title)
			}
		}
	}

	if updatedCount > 0 {
		log.Printf("[EventStatusWorker] Updated %d approved events to on_sale status", updatedCount)
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

	// Find live events that have ended
	var events []models.Event
	if err := w.db.Where("status = ? AND end_date <= ?", "live", now).Find(&events).Error; err != nil {
		return fmt.Errorf("failed to fetch ended events: %w", err)
	}

	updatedCount := 0
	for _, event := range events {
		// Change to "completed" status (we'll need to add this status to the validation)
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
		if err := w.logStatusChange(event.ID, "live", newStatus, "automatic", "system", "Event automatically completed as end time has passed"); err != nil {
			log.Printf("[EventStatusWorker] Failed to log status change for event %s: %v", event.ID, err)
		}

		updatedCount++
		log.Printf("[EventStatusWorker] Updated event %s (%s) from live to completed", event.ID, event.Title)
	}

	if updatedCount > 0 {
		log.Printf("[EventStatusWorker] Updated %d events to completed status", updatedCount)
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
