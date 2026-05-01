package services

import (
	"context"
	"fmt"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/currency"

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

//run this sql
//CREATE UNIQUE INDEX uniq_reservation_idempotency
// ON ticket_reservations(checkout_token, tier_id);

// =======================================================
// RESERVATION ONLY (SAFE + IDMPOTENT + TX-CLEAN)
// =======================================================
func (s *ReservationService) Reserve(
	ctx context.Context,
	tx *gorm.DB,
	req *UnifiedPurchaseRequest,
	checkoutToken string,
	expiresAt time.Time,
) (int64, error) {

	var total int64

	for _, t := range req.Tiers {

		var tier models.EventTier

		// lock row for safe concurrent updates
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND event_id = ?", t.TierID, req.EventID).
			First(&tier).Error; err != nil {
			return 0, fmt.Errorf("tier not found: %w", err)
		}

		// =========================
		// ATOMIC RESERVATION UPDATE
		// =========================
		res := tx.Model(&models.EventTier{}).
			Where("id = ? AND (quantity - sold - reserved) >= ?", tier.ID, t.Quantity).
			Update("reserved", gorm.Expr("reserved + ?", t.Quantity))

		if res.Error != nil {
			return 0, res.Error
		}

		if res.RowsAffected == 0 {
			return 0, fmt.Errorf("insufficient tickets")
		}

		// =========================
		// IDMPOTENT RESERVATION
		// =========================
		var existing models.TicketReservation
		err := tx.Where("checkout_token = ? AND tier_id = ?", checkoutToken, t.TierID).
			First(&existing).Error

		if err == nil {
			// already exists → skip (idempotency safe)
			continue
		}

		if err != gorm.ErrRecordNotFound {
			return 0, err
		}

		// create reservation
		if err := tx.Create(&models.TicketReservation{
			ID:            uuid.New(),
			CheckoutToken: checkoutToken,
			EventID:       req.EventID,
			TierID:        t.TierID,
			ActorID:       req.ActorID,
			ActorType:     req.ActorType,
			CustomerEmail: req.Email,
			Quantity:      t.Quantity,
			Status:        "reserved",
			ExpiresAt:     expiresAt,
		}).Error; err != nil {
			return 0, err
		}

		// =========================
		// SAFE MONEY CALCULATION
		// =========================
		priceUnit, err := currency.ToSmallestUnit(tier.Price, req.Currency)
		if err != nil {
			return 0, err
		}

		total += priceUnit * int64(t.Quantity)
	}

	return total, nil
}
