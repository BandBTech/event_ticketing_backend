package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"event-ticketing-backend/internal/gateways"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PaymentService handles all payment operations using the gateway abstraction
type PaymentService struct {
	db             *gorm.DB
	gatewayFactory *gateways.Factory
	ticketService  *TicketService
	cfg            *config.Config
}

// NewPaymentService creates a new payment service instance
func NewPaymentService(db *gorm.DB, gatewayFactory *gateways.Factory, cfg *config.Config) *PaymentService {
	return &PaymentService{
		db:             db,
		gatewayFactory: gatewayFactory,
		cfg:            cfg,
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

	// Payment ID from the gateway
	// example: pi_1234567890
	GatewayPaymentID string `json:"gateway_payment_id"`

	// Selected payment gateway
	// example: stripe
	PaymentGateway string `json:"payment_gateway"`

	// Client secret for frontend integration (Stripe, etc.)
	// example: pi_secret_...
	ClientSecret string `json:"client_secret,omitempty"`

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
	var gateway gateways.PaymentGateway
	var gatewayConfig *models.PaymentGatewayConfig
	var err error

	if req.PaymentGateway != "" {
		// Special validation for cash payments
		if req.PaymentGateway == string(models.PaymentGatewayCash) {
			if !s.isCashPaymentAllowed(req.CustomerEmail) {
				return nil, utils.NewBusinessLogicError("Cash payments are not allowed for this email address.")
			}
		}

		// Use specified gateway
		gateway, err = s.gatewayFactory.GetGateway(req.PaymentGateway)
		if err != nil {
			return nil, fmt.Errorf("payment gateway not available: %w", err)
		}
		// Load config
		if err := s.db.Where("gateway_name = ?", req.PaymentGateway).First(&gatewayConfig).Error; err != nil {
			return nil, fmt.Errorf("gateway configuration not found: %w", err)
		}
	} else {
		// Auto-select based on criteria
		criteria := &gateways.GatewaySelectionCriteria{
			Country:  req.CountryCode,
			Currency: req.Currency,
			Amount:   totalAmount,
			UserID:   req.UserID,
		}
		gateway, gatewayConfig, err = s.gatewayFactory.SelectGateway(ctx, criteria)
		if err != nil {
			return nil, fmt.Errorf("failed to select payment gateway: %w", err)
		}
	}

	// 5. Calculate gateway fees and create payment intent
	var gatewayFee float64
	var gatewayResp *gateways.PaymentIntentResponse
	var idempotencyKey string

	// Calculate gateway fees
	gatewayFee = gateway.CalculateFees(totalAmount, req.Currency)

	// 6. Generate idempotency key
	idempotencyKey = fmt.Sprintf("purchase-%s-%s-%d", req.EventID, req.CustomerEmail, time.Now().UnixNano())

	// 7. Create payment intent with gateway
	gatewayReq := &gateways.PaymentIntentRequest{
		Amount:         totalAmount,
		Currency:       req.Currency,
		IdempotencyKey: idempotencyKey,
		CustomerEmail:  req.CustomerEmail,
		CustomerName:   req.CustomerName,
		Description:    fmt.Sprintf("%d x %s ticket(s) for %s", req.Quantity, tier.TierName, event.Title),
		Metadata: map[string]string{
			"event_id":       req.EventID.String(),
			"tier_id":        req.TierID.String(),
			"quantity":       fmt.Sprintf("%d", req.Quantity),
			"event_title":    event.Title,
			"tier_name":      tier.TierName,
			"customer_email": req.CustomerEmail,
		},
	}

	if req.UserID != nil {
		gatewayReq.Metadata["user_id"] = req.UserID.String()
	}
	if req.GuestUserID != nil {
		gatewayReq.Metadata["guest_user_id"] = req.GuestUserID.String()
	}

	gatewayResp, err = gateway.CreatePaymentIntent(ctx, gatewayReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create payment intent: %w", err)
	}

	// 8. Start database transaction
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 9. Create payment_intent record
	paymentIntent := &models.PaymentIntent{
		PaymentGateway:      gatewayConfig.GatewayName,
		GatewayPaymentID:    gatewayResp.GatewayPaymentID,
		GatewayClientSecret: gatewayResp.ClientSecret,
		IdempotencyKey:      idempotencyKey,
		UserID:              req.UserID,
		GuestUserID:         req.GuestUserID,
		CustomerEmail:       req.CustomerEmail,
		CustomerName:        req.CustomerName,
		CustomerPhone:       req.CustomerPhone,
		EventID:             req.EventID,
		TierID:              req.TierID,
		Quantity:            req.Quantity,
		Currency:            req.Currency,
		CurrencySymbol:      getCurrencySymbol(req.Currency),
		ExchangeRate:        1.0, // TODO: Implement currency conversion
		BaseCurrency:        "USD",
		BaseCurrencyAmount:  totalAmount,
		UnitPrice:           tier.Price,
		Subtotal:            subtotal,
		PlatformFee:         commissionAmount,
		GatewayFee:          gatewayFee,
		TotalAmount:         totalAmount,
		Status:              gatewayResp.Status,
		CommissionRate:      commissionRate,
		CommissionAmount:    commissionAmount,
		OrganizerNetAmount:  subtotal,
		PaymentMethodType:   "",
		CountryCode:         req.CountryCode,
		ExpiresAt:           gatewayResp.ExpiresAt,
	}

	if err := tx.Create(paymentIntent).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create payment intent record: %w", err)
	}

	// 10. Create tickets (status depends on payment status)
	ticketStatus := "pending_payment"
	if gatewayResp.Status == "succeeded" {
		ticketStatus = "active"
	}

	var ticketIDs []uuid.UUID
	for i := 0; i < req.Quantity; i++ {
		// Generate sequential ticket number (centralized, atomic per event)
		ticketNum, err := utils.GenerateEventTicketNumber(tx, req.EventID, tier.TierName, event.StartDate.Year())
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
			PaymentGateway:  models.PaymentGateway(gatewayConfig.GatewayName),
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
		PaymentIntentID:  paymentIntent.ID,
		GatewayPaymentID: gatewayResp.GatewayPaymentID,
		PaymentGateway:   gatewayConfig.GatewayName,
		ClientSecret:     gatewayResp.ClientSecret,
		RedirectURL:      gatewayResp.RedirectURL,
		Amount:           totalAmount,
		Currency:         req.Currency,
		Status:           gatewayResp.Status,
		TicketIDs:        ticketIDs,
		ExpiresAt:        gatewayResp.ExpiresAt,
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

// GetAvailableGateways returns available payment gateways for a country/currency
func (s *PaymentService) GetAvailableGateways(ctx context.Context, country, currency string) ([]gateways.GatewayInfo, error) {
	return s.gatewayFactory.GetAvailableGateways(ctx, country, currency)
}

// GetPaymentIntentByID retrieves a payment intent by ID
func (s *PaymentService) GetPaymentIntentByID(ctx context.Context, paymentIntentID uuid.UUID) (*models.PaymentIntent, error) {
	var paymentIntent models.PaymentIntent
	if err := s.db.Preload("Event").Preload("Tier").First(&paymentIntent, paymentIntentID).Error; err != nil {
		return nil, fmt.Errorf("payment intent not found: %w", err)
	}
	return &paymentIntent, nil
}

// HandleWebhook processes webhook events from payment gateways
func (s *PaymentService) HandleWebhook(ctx context.Context, gatewayName string, payload []byte, signature string) error {
	gateway, err := s.gatewayFactory.GetGateway(gatewayName)
	if err != nil {
		return fmt.Errorf("Gateway not found: %w", err)
	}

	webhookEvent, err := gateway.VerifyWebhook(ctx, payload, signature)
	if err != nil {
		return fmt.Errorf("webhook verification failed: %w", err)
	}

	// Log webhook event to models.WebhookEvent
	payloadMap := make(map[string]interface{})
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		payloadMap = map[string]interface{}{"raw": string(payload)}
	}

	webhookLog := &models.WebhookEvent{
		PaymentGateway: gatewayName,
		GatewayEventID: webhookEvent.EventID,
		EventType:      webhookEvent.Type,
		Payload:        payloadMap,
		Status:         "processing",
	}
	s.db.Create(webhookLog)

	// Process based on event type
	switch webhookEvent.Type {
	case "payment_intent.succeeded", "charge.succeeded":
		return s.handlePaymentSuccess(ctx, webhookEvent, webhookLog.ID)
	case "payment_intent.payment_failed", "charge.failed":
		return s.handlePaymentFailure(ctx, webhookEvent, webhookLog.ID)
	case "payment_intent.canceled":
		return s.handlePaymentCanceled(ctx, webhookEvent, webhookLog.ID)
	case "refund.created", "refund.updated":
		return s.handleRefundEvent(ctx, webhookEvent, webhookLog.ID)
	default:
		// Log unknown event type
		return nil
	}
}

// handlePaymentSuccess confirms payment and activates tickets with full security
// Supports both guest and logged-in users with proper authorization
func (s *PaymentService) handlePaymentSuccess(ctx context.Context, event *gateways.WebhookEvent, webhookLogID uuid.UUID) error {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Find payment intent with lock to prevent duplicate processing (idempotency)
	var paymentIntent models.PaymentIntent
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("gateway_payment_id = ?", event.PaymentIntentID).
		First(&paymentIntent).Error; err != nil {
		tx.Rollback()
		// Update webhook log
		s.db.Model(&models.WebhookEvent{}).Where("id = ?", webhookLogID).
			Updates(map[string]interface{}{"status": "failed", "last_error": fmt.Sprintf("payment intent not found: %v", err)})
		return fmt.Errorf("payment intent not found: %w", err)
	}

	// Idempotency check: Skip if already processed successfully
	if paymentIntent.Status == "succeeded" {
		tx.Rollback()
		s.db.Model(&models.WebhookEvent{}).Where("id = ?", webhookLogID).Update("status", "completed")
		return nil // Already processed successfully
	}

	// Update payment intent status and metadata
	now := time.Now()
	paymentIntent.Status = "succeeded"
	paymentIntent.SucceededAt = &now

	// Extract payment method details from webhook data (NON-SENSITIVE metadata only)
	if paymentMethodData, ok := event.Data["payment_method"].(map[string]interface{}); ok {
		paymentMethodDetails := make(map[string]interface{})
		if typeStr, ok := paymentMethodData["type"].(string); ok {
			paymentIntent.PaymentMethodType = typeStr
			paymentMethodDetails["type"] = typeStr
		}
		// Extract card details (only non-sensitive)
		if cardData, ok := paymentMethodData["card"].(map[string]interface{}); ok {
			if brand, ok := cardData["brand"].(string); ok {
				paymentMethodDetails["card_brand"] = brand
			}
			if last4, ok := cardData["last4"].(string); ok {
				paymentMethodDetails["last4"] = last4
			}
			if cardType, ok := cardData["type"].(string); ok {
				paymentMethodDetails["card_type"] = cardType
			}
			if country, ok := cardData["country"].(string); ok {
				paymentMethodDetails["country"] = country
			}
		}
		paymentIntent.PaymentMethodDetails = paymentMethodDetails
	}

	// Store gateway response
	paymentIntent.GatewayResponse = event.Data

	if err := tx.Save(&paymentIntent).Error; err != nil {
		tx.Rollback()
		s.db.Model(&models.WebhookEvent{}).Where("id = ?", webhookLogID).
			Updates(map[string]interface{}{"status": "failed", "last_error": fmt.Sprintf("failed to update payment intent: %v", err)})
		return fmt.Errorf("failed to update payment intent: %w", err)
	}

	// Activate tickets - handle both guest and logged-in users separately for security
	var updateQuery *gorm.DB
	if paymentIntent.UserID != nil {
		// Logged-in user - verify user_id match
		updateQuery = tx.Model(&models.Ticket{}).
			Where("event_id = ? AND tier_id = ? AND status = ? AND user_id = ? AND guest_user_id IS NULL",
				paymentIntent.EventID, paymentIntent.TierID, "pending_payment", paymentIntent.UserID)
	} else if paymentIntent.GuestUserID != nil {
		// Guest user - verify guest_user_id match
		updateQuery = tx.Model(&models.Ticket{}).
			Where("event_id = ? AND tier_id = ? AND status = ? AND guest_user_id = ? AND user_id IS NULL",
				paymentIntent.EventID, paymentIntent.TierID, "pending_payment", paymentIntent.GuestUserID)
	} else {
		tx.Rollback()
		s.db.Model(&models.WebhookEvent{}).Where("id = ?", webhookLogID).
			Updates(map[string]interface{}{"status": "failed", "last_error": "payment intent has no user_id or guest_user_id"})
		return utils.NewBusinessLogicError("Payment intent has no user_id or guest_user_id.")
	}

	// Activate exactly the number of tickets purchased
	result := updateQuery.Limit(paymentIntent.Quantity).Updates(map[string]interface{}{
		"status":       "active",
		"activated_at": now,
	})

	if result.Error != nil {
		tx.Rollback()
		s.db.Model(&models.WebhookEvent{}).Where("id = ?", webhookLogID).
			Updates(map[string]interface{}{"status": "failed", "last_error": fmt.Sprintf("failed to activate tickets: %v", result.Error)})
		return fmt.Errorf("failed to activate tickets: %w", result.Error)
	}

	// Verify correct number of tickets activated (prevent race conditions)
	if result.RowsAffected != int64(paymentIntent.Quantity) {
		tx.Rollback()
		s.db.Model(&models.WebhookEvent{}).Where("id = ?", webhookLogID).
			Updates(map[string]interface{}{
				"status":     "failed",
				"last_error": fmt.Sprintf("ticket count mismatch: expected %d, activated %d", paymentIntent.Quantity, result.RowsAffected),
			})
		return fmt.Errorf("ticket count mismatch: expected %d, activated %d", paymentIntent.Quantity, result.RowsAffected)
	}

	// Link webhook to payment intent
	tx.Model(&models.WebhookEvent{}).Where("id = ?", webhookLogID).
		Updates(map[string]interface{}{
			"payment_intent_id": paymentIntent.ID,
			"status":            "completed",
		})

	if err := tx.Commit().Error; err != nil {
		s.db.Model(&models.WebhookEvent{}).Where("id = ?", webhookLogID).
			Updates(map[string]interface{}{"status": "failed", "last_error": fmt.Sprintf("commit failed: %v", err)})
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Log audit (supports both user types)
	s.logAudit(ctx, "payment_succeeded", "payment_intent", paymentIntent.ID, paymentIntent.UserID, nil)

	return nil
}

// handlePaymentFailure handles failed payment with inventory restoration
// Supports both guest and logged-in users
func (s *PaymentService) handlePaymentFailure(ctx context.Context, event *gateways.WebhookEvent, webhookLogID uuid.UUID) error {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Find payment intent with lock
	var paymentIntent models.PaymentIntent
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("gateway_payment_id = ?", event.PaymentIntentID).
		First(&paymentIntent).Error; err != nil {
		tx.Rollback()
		s.db.Model(&models.WebhookEvent{}).Where("id = ?", webhookLogID).
			Updates(map[string]interface{}{"status": "failed", "last_error": fmt.Sprintf("payment intent not found: %v", err)})
		return fmt.Errorf("payment intent not found: %w", err)
	}

	// Idempotency: Skip if already processed
	if paymentIntent.Status == "failed" || paymentIntent.Status == "canceled" {
		tx.Rollback()
		s.db.Model(&models.WebhookEvent{}).Where("id = ?", webhookLogID).Update("status", "completed")
		return nil
	}

	// Update payment intent
	now := time.Now()
	paymentIntent.Status = "failed"
	paymentIntent.FailedAt = &now

	// Extract error message from webhook data
	if errorMsg, ok := event.Data["error_message"].(string); ok {
		paymentIntent.GatewayMetadata = map[string]interface{}{"failure_reason": errorMsg}
	} else if lastErr, ok := event.Data["last_payment_error"].(map[string]interface{}); ok {
		if msg, ok := lastErr["message"].(string); ok {
			paymentIntent.GatewayMetadata = map[string]interface{}{"failure_reason": msg}
		}
	}

	if err := tx.Save(&paymentIntent).Error; err != nil {
		tx.Rollback()
		s.db.Model(&models.WebhookEvent{}).Where("id = ?", webhookLogID).
			Updates(map[string]interface{}{"status": "failed", "last_error": fmt.Sprintf("failed to update payment intent: %v", err)})
		return fmt.Errorf("failed to update payment intent: %w", err)
	}

	// Cancel tickets - handle both user types
	var cancelQuery *gorm.DB
	if paymentIntent.UserID != nil {
		cancelQuery = tx.Model(&models.Ticket{}).
			Where("event_id = ? AND tier_id = ? AND status = ? AND user_id = ? AND guest_user_id IS NULL",
				paymentIntent.EventID, paymentIntent.TierID, "pending_payment", paymentIntent.UserID)
	} else if paymentIntent.GuestUserID != nil {
		cancelQuery = tx.Model(&models.Ticket{}).
			Where("event_id = ? AND tier_id = ? AND status = ? AND guest_user_id = ? AND user_id IS NULL",
				paymentIntent.EventID, paymentIntent.TierID, "pending_payment", paymentIntent.GuestUserID)
	} else {
		tx.Rollback()
		return utils.NewBusinessLogicError("Payment intent has no user_id or guest_user_id.")
	}

	if err := cancelQuery.Limit(paymentIntent.Quantity).Update("status", "canceled").Error; err != nil {
		tx.Rollback()
		s.db.Model(&models.WebhookEvent{}).Where("id = ?", webhookLogID).
			Updates(map[string]interface{}{"status": "failed", "last_error": fmt.Sprintf("failed to cancel tickets: %v", err)})
		return fmt.Errorf("failed to cancel tickets: %w", err)
	}

	// Restore inventory with race condition protection
	var tier models.EventTier
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&tier, paymentIntent.TierID).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("tier not found: %w", err)
	}

	result := tx.Model(&tier).Updates(map[string]interface{}{
		"sold":      gorm.Expr("GREATEST(sold - ?, 0)", paymentIntent.Quantity), // Prevent negative values
		"available": gorm.Expr("available + ?", paymentIntent.Quantity),
	})

	if result.Error != nil {
		tx.Rollback()
		return fmt.Errorf("failed to restore inventory: %w", result.Error)
	}

	// Link webhook to payment intent
	tx.Model(&models.WebhookEvent{}).Where("id = ?", webhookLogID).
		Updates(map[string]interface{}{
			"payment_intent_id": paymentIntent.ID,
			"status":            "completed",
		})

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logAudit(ctx, "payment_failed", "payment_intent", paymentIntent.ID, paymentIntent.UserID, nil)

	return nil
}

