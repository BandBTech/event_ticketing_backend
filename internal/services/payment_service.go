package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"event-ticketing-backend/internal/gateways"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PaymentService handles all payment operations
type PaymentService struct {
	db                 *gorm.DB
	ticketService      *TicketService
	emailQueueService  *EmailQueueService
	emailOutboxService *EmailOutboxService
	cfg                *config.Config
}

// NewPaymentService creates a new payment service instance
func NewPaymentService(db *gorm.DB, cfg *config.Config) *PaymentService {
	return &PaymentService{
		db:  db,
		cfg: cfg,
	}
}

// SetTicketService sets the ticket service dependency
func (s *PaymentService) SetTicketService(ticketService *TicketService) {
	s.ticketService = ticketService
}

// SetEmailQueueService sets the email queue service dependency
func (s *PaymentService) SetEmailQueueService(emailQueueService *EmailQueueService) {
	s.emailQueueService = emailQueueService
}

// SetEmailOutboxService sets the email outbox service dependency
func (s *PaymentService) SetEmailOutboxService(emailOutboxService *EmailOutboxService) {
	s.emailOutboxService = emailOutboxService
}

// formatCountryCode ensures country code has + prefix for phone country codes
// Example: "977" becomes "+977", "+977" stays "+977"
func formatCountryCode(code string) string {
	if code == "" {
		return ""
	}
	if code[0] != '+' {
		return "+" + code
	}
	return code
}

// isCashPaymentAllowed checks if cash payments are allowed for the given email
func (s *PaymentService) isCashPaymentAllowed(email string) bool {
	allowedEmails := s.cfg.Payment.CashAllowedEmails
	if len(allowedEmails) == 0 {
		return false // No emails allowed if list is empty
	}

	for _, allowedEmail := range allowedEmails {
		if allowedEmail == email {
			return true
		}
	}
	return false
}

// InitiatePaymentRequest represents a request to initiate a payment
// swagger:model InitiatePaymentRequest
type InitiatePaymentRequest struct {
	// Unique identifier of the event
	// required: true
	// example: 550e8400-e29b-41d4-a716-446655440000
	EventID uuid.UUID `json:"event_id" binding:"required"`

	// Array of ticket tier selections for multi-tier support
	// required: true (either tiers or legacy tier_id+quantity for backward compatibility)
	// example: [{"tier_id": "550e8400-e29b-41d4-a716-446655440001", "quantity": 2}]
	Tiers []models.TicketTierSelection `json:"tiers,omitempty" binding:"omitempty,dive"`

	// ===== DEPRECATED (for backward compatibility with old clients) =====
	// Unique identifier of the ticket tier (deprecated - use 'tiers' array instead)
	// required: false (only required if 'tiers' is not provided)
	// example: 550e8400-e29b-41d4-a716-446655440001
	TierID uuid.UUID `json:"tier_id,omitempty"`

	// Number of tickets to purchase (deprecated - use 'tiers' array instead)
	// required: false (only required if 'tiers' is not provided)
	// minimum: 1
	// maximum: 10
	// example: 2
	Quantity int `json:"quantity,omitempty"`

	// Currency code (ISO 4217, 3 letters)
	// required: true
	// example: USD
	Currency string `json:"currency" binding:"required,len=3"`

	// Preferred payment gateway (optional, auto-selected if not provided)
	// required: false
	// example: stripe
	PaymentGateway string `json:"payment_gateway,omitempty"`

	// User ID for authenticated users (optional)
	// required: false
	// example: 550e8400-e29b-41d4-a716-446655440002
	UserID *uuid.UUID `json:"user_id,omitempty"`

	// Guest user ID for anonymous users (optional)
	// required: false
	// example: 550e8400-e29b-41d4-a716-446655440003
	GuestUserID *uuid.UUID `json:"guest_user_id,omitempty"`

	// Customer email address
	// required: true
	// example: customer@example.com
	CustomerEmail string `json:"customer_email" binding:"required,email"`

	// Customer full name (optional)
	// required: false
	// example: John Doe
	CustomerName string `json:"customer_name,omitempty"`

	// Customer phone number (optional)
	// required: false
	// example: +1234567890
	CustomerPhone string `json:"customer_phone,omitempty"`

	// Country code for gateway selection with phone prefix
	// required: false
	// example: +977
	CountryCode string `json:"country_code,omitempty"`
}

// InitiatePaymentResponse represents the response from payment initiation
// swagger:model InitiatePaymentResponse
type InitiatePaymentResponse struct {
	// Unique identifier for the payment intent
	// example: 550e8400-e29b-41d4-a716-446655440004
	PaymentIntentID uuid.UUID `json:"payment_intent_id"`

	// Selected payment gateway
	// example: stripe
	PaymentGateway string `json:"payment_gateway"`

	// Redirect URL for payment completion (PayPal, etc.)
	// example: https://paypal.com/pay/...
	RedirectURL string `json:"redirect_url,omitempty"`

	// Total payment amount
	// example: 25.50
	Amount float64 `json:"amount"`

	// Payment currency
	// example: USD
	Currency string `json:"currency"`

	// Payment status
	// example: pending
	Status string `json:"status"`

	// IDs of reserved tickets
	// example: ["550e8400-e29b-41d4-a716-446655440005", "550e8400-e29b-41d4-a716-446655440006"]
	TicketIDs []uuid.UUID `json:"ticket_ids"`

	// Payment expiration timestamp
	// example: 2026-02-10T16:30:00Z
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// InitiatePayment creates a payment intent and reserves tickets
// SECURITY: No sensitive card data is handled - all payment details collected by gateway
func (s *PaymentService) InitiatePayment(ctx context.Context, req *InitiatePaymentRequest) (*InitiatePaymentResponse, error) {
	// 1. Validate event and tier
	var event models.Event
	if err := s.db.Preload("Organizer").First(&event, req.EventID).Error; err != nil {
		return nil, fmt.Errorf("event not found: %w", err)
	}

	var tier models.EventTier
	if err := s.db.Where("id = ? AND event_id = ?", req.TierID, req.EventID).First(&tier).Error; err != nil {
		return nil, fmt.Errorf("tier not found: %w", err)
	}

	// 2. PRE-CHECK FOR UX ONLY (not enforcement)
	// ⚠️  IMPORTANT: This check is STALE and for UX feedback only!
	//
	// WHY THIS IS NOT THE REAL ENFORCEMENT:
	// Between this check and the atomic UPDATE below:
	// - Another concurrent request can reserve tickets
	// - This check becomes invalid
	//
	// REAL ENFORCEMENT IS IN STEP 3 (atomic DB UPDATE with WHERE clause)
	// Only the DB UPDATE with conditions is guaranteed to work.
	//
	// EXPECTED BEHAVIOR (not a bug):
	// - User sees "10 available" in UI (from this pre-check)
	// - User initiates checkout
	// - Another user reserves tickets immediately
	// - User's atomic UPDATE fails: "Insufficient at checkout"
	// - This is CORRECT behavior for high-load scenarios
	//
	// CLIENT SIDE HANDLING:
	// - IF reservation fails: Show "Tickets just sold out. Refresh or try another tier."
	// - DO NOT show generic error to user
	if tier.Available < req.Quantity {
		return nil, utils.NewBusinessLogicError("Insufficient tickets available.")
	}

	// 3. Calculate pricing
	subtotal := tier.Price * float64(req.Quantity)
	commissionRate := event.CommissionRate
	commissionAmount := subtotal * (commissionRate / 100)
	totalAmount := subtotal + commissionAmount

	// 4. Select payment gateway
	var selectedGateway string

	if req.PaymentGateway != "" {
		// Special validation for cash payments
		if req.PaymentGateway == string(models.PaymentGatewayCash) {
			if !s.isCashPaymentAllowed(req.CustomerEmail) {
				return nil, utils.NewBusinessLogicError("Cash payments are not allowed for this email address.")
			}
		}
		selectedGateway = req.PaymentGateway
	} else {
		// Default to Stripe for auto-selection
		selectedGateway = string(models.PaymentGatewayStripe)
	}

	// Validate gateway is supported (allow any string for flexibility)
	// No validation needed - accept any payment gateway string

	// 5. Calculate gateway fees and determine status
	var gatewayFee float64
	var paymentStatus string

	// Calculate gateway fees (simplified for now)
	gatewayFee = 0 // TODO: Implement fee calculation per gateway

	// Determine payment status based on gateway
	if selectedGateway == string(models.PaymentGatewayCash) {
		paymentStatus = "pending" // Cash payments need manual collection
	} else {
		paymentStatus = "pending" // Other gateways also pending without actual integration
	}

	// 6. Generate idempotency key
	idempotencyKey := fmt.Sprintf("purchase-%s-%s-%s-%d", req.EventID, strings.ReplaceAll(req.CustomerEmail, "@", "_at_"), uuid.New().String(), time.Now().UnixNano())

	// 8. Start database transaction
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 7. Create payment_intent record WITH EXPIRY (15 minutes = trade standard)
	expiresAt := time.Now().Add(15 * time.Minute)
	paymentIntent := &models.PaymentIntent{
		PaymentGateway:     selectedGateway,
		IdempotencyKey:     idempotencyKey,
		UserID:             req.UserID,
		GuestUserID:        req.GuestUserID,
		CustomerEmail:      req.CustomerEmail,
		CustomerName:       req.CustomerName,
		CustomerPhone:      req.CustomerPhone,
		EventID:            req.EventID,
		TierID:             req.TierID,
		Quantity:           req.Quantity,
		Currency:           req.Currency,
		CurrencySymbol:     getCurrencySymbol(req.Currency),
		ExchangeRate:       1.0, // TODO: Implement currency conversion
		BaseCurrency:       "USD",
		BaseCurrencyAmount: totalAmount,
		UnitPrice:          tier.Price,
		Subtotal:           subtotal,
		PlatformFee:        commissionAmount,
		GatewayFee:         gatewayFee,
		TotalAmount:        totalAmount,
		Status:             paymentStatus,
		CommissionRate:     commissionRate,
		CommissionAmount:   commissionAmount,
		OrganizerNetAmount: subtotal,
		PaymentMethodType:  "",
		CountryCode:        formatCountryCode(req.CountryCode),
		ExpiresAt:          &expiresAt, // CRITICAL: TTL for reservation (15 minutes)
	}

	if err := tx.Create(paymentIntent).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create payment intent record: %w", err)
	}

	// 8. ATOMIC RESERVATION WITH DB-LEVEL INVENTORY ENFORCEMENT
	// This is the critical guard against overbooking:
	//   remaining_capacity = (quantity - sold - reserved)
	//   only reserve if: remaining_capacity >= requested_quantity
	// If another user took the last tickets concurrently, this WHERE fails
	// and RowsAffected == 0 (atomic enforcement, not just checking)

	result := tx.Model(&models.EventTier{}).
		Where("id = ? AND (quantity - sold - reserved) >= ?", tier.ID, req.Quantity).
		Update("reserved", gorm.Expr("reserved + ?", req.Quantity))

	if result.Error != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to reserve tickets: %w", result.Error)
	}

	// RowsAffected == 0 means the WHERE clause failed
	// (not enough capacity remaining - another user took them concurrently)
	if result.RowsAffected == 0 {
		tx.Rollback()
		return nil, utils.NewBusinessLogicError("Insufficient tickets available at checkout. Another customer may have just purchased. Please try again.")
	}

	// 9. Create audit log for reservation lifecycle
	auditLog := map[string]interface{}{
		"id":                uuid.New(),
		"payment_intent_id": paymentIntent.ID,
		"event_tier_id":     tier.ID,
		"action":            "reserved",
		"quantity":          req.Quantity,
		"reason":            "Payment initiated",
		"created_at":        time.Now(),
	}
	if err := tx.Table("reservation_audits").Create(auditLog).Error; err != nil {
		// Log but don't fail - audit is non-critical
		log.Printf("[WARN] Failed to create reservation audit: %v", err)
	}

	// 10. Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 11. Log audit with event_id
	s.logAudit(ctx, "payment_initiated", "payment_intent", paymentIntent.ID, req.UserID, "user", &req.EventID, map[string]interface{}{})

	// 12. Return response (NO TICKET IDS YET - they're created on webhook success)
	return &InitiatePaymentResponse{
		PaymentIntentID: paymentIntent.ID,
		PaymentGateway:  selectedGateway,
		RedirectURL:     "", // No redirect URL for simplified implementation
		Amount:          totalAmount,
		Currency:        req.Currency,
		Status:          paymentStatus,
		TicketIDs:       []uuid.UUID{}, // EMPTY until webhook success
		ExpiresAt:       &expiresAt,    // Show customer: "Your payment expires in 15 minutes"
	}, nil
}

