package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"event-ticketing-backend/internal/gateways"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/state"
	"event-ticketing-backend/pkg/utils"

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

// Approve
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

// RejectRefund is similar to ApproveRefund but with different state transition and no gateway call
func (s *RefundService) RejectRefund(
	ctx context.Context,
	refundID, adminID uuid.UUID,
	reason string,
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
		if err := s.sm.Transition(refund.Status, models.RefundRejected); err != nil {
			return err
		}

		// update status
		if err := tx.Model(&refund).Updates(map[string]any{
			"status":           models.RefundRejected,
			"rejected_by":      adminID,
			"rejected_at":      time.Now(),
			"rejection_reason": reason,
		}).Error; err != nil {
			return err
		}

		return nil
	})

	return &refund, err
}

// ConfirmRefundWebhook handles webhook confirmation for refund processing
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

// Get all refunds for admin with pagination, filtering and sorting
func (s *RefundService) AdminGetAllRefundsList(
	ctx context.Context,
	status, search, refundType string,
	startDate, endDate *time.Time,
	page, limit int,
	sortBy, sortOrder string,
) ([]models.Refund, int64, error) {

	var refunds []models.Refund
	var total int64

	query := s.db.WithContext(ctx).Model(&models.Refund{}).
		Preload("PaymentIntent").
		Preload("Transaction").
		Preload("Event")

	if status != "" {
		query = query.Where("status = ?", status)
	}

	if refundType != "" {
		if refundType == "full" {
			query = query.Where("is_full_refund = ?", true)
		} else if refundType == "partial" {
			query = query.Where("is_full_refund = ?", false)
		}
	}

	if search != "" {
		query = query.Joins("JOIN transactions ON refunds.transaction_id = transactions.id").
			Joins("JOIN events ON refunds.event_id = events.id").
			Where("events.title ILIKE ?", "%"+search+"%")
	}

	if startDate != nil {
		query = query.Where("refunds.created_at >= ?", *startDate)
	}

	if endDate != nil {
		query = query.Where("refunds.created_at <= ?", *endDate)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	if err := query.Order(fmt.Sprintf("%s %s", sortBy, sortOrder)).
		Offset(offset).
		Limit(limit).
		Find(&refunds).Error; err != nil {
		return nil, 0, err
	}

	return refunds, total, nil
}

// Single refund retrieval for admin
func (s *RefundService) AdminGetRefund(
	ctx context.Context,
	refundID uuid.UUID,
) (*models.Refund, error) {

	var refund models.Refund

	err := s.db.WithContext(ctx).Model(&models.Refund{}).
		Preload("PaymentIntent").
		Preload("Transaction").
		Preload("Event").
		First(&refund, refundID).Error

	if err != nil {
		return nil, err
	}

	return &refund, nil
}

// Get refund status history for admin
func (s *RefundService) AdminGetRefundStatusHistory(
	ctx context.Context,
	refundID uuid.UUID,
) ([]models.RefundStatusHistory, error) {

	var history []models.RefundStatusHistory

	err := s.db.WithContext(ctx).
		Where("refund_id = ?", refundID).
		Order("changed_at ASC").
		Find(&history).Error

	if err != nil {
		return nil, err
	}

	return history, nil
}

// Retry failed refund (optional advanced feature)
func (s *RefundService) RetryFailedRefund(
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
		if refund.Status != models.RefundFailed {
			return fmt.Errorf("only failed refunds can be retried")
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

		// load gateway and retry logic (similar to ApproveRefund)

		return nil
	})

	return &refund, err
}

func uuidsToStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

