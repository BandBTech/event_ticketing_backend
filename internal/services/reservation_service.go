package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ReservationService handles ticket reservations with expiry lifecycle
type ReservationService struct {
	db *gorm.DB
}

// NewReservationService creates a new reservation service
func NewReservationService(db *gorm.DB) *ReservationService {
	return &ReservationService{db: db}
}

// CreateReservation creates a temporary ticket reservation
func (s *ReservationService) CreateReservation(ctx context.Context, req *CreatePaymentRequest) (*CreatePaymentResponse, error) {
	tx := s.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 1. Load event and validate
	var event models.Event
	if err := tx.Preload("Organizer").First(&event, req.EventID).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("event not found: %w", err)
	}

	// 2. Validate all tiers and calculate pricing
	var totalAmount float64
	var commissionTotal float64
	var tierReservations []models.TicketReservation

	for _, tierSelection := range req.TierSelections {
		var tier models.EventTier
		if err := tx.Where("id = ? AND event_id = ?", tierSelection.TierID, req.EventID).First(&tier).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("tier not found: %w", err)
		}

		// Check available capacity (including existing reservations)
		availableCapacity := tier.Quantity - tier.Sold - tier.Reserved
		if availableCapacity < tierSelection.Quantity {
			tx.Rollback()
			return nil, utils.NewBusinessLogicError(fmt.Sprintf("Insufficient tickets for tier %s. Available: %d, Requested: %d", tier.TierName, availableCapacity, tierSelection.Quantity))
		}

		// Calculate pricing for this tier
		subtotal := tier.Price * float64(tierSelection.Quantity)
		commissionRate := event.CommissionRate
		commissionAmount := subtotal * (commissionRate / 100)

		totalAmount += subtotal
		commissionTotal += commissionAmount

		// Create reservation record (temporary, will be confirmed or expired)
		reservation := models.TicketReservation{
			EventID:       req.EventID,
			TierID:        tierSelection.TierID,
			UserID:        req.UserID,
			GuestUserID:   req.GuestUserID,
			CustomerEmail: req.CustomerEmail,
			Quantity:      tierSelection.Quantity,
			Status:        models.ReservationStatusReserved,
			ExpiresAt:     time.Now().Add(15 * time.Minute), // 15-minute reservation window
		}
		tierReservations = append(tierReservations, reservation)
	}

	// 3. Generate unique checkout token for this reservation set
	// CRITICAL: Must be globally unique - use full UUID to guarantee no collision even with rapid-fire requests
	checkoutToken := fmt.Sprintf("checkout_%s_%s_%s_%d", req.EventID.String()[:8], strings.ReplaceAll(req.CustomerEmail, "@", "_at_"), uuid.New().String(), time.Now().UnixNano())

	// 4. Generate idempotency key
	// CRITICAL: Also use full UUID and sanitize email for idempotency key uniqueness
	idempotencyKey := fmt.Sprintf("payment_%s_%s_%s_%d", req.EventID.String()[:8], strings.ReplaceAll(req.CustomerEmail, "@", "_at_"), uuid.New().String(), time.Now().UnixNano())

	// 5. Create PaymentIntent record (minimal, just tracking)
	paymentIntent := &models.PaymentIntent{
		PaymentGateway:     req.PaymentGateway,
		IdempotencyKey:     idempotencyKey,
		CheckoutToken:      checkoutToken,
		UserID:             req.UserID,
		GuestUserID:        req.GuestUserID,
		CustomerEmail:      req.CustomerEmail,
		CustomerName:       req.CustomerName,
		CustomerPhone:      req.CustomerPhone,
		EventID:            req.EventID,
		TierID:             req.TierSelections[0].TierID, // Primary tier
		Quantity:           len(req.TierSelections),      // Number of tier types
		Currency:           req.Currency,
		CurrencySymbol:     getCurrencySymbol(req.Currency),
		ExchangeRate:       1.0,
		BaseCurrency:       "USD",
		BaseCurrencyAmount: totalAmount,
		UnitPrice:          0, // Multi-tier
		Subtotal:           totalAmount,
		PlatformFee:        commissionTotal,
		GatewayFee:         0,
		TotalAmount:        totalAmount,
		Status:             "pending",
		CommissionRate:     event.CommissionRate,
		CommissionAmount:   commissionTotal,
		OrganizerNetAmount: totalAmount - commissionTotal,
		CountryCode:        req.CountryCode,
	}

	if err := tx.Create(paymentIntent).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create payment intent: %w", err)
	}

	// 6. ATOMIC RESERVATION: Update tier reserved counts
	// CRITICAL: All-or-nothing - if any tier fails, rollback everything
	for i, tierSelection := range req.TierSelections {
		reservation := tierReservations[i]

		// Atomic update: increment reserved count with capacity check
		result := tx.Model(&models.EventTier{}).
			Where("id = ? AND (quantity - sold - reserved) >= ?", tierSelection.TierID, tierSelection.Quantity).
			Update("reserved", gorm.Expr("reserved + ?", tierSelection.Quantity))

		if result.Error != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to reserve tier %s: %w", tierSelection.TierID, result.Error)
		}

		// Check if update actually happened (capacity validation)
		if result.RowsAffected == 0 {
			tx.Rollback()
			return nil, utils.NewBusinessLogicError(fmt.Sprintf("Insufficient capacity for tier reservation (concurrent booking)"))
		}

		// Set the checkout token for this reservation
		reservation.CheckoutToken = checkoutToken
		if err := tx.Create(&reservation).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to create reservation record: %w", err)
		}
	}

	// 7. COMMIT RESERVATION PHASE
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit reservation: %w", err)
	}

	log.Printf("✓ Payment reserved atomically: token=%s, amount=%.2f %s, expires=%v",
		checkoutToken, totalAmount+commissionTotal, req.Currency, time.Now().Add(15*time.Minute))

	return &CreatePaymentResponse{
		PaymentIntentID: paymentIntent.ID,
		CheckoutToken:   checkoutToken,
		PaymentGateway:  req.PaymentGateway,
		RedirectURL:     "", // Will be set by gateway-specific logic
		Amount:          totalAmount + commissionTotal,
		Currency:        req.Currency,
		Status:          "reserved",    // NEW: Clear status indicating reservation phase
		ReservedTickets: []uuid.UUID{}, // Empty until confirmation
		ExpiresAt:       &tierReservations[0].ExpiresAt,
	}, nil
}

