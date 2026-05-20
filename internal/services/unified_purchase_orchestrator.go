package services

// PurchaseOrchestrator is the SINGLE entry point for all ticket purchases.
// It handles both guests and logged-in users, all gateways, multi-tier.
//
// Callers:
//   handler/checkout.go → Orchestrator.Checkout()
//
// Flow (happy path):
//  1. Validate input.
//  2. Resolve actor (find/create GuestUser, or load User).
//  3. Idempotency check — return existing intent if key already seen.
//  4. DB transaction: lock event → reserve inventory → create PaymentIntent.
//  5. Commit.
//  6. Init gateway session (outside transaction).
//  7. Persist session ID to PaymentAttempt.
//  8. Return checkout token + redirect URL.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"event-ticketing-backend/internal/gateways"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/currency"
	"event-ticketing-backend/pkg/types"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ─────────────────────────────────────────────────────────────────────────────
// Request / Response
// ─────────────────────────────────────────────────────────────────────────────

// CheckoutRequest is the unified input from the HTTP handler.
// The handler populates ActorID + ActorType from JWT (user) or
// from find-or-create logic (guest).
type CheckoutRequest struct {
	ActorID   uuid.UUID
	ActorType models.ActorType

	EventID        uuid.UUID
	Tiers          []types.TierSelection
	Currency       string
	PaymentGateway models.PaymentGateway // "stripe" | "konbini"

	CustomerEmail string
	Timezone      string

	// IdempotencyKey comes from the X-Idempotency-Key header.
	// If empty, one is derived deterministically (weaker guarantee).
	IdempotencyKey string
}

