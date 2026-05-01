package services

// import (
// 	"context"
// 	"errors"
// 	"fmt"
// 	"log"
// 	"strings"
// 	"time"

// 	"event-ticketing-backend/internal/gateways"
// 	"event-ticketing-backend/internal/models"
// 	"event-ticketing-backend/pkg/config"
// 	"event-ticketing-backend/pkg/utils"

// 	"github.com/google/uuid"
// 	"gorm.io/gorm"
// 	"gorm.io/gorm/clause"
// )

// // generateCheckoutToken creates a unique token for payment checkout
// func generateCheckoutToken() string {
// 	return utils.GenerateCheckoutToken("checkout")
// }

// // PaymentService handles all payment operations
// type PaymentService struct {
// 	db                 *gorm.DB
// 	ticketService      *TicketService
// 	emailQueueService  *EmailQueueService
// 	emailOutboxService *EmailOutboxService
// 	cfg                *config.Config
// }

// // NewPaymentService creates a new payment service instance
// func NewPaymentService(db *gorm.DB, cfg *config.Config) *PaymentService {
// 	return &PaymentService{
// 		db:  db,
// 		cfg: cfg,
// 	}
// }

// // SetTicketService sets the ticket service dependency
// func (s *PaymentService) SetTicketService(ticketService *TicketService) {
// 	s.ticketService = ticketService
// }

// // SetEmailQueueService sets the email queue service dependency
// func (s *PaymentService) SetEmailQueueService(emailQueueService *EmailQueueService) {
// 	s.emailQueueService = emailQueueService
// }

// // SetEmailOutboxService sets the email outbox service dependency
// func (s *PaymentService) SetEmailOutboxService(emailOutboxService *EmailOutboxService) {
// 	s.emailOutboxService = emailOutboxService
// }

// // formatCountryCode ensures country code has + prefix for phone country codes
// // Example: "977" becomes "+977", "+977" stays "+977"
// func formatCountryCode(code string) string {
// 	if code == "" {
// 		return ""
// 	}
// 	if code[0] != '+' {
// 		return "+" + code
// 	}
// 	return code
// }

// // isCashPaymentAllowed checks if cash payments are allowed for the given email
// func (s *PaymentService) isCashPaymentAllowed(email string) bool {
// 	allowedEmails := s.cfg.Payment.CashAllowedEmails
// 	if len(allowedEmails) == 0 {
// 		return false // No emails allowed if list is empty
// 	}

// 	for _, allowedEmail := range allowedEmails {
// 		if allowedEmail == email {
// 			return true
// 		}
// 	}
// 	return false
// }

// // InitiatePaymentRequest represents a request to initiate a payment
// // swagger:model InitiatePaymentRequest
// type InitiatePaymentRequest struct {
// 	// Unique identifier of the event
// 	// required: true
// 	// example: 550e8400-e29b-41d4-a716-446655440000
// 	EventID uuid.UUID `json:"event_id" binding:"required"`

// 	// Array of ticket tier selections for multi-tier support
// 	// required: true (either tiers or legacy tier_id+quantity for backward compatibility)
// 	// example: [{"tier_id": "550e8400-e29b-41d4-a716-446655440001", "quantity": 2}]
// 	Tiers []models.TicketTierSelection `json:"tiers,omitempty" binding:"omitempty,dive"`

// 	// ===== DEPRECATED (for backward compatibility with old clients) =====
// 	// Unique identifier of the ticket tier (deprecated - use 'tiers' array instead)
// 	// required: false (only required if 'tiers' is not provided)
// 	// example: 550e8400-e29b-41d4-a716-446655440001
// 	TierID uuid.UUID `json:"tier_id,omitempty"`

// 	// Number of tickets to purchase (deprecated - use 'tiers' array instead)
// 	// required: false (only required if 'tiers' is not provided)
// 	// minimum: 1
// 	// maximum: 10
// 	// example: 2
// 	Quantity int `json:"quantity,omitempty"`

// 	// Currency code (ISO 4217, 3 letters)
// 	// required: true
// 	// example: USD
// 	Currency string `json:"currency" binding:"required,len=3"`

// 	// Preferred payment gateway (optional, auto-selected if not provided)
// 	// required: false
// 	// example: stripe
// 	PaymentGateway string `json:"payment_gateway,omitempty"`

// 	// User ID for authenticated users (optional)
// 	// required: false
// 	// example: 550e8400-e29b-41d4-a716-446655440002
// 	UserID *uuid.UUID `json:"user_id,omitempty"`

// 	// Guest user ID for anonymous users (optional)
// 	// required: false
// 	// example: 550e8400-e29b-41d4-a716-446655440003
// 	GuestUserID *uuid.UUID `json:"guest_user_id,omitempty"`

// 	// Customer email address
// 	// required: true
// 	// example: customer@example.com
// 	CustomerEmail string `json:"customer_email" binding:"required,email"`

// 	// Customer full name (optional)
// 	// required: false
// 	// example: John Doe
// 	CustomerName string `json:"customer_name,omitempty"`

// 	// Customer phone number (optional)
// 	// required: false
// 	// example: +1234567890
// 	CustomerPhone string `json:"customer_phone,omitempty"`

// 	// Country code for gateway selection with phone prefix
// 	// required: false
// 	// example: +977
// 	CountryCode string `json:"country_code,omitempty"`
// }

