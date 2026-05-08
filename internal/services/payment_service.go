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

// generateCheckoutToken creates a unique token for payment checkout
func generateCheckoutToken() string {
	return utils.GenerateCheckoutToken("checkout")
}

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
