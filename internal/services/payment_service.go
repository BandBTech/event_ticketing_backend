package services

import (
	"context"
	"fmt"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PaymentService handles all payment operations
type PaymentService struct {
	db            *gorm.DB
	ticketService *TicketService
	cfg           *config.Config
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

	// Unique identifier of the ticket tier
	// required: true
	// example: 550e8400-e29b-41d4-a716-446655440001
	TierID uuid.UUID `json:"tier_id" binding:"required"`

	// Number of tickets to purchase (1-10)
	// required: true
	// minimum: 1
	// maximum: 10
	// example: 2
	Quantity int `json:"quantity" binding:"required,min=1,max=10"`

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

	// Country code for gateway selection (ISO 3166-1 alpha-2)
	// required: false
	// example: US
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

	// 2. Check availability
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
	idempotencyKey := fmt.Sprintf("purchase-%s-%s-%d", req.EventID, req.CustomerEmail, time.Now().UnixNano())

	// 8. Start database transaction
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 7. Create payment_intent record
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
		CountryCode:        req.CountryCode,
		ExpiresAt:          nil, // No expiration for simplified implementation
	}

	if err := tx.Create(paymentIntent).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create payment intent record: %w", err)
	}

	// 8. Create tickets (status depends on payment status)
	ticketStatus := "pending_payment"
	if paymentStatus == "succeeded" {
		ticketStatus = "active"
	}

	var ticketIDs []uuid.UUID
	for i := 0; i < req.Quantity; i++ {
		// Generate sequential ticket number (centralized, atomic per event)
		ticketNum, err := utils.GenerateEventTicketNumber(tx, tier.TierName, event.StartDate.Year())
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to generate ticket number for tier %s (%s): %w", tier.TierName, tier.ID.String(), err)
		}

		ticket := &models.Ticket{
			TicketNumber:    ticketNum,
			UserID:          req.UserID,
			GuestUserID:     req.GuestUserID,
			EventID:         req.EventID,
			TierID:          req.TierID,
			TotalAmount:     tier.Price + (commissionAmount / float64(req.Quantity)),
			PaymentGateway:  models.PaymentGateway(selectedGateway),
			Status:          ticketStatus,
			IsGuestPurchase: req.GuestUserID != nil,
		}

		if err := tx.Create(ticket).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to create ticket: %w", err)
		}

		ticketIDs = append(ticketIDs, ticket.ID)
	}

	// 11. Update tier inventory (reserve)
	if err := tx.Model(&tier).Updates(map[string]interface{}{
		"sold":      gorm.Expr("sold + ?", req.Quantity),
		"available": gorm.Expr("available - ?", req.Quantity),
	}).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update ticket inventory: %w", err)
	}

	// 12. Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 13. Log audit
	s.logAudit(ctx, "payment_initiated", "payment_intent", paymentIntent.ID, req.UserID, nil)

	// 14. Return response
	return &InitiatePaymentResponse{
		PaymentIntentID: paymentIntent.ID,
		PaymentGateway:  selectedGateway,
		RedirectURL:     "", // No redirect URL for simplified implementation
		Amount:          totalAmount,
		Currency:        req.Currency,
		Status:          paymentStatus,
		TicketIDs:       ticketIDs,
		ExpiresAt:       nil, // No expiration for simplified implementation
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

func (s *PaymentService) logAudit(ctx context.Context, action, entityType string, entityID uuid.UUID, actorID *uuid.UUID, changes map[string]interface{}) {
	audit := &models.PaymentAuditLog{
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		ActorID:    actorID,
		ActorType:  "user",
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

	s.logAudit(ctx, "payment_canceled", "payment_intent", paymentIntent.ID, &userID, nil)

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
	var paymentIntent models.PaymentIntent
	if err := s.db.First(&paymentIntent, paymentIntentID).Error; err != nil {
		return nil, fmt.Errorf("payment intent not found: %w", err)
	}

	// Check authorization
	if paymentIntent.UserID == nil || *paymentIntent.UserID != userID {
		return nil, utils.NewBusinessLogicError("Unauthorized.")
	}

	// Only succeeded payments can be refunded
	if paymentIntent.Status != "succeeded" {
		return nil, utils.NewBusinessLogicError("Only succeeded payments can be refunded")
	}

	// Validate refund conditions
	if err := s.validateRefundConditions(ctx, paymentIntentID, ticketIDs); err != nil {
		return nil, fmt.Errorf("refund not allowed: %w", err)
	}

	// Calculate refund amount based on tickets
	refundAmount := paymentIntent.TotalAmount * (float64(len(ticketIDs)) / float64(paymentIntent.Quantity))

	refund := &models.Refund{
		PaymentIntentID: paymentIntentID,
		GatewayRefundID: "", // Will be set when approved
		Amount:          refundAmount,
		Currency:        paymentIntent.Currency,
		Reason:          reason,
		Status:          "pending",
		InitiatedBy:     &userID,
		TicketCount:     len(ticketIDs),
	}

	if err := s.db.Create(refund).Error; err != nil {
		return nil, fmt.Errorf("failed to create refund request: %w", err)
	}

	s.logAudit(ctx, "refund_requested", "refund", refund.ID, &userID, nil)

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

// ApproveRefund approves and processes a refund
func (s *PaymentService) ApproveRefund(ctx context.Context, refundID, adminID uuid.UUID) (*models.Refund, error) {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var refund models.Refund
	if err := tx.Preload("PaymentIntent").First(&refund, refundID).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("refund not found: %w", err)
	}

	if refund.Status != "pending" {
		tx.Rollback()
		return nil, utils.NewBusinessLogicError("Refund is not in pending status.")
	}

	// Simplified - no gateway refund processing needed for current implementation
	// TODO: Implement gateway-specific refunds when Stripe is integrated

	// Update refund
	refund.Status = "approved"
	refund.ApprovedBy = &adminID
	now := time.Now()
	refund.ApprovedAt = &now

	if err := tx.Save(&refund).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update refund: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logAudit(ctx, "refund_approved", "refund", refund.ID, &adminID, nil)

	return &refund, nil
}

// RejectRefund rejects a refund request
func (s *PaymentService) RejectRefund(ctx context.Context, refundID, adminID uuid.UUID, reason string) (*models.Refund, error) {
	var refund models.Refund
	if err := s.db.First(&refund, refundID).Error; err != nil {
		return nil, fmt.Errorf("refund not found: %w", err)
	}

	if refund.Status != "pending" {
		return nil, utils.NewBusinessLogicError("Refund is not in pending status.")
	}

	refund.Status = "rejected"
	refund.RejectionReason = reason
	refund.ApprovedBy = &adminID
	now := time.Now()
	refund.ApprovedAt = &now

	if err := s.db.Save(&refund).Error; err != nil {
		return nil, fmt.Errorf("failed to update refund: %w", err)
	}

	s.logAudit(ctx, "refund_rejected", "refund", refund.ID, &adminID, nil)

	return &refund, nil
}

// AdminGetAllRefunds retrieves all refunds with filters
func (s *PaymentService) AdminGetAllRefunds(ctx context.Context, status string, page, limit int) ([]models.Refund, int64, error) {
	var refunds []models.Refund
	var total int64

	query := s.db.Model(&models.Refund{})
	if status != "" {
		query = query.Where("status = ?", status)
	}

	query.Count(&total)

	offset := (page - 1) * limit
	if err := query.Preload("PaymentIntent").Preload("PaymentIntent.Event").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&refunds).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to retrieve refunds: %w", err)
	}

	return refunds, total, nil
}