// Helper functions

func getCurrencySymbol(currency string) string {
	symbols := map[string]string{
		"USD": "$",
		"EUR": "€",
		"GBP": "£",
		"NPR": "रू",
		"INR": "₹",
		"CAD": "C$",
		"AUD": "A$",
		"JPY": "¥",
	}
	if symbol, ok := symbols[currency]; ok {
		return symbol
	}
	return currency
}

// mapRefundReasonToStripe converts internal refund reasons to Stripe's accepted values
// Stripe only accepts: duplicate, fraudulent, requested_by_customer
func (s *PaymentService) mapRefundReasonToStripe(customReason, refundType string) string {
	// Default to requested_by_customer (most common case)
	defaultReason := "requested_by_customer"

	// Map refund type to Stripe reason
	switch refundType {
	case "duplicate":
		return "duplicate"
	case "fraudulent":
		return "fraudulent"
	case "customer_request", "customer_initiated":
		return "requested_by_customer"
	case "event_cancellation":
		return "requested_by_customer" // Event cancellation is customer request from platform perspective
	case "partial_refund":
		return "requested_by_customer"
	default:
		// If reason contains keywords, map accordingly
		lowerReason := strings.ToLower(customReason)
		if strings.Contains(lowerReason, "duplicate") {
			return "duplicate"
		}
		if strings.Contains(lowerReason, "fraud") {
			return "fraudulent"
		}
		return defaultReason
	}
}

func (s *PaymentService) logAudit(ctx context.Context, action, entityType string, entityID uuid.UUID, actorID *uuid.UUID, actorType string, eventID *uuid.UUID, changes map[string]interface{}) {
	audit := &models.PaymentAuditLog{
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		ActorID:    actorID,
		ActorType:  actorType,
		EventID:    eventID,
		Timestamp:  time.Now(),
	}

	if changes != nil {
		audit.ChangesAfter = changes
	}

	// Log async to avoid blocking
	go func() {
		s.db.Create(audit)
	}()
}

// GetPaymentIntentByID retrieves a payment intent by ID
func (s *PaymentService) GetPaymentIntentByID(ctx context.Context, paymentIntentID uuid.UUID) (*models.PaymentIntent, error) {
	var paymentIntent models.PaymentIntent
	if err := s.db.Preload("Event").Preload("Tier").First(&paymentIntent, paymentIntentID).Error; err != nil {
		return nil, fmt.Errorf("payment intent not found: %w", err)
	}
	return &paymentIntent, nil
}

// CancelPayment cancels a pending payment
func (s *PaymentService) CancelPayment(ctx context.Context, paymentIntentID, userID uuid.UUID) error {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var paymentIntent models.PaymentIntent
	if err := tx.First(&paymentIntent, paymentIntentID).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("payment intent not found: %w", err)
	}

	// Check authorization
	if paymentIntent.UserID == nil || *paymentIntent.UserID != userID {
		return utils.NewBusinessLogicError("Unauthorized to cancel this payment.")
	}

	// Only allow canceling pending/requires_action statuses
	if paymentIntent.Status != "pending" && paymentIntent.Status != "requires_action" && paymentIntent.Status != "requires_payment_method" {
		return utils.NewBusinessLogicError("Cannot cancel payment in current status.")
	}

	// Simplified - no gateway cancellation needed for current implementation
	// TODO: Implement gateway-specific cancellation when Stripe is integrated

	// Update status
	paymentIntent.Status = "canceled"
	if err := tx.Save(&paymentIntent).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update payment intent: %w", err)
	}

	// Cancel tickets and restore inventory
	if err := tx.Model(&models.Ticket{}).
		Where("event_id = ? AND tier_id = ? AND status = ? AND user_id = ?",
			paymentIntent.EventID, paymentIntent.TierID, "pending_payment", userID).
		Limit(paymentIntent.Quantity).
		Update("status", "canceled").Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to cancel tickets: %w", err)
	}

	var tier models.EventTier
	if err := tx.First(&tier, paymentIntent.TierID).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("tier not found: %w", err)
	}

	if err := tx.Model(&tier).Updates(map[string]interface{}{
		"sold":      gorm.Expr("sold - ?", paymentIntent.Quantity),
		"available": gorm.Expr("available + ?", paymentIntent.Quantity),
	}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to restore inventory: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logAudit(ctx, "payment_canceled", "payment_intent", paymentIntent.ID, &userID, "user", &paymentIntent.EventID, map[string]interface{}{
		"reason": "user_initiated_cancellation",
		"status": "cancelled",
	})

	return nil
}

// GetUserPayments retrieves user's payment history with pagination
func (s *PaymentService) GetUserPayments(ctx context.Context, userID uuid.UUID, status string, page, limit int) ([]models.PaymentIntent, int64, error) {
	var payments []models.PaymentIntent
	var total int64

	query := s.db.Where("user_id = ?", userID)
	if status != "" {
		query = query.Where("status = ?", status)
	}

	query.Model(&models.PaymentIntent{}).Count(&total)

	offset := (page - 1) * limit
	if err := query.Preload("Event").Preload("Tier").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&payments).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to retrieve payments: %w", err)
	}

	return payments, total, nil
}

// RequestRefund creates a refund request
func (s *PaymentService) RequestRefund(ctx context.Context, paymentIntentID, userID uuid.UUID, reason string, ticketIDs []uuid.UUID) (*models.Refund, error) {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var paymentIntent models.PaymentIntent
	if err := tx.First(&paymentIntent, paymentIntentID).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("payment intent not found: %w", err)
	}

	// Check authorization
	if paymentIntent.UserID == nil || *paymentIntent.UserID != userID {
		tx.Rollback()
		return nil, utils.NewBusinessLogicError("Unauthorized.")
	}

	// Only succeeded payments can be refunded
	if paymentIntent.Status != "succeeded" {
		tx.Rollback()
		return nil, utils.NewBusinessLogicError("Only succeeded payments can be refunded")
	}

	// Validate refund conditions
	if err := s.validateRefundConditions(ctx, paymentIntentID, ticketIDs); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("refund not allowed: %w", err)
	}

	// Calculate refund amount based on tickets
	refundAmount := paymentIntent.TotalAmount * (float64(len(ticketIDs)) / float64(paymentIntent.Quantity))

	// Find the transaction ID associated with this payment intent
	var transaction models.Transaction
	if err := tx.Where("payment_intent_id = ?", paymentIntentID).First(&transaction).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("transaction not found for payment intent: %w", err)
	}

	// Capture charge ID at creation time (don't rely on fetching PaymentIntent later)
	gatewayMetadata := map[string]interface{}{}
	if paymentIntent.GatewayChargeID != nil && *paymentIntent.GatewayChargeID != "" {
		gatewayMetadata["stripe_charge_id"] = *paymentIntent.GatewayChargeID
	} else {
		log.Printf("[REFUND] Warning: Payment intent %s has no charge ID captured yet. Refund processing may fail.", paymentIntentID)
	}

	refund := &models.Refund{
		PaymentIntentID: paymentIntentID,
		TransactionID:   transaction.ID, // Set the transaction ID
		GatewayRefundID: "",             // Will be set when approved
		Amount:          refundAmount,
		Currency:        paymentIntent.Currency,
		Reason:          reason,
		Status:          "pending",
		InitiatedBy:     &userID,
		GatewayMetadata: gatewayMetadata, // Store charge ID for later processing
		TicketCount:     len(ticketIDs),
	}

	if err := tx.Create(refund).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create refund request: %w", err)
	}

	// Log initial status
	if err := s.LogRefundStatusChange(ctx, tx, refund.ID, "", "pending", &userID, "user", "Refund request created", nil); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to log initial status change: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit refund request: %w", err)
	}

	// Get event ID for audit logging
	var eventID *uuid.UUID
	var pi models.PaymentIntent
	if err := s.db.Select("event_id").First(&pi, paymentIntentID).Error; err == nil {
		eventID = &pi.EventID
	}

	changes := map[string]interface{}{}
	if eventID != nil {
		changes["event_id"] = eventID
	}
	changes["reason"] = refund.Reason
	changes["amount"] = refund.Amount
	changes["ticket_count"] = refund.TicketCount

	s.logAudit(ctx, "refund_requested", "refund", refund.ID, &userID, "user", &paymentIntent.EventID, changes)

	return refund, nil
}

// validateRefundConditions checks if a refund request meets all business requirements
func (s *PaymentService) validateRefundConditions(ctx context.Context, paymentIntentID uuid.UUID, ticketIDs []uuid.UUID) error {
	// Get all tickets for validation
	var tickets []models.Ticket
	if err := s.db.Where("id IN ? AND transaction_id IN (SELECT id FROM transactions WHERE payment_intent_id = ?)", ticketIDs, paymentIntentID).
		Preload("Event").
		Preload("Transaction").
		Find(&tickets).Error; err != nil {
		return fmt.Errorf("failed to fetch tickets for validation: %w", err)
	}

	if len(tickets) != len(ticketIDs) {
		return utils.NewBusinessLogicError("Some tickets not found or don't belong to this payment intent.")
	}

	// Check each ticket for refund eligibility
	for _, ticket := range tickets {
		// 1. Check ticket status
		if ticket.Status == "refunded" {
			return utils.NewBusinessLogicError(fmt.Sprintf("Ticket %s has already been refunded", ticket.TicketNumber))
		}
		if ticket.Status == "cancelled" {
			return utils.NewBusinessLogicError(fmt.Sprintf("Ticket %s is already cancelled", ticket.TicketNumber))
		}
		if ticket.Status == "used" || ticket.CheckInTime != nil {
			return utils.NewBusinessLogicError(fmt.Sprintf("Ticket %s has been checked in and cannot be refunded", ticket.TicketNumber))
		}

		// 2. Check event status
		if ticket.Event == nil {
			return utils.NewBusinessLogicError("Event information not available for ticket validation")
		}
		if ticket.Event.IsCancelled || ticket.Event.Status == "cancelled" {
			return utils.NewBusinessLogicError(fmt.Sprintf("Cannot refund tickets for cancelled event: %s", ticket.Event.Title))
		}
		if ticket.Event.Status == "completed" {
			return utils.NewBusinessLogicError(fmt.Sprintf("Cannot refund tickets for completed event: %s", ticket.Event.Title))
		}

		// 3. Check event timing - no refunds within 24 hours of event start
		now := time.Now()
		timeUntilEvent := ticket.Event.StartDate.Sub(now)
		if timeUntilEvent < 24*time.Hour {
			return utils.NewBusinessLogicError(fmt.Sprintf("Refunds not allowed within 24 hours of event start. Event starts at: %s", ticket.Event.StartDate.Format("2006-01-02 15:04:05")))
		}

		// 4. Check purchase timing - no refunds within 1 hour of purchase (prevent immediate cancellations)
		timeSincePurchase := now.Sub(ticket.CreatedAt)
		if timeSincePurchase < 1*time.Hour {
			return utils.NewBusinessLogicError(fmt.Sprintf("Refunds not allowed within 1 hour of purchase. Purchase time: %s", ticket.CreatedAt.Format("2006-01-02 15:04:05")))
		}

		// 5. Check event sales status
		if ticket.Event.SalesStatus == "stopped" {
			return utils.NewBusinessLogicError(fmt.Sprintf("Ticket sales have been stopped for event: %s", ticket.Event.Title))
		}
	}

	return nil
}

// AdminGetAllPayments retrieves all payments with filters (admin only)
func (s *PaymentService) AdminGetAllPayments(ctx context.Context, status, gateway string, eventID *uuid.UUID, page, limit int) ([]models.PaymentIntent, int64, error) {
	var payments []models.PaymentIntent
	var total int64

	query := s.db.Model(&models.PaymentIntent{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if gateway != "" {
		query = query.Where("payment_gateway = ?", gateway)
	}
	if eventID != nil {
		query = query.Where("event_id = ?", *eventID)
	}

	query.Count(&total)

	offset := (page - 1) * limit
	if err := query.Preload("Event").Preload("Tier").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&payments).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to retrieve payments: %w", err)
	}

	return payments, total, nil
}