// // InitiatePaymentResponse represents the response from payment initiation
// // swagger:model InitiatePaymentResponse
// type InitiatePaymentResponse struct {
// 	// Unique identifier for the payment intent
// 	// example: 550e8400-e29b-41d4-a716-446655440004
// 	PaymentIntentID uuid.UUID `json:"payment_intent_id"`

// 	// Selected payment gateway
// 	// example: stripe
// 	PaymentGateway string `json:"payment_gateway"`

// 	// Redirect URL for payment completion (PayPal, etc.)
// 	// example: https://paypal.com/pay/...
// 	RedirectURL string `json:"redirect_url,omitempty"`

// 	// Total payment amount
// 	// example: 25.50
// 	Amount float64 `json:"amount"`

// 	// Payment currency
// 	// example: USD
// 	Currency string `json:"currency"`

// 	// Payment status
// 	// example: pending
// 	Status string `json:"status"`

// 	// IDs of reserved tickets
// 	// example: ["550e8400-e29b-41d4-a716-446655440005", "550e8400-e29b-41d4-a716-446655440006"]
// 	TicketIDs []uuid.UUID `json:"ticket_ids"`

// 	// Payment expiration timestamp
// 	// example: 2026-02-10T16:30:00Z
// 	ExpiresAt *time.Time `json:"expires_at,omitempty"`
// }

// // InitiatePayment creates a payment intent and reserves tickets
// // SECURITY: No sensitive card data is handled - all payment details collected by gateway
// func (s *PaymentService) InitiatePayment(ctx context.Context, req *InitiatePaymentRequest) (*InitiatePaymentResponse, error) {
// 	// 1. Validate event and tier
// 	var event models.Event
// 	if err := s.db.Preload("Organizer").First(&event, req.EventID).Error; err != nil {
// 		return nil, fmt.Errorf("event not found: %w", err)
// 	}

// 	var tier models.EventTier
// 	if err := s.db.Where("id = ? AND event_id = ?", req.TierID, req.EventID).First(&tier).Error; err != nil {
// 		return nil, fmt.Errorf("tier not found: %w", err)
// 	}

// 	// 2. PRE-CHECK FOR UX ONLY (not enforcement)
// 	// ⚠️  IMPORTANT: This check is STALE and for UX feedback only!
// 	//
// 	// WHY THIS IS NOT THE REAL ENFORCEMENT:
// 	// Between this check and the atomic UPDATE below:
// 	// - Another concurrent request can reserve tickets
// 	// - This check becomes invalid
// 	//
// 	// REAL ENFORCEMENT IS IN STEP 3 (atomic DB UPDATE with WHERE clause)
// 	// Only the DB UPDATE with conditions is guaranteed to work.
// 	//
// 	// EXPECTED BEHAVIOR (not a bug):
// 	// - User sees "10 available" in UI (from this pre-check)
// 	// - User initiates checkout
// 	// - Another user reserves tickets immediately
// 	// - User's atomic UPDATE fails: "Insufficient at checkout"
// 	// - This is CORRECT behavior for high-load scenarios
// 	//
// 	// CLIENT SIDE HANDLING:
// 	// - IF reservation fails: Show "Tickets just sold out. Refresh or try another tier."
// 	// - DO NOT show generic error to user
// 	if tier.Available < req.Quantity {
// 		return nil, utils.NewBusinessLogicError("Insufficient tickets available.")
// 	}

// 	// 3. Calculate pricing
// 	subtotal := tier.Price * float64(req.Quantity)
// 	commissionRate := event.CommissionRate
// 	commissionAmount := subtotal * (commissionRate / 100)
// 	totalAmount := subtotal + commissionAmount

// 	// 4. Select payment gateway
// 	var selectedGateway string

// 	if req.PaymentGateway != "" {
// 		// Special validation for cash payments
// 		if req.PaymentGateway == string(models.PaymentGatewayCash) {
// 			if !s.isCashPaymentAllowed(req.CustomerEmail) {
// 				return nil, utils.NewBusinessLogicError("Cash payments are not allowed for this email address.")
// 			}
// 		}
// 		selectedGateway = req.PaymentGateway
// 	} else {
// 		// Default to Stripe for auto-selection
// 		selectedGateway = string(models.PaymentGatewayStripe)
// 	}

// 	// Validate gateway is supported (allow any string for flexibility)
// 	// No validation needed - accept any payment gateway string

// 	// 5. Calculate gateway fees and determine status
// 	var paymentStatus string

// 	// Calculate gateway fees (simplified for now)
// 	// gatewayFee = 0 // TODO: Implement fee calculation per gateway

// 	// Determine payment status based on gateway
// 	if selectedGateway == string(models.PaymentGatewayCash) {
// 		paymentStatus = "pending" // Cash payments need manual collection
// 	} else {
// 		paymentStatus = "pending" // Other gateways also pending without actual integration
// 	}

// 	// 6. Generate idempotency key
// 	idempotencyKey := fmt.Sprintf("purchase-%s-%s-%s-%d", req.EventID, strings.ReplaceAll(req.CustomerEmail, "@", "_at_"), uuid.New().String(), time.Now().UnixNano())

// 	// 8. Start database transaction
// 	tx := s.db.Begin()
// 	defer func() {
// 		if r := recover(); r != nil {
// 			tx.Rollback()
// 		}
// 	}()

