package workers

import (
	"context"
	"encoding/json"
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
	txSM     *state.StateMachine[models.TransactionStatus]

	emailOutboxService *services.EmailOutboxService
	feeExtractor       *services.FeeExtractor
	refundCalculator   *services.RefundCalculator
}

func NewPaymentWorker(
	db *gorm.DB,
	webhookSecret string,
	intentSM *state.StateMachine[models.PaymentIntentStatus],
	txSM *state.StateMachine[models.TransactionStatus],
	emailOutboxService *services.EmailOutboxService,
) *PaymentWorker {
	return &PaymentWorker{
		db:                 db,
		webhookSecret:      webhookSecret,
		intentSM:           intentSM,
		txSM:               txSM,
		emailOutboxService: emailOutboxService,
		feeExtractor:       services.NewFeeExtractor(),
		refundCalculator:   services.NewRefundCalculator(),
	}
}

func (w *PaymentWorker) HandleStripeWebhook(
	ctx context.Context,
	body []byte,
	signature string,
) error {

	event, err := webhook.ConstructEvent(body, signature, w.webhookSecret)
	if err != nil {
		return err
	}

	// 🔒 Idempotency check - critical for webhook processing
	var exists models.WebhookEvent
	if err := w.db.Where("event_id = ?", event.ID).First(&exists).Error; err == nil {
		// Event already processed
		return nil
	}

	// Log webhook event
	_ = w.db.Create(&models.WebhookEvent{
		ID:             uuid.New(),
		PaymentGateway: models.PaymentGatewayStripe,
		GatewayEventID: event.ID,
		Provider:       "stripe",
		EventID:        event.ID,
		EventType:      string(event.Type),
		Payload:        models.JSONMap{"raw": string(body)},
		Status:         "processing",
		ReceivedAt:     time.Now(),
	})

	// Route to appropriate handler based on event type
	switch event.Type {
	case "checkout.session.completed":
		return w.processCheckoutSessionCompleted(ctx, event)
	case "payment_intent.succeeded":
		return w.processPaymentIntentSucceeded(ctx, event)
	case "checkout.session.expired":
		return w.processPaymentExpired(ctx, event)
	case "payment_intent.payment_failed":
		return w.processPaymentFailed(ctx, event)
	case "charge.refunded":
		return w.processRefund(ctx, event)
	default:
		// Mark as ignored for unhandled events
		return w.markWebhookProcessed(ctx, event.ID, "ignored")
	}
}

// ================================
// CHECKOUT SESSION COMPLETED PROCESSING
// ================================

// processCheckoutSessionCompleted handles successful checkout session completion
func (w *PaymentWorker) processCheckoutSessionCompleted(ctx context.Context, event stripe.Event) error {
	var session stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
		return w.markWebhookFailed(ctx, event.ID, err)
	}

	// Extract checkout token from Stripe metadata to look up our PaymentIntent
	var checkoutToken string
	if session.Metadata != nil {
		checkoutToken = session.Metadata["checkout_token"]
	}
	if checkoutToken == "" {
		return w.markWebhookFailed(ctx, event.ID, fmt.Errorf("missing checkout_token in session metadata"))
	}

	return w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Lock and validate payment intent using checkout token
		intent, err := w.lockPaymentIntent(tx, checkoutToken)
		if err != nil {
			return err
		}

		// 2. Validate state transition
		if err := w.validatePaymentIntentTransition(intent.Status, models.PaymentIntentSucceeded); err != nil {
			return err
		}

		// Skip if already processed
		if intent.Status == models.PaymentIntentSucceeded {
			return w.markWebhookProcessed(ctx, event.ID, "processed")
		}

		// 3. Create transaction record
		transaction, err := w.createTransaction(tx, intent, string(event.Type), session)
		if err != nil {
			return err
		}

		// 4. Update payment attempt with provider information
		if session.PaymentIntent != nil {
			if err := w.updatePaymentAttemptWithProviderData(tx, intent.ID, session.PaymentIntent.ID, session.PaymentIntent.ID); err != nil {
				return err
			}
		}

		// 5. Update payment attempt status
		if err := w.updatePaymentAttemptAuthorized(tx, intent.ID); err != nil {
			return err
		}

		// 5. Activate tickets
		if err := w.activateTickets(tx, transaction.ID); err != nil {
			return err
		}

		// 6. Confirm reservations
		if err := w.confirmReservations(tx, intent.ID); err != nil {
			return err
		}

		// 7. Update payment intent status
		if err := w.updatePaymentIntentSucceeded(tx, intent); err != nil {
			return err
		}

		// 8. Send success email notification
		if err := w.sendPurchaseSuccessEmail(ctx, intent); err != nil {
			// Log error but don't fail the transaction
			fmt.Printf("Failed to send success email: %v\n", err)
		}

		// 9. Mark webhook as processed
		return w.markWebhookProcessed(ctx, event.ID, "processed")
	})
}