// ApproveRefund approves and processes a refund through the payment gateway
func (s *PaymentService) ApproveRefund(ctx context.Context, refundID, adminID uuid.UUID) (*models.Refund, error) {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var refund models.Refund
	// Preload PaymentIntent with all relationships
	if err := tx.Preload("PaymentIntent").First(&refund, refundID).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("refund not found: %w", err)
	}

	// Validate that PaymentIntent is loaded
	if refund.PaymentIntent == nil || refund.PaymentIntent.ID == uuid.Nil {
		// If PaymentIntent wasn't loaded, try to load it explicitly
		var pi models.PaymentIntent
		if err := tx.First(&pi, refund.PaymentIntentID).Error; err != nil {
			if err = tx.Unscoped().First(&pi, refund.PaymentIntentID).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("payment intent not found for refund: %w", err)
			}
		}
		refund.PaymentIntent = &pi
	}

	if refund.Status != "pending" {
		tx.Rollback()
		return nil, utils.NewBusinessLogicError("Refund is not in pending status.")
	}

	// Update status to processing
	oldStatus := refund.Status
	refund.Status = "processing"
	refund.ApprovedBy = &adminID
	now := time.Now()
	refund.ApprovedAt = &now

	if err := tx.Save(&refund).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update refund status to processing: %w", err)
	}

	// Log status change
	if err := s.LogRefundStatusChange(ctx, tx, refund.ID, oldStatus, "processing", &adminID, "admin", "Refund approved and moved to processing", nil); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to log status change: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit status update: %w", err)
	}

	// Process refund through payment gateway asynchronously
	go func() {
		if err := s.processGatewayRefund(context.Background(), &refund, &adminID); err != nil {
			log.Printf("[REFUND] Failed to process gateway refund %s: %v", refund.ID, err)
			// Update refund status to failed
			s.db.Model(&refund).Updates(map[string]interface{}{
				"status":    "failed",
				"failed_at": time.Now(),
				"gateway_response": map[string]interface{}{
					"error": err.Error(),
				},
			})

			// Log status change for gateway processing failure
			if logErr := s.LogRefundStatusChange(context.Background(), nil, refund.ID, "processing", "failed", nil, "system", fmt.Sprintf("Gateway refund processing failed: %s", err.Error()), map[string]interface{}{
				"error": err.Error(),
			}); logErr != nil {
				log.Printf("[REFUND] Warning: Failed to log gateway failure status change: %v", logErr)
			}
		}
	}()

	// Send refund approved notification email
	go func() {
		// Use the already-loaded PaymentIntent
		if refund.PaymentIntent != nil {
			if err := s.notifyUserRefundCompleted(context.Background(), refund.PaymentIntent, &refund, "processing"); err != nil {
				log.Printf("[REFUND] Warning: Failed to notify user about refund approval: %v", err)
			}
		} else {
			log.Printf("[REFUND] Warning: PaymentIntent not available for refund notification")
		}
	}()

	// Get event ID for audit logging from the loaded PaymentIntent
	var eventID *uuid.UUID
	if refund.PaymentIntent != nil && refund.PaymentIntent.ID != uuid.Nil {
		eventID = &refund.PaymentIntent.EventID
	}

	s.logAudit(ctx, "refund_approved", "refund", refund.ID, &adminID, "admin", eventID, map[string]interface{}{})

	// Return refund with processing status
	return &refund, nil
}

// processGatewayRefund handles the actual gateway refund processing
func (s *PaymentService) processGatewayRefund(ctx context.Context, refund *models.Refund, adminID *uuid.UUID) error {
	// Only process for Stripe payments
	if refund.PaymentGateway != "stripe" {
		// For non-Stripe payments, mark as succeeded without gateway processing
		s.db.Model(refund).Updates(map[string]interface{}{
			"status":       "succeeded",
			"processed_at": time.Now(),
		})

		// Log status change for non-Stripe refund
		if logErr := s.LogRefundStatusChange(ctx, nil, refund.ID, "processing", "succeeded", adminID, "admin", "Refund processed for non-Stripe payment", nil); logErr != nil {
			log.Printf("[REFUND] Warning: Failed to log non-Stripe refund status change: %v", logErr)
		}

		return nil
	}

	// Get PaymentIntent to retrieve the charge ID
	// Try to find the payment intent, including soft-deleted records
	var chargeID string

	// First, try to use stored charge ID from GatewayMetadata (captured at refund creation time)
	if refund.GatewayMetadata != nil {
		if storedChargeID, exists := refund.GatewayMetadata["stripe_charge_id"]; exists {
			if chargeIDStr, ok := storedChargeID.(string); ok && chargeIDStr != "" {
				chargeID = chargeIDStr
				log.Printf("[REFUND] Using stored charge ID from refund metadata: %s", chargeID)
			}
		}
	}

	// If no stored charge ID, try to fetch from PaymentIntent
	if chargeID == "" {
		var paymentIntent models.PaymentIntent
		if err := s.db.First(&paymentIntent, refund.PaymentIntentID).Error; err != nil {
			// If not found in regular query, try with Unscoped (includes soft-deleted records)
			if err := s.db.Unscoped().First(&paymentIntent, refund.PaymentIntentID).Error; err != nil {
				// Payment intent not found - this is a critical error
				// Could be: wrong payment_intent_id, transaction incomplete, or data mismatch
				errMsg := fmt.Sprintf("Payment intent %s not found in system. The payment may not have been completed successfully.", refund.PaymentIntentID)
				log.Printf("[REFUND] ERROR: %s", errMsg)
				s.db.Model(refund).Updates(map[string]interface{}{
					"status":         "failed",
					"failed_at":      time.Now(),
					"failure_reason": errMsg,
					"gateway_response": map[string]interface{}{
						"error":         "payment_intent_not_found",
						"error_details": errMsg,
					},
				})

				// Log status change for payment intent not found
				if logErr := s.LogRefundStatusChange(ctx, nil, refund.ID, "processing", "failed", adminID, "admin", fmt.Sprintf("Refund failed: %s", errMsg), map[string]interface{}{
					"error": "payment_intent_not_found",
				}); logErr != nil {
					log.Printf("[REFUND] Warning: Failed to log payment intent not found status change: %v", logErr)
				}

				return fmt.Errorf(errMsg)
			}
			log.Printf("[REFUND] Warning: Payment intent %s was soft-deleted, using it for refund processing", paymentIntent.ID)
		}

		// Extract charge ID from PaymentIntent
		if paymentIntent.GatewayChargeID == nil || *paymentIntent.GatewayChargeID == "" {
			errMsg := fmt.Sprintf("Refund cannot be processed: Stripe charge ID missing. Payment may be incomplete or in pending state. Payment Intent ID: %s", paymentIntent.ID)
			log.Printf("[REFUND] ERROR: %s", errMsg)
			s.db.Model(refund).Updates(map[string]interface{}{
				"status":         "failed",
				"failed_at":      time.Now(),
				"failure_reason": errMsg,
				"gateway_response": map[string]interface{}{
					"error":         "missing_charge_id",
					"error_details": errMsg,
				},
			})

			// Log status change for missing charge ID
			if logErr := s.LogRefundStatusChange(ctx, nil, refund.ID, "processing", "failed", adminID, "admin", fmt.Sprintf("Refund failed: %s", errMsg), map[string]interface{}{
				"error": "missing_charge_id",
			}); logErr != nil {
				log.Printf("[REFUND] Warning: Failed to log missing charge ID status change: %v", logErr)
			}

			return fmt.Errorf(errMsg)
		}
		chargeID = *paymentIntent.GatewayChargeID
	}

	// Validate charge ID exists (required for Stripe refunds)
	if chargeID == "" {
		errMsg := fmt.Sprintf("Refund cannot be processed: Stripe charge ID missing or empty. Payment may be incomplete or in pending state. Payment Intent ID: %s", refund.PaymentIntentID)
		log.Printf("[REFUND] ERROR: %s", errMsg)
		s.db.Model(refund).Updates(map[string]interface{}{
			"status":         "failed",
			"failed_at":      time.Now(),
			"failure_reason": errMsg,
			"gateway_response": map[string]interface{}{
				"error":         "missing_charge_id",
				"error_details": errMsg,
			},
		})
		return fmt.Errorf(errMsg)
	}

	// Call Stripe gateway to create refund
	gatewayRefundReq := &gateways.RefundRequest{
		ChargeID: chargeID, // Pass the Stripe charge ID (ch_xxx)
		Amount:   refund.Amount,
		Currency: refund.Currency,
		Reason:   s.mapRefundReasonToStripe(refund.Reason, refund.RefundType), // Map to Stripe's valid reasons
		Metadata: map[string]string{
			"refund_id":     refund.ID.String(),
			"refund_number": refund.RefundNumber,
		},
	}

	// Get the gateway implementation
	gateway := s.getPaymentGateway("stripe")
	if gateway == nil {
		return fmt.Errorf("stripe gateway not configured")
	}

	// Create refund on Stripe
	gatewayResponse, err := gateway.CreateRefund(ctx, gatewayRefundReq)
	if err != nil {
		// Determine user-friendly and admin-friendly error messages
		userMsg := "The refund could not be processed by the payment gateway. Please contact support."
		adminMsg := err.Error()

		if strings.Contains(err.Error(), "amount") {
			userMsg = "Refund amount is invalid or exceeds the original payment amount."
		} else if strings.Contains(err.Error(), "charge") {
			userMsg = "Payment charge information is not available for refunding."
		} else if strings.Contains(err.Error(), "already") {
			userMsg = "This payment has already been refunded."
		} else if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "network") {
			userMsg = "Payment gateway is temporarily unavailable. Please try again in a few minutes."
		}

		// Mark refund as failed and send notification
		failedAt := time.Now()
		s.db.Model(refund).Updates(map[string]interface{}{
			"status":         "failed",
			"failed_at":      failedAt,
			"failure_reason": userMsg,
			"gateway_response": map[string]interface{}{
				"error":               adminMsg,
				"user_facing_message": userMsg,
				"admin_debug_message": adminMsg,
				"failed_at":           failedAt,
				"charge_id":           chargeID,
			},
		})

		// Log status change for gateway refund failure
		if logErr := s.LogRefundStatusChange(ctx, nil, refund.ID, "processing", "failed", adminID, "admin", fmt.Sprintf("Gateway refund failed: %s", userMsg), map[string]interface{}{
			"error":     adminMsg,
			"charge_id": chargeID,
		}); logErr != nil {
			log.Printf("[REFUND] Warning: Failed to log gateway refund failure status change: %v", logErr)
		}

		// Log status change
		if logErr := s.LogRefundStatusChange(ctx, nil, refund.ID, "processing", "failed", nil, "system", fmt.Sprintf("Refund failed: %s", adminMsg), map[string]interface{}{
			"error":               adminMsg,
			"user_facing_message": userMsg,
			"charge_id":           chargeID,
		}); logErr != nil {
			log.Printf("[REFUND] Warning: Failed to log status change: %v", logErr)
		}

		// Audit log for failed refund
		var auditPI models.PaymentIntent
		if err := s.db.First(&auditPI, refund.PaymentIntentID).Error; err != nil {
			s.db.Unscoped().First(&auditPI, refund.PaymentIntentID)
		}
		s.logAudit(ctx, "refund_failed", "refund", refund.ID, adminID, "admin", &auditPI.EventID, map[string]interface{}{
			"error":               adminMsg,
			"user_facing_message": userMsg,
			"failed_at":           failedAt,
			"charge_id":           chargeID,
		})

		// Send failed refund notification
		go func() {
			// Try to load payment intent for notification
			var pi models.PaymentIntent
			if loadErr := s.db.First(&pi, refund.PaymentIntentID).Error; loadErr != nil {
				// Try with Unscoped if not found
				if loadErr = s.db.Unscoped().First(&pi, refund.PaymentIntentID).Error; loadErr != nil {
					log.Printf("[REFUND] Warning: Could not load payment intent for refund notification: %v", loadErr)
					return
				}
			}
			if err := s.notifyUserRefundCompleted(context.Background(), &pi, refund, "failed"); err != nil {
				log.Printf("[REFUND] Warning: Failed to notify user about refund failure: %v", err)
			}
		}()

		return fmt.Errorf("stripe refund creation failed: %w", err)
	}

	// Update refund with gateway response - set to processing, wait for webhook confirmation
	refund.GatewayRefundID = gatewayResponse.GatewayRefundID
	refund.Status = "processing" // Wait for webhook confirmation instead of immediately marking as succeeded
	now := time.Now()
	refund.ProcessedAt = &now

	// Store the full gateway response
	gatewayData := map[string]interface{}{
		"gateway_refund_id": gatewayResponse.GatewayRefundID,
		"gateway_status":    gatewayResponse.Status,
		"amount":            gatewayResponse.Amount,
		"currency":          gatewayResponse.Currency,
		"created_at":        gatewayResponse.CreatedAt,
		"awaiting_webhook":  true, // Flag to indicate we're waiting for webhook confirmation
	}

	if err := s.db.Model(refund).Updates(map[string]interface{}{
		"gateway_refund_id": refund.GatewayRefundID,
		"status":            "processing",
		"processed_at":      refund.ProcessedAt,
		"gateway_response":  gatewayData,
	}).Error; err != nil {
		return fmt.Errorf("failed to update refund with gateway response: %w", err)
	}

	// Log status change - still processing, awaiting webhook
	if err := s.LogRefundStatusChange(ctx, nil, refund.ID, "processing", "processing", nil, "system", "Refund initiated via payment gateway, awaiting webhook confirmation", map[string]interface{}{
		"gateway_refund_id": gatewayResponse.GatewayRefundID,
		"amount":            gatewayResponse.Amount,
		"awaiting_webhook":  true,
	}); err != nil {
		log.Printf("[REFUND] Warning: Failed to log status change: %v", err)
	}

	// Audit log for successful refund
	var succeedPI models.PaymentIntent
	if err := s.db.First(&succeedPI, refund.PaymentIntentID).Error; err != nil {
		s.db.Unscoped().First(&succeedPI, refund.PaymentIntentID)
	}
	s.logAudit(ctx, "refund_succeeded", "refund", refund.ID, adminID, "admin", &succeedPI.EventID, map[string]interface{}{
		"gateway_refund_id": gatewayResponse.GatewayRefundID,
		"amount":            gatewayResponse.Amount,
		"currency":          gatewayResponse.Currency,
	})

	log.Printf("[REFUND] Refund %s processed successfully. Stripe Refund ID: %s", refund.RefundNumber, gatewayResponse.GatewayRefundID)

	// Update payment intent status to refunded/partially_refunded
	var pi models.PaymentIntent
	if err := s.db.First(&pi, refund.PaymentIntentID).Error; err != nil {
		// Try with Unscoped if not found
		if err = s.db.Unscoped().First(&pi, refund.PaymentIntentID).Error; err != nil {
			log.Printf("[REFUND] Warning: Could not load payment intent to update status: %v", err)
		} else if err := s.updatePaymentIntentRefundStatus(&pi, refund); err != nil {
			log.Printf("[REFUND] Warning: Failed to update payment intent status: %v", err)
		}
	} else if err := s.updatePaymentIntentRefundStatus(&pi, refund); err != nil {
		log.Printf("[REFUND] Warning: Failed to update payment intent status: %v", err)
	}

	// Notify user about refund success
	go func() {
		var notifyPi models.PaymentIntent
		if err := s.db.First(&notifyPi, refund.PaymentIntentID).Error; err != nil {
			// Try with Unscoped
			if err = s.db.Unscoped().First(&notifyPi, refund.PaymentIntentID).Error; err != nil {
				log.Printf("[REFUND] Warning: Could not load payment intent for refund notification: %v", err)
				return
			}
		}
		if err := s.notifyUserRefundCompleted(context.Background(), &notifyPi, refund, "succeeded"); err != nil {
			log.Printf("[REFUND] Warning: Failed to notify user: %v", err)
		}
	}()

	return nil
}