// 	// 7a. Create payment_intent record (PURE ORCHESTRATION LAYER)
// 	expiresAt := time.Now().Add(15 * time.Minute)
// 	paymentIntent := &models.PaymentIntent{
// 		IdempotencyKey: idempotencyKey,
// 		UserID:         req.UserID,
// 		GuestUserID:    req.GuestUserID,
// 		EventID:        req.EventID,
// 		TierID:         req.TierID,
// 		Quantity:       req.Quantity,
// 		AmountTotal:    int64(totalAmount * 100), // Convert dollars to cents
// 		Currency:       req.Currency,
// 		Status:         paymentStatus,
// 		ExpiresAt:      &expiresAt, // CRITICAL: TTL for reservation (15 minutes)
// 		CheckoutToken:  generateCheckoutToken(),
// 	}

// 	// 7b. Create payment_attempt record (GATEWAY-SPECIFIC ABSTRACTION LAYER)
// 	paymentAttempt := &models.PaymentAttempt{
// 		PaymentIntentID:   paymentIntent.ID, // Link to orchestration
// 		Provider:          selectedGateway,  // stripe, paypal, esewa, etc.
// 		Amount:            int64(totalAmount * 100),
// 		Currency:          req.Currency,
// 		Status:            "initiated",
// 		PaymentMethodType: "",
// 		InitiatedAt:       time.Now(),
// 		ExpiresAt:         &expiresAt,
// 	}

// 	if err := tx.Create(paymentIntent).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to create payment intent record: %w", err)
// 	}

// 	// Create payment attempt (link orchestration to gateway)
// 	paymentAttempt.PaymentIntentID = paymentIntent.ID
// 	if err := tx.Create(paymentAttempt).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to create payment attempt record: %w", err)
// 	}

// 	// 8. ATOMIC RESERVATION WITH DB-LEVEL INVENTORY ENFORCEMENT
// 	// This is the critical guard against overbooking:
// 	//   remaining_capacity = (quantity - sold - reserved)
// 	//   only reserve if: remaining_capacity >= requested_quantity
// 	// If another user took the last tickets concurrently, this WHERE fails
// 	// and RowsAffected == 0 (atomic enforcement, not just checking)

// 	result := tx.Model(&models.EventTier{}).
// 		Where("id = ? AND (quantity - sold - reserved) >= ?", tier.ID, req.Quantity).
// 		Update("reserved", gorm.Expr("reserved + ?", req.Quantity))

// 	if result.Error != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to reserve tickets: %w", result.Error)
// 	}

// 	// RowsAffected == 0 means the WHERE clause failed
// 	// (not enough capacity remaining - another user took them concurrently)
// 	if result.RowsAffected == 0 {
// 		tx.Rollback()
// 		return nil, utils.NewBusinessLogicError("Insufficient tickets available at checkout. Another customer may have just purchased. Please try again.")
// 	}

// 	// 9. Create audit log for reservation lifecycle
// 	auditLog := map[string]interface{}{
// 		"id":                uuid.New(),
// 		"payment_intent_id": paymentIntent.ID,
// 		"event_tier_id":     tier.ID,
// 		"action":            "reserved",
// 		"quantity":          req.Quantity,
// 		"reason":            "Payment initiated",
// 		"created_at":        time.Now(),
// 	}
// 	if err := tx.Table("reservation_audits").Create(auditLog).Error; err != nil {
// 		// Log but don't fail - audit is non-critical
// 		log.Printf("[WARN] Failed to create reservation audit: %v", err)
// 	}

// 	// 10. Commit transaction
// 	if err := tx.Commit().Error; err != nil {
// 		return nil, fmt.Errorf("failed to commit transaction: %w", err)
// 	}

// 	// 11. Log audit with event_id
// 	s.logAudit(ctx, "payment_initiated", "payment_intent", paymentIntent.ID, req.UserID, "user", &req.EventID, map[string]interface{}{})

// 	// 12. Return response (NO TICKET IDS YET - they're created on webhook success)
// 	return &InitiatePaymentResponse{
// 		PaymentIntentID: paymentIntent.ID,
// 		PaymentGateway:  selectedGateway,
// 		RedirectURL:     "", // No redirect URL for simplified implementation
// 		Amount:          totalAmount,
// 		Currency:        req.Currency,
// 		Status:          paymentStatus,
// 		TicketIDs:       []uuid.UUID{}, // EMPTY until webhook success
// 		ExpiresAt:       &expiresAt,    // Show customer: "Your payment expires in 15 minutes"
// 	}, nil
// }

// // Helper functions

// func getCurrencySymbol(currency string) string {
// 	symbols := map[string]string{
// 		"USD": "$",
// 		"EUR": "€",
// 		"GBP": "£",
// 		"NPR": "रू",
// 		"INR": "₹",
// 		"CAD": "C$",
// 		"AUD": "A$",
// 		"JPY": "¥",
// 	}
// 	if symbol, ok := symbols[currency]; ok {
// 		return symbol
// 	}
// 	return currency
// }

// // mapRefundReasonToStripe converts internal refund reasons to Stripe's accepted values
// // Stripe only accepts: duplicate, fraudulent, requested_by_customer
// func (s *PaymentService) mapRefundReasonToStripe(customReason, refundType string) string {
// 	// Default to requested_by_customer (most common case)
// 	defaultReason := "requested_by_customer"

