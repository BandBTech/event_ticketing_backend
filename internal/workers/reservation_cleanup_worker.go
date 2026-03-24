package workers

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
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
func (w *ReservationCleanupWorker) releaseReservation(ctx context.Context, payment *models.PaymentIntent) error {
	tx := w.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 2. Find the tier to update reservation count
	var tier models.EventTier
	if err := tx.Where("id = ?", payment.TierID).First(&tier).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("tier not found: %w", err)
	}

	// 3. ATOMIC release: only decrement if reserved count matches expectation
	// WHERE: reserved >= quantity_to_release
	// This prevents double-release or corruption if cleanup runs twice
	result := tx.Model(&tier).
		Where("id = ? AND reserved >= ?", tier.ID, payment.Quantity).
		Update("reserved", gorm.Expr("reserved - ?", payment.Quantity))

	if result.Error != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update tier reservation: %w", result.Error)
	}

	// RowsAffected == 0 means reservation was already released (idempotent, OK to ignore)
	if result.RowsAffected == 0 {
		log.Printf("[CLEANUP] Reservation already released for payment %s, skipping", payment.ID)
		tx.Rollback()
		return nil // Already cleaned up, not an error
	}

	// 3. Mark payment as expired
	if err := tx.Model(payment).Update("status", "expired").Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to mark payment as expired: %w", err)
	}

	// 4. Log the release in audit trail
	auditLog := map[string]interface{}{
		"id":                uuid.New().String(),
		"payment_intent_id": payment.ID,
		"event_tier_id":     tier.ID,
		"action":            "expired",
		"quantity":          payment.Quantity,
		"reason":            "Payment not completed within 15 minutes",
		"created_at":        time.Now(),
	}
	if err := tx.Table("reservation_audits").Create(auditLog).Error; err != nil {
		log.Printf("[WARN] Failed to log expiry: %v", err)
		// Don't fail the entire cleanup for audit log issue
	}

	// 5. Commit transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit release transaction: %w", err)
	}

	log.Printf("[CLEANUP] Released %d reserved tickets for payment %s (expires_at was %s)",
		payment.Quantity, payment.ID, payment.ExpiresAt)

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