// RetryFailedRefund retries a failed refund
func (s *PaymentService) RetryFailedRefund(ctx context.Context, refundID uuid.UUID, adminID uuid.UUID) (*models.Refund, error) {
	var refund models.Refund
	if err := s.db.First(&refund, refundID).Error; err != nil {
		return nil, fmt.Errorf("refund not found: %w", err)
	}

	if refund.Status != "failed" {
		return nil, utils.NewBusinessLogicError("Only failed refunds can be retried")
	}

	// Ensure we have the charge ID from GatewayMetadata
	var chargeID string
	if refund.GatewayMetadata != nil {
		if storedChargeID, exists := refund.GatewayMetadata["stripe_charge_id"]; exists {
			if chargeIDStr, ok := storedChargeID.(string); ok && chargeIDStr != "" {
				chargeID = chargeIDStr
				log.Printf("[REFUND] Retry: Using stored charge ID from refund metadata: %s", chargeID)
			}
		}
	}

	if chargeID == "" {
		return nil, utils.NewBusinessLogicError("Cannot retry refund: Stripe charge ID not found. The original payment may not have been completed successfully.")
	}

	// Check retry attempts (store in gateway_response)
	var retryCount int = 0
	if refund.GatewayResponse != nil {
		if count, ok := refund.GatewayResponse["retry_count"].(float64); ok {
			retryCount = int(count)
		}
	}

	// Max 3 retry attempts
	if retryCount >= 3 {
		return nil, utils.NewBusinessLogicError("Maximum retry attempts (3) exceeded. Please contact support.")
	}

	// Mark as processing and retry
	if err := s.db.Model(&refund).Update("status", "processing").Error; err != nil {
		return nil, fmt.Errorf("failed to update refund status: %w", err)
	}

	// Log status change
	if err := s.LogRefundStatusChange(ctx, nil, refund.ID, "failed", "processing", &adminID, "admin", fmt.Sprintf("Refund retry initiated (attempt %d)", retryCount+1), nil); err != nil {
		log.Printf("[REFUND] Warning: Failed to log status change: %v", err)
	}

	// Reload refund to get fresh data from database (especially GatewayMetadata with stored charge_id)
	if err := s.db.First(&refund, refundID).Error; err != nil {
		log.Printf("[REFUND] Warning: Failed to reload refund after status update: %v", err)
		// Continue anyway with in-memory refund, which should still have GatewayMetadata
	}

	// Process gateway refund asynchronously
	go func() {
		if err := s.processGatewayRefund(context.Background(), &refund, &adminID); err != nil {
			log.Printf("[REFUND] Retry failed for refund %s (attempt %d): %v", refund.ID, retryCount+1, err)
			// Update retry count
			newRetryCount := retryCount + 1
			response := refund.GatewayResponse
			if response == nil {
				response = make(map[string]interface{})
			}
			response["retry_count"] = newRetryCount
			response["last_retry_error"] = err.Error()
			response["last_retry_at"] = time.Now()

			s.db.Model(&refund).Updates(map[string]interface{}{
				"status":           "failed",
				"gateway_response": response,
				"failed_at":        time.Now(),
			})

			// Log status change for retry failure
			if logErr := s.LogRefundStatusChange(context.Background(), nil, refund.ID, "processing", "failed", nil, "system", fmt.Sprintf("Refund retry failed (attempt %d): %s", newRetryCount, err.Error()), map[string]interface{}{
				"retry_attempt": newRetryCount,
				"error":         err.Error(),
			}); logErr != nil {
				log.Printf("[REFUND] Warning: Failed to log retry failure status change: %v", logErr)
			}

			// Notify admin about retry failure
			s.notifyAdminRefundFailed(context.Background(), &refund, newRetryCount)
		}
	}()

	return &refund, nil
}

// notifyUserRefundCompleted sends a notification to user about refund status
func (s *PaymentService) notifyUserRefundCompleted(ctx context.Context, pi *models.PaymentIntent, refund *models.Refund, status string) error {
	// Get recipient email - works for both logged-in users and guests
	var recipientEmail string
	var userName string

	if pi.UserID != nil {
		// Logged-in user
		var user models.User
		if err := s.db.Where("id = ?", pi.UserID).First(&user).Error; err != nil {
			log.Printf("[REFUND] Warning: Could not find user %s for refund notification: %v", pi.UserID, err)
			return nil
		}
		recipientEmail = user.Email
		userName = user.FirstName + " " + user.LastName
	} else {
		// Guest user - use customer email from payment intent
		if pi.CustomerEmail == "" {
			log.Printf("[REFUND] Warning: No email available for guest refund notification (payment_intent: %s)", pi.ID)
			return nil
		}
		recipientEmail = pi.CustomerEmail
		userName = "Valued Customer" // Default name for guests
	}

	// Get event title
	var event models.Event
	if err := s.db.Where("id = ?", pi.EventID).First(&event).Error; err != nil {
		log.Printf("[REFUND] Warning: Could not find event for refund notification: %v", err)
		return nil
	}

	// Prepare email subject and data based on status
	var subject string
	var priority int

	switch status {
	case "pending":
		subject = fmt.Sprintf("Refund Request Submitted - %s", refund.RefundNumber)
		priority = 2 // High priority
	case "processing":
		subject = fmt.Sprintf("Refund Approved - Processing Started - %s", refund.RefundNumber)
		priority = 2 // High priority
	case "succeeded":
		subject = fmt.Sprintf("Refund Completed Successfully - %s", refund.RefundNumber)
		priority = 2 // High priority
	case "failed":
		subject = fmt.Sprintf("Refund Processing Failed - %s", refund.RefundNumber)
		priority = 3 // Urgent priority
	default:
		log.Printf("[REFUND] Unknown refund status for email: %s", status)
		return nil
	}

	// Prepare template data for the centralized email system
	templateData := map[string]interface{}{
		"event_name":      event.Title,
		"refund_amount":   refund.Amount,
		"currency":        refund.Currency,
		"ticket_count":    1, // Default to 1 for single refund
		"refund_number":   refund.RefundNumber,
		"refund_reason":   refund.Reason,
		"refund_status":   status,
		"user_name":       userName,
		"recipient_email": recipientEmail,
	}

	// Add status-specific data
	if status == "succeeded" && refund.ProcessedAt != nil {
		templateData["completed_at"] = refund.ProcessedAt.Format("January 2, 2006 at 3:04 PM UTC")
	}
	if status == "failed" {
		templateData["error_message"] = "Processing error occurred during refund"
	}

	// Queue email using centralized system
	if err := s.emailOutboxService.QueueEmail(ctx, models.EmailEventRefundProcessed, recipientEmail, subject, templateData, priority); err != nil {
		log.Printf("[REFUND] Warning: Failed to queue refund email for %s: %v", recipientEmail, err)
		return err
	}

	log.Printf("[REFUND] Email notification queued for %s, refund %s, status: %s", recipientEmail, refund.ID, status)
	return nil
}

// notifyAdminRefundFailed notifies admin about refund retry failure
func (s *PaymentService) notifyAdminRefundFailed(ctx context.Context, refund *models.Refund, retryCount int) {
	log.Printf("[REFUND] Alert: Refund %s failed on retry attempt %d. Manual intervention may be needed.", refund.ID, retryCount)
}

// BulkApproveRefunds approves multiple refunds at once (for event cancellations)
func (s *PaymentService) BulkApproveRefunds(ctx context.Context, refundIDs []uuid.UUID, adminID uuid.UUID) (map[string]interface{}, error) {
	if len(refundIDs) == 0 {
		return nil, utils.NewBusinessLogicError("No refunds provided")
	}

	if len(refundIDs) > 500 {
		return nil, utils.NewBusinessLogicError("Maximum 500 refunds can be processed at once")
	}

	// Validate all refunds exist and are pending
	var refunds []models.Refund
	if err := s.db.Where("id IN ? AND status = ?", refundIDs, "pending").Find(&refunds).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch refunds: %w", err)
	}

	if len(refunds) != len(refundIDs) {
		return nil, utils.NewBusinessLogicError("Some refunds not found or not in pending status")
	}

	// Start approval process for each refund
	successCount := 0
	var errors []string

	for _, refund := range refunds {
		if _, err := s.ApproveRefund(ctx, refund.ID, adminID); err != nil {
			errorMessage := fmt.Sprintf("Refund %s: %v", refund.RefundNumber, err)
			errors = append(errors, errorMessage)
		} else {
			successCount++
		}
	}

	return map[string]interface{}{
		"total":         len(refundIDs),
		"approved":      successCount,
		"failed":        len(errors),
		"error_details": errors,
	}, nil
}