// 	// Map refund type to Stripe reason
// 	switch refundType {
// 	case "duplicate":
// 		return "duplicate"
// 	case "fraudulent":
// 		return "fraudulent"
// 	case "customer_request", "customer_initiated":
// 		return "requested_by_customer"
// 	case "event_cancellation":
// 		return "requested_by_customer" // Event cancellation is customer request from platform perspective
// 	case "partial_refund":
// 		return "requested_by_customer"
// 	default:
// 		// If reason contains keywords, map accordingly
// 		lowerReason := strings.ToLower(customReason)
// 		if strings.Contains(lowerReason, "duplicate") {
// 			return "duplicate"
// 		}
// 		if strings.Contains(lowerReason, "fraud") {
// 			return "fraudulent"
// 		}
// 		return defaultReason
// 	}
// }

// func (s *PaymentService) logAudit(ctx context.Context, action, entityType string, entityID uuid.UUID, actorID *uuid.UUID, actorType string, eventID *uuid.UUID, changes map[string]interface{}) {
// 	audit := &models.PaymentAuditLog{
// 		Action:     action,
// 		EntityType: entityType,
// 		EntityID:   entityID,
// 		ActorID:    actorID,
// 		ActorType:  actorType,
// 		EventID:    eventID,
// 		Timestamp:  time.Now(),
// 	}

// 	if changes != nil {
// 		audit.ChangesAfter = changes
// 	}

// 	// Log async to avoid blocking
// 	go func() {
// 		s.db.Create(audit)
// 	}()
// }

// // GetPaymentIntentByID retrieves a payment intent by ID
// func (s *PaymentService) GetPaymentIntentByID(ctx context.Context, paymentIntentID uuid.UUID) (*models.PaymentIntent, error) {
// 	var paymentIntent models.PaymentIntent
// 	if err := s.db.Preload("Event").Preload("Tier").First(&paymentIntent, paymentIntentID).Error; err != nil {
// 		return nil, fmt.Errorf("payment intent not found: %w", err)
// 	}
// 	return &paymentIntent, nil
// }

// // CancelPayment cancels a pending payment
// func (s *PaymentService) CancelPayment(ctx context.Context, paymentIntentID, userID uuid.UUID) error {
// 	tx := s.db.Begin()
// 	defer func() {
// 		if r := recover(); r != nil {
// 			tx.Rollback()
// 		}
// 	}()

// 	var paymentIntent models.PaymentIntent
// 	if err := tx.First(&paymentIntent, paymentIntentID).Error; err != nil {
// 		tx.Rollback()
// 		return fmt.Errorf("payment intent not found: %w", err)
// 	}

// 	// Check authorization
// 	if paymentIntent.UserID == nil || *paymentIntent.UserID != userID {
// 		return utils.NewBusinessLogicError("Unauthorized to cancel this payment.")
// 	}

// 	// Only allow canceling pending/requires_action statuses
// 	if paymentIntent.Status != "pending" && paymentIntent.Status != "requires_action" && paymentIntent.Status != "requires_payment_method" {
// 		return utils.NewBusinessLogicError("Cannot cancel payment in current status.")
// 	}

// 	// Simplified - no gateway cancellation needed for current implementation
// 	// TODO: Implement gateway-specific cancellation when Stripe is integrated

// 	// Update status
// 	paymentIntent.Status = "canceled"
// 	if err := tx.Save(&paymentIntent).Error; err != nil {
// 		tx.Rollback()
// 		return fmt.Errorf("failed to update payment intent: %w", err)
// 	}

// 	// Cancel tickets and restore inventory
// 	if err := tx.Model(&models.Ticket{}).
// 		Where("event_id = ? AND tier_id = ? AND status = ? AND user_id = ?",
// 			paymentIntent.EventID, paymentIntent.TierID, "pending_payment", userID).
// 		Limit(paymentIntent.Quantity).
// 		Update("status", "canceled").Error; err != nil {
// 		tx.Rollback()
// 		return fmt.Errorf("failed to cancel tickets: %w", err)
// 	}

// 	var tier models.EventTier
// 	if err := tx.First(&tier, paymentIntent.TierID).Error; err != nil {
// 		tx.Rollback()
// 		return fmt.Errorf("tier not found: %w", err)
// 	}

// 	if err := tx.Model(&tier).Updates(map[string]interface{}{
// 		"sold":      gorm.Expr("sold - ?", paymentIntent.Quantity),
// 		"available": gorm.Expr("available + ?", paymentIntent.Quantity),
// 	}).Error; err != nil {
// 		tx.Rollback()
// 		return fmt.Errorf("failed to restore inventory: %w", err)
// 	}

// 	if err := tx.Commit().Error; err != nil {
// 		return fmt.Errorf("failed to commit transaction: %w", err)
// 	}

// 	s.logAudit(ctx, "payment_canceled", "payment_intent", paymentIntent.ID, &userID, "user", &paymentIntent.EventID, map[string]interface{}{
// 		"reason": "user_initiated_cancellation",
// 		"status": "cancelled",
// 	})

// 	return nil
// }

// // GetUserPayments retrieves user's payment history with pagination
// func (s *PaymentService) GetUserPayments(ctx context.Context, userID uuid.UUID, status string, page, limit int) ([]models.PaymentIntent, int64, error) {
// 	var payments []models.PaymentIntent
// 	var total int64

// 	query := s.db.Where("user_id = ?", userID)
// 	if status != "" {
// 		query = query.Where("status = ?", status)
// 	}

// 	query.Model(&models.PaymentIntent{}).Count(&total)

// 	offset := (page - 1) * limit
// 	if err := query.Preload("Event").Preload("Tier").
// 		Order("created_at DESC").
// 		Offset(offset).
// 		Limit(limit).
// 		Find(&payments).Error; err != nil {
// 		return nil, 0, fmt.Errorf("failed to retrieve payments: %w", err)
// 	}