// handlePaymentCanceled handles canceled payment
func (s *PaymentService) handlePaymentCanceled(ctx context.Context, event *gateways.WebhookEvent, webhookLogID uuid.UUID) error {
	return s.handlePaymentFailure(ctx, event, webhookLogID) // Same logic as failure
}

// handleRefundEvent handles refund webhook events
func (s *PaymentService) handleRefundEvent(ctx context.Context, event *gateways.WebhookEvent, webhookLogID uuid.UUID) error {
	// Update webhook log
	s.db.Model(&models.WebhookEvent{}).Where("id = ?", webhookLogID).Update("status", "completed")
	// TODO: Implement refund event handling when refund gateway integration is complete
	return nil
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

	// Cancel with gateway
	gateway, err := s.gatewayFactory.GetGateway(paymentIntent.PaymentGateway)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("gateway not found: %w", err)
	}

	if err := gateway.CancelPaymentIntent(ctx, paymentIntent.GatewayPaymentID); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to cancel with gateway: %w", err)
	}

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

	// Process refund with gateway
	gateway, err := s.gatewayFactory.GetGateway(refund.PaymentIntent.PaymentGateway)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gateway not found: %w", err)
	}

	gatewayRefund, err := gateway.CreateRefund(ctx, &gateways.RefundRequest{
		GatewayPaymentID: refund.PaymentIntent.GatewayPaymentID,
		Amount:           refund.Amount,
		Currency:         refund.Currency,
		Reason:           refund.Reason,
	})
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create refund with gateway: %w", err)
	}

	// Update refund
	refund.GatewayRefundID = gatewayRefund.GatewayRefundID
	refund.Status = gatewayRefund.Status
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

