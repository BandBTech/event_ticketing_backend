package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"event-ticketing-backend/internal/gateways"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/state"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RefundService struct {
	db         *gorm.DB
	gwRegistry *gateways.Registry
	sm         *state.StateMachine[models.RefundStatus]
	refundCalc *RefundCalculator
}

func NewRefundService(
	db *gorm.DB,
	gw *gateways.Registry,
	sm *state.StateMachine[models.RefundStatus],
	refundCalc *RefundCalculator,
) *RefundService {
	return &RefundService{
		db:         db,
		gwRegistry: gw,
		sm:         sm,
		refundCalc: refundCalc,
	}
}

type RefundRequest struct {
	PaymentIntentID uuid.UUID
	TicketIDs       []uuid.UUID
	Reason          string
	RequestedBy     uuid.UUID
	IdempotencyKey  string
}

func (s *RefundService) RequestRefund(ctx context.Context, req *RefundRequest) (*models.Refund, error) {

	returnValue := &models.Refund{}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

		// 🔒 LOCK PAYMENT INTENT
		var intent models.PaymentIntent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&intent, req.PaymentIntentID).Error; err != nil {
			return err
		}

		// 🚨 BLOCK MULTIPLE ACTIVE REFUNDS (IMPORTANT RULE)
		var existing models.Refund
		err := tx.Where(
			"payment_intent_id = ? AND status IN ?",
			req.PaymentIntentID,
			[]models.RefundStatus{
				models.RefundPending,
				models.RefundProcessing,
			},
		).First(&existing).Error

		if err == nil {
			return fmt.Errorf("refund already in progress for this payment")
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		// validate tickets (optional external function)
		if err := s.validateTickets(tx, req.PaymentIntentID, req.TicketIDs); err != nil {
			return err
		}

		// transaction load
		var txn models.Transaction
		if err := tx.Where("payment_intent_id = ?", req.PaymentIntentID).
			First(&txn).Error; err != nil {
			return err
		}

		// Calculate refund amount using centralized rules
		refundAmount, err := s.refundCalc.CalculateRefund(&txn, req.TicketIDs)
		if err != nil {
			return fmt.Errorf("failed to calculate refund amount: %w", err)
		}

		refund := &models.Refund{
			ID:                uuid.New(),
			RefundNumber:      fmt.Sprintf("REF-%s", uuid.New().String()[:8]),
			PaymentIntentID:   req.PaymentIntentID,
			TransactionID:     txn.ID,
			EventID:           txn.EventID,
			Amount:            refundAmount,
			Currency:          txn.Currency,
			Reason:            req.Reason,
			Status:            models.RefundPending,
			IsFullRefund:      len(req.TicketIDs) == txn.Quantity,
			AffectedTicketIDs: uuidsToStrings(req.TicketIDs),
		}

		if err := tx.Create(refund).Error; err != nil {
			return err
		}

		*returnValue = *refund
		return nil
	})

	return returnValue, err
}