// User get all refunds
func (s *RefundService) GetUserRefunds(
	ctx context.Context,
	userID uuid.UUID,
	page, limit int,
) ([]models.Refund, int64, error) {

	var refunds []models.Refund
	var total int64

	query := s.db.WithContext(ctx).Model(&models.Refund{}).
		Joins("JOIN transactions ON refunds.transaction_id = transactions.id").
		Where("transactions.actor_id = ?", userID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	if err := query.Order("refunds.created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&refunds).Error; err != nil {
		return nil, 0, err
	}

	return refunds, total, nil
}

// GetUserRefundStatusHistory
func (s *RefundService) GetUserRefundStatusHistory(
	ctx context.Context,
	userID uuid.UUID,
	refundID uuid.UUID,
) ([]models.RefundStatusHistory, error) {

	var history []models.RefundStatusHistory

	err := s.db.WithContext(ctx).
		Where("refund_id = ?", refundID).
		Order("changed_at ASC").
		Find(&history).Error

	if err != nil {
		return nil, err
	}

	return history, nil
}

// CheckRefundEligibility checks whether tickets can be refunded
func (s *RefundService) CheckRefundEligibility(
	ticketIDs []uuid.UUID,
) (bool, string, error) {

	if len(ticketIDs) == 0 {
		return false, "No tickets provided", nil
	}

	var tickets []models.Ticket

	if err := s.db.
		Preload("Event").
		Where("id IN ?", ticketIDs).
		Find(&tickets).Error; err != nil {
		return false, "", err
	}

	if len(tickets) != len(ticketIDs) {
		return false, "Some tickets were not found", nil
	}

	now := time.Now()

	for _, ticket := range tickets {

		// Ticket status validation
		if ticket.Status != models.TicketActive {
			return false,
				fmt.Sprintf("Ticket %s is not active", ticket.TicketNumber),
				nil
		}

		// Already checked in
		if ticket.CheckedInAt != nil {
			return false,
				fmt.Sprintf("Ticket %s is already checked in", ticket.TicketNumber),
				nil
		}

		// Event already ended
		if ticket.Event.EndDate.Before(now) {
			return false,
				fmt.Sprintf("Event already ended for ticket %s", ticket.TicketNumber),
				nil
		}

		// Optional:
		// Refund cutoff logic (example: no refund within 2 hours)
		refundCutoff := ticket.Event.StartDate.Add(-2 * time.Hour)

		if now.After(refundCutoff) {
			return false,
				fmt.Sprintf(
					"Refund window closed for ticket %s",
					ticket.TicketNumber,
				),
				nil
		}
	}

	return true, "", nil
}

// CancelTicketWithRefund marks tickets as cancelled and creates refund request
func (s *RefundService) CancelTicketWithRefund(
	ticketID uuid.UUID,
	userID uuid.UUID,
	reason string,
) (*models.RefundRequest, error) {

	tx := s.db.Begin()

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var ticket models.Ticket

	if err := tx.
		Preload("Event").
		Where("id = ?", ticketID).
		First(&ticket).Error; err != nil {

		tx.Rollback()
		return nil, err
	}

	// Ownership validation
	if ticket.ActorID != userID {
		tx.Rollback()
		return nil, utils.NewBusinessLogicError("Ticket does not belong to user")
	}

	// Double safety eligibility validation
	eligible, eligibilityReason, err :=
		s.CheckRefundEligibility([]uuid.UUID{ticketID})

	if err != nil {
		tx.Rollback()
		return nil, err
	}

	if !eligible {
		tx.Rollback()
		return nil, utils.NewBusinessLogicError(eligibilityReason)
	}

	now := time.Now()

	// Update ticket status
	if err := tx.Model(&ticket).
		Updates(map[string]interface{}{
			"status":     models.TicketCanceled,
			"updated_at": now,
		}).Error; err != nil {

		tx.Rollback()
		return nil, err
	}

	// Create refund request
	refundRequest := &models.RefundRequest{
		ID:        uuid.New(),
		UserID:    &ticket.ActorID,
		Reason:    reason,
		Status:    string(models.RefundPending),
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := tx.Create(refundRequest).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// Optional:
	// Release inventory immediately
	if err := tx.Model(&models.EventTier{}).
		Where("id = ?", ticket.TierID).
		Update("sold", gorm.Expr("GREATEST(sold - 1, 0)")).
		Error; err != nil {

		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	return refundRequest, nil
}