// GetGatewayConfigs retrieves all gateway configurations
func (s *PaymentService) GetGatewayConfigs(ctx context.Context) ([]models.PaymentGatewayConfig, error) {
	var configs []models.PaymentGatewayConfig
	if err := s.db.Find(&configs).Error; err != nil {
		return nil, fmt.Errorf("failed to retrieve gateway configurations: %w", err)
	}
	return configs, nil
}

// GetPaymentAnalytics retrieves payment analytics
func (s *PaymentService) GetPaymentAnalytics(ctx context.Context, startDate, endDate string) (map[string]interface{}, error) {
	var analytics struct {
		TotalRevenue   float64
		TotalPayments  int64
		SuccessRate    float64
		AverageAmount  float64
		RefundRate     float64
		TopGateway     string
		TopCurrency    string
		TotalRefunds   int64
		RefundedAmount float64
	}

	query := s.db.Model(&models.PaymentIntent{})
	if startDate != "" {
		query = query.Where("created_at >= ?", startDate)
	}
	if endDate != "" {
		query = query.Where("created_at <= ?", endDate)
	}

	// Total revenue and payments
	query.Where("status = ?", "succeeded").
		Select("SUM(total_amount) as total_revenue, COUNT(*) as total_payments, AVG(total_amount) as average_amount").
		Scan(&analytics)

	// Total payments including failed
	var totalCount int64
	s.db.Model(&models.PaymentIntent{}).Count(&totalCount)
	if totalCount > 0 {
		analytics.SuccessRate = (float64(analytics.TotalPayments) / float64(totalCount)) * 100
	}

	// Refund stats
	refundQuery := s.db.Model(&models.Refund{})
	if startDate != "" {
		refundQuery = refundQuery.Where("created_at >= ?", startDate)
	}
	if endDate != "" {
		refundQuery = refundQuery.Where("created_at <= ?", endDate)
	}
	refundQuery.Where("status = ?", "succeeded").
		Select("COUNT(*) as total_refunds, SUM(amount) as refunded_amount").
		Scan(&analytics)

	if analytics.TotalPayments > 0 {
		analytics.RefundRate = (float64(analytics.TotalRefunds) / float64(analytics.TotalPayments)) * 100
	}

	// Top gateway
	var topGateway struct {
		Gateway string
		Count   int64
	}
	s.db.Model(&models.PaymentIntent{}).
		Select("payment_gateway as gateway, COUNT(*) as count").
		Where("status = ?", "succeeded").
		Group("payment_gateway").
		Order("count DESC").
		Limit(1).
		Scan(&topGateway)
	analytics.TopGateway = topGateway.Gateway

	// Top currency
	var topCurrency struct {
		Currency string
		Count    int64
	}
	s.db.Model(&models.PaymentIntent{}).
		Select("currency, COUNT(*) as count").
		Where("status = ?", "succeeded").
		Group("currency").
		Order("count DESC").
		Limit(1).
		Scan(&topCurrency)
	analytics.TopCurrency = topCurrency.Currency

	return map[string]interface{}{
		"total_revenue":   analytics.TotalRevenue,
		"total_payments":  analytics.TotalPayments,
		"success_rate":    analytics.SuccessRate,
		"average_amount":  analytics.AverageAmount,
		"refund_rate":     analytics.RefundRate,
		"top_gateway":     analytics.TopGateway,
		"top_currency":    analytics.TopCurrency,
		"total_refunds":   analytics.TotalRefunds,
		"refunded_amount": analytics.RefundedAmount,
	}, nil
}