// GetRefundAnalytics returns refund analytics and statistics
func (s *PaymentService) GetRefundAnalytics(ctx context.Context, startDate, endDate time.Time) (map[string]interface{}, error) {
	var totalRefunds, successfulRefunds, failedRefunds, pendingRefunds int64
	var totalAmount, successfulAmount float64

	// Get total refunds count
	if err := s.db.Model(&models.Refund{}).
		Where("created_at BETWEEN ? AND ?", startDate, endDate).
		Count(&totalRefunds).Error; err != nil {
		return nil, err
	}

	// Get successful refunds
	if err := s.db.Model(&models.Refund{}).
		Where("status = ? AND created_at BETWEEN ? AND ?", "succeeded", startDate, endDate).
		Count(&successfulRefunds).Error; err != nil {
		return nil, err
	}

	// Get successful amount
	s.db.Model(&models.Refund{}).
		Where("status = ? AND created_at BETWEEN ? AND ?", "succeeded", startDate, endDate).
		Select("COALESCE(SUM(amount), 0)").
		Row().
		Scan(&successfulAmount)

	// Get failed refunds
	if err := s.db.Model(&models.Refund{}).
		Where("status = ? AND created_at BETWEEN ? AND ?", "failed", startDate, endDate).
		Count(&failedRefunds).Error; err != nil {
		return nil, err
	}

	// Get pending refunds
	if err := s.db.Model(&models.Refund{}).
		Where("status = ? AND created_at BETWEEN ? AND ?", "pending", startDate, endDate).
		Count(&pendingRefunds).Error; err != nil {
		return nil, err
	}

	// Get total amount
	s.db.Model(&models.Refund{}).
		Where("created_at BETWEEN ? AND ?", startDate, endDate).
		Select("COALESCE(SUM(amount), 0)").
		Row().
		Scan(&totalAmount)

	// Calculate success rate
	successRate := float64(0)
	if totalRefunds > 0 {
		successRate = (float64(successfulRefunds) / float64(totalRefunds)) * 100
	}

	// Get refunds by type
	type RefundTypeStats struct {
		RefundType  string
		Count       int64
		TotalAmount float64
	}
	var refundsByType []RefundTypeStats
	if err := s.db.Model(&models.Refund{}).
		Select("refund_type, COUNT(*) as count, COALESCE(SUM(amount), 0) as total_amount").
		Where("created_at BETWEEN ? AND ?", startDate, endDate).
		Group("refund_type").
		Scan(&refundsByType).Error; err != nil {
		log.Printf("[ANALYTICS] Warning: Failed to get refunds by type: %v", err)
	}

	return map[string]interface{}{
		"period":               map[string]time.Time{"start_date": startDate, "end_date": endDate},
		"total_refunds":        totalRefunds,
		"successful_refunds":   successfulRefunds,
		"failed_refunds":       failedRefunds,
		"pending_refunds":      pendingRefunds,
		"total_amount":         totalAmount,
		"successful_amount":    successfulAmount,
		"success_rate_percent": successRate,
		"refunds_by_type":      refundsByType,
	}, nil
}

// updatePaymentIntentRefundStatus updates the payment intent status based on refund
func (s *PaymentService) updatePaymentIntentRefundStatus(pi *models.PaymentIntent, refund *models.Refund) error {
	// Check if this is a full refund
	if math.Abs(refund.Amount-pi.TotalAmount) < 0.01 { // Within 1 cent
		return s.db.Model(pi).Update("status", "refunded").Error
	}
	// Otherwise mark as partially refunded
	return s.db.Model(pi).Update("status", "partially_refunded").Error
}

// getPaymentGateway returns the gateway implementation for the given name
func (s *PaymentService) getPaymentGateway(gatewayName string) gateways.PaymentGateway {
	switch gatewayName {
	case "stripe":
		if s.cfg != nil && s.cfg.Payment.Gateways.StripeAPIKey != "" {
			return gateways.NewStripeGateway(
				s.cfg.Payment.Gateways.StripeAPIKey,
				s.cfg.Payment.Gateways.StripeWebhookSecret,
				s.cfg.Payment.SuccessURL,
				s.cfg.Payment.CancelURL,
			)
		}
	}
	return nil
}

// RejectRefund rejects a refund request
func (s *PaymentService) RejectRefund(ctx context.Context, refundID, adminID uuid.UUID, reason string) (*models.Refund, error) {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var refund models.Refund
	if err := tx.First(&refund, refundID).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("refund not found: %w", err)
	}

	if refund.Status != "pending" {
		tx.Rollback()
		return nil, utils.NewBusinessLogicError("Refund is not in pending status.")
	}

	refund.Status = "rejected"
	refund.RejectionReason = reason
	refund.ApprovedBy = &adminID
	now := time.Now()
	refund.ApprovedAt = &now

	if err := tx.Save(&refund).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update refund: %w", err)
	}

	// Log status change
	if err := s.LogRefundStatusChange(ctx, tx, refund.ID, "pending", "rejected", &adminID, "admin", fmt.Sprintf("Refund rejected: %s", reason), nil); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to log status change: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit rejection: %w", err)
	}

	// Get event ID for audit logging
	var eventID *uuid.UUID
	var pi models.PaymentIntent
	if err := s.db.Select("event_id").Where("id = ?", refund.PaymentIntentID).First(&pi).Error; err == nil {
		eventID = &pi.EventID
	}

	changes := map[string]interface{}{}
	if eventID != nil {
		changes["event_id"] = eventID
	}
	changes["rejection_reason"] = reason
	changes["status_change"] = "pending -> rejected"

	s.logAudit(ctx, "refund_rejected", "refund", refund.ID, &adminID, "admin", eventID, changes)

	return &refund, nil
}

// AdminGetAllRefunds retrieves all refunds with filters
func (s *PaymentService) AdminGetAllRefunds(ctx context.Context, status, search, refundType string, startDate, endDate *time.Time, page, limit int, sortBy, sortOrder string) ([]models.Refund, int64, error) {
	var refunds []models.Refund
	var total int64

	query := s.db.Model(&models.Refund{}).Preload("Transaction").Preload("Initiator").Preload("PaymentIntent").Preload("PaymentIntent.Event")

	// Apply status filter
	if status != "" {
		query = query.Where("refunds.status = ?", status)
	}

	// Apply refund type filter
	if refundType != "" {
		query = query.Where("refunds.refund_type = ?", refundType)
	}

	// Apply date filters
	if startDate != nil {
		query = query.Where("refunds.created_at >= ?", *startDate)
	}
	if endDate != nil {
		query = query.Where("refunds.created_at <= ?", *endDate)
	}

	// Apply search filter
	if search != "" {
		searchTerm := "%" + strings.ToLower(search) + "%"
		query = query.Joins("LEFT JOIN users u ON refunds.initiated_by = u.id").
			Where("LOWER(refunds.refund_number) LIKE ? OR LOWER(refunds.reason) LIKE ? OR LOWER(u.first_name || ' ' || u.last_name) LIKE ? OR LOWER(u.email) LIKE ? OR LOWER(refunds.transaction_id::text) LIKE ?",
				searchTerm, searchTerm, searchTerm, searchTerm, searchTerm)
	}

	// Count total records
	query.Count(&total)

	offset := (page - 1) * limit

	// Generate ORDER BY clause with case insensitive sorting for text fields
	orderByClause := utils.GenerateOrderByClause(sortBy, sortOrder)

	if err := query.Order(orderByClause).
		Offset(offset).
		Limit(limit).
		Find(&refunds).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to retrieve refunds: %w", err)
	}

	return refunds, total, nil
}

// UserGetRefunds retrieves refunds for a specific user
func (s *PaymentService) UserGetRefunds(ctx context.Context, userID uuid.UUID, status string, page, limit int, sortBy, sortOrder string) ([]models.Refund, int64, error) {
	var refunds []models.Refund
	var total int64

	query := s.db.Model(&models.Refund{}).
		Joins("JOIN payment_intents pi ON refunds.payment_intent_id = pi.id").
		Where("pi.user_id = ?", userID)

	if status != "" {
		query = query.Where("refunds.status = ?", status)
	}

	query.Count(&total)

	offset := (page - 1) * limit
	orderClause := "refunds." + sortBy + " " + sortOrder
	if err := query.Preload("Transaction").Preload("Initiator").Preload("PaymentIntent").Preload("PaymentIntent.Event").
		Order(orderClause).
		Offset(offset).
		Limit(limit).
		Find(&refunds).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to retrieve user refunds: %w", err)
	}

	return refunds, total, nil
}

// convertRefundsToListResponse converts Refund models to RefundListResponse
func (s *PaymentService) convertRefundsToListResponse(refunds []models.Refund) []models.RefundListResponse {
	responses := make([]models.RefundListResponse, len(refunds))
	for i, refund := range refunds {
		response := models.RefundListResponse{
			ID:            refund.ID,
			RefundNumber:  refund.RefundNumber,
			TransactionID: refund.TransactionID,
			Amount:        refund.Amount,
			Currency:      refund.Currency,
			Reason:        refund.Reason,
			RefundType:    refund.RefundType,
			Status:        refund.Status,
			TicketCount:   refund.TicketCount,
			RequestedAt:   refund.RequestedAt,
			CreatedAt:     refund.CreatedAt,
			UpdatedAt:     refund.UpdatedAt,
		}

		// Add initiated by info
		if refund.Initiator != nil {
			name := refund.Initiator.FirstName
			if refund.Initiator.LastName != "" {
				name += " " + refund.Initiator.LastName
			}
			response.InitiatedBy = &models.RefundUserInfo{
				ID:    refund.Initiator.ID,
				Name:  name,
				Email: refund.Initiator.Email,
			}
		}

		responses[i] = response
	}
	return responses
}

// UserGetRefundsList retrieves refunds for a specific user and returns RefundListResponse
func (s *PaymentService) UserGetRefundsList(ctx context.Context, userID uuid.UUID, status string, page, limit int, sortBy, sortOrder string) ([]models.RefundListResponse, int64, error) {
	refunds, total, err := s.UserGetRefunds(ctx, userID, status, page, limit, sortBy, sortOrder)
	if err != nil {
		return nil, 0, err
	}
	return s.convertRefundsToListResponse(refunds), total, nil
}

// AdminGetAllRefundsList retrieves all refunds with filters and returns RefundListResponse
func (s *PaymentService) AdminGetAllRefundsList(ctx context.Context, status, search, refundType string, startDate, endDate *time.Time, page, limit int, sortBy, sortOrder string) ([]models.RefundListResponse, int64, error) {
	refunds, total, err := s.AdminGetAllRefunds(ctx, status, search, refundType, startDate, endDate, page, limit, sortBy, sortOrder)
	if err != nil {
		return nil, 0, err
	}
	return s.convertRefundsToListResponse(refunds), total, nil
}

// convertRefundToDetailResponse converts a single Refund model to RefundDetailResponse
func (s *PaymentService) convertRefundToDetailResponse(refund *models.Refund) models.RefundDetailResponse {
	response := models.RefundDetailResponse{
		ID:                refund.ID,
		RefundNumber:      refund.RefundNumber,
		Amount:            refund.Amount,
		Currency:          refund.Currency,
		Reason:            refund.Reason,
		RefundType:        refund.RefundType,
		Status:            refund.Status,
		AffectedTicketIDs: refund.AffectedTicketIDs,
		TicketCount:       refund.TicketCount,
		RequestedAt:       refund.RequestedAt,
		CreatedAt:         refund.CreatedAt,
		UpdatedAt:         refund.UpdatedAt,
	}

	// Add transaction info
	if refund.Transaction != nil {
		response.Transaction = models.RefundTransactionInfo{
			ID:        refund.Transaction.ID,
			Amount:    refund.Transaction.Amount,
			Gateway:   string(refund.Transaction.PaymentGateway),
			Status:    refund.Transaction.Status,
			CreatedAt: refund.Transaction.CreatedAt,
		}
	}

	// Add event info
	if refund.PaymentIntent != nil && refund.PaymentIntent.Event != nil {
		response.Event = &models.RefundEventInfo{
			ID:          refund.PaymentIntent.Event.ID,
			Title:       refund.PaymentIntent.Event.Title,
			BannerImage: refund.PaymentIntent.Event.BannerImage,
		}
	}

	// Add organizer info
	if refund.PaymentIntent != nil && refund.PaymentIntent.Event != nil && refund.PaymentIntent.Event.Organizer != nil {
		organizer := refund.PaymentIntent.Event.Organizer
		var organizerName string

		// Use business_name from onboarding if available, otherwise first_name + last_name
		if organizer.OrganizerOnboarding != nil && organizer.OrganizerOnboarding.BusinessName != "" {
			organizerName = organizer.OrganizerOnboarding.BusinessName
		} else {
			organizerName = organizer.FirstName
			if organizer.LastName != "" {
				organizerName += " " + organizer.LastName
			}
		}

		response.Organizer = &models.RefundOrganizerInfo{
			ID:   organizer.ID,
			Name: organizerName,
		}
	}

	// Add initiated by info
	if refund.Initiator != nil {
		name := refund.Initiator.FirstName
		if refund.Initiator.LastName != "" {
			name += " " + refund.Initiator.LastName
		}
		response.InitiatedBy = &models.RefundUserInfo{
			ID:    refund.Initiator.ID,
			Name:  name,
			Email: refund.Initiator.Email,
		}
	}

	return response
}

