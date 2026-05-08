package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/webhook"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/internal/state"
)

type PaymentWorker struct {
	db            *gorm.DB
	webhookSecret string

	intentSM *state.StateMachine[models.PaymentIntentStatus]
	paSM     *state.StateMachine[models.PaymentAttemptStatus]
	txSM     *state.StateMachine[models.TransactionStatus]

	emailOutboxService *services.EmailOutboxService
	ticketService      *services.TicketService
	feeExtractor       *services.FeeExtractor
	refundCalculator   *services.RefundCalculator
}

func NewPaymentWorker(
	db *gorm.DB,
	webhookSecret string,
	intentSM *state.StateMachine[models.PaymentIntentStatus],
	txSM *state.StateMachine[models.TransactionStatus],
	emailOutboxService *services.EmailOutboxService,
	ticketService *services.TicketService,
) *PaymentWorker {
	return &PaymentWorker{
		db:                 db,
		webhookSecret:      webhookSecret,
		intentSM:           intentSM,
		paSM:               state.NewStateMachine(state.PaymentAttemptTransitions),
		txSM:               txSM,
		emailOutboxService: emailOutboxService,
		ticketService:      ticketService,
		feeExtractor:       services.NewFeeExtractor(),
		refundCalculator:   services.NewRefundCalculator(),
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

	// Idempotency check — skip already-processed events
	var exists models.WebhookEvent
	if err := w.db.Where("gateway_event_id = ?", event.ID).First(&exists).Error; err == nil {
		fmt.Printf("[WEBHOOK] %s already processed, skipping\n", event.ID)
		return nil
	}

	// Persist webhook event before processing
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

	switch event.Type {
	case "payment_intent.succeeded":
		return w.processPaymentIntentSucceeded(ctx, event)
	case "payment_intent.payment_failed":
		return w.processPaymentFailed(ctx, event)
	case "checkout.session.completed":
		return w.processCheckoutSessionCompleted(ctx, event)
	case "checkout.session.expired":
		return w.processPaymentExpired(ctx, event)
	case "charge.refunded":
		return w.processRefund(ctx, event)
	default:
		return w.markWebhookProcessed(ctx, event.ID, "ignored")
	}
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
	return w.markWebhookProcessed(ctx, event.ID, "processed")
}

func (w *PaymentWorker) processPaymentIntentSucceeded(ctx context.Context, event stripe.Event) error {
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
		transaction, err := w.createTransaction(tx, &intent, "payment_intent.succeeded", pi)
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
		if err := w.updatePaymentIntentSucceededAt(tx, intent.ID); err != nil {
			return fmt.Errorf("failed to update payment intent succeeded at: %w", err)
		}
		if err := w.updatePaymentAttemptCompletedAt(tx, transaction.PaymentAttemptID); err != nil {
			return fmt.Errorf("failed to update payment attempt completed at: %w", err)
		}

		if err := w.validateTransactionTransition(models.TransactionProcessing, models.TransactionSucceeded); err != nil {
			return fmt.Errorf("invalid transaction status transition: %w", err)
		}

		if err := w.validatePaymentAttemptTransition(models.PaymentAttemptInitiated, models.PaymentAttemptAuthorized); err != nil {
			return fmt.Errorf("invalid payment attempt status transition: %w", err)
		}

		if err := w.sendPurchaseSuccessEmail(ctx, &intent); err != nil {
			fmt.Printf("failed to send purchase success email: %v\n", err)
		}

		// 6. Mark webhook processed
		return w.markWebhookProcessed(ctx, event.ID, "processed")
	})
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
		if err := w.updatePaymentIntentFailed(tx, intent.ID); err != nil {
			return fmt.Errorf("failed to update payment intent failed status: %w", err)
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
	organizerEarning := intent.AmountTotal - platformFee - gatewayFee

	// Extract provider charge ID from the webhook payload
	var providerChargeID string
	switch e := eventData.(type) {
	case stripe.CheckoutSession:
		if e.PaymentIntent != nil {
			providerChargeID = e.PaymentIntent.ID
		}
	case stripe.PaymentIntent:
		providerChargeID = e.ID
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
		OrganizerEarning: organizerEarning,
		Quantity:         intent.Quantity,
		IsPaidOut:        false,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := tx.Create(transaction).Error; err != nil {
		return nil, fmt.Errorf("failed creating transaction: %w", err)
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
	refundStatus := models.TicketRefundFull
	ticketStatus := models.TicketActive // stays active for partial refunds

	if refund.IsFullRefund {
		refundStatus = models.TicketRefundFull
		ticketStatus = models.TicketRefunded
	} else {
		refundStatus = models.TicketRefundPartial
	}

	for _, ticketIDStr := range refund.AffectedTicketIDs {
		ticketID, err := uuid.Parse(ticketIDStr)
		if err != nil {
			continue
		}

		updates := map[string]any{
			"refund_status": refundStatus,
			"refund_id":     &refund.ID,
			"refunded_at":   &now,
			"refund_amount": refund.Amount,
		}
		if refund.IsFullRefund {
			updates["status"] = ticketStatus
		}

		if err := tx.Model(&models.Ticket{}).Where("id = ?", ticketID).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func (w *PaymentWorker) restoreInventory(tx *gorm.DB, refund *models.Refund) error {
	for _, ticketIDStr := range refund.AffectedTicketIDs {
		ticketID, err := uuid.Parse(ticketIDStr)
		if err != nil {
			continue
		}

		var ticket models.Ticket
		if err := tx.Where("id = ?", ticketID).First(&ticket).Error; err != nil {
			continue
		}

		if err := tx.Exec(`
			UPDATE event_tiers
			SET available_quantity = available_quantity + 1
			WHERE id = ?
		`, ticket.TierID).Error; err != nil {
			return err
		}
	}
	return nil
}

func (w *PaymentWorker) updateTransactionRefundStatus(tx *gorm.DB, transactionID uuid.UUID) error {
	return tx.Model(&models.Transaction{}).
		Where("id = ?", transactionID).
		Update("status", models.TransactionRefunded).Error
}

// update succeeded_at when payment is successful
func (w *PaymentWorker) updatePaymentIntentSucceededAt(tx *gorm.DB, paymentIntentID uuid.UUID) error {
	return tx.Model(&models.PaymentIntent{}).
		Where("id = ?", paymentIntentID).
		Updates(map[string]any{
			"status":       models.PaymentIntentSucceeded,
			"succeeded_at": time.Now(),
			"updated_at":   time.Now(),
		}).Error
}

// update completed_at when payment is successful
func (w *PaymentWorker) updatePaymentAttemptCompletedAt(tx *gorm.DB, paymentAttemptID uuid.UUID) error {
	return tx.Model(&models.PaymentAttempt{}).
		Where("id = ?", paymentAttemptID).
		Update("completed_at", time.Now()).Error
}

// update failed payment intent status and timestamp
func (w *PaymentWorker) updatePaymentIntentFailed(tx *gorm.DB, paymentIntentID uuid.UUID) error {
	return tx.Model(&models.PaymentIntent{}).
		Where("id = ?", paymentIntentID).
		Updates(map[string]any{
			"status":      models.PaymentIntentFailed,
			"canceled_at": time.Now(),
			"updated_at":  time.Now(),
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

	var ticketURLs []string
	for _, ticket := range tickets {
		ticketURLs = append(ticketURLs, fmt.Sprintf("https://user.timroticket.com/tickets/%s", ticket.ID))
	}

	templateData := map[string]interface{}{
		"customer_email": intent.CustomerEmail,
		"event_name":     event.Title,
		"event_date":     event.StartDate.Format("January 2, 2006 at 3:04 PM"),
		"event_location": event.Location,
		"tickets":        tickets,
		"total_amount":   fmt.Sprintf("$%.2f", float64(intent.AmountTotal)/100),
		"transaction_id": transaction.ID.String(),
		"ticket_urls":    ticketURLs,
		"google_calendar_url": fmt.Sprintf(
			"https://calendar.google.com/calendar/render?action=TEMPLATE&text=%s&dates=%s/%s&location=%s",
			event.Title,
			event.StartDate.Format("20060102T150405Z"),
			event.EndDate.Format("20060102T150405Z"),
			event.Location,
		),
	}

	return w.emailOutboxService.QueueEmail(
		ctx,
		"ticket_purchase_success",
		intent.CustomerEmail,
		fmt.Sprintf("Your tickets for %s", event.Title),
		templateData,
		1,
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

	templateData := map[string]interface{}{
		"customer_email": intent.CustomerEmail,
		"event_name":     event.Title,
		"event_date":     event.StartDate.Format("January 2, 2006 at 3:04 PM"),
		"total_amount":   fmt.Sprintf("$%.2f", float64(intent.AmountTotal)/100),
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

	templateData := map[string]interface{}{
		"customer_email":   intent.CustomerEmail,
		"event_name":       event.Title,
		"refund_amount":    fmt.Sprintf("$%.2f", float64(refund.Amount)/100),
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