// ========================================
// Payment Gateway Admin Management Methods
// ========================================

// GetSupportedGatewayTypes returns list of supported gateway types with their metadata
// This now fetches from a dynamic registry, making it fully extensible
func (s *PaymentService) GetSupportedGatewayTypes(ctx context.Context) ([]map[string]interface{}, error) {
	// TODO: Move this to a database table 'gateway_types' for full admin control
	// For now, return types from the gateway initializer registry

	// Get all registered gateway types from the factory's initializer registry
	supportedTypes := []map[string]interface{}{
		{
			"name":             "stripe",
			"display_name":     "Stripe",
			"description":      "Global payment processing platform. Auto-converts 135+ currencies.",
			"features":         []string{"cards", "wallets", "bank_transfers", "subscriptions", "auto_conversion"},
			"requires_webhook": true,
			"setup_guide_url":  "https://stripe.com/docs/keys",
			"status":           "implemented",
		},
		{
			"name":             "paypal",
			"display_name":     "PayPal",
			"description":      "Digital wallet. Auto-converts 25+ currencies.",
			"features":         []string{"paypal_wallet", "cards", "bank_transfers", "auto_conversion"},
			"requires_webhook": true,
			"setup_guide_url":  "https://developer.paypal.com/api/rest/",
			"status":           "pending_implementation",
		},
		{
			"name":             "esewa",
			"display_name":     "eSewa",
			"description":      "Nepal's digital wallet. Gateway handles currency conversions.",
			"features":         []string{"wallet", "bank_transfer"},
			"requires_webhook": true,
			"setup_guide_url":  "https://developer.esewa.com.np/",
			"status":           "pending_implementation",
		},
		{
			"name":             "khalti",
			"display_name":     "Khalti",
			"description":      "Nepal's digital wallet. Gateway handles currency conversions.",
			"features":         []string{"wallet", "cards", "bank_transfer"},
			"requires_webhook": true,
			"setup_guide_url":  "https://docs.khalti.com/",
			"status":           "pending_implementation",
		},
		{
			"name":             "razorpay",
			"display_name":     "Razorpay",
			"description":      "India's payment gateway. Gateway handles currency conversions.",
			"features":         []string{"cards", "upi", "wallets", "netbanking"},
			"requires_webhook": true,
			"setup_guide_url":  "https://razorpay.com/docs/",
			"status":           "pending_implementation",
		},
	}

	// Note: In production, move this to a 'gateway_types' table:
	// var gatewayTypes []models.GatewayType
	// if err := s.db.Find(&gatewayTypes).Error; err != nil {
	//     return nil, err
	// }
	// This allows admins to add new gateway types via admin panel

	return supportedTypes, nil
}

