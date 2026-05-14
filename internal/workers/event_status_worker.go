package workers

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"

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

	// Schedule live status updates every minute (frequent for development/testing)
	// For production, you can change to "*/15 * * * *" for every 15 minutes
	// Cron format: "*/1 * * * *" = Every minute
	_, err = w.cronScheduler.AddFunc("*/1 * * * *", w.updateLiveEventStatuses)
	if err != nil {
		log.Printf("[EventStatusWorker] Failed to schedule live status updates: %v", err)
		return
	}

	w.cronScheduler.Start()
	log.Println("[EventStatusWorker] Event status worker started successfully - General updates: Every 5 minutes, Live updates: Every minute")
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

func (w *EventStatusWorker) applyAutomaticTransition(event models.Event, targetStatus, targetSalesStatus, reason string) (bool, error) {
	statusChanged := targetStatus != "" && event.Status != targetStatus
	salesStatusChanged := targetSalesStatus != "" && event.SalesStatus != targetSalesStatus
	if !statusChanged && !salesStatusChanged {
		return false, nil
	}

	if err := w.eventService.UpdateEventStatusAndSalesStatusWithLogging(
		event.ID,
		targetStatus,
		targetSalesStatus,
		models.EventStatusTypeAutomatic.String(),
		"system",
		reason,
	); err != nil {
		return false, err
	}

	return true, nil
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

	// 4. Expire tickets for completed events
	if err := w.expireTicketsForCompletedEvents(ctx); err != nil {
		log.Printf("[EventStatusWorker] Error expiring tickets for completed events: %v", err)
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

	// 2. Update events to live status when start_date is reached
	if err := w.updateEventsToLive(ctx); err != nil {
		log.Printf("[EventStatusWorker] Error updating events to live status: %v", err)
	}
}

// updateScheduledToSalesStatus transitions scheduled events to on_sale or sales_end based on tier sales periods
func (w *EventStatusWorker) updateScheduledToSalesStatus(ctx context.Context) error {
	now := time.Now().UTC()

	// Find scheduled events (exclude hold status and final statuses - manually paused and completed events should not be auto-updated)
	var events []models.Event
	if err := w.db.Preload("Tiers").
		Where("status = ? AND is_cancelled = false AND status NOT IN (?) AND (sales_status IS NULL OR sales_status NOT IN (?))",
			models.EventStatusScheduled.String(),
			[]string{models.EventStatusCompleted.String(), models.EventStatusCancelled.String(), models.EventStatusRejected.String(), models.EventStatusHold.String()},
			[]string{models.EventSalesStatusPaused.String(), models.EventSalesStatusStopped.String()},
		).
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
		var activeTiers []string
		var endedTiers []string

		for _, tier := range event.Tiers {
			if tier.SalesStart != nil && tier.SalesEnd != nil {
				// Check if any tier is currently active (now between sales_start and sales_end)
				if now.After(*tier.SalesStart) && now.Before(*tier.SalesEnd) {
					anyTierActive = true
					allTiersEnded = false
					activeTiers = append(activeTiers, tier.TierName)
				}

				// Check if we're after this tier's end
				if now.Before(*tier.SalesEnd) {
					allTiersEnded = false
				} else {
					endedTiers = append(endedTiers, tier.TierName)
				}
			}
		}

		// Determine target status:
		// - If any tier is active: on_sale
		// - If all tiers have ended: sales_end
		// - Otherwise (before first tier starts): stay scheduled (don't auto-transition)
		targetStatus := event.Status
		var reason string
		if anyTierActive {
			targetStatus = models.EventStatusOnSale.String()
			reason = fmt.Sprintf("Event automatically transitioned to on_sale - active tier(s): %s", strings.Join(activeTiers, ", "))
		} else if allTiersEnded {
			targetStatus = models.EventStatusSalesEnd.String()
			reason = fmt.Sprintf("Event automatically transitioned to sales_end - all tier(s) ended: %s", strings.Join(endedTiers, ", "))
		}
		// If before all tiers start, keep current status (scheduled)

		if updated, err := w.applyAutomaticTransition(event, targetStatus, "", reason); err != nil {
			log.Printf("[EventStatusWorker] Failed to update event %s from scheduled to %s: %v", event.ID, targetStatus, err)
			continue
		} else if updated {
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
		Where("status = ? AND is_cancelled = false", models.EventStatusPending.String()).
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
			// Use central function to cancel event with logging
			err := w.eventService.CancelEventWithLogging(
				event.ID,
				"Automatically cancelled due to expired sales period for all tiers",
				models.EventStatusTypeAutomatic.String(),
				"system",
				"Event automatically cancelled as all tier sales periods have expired",
			)

			if err != nil {
				log.Printf("[EventStatusWorker] Failed to cancel expired pending event %s: %v", event.ID, err)
				continue
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

// updateEventsToLive changes events to "live" when start time is reached
// Includes events in any active status (scheduled, on_sale, sales_end, sales_upcoming, approved, hold)
// Event start has priority over tier windows; when start is reached, event becomes live and ticket sales stop.
func (w *EventStatusWorker) updateEventsToLive(ctx context.Context) error {
	now := time.Now().UTC()

	// Find events that should be live (any active status where start time has been reached)
	// Exclude events where sales have been manually paused OR final statuses
	var events []models.Event
	if err := w.db.Where("status IN (?) AND start_date <= ? AND end_date > ? AND status NOT IN (?)",
		[]string{
			models.EventStatusScheduled.String(), models.EventStatusApproved.String(), models.EventStatusOnSale.String(),
			models.EventStatusSalesEnd.String(), models.EventStatusSalesUpcoming.String(), models.EventStatusHold.String(),
		},
		now, now,
		[]string{models.EventStatusCompleted.String(), models.EventStatusCancelled.String(), models.EventStatusRejected.String()},
	).Find(&events).Error; err != nil {
		return fmt.Errorf("failed to fetch events for live status: %w", err)
	}

	updatedCount := 0
	for _, event := range events {
		oldStatus := event.Status

		if _, err := w.applyAutomaticTransition(
			event,
			models.EventStatusLive.String(),
			models.EventSalesStatusStopped.String(),
			"Event automatically set to live as start time has been reached; ticket sales closed at event start",
		); err != nil {
			log.Printf("[EventStatusWorker] Failed to update event %s to live: %v", event.ID, err)
			continue
		}

		updatedCount++
		log.Printf("[EventStatusWorker] Updated event %s (%s) from %s to live (start date reached)", event.ID, event.Title, oldStatus)
	}

	if updatedCount > 0 {
		log.Printf("[EventStatusWorker] Updated %d events to live status (start date reached)", updatedCount)
	}

	return nil
}

// updateEndedEvents handles events that have ended
func (w *EventStatusWorker) updateEndedEvents(ctx context.Context) error {
	now := time.Now().UTC()

	// Find events that have ended (any active status where end_date has passed)
	// Include all active statuses that should transition to completed when event ends
	// Exclude only final statuses; ended events must complete regardless of sales_status.
	var events []models.Event
	if err := w.db.Where("status IN (?) AND end_date <= ? AND is_cancelled = false AND status NOT IN (?)",
		[]string{
			models.EventStatusScheduled.String(),
			models.EventStatusApproved.String(),
			models.EventStatusOnSale.String(),
			models.EventStatusSalesEnd.String(),
			models.EventStatusSalesUpcoming.String(),
			models.EventStatusLive.String(),
			models.EventStatusHold.String(),
		},
		now,
		[]string{models.EventStatusCompleted.String(), models.EventStatusCancelled.String(), models.EventStatusRejected.String()},
	).Find(&events).Error; err != nil {
		return fmt.Errorf("failed to fetch ended events: %w", err)
	}

	updatedCount := 0
	for _, event := range events {
		oldStatus := event.Status

		if _, err := w.applyAutomaticTransition(
			event,
			models.EventStatusCompleted.String(),
			models.EventSalesStatusStopped.String(),
			"Event automatically completed as end time has passed",
		); err != nil {
			log.Printf("[EventStatusWorker] Failed to update ended event %s: %v", event.ID, err)
			continue
		}

		updatedCount++
		log.Printf("[EventStatusWorker] Updated event %s (%s) from %s to completed", event.ID, event.Title, oldStatus)
	}

	if updatedCount > 0 {
		log.Printf("[EventStatusWorker] Updated %d events to completed status", updatedCount)
	}

	return nil
}

// expireTicketsForCompletedEvents automatically expires tickets for events that have completed
func (w *EventStatusWorker) expireTicketsForCompletedEvents(ctx context.Context) error {
	// Find all completed events
	var events []models.Event
	if err := w.db.Where("status = ? AND is_cancelled = false", models.EventStatusCompleted.String()).Find(&events).Error; err != nil {
		return fmt.Errorf("failed to fetch completed events: %w", err)
	}

	totalExpiredTickets := 0
	for _, event := range events {
		// Update all active tickets for this event to expired status
		// Only expire tickets that are still active (not used, cancelled, refunded, or already expired)
		result := w.db.Model(&models.Ticket{}).
			Where("event_id = ? AND status = ?", event.ID, "active").
			Updates(map[string]interface{}{
				"status":     "expired",
				"updated_at": time.Now().UTC(),
			})

		if result.Error != nil {
			log.Printf("[EventStatusWorker] Failed to expire tickets for completed event %s: %v", event.ID, result.Error)
			continue
		}

		if result.RowsAffected > 0 {
			log.Printf("[EventStatusWorker] Expired %d tickets for completed event %s (%s)", result.RowsAffected, event.ID, event.Title)
			totalExpiredTickets += int(result.RowsAffected)
		}
	}

	if totalExpiredTickets > 0 {
		log.Printf("[EventStatusWorker] Total tickets expired for completed events: %d", totalExpiredTickets)
	}

	return nil
}

// updateTierBasedSalesStatus handles dynamic transitions between on_sale, sales_end, and sales_upcoming based on tier sales periods
// If ANY tier is currently active (now between tier.sales_start and tier.sales_end): status = on_sale (even if currently on hold)
// If NO tier is currently active:
//   - If we're BEFORE all tiers start: keep status as-is (don't force to sales_end)
//   - If we're AFTER all tiers end: status = sales_end (except for hold status)
//   - If we're BETWEEN sale periods (some tiers ended, some not started yet): status = sales_upcoming (except for hold status)
//
// Events with status "hold" will transition to "on_sale" when any tier becomes active, but stay "hold" otherwise
// Events with sales_status = "paused" or "stopped" are excluded from automatic status changes
func (w *EventStatusWorker) updateTierBasedSalesStatus(ctx context.Context) error {
	now := time.Now().UTC()

	// Find events with status on_sale, sales_end, sales_upcoming, live, hold (include hold for multi-tier transitions)
	// Exclude events where sales were manually paused or stopped
	var events []models.Event
	if err := w.db.Preload("Tiers").
		Where("status IN (?) AND is_cancelled = false AND status NOT IN (?) AND (sales_status IS NULL OR sales_status NOT IN (?))",
			[]string{models.EventStatusOnSale.String(), models.EventStatusSalesEnd.String(), models.EventStatusSalesUpcoming.String(), models.EventStatusHold.String(), models.EventStatusLive.String()},
			[]string{models.EventStatusCompleted.String(), models.EventStatusCancelled.String(), models.EventStatusRejected.String()},
			[]string{models.EventSalesStatusPaused.String(), models.EventSalesStatusStopped.String()},
		).
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
		allTiersNotStarted := true
		hasFutureTiers := false
		var earliestSalesStart *time.Time
		var latestSalesEnd *time.Time
		var activeTiers []string
		var endedTiers []string
		var futureTiers []string
		var notStartedTiers []string

		for _, tier := range event.Tiers {
			if tier.SalesStart != nil && tier.SalesEnd != nil {
				// Track if we've found any tier that has started
				if now.After(*tier.SalesStart) {
					allTiersNotStarted = false
				}

				// Check if any tier is currently active (now between sales_start and sales_end)
				if now.After(*tier.SalesStart) && now.Before(*tier.SalesEnd) {
					anyTierActive = true
					allTiersEnded = false
					allTiersNotStarted = false
					activeTiers = append(activeTiers, tier.TierName)
				}

				// Check if any tier has NOT ended yet (sales_end >= now)
				if now.Before(*tier.SalesEnd) || now.Equal(*tier.SalesEnd) {
					allTiersEnded = false
				} else {
					endedTiers = append(endedTiers, tier.TierName)
				}

				// Check if any tier starts in the future (for sales_upcoming logic)
				if now.Before(*tier.SalesStart) {
					hasFutureTiers = true
					futureTiers = append(futureTiers, tier.TierName)
				}

				// Track not started tiers
				if now.Before(*tier.SalesStart) {
					notStartedTiers = append(notStartedTiers, tier.TierName)
				}

				// Track earliest sales start for logging
				if earliestSalesStart == nil || tier.SalesStart.Before(*earliestSalesStart) {
					earliestSalesStart = tier.SalesStart
				}

				// Track latest sales end for logging
				if latestSalesEnd == nil || tier.SalesEnd.After(*latestSalesEnd) {
					latestSalesEnd = tier.SalesEnd
				}
			}
		}

		// Determine target status and sales_status:
		// For LIVE events: Don't change the main status, but update sales_status based on tier activity
		// For HOLD events: Only transition to on_sale when any tier becomes active, otherwise stay hold
		// For other events: Apply normal tier-based status logic
		targetStatus := event.Status
		targetSalesStatus := event.SalesStatus
		var reason string

		if event.Status == models.EventStatusLive.String() {
			// For live events, only update sales_status based on tier activity
			if anyTierActive {
				targetSalesStatus = models.EventSalesStatusActive.String()
				reason = fmt.Sprintf("Event sales activated - active tier(s): %s", strings.Join(activeTiers, ", "))
				log.Printf("[EventStatusWorker] Event %s (%s): LIVE event with ACTIVE tiers → sales_status: active", event.ID, event.Title)
			} else if allTiersEnded {
				targetSalesStatus = models.EventSalesStatusStopped.String()
				reason = fmt.Sprintf("Event sales stopped - all tier(s) ended: %s", strings.Join(endedTiers, ", "))
				log.Printf("[EventStatusWorker] Event %s (%s): LIVE event with all tiers ENDED → sales_status: stopped", event.ID, event.Title)
			} else {
				log.Printf("[EventStatusWorker] Event %s (%s): LIVE event with mixed tier state → keeping sales_status: %s", event.ID, event.Title, event.SalesStatus)
			}
		} else if event.Status == models.EventStatusHold.String() {
			// For hold events: Only transition to on_sale when any tier becomes active
			if anyTierActive {
				targetStatus = models.EventStatusOnSale.String()
				targetSalesStatus = models.EventSalesStatusActive.String() // Set sales_status to active when transitioning from hold to on_sale
				reason = fmt.Sprintf("Event automatically transitioned from hold to on_sale - active tier(s): %s", strings.Join(activeTiers, ", "))
				log.Printf("[EventStatusWorker] Event %s (%s): HOLD event with ACTIVE tiers → on_sale", event.ID, event.Title)
			} else {
				// Stay on hold - don't transition to sales_end or sales_upcoming
				log.Printf("[EventStatusWorker] Event %s (%s): HOLD event with no active tiers → staying hold", event.ID, event.Title)
			}
		} else {
			// Normal status logic for non-live, non-hold events
			if anyTierActive {
				targetStatus = models.EventStatusOnSale.String()
				targetSalesStatus = models.EventSalesStatusActive.String() // Set sales_status to active when on sale
				reason = fmt.Sprintf("Event transitioned to on_sale - active tier(s): %s", strings.Join(activeTiers, ", "))
				log.Printf("[EventStatusWorker] Event %s (%s): Tier is ACTIVE (between sales_start and sales_end) → on_sale", event.ID, event.Title)
			} else if allTiersEnded {
				targetStatus = models.EventStatusSalesEnd.String()
				targetSalesStatus = models.EventSalesStatusStopped.String() // Set sales_status to stopped when sales ended
				reason = fmt.Sprintf("Event transitioned to sales_end - all tier(s) ended: %s", strings.Join(endedTiers, ", "))
				log.Printf("[EventStatusWorker] Event %s (%s): All tiers ENDED → sales_end", event.ID, event.Title)
			} else if !allTiersNotStarted && hasFutureTiers {
				// Between sale periods: some tiers ended, some future tiers exist
				targetStatus = models.EventStatusSalesUpcoming.String()
				// Keep existing sales_status for sales_upcoming (don't change to paused)
				reason = fmt.Sprintf("Event transitioned to sales_upcoming - waiting for future tier(s): %s", strings.Join(futureTiers, ", "))
				log.Printf("[EventStatusWorker] Event %s (%s): BETWEEN sale periods (some ended, some future) → sales_upcoming", event.ID, event.Title)
			} else if allTiersNotStarted {
				targetStatus = models.EventStatusScheduled.String()
				// Keep existing sales_status for scheduled
				reason = fmt.Sprintf("Event transitioned to scheduled - all tier(s) not yet started: %s", strings.Join(notStartedTiers, ", "))
				log.Printf("[EventStatusWorker] Event %s (%s): All tiers NOT YET STARTED (earliest: %v) → scheduled", event.ID, event.Title, earliestSalesStart)
			} else {
				log.Printf("[EventStatusWorker] Event %s (%s): Mixed state (some tiers active, some not) → keeping %s", event.ID, event.Title, event.Status)
			}
		}

		// Only update if status or sales_status changed
		statusChanged := event.Status != targetStatus
		salesStatusChanged := event.SalesStatus != targetSalesStatus

		if statusChanged || salesStatusChanged {
			oldStatus := event.Status
			oldSalesStatus := event.SalesStatus

			if _, err := w.applyAutomaticTransition(event, targetStatus, targetSalesStatus, reason); err != nil {
				log.Printf("[EventStatusWorker] ❌ Failed to update event %s: %v", event.ID, err)
				continue
			}

			updatedCount++
			if statusChanged && salesStatusChanged {
				log.Printf("[EventStatusWorker] ✅ Updated event %s (%s) from status:%s/sales:%s to status:%s/sales:%s", event.ID, event.Title, oldStatus, oldSalesStatus, targetStatus, targetSalesStatus)
			} else if statusChanged {
				log.Printf("[EventStatusWorker] ✅ Updated event %s (%s) from %s to %s", event.ID, event.Title, oldStatus, targetStatus)
			} else if salesStatusChanged {
				log.Printf("[EventStatusWorker] ✅ Updated event %s (%s) sales_status from %s to %s", event.ID, event.Title, oldSalesStatus, targetSalesStatus)
			}
		}
	}

	if updatedCount > 0 {
		log.Printf("[EventStatusWorker] Updated %d events based on tier sales periods", updatedCount)
	}

	return nil
}