// ================================
// PAYMENT INTENT SUCCEEDED PROCESSING
// ================================

// processPaymentIntentSucceeded handles payment intent succeeded events
func (w *PaymentWorker) processPaymentIntentSucceeded(ctx context.Context, event stripe.Event) error {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
		return w.markWebhookFailed(ctx, event.ID, err)
	}

	// Extract checkout token from PaymentIntent metadata to look up our PaymentIntent
	var checkoutToken string
	if pi.Metadata != nil {
		checkoutToken = pi.Metadata["checkout_token"]
	}
	if checkoutToken == "" {
		return w.markWebhookFailed(ctx, event.ID, fmt.Errorf("missing checkout_token in payment intent metadata"))
	}

	return w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Lock and validate payment intent using checkout token
		intent, err := w.lockPaymentIntent(tx, checkoutToken)
		if err != nil {
			return err
		}

		// 2. Validate state transition
		if err := w.validatePaymentIntentTransition(intent.Status, models.PaymentIntentSucceeded); err != nil {
			return err
		}

		// Skip if already processed
		if intent.Status == models.PaymentIntentSucceeded {
			return w.markWebhookProcessed(ctx, event.ID, "processed")
		}

		// 3. Create transaction record
		transaction, err := w.createTransaction(tx, intent, string(event.Type), pi)
		if err != nil {
			return err
		}

		// 4. Update payment attempt with provider information and status
		if err := w.updatePaymentAttemptWithProviderData(tx, intent.ID, pi.ID, pi.LatestCharge.ID); err != nil {
			return err
		}

		// 5. Update payment attempt status
		if err := w.updatePaymentAttemptAuthorized(tx, intent.ID); err != nil {
			return err
		}

		// 5. Activate tickets
		if err := w.activateTickets(tx, transaction.ID); err != nil {
			return err
		}

		// 6. Confirm reservations
		if err := w.confirmReservations(tx, intent.ID); err != nil {
			return err
		}

		// 7. Update payment intent status
		if err := w.updatePaymentIntentSucceeded(tx, intent); err != nil {
			return err
		}

		// 8. Send success email notification
		if err := w.sendPurchaseSuccessEmail(ctx, intent); err != nil {
			// Log error but don't fail the transaction
			fmt.Printf("Failed to send success email: %v\n", err)
		}

		// 9. Mark webhook as processed
		return w.markWebhookProcessed(ctx, event.ID, "processed")
	})
}

// ================================
// PAYMENT EXPIRED PROCESSING
// ================================

// processPaymentExpired handles expired checkout sessions
func (w *PaymentWorker) processPaymentExpired(ctx context.Context, event stripe.Event) error {
	var session stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
		return w.markWebhookFailed(ctx, event.ID, err)
	}

	token := session.Metadata["checkout_token"]
	if token == "" {
		return w.markWebhookFailed(ctx, event.ID, fmt.Errorf("missing checkout_token in metadata"))
	}

	return w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Lock and validate payment intent
		intent, err := w.lockPaymentIntent(tx, token)
		if err != nil {
			return err
		}

		// 2. Validate state transition
		if err := w.validatePaymentIntentTransition(intent.Status, models.PaymentIntentExpired); err != nil {
			return err
		}

		// 3. Update payment attempt status to failed
		if err := w.updatePaymentAttemptFailed(tx, intent.ID); err != nil {
			return err
		}

		// 4. Release reservations
		if err := w.releaseReservations(tx, intent.ID); err != nil {
			return err
		}

		// 5. Cancel tickets
		if err := w.cancelPendingTickets(tx, intent.ID); err != nil {
			return err
		}

		// 6. Update payment intent status
		if err := w.updatePaymentIntentExpired(tx, intent); err != nil {
			return err
		}

		// 6. Send payment failed email notification
		if err := w.sendPaymentFailedEmail(ctx, intent); err != nil {
			// Log error but don't fail the transaction
			fmt.Printf("Failed to send expired payment email: %v\n", err)
		}

		// 7. Mark webhook as processed
		return w.markWebhookProcessed(ctx, event.ID, "processed")
	})
}