// 	return payments, total, nil
// }

// // RequestRefund creates a refund request
// func (s *PaymentService) RequestRefund(ctx context.Context, paymentIntentID, userID uuid.UUID, reason string, ticketIDs []uuid.UUID) (*models.Refund, error) {
// 	tx := s.db.Begin()
// 	defer func() {
// 		if r := recover(); r != nil {
// 			tx.Rollback()
// 		}
// 	}()

// 	var paymentIntent models.PaymentIntent
// 	if err := tx.First(&paymentIntent, paymentIntentID).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("payment intent not found: %w", err)
// 	}

// 	// Check authorization
// 	if paymentIntent.UserID == nil || *paymentIntent.UserID != userID {
// 		tx.Rollback()
// 		return nil, utils.NewBusinessLogicError("Unauthorized.")
// 	}

// 	// Only succeeded payments can be refunded
// 	if paymentIntent.Status != "succeeded" {
// 		tx.Rollback()
// 		return nil, utils.NewBusinessLogicError("Only succeeded payments can be refunded")
// 	}

// 	// Validate refund conditions
// 	if err := s.validateRefundConditions(ctx, paymentIntentID, ticketIDs); err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("refund not allowed: %w", err)
// 	}

// 	// Calculate refund amount based on tickets
// 	refundAmount := float64(paymentIntent.AmountTotal) * (float64(len(ticketIDs)) / float64(paymentIntent.Quantity)) / 100

// 	// Find the transaction ID associated with this payment intent
// 	var transaction models.Transaction
// 	if err := tx.Where("payment_intent_id = ?", paymentIntentID).First(&transaction).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("transaction not found for payment intent: %w", err)
// 	}

// 	// Capture charge ID at creation time (don't rely on fetching PaymentIntent later)
// 	gatewayMetadata := map[string]interface{}{}

// 	// Get the charge ID from PaymentAttempt
// 	var paymentAttempt models.PaymentAttempt
// 	if err := tx.Where("payment_intent_id = ?", paymentIntentID).First(&paymentAttempt).Error; err == nil {
// 		if paymentAttempt.ProviderChargeID != "" {
// 			gatewayMetadata["stripe_charge_id"] = paymentAttempt.ProviderChargeID
// 		}
// 	} else {
// 		log.Printf("[REFUND] Warning: No payment attempt found for payment intent %s. Refund processing may fail.", paymentIntentID)
// 	}

// 	refund := &models.Refund{
// 		PaymentIntentID:  paymentIntentID,
// 		TransactionID:    transaction.ID,            // Set the transaction ID
// 		ProviderRefundID: "",                        // Will be set when approved
// 		Amount:           int64(refundAmount * 100), // Convert to cents
// 		Currency:         paymentIntent.Currency,
// 		Reason:           reason,
// 		Status:           "pending",
// 		InitiatedBy:      &userID,
// 		GatewayMetadata:  gatewayMetadata, // Store charge ID for later processing
// 		TicketCount:      len(ticketIDs),
// 		AffectedTicketIDs: func() []string {
// 			ids := make([]string, len(ticketIDs))
// 			for i, id := range ticketIDs {
// 				ids[i] = id.String()
// 			}
// 			return ids
// 		}(),
// 	}

// 	if err := tx.Create(refund).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to create refund request: %w", err)
// 	}

// 	// Log initial status
// 	if err := s.LogRefundStatusChange(ctx, tx, refund.ID, "", "pending", &userID, "user", "Refund request created", nil); err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to log initial status change: %w", err)
// 	}

// 	if err := tx.Commit().Error; err != nil {
// 		return nil, fmt.Errorf("failed to commit refund request: %w", err)
// 	}

// 	// Get event ID for audit logging
// 	var eventID *uuid.UUID
// 	var pi models.PaymentIntent
// 	if err := s.db.Select("event_id").First(&pi, paymentIntentID).Error; err == nil {
// 		eventID = &pi.EventID
// 	}

// 	changes := map[string]interface{}{}
// 	if eventID != nil {
// 		changes["event_id"] = eventID
// 	}
// 	changes["reason"] = refund.Reason
// 	changes["amount"] = refund.Amount
// 	changes["ticket_count"] = refund.TicketCount

// 	s.logAudit(ctx, "refund_requested", "refund", refund.ID, &userID, "user", &paymentIntent.EventID, changes)

// 	return refund, nil
// }

// // validateRefundConditions checks if a refund request meets all business requirements
// func (s *PaymentService) validateRefundConditions(ctx context.Context, paymentIntentID uuid.UUID, ticketIDs []uuid.UUID) error {
// 	// Get all tickets for validation
// 	var tickets []models.Ticket
// 	if err := s.db.Where("id IN ? AND transaction_id IN (SELECT id FROM transactions WHERE payment_intent_id = ?)", ticketIDs, paymentIntentID).
// 		Preload("Event").
// 		Preload("Transaction").
// 		Find(&tickets).Error; err != nil {
// 		return fmt.Errorf("failed to fetch tickets for validation: %w", err)
// 	}

// 	if len(tickets) != len(ticketIDs) {
// 		return utils.NewBusinessLogicError("Some tickets not found or don't belong to this payment intent.")
// 	}