// GetAllGatewayConfigs retrieves all payment gateway configurations with pagination
func (s *PaymentService) GetAllGatewayConfigs(ctx context.Context, page, limit int) ([]models.PaymentGatewayConfig, int64, error) {
	var configs []models.PaymentGatewayConfig
	var total int64

	// Count total
	if err := s.db.Model(&models.PaymentGatewayConfig{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Get paginated results
	offset := (page - 1) * limit
	if err := s.db.WithContext(ctx).
		Order("priority ASC, created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&configs).Error; err != nil {
		return nil, 0, err
	}

	return configs, total, nil
}

// CreateGatewayConfig creates a new payment gateway configuration
func (s *PaymentService) CreateGatewayConfig(ctx context.Context, req *models.CreatePaymentGatewayConfigRequest, adminID uuid.UUID) (*models.PaymentGatewayConfig, error) {
	// Create the config from request
	config := &models.PaymentGatewayConfig{
		ID:                  uuid.New(),
		GatewayName:         req.GatewayName,
		DisplayName:         req.DisplayName,
		IsEnabled:           req.IsEnabled,
		IsTestMode:          req.IsTestMode,
		Priority:            req.Priority,
		SupportedCountries:  req.SupportedCountries,
		SupportedCurrencies: req.SupportedCurrencies,
		APIKey:              req.APIKey,
		APISecret:           req.APISecret,
		WebhookSecret:       req.WebhookSecret,
		Config:              req.Config,
		PercentageFee:       req.PercentageFee,
		FixedFee:            req.FixedFee,
		MinAmount:           req.MinAmount,
		MaxAmount:           req.MaxAmount,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
	config.UpdatedAt = time.Now()

	// Validate required fields
	if config.GatewayName == "" {
		return nil, utils.NewBusinessLogicError("Gateway_name is required.")
	}
	if config.DisplayName == "" {
		return nil, utils.NewBusinessLogicError("Display_name is required.")
	}

	// Check encryption key is configured
	if s.cfg != nil && s.cfg.Security.EncryptionKey == "" {
		return nil, utils.NewBusinessLogicError("CREDENTIAL_ENCRYPTION_KEY not configured in .env.")
	}

	// Encrypt credentials before saving to database
	if s.cfg != nil && s.cfg.Security.EncryptionKey != "" {
		encryptionKey := s.cfg.Security.EncryptionKey
		keyID := s.cfg.Security.CurrentKeyID

		// Encrypt API key with key ID
		if config.APIKeyEncrypted != "" {
			encrypted, err := utils.EncryptAES256GCMWithID(config.APIKeyEncrypted, encryptionKey, keyID)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt API key: %w", err)
			}
			config.APIKeyEncrypted = encrypted
		}

		// Encrypt API secret with key ID
		if config.APISecretEncrypted != "" {
			encrypted, err := utils.EncryptAES256GCMWithID(config.APISecretEncrypted, encryptionKey, keyID)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt API secret: %w", err)
			}
			config.APISecretEncrypted = encrypted
		}

		// Encrypt webhook secret with key ID
		if config.WebhookSecretEncrypted != "" {
			encrypted, err := utils.EncryptAES256GCMWithID(config.WebhookSecretEncrypted, encryptionKey, keyID)
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt webhook secret: %w", err)
			}
			config.WebhookSecretEncrypted = encrypted
		}
	}

	// Check for duplicate gateway name
	var existingCount int64
	if err := s.db.Model(&models.PaymentGatewayConfig{}).
		Where("gateway_name = ?", config.GatewayName).
		Count(&existingCount).Error; err != nil {
		return nil, err
	}
	if existingCount > 0 {
		return nil, fmt.Errorf("gateway configuration for %s already exists", config.GatewayName)
	}

	// Create the configuration
	if err := s.db.WithContext(ctx).Create(config).Error; err != nil {
		return nil, err
	}

	// Log audit trail
	auditLog := &models.PaymentAuditLog{
		ID:         uuid.New(),
		Action:     "gateway_config_created",
		EntityType: "payment_gateway_config",
		EntityID:   config.ID,
		ActorID:    &adminID,
		ActorType:  "admin",
		Metadata: map[string]interface{}{
			"gateway_name": config.GatewayName,
			"display_name": config.DisplayName,
			"is_enabled":   config.IsEnabled,
			"is_test_mode": config.IsTestMode,
		},
		Timestamp: time.Now(),
		CreatedAt: time.Now(),
	}
	s.db.Create(auditLog)

	return config, nil
}

