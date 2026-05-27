package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/webhook"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/internal/state"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/currency"
	"event-ticketing-backend/pkg/utils"
)

type PaymentWorker struct {
	db            *gorm.DB
	webhookSecret string

	intentSM *state.StateMachine[models.PaymentIntentStatus]
	paSM     *state.StateMachine[models.PaymentAttemptStatus]
	txSM     *state.StateMachine[models.TransactionStatus]
	refundSM *state.StateMachine[models.RefundStatus]

	emailOutboxService *services.EmailOutboxService
	ticketService      *services.TicketService
	feeExtractor       *services.FeeExtractor
	refundCalculator   *services.RefundCalculator
	config             *config.Config
}

func NewPaymentWorker(
	db *gorm.DB,
	webhookSecret string,
	intentSM *state.StateMachine[models.PaymentIntentStatus],
	txSM *state.StateMachine[models.TransactionStatus],
	emailOutboxService *services.EmailOutboxService,
	ticketService *services.TicketService,
	config *config.Config,
) *PaymentWorker {
	return &PaymentWorker{
		db:                 db,
		webhookSecret:      webhookSecret,
		intentSM:           intentSM,
		paSM:               state.NewStateMachine(state.PaymentAttemptTransitions),
		txSM:               txSM,
		refundSM:           state.NewStateMachine(state.RefundTransitions),
		emailOutboxService: emailOutboxService,
		ticketService:      ticketService,
		feeExtractor:       services.NewFeeExtractor(),
		refundCalculator:   services.NewRefundCalculator(),
		config:             config,
	}
}

// ================================
// WEBHOOK ENTRY POINT
// ================================

func (w *PaymentWorker) HandleStripeWebhook(
	ctx context.Context,
	body []byte,
	signature string,
) error {

	event, err := webhook.ConstructEvent(body, signature, w.webhookSecret)
	if err != nil {
		return err
	}

	fmt.Printf("[WEBHOOK] Received %s event: %s\n", event.Type, event.ID)

	// Idempotency check
	var exists models.WebhookEvent

	if err := w.db.
		Where("gateway_event_id = ?", event.ID).
		First(&exists).Error; err == nil {

		fmt.Printf("[WEBHOOK] %s already processed, skipping\n", event.ID)
		return nil
	}

	// Store webhook event
	webhookEvent := models.WebhookEvent{
		ID:             uuid.New(),
		PaymentGateway: models.PaymentGatewayStripe,
		GatewayEventID: event.ID,
		EventType:      string(event.Type),
		Payload:        models.JSONMap{"raw": string(body)},
		Status:         "processing",
		ReceivedAt:     time.Now(),
	}

	if err := w.db.Create(&webhookEvent).Error; err != nil {
		return err
	}

	fmt.Printf("[WEBHOOK] Processing %s (%s)\n", event.Type, event.ID)

	// Process event
	switch event.Type {

	case "payment_intent.succeeded":
		err = w.processPaymentIntentSucceeded(ctx, event)

	case "checkout.session.async_payment_succeeded":
		err = w.processPaymentIntentSucceeded(ctx, event)

	case "payment_intent.payment_failed":
		err = w.processPaymentFailed(ctx, event)

	case "checkout.session.completed":
		err = w.processCheckoutSessionCompleted(ctx, event)

	case "checkout.session.expired":
		err = w.processPaymentExpired(ctx, event)

	case "charge.refunded":
		err = w.processRefund(ctx, event)
	case "refund.created", "refund.updated":
		err = w.processRefundEvent(ctx, event)

	default:
		err = w.markWebhookProcessed(ctx, event.ID, "ignored")
	}

	// Handle processing failure
	if err != nil {

		fmt.Printf(
			"[WEBHOOK] Failed processing %s (%s): %v\n",
			event.Type,
			event.ID,
			err,
		)

		_ = w.markWebhookFailed(ctx, event.ID, err)

		return err
	}

	// Mark success
	return w.markWebhookProcessed(ctx, event.ID, "processed")
}

// ================================
// EVENT HANDLERS
// ================================

