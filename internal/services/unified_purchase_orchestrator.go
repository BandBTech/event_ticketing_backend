package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UnifiedPurchaseOrchestrator handles ticket purchases for both guests and logged-in users
// SINGLE SOURCE OF TRUTH for all ticket purchase operations (Cash, Stripe, etc.)
// Maintains proper identity data and eliminates code duplication
type UnifiedPurchaseOrchestrator struct {
	ticketService      *TicketService
	reservationService *ReservationService
	emailQueueService  *EmailQueueService
	db                 *gorm.DB
}

// NewUnifiedPurchaseOrchestrator creates a new unified purchase orchestrator
func NewUnifiedPurchaseOrchestrator(
	ticketService *TicketService,
	reservationService *ReservationService,
	emailQueueService *EmailQueueService,
	db *gorm.DB,
) *UnifiedPurchaseOrchestrator {
	return &UnifiedPurchaseOrchestrator{
		ticketService:      ticketService,
		reservationService: reservationService,
		emailQueueService:  emailQueueService,
		db:                 db,
	}
}

// UnifiedPurchaseRequest represents a ticket purchase request for BOTH guests and logged-in users
// Identity is determined by which fields are populated:
// - UserID + Email = Logged-in user purchase
// - GuestUserID + Email OR Email alone = Guest purchase
type UnifiedPurchaseRequest struct {
	// Identity Information (one of these must be provided)
	UserID      *uuid.UUID `json:"user_id,omitempty"`       // For logged-in users
	GuestUserID *uuid.UUID `json:"guest_user_id,omitempty"` // For existing guests

	// Customer Details
	Email       string `json:"email" binding:"required,email"`
	FirstName   string `json:"first_name,omitempty"`
	LastName    string `json:"last_name,omitempty"`
	Phone       string `json:"phone,omitempty"`
	CountryCode string `json:"country_code,omitempty"`

	// Event & Ticket Details
	EventID uuid.UUID                    `json:"event_id" binding:"required"`
	Tiers   []models.TicketTierSelection `json:"tiers" binding:"required,min=1,dive"`

	// Payment Information
	PaymentGateway models.PaymentGateway `json:"payment_gateway" binding:"required"`
	Currency       string                `json:"currency,omitempty"` // Auto-detect if not provided
}

// UnifiedPurchaseResponse represents the response from a unified purchase
type UnifiedPurchaseResponse struct {
	// Purchase Identification
	CheckoutToken string `json:"checkout_token"`

	// User Information (identity preserved)
	UserID      *uuid.UUID `json:"user_id,omitempty"`
	GuestUserID *uuid.UUID `json:"guest_user_id,omitempty"`
	Email       string     `json:"email"`

	// Payment Information
	PaymentGateway string  `json:"payment_gateway"`
	Amount         float64 `json:"amount"`
	Currency       string  `json:"currency"`
	Status         string  `json:"status"` // "completed" for cash, "pending" for gateway

	// Gateway Data (for payment completion)
	GatewayData map[string]interface{} `json:"gateway_data,omitempty"`
	ExpiresAt   time.Time              `json:"expires_at"`

	// Ticket Information
	TicketCount int `json:"ticket_count"`

	// For cash payments: immediate completion
	// For gateway payments: redirect/session info
	ImmediateCompletion bool   `json:"immediate_completion"`
	RedirectURL         string `json:"redirect_url,omitempty"`
}