// 	// Check each ticket for refund eligibility
// 	for _, ticket := range tickets {
// 		// 1. Check ticket status
// 		if ticket.Status == "refunded" {
// 			return utils.NewBusinessLogicError(fmt.Sprintf("Ticket %s has already been refunded", ticket.TicketNumber))
// 		}
// 		if ticket.Status == "cancelled" {
// 			return utils.NewBusinessLogicError(fmt.Sprintf("Ticket %s is already cancelled", ticket.TicketNumber))
// 		}
// 		if ticket.Status == "used" || ticket.CheckInTime != nil {
// 			return utils.NewBusinessLogicError(fmt.Sprintf("Ticket %s has been checked in and cannot be refunded", ticket.TicketNumber))
// 		}

// 		// 2. Check event status
// 		if ticket.Event == nil {
// 			return utils.NewBusinessLogicError("Event information not available for ticket validation")
// 		}
// 		if ticket.Event.IsCancelled || ticket.Event.Status == "cancelled" {
// 			return utils.NewBusinessLogicError(fmt.Sprintf("Cannot refund tickets for cancelled event: %s", ticket.Event.Title))
// 		}
// 		if ticket.Event.Status == "completed" {
// 			return utils.NewBusinessLogicError(fmt.Sprintf("Cannot refund tickets for completed event: %s", ticket.Event.Title))
// 		}

// 		// 3. Check event timing - no refunds within 24 hours of event start
// 		now := time.Now()
// 		timeUntilEvent := ticket.Event.StartDate.Sub(now)
// 		if timeUntilEvent < 24*time.Hour {
// 			return utils.NewBusinessLogicError(fmt.Sprintf("Refunds not allowed within 24 hours of event start. Event starts at: %s", ticket.Event.StartDate.Format("2006-01-02 15:04:05")))
// 		}

// 		// 4. Check purchase timing - no refunds within 1 hour of purchase (prevent immediate cancellations)
// 		timeSincePurchase := now.Sub(ticket.CreatedAt)
// 		if timeSincePurchase < 1*time.Hour {
// 			return utils.NewBusinessLogicError(fmt.Sprintf("Refunds not allowed within 1 hour of purchase. Purchase time: %s", ticket.CreatedAt.Format("2006-01-02 15:04:05")))
// 		}

// 		// 5. Check event sales status
// 		if ticket.Event.SalesStatus == "stopped" {
// 			return utils.NewBusinessLogicError(fmt.Sprintf("Ticket sales have been stopped for event: %s", ticket.Event.Title))
// 		}
// 	}

// 	return nil
// }

// // AdminGetAllPayments retrieves all payments with filters (admin only)
// func (s *PaymentService) AdminGetAllPayments(ctx context.Context, status, gateway string, eventID *uuid.UUID, page, limit int) ([]models.PaymentIntent, int64, error) {
// 	var payments []models.PaymentIntent
// 	var total int64

// 	query := s.db.Model(&models.PaymentIntent{})
// 	if status != "" {
// 		query = query.Where("status = ?", status)
// 	}
// 	if gateway != "" {
// 		query = query.Where("payment_gateway = ?", gateway)
// 	}
// 	if eventID != nil {
// 		query = query.Where("event_id = ?", *eventID)
// 	}

// 	query.Count(&total)

// 	offset := (page - 1) * limit
// 	if err := query.Preload("Event").Preload("Tier").
// 		Order("created_at DESC").
// 		Offset(offset).
// 		Limit(limit).
// 		Find(&payments).Error; err != nil {
// 		return nil, 0, fmt.Errorf("failed to retrieve payments: %w", err)
// 	}

// 	return payments, total, nil
// }

// // getPaymentGateway returns the gateway implementation for the given name
// func (s *PaymentService) getPaymentGateway(gatewayName string) gateways.PaymentGateway {
// 	switch gatewayName {
// 	case "stripe":
// 		if s.cfg != nil && s.cfg.Payment.Gateways.StripeAPIKey != "" {
// 			return gateways.NewStripeGateway(
// 				s.cfg.Payment.Gateways.StripeAPIKey,
// 				s.cfg.Payment.Gateways.StripeWebhookSecret,
// 				s.cfg.Payment.SuccessURL,
// 				s.cfg.Payment.CancelURL,
// 			)
// 		}
// 	}
// 	return nil
// }

// // CreatePaymentResponse represents the response from atomic payment creation
// type CreatePaymentResponse struct {
// 	PaymentIntentID uuid.UUID
// 	CheckoutToken   string
// 	PaymentGateway  string
// 	RedirectURL     string
// 	Amount          float64
// 	Currency        string
// 	Status          string
// 	ReservedTickets []uuid.UUID
// 	ExpiresAt       *time.Time
// }

// // HandlePaymentSuccess processes a successful payment (from webhook)
// // IDEMPOTENT: Safe to call multiple times - no side effects from duplicates
// // SECURITY: Uses row-level locking to prevent race conditions
// func (s *PaymentService) HandlePaymentSuccess(ctx context.Context, gatewayPaymentID string, gateway string) (*models.PaymentIntent, error) {
// 	tx := s.db.WithContext(ctx).Begin()
// 	defer func() {
// 		if r := recover(); r != nil {
// 			tx.Rollback()
// 		}
// 	}()