// CheckoutResponse is returned to the HTTP handler and then to the client.
type CheckoutResponse struct {
	CheckoutToken    string    `json:"checkout_token"`
	AmountTotal      int64     `json:"amount_total"`
	Currency         string    `json:"currency"`
	ExpiresAt        time.Time `json:"expires_at"`
	RedirectURL      string    `json:"redirect_url"`
	GatewaySessionID string    `json:"gateway_session_id"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Orchestrator
// ─────────────────────────────────────────────────────────────────────────────

type PurchaseOrchestrator struct {
	db          *gorm.DB
	reservation *ReservationService
	gwRegistry  *gateways.Registry
	successURL  string
	cancelURL   string
}

func NewPurchaseOrchestrator(
	db *gorm.DB,
	reservation *ReservationService,
	gwRegistry *gateways.Registry,
	successURL, cancelURL string,
) *PurchaseOrchestrator {
	return &PurchaseOrchestrator{
		db:          db,
		reservation: reservation,
		gwRegistry:  gwRegistry,
		successURL:  successURL,
		cancelURL:   cancelURL,
	}
}

// Checkout is the single public method. Call it for guests and users alike.
func (o *PurchaseOrchestrator) Checkout(ctx context.Context, req *CheckoutRequest) (*CheckoutResponse, error) {

	if err := o.validate(req); err != nil {
		return nil, err
	}

	// ─────────────────────────────────────────────
	// 1. IDEMPOTENCY CHECK
	// ─────────────────────────────────────────────
	var existing models.PaymentIntent

	err := o.db.WithContext(ctx).
		Where("actor_id = ? AND event_id = ? AND idempotency_key = ?",
			req.ActorID, req.EventID, req.IdempotencyKey).
		First(&existing).Error

	if err == nil {
		if existing.ExpiresAt != nil && existing.ExpiresAt.After(time.Now()) {
			return o.intentToResponse(&existing), nil
		}
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	// ─────────────────────────────────────────────
	// 2. CREATE IDS FIRST (CRITICAL FIX)
	// ─────────────────────────────────────────────
	intentID := uuid.New()
	checkoutToken := utils.GenerateCheckoutToken(string(req.PaymentGateway))
	expiresAt := time.Now().Add(15 * time.Minute)

	var intent models.PaymentIntent
	var total int64

	// ─────────────────────────────────────────────
	// 3. TRANSACTION
	// ─────────────────────────────────────────────
	err = o.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

		var event models.Event

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND status = 'on_sale'", req.EventID).
			First(&event).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return utils.NewBusinessLogicError("Event is not available for ticket purchases. Please check that the event status is 'on_sale'.")
			}
			return utils.NewDatabaseError("Failed to retrieve event", err)
		}

		// ─────────────────────────────────────────
		// RESERVATION (NOW SAFE)
		// ─────────────────────────────────────────
		var err error
		total, err = o.reservation.Reserve(
			ctx,
			tx,
			&ReserveInput{
				PaymentIntentID: intentID, // ✅ FIXED
				EventID:         req.EventID,
				ActorID:         req.ActorID,
				ActorType:       req.ActorType,
				CustomerEmail:   req.CustomerEmail,
				Tiers:           req.Tiers,
				Currency:        req.Currency,
			},
			checkoutToken,
			expiresAt,
		)
		if err != nil {
			return err
		}

		// ─────────────────────────────────────────
		// CREATE PAYMENT INTENT
		// ─────────────────────────────────────────
		intent = models.PaymentIntent{
			ID:             intentID,
			ActorID:        req.ActorID,
			ActorType:      req.ActorType,
			EventID:        req.EventID,
			PaymentGateway: req.PaymentGateway,
			Currency:       req.Currency,
			Quantity:       calculateTotalQuantity(req.Tiers),
			AmountTotal:    total,
			Status:         models.PaymentIntentRequiresPaymentMethod,
			CheckoutToken:  checkoutToken,
			IdempotencyKey: req.IdempotencyKey,
			CustomerEmail:  req.CustomerEmail,
			ExpiresAt:      &expiresAt,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}

		return tx.Create(&intent).Error
	})

	if err != nil {
		return nil, err
	}

	// ─────────────────────────────────────────────
	// 4. GATEWAY SESSION
	// ─────────────────────────────────────────────
	lineItems, err := o.buildLineItems(ctx, req)
	if err != nil {
		return nil, err
	}

	gw, err := o.gwRegistry.Get(string(req.PaymentGateway))
	if err != nil {
		return nil, err
	}

	sess, err := gw.InitSession(ctx, &gateways.SessionRequest{
		CheckoutToken: checkoutToken,
		CustomerEmail: req.CustomerEmail,
		Currency:      req.Currency,
		LineItems:     lineItems,
		SuccessURL:    fmt.Sprintf("%s?token=%s", o.successURL, checkoutToken),
		CancelURL:     fmt.Sprintf("%s?token=%s", o.cancelURL, checkoutToken),
		Metadata: map[string]string{
			"checkout_token": checkoutToken,
		},
	})
	if err != nil {
		_ = o.db.Transaction(func(tx *gorm.DB) error {
			return o.reservation.Release(ctx, tx, intentID)
		})
		return nil, err
	}

	// ─────────────────────────────────────────────
	// 5. PAYMENT ATTEMPT
	// ─────────────────────────────────────────────
	_ = o.db.Create(&models.PaymentAttempt{
		ID:                  uuid.New(),
		PaymentIntentID:     intent.ID,
		PaymentGateway:      req.PaymentGateway,
		ProviderReferenceID: sess.GatewayPaymentIntentID,
		ProviderSessionID:   sess.GatewaySessionID,
		RedirectURL:         sess.RedirectURL,
		Amount:              total,
		Currency:            req.Currency,
		Status:              models.PaymentAttemptInitiated,
		CreatedAt:           time.Now(),
	})

	// ─────────────────────────────────────────────
	// 6. RESPONSE
	// ─────────────────────────────────────────────
	return &CheckoutResponse{
		CheckoutToken:    checkoutToken,
		AmountTotal:      total,
		Currency:         req.Currency,
		ExpiresAt:        expiresAt,
		RedirectURL:      sess.RedirectURL,
		GatewaySessionID: sess.GatewaySessionID,
	}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func (o *PurchaseOrchestrator) validate(req *CheckoutRequest) error {

	if req.EventID == uuid.Nil {
		return fmt.Errorf("event_id is required")
	}
	if len(req.Tiers) == 0 {
		return fmt.Errorf("at least one tier is required")
	}
	if req.Currency == "" {
		return fmt.Errorf("currency is required")
	}
	if req.PaymentGateway == "" {
		return fmt.Errorf("payment gateway is required")
	}
	if req.CustomerEmail == "" {
		return fmt.Errorf("customer_email is required")
	}
	return nil
}

func (o *PurchaseOrchestrator) findExistingIntent(ctx context.Context, req *CheckoutRequest) (*models.PaymentIntent, error) {
	var intent models.PaymentIntent
	err := o.db.WithContext(ctx).
		Where("actor_id = ? AND event_id = ? AND idempotency_key = ?",
			req.ActorID, req.EventID, req.IdempotencyKey).
		First(&intent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &intent, err
}

func (o *PurchaseOrchestrator) intentToResponse(i *models.PaymentIntent) *CheckoutResponse {
	resp := &CheckoutResponse{
		CheckoutToken: i.CheckoutToken,
		AmountTotal:   i.AmountTotal,
		Currency:      i.Currency,
	}
	if i.ExpiresAt != nil {
		resp.ExpiresAt = *i.ExpiresAt
	}

	// For existing intents, also return Stripe session data from PaymentAttempt
	var attempt models.PaymentAttempt
	if err := o.db.Where("payment_intent_id = ?", i.ID.String()).First(&attempt).Error; err == nil {
		resp.GatewaySessionID = attempt.ProviderSessionID
		resp.RedirectURL = attempt.RedirectURL // Use stored full URL
	}

	return resp
}

func (o *PurchaseOrchestrator) buildLineItems(ctx context.Context, req *CheckoutRequest) ([]gateways.LineItem, error) {
	var event models.Event
	if err := o.db.WithContext(ctx).Select("title").First(&event, req.EventID).Error; err != nil {
		return nil, fmt.Errorf("load event: %w", err)
	}

	items := make([]gateways.LineItem, 0, len(req.Tiers))
	for _, t := range req.Tiers {
		var tier models.EventTier
		if err := o.db.WithContext(ctx).
			Select("tier_name, price, currency").
			Where("id = ? AND event_id = ?", t.TierID, req.EventID).
			First(&tier).Error; err != nil {
			return nil, fmt.Errorf("load tier %s: %w", t.TierID, err)
		}

		unitAmount, err := currency.ToSmallestUnit(tier.Price, req.Currency)
		if err != nil {
			return nil, err
		}

		items = append(items, gateways.LineItem{
			Name:        event.Title + " — " + tier.TierName,
			Description: fmt.Sprintf("%d ticket(s)", t.Quantity),
			UnitAmount:  unitAmount,
			Currency:    req.Currency,
			Quantity:    t.Quantity,
		})
	}
	return items, nil
}

func generateIdempotencyKey(actorID, eventID uuid.UUID, tiers []types.TierSelection) string {
	// Deterministic but weak — prefer X-Idempotency-Key header from client.
	parts := []string{actorID.String(), eventID.String()}
	for _, t := range tiers {
		parts = append(parts, fmt.Sprintf("%s:%d", t.TierID, t.Quantity))
	}
	return strings.Join(parts, "|")
}

func calculateTotalQuantity(tiers []types.TierSelection) int {
	total := 0
	for _, t := range tiers {
		total += t.Quantity
	}
	return total
}
