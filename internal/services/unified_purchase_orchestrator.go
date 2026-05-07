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
	CheckoutToken string
	AmountTotal   int64 // smallest currency unit
	Currency      string
	ExpiresAt     time.Time
	// RedirectURL is where the customer goes to pay.
	// For Konbini this points to our own success page (which shows the payment code).
	RedirectURL string
	// GatewaySessionID is the provider's session reference — useful for frontend polling.
	GatewaySessionID string
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
	// 1. IDEMPOTENCY CHECK (DB SAFE)
	// ─────────────────────────────────────────────
	var existing models.PaymentIntent

	err := o.db.WithContext(ctx).
		Where("actor_id = ? AND event_id = ? AND idempotency_key = ?",
			req.ActorID, req.EventID, req.IdempotencyKey).
		First(&existing).Error

	if err == nil {
		// Check if the existing intent is still valid (not expired)
		if existing.ExpiresAt != nil && existing.ExpiresAt.After(time.Now()) {
			return o.intentToResponse(&existing), nil
		}
		// If expired, continue to create a new one (idempotency key will be reused)
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	// ─────────────────────────────────────────────
	// 2. GENERATE CHECKOUT TOKEN (ONCE ONLY)
	// ─────────────────────────────────────────────
	checkoutToken := newCheckoutToken()
	expiresAt := time.Now().Add(15 * time.Minute)

	var intent models.PaymentIntent
	var total int64

	// ─────────────────────────────────────────────
	// 3. DB TRANSACTION (SAFE INVENTORY LOCK)
	// ─────────────────────────────────────────────
	err = o.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {

		var event models.Event

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND status = 'on_sale'", req.EventID).
			First(&event).Error; err != nil {
			return fmt.Errorf("event not available")
		}

		// reserve inventory
		var err error
		total, err = o.reservation.Reserve(
			ctx,
			tx,
			&ReserveInput{
				EventID:       req.EventID,
				ActorID:       req.ActorID,
				ActorType:     req.ActorType,
				CustomerEmail: req.CustomerEmail,
				Tiers:         req.Tiers,
				Currency:      req.Currency,
			},
			checkoutToken,
			expiresAt,
		)
		if err != nil {
			return err
		}

		intent = models.PaymentIntent{
			ID:             uuid.New(),
			ActorID:        req.ActorID,
			ActorType:      req.ActorType,
			EventID:        req.EventID,
			PaymentGateway: req.PaymentGateway,
			Currency:       req.Currency,
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
	// 4. GATEWAY SESSION (OUTSIDE TX)
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

		// rollback reservation
		_ = o.db.Transaction(func(tx *gorm.DB) error {
			return o.reservation.Release(ctx, tx, checkoutToken)
		})

		return nil, err
	}

	// ─────────────────────────────────────────────
	// 5. SAVE PAYMENT ATTEMPT
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

func newCheckoutToken() string {
	return "chk_" + uuid.New().String()
}