func (s *RefundService) ApproveRefund(
	ctx context.Context,
	refundID, adminID uuid.UUID,
) (*models.Refund, error) {

	var refund models.Refund

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

		// 🔒 LOCK REFUND ROW
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&refund, refundID).Error; err != nil {
			return err
		}

		// 🚨 IDEMPOTENCY GUARD
		if refund.Status != models.RefundPending {
			return fmt.Errorf("refund already processed or in progress")
		}

		// 🔐 STATE MACHINE CHECK
		if err := s.sm.Transition(refund.Status, models.RefundProcessing); err != nil {
			return err
		}

		// update status
		if err := tx.Model(&refund).Updates(map[string]any{
			"status":      models.RefundProcessing,
			"approved_by": adminID,
			"approved_at": time.Now(),
		}).Error; err != nil {
			return err
		}

		// load gateway
		var intent models.PaymentIntent
		if err := tx.First(&intent, refund.PaymentIntentID).Error; err != nil {
			return err
		}

		gw, err := s.gwRegistry.Get(string(intent.PaymentGateway))
		if err != nil {
			return err
		}

		// get charge ID from transaction (SOURCE OF TRUTH)
		var txn models.Transaction
		if err := tx.First(&txn, refund.TransactionID).Error; err != nil {
			return err
		}

		if txn.ProviderChargeID == "" {
			return fmt.Errorf("missing provider charge id")
		}

		// call gateway
		resp, err := gw.CreateRefund(ctx, &gateways.RefundRequest{
			GatewayChargeID: txn.ProviderChargeID,
			Amount:          refund.Amount,
			Currency:        refund.Currency,
			Reason:          refund.Reason,
			Metadata: map[string]string{
				"refund_id": refund.ID.String(),
			},
		})
		if err != nil {
			_ = s.sm.Transition(refund.Status, models.RefundFailed)
			return err
		}

		// store provider refund id
		return tx.Model(&refund).Updates(map[string]any{
			"provider_refund_id": resp.GatewayRefundID,
		}).Error
	})

	return &refund, err
}

func (s *RefundService) ConfirmRefundWebhook(
	ctx context.Context,
	providerRefundID string,
) error {

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

		var refund models.Refund

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("provider_refund_id = ?", providerRefundID).
			First(&refund).Error; err != nil {
			return err
		}

		// idempotency
		if refund.Status == models.RefundSucceeded {
			return nil
		}

		// state transition
		if err := s.sm.Transition(refund.Status, models.RefundSucceeded); err != nil {
			return err
		}

		now := time.Now()

		// update refund
		if err := tx.Model(&refund).Updates(map[string]any{
			"status":       models.RefundSucceeded,
			"processed_at": now,
		}).Error; err != nil {
			return err
		}

		// update tickets
		if len(refund.AffectedTicketIDs) > 0 {

			var ticketIDs []uuid.UUID
			for _, id := range refund.AffectedTicketIDs {
				tid, _ := uuid.Parse(id)
				ticketIDs = append(ticketIDs, tid)
			}

			tx.Model(&models.Ticket{}).
				Where("id IN ?", ticketIDs).
				Updates(map[string]any{
					"status":      models.TicketRefunded,
					"refunded_at": now,
					"refund_id":   refund.ID,
				})

			// restore inventory
			type agg struct {
				TierID   uuid.UUID
				Quantity int
			}

			var result []agg
			tx.Model(&models.Ticket{}).
				Select("tier_id, count(*) as quantity").
				Where("id IN ?", ticketIDs).
				Group("tier_id").
				Scan(&result)

			for _, r := range result {
				tx.Model(&models.EventTier{}).
					Where("id = ?", r.TierID).
					Updates(map[string]any{
						"sold":      gorm.Expr("sold - ?", r.Quantity),
						"available": gorm.Expr("available + ?", r.Quantity),
					})
			}
		}

		return nil
	})
}
func (s *RefundService) validateTickets(
	tx *gorm.DB,
	intentID uuid.UUID,
	ticketIDs []uuid.UUID,
) error {

	if len(ticketIDs) == 0 {
		return fmt.Errorf("no tickets provided for refund")
	}

	var tickets []models.Ticket

	err := tx.Where(`
		id IN ? AND transaction_id IN (
			SELECT id FROM transactions WHERE payment_intent_id = ?
		)
	`, ticketIDs, intentID).Find(&tickets).Error

	if err != nil {
		return err
	}

	if len(tickets) != len(ticketIDs) {
		return fmt.Errorf("invalid tickets or not belonging to this payment")
	}

	for _, t := range tickets {

		if t.Status == models.TicketRefunded {
			return fmt.Errorf("ticket already refunded: %s", t.TicketNumber)
		}

		if t.Status == models.TicketUsed || t.CheckedInAt != nil {
			return fmt.Errorf("ticket already used: %s", t.TicketNumber)
		}
	}

	return nil
}

func uuidsToStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}