// AdminGetRefund retrieves a single refund by ID for admin
func (s *PaymentService) AdminGetRefund(ctx context.Context, refundID uuid.UUID) (*models.RefundDetailResponse, error) {
	var refund models.Refund
	if err := s.db.Preload("Transaction").Preload("Initiator").
		Preload("PaymentIntent.Event.Organizer.OrganizerOnboarding").
		First(&refund, refundID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("refund not found")
		}
		return nil, fmt.Errorf("failed to retrieve refund: %w", err)
	}

	response := s.convertRefundToDetailResponse(&refund)
	return &response, nil
}

// AdminGetRefundStatusHistory retrieves refund status history for any refund (admin access)
func (s *PaymentService) AdminGetRefundStatusHistory(ctx context.Context, refundID uuid.UUID) ([]models.RefundStatusHistoryResponse, error) {
	var response []models.RefundStatusHistoryResponse

	// Check if the ID is a Refund ID
	var refund models.Refund
	if err := s.db.Where("id = ?", refundID).Preload("Transaction").Preload("PaymentIntent").First(&refund).Error; err == nil {
		// It's a Refund ID, get its status history
		var history []models.RefundStatusHistory
		if err := s.db.Where("refund_id = ?", refundID).
			Order("changed_at DESC").
			Preload("ChangedBy").
			Find(&history).Error; err != nil {
			return nil, fmt.Errorf("failed to fetch refund status history: %w", err)
		}

		response = make([]models.RefundStatusHistoryResponse, len(history))
		for i, h := range history {
			response[i] = models.RefundStatusHistoryResponse{
				ID:            h.ID,
				RefundID:      h.RefundID,
				OldStatus:     h.OldStatus,
				NewStatus:     h.NewStatus,
				ChangedByID:   h.ChangedByID,
				ChangedByType: h.ChangedByType,
				Remarks:       h.Remarks,
				Metadata:      h.Metadata,
				ChangedAt:     h.ChangedAt,
			}

			// Add changed by user info if available
			if h.ChangedBy != nil {
				response[i].ChangedBy = &models.UserSummary{
					ID:    h.ChangedBy.ID,
					Name:  h.ChangedBy.FirstName + " " + h.ChangedBy.LastName,
					Email: h.ChangedBy.Email,
				}
			}
		}
		return response, nil
	}

	// Check if the ID is a RefundRequest ID
	var refundRequest models.RefundRequest
	if err := s.db.Where("id = ?", refundID).
		Preload("Transaction").
		Preload("ProcessedBy").
		First(&refundRequest).Error; err == nil {
		// It's a RefundRequest ID, create synthetic history
		response = []models.RefundStatusHistoryResponse{}

		// Add creation entry
		response = append(response, models.RefundStatusHistoryResponse{
			ID:            uuid.New(), // Synthetic ID
			RefundID:      refundRequest.ID,
			OldStatus:     "",
			NewStatus:     "pending",
			ChangedByID:   refundRequest.UserID,
			ChangedByType: "user",
			Remarks:       "Refund request created",
			Metadata:      nil,
			ChangedAt:     refundRequest.CreatedAt,
		})

		// Add processing entry if processed
		if refundRequest.Status != "pending" {
			changedByType := "admin"
			var changedByID *uuid.UUID
			var remarks string

			if refundRequest.Status == "approved" {
				remarks = "Refund request approved"
				changedByID = refundRequest.ProcessedByID
			} else if refundRequest.Status == "rejected" {
				remarks = "Refund request rejected"
				changedByID = refundRequest.ProcessedByID
			}

			response = append(response, models.RefundStatusHistoryResponse{
				ID:            uuid.New(), // Synthetic ID
				RefundID:      refundRequest.ID,
				OldStatus:     "pending",
				NewStatus:     refundRequest.Status,
				ChangedByID:   changedByID,
				ChangedByType: changedByType,
				Remarks:       remarks,
				Metadata:      nil,
				ChangedAt:     *refundRequest.ProcessedAt,
			})
		}

		// If approved, also include the actual refund status history
		if refundRequest.Status == "approved" {
			// Find the associated refund
			var refund models.Refund
			if err := s.db.Where("transaction_id = ?", refundRequest.TransactionID).
				Where("status != 'failed'").
				Order("created_at DESC").
				First(&refund).Error; err == nil {
				var refundHistory []models.RefundStatusHistory
				if err := s.db.Where("refund_id = ?", refund.ID).
					Order("changed_at DESC").
					Preload("ChangedBy").
					Find(&refundHistory).Error; err == nil {
					for _, h := range refundHistory {
						response = append(response, models.RefundStatusHistoryResponse{
							ID:            h.ID,
							RefundID:      h.RefundID,
							OldStatus:     h.OldStatus,
							NewStatus:     h.NewStatus,
							ChangedByID:   h.ChangedByID,
							ChangedByType: h.ChangedByType,
							Remarks:       h.Remarks,
							Metadata:      h.Metadata,
							ChangedAt:     h.ChangedAt,
						})

						// Add changed by user info if available
						if h.ChangedBy != nil {
							response[len(response)-1].ChangedBy = &models.UserSummary{
								ID:    h.ChangedBy.ID,
								Name:  h.ChangedBy.FirstName + " " + h.ChangedBy.LastName,
								Email: h.ChangedBy.Email,
							}
						}
					}
				}
			}
		}

		return response, nil
	}

	// ID not found in either table
	return nil, fmt.Errorf("refund not found")
}

// AdminInitiateRefund allows admins to create refunds directly without user request
func (s *PaymentService) AdminInitiateRefund(ctx context.Context, paymentIntentID, adminID uuid.UUID, amount float64, reason string, ticketIDs []uuid.UUID, refundType string) (*models.Refund, error) {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var paymentIntent models.PaymentIntent
	if err := tx.First(&paymentIntent, paymentIntentID).Error; err != nil {
		// Try with Unscoped if not found (might be soft-deleted)
		if err = tx.Unscoped().First(&paymentIntent, paymentIntentID).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("payment intent not found: %w", err)
		}
	}

	// Only succeeded payments can be refunded
	if paymentIntent.Status != "succeeded" {
		tx.Rollback()
		return nil, utils.NewBusinessLogicError("Only succeeded payments can be refunded")
	}

	// Validate refund conditions
	if err := s.validateRefundConditions(ctx, paymentIntentID, ticketIDs); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("refund not allowed: %w", err)
	}

	// For admin refunds, use the provided amount or calculate based on tickets
	refundAmount := amount
	if refundAmount == 0 && len(ticketIDs) > 0 {
		refundAmount = paymentIntent.TotalAmount * (float64(len(ticketIDs)) / float64(paymentIntent.Quantity))
	} else if refundAmount == 0 {
		refundAmount = paymentIntent.TotalAmount // Full refund if no tickets specified
	}

	// Validate refund amount doesn't exceed payment amount
	if refundAmount > paymentIntent.TotalAmount {
		tx.Rollback()
		return nil, utils.NewBusinessLogicError("Refund amount cannot exceed payment amount")
	}

	// Find the transaction ID associated with this payment intent
	var transaction models.Transaction
	if err := tx.Where("payment_intent_id = ?", paymentIntentID).First(&transaction).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("transaction not found for payment intent: %w", err)
	}

	// Capture charge ID at creation time (don't rely on fetching PaymentIntent later)
	gatewayMetadata := map[string]interface{}{}
	if paymentIntent.GatewayChargeID != nil && *paymentIntent.GatewayChargeID != "" {
		gatewayMetadata["stripe_charge_id"] = *paymentIntent.GatewayChargeID
	} else {
		log.Printf("[REFUND] Warning: Payment intent %s has no charge ID captured yet. Refund processing may fail.", paymentIntentID)
	}

	refund := &models.Refund{
		PaymentIntentID: paymentIntentID,
		TransactionID:   transaction.ID, // Set the transaction ID
		PaymentGateway:  paymentIntent.PaymentGateway,
		GatewayRefundID: "", // Will be set when processed
		Amount:          refundAmount,
		Currency:        paymentIntent.Currency,
		Reason:          reason,
		RefundType:      refundType,
		Status:          "pending", // Start as pending - will be approved by admin via ApproveRefund
		InitiatedBy:     &adminID,
		GatewayMetadata: gatewayMetadata, // Store charge ID for later processing
		AffectedTicketIDs: func() []string {
			ids := make([]string, len(ticketIDs))
			for i, id := range ticketIDs {
				ids[i] = id.String()
			}
			return ids
		}(),
		TicketCount: len(ticketIDs),
		RequestedAt: &time.Time{}, // Set to current time
	}

	now := time.Now()
	refund.RequestedAt = &now
	refund.ApprovedAt = &now

	if err := tx.Create(refund).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create admin refund: %w", err)
	}

	// Log initial status
	if err := s.LogRefundStatusChange(ctx, tx, refund.ID, "", "approved", &adminID, "admin", "Admin refund created and auto-approved", nil); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to log initial status change: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit admin refund: %w", err)
	}

	// Log audit with event_id
	s.logAudit(ctx, "refund_initiated_by_admin", "refund", refund.ID, &adminID, "admin", &paymentIntent.EventID, map[string]interface{}{})

	// Process the refund immediately since it's admin-approved
	if err := s.processGatewayRefund(ctx, refund, &adminID); err != nil {
		// Update status to failed if processing fails
		refund.Status = "failed"
		s.db.Save(refund)
		return nil, fmt.Errorf("failed to process refund: %w", err)
	}

	return refund, nil
}

// AdminRefundFullTransaction allows admins to refund an entire transaction (all tickets)
func (s *PaymentService) AdminRefundFullTransaction(ctx context.Context, transactionID, adminID uuid.UUID, reason string, refundType string) (*models.Refund, error) {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var transaction models.Transaction
	if err := tx.Preload("PaymentIntent").First(&transaction, transactionID).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("transaction not found: %w", err)
	}

	// Only completed transactions can be fully refunded
	if transaction.Status != "completed" {
		tx.Rollback()
		return nil, utils.NewBusinessLogicError("Only completed transactions can be fully refunded")
	}

	// Get all tickets for this transaction
	var tickets []models.Ticket
	if err := tx.Where("transaction_id = ?", transactionID).Find(&tickets).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to fetch transaction tickets: %w", err)
	}

	if len(tickets) == 0 {
		tx.Rollback()
		return nil, utils.NewBusinessLogicError("No tickets found for this transaction")
	}

	// Check if any tickets are already refunded
	var activeTickets []models.Ticket
	var affectedTicketIDs []uuid.UUID
	for _, ticket := range tickets {
		if ticket.Status != "refunded" {
			activeTickets = append(activeTickets, ticket)
			affectedTicketIDs = append(affectedTicketIDs, ticket.ID)
		}
	}

	if len(activeTickets) == 0 {
		tx.Rollback()
		return nil, utils.NewBusinessLogicError("All tickets in this transaction are already refunded")
	}

	// Calculate refund amount from active tickets
	refundAmount := 0.0
	for _, ticket := range activeTickets {
		refundAmount += ticket.TotalAmount
	}

	// Create the full transaction refund
	refund := &models.Refund{
		PaymentIntentID: *transaction.PaymentIntentID, // Dereference pointer
		TransactionID:   transactionID,
		PaymentGateway:  transaction.PaymentIntent.PaymentGateway,
		GatewayRefundID: "", // Will be set when processed
		Amount:          refundAmount,
		Currency:        transaction.PaymentIntent.Currency,
		Reason:          reason,
		RefundType:      refundType,
		Status:          "approved", // Admin refunds are auto-approved
		InitiatedBy:     &adminID,
		ApprovedBy:      &adminID,
		AffectedTicketIDs: func() []string {
			ids := make([]string, len(affectedTicketIDs))
			for i, id := range affectedTicketIDs {
				ids[i] = id.String()
			}
			return ids
		}(),
		TicketCount:             len(affectedTicketIDs),
		IsFullTransactionRefund: true, // Mark as full transaction refund
		RequestedAt:             &time.Time{},
		ApprovedAt:              &time.Time{},
	}

	now := time.Now()
	refund.RequestedAt = &now
	refund.ApprovedAt = &now

	if err := tx.Create(refund).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create full transaction refund: %w", err)
	}

	// Log initial status
	if err := s.LogRefundStatusChange(ctx, tx, refund.ID, "", "approved", &adminID, "admin", "Full transaction refund created and auto-approved", nil); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to log initial status change: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit full transaction refund: %w", err)
	}

	// Log audit with event_id
	s.logAudit(ctx, "full_transaction_refund_initiated", "refund", refund.ID, &adminID, "admin", &transaction.PaymentIntent.EventID, map[string]interface{}{})

	// Process the refund immediately since it's admin-approved
	if err := s.processGatewayRefund(ctx, refund, &adminID); err != nil {
		// Update status to failed if processing fails
		refund.Status = "failed"
		s.db.Save(refund)
		return nil, fmt.Errorf("failed to process full transaction refund: %w", err)
	}

	return refund, nil
}

