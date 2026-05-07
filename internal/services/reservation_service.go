package services

import (
	"context"
	"fmt"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/currency"
	"event-ticketing-backend/pkg/types"

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

	for _, t := range in.Tiers {

		var tier models.EventTier
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND event_id = ?", t.TierID, in.EventID).
			First(&tier).Error; err != nil {
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

		res := tx.Model(&models.EventTier{}).
			Where("id = ? AND (quantity - sold - reserved) >= ?", t.TierID, t.Quantity).
			Update("reserved", gorm.Expr("reserved + ?", t.Quantity))

		if res.Error != nil {
			return 0, res.Error
		}
		if res.RowsAffected == 0 {
			return 0, fmt.Errorf("insufficient inventory")
		}

		if err := tx.Create(&models.TicketReservation{
			ID:              uuid.New(),
			PaymentIntentID: in.PaymentIntentID, // ✅ FINAL FIX
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