// ================================
// PAYMENT FAILED PROCESSING
// ================================

// processPaymentFailed handles failed payment intents
func (w *PaymentWorker) processPaymentFailed(ctx context.Context, event stripe.Event) error {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
		return w.markWebhookFailed(ctx, event.ID, err)
	}

	token := pi.Metadata["checkout_token"]
	if token == "" {
		return w.markWebhookFailed(ctx, event.ID, fmt.Errorf("missing checkout_token in metadata"))
	}

	return w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Lock and validate payment intent
		intent, err := w.lockPaymentIntent(tx, token)
		if err != nil {
			return err
		}

		// 2. Validate state transition
		if err := w.validatePaymentIntentTransition(intent.Status, models.PaymentIntentCanceled); err != nil {
			return err
		}

		// 3. Update payment attempt status to failed
		if err := w.updatePaymentAttemptFailed(tx, intent.ID); err != nil {
			return err
		}

		// 4. Release reservations
		if err := w.releaseReservations(tx, intent.ID); err != nil {
			return err
		}

		// 5. Cancel tickets
		if err := w.cancelPendingTickets(tx, intent.ID); err != nil {
			return err
		}

		// 6. Update payment intent status
		if err := w.updatePaymentIntentCanceled(tx, intent); err != nil {
			return err
		}

		// 6. Send payment failed email notification
		if err := w.sendPaymentFailedEmail(ctx, intent); err != nil {
			// Log error but don't fail the transaction
			fmt.Printf("Failed to send failed payment email: %v\n", err)
		}

		// 7. Mark webhook as processed
		return w.markWebhookProcessed(ctx, event.ID, "processed")
	})
}

// ================================
// REFUND PROCESSING
// ================================

// processRefund handles refund webhooks
func (w *PaymentWorker) processRefund(ctx context.Context, event stripe.Event) error {
	var charge stripe.Charge
	if err := json.Unmarshal(event.Data.Raw, &charge); err != nil {
		return w.markWebhookFailed(ctx, event.ID, err)
	}

	return w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Lock and validate refund
		refund, err := w.lockRefundByProviderID(tx, charge.ID)
		if err != nil {
			return err
		}

		// 2. Validate state transition
		if err := w.validateRefundTransition(refund.Status, models.RefundSucceeded); err != nil {
			return err
		}

		// 3. Update refund status
		if err := w.updateRefundSucceeded(tx, refund); err != nil {
			return err
		}

		// 4. Update ticket refund status
		if err := w.updateTicketRefundStatus(tx, refund); err != nil {
			return err
		}

		// 5. Restore inventory
		if err := w.restoreInventory(tx, refund); err != nil {
			return err
		}

		// 6. Update transaction refund status
		if err := w.updateTransactionRefundStatus(tx, refund.TransactionID); err != nil {
			return err
		}

		// 7. Send refund processed email notification
		var intent models.PaymentIntent
		if err := tx.Where("id = ?", refund.PaymentIntentID).First(&intent).Error; err == nil {
			if err := w.sendRefundProcessedEmail(ctx, &intent, refund); err != nil {
				// Log error but don't fail the transaction
				fmt.Printf("Failed to send refund email: %v\n", err)
			}
		}

		// 8. Mark webhook as processed
		return w.markWebhookProcessed(ctx, event.ID, "processed")
	})
}

// ================================
// HELPER FUNCTIONS
// ================================

// lockPaymentIntent locks and retrieves payment intent for update
func (w *PaymentWorker) lockPaymentIntent(tx *gorm.DB, checkoutToken string) (*models.PaymentIntent, error) {
	var intent models.PaymentIntent
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("checkout_token = ?", checkoutToken).
		First(&intent).Error; err != nil {
		return nil, err
	}
	return &intent, nil
}

// lockRefundByProviderID locks and retrieves refund by provider refund ID
func (w *PaymentWorker) lockRefundByProviderID(tx *gorm.DB, providerRefundID string) (*models.Refund, error) {
	var refund models.Refund
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("provider_refund_id = ?", providerRefundID).
		First(&refund).Error; err != nil {
		return nil, err
	}
	return &refund, nil
}

// validatePaymentIntentTransition validates state machine transition
// validateRefundTransition validates refund state machine transition
func (w *PaymentWorker) validateRefundTransition(current, target models.RefundStatus) error {
	refundSM := state.NewStateMachine(state.RefundTransitions)
	return refundSM.Transition(current, target)
}