// 	// 1. LOCK: Find and lock payment intent row
// 	// FOR UPDATE ensures only one webhook handler processes this payment
// 	var paymentIntent models.PaymentIntent
// 	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}). // Row-level lock
// 									Where("payment_gateway = ? AND gateway_payment_id = ?", gateway, gatewayPaymentID).
// 									First(&paymentIntent).Error; err != nil {
// 		if errors.Is(err, gorm.ErrRecordNotFound) {
// 			// Payment intent doesn't exist - create it from webhook data
// 			// This handles the case where webhook arrives before DB insert
// 			return nil, utils.NewBusinessLogicError("Payment intent not found. Retry webhook processing.")
// 		}
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to lock payment intent: %w", err)
// 	}

// 	// 2. IDEMPOTENCY CHECK: Already processed?
// 	if paymentIntent.Status == "succeeded" {
// 		// Already processed - just return it (idempotent)
// 		tx.Rollback()
// 		return &paymentIntent, nil
// 	}

// 	// 3. UPDATE PAYMENT STATUS
// 	paymentIntent.Status = "succeeded"
// 	now := time.Now()
// 	paymentIntent.SucceededAt = &now

// 	if err := tx.Save(&paymentIntent).Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to update payment intent: %w", err)
// 	}

// 	// 4. ACTIVATE TICKETS (update from pending_payment to active)
// 	if err := tx.Model(&models.Ticket{}).
// 		Where("payment_intent_id IS NULL AND user_id = ? AND guest_user_id = ? AND event_id = ? AND status = ?",
// 			paymentIntent.UserID, paymentIntent.GuestUserID, paymentIntent.EventID, "pending_payment").
// 		Limit(paymentIntent.Quantity).
// 		Update("status", "active").Error; err != nil {
// 		tx.Rollback()
// 		return nil, fmt.Errorf("failed to activate tickets: %w", err)
// 	}

// 	// 5. COMMIT
// 	// NOTE: Transaction records are now created in payment_worker.processPaymentIntentSucceeded()
// 	// via webhook processing. This avoids duplicate transaction creation.
// 	if err := tx.Commit().Error; err != nil {
// 		return nil, fmt.Errorf("failed to commit transaction: %w", err)
// 	}

// 	s.logAudit(ctx, "payment_succeeded", "payment_intent", paymentIntent.ID, paymentIntent.UserID, "user", &paymentIntent.EventID, map[string]interface{}{})

// 	return &paymentIntent, nil
// }

// // VerifyPayment is a fallback endpoint for recovering from missed/failed webhooks
// // Used when webhooks fail, network issues, or Stripe delays occur
// // Returns: true if payment succeeded and processed, false if still pending, error if failed
// func (s *PaymentService) VerifyPayment(ctx context.Context, checkoutToken string) (*models.PaymentIntent, error) {
// 	var paymentIntent models.PaymentIntent
// 	if err := s.db.WithContext(ctx).
// 		Where("checkout_token = ?", checkoutToken).
// 		First(&paymentIntent).Error; err != nil {
// 		return nil, fmt.Errorf("payment not found")
// 	}

// 	// If already succeeded, just return
// 	if paymentIntent.Status == "succeeded" {
// 		return &paymentIntent, nil
// 	}

// 	// If still pending, check with Stripe
// 	// TODO: Implement per-gateway verification logic
// 	// This would call gateway API to check actual status

// 	return &paymentIntent, nil
// }

// // LogRefundStatusChange logs a refund status change to the refund_status_history table
// func (s *PaymentService) LogRefundStatusChange(ctx context.Context, tx *gorm.DB, refundID uuid.UUID, oldStatus, newStatus models.PaymentStatus, changedByID *uuid.UUID, changedByType, remarks string, metadata map[string]interface{}) error {
// 	statusHistory := &models.RefundStatusHistory{
// 		RefundID:      refundID,
// 		OldStatus:     oldStatus,
// 		NewStatus:     newStatus,
// 		ChangedByID:   changedByID,
// 		ChangedByType: changedByType,
// 		Remarks:       remarks,
// 		Metadata:      metadata,
// 		ChangedAt:     time.Now(),
// 	}

// 	if tx != nil {
// 		if err := tx.WithContext(ctx).Create(statusHistory).Error; err != nil {
// 			log.Printf("[REFUND] Warning: Failed to log refund status change: %v", err)
// 			return err
// 		}
// 	} else {
// 		if err := s.db.WithContext(ctx).Create(statusHistory).Error; err != nil {
// 			log.Printf("[REFUND] Warning: Failed to log refund status change: %v", err)
// 			return err
// 		}
// 	}

// 	return nil
// }

// // GetUserRefundStatusHistory retrieves refund status history for a specific refund belonging to the user
// func (s *PaymentService) GetUserRefundStatusHistory(ctx context.Context, userID uuid.UUID, refundID uuid.UUID) ([]models.RefundStatusHistoryResponse, error) {
// 	var response []models.RefundStatusHistoryResponse

// 	// Check if the ID is a Refund ID
// 	var refund models.Refund
// 	if err := s.db.Where("id = ?", refundID).Preload("Transaction").Preload("PaymentIntent").First(&refund).Error; err == nil {
// 		// Verify the refund belongs to the user
// 		if refund.PaymentIntent.UserID == nil || *refund.PaymentIntent.UserID != userID {
// 			// Check if it's a guest user with matching email
// 			if refund.PaymentIntent.GuestUserID == nil {
// 				return nil, fmt.Errorf("refund not found or access denied")
// 			}
// 			var guestUser models.GuestUser
// 			if err := s.db.Where("id = ?", refund.PaymentIntent.GuestUserID).First(&guestUser).Error; err != nil {
// 				return nil, fmt.Errorf("refund not found or access denied")
// 			}
// 			var user models.User
// 			if err := s.db.Where("email = ?", guestUser.Email).First(&user).Error; err != nil || user.ID != userID {
// 				return nil, fmt.Errorf("refund not found or access denied")
// 			}
// 		}