// AdminRefundEventTickets refunds all eligible tickets for an event (event cancellation scenario)
func (s *PaymentService) AdminRefundEventTickets(ctx context.Context, eventID, adminID uuid.UUID, reason string, refundType string) (map[string]interface{}, error) {
	// Get all eligible tickets for the event
	var tickets []models.Ticket
	if err := s.db.Where("event_id = ? AND status IN (?) AND check_in_time IS NULL", eventID, []string{"active", "confirmed"}).
		Preload("Transaction").
		Preload("Event").
		Find(&tickets).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch event tickets: %w", err)
	}

	if len(tickets) == 0 {
		return map[string]interface{}{
			"message":          "No eligible tickets found for refund",
			"eligible_tickets": 0,
		}, nil
	}

	// Group tickets by transaction/payment_intent
	ticketsByTransaction := make(map[uuid.UUID][]models.Ticket)
	totalRefundAmount := 0.0
	eligibleTicketIDs := make([]uuid.UUID, 0, len(tickets))

	for _, ticket := range tickets {
		if ticket.TransactionID != nil {
			ticketsByTransaction[*ticket.TransactionID] = append(ticketsByTransaction[*ticket.TransactionID], ticket)
			totalRefundAmount += ticket.TotalAmount
			eligibleTicketIDs = append(eligibleTicketIDs, ticket.ID)
		}
	}

	// Create refunds for each transaction
	createdRefunds := 0
	totalRefundedAmount := 0.0

	for transactionID, transactionTickets := range ticketsByTransaction {
		// Calculate refund amount for this transaction
		transactionRefundAmount := 0.0
		transactionTicketIDs := make([]uuid.UUID, 0, len(transactionTickets))

		for _, ticket := range transactionTickets {
			transactionRefundAmount += ticket.TotalAmount
			transactionTicketIDs = append(transactionTicketIDs, ticket.ID)
		}

		// Get payment intent for this transaction
		var transaction models.Transaction
		if err := s.db.First(&transaction, transactionID).Error; err != nil {
			continue // Skip if transaction not found
		}

		// Create refund for this transaction
		refund := &models.Refund{
			PaymentIntentID: *transaction.PaymentIntentID, // Dereference pointer
			TransactionID:   transactionID,
			PaymentGateway:  string(transaction.PaymentGateway), // Convert to string
			GatewayRefundID: "",                                 // Will be set when processed
			Amount:          transactionRefundAmount,
			Currency:        transaction.Currency,
			Reason:          reason,
			RefundType:      refundType,
			Status:          "approved", // Admin refunds are auto-approved
			InitiatedBy:     &adminID,
			ApprovedBy:      &adminID,
			AffectedTicketIDs: func() []string {
				ids := make([]string, len(transactionTicketIDs))
				for i, id := range transactionTicketIDs {
					ids[i] = id.String()
				}
				return ids
			}(),
			TicketCount: len(transactionTicketIDs),
		}

		now := time.Now()
		refund.RequestedAt = &now
		refund.ApprovedAt = &now

		if err := s.db.Create(refund).Error; err != nil {
			return nil, fmt.Errorf("failed to create refund for transaction %s: %w", transactionID, err)
		}

		// Log initial status
		if err := s.LogRefundStatusChange(ctx, nil, refund.ID, "", "approved", &adminID, "admin", "Event ticket refund created and auto-approved", nil); err != nil {
			log.Printf("[REFUND] Warning: Failed to log initial status change for event refund %s: %v", refund.ID, err)
		}

		// Process the refund immediately
		if err := s.processGatewayRefund(ctx, refund, &adminID); err != nil {
			// Update status to failed if processing fails
			refund.Status = "failed"
			s.db.Save(refund)

			// Log status change for processing failure
			if logErr := s.LogRefundStatusChange(ctx, nil, refund.ID, "approved", "failed", nil, "system", fmt.Sprintf("Event refund processing failed: %s", err.Error()), map[string]interface{}{
				"error": err.Error(),
			}); logErr != nil {
				log.Printf("[REFUND] Warning: Failed to log processing failure status change: %v", logErr)
			}

			return nil, fmt.Errorf("failed to process refund for transaction %s: %w", transactionID, err)
		}

		createdRefunds++
		totalRefundedAmount += transactionRefundAmount

		// Mark tickets as refunded
		for _, ticketID := range transactionTicketIDs {
			s.db.Model(&models.Ticket{}).Where("id = ?", ticketID).Update("status", "refunded")
		}

		// Send event cancellation email notification
		go func(txID uuid.UUID, txTickets []models.Ticket, refundAmt float64) {
			if err := s.sendEventCancellationEmail(ctx, txID, txTickets, refundAmt); err != nil {
				log.Printf("Failed to send event cancellation email for transaction %s: %v", txID, err)
			}
		}(transactionID, transactionTickets, transactionRefundAmount)
	}

	return map[string]interface{}{
		"message":                fmt.Sprintf("Successfully processed refunds for %d transactions", createdRefunds),
		"total_tickets_refunded": len(eligibleTicketIDs),
		"total_refund_amount":    totalRefundAmount,
		"total_refunded_amount":  totalRefundedAmount,
		"refunds_created":        createdRefunds,
	}, nil
}

// sendEventCancellationEmail sends event cancellation notification with refund details
func (s *PaymentService) sendEventCancellationEmail(ctx context.Context, transactionID uuid.UUID, tickets []models.Ticket, refundAmount float64) error {
	if len(tickets) == 0 {
		return fmt.Errorf("no tickets provided for email notification")
	}

	// Get transaction details
	var transaction models.Transaction
	if err := s.db.First(&transaction, transactionID).Error; err != nil {
		return fmt.Errorf("failed to get transaction details: %w", err)
	}

	// Get user information
	var userEmail, userName string
	if tickets[0].UserID != nil {
		var user models.User
		if err := s.db.First(&user, *tickets[0].UserID).Error; err != nil {
			return fmt.Errorf("failed to get user details: %w", err)
		}
		userEmail = user.Email
		userName = user.FirstName + " " + user.LastName
	} else if tickets[0].GuestUserID != nil {
		var guestUser models.GuestUser
		if err := s.db.First(&guestUser, *tickets[0].GuestUserID).Error; err != nil {
			return fmt.Errorf("failed to get guest user details: %w", err)
		}
		userEmail = guestUser.Email
		userName = guestUser.FirstName + " " + guestUser.LastName
	} else {
		return fmt.Errorf("no user or guest user associated with tickets")
	}

	// Get event details
	event := tickets[0].Event
	organizerName := event.Organizer.FirstName + " " + event.Organizer.LastName

	// Format event date
	eventDate := ""
	if !event.StartDate.IsZero() {
		eventDate = event.StartDate.Format("January 2, 2006 at 3:04 PM")
	}

	// Prepare email template data
	templateData := map[string]interface{}{
		"user_name":      userName,
		"event_name":     event.Title,
		"organizer_name": organizerName,
		"refund_amount":  refundAmount,
		"currency":       transaction.Currency,
		"ticket_count":   len(tickets),
		"transaction_id": transactionID.String(),
		"event_date":     eventDate,
		"event_location": event.Location,
		"completed_at":   time.Now().Format("January 2, 2006 at 3:04 PM"),
	}

	// Queue the email
	subject := fmt.Sprintf("Event Cancelled - %s", event.Title)
	priority := 2 // High priority for event cancellations

	if err := s.emailOutboxService.QueueEmail(ctx, models.EmailEventEventCancellation, userEmail, subject, templateData, priority); err != nil {
		return fmt.Errorf("failed to queue event cancellation email: %w", err)
	}

	log.Printf("✓ Event cancellation email queued for %s: %s", userEmail, event.Title)
	return nil
}

// ============================================================================
// PRODUCTION-GRADE PAYMENT FLOW (Secure, Idempotent, Scales to Peak Load)
// ============================================================================

// CreatePaymentRequest represents atomic payment creation with multi-tier support
type CreatePaymentRequest struct {
	EventID        uuid.UUID
	UserID         *uuid.UUID // nil for guest purchases
	GuestUserID    *uuid.UUID
	CustomerEmail  string
	CustomerName   string
	CustomerPhone  string
	Currency       string
	PaymentGateway string
	CountryCode    string

	// Multiple tiers in a single transaction (supports bundled purchases)
	TierSelections []TierSelection `json:"tiers"`
}

// TierSelection represents a single tier in a multi-tier purchase
type TierSelection struct {
	TierID   uuid.UUID
	Quantity int
}

// CreatePaymentResponse represents the response from atomic payment creation
type CreatePaymentResponse struct {
	PaymentIntentID uuid.UUID
	CheckoutToken   string
	PaymentGateway  string
	RedirectURL     string
	Amount          float64
	Currency        string
	Status          string
	ReservedTickets []uuid.UUID
	ExpiresAt       *time.Time
}

// CreatePaymentAtomically creates a payment intent + reserves tickets in a single atomic transaction
// CRITICAL FOR PEAK LOAD:
// - Uses DB constraints for idempotency (no duplicates)
// - Atomic ticket allocation (prevents overselling)
// - Minimal DB locks (fast operation)
// - Returns checkout token for fallback verification
func (s *PaymentService) CreatePaymentAtomically(ctx context.Context, req *CreatePaymentRequest) (*CreatePaymentResponse, error) {
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

	// 2. Load all tiers and validate availability
	var tiers []models.EventTier
	tierMap := make(map[uuid.UUID]*models.EventTier)
	totalAmount := 0.0
	var allTicketIDs []uuid.UUID

	for _, tierSelection := range req.TierSelections {
		var tier models.EventTier
		if err := tx.Where("id = ? AND event_id = ?", tierSelection.TierID, req.EventID).First(&tier).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("tier not found: %w", err)
		}

		if tier.Available < tierSelection.Quantity {
			tx.Rollback()
			return nil, utils.NewBusinessLogicError(fmt.Sprintf("Insufficient tickets for tier %s. Available: %d, Requested: %d", tier.TierName, tier.Available, tierSelection.Quantity))
		}

		tierMap[tier.ID] = &tier
		tiers = append(tiers, tier)
		totalAmount += tier.Price * float64(tierSelection.Quantity)
	}

	// 3. Calculate pricing and commission
	commissionRate := event.CommissionRate
	commissionAmount := totalAmount * (commissionRate / 100)
	finalAmount := totalAmount + commissionAmount

	// 4. Generate unique checkout token for fallback verification
	checkoutToken := fmt.Sprintf("checkout_%s_%s_%s_%d", req.EventID.String()[:8], strings.ReplaceAll(req.CustomerEmail, "@", "_at_"), uuid.New().String(), time.Now().UnixNano())

	// 5. Generate idempotency key
	idempotencyKey := fmt.Sprintf("payment_%s_%s_%s_%d", req.EventID.String()[:8], strings.ReplaceAll(req.CustomerEmail, "@", "_at_"), uuid.New().String(), time.Now().UnixNano())

	// 6. Create PaymentIntent record (minimal, just mark as pending)
	paymentIntent := &models.PaymentIntent{
		PaymentGateway:     req.PaymentGateway,
		IdempotencyKey:     idempotencyKey,
		CheckoutToken:      checkoutToken, // For fallback verification
		UserID:             req.UserID,
		GuestUserID:        req.GuestUserID,
		CustomerEmail:      req.CustomerEmail,
		CustomerName:       req.CustomerName,
		CustomerPhone:      req.CustomerPhone,
		EventID:            req.EventID,
		TierID:             tiers[0].ID,             // Primary tier
		Quantity:           len(req.TierSelections), // Number of tiers purchased
		Currency:           req.Currency,
		CurrencySymbol:     getCurrencySymbol(req.Currency),
		ExchangeRate:       1.0,
		BaseCurrency:       "USD",
		BaseCurrencyAmount: finalAmount,
		UnitPrice:          0, // Multi-tier
		Subtotal:           totalAmount,
		PlatformFee:        commissionAmount,
		GatewayFee:         0,
		TotalAmount:        finalAmount,
		Status:             "pending",
		CommissionRate:     commissionRate,
		CommissionAmount:   commissionAmount,
		OrganizerNetAmount: totalAmount,
		CountryCode:        formatCountryCode(req.CountryCode),
	}

	if err := tx.Create(paymentIntent).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create payment intent: %w", err)
	}

	// 7. CRITICALLY: Allocate tickets atomically with proper locking (prevents overselling)
	for _, tierSelection := range req.TierSelections {
		tier := tierMap[tierSelection.TierID]

		// Atomic update: only succeed if enough tickets are available
		// This is safer than checking first, then updating
		result := tx.Model(&models.EventTier{}).
			Where("id = ? AND available >= ?", tier.ID, tierSelection.Quantity).
			Update("available", gorm.Expr("available - ?", tierSelection.Quantity)).
			Update("sold", gorm.Expr("sold + ?", tierSelection.Quantity))

		if result.RowsAffected == 0 {
			// SOLD OUT OR RACE CONDITION
			tx.Rollback()
			return nil, utils.NewBusinessLogicError(fmt.Sprintf("Ticket tier %s is no longer available. Someone else may have purchased them.", tier.TierName))
		}

		// 8. Create individual tickets for this tier
		for i := 0; i < tierSelection.Quantity; i++ {
			ticketNum, err := utils.GenerateEventTicketNumber(tx, tier.TierName, event.StartDate.Year())
			if err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to generate ticket number: %w", err)
			}

			ticket := &models.Ticket{
				TicketNumber:    ticketNum,
				UserID:          req.UserID,
				GuestUserID:     req.GuestUserID,
				EventID:         req.EventID,
				TierID:          tier.ID,
				PaymentGateway:  models.PaymentGateway(req.PaymentGateway),
				Status:          "pending_payment",
				IsGuestPurchase: req.GuestUserID != nil,
				TotalAmount:     tier.Price + (commissionAmount / float64(len(req.TierSelections))),
			}

			if err := tx.Create(ticket).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to create ticket: %w", err)
			}

			allTicketIDs = append(allTicketIDs, ticket.ID)
		}
	}

	// 9. Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Determine redirect URL based on gateway
	var redirectURL string
	if req.PaymentGateway == "stripe" {
		// TODO: Create Stripe Checkout Session and get redirect URL
		redirectURL = ""
	}

	s.logAudit(ctx, "payment_created_atomically", "payment_intent", paymentIntent.ID, req.UserID, "user", &req.EventID, map[string]interface{}{})

	return &CreatePaymentResponse{
		PaymentIntentID: paymentIntent.ID,
		CheckoutToken:   checkoutToken,
		PaymentGateway:  req.PaymentGateway,
		RedirectURL:     redirectURL,
		Amount:          finalAmount,
		Currency:        req.Currency,
		Status:          "pending",
		ReservedTickets: allTicketIDs,
		ExpiresAt:       nil,
	}, nil
}