// ProcessUnifiedPurchase handles ticket purchases for BOTH guests and logged-in users
// This is the single entry point for all ticket purchase operations
// Maintains identity data and handles all payment methods uniformly
// THREAD-SAFE: Uses distributed locking + database-level locks to prevent overselling
func (uo *UnifiedPurchaseOrchestrator) ProcessUnifiedPurchase(
	ctx context.Context,
	req *UnifiedPurchaseRequest,
) (*UnifiedPurchaseResponse, error) {

	// ========================================
	// 1. VALIDATE TIERS & REQUEST (early validation)
	// ========================================
	if len(req.Tiers) == 0 {
		return nil, utils.NewBusinessLogicError("At least one tier must be selected")
	}
	for _, tier := range req.Tiers {
		if tier.TierID == uuid.Nil {
			return nil, utils.NewBusinessLogicError("Invalid tier ID")
		}
		if tier.Quantity <= 0 {
			return nil, utils.NewBusinessLogicError("Tier quantity must be greater than 0")
		}
		if tier.Quantity > 10 {
			return nil, utils.NewBusinessLogicError("Cannot purchase more than 10 tickets per tier")
		}
	}

	// ========================================
	// 2. VALIDATE & NORMALIZE IDENTITY
	// ========================================
	userID, guestUserID, customerEmail, err := uo.normalizeAndValidateIdentity(ctx, req)
	if err != nil {
		return nil, err
	}

	// ========================================
	// 3. VALIDATE PURCHASE REQUEST
	// ========================================
	if err := uo.validatePurchaseRequest(ctx, req); err != nil {
		return nil, err
	}

	// ========================================
	// 4. SORT TIER IDS FOR CONSISTENT LOCKING ORDER (prevents deadlock)
	// ========================================
	sortedTiers := make([]models.TicketTierSelection, len(req.Tiers))
	copy(sortedTiers, req.Tiers)
	// Sort by tier ID to ensure consistent lock acquisition order across all requests
	for i := 0; i < len(sortedTiers)-1; i++ {
		for j := i + 1; j < len(sortedTiers); j++ {
			if sortedTiers[i].TierID.String() > sortedTiers[j].TierID.String() {
				sortedTiers[i], sortedTiers[j] = sortedTiers[j], sortedTiers[i]
			}
		}
	}

	// ========================================
	// 5. ROUTE TO APPROPRIATE PAYMENT HANDLER
	// ========================================
	switch req.PaymentGateway {
	case models.PaymentGatewayCash:
		// Create new request with sorted tiers to ensure consistent ordering
		sortedReq := &UnifiedPurchaseRequest{
			UserID:         req.UserID,
			GuestUserID:    req.GuestUserID,
			Email:          req.Email,
			FirstName:      req.FirstName,
			LastName:       req.LastName,
			Phone:          req.Phone,
			CountryCode:    req.CountryCode,
			EventID:        req.EventID,
			Tiers:          sortedTiers,
			PaymentGateway: req.PaymentGateway,
			Currency:       req.Currency,
		}
		return uo.processCashPayment(ctx, userID, guestUserID, customerEmail, sortedReq, "")

	case models.PaymentGatewayStripe:
		// Create new request with sorted tiers to ensure consistent ordering
		sortedReq := &UnifiedPurchaseRequest{
			UserID:         req.UserID,
			GuestUserID:    req.GuestUserID,
			Email:          req.Email,
			FirstName:      req.FirstName,
			LastName:       req.LastName,
			Phone:          req.Phone,
			CountryCode:    req.CountryCode,
			EventID:        req.EventID,
			Tiers:          sortedTiers,
			PaymentGateway: req.PaymentGateway,
			Currency:       req.Currency,
		}
		return uo.processStripePayment(ctx, userID, guestUserID, customerEmail, sortedReq, "")

	default:
		return nil, fmt.Errorf("unsupported payment gateway: %s", req.PaymentGateway)
	}
}