func (w *PaymentWorker) processCheckoutSessionCompleted(ctx context.Context, event stripe.Event) error {
	var session stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
		return err
	}
	fmt.Printf("[CHECKOUT] session completed: %s\n", session.ID)
	return w.markWebhookProcessed(ctx, event.ID, "processed")
}

func (w *PaymentWorker) processPaymentExpired(ctx context.Context, event stripe.Event) error {
	return w.markWebhookProcessed(ctx, event.ID, "processed")
}

func (w *PaymentWorker) processRefund(ctx context.Context, event stripe.Event) error {
	var charge stripe.Charge
	if err := json.Unmarshal(event.Data.Raw, &charge); err != nil {
		return fmt.Errorf("failed to parse charge from refund event: %w", err)
	}

	if charge.Refunds == nil || len(charge.Refunds.Data) == 0 {
		fmt.Printf("[WEBHOOK] charge.refunded event has no refunds, skipping\n")
		return w.markWebhookProcessed(ctx, event.ID, "processed")
	}

	return w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range charge.Refunds.Data {
			if charge.Refunds.Data[i] == nil {
				continue
			}
			if err := w.applyStripeRefundUpdate(ctx, tx, charge.Refunds.Data[i]); err != nil {
				return err
			}
		}

		return nil
	})
}

func (w *PaymentWorker) processRefundEvent(ctx context.Context, event stripe.Event) error {
	var stripeRefund stripe.Refund
	if err := json.Unmarshal(event.Data.Raw, &stripeRefund); err != nil {
		return fmt.Errorf("failed to parse refund event: %w", err)
	}

	return w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return w.applyStripeRefundUpdate(ctx, tx, &stripeRefund)
	})
}

func (w *PaymentWorker) applyStripeRefundUpdate(ctx context.Context, tx *gorm.DB, stripeRefund *stripe.Refund) error {
	providerRefundID := stripeRefund.ID

	var refund models.Refund
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("provider_refund_id = ?", providerRefundID).
		First(&refund).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fmt.Printf("[WEBHOOK] no refund found for provider_refund_id=%s, skipping\n", providerRefundID)
			return nil
		}
		return err
	}

	targetStatus := models.RefundSucceeded
	historyRemark := "refund succeeded via stripe webhook"
	auditAction := "refund_succeeded"
	processedAt := true

	switch stripeRefund.Status {
	case stripe.RefundStatusSucceeded:
		targetStatus = models.RefundSucceeded
	case stripe.RefundStatusFailed, stripe.RefundStatusCanceled:
		targetStatus = models.RefundFailed
		historyRemark = "refund failed via stripe webhook"
		auditAction = "refund_failed"
	case stripe.RefundStatusPending:
		targetStatus = models.RefundProcessing
		historyRemark = "refund still processing via stripe webhook"
		auditAction = "refund_processing"
		processedAt = false
	}

	if refund.Status == targetStatus {
		return nil
	}

	if err := w.validateRefundTransition(refund.Status, targetStatus); err != nil {
		return fmt.Errorf("invalid refund transition: %w", err)
	}

	now := time.Now()
	refundUpdates := map[string]any{
		"status":     targetStatus,
		"updated_at": now,
	}
	if processedAt {
		refundUpdates["processed_at"] = now
	}
	if err := tx.Model(&refund).Updates(refundUpdates).Error; err != nil {
		return err
	}

	if err := w.logRefundStatusHistory(tx, refund.ID, refund.Status, targetStatus, nil, "webhook", historyRemark); err != nil {
		return err
	}

	if err := services.LogPaymentAuditTx(
		tx,
		auditAction,
		"refund",
		refund.ID,
		nil,
		"webhook",
		&refund.EventID,
		map[string]interface{}{
			"provider_refund_id": providerRefundID,
			"old_status":         refund.Status,
			"new_status":         targetStatus,
			"stripe_status":      stripeRefund.Status,
		},
	); err != nil {
		return err
	}

	if targetStatus != models.RefundSucceeded {
		return nil
	}

	if refund.TicketID != uuid.Nil {
		if err := tx.Model(&models.Ticket{}).Where("id = ?", refund.TicketID).Updates(map[string]any{
			"status":        models.TicketRefunded,
			"refund_id":     refund.ID,
			"refunded_at":   now,
			"refund_amount": refund.Amount,
		}).Error; err != nil {
			return err
		}

		if err := services.LogPaymentAuditTx(
			tx,
			"ticket_refunded",
			"ticket",
			refund.TicketID,
			nil,
			"webhook",
			&refund.EventID,
			map[string]interface{}{
				"refund_id":           refund.ID,
				"refund_amount_cents": refund.Amount,
			},
		); err != nil {
			return err
		}

		if err := tx.Exec(`UPDATE event_tiers SET quantity = quantity + 1 WHERE id = (SELECT tier_id FROM tickets WHERE id = ?)`, refund.TicketID).Error; err != nil {
			fmt.Printf("[WEBHOOK] failed to restore inventory for ticket %s: %v\n", refund.TicketID, err)
		}
	} else {
		if err := tx.Model(&models.Ticket{}).
			Where("transaction_id = ? AND status <> ?", refund.TransactionID, models.TicketRefunded).
			Updates(map[string]any{
				"status":      models.TicketRefunded,
				"refund_id":   refund.ID,
				"refunded_at": now,
			}).Error; err != nil {
			return err
		}
	}

	var intent models.PaymentIntent
	if err := tx.First(&intent, refund.PaymentIntentID).Error; err == nil {
		go func() {
			if err := w.sendRefundProcessedEmail(ctx, &intent, &refund); err != nil {
				fmt.Printf("[WEBHOOK] failed to send refund email: %v\n", err)
			}
		}()
	}

	return nil
}

