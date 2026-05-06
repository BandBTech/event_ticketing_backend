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

	// ── Idempotency: return existing intent ──────────────────────────────
	if existing, err := o.findExistingIntent(ctx, req); err != nil {
		return nil, err
	} else if existing != nil {
		return o.intentToResponse(existing), nil
	}

	// ── DB transaction ────────────────────────────────────────────────────
	checkoutToken := newCheckoutToken()
	expiresAt := time.Now().Add(15 * time.Minute)
	var intent models.PaymentIntent

	err := o.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock event to prevent concurrent event cancellations during checkout.
		var event models.Event
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id, status, commission_rate").
			Where("id = ? AND status = 'published'", req.EventID).
			First(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("event is not available for purchase")
			}
			return err
		}

		// Reserve inventory (atomic, idempotent).
		total, err := o.reservation.Reserve(ctx, tx, &ReserveInput{
			EventID:       req.EventID,
			ActorID:       req.ActorID,
			ActorType:     req.ActorType,
			CustomerEmail: req.CustomerEmail,
			Tiers:         req.Tiers,
			Currency:      req.Currency,
		}, checkoutToken, expiresAt)
		if err != nil {
			return err
		}

		// Create PaymentIntent.
		intent = models.PaymentIntent{
			ID:             uuid.New(),
			CheckoutToken:  checkoutToken,
			IdempotencyKey: req.IdempotencyKey,
			ActorID:        req.ActorID,
			ActorType:      string(req.ActorType),
			EventID:        req.EventID,
			PaymentGateway: req.PaymentGateway,
			Currency:       req.Currency,
			AmountTotal:    total,
			Status:         models.PaymentIntentRequiresPaymentMethod,
			CustomerEmail:  req.CustomerEmail,
			ExpiresAt:      &expiresAt,
		}
		return tx.Create(&intent).Error
	})
	if err != nil {
		return nil, err
	}

	// ── Gateway session (outside transaction) ────────────────────────────
	lineItems, err := o.buildLineItems(ctx, req)
	if err != nil {
		return nil, err
	}

	gw, err := o.gwRegistry.Get(string(req.PaymentGateway))
	if err != nil {
		return nil, err
	}

	sessResp, err := gw.InitSession(ctx, &gateways.SessionRequest{
		CheckoutToken: checkoutToken,
		ActorType:     string(req.ActorType),
		CustomerEmail: req.CustomerEmail,
		Currency:      req.Currency,
		LineItems:     lineItems,
		SuccessURL:    fmt.Sprintf("%s?checkout_token=%s", o.successURL, checkoutToken),
		CancelURL:     fmt.Sprintf("%s?checkout_token=%s", o.cancelURL, checkoutToken),
		Metadata: map[string]string{
			"checkout_token": checkoutToken,
			"actor_type":     string(req.ActorType),
		},
	})
	if err != nil {
		// Gateway session failed — release the reservation so inventory isn't stuck.
		_ = o.db.Transaction(func(tx *gorm.DB) error {
			return o.reservation.Release(ctx, tx, checkoutToken)
		})
		return nil, fmt.Errorf("payment gateway init failed: %w", err)
	}

	// Persist PaymentAttempt (gateway-specific details).
	attempt := models.PaymentAttempt{
		ID:                  uuid.New(),
		PaymentIntentID:     intent.ID,
		PaymentGateway:      req.PaymentGateway,
		ProviderReferenceID: sessResp.GatewaySessionID,
		Currency:            req.Currency,
		Amount:              intent.AmountTotal,
		Status:              models.PaymentAttemptInitiated,
	}
	_ = o.db.Create(&attempt)

	return &CheckoutResponse{
		CheckoutToken:    checkoutToken,
		AmountTotal:      intent.AmountTotal,
		Currency:         req.Currency,
		ExpiresAt:        expiresAt,
		RedirectURL:      sessResp.RedirectURL,
		GatewaySessionID: sessResp.GatewaySessionID,
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