// processCashPayment handles immediate cash payment completion for both user types
// THREAD-SAFE: Acquires distributed tier locks + database-level locks before any modifications
func (uo *UnifiedPurchaseOrchestrator) processCashPayment(
	ctx context.Context,
	userID *uuid.UUID,
	guestUserID *uuid.UUID,
	customerEmail string,
	req *UnifiedPurchaseRequest,
	_ string, // currency param ignored - will be determined after locks acquired
) (*UnifiedPurchaseResponse, error) {

	log.Printf("[UNIFIED_PURCHASE] Processing cash payment - userID=%v, email=%s, event=%s, tiers=%d\n",
		userID, customerEmail, req.EventID, len(req.Tiers))

	// Calculate total quantity
	totalQuantity := 0
	for _, tier := range req.Tiers {
		totalQuantity += tier.Quantity
	}

	// Validate quantity limits based on user type
	if userID != nil && totalQuantity > 10 {
		return nil, utils.NewBusinessLogicError("Logged-in users can purchase maximum 10 tickets")
	}
	if userID == nil && totalQuantity > 6 {
		return nil, utils.NewBusinessLogicError("Guests can purchase maximum 6 tickets")
	}

	// ======== CRITICAL: ACQUIRE LOCKS BEFORE ANY DB MODIFICATIONS ========
	// Use tier-level distributed locking to prevent race conditions
	// Tiers are already sorted in ProcessUnifiedPurchase to prevent deadlock
	var unlocks []func()
	for _, tierSel := range req.Tiers {
		unlock := utils.GetInventoryLock().LockTier(tierSel.TierID.String())
		unlocks = append(unlocks, unlock)
	}
	defer func() {
		for _, unlock := range unlocks {
			unlock()
		}
	}()

	// Execute with retry logic for deadlock recovery
	return utils.WithRetryFunc(func() (*UnifiedPurchaseResponse, error) {
		tx := uo.db.Begin()
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
			}
		}()

		// Get or create user identity
		var finalUserID, finalGuestUserID *uuid.UUID

		if userID != nil {
			// Logged-in user - verify exists
			var user models.User
			if err := tx.Where("id = ?", userID).First(&user).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("user not found: %w", err)
			}
			finalUserID = userID
		} else {
			// Guest user - create or find
			guestUser, err := uo.ticketService.createOrFindGuestUser(tx, &models.GuestPurchaseRequest{
				EventID:     req.EventID,
				Email:       customerEmail,
				FirstName:   req.FirstName,
				LastName:    req.LastName,
				Phone:       req.Phone,
				CountryCode: req.CountryCode,
			})
			if err != nil {
				tx.Rollback()
				return nil, err
			}
			finalGuestUserID = &guestUser.ID
		}

		// Get event for audit and commission calculations
		var event models.Event
		if err := tx.Preload("Organizer").Where("id = ?", req.EventID).First(&event).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("event not found: %w", err)
		}

		// Determine currency from first tier (after locks acquired)
		var currency string
		if req.Currency != "" {
			currency = req.Currency
		} else {
			var firstTier models.EventTier
			if err := tx.Where("id = ? AND event_id = ?", req.Tiers[0].TierID, req.EventID).
				First(&firstTier).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to get tier currency: %w", err)
			}
			currency = firstTier.Currency
		}

		// Create tickets for each tier selection
		var allTickets []*models.Ticket
		totalAmount := 0.0

		for _, tierSelection := range req.Tiers {
			// Get tier with database-level lock (FOR UPDATE)
			var tier models.EventTier
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND event_id = ?", tierSelection.TierID, req.EventID).
				First(&tier).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("tier not found: %w", err)
			}

			// Validate tier is active and has availability
			if !tier.IsActive {
				tx.Rollback()
				return nil, fmt.Errorf("tier %s is not active", tier.TierName)
			}

			if tier.Available < tierSelection.Quantity {
				tx.Rollback()
				return nil, fmt.Errorf("insufficient tickets for tier %s (available: %d, requested: %d)",
					tier.TierName, tier.Available, tierSelection.Quantity)
			}

			// Create individual tickets
			for i := 0; i < tierSelection.Quantity; i++ {
				ticket := &models.Ticket{
					UserID:          finalUserID,
					GuestUserID:     finalGuestUserID,
					EventID:         req.EventID,
					TierID:          tierSelection.TierID,
					TotalAmount:     tier.Price,
					PaymentGateway:  req.PaymentGateway,
					Status:          "active", // Cash payments are immediately active
					IsGuestPurchase: userID == nil,
				}

				// Generate ticket number
				ticketNumber, err := utils.GenerateEventTicketNumber(tx, tier.TierName, event.StartDate.Year())
				if err != nil {
					tx.Rollback()
					return nil, err
				}
				ticket.TicketNumber = ticketNumber

				// Create ticket
				if err := tx.Create(ticket).Error; err != nil {
					tx.Rollback()
					return nil, err
				}

				// Load event relationship
				if err := tx.Preload("Event").First(ticket, ticket.ID).Error; err != nil {
					tx.Rollback()
					return nil, err
				}

				allTickets = append(allTickets, ticket)
				totalAmount += tier.Price
			}

			// ======== CRITICAL: ATOMIC AVAILABILITY UPDATE ========
			// Only update if availability is still sufficient (double-check pattern)
			// This prevents overselling in case of concurrent requests
			result := tx.Model(&tier).
				Where("id = ? AND available >= ?", tier.ID, tierSelection.Quantity).
				Updates(map[string]interface{}{
					"available": gorm.Expr("available - ?", tierSelection.Quantity),
					"sold":      gorm.Expr("sold + ?", tierSelection.Quantity),
				})

			if result.Error != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to update tier availability: %w", result.Error)
			}

			// CRITICAL: Verify update actually succeeded (rows affected check)
			if result.RowsAffected == 0 {
				tx.Rollback()
				return nil, fmt.Errorf("tier availability changed during transaction (race condition) - tier: %s", tier.TierName)
			}
		}

		// Calculate commission
		commissionRate := event.CommissionRate
		commissionAmount := totalAmount * (commissionRate / 100)

		// Create transaction record
		transaction := &models.Transaction{
			EventID:          req.EventID,
			UserID:           finalUserID,
			GuestUserID:      finalGuestUserID,
			PaymentGateway:   req.PaymentGateway,
			Amount:           totalAmount,
			Currency:         currency,
			Quantity:         totalQuantity,
			Status:           "completed",
			GatewayTxnID:     "", // No gateway for cash
			GatewayData:      map[string]interface{}{"payment_method": "cash"},
			CommissionRate:   commissionRate,
			CommissionAmount: commissionAmount,
			OrganizerShare:   totalAmount - commissionAmount,
		}

		// Associate tickets with transaction
		for _, ticket := range allTickets {
			ticket.TransactionID = &transaction.ID
			if err := tx.Save(ticket).Error; err != nil {
				tx.Rollback()
				return nil, err
			}
		}

		if err := tx.Create(transaction).Error; err != nil {
			tx.Rollback()
			return nil, err
		}

		// Commit transaction
		if err := tx.Commit().Error; err != nil {
			return nil, err
		}

		// Audit logging
		uo.ticketService.logAudit(ctx, "payment_succeeded", "transaction", transaction.ID, nil, "system",
			&req.EventID, map[string]interface{}{
				"user_id":         userID,
				"guest_user_id":   guestUserID,
				"payment_gateway": "cash",
				"total_amount":    totalAmount,
				"total_tickets":   totalQuantity,
			})

		// Queue confirmation emails
		if uo.emailQueueService != nil {
			if userID != nil {
				// Send to logged-in user
				uo.ticketService.sendUserTicketConfirmationEmails(allTickets)
			} else if finalGuestUserID != nil {
				// Send to guest
				if err := uo.emailQueueService.QueueGuestTicketConfirmationEmail(customerEmail, allTickets); err != nil {
					log.Printf("Failed to queue confirmation email for %s: %v", customerEmail, err)
				}
			}
		}

		// Return response
		return &UnifiedPurchaseResponse{
			CheckoutToken:       fmt.Sprintf("cash_%s_%s_%d", req.EventID.String()[:8], uuid.New().String(), time.Now().UnixNano()),
			UserID:              finalUserID,
			GuestUserID:         finalGuestUserID,
			Email:               customerEmail,
			PaymentGateway:      string(req.PaymentGateway),
			Amount:              totalAmount,
			Currency:            currency,
			Status:              "completed",
			TicketCount:         totalQuantity,
			ImmediateCompletion: true,
			ExpiresAt:           time.Now().Add(30 * time.Minute),
		}, nil
	})
}