func (w *PaymentWorker) processPaymentIntentSucceeded(ctx context.Context, event stripe.Event) error {
	var checkoutToken string
	var eventData interface{}

	if event.Type == "checkout.session.async_payment_succeeded" {
		var session stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
			return err
		}
		if session.Metadata != nil {
			checkoutToken = session.Metadata["checkout_token"]
		}
		eventData = session
	} else {
		var pi stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
			return err
		}
		if pi.Metadata != nil {
			checkoutToken = pi.Metadata["checkout_token"]
		}
		eventData = pi
	}

	if checkoutToken == "" {
		return fmt.Errorf("missing checkout_token in %s metadata", event.Type)
	}

	var committedIntent models.PaymentIntent

	if err := w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock intent row for this checkout session
		var intent models.PaymentIntent
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("checkout_token = ?", checkoutToken).
			First(&intent).Error; err != nil {
			return fmt.Errorf("payment intent not found for checkout_token %s: %w", checkoutToken, err)
		}

		// Idempotency guard — already processed
		if intent.Status == models.PaymentIntentSucceeded {
			return nil
		}

		// 1. Load event for tier/ticket generation context
		var eventRecord models.Event
		if err := tx.Where("id = ?", intent.EventID).First(&eventRecord).Error; err != nil {
			return fmt.Errorf("failed to load event: %w", err)
		}

		// 2. Create transaction record (financial ledger entry)
		transaction, err := w.createTransaction(tx, &intent, string(event.Type), eventData)
		if err != nil {
			return fmt.Errorf("failed to create transaction: %w", err)
		}

		// 3. Create tickets (source of truth: tier selections on intent)
		if err := w.createTickets(tx, &intent, eventRecord, transaction.ID); err != nil {
			return fmt.Errorf("failed to create tickets: %w", err)
		}

		// 4. Confirm reservations
		if err := w.confirmReservations(tx, intent.ID); err != nil {
			return fmt.Errorf("failed to confirm reservations: %w", err)
		}

		// 5. Advance intent status
		if err := w.validatePaymentIntentTransition(intent.Status, models.PaymentIntentSucceeded); err != nil {
			return fmt.Errorf("invalid payment intent status transition: %w", err)
		}
		if err := w.markPaymentIntentSucceeded(tx, intent.ID); err != nil {
			return fmt.Errorf("failed to update payment intent succeeded at: %w", err)
		}
		if err := w.markPaymentAttemptAuthorized(tx, transaction.PaymentAttemptID); err != nil {
			return fmt.Errorf("failed to update payment attempt completed at: %w", err)
		}

		if err := w.validateTransactionTransition(models.TransactionProcessing, models.TransactionSucceeded); err != nil {
			return fmt.Errorf("invalid transaction status transition: %w", err)
		}

		if err := w.validatePaymentAttemptTransition(models.PaymentAttemptInitiated, models.PaymentAttemptAuthorized); err != nil {
			return fmt.Errorf("invalid payment attempt status transition: %w", err)
		}

		committedIntent = intent
		return nil
	}); err != nil {
		return err
	}

	if err := w.sendPurchaseSuccessEmail(ctx, &committedIntent); err != nil {
		fmt.Printf("failed to queue purchase success email: %v\n", err)
	}

	return nil
}