// ConfirmReservation converts a reservation to confirmed tickets
func (s *ReservationService) ConfirmReservation(ctx context.Context, checkoutToken string, paymentIntentID string) error {
	tx := s.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 1. Find all reservations for this checkout token (including already-confirmed for idempotency)
	var reservations []models.TicketReservation
	if err := tx.Where("checkout_token = ?", checkoutToken).
		Find(&reservations).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to find reservations: %w", err)
	}

	if len(reservations) == 0 {
		tx.Rollback()
		return fmt.Errorf("no reservations found for token: %s", checkoutToken)
	}

	// 2. IDEMPOTENCY CHECK: If all reservations are already confirmed, treat as success
	allConfirmed := true
	for _, r := range reservations {
		if r.Status != models.ReservationStatusConfirmed {
			allConfirmed = false
			break
		}
	}
	if allConfirmed {
		tx.Rollback()
		log.Printf("[IDEMPOTENT] Reservation already confirmed for token: %s", checkoutToken)
		return nil // Idempotent - already processed by another webhook
	}

	// 2. Check if any reservations have expired
	now := time.Now()
	for _, reservation := range reservations {
		if reservation.Status == models.ReservationStatusReserved && reservation.ExpiresAt.Before(now) {
			tx.Rollback()
			return fmt.Errorf("reservation expired for tier %s", reservation.TierID)
		}
	}

	// 3. Create actual tickets and update inventory (only for reserved status)
	var ticketIDs []uuid.UUID
	for _, reservation := range reservations {
		// Skip already-confirmed reservations (idempotency)
		if reservation.Status == models.ReservationStatusConfirmed {
			log.Printf("[IDEMPOTENT] Skipping already-confirmed reservation: tier=%s, quantity=%d", reservation.TierID, reservation.Quantity)
			continue
		}

		if reservation.Status != models.ReservationStatusReserved {
			log.Printf("[WARN] Skipping reservation with unexpected status: %s", reservation.Status)
			continue
		}

		// Create tickets for this reservation
		for i := 0; i < reservation.Quantity; i++ {
			ticket := &models.Ticket{
				EventID:       reservation.EventID,
				TierID:        reservation.TierID,
				UserID:        reservation.UserID,
				GuestUserID:   reservation.GuestUserID,
				Status:        "confirmed",
				PaymentStatus: "completed",
				PaidAt:        &now,
			}

			if err := tx.Create(ticket).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to create ticket: %w", err)
			}

			ticketIDs = append(ticketIDs, ticket.ID)
		}

		// Update tier: move from reserved to sold
		result := tx.Model(&models.EventTier{}).
			Where("id = ?", reservation.TierID).
			Updates(map[string]interface{}{
				"reserved": gorm.Expr("reserved - ?", reservation.Quantity),
				"sold":     gorm.Expr("sold + ?", reservation.Quantity),
			})

		if result.Error != nil || result.RowsAffected == 0 {
			tx.Rollback()
			return fmt.Errorf("failed to update tier inventory for %s", reservation.TierID)
		}

		// Mark reservation as confirmed
		reservation.Status = models.ReservationStatusConfirmed
		reservation.ConfirmedAt = &now
		if err := tx.Save(&reservation).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update reservation status: %w", err)
		}
	}

	// 4. Commit confirmation
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit reservation confirmation: %w", err)
	}

	if len(ticketIDs) > 0 {
		log.Printf("✓ Reservation confirmed: token=%s, new_tickets=%d", checkoutToken, len(ticketIDs))
	} else {
		log.Printf("✓ Reservation idempotent (already processed): token=%s", checkoutToken)
	}
	return nil
}

