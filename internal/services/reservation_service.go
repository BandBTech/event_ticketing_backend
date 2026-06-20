package services

import (
	"context"
	"fmt"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/currency"
	"event-ticketing-backend/pkg/types"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ReservationService struct {
	db *gorm.DB
}

func NewReservationService(db *gorm.DB) *ReservationService {
	return &ReservationService{db: db}
}

type ReserveInput struct {
	PaymentIntentID uuid.UUID
	EventID         uuid.UUID
	ActorID         uuid.UUID
	ActorType       models.ActorType
	CustomerEmail   string
	Tiers           []types.TierSelection
	Currency        string
}

// Reserve is SAFE, IDPOTENT, and concurrency-safe
func (s *ReservationService) Reserve(
	ctx context.Context,
	tx *gorm.DB,
	in *ReserveInput,
	checkoutToken string,
	expiresAt time.Time,
) (int64, error) {

	var total int64

	// Release any expired reservations for this event before checking availability.
	// This ensures stale holds from crashed/abandoned sessions don't block new purchases.
	_ = tx.Exec(`
		UPDATE event_tiers
		SET reserved = event_tiers.reserved - r.quantity
		FROM ticket_reservations r
		WHERE r.tier_id = event_tiers.id
		  AND r.event_id = ?
		  AND r.status = ?
		  AND r.expires_at < NOW()
	`, in.EventID, models.ReservationReserved).Error

	_ = tx.Exec(`
		UPDATE ticket_reservations
		SET status = ?
		WHERE event_id = ?
		  AND status = ?
		  AND expires_at < NOW()
	`, models.ReservationExpired, in.EventID, models.ReservationReserved).Error

	for _, t := range in.Tiers {

		var tier models.EventTier
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND event_id = ?", t.TierID, in.EventID).
			First(&tier).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return 0, utils.NewNotFoundError("ticket tier")
			}
			return 0, err
		}

		// ✅ FIXED: use PaymentIntentID ONLY
		var existing models.TicketReservation
		err := tx.Where("payment_intent_id = ? AND tier_id = ?",
			in.PaymentIntentID, t.TierID).
			First(&existing).Error

		if err == nil {
			continue
		}
		if err != gorm.ErrRecordNotFound {
			return 0, err
		}

		// Calculate available tickets before attempting reservation
		availableTickets := tier.Quantity - tier.Sold - tier.Reserved

		// Check if sufficient tickets available BEFORE attempting update
		if availableTickets < t.Quantity {
			return 0, utils.NewBusinessLogicError(
				fmt.Sprintf(
					"Insufficient tickets available. You requested %d ticket(s) but only %d available in this tier. Please select fewer tickets or choose a different tier.",
					t.Quantity,
					availableTickets,
				),
			)
		}

		res := tx.Model(&models.EventTier{}).
			Where("id = ? AND (quantity - sold - reserved) >= ?", t.TierID, t.Quantity).
			Update("reserved", gorm.Expr("reserved + ?", t.Quantity))

		if res.Error != nil {
			return 0, utils.NewDatabaseError("Failed to reserve tickets", res.Error)
		}
		if res.RowsAffected == 0 {
			// This shouldn't happen now due to check above, but handle race condition just in case
			// Reload tier to get current accurate count
			var updatedTier models.EventTier
			if err := tx.First(&updatedTier, t.TierID).Error; err == nil {
				currentAvailable := updatedTier.Quantity - updatedTier.Sold - updatedTier.Reserved
				return 0, utils.NewBusinessLogicError(
					fmt.Sprintf(
						"Tickets were just sold out. You requested %d ticket(s) but only %d available now. Please try again with fewer tickets.",
						t.Quantity,
						currentAvailable,
					),
				)
			}
			return 0, utils.NewBusinessLogicError("Insufficient tickets available for the selected tier.")
		}

		// Create reservation record
		if err := tx.Create(&models.TicketReservation{
			PaymentIntentID: in.PaymentIntentID,
			CheckoutToken:   checkoutToken,
			EventID:         in.EventID,
			TierID:          t.TierID,
			ActorID:         in.ActorID,
			ActorType:       in.ActorType,
			CustomerEmail:   in.CustomerEmail,
			Quantity:        t.Quantity,
			Status:          models.ReservationReserved,
			ExpiresAt:       expiresAt,
		}).Error; err != nil {
			return 0, err
		}

		unit, _ := currency.ToSmallestUnit(tier.Price, in.Currency)
		total += unit * int64(t.Quantity)
	}

	return total, nil
}

// Release (payment failure / cancel / expiry)
func (s *ReservationService) Release(
	ctx context.Context,
	tx *gorm.DB,
	paymentIntentID uuid.UUID,
) error {

	var reservations []models.TicketReservation

	if err := tx.Where("payment_intent_id = ? AND status = ?", paymentIntentID, models.ReservationReserved).Find(&reservations).Error; err != nil {
		return err
	}

	for _, r := range reservations {

		tx.Model(&models.EventTier{}).
			Where("id = ?", r.TierID).
			Update("reserved", gorm.Expr("reserved - ?", r.Quantity))

		tx.Model(&r).
			Update("status", models.ReservationExpired)
	}

	return nil
}