func (w *PaymentWorker) processPaymentFailed(ctx context.Context, event stripe.Event) error {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
		return err
	}

	checkoutToken := ""
	if pi.Metadata != nil {
		checkoutToken = pi.Metadata["checkout_token"]
	}
	if checkoutToken == "" {
		return fmt.Errorf("missing checkout_token in payment_intent metadata")
	}

	return w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock intent to get its ID before releasing resources
		var intent models.PaymentIntent
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("checkout_token = ?", checkoutToken).
			First(&intent).Error; err != nil {
			return fmt.Errorf("payment intent not found for checkout_token %s: %w", checkoutToken, err)
		}

		// Idempotency guard
		if intent.Status == models.PaymentIntentFailed {
			return nil
		}

		// Release reservations and cancel any pending tickets
		if err := w.releaseReservations(tx, intent.ID); err != nil {
			return fmt.Errorf("failed to release reservations: %w", err)
		}
		if err := w.cancelPendingTickets(tx, intent.ID); err != nil {
			return fmt.Errorf("failed to cancel pending tickets: %w", err)
		}

		// Advance intent status
		if err := w.validatePaymentIntentTransition(intent.Status, models.PaymentIntentFailed); err != nil {
			return fmt.Errorf("invalid payment intent status transition: %w", err)
		}
		if err := w.validateTransactionTransition(models.TransactionProcessing, models.TransactionFailed); err != nil {
			return fmt.Errorf("invalid transaction status transition: %w", err)
		}

		if err := w.validatePaymentAttemptTransition(models.PaymentAttemptInitiated, models.PaymentAttemptFailed); err != nil {
			return fmt.Errorf("invalid payment attempt status transition: %w", err)
		}
		if err := w.markPaymentIntentFailed(tx, intent.ID); err != nil {
			return fmt.Errorf("failed to update payment intent failed at: %w", err)
		}

		return w.markWebhookProcessed(ctx, event.ID, "processed")
	})
}

// ================================
// CORE BUSINESS LOGIC
// ================================