// ExpireReservations releases expired reservations and restores inventory
func (s *ReservationService) ExpireReservations(ctx context.Context) error {
	tx := s.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	now := time.Now()

	// 1. Find expired reservations
	var expiredReservations []models.TicketReservation
	if err := tx.Where("status = ? AND expires_at < ?", models.ReservationStatusReserved, now).
		Find(&expiredReservations).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to find expired reservations: %w", err)
	}

	if len(expiredReservations) == 0 {
		tx.Commit() // Nothing to do
		return nil
	}

	// 2. Group by tier for efficient updates
	tierUpdates := make(map[uuid.UUID]int)
	for _, reservation := range expiredReservations {
		tierUpdates[reservation.TierID] += reservation.Quantity
	}

	// 3. Update tier inventory (release reserved tickets)
	for tierID, quantityToRelease := range tierUpdates {
		result := tx.Model(&models.EventTier{}).
			Where("id = ?", tierID).
			Update("reserved", gorm.Expr("reserved - ?", quantityToRelease))

		if result.Error != nil {
			tx.Rollback()
			return fmt.Errorf("failed to release reserved inventory for tier %s: %w", tierID, result.Error)
		}
	}

	// 4. Mark reservations as expired
	if err := tx.Model(&models.TicketReservation{}).
		Where("status = ? AND expires_at < ?", models.ReservationStatusReserved, now).
		Update("status", models.ReservationStatusExpired).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to mark reservations as expired: %w", err)
	}

	// 5. Commit expiry
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit reservation expiry: %w", err)
	}

	log.Printf("✓ Expired %d reservations, released inventory for %d tiers", len(expiredReservations), len(tierUpdates))
	return nil
}

// GetReservationByCheckoutToken retrieves reservation details
func (s *ReservationService) GetReservationByCheckoutToken(ctx context.Context, checkoutToken string) (*models.TicketReservation, error) {
	var reservation models.TicketReservation
	if err := s.db.Where("checkout_token = ?", checkoutToken).
		Preload("Event").Preload("Tier").
		First(&reservation).Error; err != nil {
		return nil, fmt.Errorf("reservation not found: %w", err)
	}
	return &reservation, nil
}
