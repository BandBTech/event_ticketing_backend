package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/types"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UnifiedPurchaseOrchestrator struct {
	db                    *gorm.DB
	reservationService    *ReservationService
	paymentGatewayService *PaymentGatewayService
}

// =======================================================
// REQUEST
// =======================================================

type UnifiedPurchaseRequest struct {
	ActorID   uuid.UUID
	ActorType string

	EventID uuid.UUID
	Tiers   []types.TierSelection

	PaymentGateway models.PaymentGateway
	Currency       string

	Email    string
	FullName string

	IdempotencyKey string
}

// =======================================================
// RESPONSE
// =======================================================

type UnifiedPurchaseResponse struct {
	CheckoutToken string
	Amount        int64
	Currency      string
	Status        models.PaymentStatus
	ExpiresAt     time.Time
	RedirectURL   string
}

// =======================================================
// ENTRY
// =======================================================

func (o *UnifiedPurchaseOrchestrator) Process(
	ctx context.Context,
	req *UnifiedPurchaseRequest,
) (*UnifiedPurchaseResponse, error) {

	if req.ActorID == uuid.Nil {
		return nil, fmt.Errorf("actor_id required")
	}

	if req.EventID == uuid.Nil {
		return nil, fmt.Errorf("event_id required")
	}

	if len(req.Tiers) == 0 {
		return nil, fmt.Errorf("at least one tier required")
	}

	utils.SortTiers(req.Tiers)

	if req.IdempotencyKey == "" {
		req.IdempotencyKey = utils.GenerateCheckoutToken(
			req.ActorID.String() + req.EventID.String(),
		)
	}

	return o.process(ctx, req)
}

// =======================================================
// CORE
// =======================================================

func (o *UnifiedPurchaseOrchestrator) process(
	ctx context.Context,
	req *UnifiedPurchaseRequest,
) (*UnifiedPurchaseResponse, error) {

	tx := o.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// =====================================================
	// LOCK EVENT (prevents race on event-level operations)
	// =====================================================
	var event models.Event
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", req.EventID).
		First(&event).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// =====================================================
	// IDEMPOTENCY CHECK (DB SAFE + UNIQUE INDEX REQUIRED)
	// =====================================================
	var existing models.PaymentIntent

	err := tx.
		Where("actor_id = ? AND event_id = ? AND idempotency_key = ?",
			req.ActorID, req.EventID, req.IdempotencyKey).
		First(&existing).Error

	if err == nil {
		tx.Rollback()

		return &UnifiedPurchaseResponse{
			CheckoutToken: existing.CheckoutToken,
			Amount:        existing.AmountTotal,
			Currency:      existing.Currency,
			ExpiresAt:     *existing.ExpiresAt,
			Status:        existing.Status,
		}, nil
	}

	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		tx.Rollback()
		return nil, err
	}

	// =====================================================
	// CREATE NEW FLOW
	// =====================================================
	checkoutToken := utils.GenerateCheckoutToken(req.PaymentGateway.String())
	expiresAt := time.Now().Add(15 * time.Minute)

	// =====================================================
	// RESERVATION (atomic via service)
	// =====================================================
	total, err := o.reservationService.Reserve(
		ctx,
		tx,
		req,
		checkoutToken,
		expiresAt,
	)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// =====================================================
	// PAYMENT INTENT (SINGLE SOURCE OF TRUTH)
	// =====================================================
	intent := &models.PaymentIntent{
		ID:             uuid.New(),
		CheckoutToken:  checkoutToken,
		ActorID:        req.ActorID,
		ActorType:      req.ActorType,
		EventID:        req.EventID,
		AmountTotal:    total,
		Currency:       req.Currency,
		Status:         models.PaymentPending,
		ExpiresAt:      &expiresAt,
		PaymentGateway: req.PaymentGateway,
		IdempotencyKey: req.IdempotencyKey,
	}

	if err := tx.Create(intent).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	// =====================================================
	// GATEWAY INIT (OUTSIDE TX)
	// =====================================================
	gateway, err := o.paymentGatewayService.InitializeGatewayData(
		intent,
		req,
		event.Title,
	)
	if err != nil {
		return nil, err
	}

	return &UnifiedPurchaseResponse{
		CheckoutToken: checkoutToken,
		Amount:        total,
		Currency:      req.Currency,
		Status:        models.PaymentPending,
		ExpiresAt:     expiresAt,
		RedirectURL:   gateway.URL,
	}, nil
}