// createTransaction builds and persists the financial ledger entry for a successful payment.
// Idempotent: returns existing transaction if one already exists for this intent.
func (w *PaymentWorker) createTransaction(
	tx *gorm.DB,
	intent *models.PaymentIntent,
	eventType string,
	eventData interface{},
) (*models.Transaction, error) {

	// Idempotency: return existing if already created
	var existing models.Transaction
	err := tx.
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("payment_intent_id = ?", intent.ID).
		First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Load event for commission rate
	var event models.Event
	if err := tx.Where("id = ?", intent.EventID).First(&event).Error; err != nil {
		return nil, fmt.Errorf("failed to load event: %w", err)
	}

	// Load latest payment attempt
	var attempt models.PaymentAttempt
	if err := tx.
		Where("payment_intent_id = ?", intent.ID).
		Order("created_at DESC").
		First(&attempt).Error; err != nil {
		return nil, fmt.Errorf("failed to load payment attempt: %w", err)
	}

	// Extract or estimate gateway fee
	var gatewayFee int64
	feeSnapshot, err := w.feeExtractor.ExtractFromWebhook(eventType, eventData)
	if err != nil {
		return nil, fmt.Errorf("failed extracting fees: %w", err)
	}
	if feeSnapshot != nil {
		gatewayFee = feeSnapshot.GatewayFee
	} else {
		gatewayFee, err = w.feeExtractor.EstimateGatewayFee(
			intent.AmountTotal,
			intent.Currency,
			intent.PaymentGateway,
		)
		if err != nil {
			return nil, fmt.Errorf("failed estimating gateway fee: %w", err)
		}
	}

	// Calculate platform fee and organizer earning
	platformFee, err := w.feeExtractor.CalculatePlatformFee(
		intent.AmountTotal,
		intent.Currency,
		event.CommissionRate,
	)
	if err != nil {
		return nil, fmt.Errorf("failed calculating platform fee: %w", err)
	}
	organizerShare := intent.AmountTotal - platformFee - gatewayFee

	// Extract provider charge ID from the webhook payload
	var providerChargeID string
	switch e := eventData.(type) {
	case stripe.CheckoutSession:
		if e.PaymentIntent != nil {
			providerChargeID = e.PaymentIntent.ID
		}
	case stripe.PaymentIntent:
		if e.LatestCharge != nil && e.LatestCharge.ID != "" {
			providerChargeID = e.LatestCharge.ID
		} else {
			providerChargeID = e.ID
		}
	}

	now := time.Now()
	transaction := &models.Transaction{
		ID:               uuid.New(),
		PaymentIntentID:  intent.ID,
		PaymentAttemptID: attempt.ID,
		EventID:          intent.EventID,
		ActorID:          intent.ActorID,
		ActorType:        intent.ActorType,
		ProviderChargeID: providerChargeID,
		PaymentGateway:   intent.PaymentGateway,
		AmountTotal:      intent.AmountTotal,
		Currency:         intent.Currency,
		PlatformFee:      platformFee,
		GatewayFee:       gatewayFee,
		OrganizerShare:   organizerShare,
		Quantity:         intent.Quantity,
		Status:           models.TransactionSucceeded,
		IsPaidOut:        false,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := tx.Create(transaction).Error; err != nil {
		return nil, fmt.Errorf("failed creating transaction: %w", err)
	}

	if err := services.LogPaymentAuditTx(
		tx,
		"transaction_created",
		"transaction",
		transaction.ID,
		&intent.ActorID,
		string(intent.ActorType),
		&intent.EventID,
		map[string]interface{}{
			"payment_intent_id":  transaction.PaymentIntentID,
			"payment_attempt_id": transaction.PaymentAttemptID,
			"provider_charge_id": transaction.ProviderChargeID,
			"payment_gateway":    transaction.PaymentGateway,
			"amount_total_cents": transaction.AmountTotal,
			"currency":           transaction.Currency,
			"platform_fee_cents": transaction.PlatformFee,
			"gateway_fee_cents":  transaction.GatewayFee,
			"organizer_share":    transaction.OrganizerShare,
			"quantity":           transaction.Quantity,
			"status":             transaction.Status,
		},
	); err != nil {
		return nil, fmt.Errorf("failed creating transaction audit log: %w", err)
	}

	return transaction, nil
}

// createTickets delegates ticket generation to TicketService after a confirmed payment.
// Source of truth for tier selections is the intent's TicketTierSelections.
func (w *PaymentWorker) createTickets(
	tx *gorm.DB,
	intent *models.PaymentIntent,
	event models.Event,
	transactionID uuid.UUID,
) error {

	return w.ticketService.CreateTicketsAfterPayment(tx, intent, event, transactionID)
}

// ================================
// RESERVATION HELPERS
// ================================

func (w *PaymentWorker) confirmReservations(tx *gorm.DB, paymentIntentID uuid.UUID) error {
	return tx.Model(&models.TicketReservation{}).
		Where("payment_intent_id = ?", paymentIntentID).
		Update("status", "confirmed").Error
}

func (w *PaymentWorker) releaseReservations(tx *gorm.DB, paymentIntentID uuid.UUID) error {
	return tx.Model(&models.TicketReservation{}).
		Where("payment_intent_id = ?", paymentIntentID).
		Update("status", "released").Error
}

func (w *PaymentWorker) cancelPendingTickets(tx *gorm.DB, paymentIntentID uuid.UUID) error {
	return tx.Model(&models.Ticket{}).
		Where("payment_intent_id = ? AND status = ?", paymentIntentID, "pending_payment").
		Update("status", models.TicketCanceled).Error
}

// ================================
// REFUND HELPERS
// ================================

func (w *PaymentWorker) validateRefundTransition(current, target models.RefundStatus) error {
	refundSM := state.NewStateMachine(state.RefundTransitions)
	return refundSM.Transition(current, target)
}

func (w *PaymentWorker) updateRefundSucceeded(tx *gorm.DB, refund *models.Refund) error {
	return tx.Model(refund).Updates(map[string]any{
		"status":     models.RefundSucceeded,
		"updated_at": time.Now(),
	}).Error
}

func (w *PaymentWorker) updateTicketRefundStatus(tx *gorm.DB, refund *models.Refund) error {
	now := time.Now()
	ticketStatus := models.TicketRefunded

	if refund.TicketID == uuid.Nil {
		return tx.Model(&models.Ticket{}).
			Where("transaction_id = ? AND status <> ?", refund.TransactionID, models.TicketRefunded).
			Updates(map[string]any{
				"refund_id":   &refund.ID,
				"refunded_at": &now,
				"status":      ticketStatus,
			}).Error
	}

	updates := map[string]any{
		"refund_id":     &refund.ID,
		"refunded_at":   &now,
		"refund_amount": refund.Amount,
		"status":        ticketStatus,
	}

	return tx.Model(&models.Ticket{}).Where("id = ?", refund.TicketID).Updates(updates).Error
}

func (w *PaymentWorker) restoreInventory(tx *gorm.DB, refund *models.Refund) error {
	if refund.TicketID == uuid.Nil {
		return nil
	}
	var ticket models.Ticket
	if err := tx.Where("id = ?", refund.TicketID).First(&ticket).Error; err != nil {
		return nil // ticket not found — skip silently
	}

	return tx.Exec(`
		UPDATE event_tiers
		SET quantity = quantity + 1
		WHERE id = ?
	`, ticket.TierID).Error
}

// NOTE: updateTransactionRefundStatus is deprecated and should not be used.
// Transaction status should NEVER change - refunds are separate entities.
// Keeping function for reference but it's no longer called anywhere.
// TODO: Remove this function in future cleanup
//
// DEPRECATED: func (w *PaymentWorker) updateTransactionRefundStatus(tx *gorm.DB, transactionID uuid.UUID) error {

func (w *PaymentWorker) logRefundStatusHistory(
	tx *gorm.DB,
	refundID uuid.UUID,
	fromStatus, toStatus models.RefundStatus,
	changedBy *uuid.UUID,
	changedByType string,
	remark string,
) error {
	history := &models.RefundStatusHistory{
		ID:            uuid.New(),
		RefundID:      refundID,
		OldStatus:     fromStatus,
		NewStatus:     toStatus,
		ChangedByID:   changedBy,
		ChangedByType: changedByType,
		Remarks:       remark,
		ChangedAt:     time.Now(),
	}
	return tx.Create(history).Error
}

func (w *PaymentWorker) markPaymentIntentSucceeded(
	tx *gorm.DB,
	paymentIntentID uuid.UUID,
) error {
	return tx.Model(&models.PaymentIntent{}).
		Where("id = ?", paymentIntentID).
		Updates(map[string]interface{}{
			"status":       models.PaymentIntentSucceeded,
			"succeeded_at": time.Now(),
			"updated_at":   time.Now(),
		}).Error
}

func (w *PaymentWorker) markPaymentAttemptAuthorized(
	tx *gorm.DB,
	paymentAttemptID uuid.UUID,
) error {
	return tx.Model(&models.PaymentAttempt{}).
		Where("id = ?", paymentAttemptID).
		Updates(map[string]interface{}{
			"status":     models.PaymentAttemptAuthorized,
			"updated_at": time.Now(),
		}).Error
}

func (w *PaymentWorker) markPaymentIntentFailed(
	tx *gorm.DB,
	paymentIntentID uuid.UUID,
) error {
	return tx.Model(&models.PaymentIntent{}).
		Where("id = ?", paymentIntentID).
		Updates(map[string]interface{}{
			"status":     models.PaymentIntentFailed,
			"failed_at":  time.Now(),
			"updated_at": time.Now(),
		}).Error
}

// ================================
// AUDIT LOG HELPERS

// ================================
// STATE MACHINE VALIDATORS
// ================================

func (w *PaymentWorker) validatePaymentIntentTransition(from, to models.PaymentIntentStatus) error {
	if !w.intentSM.Can(from, to) {
		return fmt.Errorf("invalid payment intent transition %v → %v", from, to)
	}
	return nil
}

func (w *PaymentWorker) validateTransactionTransition(from, to models.TransactionStatus) error {
	if !w.txSM.Can(from, to) {
		return fmt.Errorf("invalid transaction transition %v → %v", from, to)
	}
	return nil
}

func (w *PaymentWorker) validatePaymentAttemptTransition(from, to models.PaymentAttemptStatus) error {
	if !w.paSM.Can(from, to) {
		return fmt.Errorf("invalid payment attempt transition %v → %v", from, to)
	}
	return nil
}

// ================================
// WEBHOOK STATE HELPERS
// ================================

func (w *PaymentWorker) markWebhookProcessed(ctx context.Context, eventID string, status string) error {
	return w.db.WithContext(ctx).
		Model(&models.WebhookEvent{}).
		Where("gateway_event_id = ?", eventID).
		Updates(map[string]any{
			"status":       status,
			"processed_at": time.Now(),
		}).Error
}

func (w *PaymentWorker) markWebhookFailed(ctx context.Context, eventID string, err error) error {
	return w.db.WithContext(ctx).
		Model(&models.WebhookEvent{}).
		Where("gateway_event_id = ?", eventID).
		Updates(map[string]any{
			"status":        "failed",
			"error_message": err.Error(),
		}).Error
}

// ================================
// EMAIL NOTIFICATIONS
// ================================

func (w *PaymentWorker) sendPurchaseSuccessEmail(ctx context.Context, intent *models.PaymentIntent) error {
	if intent.CustomerEmail == "" {
		return nil
	}

	var transaction models.Transaction
	if err := w.db.WithContext(ctx).Where("payment_intent_id = ?", intent.ID).First(&transaction).Error; err != nil {
		return err
	}

	var tickets []models.Ticket
	if err := w.db.WithContext(ctx).Where("transaction_id = ?", transaction.ID).Find(&tickets).Error; err != nil {
		return err
	}

	var event models.Event
	if err := w.db.WithContext(ctx).Where("id = ?", intent.EventID).First(&event).Error; err != nil {
		return err
	}

	recipientName := "Customer"
	switch intent.ActorType {
	case models.ActorUser:
		var user models.User
		if err := w.db.WithContext(ctx).Where("id = ?", intent.ActorID).First(&user).Error; err == nil {
			recipientName = strings.TrimSpace(user.FirstName + " " + user.LastName)
			if recipientName == "" {
				recipientName = user.Email
			}
		}
	case models.ActorGuest:
		var guest models.GuestUser
		if err := w.db.WithContext(ctx).Where("id = ?", intent.ActorID).First(&guest).Error; err == nil {
			recipientName = strings.TrimSpace(guest.FirstName + " " + guest.LastName)
			if recipientName == "" {
				recipientName = guest.Email
			}
		}
	}
	if recipientName == "" {
		recipientName = intent.CustomerEmail
	}

	var organizerName string
	if event.OrganizerID != uuid.Nil {
		var organizer models.User
		if err := w.db.WithContext(ctx).Where("id = ?", event.OrganizerID).First(&organizer).Error; err == nil {
			organizerName = strings.TrimSpace(organizer.FirstName + " " + organizer.LastName)
		}
	}

	venue := event.VenueName
	if venue == "" {
		venue = event.Location
	}

	eventEndDate := ""
	eventEndTime := ""
	calendarEnd := event.EndDate
	if !event.EndDate.IsZero() {
		eventEndDate = event.EndDate.Format("January 2, 2006")
		eventEndTime = event.EndDate.Format("3:04 PM")
	} else {
		calendarEnd = event.StartDate
	}

	jwtService := utils.NewJWTService(&w.config.JWT)
	ticketViewURL := ""
	token, err := jwtService.GenerateTicketToken(
		event,
		intent.ActorID,
		intent.CheckoutToken,
	)
	if err == nil {
		ticketViewURL = w.config.URLs.UserBaseURL + "/tickets/view?token=" + token
	}
	amount, _ := currency.FromSmallestUnit(intent.AmountTotal, transaction.Currency)
	metadata := utils.ResolveMoneyMetadata(transaction.Currency, "")

	templateData := map[string]interface{}{
		"customer_email":  intent.CustomerEmail,
		"user_name":       recipientName,
		"guest_name":      recipientName,
		"event_name":      event.Title,
		"event_date":      event.StartDate.Format("January 2, 2006"),
		"event_time":      event.StartDate.Format("3:04 PM"),
		"event_end_date":  eventEndDate,
		"event_end_time":  eventEndTime,
		"event_location":  event.Location,
		"venue":           venue,
		"organizer_name":  organizerName,
		"total_tickets":   intent.Quantity,
		"total_amount":    amount,
		"currency_symbol": metadata.CurrencySymbol,
		"payment_gateway": string(intent.PaymentGateway),
		"transaction_id":  transaction.ID.String(),
		"ticket_view_url": ticketViewURL,
		"is_guest":        intent.ActorType == models.ActorGuest,
		"year":            time.Now().Year(),
		"google_calendar_url": fmt.Sprintf(
			"https://calendar.google.com/calendar/render?action=TEMPLATE&text=%s&dates=%s/%s&location=%s",
			event.Title,
			event.StartDate.Format("20060102T150405Z"),
			calendarEnd.Format("20060102T150405Z"),
			event.Location,
		),
	}

	return w.emailOutboxService.QueueEmail(
		ctx,
		models.EmailEventTicketConfirmation,
		intent.CustomerEmail,
		fmt.Sprintf("Your tickets for %s", event.Title),
		templateData,
		2,
	)
}

func (w *PaymentWorker) sendPaymentFailedEmail(ctx context.Context, intent *models.PaymentIntent) error {
	if intent.CustomerEmail == "" {
		return nil
	}

	var event models.Event
	if err := w.db.WithContext(ctx).Where("id = ?", intent.EventID).First(&event).Error; err != nil {
		return err
	}
	amount, _ := currency.FromSmallestUnit(intent.AmountTotal, intent.Currency)

	templateData := map[string]interface{}{
		"customer_email": intent.CustomerEmail,
		"event_name":     event.Title,
		"event_date":     event.StartDate.Format("January 2, 2006 at 3:04 PM"),
		"total_amount":   amount,
		"failure_reason": "Payment was declined by your bank or card issuer",
	}

	return w.emailOutboxService.QueueEmail(
		ctx,
		"payment_failed",
		intent.CustomerEmail,
		fmt.Sprintf("Payment failed for %s", event.Title),
		templateData,
		1,
	)
}

func (w *PaymentWorker) sendRefundProcessedEmail(ctx context.Context, intent *models.PaymentIntent, refund *models.Refund) error {
	if intent.CustomerEmail == "" {
		return nil
	}

	var event models.Event
	if err := w.db.WithContext(ctx).Where("id = ?", intent.EventID).First(&event).Error; err != nil {
		return err
	}
	amount, _ := currency.FromSmallestUnit(intent.AmountTotal, intent.Currency)

	templateData := map[string]interface{}{
		"customer_email":   intent.CustomerEmail,
		"event_name":       event.Title,
		"refund_amount":    amount,
		"refund_reason":    refund.Reason,
		"processed_at":     refund.CreatedAt.Format("January 2, 2006 at 3:04 PM"),
		"is_full_refund":   refund.IsFullRefund,
		"refund_reference": refund.ProviderRefundID,
	}

	return w.emailOutboxService.QueueEmail(
		ctx,
		"refund_processed",
		intent.CustomerEmail,
		fmt.Sprintf("Refund processed for %s", event.Title),
		templateData,
		1,
	)
}