// 		// It's a Refund ID, get its status history
// 		var history []models.RefundStatusHistory
// 		if err := s.db.Where("refund_id = ?", refundID).
// 			Order("changed_at DESC").
// 			Preload("ChangedBy").
// 			Find(&history).Error; err != nil {
// 			return nil, fmt.Errorf("failed to fetch refund status history: %w", err)
// 		}

// 		response = make([]models.RefundStatusHistoryResponse, len(history))
// 		for i, h := range history {
// 			response[i] = models.RefundStatusHistoryResponse{
// 				ID:            h.ID,
// 				RefundID:      h.RefundID,
// 				OldStatus:     h.OldStatus,
// 				NewStatus:     h.NewStatus,
// 				ChangedByID:   h.ChangedByID,
// 				ChangedByType: models.PaymentStatus(h.ChangedByType),
// 				Remarks:       h.Remarks,
// 				Metadata:      h.Metadata,
// 				ChangedAt:     h.ChangedAt,
// 			}

// 			// Add changed by user info if available
// 			if h.ChangedBy != nil {
// 				response[i].ChangedBy = &models.UserSummary{
// 					ID:    h.ChangedBy.ID,
// 					Name:  h.ChangedBy.FirstName + " " + h.ChangedBy.LastName,
// 					Email: h.ChangedBy.Email,
// 				}
// 			}
// 		}
// 		return response, nil
// 	}

// 	// Check if the ID is a RefundRequest ID
// 	var refundRequest models.RefundRequest
// 	if err := s.db.Where("id = ?", refundID).
// 		Where("(user_id = ? OR guest_user_id IN (SELECT id FROM guest_users WHERE email IN (SELECT email FROM users WHERE id = ?)))", userID, userID).
// 		Preload("Transaction").
// 		Preload("ProcessedBy").
// 		First(&refundRequest).Error; err == nil {
// 		// It's a RefundRequest ID, create synthetic history
// 		response = []models.RefundStatusHistoryResponse{}

// 		// Add creation entry
// 		response = append(response, models.RefundStatusHistoryResponse{
// 			ID:            uuid.New(), // Synthetic ID
// 			RefundID:      refundRequest.ID,
// 			OldStatus:     "",
// 			NewStatus:     "pending",
// 			ChangedByID:   refundRequest.UserID,
// 			ChangedByType: "user",
// 			Remarks:       "Refund request created",
// 			Metadata:      nil,
// 			ChangedAt:     refundRequest.CreatedAt,
// 		})

// 		// Add processing entry if processed
// 		if refundRequest.Status != "pending" {
// 			changedByType := "admin"
// 			var changedByID *uuid.UUID
// 			var remarks string

// 			if refundRequest.Status == "approved" {
// 				remarks = "Refund request approved"
// 				changedByID = refundRequest.ProcessedByID
// 			} else if refundRequest.Status == "rejected" {
// 				remarks = "Refund request rejected"
// 				changedByID = refundRequest.ProcessedByID
// 			}

// 			response = append(response, models.RefundStatusHistoryResponse{
// 				ID:            uuid.New(), // Synthetic ID
// 				RefundID:      refundRequest.ID,
// 				OldStatus:     "pending",
// 				NewStatus:     refundRequest.Status,
// 				ChangedByID:   changedByID,
// 				ChangedByType: changedByType,
// 				Remarks:       remarks,
// 				Metadata:      nil,
// 				ChangedAt:     *refundRequest.ProcessedAt,
// 			})
// 		}

// 		// If approved, also include the actual refund status history
// 		if refundRequest.Status == "approved" {
// 			// Find the associated refund
// 			var refund models.Refund
// 			if err := s.db.Where("transaction_id = ?", refundRequest.TransactionID).
// 				Where("status != 'failed'").
// 				Order("created_at DESC").
// 				First(&refund).Error; err == nil {
// 				var refundHistory []models.RefundStatusHistory
// 				if err := s.db.Where("refund_id = ?", refund.ID).
// 					Order("changed_at DESC").
// 					Preload("ChangedBy").
// 					Find(&refundHistory).Error; err == nil {
// 					for _, h := range refundHistory {
// 						response = append(response, models.RefundStatusHistoryResponse{
// 							ID:            h.ID,
// 							RefundID:      h.RefundID,
// 							OldStatus:     h.OldStatus,
// 							NewStatus:     h.NewStatus,
// 							ChangedByID:   h.ChangedByID,
// 							ChangedByType: h.ChangedByType,
// 							Remarks:       h.Remarks,
// 							Metadata:      h.Metadata,
// 							ChangedAt:     h.ChangedAt,
// 						})

// 						// Add changed by user info if available
// 						if h.ChangedBy != nil {
// 							response[len(response)-1].ChangedBy = &models.UserSummary{
// 								ID:    h.ChangedBy.ID,
// 								Name:  h.ChangedBy.FirstName + " " + h.ChangedBy.LastName,
// 								Email: h.ChangedBy.Email,
// 							}
// 						}
// 					}
// 				}
// 			}
// 		}

// 		return response, nil
// 	}

// 	// ID not found in either table
// 	return nil, fmt.Errorf("refund not found")
// }