// createTransaction creates a comprehensive transaction record with all financial calculations
func (w *PaymentWorker) createTransaction(tx *gorm.DB, intent *models.PaymentIntent, eventType string, eventData interface{}) (*models.Transaction, error) {
	// Get event details for commission calculation
	var event models.Event
	if err := tx.Where("id = ?", intent.EventID).First(&event).Error; err != nil {
		return nil, fmt.Errorf("failed to get event: %w", err)
	}

	// Get the latest payment attempt
	var attempt models.PaymentAttempt
	if err := tx.Where("payment_intent_id = ?", intent.ID).
		Order("created_at DESC").First(&attempt).Error; err != nil {
		return nil, fmt.Errorf("failed to get payment attempt: %w", err)
	}

	// Get tickets for this transaction
	var tickets []models.Ticket
	if err := tx.Where("payment_intent_id = ? AND status = ?", intent.ID, models.TicketActive).
		Find(&tickets).Error; err != nil {
		return nil, fmt.Errorf("failed to get tickets: %w", err)
	}

	// Extract ticket IDs
	ticketIDs := make([]string, len(tickets))
	for i, ticket := range tickets {
		ticketIDs[i] = ticket.ID.String()
	}

	// Extract actual gateway fees from webhook data
	var gatewayFee int64
	feeSnapshot, err := w.feeExtractor.ExtractFromWebhook(eventType, eventData)
	if err != nil {
		return nil, fmt.Errorf("failed to extract fees: %w", err)
	}

	if feeSnapshot != nil {
		// Use actual fees from webhook
		gatewayFee = feeSnapshot.GatewayFee
	} else {
		// Fallback to estimation if webhook doesn't contain fee data
		gatewayFee, err = w.feeExtractor.EstimateGatewayFee(intent.AmountTotal, intent.Currency, intent.PaymentGateway)
		if err != nil {
			return nil, fmt.Errorf("failed to estimate gateway fee: %w", err)
		}
	}

	// Calculate platform fee (commission)
	platformFee, err := w.feeExtractor.CalculatePlatformFee(intent.AmountTotal, intent.Currency, event.CommissionRate)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate platform fee: %w", err)
	}

	// Organizer earning = total - platform fee - gateway fee
	organizerEarning := intent.AmountTotal - platformFee - gatewayFee

	// Extract provider information from webhook data
	var providerChargeID, providerTxnID string
	var paymentGateway models.PaymentGateway = models.PaymentGatewayStripe

	switch e := eventData.(type) {
	case stripe.CheckoutSession:
		providerChargeID = e.PaymentIntent.ID
		providerTxnID = e.ID
	case stripe.PaymentIntent:
		providerChargeID = e.ID
		providerTxnID = e.ID
	}

	// Validate transaction status transition
	if err := w.validateTransactionTransition(models.TransactionPending, models.TransactionSucceeded); err != nil {
		return nil, fmt.Errorf("invalid transaction status transition: %w", err)
	}

	transaction := &models.Transaction{
		ID:               uuid.New(),
		PaymentIntentID:  intent.ID,
		PaymentAttemptID: attempt.ID,
		EventID:          intent.EventID,
		ActorID:          intent.ActorID,
		ActorType:        intent.ActorType,

		// Gateway information
		ProviderChargeID: providerChargeID,
		PaymentGateway:   paymentGateway,
		ProviderTxnID:    providerTxnID,

		// Financial data
		AmountTotal: intent.AmountTotal,
		Currency:    intent.Currency,

		// Fee breakdown
		PlatformFee:      platformFee,
		GatewayFee:       gatewayFee,
		OrganizerEarning: organizerEarning,

		// Ticket information
		Quantity:  len(tickets),
		TicketIDs: ticketIDs,

		// Status
		Status: models.TransactionSucceeded,

		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := tx.Create(transaction).Error; err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	return transaction, nil
}

// updatePaymentAttemptAuthorized updates payment attempt to authorized status
func (w *PaymentWorker) updatePaymentAttemptAuthorized(tx *gorm.DB, paymentIntentID uuid.UUID) error {
	now := time.Now()
	return tx.Model(&models.PaymentAttempt{}).
		Where("payment_intent_id = ?", paymentIntentID).
		Updates(map[string]any{
			"status":      models.PaymentAttemptAuthorized,
			"captured_at": &now,
		}).Error
}