// processStripePayment handles Stripe payment initiation for both user types
// THREAD-SAFE: Acquires distributed tier locks before creating reservation
func (uo *UnifiedPurchaseOrchestrator) processStripePayment(
	ctx context.Context,
	userID *uuid.UUID,
	guestUserID *uuid.UUID,
	customerEmail string,
	req *UnifiedPurchaseRequest,
	_ string, // currency param ignored - will be determined after locks acquired
) (*UnifiedPurchaseResponse, error) {

	log.Printf("[UNIFIED_PURCHASE] Processing Stripe payment - userID=%v, email=%s, event=%s, tiers=%d\n",
		userID, customerEmail, req.EventID, len(req.Tiers))

	// Calculate total quantity
	totalQuantity := 0
	for _, tier := range req.Tiers {
		totalQuantity += tier.Quantity
	}

	// Validate quantity limits based on user type
	if userID != nil && totalQuantity > 10 {
		return nil, utils.NewBusinessLogicError("Logged-in users can purchase maximum 10 tickets")
	}
	if userID == nil && totalQuantity > 6 {
		return nil, utils.NewBusinessLogicError("Guests can purchase maximum 6 tickets")
	}

	// ======== CRITICAL: ACQUIRE LOCKS BEFORE ANY RESERVATION ========
	// Use tier-level distributed locking to prevent race conditions
	// Tiers are already sorted in ProcessUnifiedPurchase to prevent deadlock
	var unlocks []func()
	for _, tierSel := range req.Tiers {
		unlock := utils.GetInventoryLock().LockTier(tierSel.TierID.String())
		unlocks = append(unlocks, unlock)
	}
	defer func() {
		for _, unlock := range unlocks {
			unlock()
		}
	}()

	// ======== DETERMINE CURRENCY WITHIN LOCK PROTECTION ========
	var currency string
	if req.Currency != "" {
		currency = req.Currency
	} else {
		var firstTier models.EventTier
		if err := uo.db.Where("id = ? AND event_id = ?", req.Tiers[0].TierID, req.EventID).
			First(&firstTier).Error; err != nil {
			return nil, fmt.Errorf("failed to get tier currency: %w", err)
		}
		currency = firstTier.Currency
	}

	// Build reservation request from unified request
	tierSelections := make([]TierSelection, len(req.Tiers))
	for i, t := range req.Tiers {
		tierSelections[i] = TierSelection{
			TierID:   t.TierID,
			Quantity: t.Quantity,
		}
	}

	var firstName, lastName string
	if userID != nil {
		// Get logged-in user details
		var user models.User
		if err := uo.db.Where("id = ?", userID).First(&user).Error; err != nil {
			return nil, fmt.Errorf("user not found: %w", err)
		}
		firstName = user.FirstName
		lastName = user.LastName
	} else {
		firstName = req.FirstName
		lastName = req.LastName
		if firstName == "" {
			firstName = "Guest"
		}
		if lastName == "" {
			lastName = "User"
		}
	}

	// Create reservation (same logic for both user types)
	// CRITICAL: Reservation service will also check availability with locks
	paymentReq := &CreatePaymentRequest{
		EventID:        req.EventID,
		UserID:         userID,
		GuestUserID:    guestUserID,
		CustomerEmail:  customerEmail,
		CustomerName:   firstName + " " + lastName,
		CustomerPhone:  req.Phone,
		Currency:       currency,
		PaymentGateway: string(req.PaymentGateway),
		CountryCode:    req.CountryCode,
		TierSelections: tierSelections,
	}

	// Create reservation
	paymentResp, err := uo.reservationService.CreateReservation(ctx, paymentReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create reservation: %w", err)
	}

	log.Printf("[UNIFIED_PURCHASE] Reservation created - token=%s, amount=%.2f %s\n",
		paymentResp.CheckoutToken, paymentResp.Amount, paymentResp.Currency)

	// Create checkout session
	checkoutSession := &models.CheckoutSession{
		TicketID:       uuid.Nil,
		UserID:         userID,
		GuestUserID:    guestUserID,
		CheckoutToken:  paymentResp.CheckoutToken,
		PaymentGateway: req.PaymentGateway,
		Amount:         paymentResp.Amount,
		Currency:       paymentResp.Currency,
		Status:         "pending",
		GatewayData:    map[string]interface{}{},
		ExpiresAt:      *paymentResp.ExpiresAt,
	}

	// Load payment intent
	var paymentIntent models.PaymentIntent
	if err := uo.db.Where("checkout_token = ?", paymentResp.CheckoutToken).First(&paymentIntent).Error; err != nil {
		return nil, fmt.Errorf("failed to load payment intent: %w", err)
	}
	checkoutSession.PaymentIntentID = &paymentIntent.ID

	// Save checkout session
	if err := uo.db.Create(checkoutSession).Error; err != nil {
		return nil, fmt.Errorf("failed to save checkout session: %w", err)
	}

	// Initialize gateway-specific data (creates Stripe session)
	tempTicket := &models.Ticket{
		EventID: req.EventID,
		TierID:  req.Tiers[0].TierID,
	}
	// Load the event for tempTicket to avoid nil pointer dereference
	var event models.Event
	if err := uo.db.Where("id = ?", req.EventID).First(&event).Error; err != nil {
		return nil, fmt.Errorf("failed to load event for temp ticket: %w", err)
	}
	tempTicket.Event = &event

	var guestUserForGateway *models.GuestUser
	if guestUserID != nil {
		if err := uo.db.Where("id = ?", guestUserID).First(&guestUserForGateway).Error; err != nil {
			return nil, fmt.Errorf("failed to load guest user: %w", err)
		}
	} else {
		// For new guest users, find or create guest user by email
		var existingGuest models.GuestUser
		if err := uo.db.Where("email = ?", customerEmail).First(&existingGuest).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				// Create new guest user
				newGuest := &models.GuestUser{
					Email:       customerEmail,
					FirstName:   req.FirstName,
					LastName:    req.LastName,
					Phone:       req.Phone,
					CountryCode: req.CountryCode,
				}
				if err := uo.db.Create(newGuest).Error; err != nil {
					return nil, fmt.Errorf("failed to create guest user: %w", err)
				}
				guestUserForGateway = newGuest
			} else {
				return nil, fmt.Errorf("failed to find guest user: %w", err)
			}
		} else {
			guestUserForGateway = &existingGuest
		}
	}

	guestPurchaseReq := &models.GuestPurchaseRequest{
		EventID:        req.EventID,
		Email:          customerEmail,
		FirstName:      firstName,
		LastName:       lastName,
		Phone:          req.Phone,
		CountryCode:    req.CountryCode,
		Tiers:          req.Tiers,
		PaymentGateway: req.PaymentGateway,
	}

	// Initialize gateway data based on user type
	var gatewayErr error
	if userID != nil {
		gatewayErr = uo.ticketService.initializeUserGatewayData(checkoutSession, guestPurchaseReq, tempTicket, *userID)
	} else {
		gatewayErr = uo.ticketService.initializeGatewayData(checkoutSession, guestPurchaseReq, tempTicket, guestUserForGateway)
	}

	if gatewayErr != nil {
		// Clean up if gateway init fails
		uo.db.Delete(checkoutSession)
		return nil, fmt.Errorf("failed to initialize gateway: %w", gatewayErr)
	}

	// Save updated checkout session with gateway data
	if err := uo.db.Save(checkoutSession).Error; err != nil {
		return nil, err
	}

	// Extract redirect URL from gateway data
	redirectURL := ""
	if url, ok := checkoutSession.GatewayData["url"].(string); ok {
		redirectURL = url
	}

	return &UnifiedPurchaseResponse{
		CheckoutToken:       paymentResp.CheckoutToken,
		UserID:              userID,
		GuestUserID:         guestUserID,
		Email:               customerEmail,
		PaymentGateway:      string(req.PaymentGateway),
		Amount:              paymentResp.Amount,
		Currency:            paymentResp.Currency,
		Status:              "pending",
		GatewayData:         checkoutSession.GatewayData,
		TicketCount:         totalQuantity,
		ImmediateCompletion: false,
		RedirectURL:         redirectURL,
		ExpiresAt:           *paymentResp.ExpiresAt,
	}, nil
}