// GetGatewayConfigByID retrieves a gateway configuration by ID
func (s *PaymentService) GetGatewayConfigByID(ctx context.Context, gatewayID uuid.UUID) (*models.PaymentGatewayConfig, error) {
	var config models.PaymentGatewayConfig
	if err := s.db.WithContext(ctx).
		Where("id = ?", gatewayID).
		First(&config).Error; err != nil {
		return nil, err
	}
	return &config, nil
}

// UpdateGatewayConfig updates an existing gateway configuration
func (s *PaymentService) UpdateGatewayConfig(ctx context.Context, gatewayID uuid.UUID, updates *models.PaymentGatewayConfig, adminID uuid.UUID) (*models.PaymentGatewayConfig, error) {
	// Fetch existing configuration
	var existing models.PaymentGatewayConfig
	if err := s.db.WithContext(ctx).
		Where("id = ?", gatewayID).
		First(&existing).Error; err != nil {
		return nil, err
	}

	// Update fields
	updateMap := map[string]interface{}{
		"updated_at": time.Now(),
	}

	if updates.DisplayName != "" {
		updateMap["display_name"] = updates.DisplayName
	}
	if updates.APIKeyEncrypted != "" {
		updateMap["api_key_encrypted"] = updates.APIKeyEncrypted
	}
	if updates.APISecretEncrypted != "" {
		updateMap["api_secret_encrypted"] = updates.APISecretEncrypted
	}
	if updates.WebhookSecretEncrypted != "" {
		updateMap["webhook_secret_encrypted"] = updates.WebhookSecretEncrypted
	}
	if len(updates.SupportedCountries) > 0 {
		updateMap["supported_countries"] = updates.SupportedCountries
	}
	if len(updates.SupportedCurrencies) > 0 {
		updateMap["supported_currencies"] = updates.SupportedCurrencies
	}
	if updates.Priority != 0 {
		updateMap["priority"] = updates.Priority
	}
	if updates.Config != nil {
		updateMap["config"] = updates.Config
	}

	// Update the configuration
	if err := s.db.WithContext(ctx).
		Model(&existing).
		Updates(updateMap).Error; err != nil {
		return nil, err
	}

	// Log audit trail
	auditLog := &models.PaymentAuditLog{
		ID:         uuid.New(),
		Action:     "gateway_config_updated",
		EntityType: "payment_gateway_config",
		EntityID:   gatewayID,
		ActorID:    &adminID,
		ActorType:  "admin",
		Metadata: map[string]interface{}{
			"gateway_id":   gatewayID,
			"gateway_name": existing.GatewayName,
			"updated_fields": func() []string {
				fields := []string{}
				for k := range updateMap {
					if k != "updated_at" {
						fields = append(fields, k)
					}
				}
				return fields
			}(),
		},
		Timestamp: time.Now(),
		CreatedAt: time.Now(),
	}
	s.db.Create(auditLog)

	// Reload the configuration
	if err := s.db.WithContext(ctx).
		Where("id = ?", gatewayID).
		First(&existing).Error; err != nil {
		return nil, err
	}

	return &existing, nil
}

// DeleteGatewayConfig deletes a gateway configuration
func (s *PaymentService) DeleteGatewayConfig(ctx context.Context, gatewayID uuid.UUID) error {
	// Check if gateway has any payments
	var paymentCount int64
	if err := s.db.Model(&models.PaymentIntent{}).
		Where("gateway_config_id = ?", gatewayID).
		Count(&paymentCount).Error; err != nil {
		return err
	}

	if paymentCount > 0 {
		return fmt.Errorf("cannot delete gateway configuration: %d payments exist using this gateway", paymentCount)
	}

	// Delete the configuration
	if err := s.db.WithContext(ctx).
		Where("id = ?", gatewayID).
		Delete(&models.PaymentGatewayConfig{}).Error; err != nil {
		return err
	}

	return nil
}

// ToggleGatewayStatus enables or disables a payment gateway
func (s *PaymentService) ToggleGatewayStatus(ctx context.Context, gatewayID uuid.UUID, isEnabled bool) (*models.PaymentGatewayConfig, error) {
	var config models.PaymentGatewayConfig
	if err := s.db.WithContext(ctx).
		Model(&config).
		Where("id = ?", gatewayID).
		Update("is_enabled", isEnabled).Error; err != nil {
		return nil, err
	}

	// Reload the configuration
	if err := s.db.WithContext(ctx).
		Where("id = ?", gatewayID).
		First(&config).Error; err != nil {
		return nil, err
	}

	return &config, nil
}

// ToggleGatewayMode toggles between test (sandbox) and live (production) mode
func (s *PaymentService) ToggleGatewayMode(ctx context.Context, gatewayID uuid.UUID, isTestMode bool) (*models.PaymentGatewayConfig, error) {
	var config models.PaymentGatewayConfig
	if err := s.db.WithContext(ctx).
		Model(&config).
		Where("id = ?", gatewayID).
		Update("is_test_mode", isTestMode).Error; err != nil {
		return nil, err
	}

	// Reload the configuration
	if err := s.db.WithContext(ctx).
		Where("id = ?", gatewayID).
		First(&config).Error; err != nil {
		return nil, err
	}

	return &config, nil
}

