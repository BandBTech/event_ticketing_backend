package workers

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/webhook"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/state"
)

type PaymentWorker struct {
	db            *gorm.DB
	webhookSecret string

	intentSM *state.StateMachine[models.PaymentIntentStatus]
	txSM     *state.StateMachine[models.TransactionStatus]
}

func NewPaymentWorker(
	db *gorm.DB,
	webhookSecret string,
	intentSM *state.StateMachine[models.PaymentIntentStatus],
	txSM *state.StateMachine[models.TransactionStatus],
) *PaymentWorker {
	return &PaymentWorker{
		db:            db,
		webhookSecret: webhookSecret,
		intentSM:      intentSM,
		txSM:          txSM,
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

	// 🔒 idempotency (critical)
	var exists models.WebhookEvent
	if err := w.db.Where("event_id = ?", event.ID).First(&exists).Error; err == nil {
		return nil
	}

	_ = w.db.Create(&models.WebhookEvent{
		ID:         uuid.New(),
		EventID:    event.ID,
		EventType:  string(event.Type),
		Payload:    models.JSONMap{"raw": string(body)},
		Status:     "pending",
		ReceivedAt: time.Now(),
	})

	switch event.Type {

	case "checkout.session.completed":
		return w.handleSuccess(ctx, event)

	case "checkout.session.expired":
		return w.handleExpired(ctx, event)

	case "payment_intent.payment_failed":
		return w.handleFailed(ctx, event)
	}

	return nil
}

func (w *PaymentWorker) handleSuccess(
	ctx context.Context,
	event stripe.Event,
) error {

	var session stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
		return err
	}

	token := session.Metadata["checkout_token"]

	return w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

		var intent models.PaymentIntent

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("checkout_token = ?", token).
			First(&intent).Error; err != nil {
			return err
		}

		// 🔐 STATE MACHINE
		if err := w.intentSM.Transition(
			intent.Status,
			models.PaymentIntentSucceeded,
		); err != nil {
			return err
		}

		if intent.Status == models.PaymentIntentSucceeded {
			return nil
		}

		// transaction
		transaction := models.Transaction{
			ID:              uuid.New(),
			PaymentIntentID: intent.ID,
			EventID:         intent.EventID,
			ActorID:         intent.ActorID,
			ActorType:       intent.ActorType,
			AmountTotal:     intent.AmountTotal,
			Currency:        intent.Currency,
			Status:          models.TransactionSucceeded,
			CreatedAt:       time.Now(),
		}

		if err := tx.Create(&transaction).Error; err != nil {
			return err
		}

		// update intent
		return tx.Model(&intent).Updates(map[string]any{
			"status":       models.PaymentIntentSucceeded,
			"succeeded_at": time.Now(),
		}).Error
	})
}

func (w *PaymentWorker) handleFailed(
	ctx context.Context,
	event stripe.Event,
) error {

	var pi stripe.PaymentIntent
	_ = json.Unmarshal(event.Data.Raw, &pi)

	token := pi.Metadata["checkout_token"]

	return w.db.Transaction(func(tx *gorm.DB) error {

		var intent models.PaymentIntent
		tx.Where("checkout_token = ?", token).First(&intent)

		_ = w.intentSM.Transition(
			intent.Status,
			models.PaymentIntentCanceled,
		)

		return tx.Model(&intent).
			Update("status", models.PaymentIntentCanceled).Error
	})
}

func (w *PaymentWorker) handleExpired(
	ctx context.Context,
	event stripe.Event,
) error {

	var session stripe.CheckoutSession
	_ = json.Unmarshal(event.Data.Raw, &session)

	token := session.Metadata["checkout_token"]

	return w.db.Transaction(func(tx *gorm.DB) error {

		var intent models.PaymentIntent
		tx.Where("checkout_token = ?", token).First(&intent)

		_ = w.intentSM.Transition(
			intent.Status,
			models.PaymentIntentExpired,
		)

		return tx.Model(&intent).
			Update("status", models.PaymentIntentExpired).Error
	})
}