// updatePaymentAttemptFailed updates payment attempt to failed status
func (w *PaymentWorker) updatePaymentAttemptFailed(tx *gorm.DB, paymentIntentID uuid.UUID) error {
	return tx.Model(&models.PaymentAttempt{}).
		Where("payment_intent_id = ?", paymentIntentID).
		Update("status", models.PaymentAttemptFailed).Error
}

// updatePaymentAttemptWithProviderData updates payment attempt with Stripe provider information
func (w *PaymentWorker) updatePaymentAttemptWithProviderData(tx *gorm.DB, paymentIntentID uuid.UUID, providerReferenceID, providerChargeID string) error {
	return tx.Model(&models.PaymentAttempt{}).
		Where("payment_intent_id = ?", paymentIntentID).
		Updates(map[string]any{
			"provider_reference_id": providerReferenceID,
			"provider_charge_id":    providerChargeID,
		}).Error
}

// activateTickets activates pending tickets to active status
func (w *PaymentWorker) activateTickets(tx *gorm.DB, transactionID uuid.UUID) error {
	now := time.Now()
	return tx.Model(&models.Ticket{}).
		Where("transaction_id = ? AND status = ?", transactionID, "pending_payment").
		Updates(map[string]any{
			"status":  models.TicketActive,
			"paid_at": &now,
		}).Error
}

// confirmReservations confirms ticket reservations
func (w *PaymentWorker) confirmReservations(tx *gorm.DB, paymentIntentID uuid.UUID) error {
	return tx.Model(&models.TicketReservation{}).
		Where("payment_intent_id = ?", paymentIntentID).
		Update("status", "confirmed").Error
}

// releaseReservations releases ticket reservations back to available
func (w *PaymentWorker) releaseReservations(tx *gorm.DB, paymentIntentID uuid.UUID) error {
	return tx.Model(&models.TicketReservation{}).
		Where("payment_intent_id = ?", paymentIntentID).
		Update("status", "released").Error
}

// cancelPendingTickets cancels pending tickets
func (w *PaymentWorker) cancelPendingTickets(tx *gorm.DB, paymentIntentID uuid.UUID) error {
	return tx.Model(&models.Ticket{}).
		Where("payment_intent_id = ? AND status = ?", paymentIntentID, "pending_payment").
		Update("status", models.TicketCanceled).Error
}

// updatePaymentIntentSucceeded updates payment intent to succeeded
func (w *PaymentWorker) updatePaymentIntentSucceeded(tx *gorm.DB, intent *models.PaymentIntent) error {
	return tx.Model(intent).Updates(map[string]any{
		"status":       models.PaymentIntentSucceeded,
		"succeeded_at": time.Now(),
	}).Error
}

// updatePaymentIntentExpired updates payment intent to expired
func (w *PaymentWorker) updatePaymentIntentExpired(tx *gorm.DB, intent *models.PaymentIntent) error {
	return tx.Model(intent).Update("status", models.PaymentIntentExpired).Error
}

// updatePaymentIntentCanceled updates payment intent to canceled
func (w *PaymentWorker) updatePaymentIntentCanceled(tx *gorm.DB, intent *models.PaymentIntent) error {
	return tx.Model(intent).Update("status", models.PaymentIntentCanceled).Error
}

// updateRefundSucceeded updates refund to succeeded status
func (w *PaymentWorker) updateRefundSucceeded(tx *gorm.DB, refund *models.Refund) error {
	return tx.Model(refund).Updates(map[string]any{
		"status":     models.RefundSucceeded,
		"updated_at": time.Now(),
	}).Error
}

// updateTicketRefundStatus updates ticket refund status for refunded tickets
func (w *PaymentWorker) updateTicketRefundStatus(tx *gorm.DB, refund *models.Refund) error {
	now := time.Now()
	refundStatus := models.TicketRefundFull
	ticketStatus := models.TicketActive // Keep active for partial refunds

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
			"refund_type":   refund.Type,
		}

		if refund.IsFullRefund {
			updates["status"] = ticketStatus
		}

		if err := tx.Model(&models.Ticket{}).
			Where("id = ?", ticketID).
			Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

// restoreInventory restores inventory for refunded tickets
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

// updateTransactionRefundStatus updates transaction to refunded status
func (w *PaymentWorker) updateTransactionRefundStatus(tx *gorm.DB, transactionID uuid.UUID) error {
	return tx.Model(&models.Transaction{}).
		Where("id = ?", transactionID).
		Update("status", models.TransactionRefunded).Error
}