// TestGatewayConnection tests the connection and credentials of a payment gateway
func (s *PaymentService) TestGatewayConnection(ctx context.Context, gatewayID uuid.UUID) (map[string]interface{}, error) {
	// Fetch gateway configuration
	config, err := s.GetGatewayConfigByID(ctx, gatewayID)
	if err != nil {
		return nil, err
	}

	// Get the gateway instance
	gateway, err := s.gatewayFactory.GetGateway(config.GatewayName)
	if err != nil {
		return map[string]interface{}{
			"success":   false,
			"gateway":   config.GatewayName,
			"message":   "Gateway not initialized",
			"error":     err.Error(),
			"tested_at": time.Now(),
		}, nil
	}

	// Create a small test payment intent (will not be charged)
	testRequest := &gateways.PaymentIntentRequest{
		Amount:         100, // $1.00 or equivalent
		Currency:       "USD",
		Description:    "Connection test - will not be charged",
		CustomerEmail:  "test@example.com",
		CustomerName:   "Test User",
		SuccessURL:     "https://example.com/success",
		CancelURL:      "https://example.com/cancel",
		IdempotencyKey: uuid.New().String(),
		Metadata: map[string]string{
			"type": "connection_test",
		},
	}

	// Try to create a payment intent
	response, err := gateway.CreatePaymentIntent(ctx, testRequest)
	if err != nil {
		return map[string]interface{}{
			"success":   false,
			"gateway":   config.GatewayName,
			"message":   "Connection test failed",
			"error":     err.Error(),
			"tested_at": time.Now(),
		}, nil
	}

	// Cancel the test payment immediately
	if response.GatewayPaymentID != "" {
		_ = gateway.CancelPaymentIntent(ctx, response.GatewayPaymentID)
	}

	return map[string]interface{}{
		"success":   true,
		"gateway":   config.GatewayName,
		"message":   "Connection test successful",
		"mode":      map[string]bool{"test_mode": config.IsTestMode},
		"tested_at": time.Now(),
	}, nil
}

// ReloadGateways reloads all payment gateways from the database
func (s *PaymentService) ReloadGateways(ctx context.Context) error {
	return s.gatewayFactory.InitializeGatewaysFromDB(ctx)
}

// ValidateGatewayConfig validates a gateway configuration before saving
func (s *PaymentService) ValidateGatewayConfig(ctx context.Context, gatewayID uuid.UUID) (map[string]interface{}, error) {
	// Fetch gateway configuration
	config, err := s.GetGatewayConfigByID(ctx, gatewayID)
	if err != nil {
		return nil, err
	}

	validation := map[string]interface{}{
		"is_valid": true,
		"errors":   []string{},
		"warnings": []string{},
	}

	errors := []string{}
	warnings := []string{}

	// Validate required fields
	if config.GatewayName == "" {
		errors = append(errors, "gateway_name is required")
	}
	if config.DisplayName == "" {
		errors = append(errors, "display_name is required")
	}

	// Validate credentials - check if at least API keys are present
	if config.APIKeyEncrypted == "" && config.APISecretEncrypted == "" {
		errors = append(errors, "at least one API credential (api_key or api_secret) is required")
	}

	// Check webhook secret for production
	if !config.IsTestMode && config.WebhookSecretEncrypted == "" {
		warnings = append(warnings, "webhook_secret is recommended for production mode")
	}

	// Note: Countries and currencies are dynamic - gateways handle conversion
	if len(config.SupportedCurrencies) == 0 {
		warnings = append(warnings, "no supported_currencies specified - gateway will handle all currencies")
	}

	if len(config.SupportedCountries) == 0 {
		warnings = append(warnings, "no supported_countries specified - gateway will be available for all countries")
	}

	// Check if in test mode
	if config.IsTestMode {
		warnings = append(warnings, "gateway is in TEST/SANDBOX mode")
	}

	// Check if disabled
	if !config.IsEnabled {
		warnings = append(warnings, "gateway is currently disabled")
	}

	validation["errors"] = errors
	validation["warnings"] = warnings
	validation["is_valid"] = len(errors) == 0

	return validation, nil
}