// normalizeAndValidateIdentity validates and normalizes user identity
// Returns (userID, guestUserID, email, error)
func (uo *UnifiedPurchaseOrchestrator) normalizeAndValidateIdentity(
	ctx context.Context,
	req *UnifiedPurchaseRequest,
) (*uuid.UUID, *uuid.UUID, string, error) {

	// Case 1: Logged-in user purchase (UserID provided)
	if req.UserID != nil {
		var user models.User
		if err := uo.db.Where("id = ?", req.UserID).First(&user).Error; err != nil {
			return nil, nil, "", fmt.Errorf("user not found: %w", err)
		}
		return req.UserID, nil, user.Email, nil
	}

	// Case 2: Existing guest user (GuestUserID provided)
	if req.GuestUserID != nil {
		var guest models.GuestUser
		if err := uo.db.Where("id = ?", req.GuestUserID).First(&guest).Error; err != nil {
			return nil, nil, "", fmt.Errorf("guest user not found: %w", err)
		}
		return nil, req.GuestUserID, guest.Email, nil
	}

	// Case 3: New guest user (Email provided)
	if req.Email != "" {
		// Email is sufficient for new guest purchases
		return nil, nil, req.Email, nil
	}

	return nil, nil, "", utils.NewBusinessLogicError("Either user_id, guest_user_id, or email must be provided")
}

// validatePurchaseRequest validates the purchase request
func (uo *UnifiedPurchaseOrchestrator) validatePurchaseRequest(
	ctx context.Context,
	req *UnifiedPurchaseRequest,
) error {

	// Validate event exists
	var event models.Event
	if err := uo.db.Where("id = ?", req.EventID).First(&event).Error; err != nil {
		return fmt.Errorf("event not found: %w", err)
	}

	// Validate all tiers exist and belong to event
	for _, tierSelection := range req.Tiers {
		var tier models.EventTier
		if err := uo.db.Where("id = ? AND event_id = ?", tierSelection.TierID, req.EventID).First(&tier).Error; err != nil {
			return fmt.Errorf("tier not found: %w", err)
		}

		if tierSelection.Quantity < 1 {
			return utils.NewBusinessLogicError("Quantity must be at least 1")
		}
	}

	return nil
}