// validatePaymentIntentTransition validates payment intent status transitions using state machine
func (w *PaymentWorker) validatePaymentIntentTransition(from, to models.PaymentIntentStatus) error {
	if !w.intentSM.Can(from, to) {
		return fmt.Errorf("invalid payment intent transition %v → %v", from, to)
	}
	return nil
}

// validateTransactionTransition validates transaction status transitions using state machine
func (w *PaymentWorker) validateTransactionTransition(from, to models.TransactionStatus) error {
	if !w.txSM.Can(from, to) {
		return fmt.Errorf("invalid transaction transition %v → %v", from, to)
	}
	return nil
}

// markWebhookFailed marks webhook event as failed with error
func (w *PaymentWorker) markWebhookFailed(ctx context.Context, eventID string, err error) error {
	return w.db.WithContext(ctx).Model(&models.WebhookEvent{}).
		Where("event_id = ?", eventID).
		Updates(map[string]any{
			"status":     "failed",
			"last_error": err.Error(),
		}).Error
}

// markWebhookProcessed marks webhook event as processed
func (w *PaymentWorker) markWebhookProcessed(ctx context.Context, eventID string, status string) error {
	return w.db.WithContext(ctx).Model(&models.WebhookEvent{}).
		Where("event_id = ?", eventID).
		Update("status", status).Error
}

// ================================
// EMAIL NOTIFICATION FUNCTIONS
// ================================

// sendPurchaseSuccessEmail sends email notification for successful ticket purchase
func (w *PaymentWorker) sendPurchaseSuccessEmail(ctx context.Context, intent *models.PaymentIntent) error {
	if intent.CustomerEmail == "" {
		return nil // Skip if no email
	}

	// Get transaction and tickets for email data
	var transaction models.Transaction
	if err := w.db.WithContext(ctx).Where("payment_intent_id = ?", intent.ID).First(&transaction).Error; err != nil {
		return err
	}

	var tickets []models.Ticket
	if err := w.db.WithContext(ctx).Where("transaction_id = ?", transaction.ID).Find(&tickets).Error; err != nil {
		return err
	}

	// Get event details
	var event models.Event
	if err := w.db.WithContext(ctx).Where("id = ?", intent.EventID).First(&event).Error; err != nil {
		return err
	}

	// Prepare email template data
	templateData := map[string]interface{}{
		"customer_email": intent.CustomerEmail,
		"event_name":     event.Title,
		"event_date":     event.StartDate.Format("January 2, 2006 at 3:04 PM"),
		"event_location": event.Location,
		"tickets":        tickets,
		"total_amount":   fmt.Sprintf("$%.2f", float64(intent.AmountTotal)/100),
		"transaction_id": transaction.ID.String(),
	}

	// Generate ticket view URLs
	var ticketURLs []string
	for _, ticket := range tickets {
		ticketURLs = append(ticketURLs, fmt.Sprintf("https://user.timroticket.com/tickets/%s", ticket.ID.String()))
	}
	templateData["ticket_urls"] = ticketURLs

	// Add Google Calendar link
	templateData["google_calendar_url"] = fmt.Sprintf(
		"https://calendar.google.com/calendar/render?action=TEMPLATE&text=%s&dates=%s/%s&location=%s",
		event.Title,
		event.StartDate.Format("20060102T150405Z"),
		event.EndDate.Format("20060102T150405Z"),
		event.Location,
	)

	return w.emailOutboxService.QueueEmail(
		ctx,
		"ticket_purchase_success",
		intent.CustomerEmail,
		fmt.Sprintf("Your tickets for %s", event.Title),
		templateData,
		1, // High priority
	)
}

// sendPaymentFailedEmail sends email notification for failed payment
func (w *PaymentWorker) sendPaymentFailedEmail(ctx context.Context, intent *models.PaymentIntent) error {
	if intent.CustomerEmail == "" {
		return nil // Skip if no email
	}

	// Get event details
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
		1, // High priority
	)
}

// sendRefundProcessedEmail sends email notification for processed refund
func (w *PaymentWorker) sendRefundProcessedEmail(ctx context.Context, intent *models.PaymentIntent, refund *models.Refund) error {
	if intent.CustomerEmail == "" {
		return nil // Skip if no email
	}

	// Get event details
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
		1, // High priority
	)
}