// GetAuditLogs retrieves payment audit logs with filtering and pagination
func (s *PaymentService) GetAuditLogs(ctx context.Context, page, limit int, filters map[string]interface{}) ([]models.PaymentAuditLog, int64, error) {
	var logs []models.PaymentAuditLog
	var total int64

	query := s.db.Model(&models.PaymentAuditLog{}).Preload("Actor").Preload("Event")

	// Apply filters
	for key, value := range filters {
		query = query.Where(key+" = ?", value)
	}

	// Get total count
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Get paginated results
	offset := (page - 1) * limit
	if err := query.Order("timestamp DESC").Offset(offset).Limit(limit).Find(&logs).Error; err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// RetryWebhook retries processing a failed webhook event
func (s *PaymentService) RetryWebhook(ctx context.Context, webhookID uuid.UUID) error {
	var webhook models.WebhookEvent
	if err := s.db.First(&webhook, webhookID).Error; err != nil {
		return fmt.Errorf("webhook not found: %w", err)
	}

	// Check if webhook is in a retryable state
	if webhook.Status == "processed" {
		return utils.NewBusinessLogicError("Webhook already processed successfully.")
	}

	if webhook.ProcessedCount >= 5 {
		return utils.NewBusinessLogicError("Webhook has reached maximum retry attempts.")
	}

	// Get the gateway to process the webhook
	gateway, err := s.gatewayFactory.GetGateway(webhook.PaymentGateway)
	if err != nil {
		return fmt.Errorf("gateway not available: %w", err)
	}

	// Convert payload to JSON bytes
	payloadBytes, err := json.Marshal(webhook.Payload)
	if err != nil {
		return fmt.Errorf("invalid webhook payload: %w", err)
	}

	// Extract signature from headers (if available)
	signature := ""
	if webhook.Headers != nil {
		if sig, ok := webhook.Headers["stripe-signature"].(string); ok {
			signature = sig
		} else if sig, ok := webhook.Headers["paypal-transmission-sig"].(string); ok {
			signature = sig
		}
	}

	// Verify and process webhook
	_, err = gateway.VerifyWebhook(ctx, payloadBytes, signature)
	if err != nil {
		// Update webhook with error
		webhook.Status = "failed"
		webhook.ProcessedCount++
		webhook.LastError = err.Error()
		s.db.Save(&webhook)
		return fmt.Errorf("webhook verification failed: %w", err)
	}

	// Mark as processed
	webhook.Status = "processed"
	webhook.ProcessedCount++
	webhook.LastError = ""
	now := time.Now()
	webhook.ProcessedAt = &now

	if err := s.db.Save(&webhook).Error; err != nil {
		return fmt.Errorf("failed to update webhook status: %w", err)
	}

	return nil
}

// RetryTransaction retries processing a failed payment intent
func (s *PaymentService) RetryTransaction(ctx context.Context, paymentIntentID uuid.UUID) error {
	var paymentIntent models.PaymentIntent
	if err := s.db.First(&paymentIntent, paymentIntentID).Error; err != nil {
		return fmt.Errorf("payment intent not found: %w", err)
	}

	// Check if payment intent is in a retryable state
	if paymentIntent.Status == "succeeded" {
		return utils.NewBusinessLogicError("Payment intent already succeeded.")
	}

	if paymentIntent.Status != "failed" && paymentIntent.Status != "canceled" {
		return utils.NewBusinessLogicError("Payment intent is not in a failed or canceled state.")
	}

	// Get the gateway
	gateway, err := s.gatewayFactory.GetGateway(paymentIntent.PaymentGateway)
	if err != nil {
		return fmt.Errorf("gateway not available: %w", err)
	}

	// Get current payment intent status from gateway
	response, err := gateway.GetPaymentIntent(ctx, paymentIntent.GatewayPaymentID)
	if err != nil {
		return fmt.Errorf("failed to fetch payment intent from gateway: %w", err)
	}

	// Update local payment intent with latest status
	paymentIntent.Status = response.Status
	if response.Status == "succeeded" {
		now := time.Now()
		paymentIntent.SucceededAt = &now
	}

	if err := s.db.Save(&paymentIntent).Error; err != nil {
		return fmt.Errorf("failed to update payment intent: %w", err)
	}

	return nil
}

// ReencryptAllGatewayConfigs re-encrypts all gateway configurations with the current encryption key
func (s *PaymentService) ReencryptAllGatewayConfigs(ctx context.Context) (int, error) {
	if s.cfg == nil || s.cfg.Security.EncryptionKey == "" {
		return 0, utils.NewBusinessLogicError("Encryption key not configured.")
	}

	var configs []models.PaymentGatewayConfig
	if err := s.db.Find(&configs).Error; err != nil {
		return 0, fmt.Errorf("failed to fetch gateway configs: %w", err)
	}

	encryptionKey := s.cfg.Security.EncryptionKey
	rotationKeys := s.cfg.Security.RotationKeys
	keyID := s.cfg.Security.CurrentKeyID
	count := 0

	for _, config := range configs {
		updated := false

		// Decrypt and re-encrypt API key
		if config.APIKeyEncrypted != "" {
			decrypted, err := utils.DecryptAES256GCMWithRotation(config.APIKeyEncrypted, encryptionKey, rotationKeys)
			if err != nil {
				return count, fmt.Errorf("failed to decrypt API key for %s: %w", config.GatewayName, err)
			}

			reencrypted, err := utils.EncryptAES256GCMWithID(decrypted, encryptionKey, keyID)
			if err != nil {
				return count, fmt.Errorf("failed to re-encrypt API key for %s: %w", config.GatewayName, err)
			}

			config.APIKeyEncrypted = reencrypted
			updated = true
		}

		// Decrypt and re-encrypt API secret
		if config.APISecretEncrypted != "" {
			decrypted, err := utils.DecryptAES256GCMWithRotation(config.APISecretEncrypted, encryptionKey, rotationKeys)
			if err != nil {
				return count, fmt.Errorf("failed to decrypt API secret for %s: %w", config.GatewayName, err)
			}

			reencrypted, err := utils.EncryptAES256GCMWithID(decrypted, encryptionKey, keyID)
			if err != nil {
				return count, fmt.Errorf("failed to re-encrypt API secret for %s: %w", config.GatewayName, err)
			}

			config.APISecretEncrypted = reencrypted
			updated = true
		}

		// Decrypt and re-encrypt webhook secret
		if config.WebhookSecretEncrypted != "" {
			decrypted, err := utils.DecryptAES256GCMWithRotation(config.WebhookSecretEncrypted, encryptionKey, rotationKeys)
			if err != nil {
				return count, fmt.Errorf("failed to decrypt webhook secret for %s: %w", config.GatewayName, err)
			}

			reencrypted, err := utils.EncryptAES256GCMWithID(decrypted, encryptionKey, keyID)
			if err != nil {
				return count, fmt.Errorf("failed to re-encrypt webhook secret for %s: %w", config.GatewayName, err)
			}

			config.WebhookSecretEncrypted = reencrypted
			updated = true
		}

		// Save updated config
		if updated {
			if err := s.db.Save(&config).Error; err != nil {
				return count, fmt.Errorf("failed to save re-encrypted config for %s: %w", config.GatewayName, err)
			}
			count++
		}
	}

	return count, nil
}

// VerifyAdminPassword verifies that the provided password matches the admin user's password
func (s *PaymentService) VerifyAdminPassword(ctx context.Context, adminID uuid.UUID, password string) error {
	var user models.User
	if err := s.db.Where("id = ? AND role = ?", adminID, "admin").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("admin user not found")
		}
		return fmt.Errorf("failed to retrieve admin user: %w", err)
	}

	if !user.CheckPassword(password) {
		return fmt.Errorf("invalid password")
	}

	return nil
}
