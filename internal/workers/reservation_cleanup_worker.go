package workers

import (
	"context"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
)

// ReservationCleanupWorker handles TTL-based reservation cleanup
// Job: Every 5 minutes, find expired pending payments and release reserved tickets
type ReservationCleanupWorker struct {
	config *config.Config
	db     *gorm.DB
}

// NewReservationCleanupWorker creates a new cleanup worker
func NewReservationCleanupWorker(cfg *config.Config, db *gorm.DB) *ReservationCleanupWorker {
	return &ReservationCleanupWorker{
		config: cfg,
		db:     db,
	}
}

// Start runs the cleanup job in background (call once on app startup)
func (w *ReservationCleanupWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute) // Run every 5 minutes
	go func() {
		for {
			select {
			case <-ticker.C:
				if err := w.cleanupExpiredReservations(ctx); err != nil {
					log.Printf("[CLEANUP_ERROR] Failed to cleanup expired reservations: %v", err)
				}
			case <-ctx.Done():
				ticker.Stop()
				log.Printf("[CLEANUP] Reservation cleanup worker stopped")
				return
			}
		}
	}()
}

// cleanupExpiredReservations finds and releases expired pending payments
func (w *ReservationCleanupWorker) cleanupExpiredReservations(ctx context.Context) error {
	// Find all expired pending payments: WHERE status='pending' AND expires_at < NOW()
	var expiredPayments []models.PaymentIntent

	if err := w.db.Where("status = ? AND expires_at < ?", "pending", time.Now()).
		Find(&expiredPayments).Error; err != nil {
		return fmt.Errorf("failed to find expired payments: %w", err)
	}

	if len(expiredPayments) == 0 {
		return nil // Nothing to cleanup
	}

	log.Printf("[CLEANUP] Found %d expired payment reservations, releasing tickets...", len(expiredPayments))

	// Process each expired payment
	for _, payment := range expiredPayments {
		if err := w.releaseReservation(ctx, &payment); err != nil {
			log.Printf("[CLEANUP_ERROR] Failed to release reservation %s: %v", payment.ID, err)
			// Continue with next one instead of failing completely
			continue
		}
	}

	return nil
}

// releaseReservation marks a payment as expired and returns reserved tickets to available pool
func (w *ReservationCleanupWorker) releaseReservation(
	ctx context.Context,
	payment *models.PaymentIntent,
) error {

	tx := w.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 1. Load ALL reservations for this payment
	var reservations []models.TicketReservation

	if err := tx.
		Where("payment_intent_id = ? AND status = ?", payment.ID, "reserved").
		Find(&reservations).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to load reservations: %w", err)
	}

	// nothing to release (idempotent)
	if len(reservations) == 0 {
		tx.Rollback()
		return nil
	}

	// 2. Release per tier (grouped safely)
	for _, r := range reservations {

		result := tx.Model(&models.EventTier{}).
			Where("id = ? AND reserved >= ?", r.TierID, r.Quantity).
			Update("reserved", gorm.Expr("reserved - ?", r.Quantity))

		if result.Error != nil {
			tx.Rollback()
			return result.Error
		}

		// mark reservation released (idempotent safe)
		if err := tx.Model(&r).
			Update("status", "released").Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	// 3. Mark payment expired
	if err := tx.Model(&models.PaymentIntent{}).
		Where("id = ?", payment.ID).
		Update("status", "expired").Error; err != nil {
		tx.Rollback()
		return err
	}

	// 4. Commit
	if err := tx.Commit().Error; err != nil {
		return err
	}

	return nil
}

// GetExpiredReservationStats returns statistics on expired reservations (for monitoring)
func (w *ReservationCleanupWorker) GetExpiredReservationStats(ctx context.Context) (map[string]interface{}, error) {
	var stats struct {
		TotalReserved  int64
		TotalExpired   int64
		TotalConfirmed int64
	}

	// Count reserved tickets
	if err := w.db.Model(&models.EventTier{}).
		Select("COALESCE(SUM(reserved), 0) as total_reserved").
		Scan(&stats.TotalReserved).Error; err != nil {
		return nil, err
	}

	// Count expired payments
	if err := w.db.Model(&models.PaymentIntent{}).
		Where("status = ?", "expired").
		Count(&stats.TotalExpired).Error; err != nil {
		return nil, err
	}

	// Count confirmed (succeeded) payments
	if err := w.db.Model(&models.PaymentIntent{}).
		Where("status = ?", "succeeded").
		Count(&stats.TotalConfirmed).Error; err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total_reserved":  stats.TotalReserved,
		"total_expired":   stats.TotalExpired,
		"total_confirmed": stats.TotalConfirmed,
		"checked_at":      time.Now(),
	}, nil
}