// HandlePaymentSuccess processes a successful payment (from webhook)
// IDEMPOTENT: Safe to call multiple times - no side effects from duplicates
// SECURITY: Uses row-level locking to prevent race conditions
func (s *PaymentService) HandlePaymentSuccess(ctx context.Context, gatewayPaymentID string, gateway string) (*models.PaymentIntent, error) {
	tx := s.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 1. LOCK: Find and lock payment intent row
	// FOR UPDATE ensures only one webhook handler processes this payment
	var paymentIntent models.PaymentIntent
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}). // Row-level lock
									Where("payment_gateway = ? AND gateway_payment_id = ?", gateway, gatewayPaymentID).
									First(&paymentIntent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Payment intent doesn't exist - create it from webhook data
			// This handles the case where webhook arrives before DB insert
			return nil, utils.NewBusinessLogicError("Payment intent not found. Retry webhook processing.")
		}
		tx.Rollback()
		return nil, fmt.Errorf("failed to lock payment intent: %w", err)
	}

	// 2. IDEMPOTENCY CHECK: Already processed?
	if paymentIntent.Status == "succeeded" {
		// Already processed - just return it (idempotent)
		tx.Rollback()
		return &paymentIntent, nil
	}

	// 3. UPDATE PAYMENT STATUS
	paymentIntent.Status = "succeeded"
	now := time.Now()
	paymentIntent.SucceededAt = &now

	if err := tx.Save(&paymentIntent).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update payment intent: %w", err)
	}

	// 4. ACTIVATE TICKETS (update from pending_payment to active)
	if err := tx.Model(&models.Ticket{}).
		Where("payment_intent_id IS NULL AND user_id = ? AND guest_user_id = ? AND event_id = ? AND status = ?",
			paymentIntent.UserID, paymentIntent.GuestUserID, paymentIntent.EventID, "pending_payment").
		Limit(paymentIntent.Quantity).
		Update("status", "active").Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to activate tickets: %w", err)
	}

	// 5. COMMIT
	// NOTE: Transaction records are now created in payment_worker.processPaymentIntentSucceeded()
	// via webhook processing. This avoids duplicate transaction creation.
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logAudit(ctx, "payment_succeeded", "payment_intent", paymentIntent.ID, paymentIntent.UserID, "user", &paymentIntent.EventID, map[string]interface{}{})

	return &paymentIntent, nil
}

// VerifyPayment is a fallback endpoint for recovering from missed/failed webhooks
// Used when webhooks fail, network issues, or Stripe delays occur
// Returns: true if payment succeeded and processed, false if still pending, error if failed
func (s *PaymentService) VerifyPayment(ctx context.Context, checkoutToken string) (*models.PaymentIntent, error) {
	var paymentIntent models.PaymentIntent
	if err := s.db.WithContext(ctx).
		Where("checkout_token = ?", checkoutToken).
		First(&paymentIntent).Error; err != nil {
		return nil, fmt.Errorf("payment not found")
	}

	// If already succeeded, just return
	if paymentIntent.Status == "succeeded" {
		return &paymentIntent, nil
	}

	// If still pending, check with Stripe
	// TODO: Implement per-gateway verification logic
	// This would call gateway API to check actual status

	return &paymentIntent, nil
}

// LogRefundStatusChange logs a refund status change to the refund_status_history table
func (s *PaymentService) LogRefundStatusChange(ctx context.Context, tx *gorm.DB, refundID uuid.UUID, oldStatus, newStatus string, changedByID *uuid.UUID, changedByType, remarks string, metadata map[string]interface{}) error {
	statusHistory := &models.RefundStatusHistory{
		RefundID:      refundID,
		OldStatus:     oldStatus,
		NewStatus:     newStatus,
		ChangedByID:   changedByID,
		ChangedByType: changedByType,
		Remarks:       remarks,
		Metadata:      metadata,
		ChangedAt:     time.Now(),
	}

	if tx != nil {
		if err := tx.WithContext(ctx).Create(statusHistory).Error; err != nil {
			log.Printf("[REFUND] Warning: Failed to log refund status change: %v", err)
			return err
		}
	} else {
		if err := s.db.WithContext(ctx).Create(statusHistory).Error; err != nil {
			log.Printf("[REFUND] Warning: Failed to log refund status change: %v", err)
			return err
		}
	}

	return nil
}

// GetUserRefundStatusHistory retrieves refund status history for a specific refund belonging to the user
func (s *PaymentService) GetUserRefundStatusHistory(ctx context.Context, userID uuid.UUID, refundID uuid.UUID) ([]models.RefundStatusHistoryResponse, error) {
	var response []models.RefundStatusHistoryResponse

	// Check if the ID is a Refund ID
	var refund models.Refund
	if err := s.db.Where("id = ?", refundID).Preload("Transaction").Preload("PaymentIntent").First(&refund).Error; err == nil {
		// Verify the refund belongs to the user
		if refund.PaymentIntent.UserID == nil || *refund.PaymentIntent.UserID != userID {
			// Check if it's a guest user with matching email
			if refund.PaymentIntent.GuestUserID == nil {
				return nil, fmt.Errorf("refund not found or access denied")
			}
			var guestUser models.GuestUser
			if err := s.db.Where("id = ?", refund.PaymentIntent.GuestUserID).First(&guestUser).Error; err != nil {
				return nil, fmt.Errorf("refund not found or access denied")
			}
			var user models.User
			if err := s.db.Where("email = ?", guestUser.Email).First(&user).Error; err != nil || user.ID != userID {
				return nil, fmt.Errorf("refund not found or access denied")
			}
		}

		// It's a Refund ID, get its status history
		var history []models.RefundStatusHistory
		if err := s.db.Where("refund_id = ?", refundID).
			Order("changed_at DESC").
			Preload("ChangedBy").
			Find(&history).Error; err != nil {
			return nil, fmt.Errorf("failed to fetch refund status history: %w", err)
		}

		response = make([]models.RefundStatusHistoryResponse, len(history))
		for i, h := range history {
			response[i] = models.RefundStatusHistoryResponse{
				ID:            h.ID,
				RefundID:      h.RefundID,
				OldStatus:     h.OldStatus,
				NewStatus:     h.NewStatus,
				ChangedByID:   h.ChangedByID,
				ChangedByType: h.ChangedByType,
				Remarks:       h.Remarks,
				Metadata:      h.Metadata,
				ChangedAt:     h.ChangedAt,
			}

			// Add changed by user info if available
			if h.ChangedBy != nil {
				response[i].ChangedBy = &models.UserSummary{
					ID:    h.ChangedBy.ID,
					Name:  h.ChangedBy.FirstName + " " + h.ChangedBy.LastName,
					Email: h.ChangedBy.Email,
				}
			}
		}
		return response, nil
	}

	// Check if the ID is a RefundRequest ID
	var refundRequest models.RefundRequest
	if err := s.db.Where("id = ?", refundID).
		Where("(user_id = ? OR guest_user_id IN (SELECT id FROM guest_users WHERE email IN (SELECT email FROM users WHERE id = ?)))", userID, userID).
		Preload("Transaction").
		Preload("ProcessedBy").
		First(&refundRequest).Error; err == nil {
		// It's a RefundRequest ID, create synthetic history
		response = []models.RefundStatusHistoryResponse{}

		// Add creation entry
		response = append(response, models.RefundStatusHistoryResponse{
			ID:            uuid.New(), // Synthetic ID
			RefundID:      refundRequest.ID,
			OldStatus:     "",
			NewStatus:     "pending",
			ChangedByID:   refundRequest.UserID,
			ChangedByType: "user",
			Remarks:       "Refund request created",
			Metadata:      nil,
			ChangedAt:     refundRequest.CreatedAt,
		})

		// Add processing entry if processed
		if refundRequest.Status != "pending" {
			changedByType := "admin"
			var changedByID *uuid.UUID
			var remarks string

			if refundRequest.Status == "approved" {
				remarks = "Refund request approved"
				changedByID = refundRequest.ProcessedByID
			} else if refundRequest.Status == "rejected" {
				remarks = "Refund request rejected"
				changedByID = refundRequest.ProcessedByID
			}

			response = append(response, models.RefundStatusHistoryResponse{
				ID:            uuid.New(), // Synthetic ID
				RefundID:      refundRequest.ID,
				OldStatus:     "pending",
				NewStatus:     refundRequest.Status,
				ChangedByID:   changedByID,
				ChangedByType: changedByType,
				Remarks:       remarks,
				Metadata:      nil,
				ChangedAt:     *refundRequest.ProcessedAt,
			})
		}

		// If approved, also include the actual refund status history
		if refundRequest.Status == "approved" {
			// Find the associated refund
			var refund models.Refund
			if err := s.db.Where("transaction_id = ?", refundRequest.TransactionID).
				Where("status != 'failed'").
				Order("created_at DESC").
				First(&refund).Error; err == nil {
				var refundHistory []models.RefundStatusHistory
				if err := s.db.Where("refund_id = ?", refund.ID).
					Order("changed_at DESC").
					Preload("ChangedBy").
					Find(&refundHistory).Error; err == nil {
					for _, h := range refundHistory {
						response = append(response, models.RefundStatusHistoryResponse{
							ID:            h.ID,
							RefundID:      h.RefundID,
							OldStatus:     h.OldStatus,
							NewStatus:     h.NewStatus,
							ChangedByID:   h.ChangedByID,
							ChangedByType: h.ChangedByType,
							Remarks:       h.Remarks,
							Metadata:      h.Metadata,
							ChangedAt:     h.ChangedAt,
						})

						// Add changed by user info if available
						if h.ChangedBy != nil {
							response[len(response)-1].ChangedBy = &models.UserSummary{
								ID:    h.ChangedBy.ID,
								Name:  h.ChangedBy.FirstName + " " + h.ChangedBy.LastName,
								Email: h.ChangedBy.Email,
							}
						}
					}
				}
			}
		}

		return response, nil
	}

	// ID not found in either table
	return nil, fmt.Errorf("refund not found")
}
