package handlers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v74"
	"github.com/stripe/stripe-go/v74/webhook"
	"gorm.io/gorm"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"
)

// findCheckoutSessionByEvent finds a checkout session using multiple lookup strategies
// for different types of Stripe webhook events
func (h *WebhookHandler) findCheckoutSessionByEvent(tx *gorm.DB, eventType string, eventData interface{}, requestID string) (*models.CheckoutSession, error) {
	var checkoutSession models.CheckoutSession

	switch eventType {
	case "payment_intent.succeeded", "payment_intent.payment_failed", "payment_intent.canceled":
		// For payment intent events, try checkout_token from metadata first
		paymentIntent, ok := eventData.(*stripe.PaymentIntent)
		if !ok {
			return nil, fmt.Errorf("invalid payment intent data")
		}

		// Strategy 1: Try checkout_token from metadata
		if checkoutToken, ok := paymentIntent.Metadata["checkout_token"]; ok && checkoutToken != "" {
			if err := tx.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err == nil {
				return &checkoutSession, nil // Found by checkout_token
			}
			// Log but continue to fallback strategies
			log.Printf("WARN: Checkout session not found by checkout_token: %s", checkoutToken)
		}

		// Strategy 2: Try to find by payment_intent_id in gateway_data
		// This handles cases where checkout_token is missing but we stored the payment_intent_id
		if err := tx.Where("gateway_data->>'payment_intent_id' = ?", paymentIntent.ID).First(&checkoutSession).Error; err == nil {
			return &checkoutSession, nil // Found by payment_intent_id
		}

		return nil, fmt.Errorf("checkout session not found for payment_intent: %s", paymentIntent.ID)

	case "checkout.session.completed":
		// For checkout session events, use stripe_session_id
		checkoutSessionData, ok := eventData.(*stripe.CheckoutSession)
		if !ok {
			return nil, fmt.Errorf("invalid checkout session data")
		}

		if err := tx.Where("stripe_session_id = ?", checkoutSessionData.ID).First(&checkoutSession).Error; err != nil {
			return nil, fmt.Errorf("checkout session not found for stripe_session_id: %s", checkoutSessionData.ID)
		}

		return &checkoutSession, nil

	default:
		return nil, fmt.Errorf("unsupported event type for checkout session lookup: %s", eventType)
	}
}

// WebhookHandler handles Stripe webhook events
type WebhookHandler struct {
	ticketService *services.TicketService
	config        *config.Config
}

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(ticketService *services.TicketService, config *config.Config) *WebhookHandler {
	return &WebhookHandler{
		ticketService: ticketService,
		config:        config,
	}
}

// StripeWebhook godoc
// @Summary Handle Stripe webhook events
// @Description Process webhook events from Stripe for payment processing with comprehensive security and audit logging
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param Stripe-Signature header string true "Stripe webhook signature"
// @Param X-Webhook-ID header string false "Idempotency key for webhook processing"
// @Success 200 {object} utils.Response "Webhook processed successfully"
// @Failure 400 {object} utils.Response "Invalid webhook signature or payload"
// @Failure 409 {object} utils.Response "Webhook already processed"
// @Failure 429 {object} utils.Response "Rate limit exceeded"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/webhooks/stripe [post]
func (h *WebhookHandler) StripeWebhook(c *gin.Context) {
	ctx := context.Background()
	requestID := c.GetString("request_id")
	startTime := time.Now()

	// Extract headers for logging
	headers := make(map[string]interface{})
	for key, values := range c.Request.Header {
		if strings.HasPrefix(strings.ToLower(key), "stripe-") ||
			strings.HasPrefix(strings.ToLower(key), "x-") {
			headers[key] = values[0] // Store first value
		}
	}

	// Get the raw request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.logWebhookError(ctx, "body_read_error", "", requestID, "Failed to read request body", err, headers)
		utils.HandleError(c, utils.NewInternalServerError("Failed to read request body", nil))
		return
	}

	// Validate content type
	contentType := c.GetHeader("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		h.logWebhookError(ctx, "invalid_content_type", "", requestID, "Invalid content type", nil, headers)
		utils.HandleError(c, utils.NewValidationError("Invalid content type", nil))
		return
	}

	// Get the Stripe signature from headers
	signature := c.GetHeader("Stripe-Signature")
	if signature == "" {
		h.logWebhookError(ctx, "missing_signature", "", requestID, "Missing Stripe signature header", nil, headers)
		utils.HandleError(c, utils.NewValidationError("Missing Stripe signature", nil))
		return
	}

	// Resolve webhook secrets from config/env (supports key rotation)
	webhookSecrets := h.resolveStripeWebhookSecrets()
	if len(webhookSecrets) == 0 {
		h.logWebhookError(ctx, "webhook_secret_missing", "", requestID, "Stripe webhook secret is not configured", nil, headers)
		utils.HandleError(c, utils.NewInternalServerError("Webhook secret is not configured", nil))
		return
	}

	// Verify webhook signature with enhanced error handling
	var (
		event          stripe.Event
		signatureError error
	)

	for _, secret := range webhookSecrets {
		event, signatureError = webhook.ConstructEventWithOptions(body, signature, secret, webhook.ConstructEventOptions{
			IgnoreAPIVersionMismatch: true,
		})
		if signatureError == nil {
			break
		}
	}

	if signatureError != nil {
		headers["configured_webhook_secret_count"] = len(webhookSecrets)
		h.logWebhookError(ctx, "signature_verification_failed", "", requestID, "Webhook signature verification failed", signatureError, headers)
		utils.HandleError(c, utils.NewValidationError("Invalid webhook signature", nil))
		return
	}

	// Check for idempotency - prevent duplicate processing
	webhookID := c.GetHeader("X-Webhook-ID")
	if webhookID == "" {
		// Generate idempotency key from event ID if not provided
		webhookID = fmt.Sprintf("stripe-%s", event.ID)
	}

	// Check if webhook was already processed
	var existingEvent models.WebhookEvent
	if err := h.ticketService.GetDB().Where("gateway_event_id = ? AND payment_gateway = ?", event.ID, "stripe").First(&existingEvent).Error; err == nil {
		if existingEvent.Status == "processed" {
			h.logWebhookInfo(ctx, "webhook_duplicate", event.ID, requestID, "Webhook already processed", gin.H{
				"existing_event_id": existingEvent.ID,
				"processed_at":      existingEvent.ProcessedAt,
			})
			utils.SuccessResponse(c, http.StatusOK, "Webhook already processed", gin.H{
				"event_id":   event.ID,
				"event_type": event.Type,
				"duplicate":  true,
			})
			return
		}
	}

	// Store webhook event for audit and potential replay
	webhookEvent := &models.WebhookEvent{
		PaymentGateway: "stripe",
		GatewayEventID: event.ID,
		EventType:      event.Type,
		APIVersion:     event.APIVersion,
		Status:         "processing",
		Payload:        map[string]interface{}{"raw": string(event.Data.Raw)}, // Store as string
		Headers:        headers,
		ReceivedAt:     time.Now(),
	}

	if err := h.ticketService.GetDB().Create(webhookEvent).Error; err != nil {
		h.logWebhookError(ctx, "webhook_storage_failed", event.ID, requestID, "Failed to store webhook event", err, headers)
		utils.HandleError(c, utils.NewInternalServerError("Failed to process webhook", nil))
		return
	}

	// Process the webhook event with comprehensive error handling
	processingStart := time.Now()
	err = h.processStripeEventSecure(ctx, event, webhookEvent.ID, requestID)
	processingDuration := time.Since(processingStart)

	// Update webhook event status
	updateData := map[string]interface{}{
		"processed_at":    time.Now(),
		"processed_count": 1,
	}

	if err != nil {
		updateData["status"] = "failed"
		updateData["last_error"] = err.Error()
		h.logWebhookError(ctx, "webhook_processing_failed", event.ID, requestID, "Webhook processing failed", err, gin.H{
			"processing_duration_ms": processingDuration.Milliseconds(),
			"webhook_event_id":       webhookEvent.ID,
		})
		utils.HandleError(c, utils.NewInternalServerError("Failed to process webhook event", nil))
		return
	}

	updateData["status"] = "processed"
	if err := h.ticketService.GetDB().Model(webhookEvent).Updates(updateData).Error; err != nil {
		log.Printf("Failed to update webhook event status: %v", err)
	}

	// Log successful processing
	h.logWebhookSuccess(ctx, event.Type+"_processed", event.ID, requestID, "Webhook processed successfully", gin.H{
		"processing_duration_ms": processingDuration.Milliseconds(),
		"total_duration_ms":      time.Since(startTime).Milliseconds(),
		"webhook_event_id":       webhookEvent.ID,
	})

	// Log audit trail
	h.logAudit(ctx, "webhook_processed", "webhook_event", webhookEvent.ID, nil, gin.H{
		"event_type":             event.Type,
		"gateway_event_id":       event.ID,
		"processing_duration_ms": processingDuration.Milliseconds(),
	})

	// Return success response
	utils.SuccessResponse(c, http.StatusOK, "Webhook processed successfully", gin.H{
		"event_id":         event.ID,
		"event_type":       event.Type,
		"processed":        true,
		"webhook_event_id": webhookEvent.ID,
	})
}

// processStripeEventSecure processes different types of Stripe webhook events with comprehensive security
func (h *WebhookHandler) processStripeEventSecure(ctx context.Context, event stripe.Event, webhookEventID uuid.UUID, requestID string) error {
	// Validate event type
	if !h.isValidEventType(event.Type) {
		h.logWebhookError(ctx, "invalid_event_type", event.ID, requestID, fmt.Sprintf("Unsupported event type: %s", event.Type), nil, nil)
		return fmt.Errorf("unsupported event type: %s", event.Type)
	}

	// Process based on event type
	switch event.Type {
	case "payment_intent.succeeded":
		return h.handlePaymentIntentSucceededSecure(ctx, event.Data.Object, webhookEventID, requestID)

	case "payment_intent.payment_failed":
		return h.handlePaymentIntentFailedSecure(ctx, event.Data.Object, webhookEventID, requestID)

	case "payment_intent.canceled":
		return h.handlePaymentIntentCanceledSecure(ctx, event.Data.Object, webhookEventID, requestID)

	case "checkout.session.completed":
		return h.handleCheckoutSessionCompletedSecure(ctx, event.Data.Object, webhookEventID, requestID)

	default:
		log.Printf("Unhandled webhook event type: %s", event.Type)
		return nil // Don't return error for unhandled events
	}
}

// isValidEventType validates that the event type is one we handle
func (h *WebhookHandler) isValidEventType(eventType string) bool {
	validTypes := map[string]bool{
		"payment_intent.succeeded":      true,
		"payment_intent.payment_failed": true,
		"payment_intent.canceled":       true,
		"checkout.session.completed":    true,
	}
	return validTypes[eventType]
}

// handlePaymentIntentSucceededSecure handles successful payment events with comprehensive security
func (h *WebhookHandler) handlePaymentIntentSucceededSecure(ctx context.Context, data interface{}, webhookEventID uuid.UUID, requestID string) error {
	paymentIntent, ok := data.(*stripe.PaymentIntent)
	if !ok {
		return fmt.Errorf("invalid payment intent data")
	}

	h.logWebhookInfo(ctx, "payment_succeeded_processing", paymentIntent.ID, requestID, "Processing successful payment", gin.H{
		"amount":           paymentIntent.Amount,
		"currency":         paymentIntent.Currency,
		"webhook_event_id": webhookEventID,
	})

	// Start database transaction
	tx := h.ticketService.GetDB().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			h.logWebhookError(ctx, "payment_processing_panic", paymentIntent.ID, requestID, "Panic during payment processing", fmt.Errorf("%v", r), nil)
		}
	}()

	// Find checkout session using robust lookup
	checkoutSession, err := h.findCheckoutSessionByEvent(tx, "payment_intent.succeeded", data, requestID)
	if err != nil {
		tx.Rollback()
		h.logWebhookError(ctx, "checkout_session_not_found", paymentIntent.ID, requestID, "Checkout session not found", err, gin.H{
			"payment_intent_id": paymentIntent.ID,
			"metadata":          paymentIntent.Metadata,
		})
		return fmt.Errorf("checkout session not found: %w", err)
	}

	// Update checkout session status with transaction
	checkoutSession.Status = "completed"
	checkoutSession.GatewayData = map[string]interface{}{
		"payment_intent_id": paymentIntent.ID,
		"amount_received":   paymentIntent.AmountReceived,
		"currency":          paymentIntent.Currency,
		"payment_method":    paymentIntent.PaymentMethod,
		"processed_at":      time.Now(),
	}

	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		h.logWebhookError(ctx, "checkout_session_update_failed", paymentIntent.ID, requestID, "Failed to update checkout session", err, nil)
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// Create PaymentIntent record for database tracking
	now := time.Now()
	paymentIntentRecord := &models.PaymentIntent{
		ID:                uuid.MustParse(paymentIntent.ID),
		PaymentGateway:    string(models.PaymentGatewayStripe),
		IdempotencyKey:    paymentIntent.ID, // Use payment intent ID as idempotency key
		UserID:            checkoutSession.UserID,
		GuestUserID:       checkoutSession.GuestUserID,
		CustomerEmail:     paymentIntent.ReceiptEmail,
		EventID:           checkoutSession.Ticket.EventID, // Get event ID from ticket
		TierID:            checkoutSession.Ticket.TierID,  // Get tier ID from ticket
		Quantity:          1,                              // Default to 1, could be enhanced
		Currency:          strings.ToUpper(string(paymentIntent.Currency)),
		TotalAmount:       float64(paymentIntent.Amount) / 100, // Convert from cents
		Status:            "succeeded",
		PaymentMethodType: "card",
		PaymentMethodDetails: map[string]interface{}{
			"type": "card",
		},
		GatewayResponse: map[string]interface{}{
			"payment_intent_id": paymentIntent.ID,
			"amount":            paymentIntent.Amount,
			"currency":          paymentIntent.Currency,
			"status":            paymentIntent.Status,
		},
		GatewayMetadata: map[string]interface{}{
			"checkout_token": checkoutSession.CheckoutToken,
			"event_id":       checkoutSession.Ticket.EventID.String(),
		},
		SucceededAt: &now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := tx.Create(paymentIntentRecord).Error; err != nil {
		tx.Rollback()
		h.logWebhookError(ctx, "payment_intent_create_failed", paymentIntent.ID, requestID, "Failed to create payment intent record", err, nil)
		return fmt.Errorf("failed to create payment intent record: %w", err)
	}

	// Log audit for checkout session update
	h.logAudit(ctx, "checkout_session_completed", "checkout_session", checkoutSession.ID, nil, gin.H{
		"payment_intent_id": paymentIntent.ID,
		"checkout_token":    checkoutSession.CheckoutToken,
		"webhook_event_id":  webhookEventID,
	})

	// Process the successful payment (create tickets, send emails, etc.)
	if err := h.ticketService.ProcessSuccessfulPayment(checkoutSession.CheckoutToken); err != nil {
		tx.Rollback()
		h.logWebhookError(ctx, "payment_processing_failed", paymentIntent.ID, requestID, "Failed to process successful payment", err, gin.H{
			"checkout_token": checkoutSession.CheckoutToken,
		})
		return fmt.Errorf("failed to process successful payment: %w", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		h.logWebhookError(ctx, "transaction_commit_failed", paymentIntent.ID, requestID, "Failed to commit transaction", err, nil)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	h.logWebhookSuccess(ctx, "payment_succeeded_completed", paymentIntent.ID, requestID, "Payment processing completed successfully", gin.H{
		"checkout_session_id": checkoutSession.ID,
		"checkout_token":      checkoutSession.CheckoutToken,
	})

	return nil
}

// handlePaymentIntentFailedSecure handles failed payment events with comprehensive security
func (h *WebhookHandler) handlePaymentIntentFailedSecure(ctx context.Context, data interface{}, webhookEventID uuid.UUID, requestID string) error {
	paymentIntent, ok := data.(*stripe.PaymentIntent)
	if !ok {
		return fmt.Errorf("invalid payment intent data")
	}

	h.logWebhookInfo(ctx, "payment_failed_processing", paymentIntent.ID, requestID, "Processing failed payment", gin.H{
		"webhook_event_id": webhookEventID,
	})

	// Start database transaction
	tx := h.ticketService.GetDB().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			h.logWebhookError(ctx, "payment_failure_processing_panic", paymentIntent.ID, requestID, "Panic during payment failure processing", fmt.Errorf("%v", r), nil)
		}
	}()

	// Find the checkout session using metadata from payment intent
	checkoutToken, ok := paymentIntent.Metadata["checkout_token"]
	if !ok || checkoutToken == "" {
		tx.Rollback()
		h.logWebhookError(ctx, "missing_checkout_token", paymentIntent.ID, requestID, "Checkout token not found in payment intent metadata", nil, gin.H{
			"metadata": paymentIntent.Metadata,
		})
		return fmt.Errorf("checkout token not found in payment intent metadata")
	}

	// Find the checkout session by checkout token
	var checkoutSession models.CheckoutSession
	if err := tx.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		h.logWebhookError(ctx, "checkout_session_not_found", paymentIntent.ID, requestID, "Checkout session not found for failed payment", err, gin.H{
			"checkout_token":    checkoutToken,
			"payment_intent_id": paymentIntent.ID,
		})
		return fmt.Errorf("checkout session not found: %w", err)
	}

	// Update checkout session status with failure details
	checkoutSession.Status = "failed"
	checkoutSession.GatewayData = map[string]interface{}{
		"payment_intent_id": paymentIntent.ID,
		"failure_reason":    "payment_failed",
		"failure_code":      h.extractFailureCode(paymentIntent.LastPaymentError),
		"failure_message":   h.extractFailureMessage(paymentIntent.LastPaymentError),
		"failed_at":         time.Now(),
	}

	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		h.logWebhookError(ctx, "checkout_session_update_failed", paymentIntent.ID, requestID, "Failed to update checkout session for failure", err, nil)
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// Log audit for checkout session failure
	h.logAudit(ctx, "checkout_session_failed", "checkout_session", checkoutSession.ID, nil, gin.H{
		"payment_intent_id": paymentIntent.ID,
		"checkout_token":    checkoutSession.CheckoutToken,
		"failure_reason":    "payment_failed",
		"webhook_event_id":  webhookEventID,
	})

	// Process the failed payment (release reserved tickets, send failure email, etc.)
	if err := h.ticketService.ProcessFailedPayment(checkoutSession.CheckoutToken); err != nil {
		tx.Rollback()
		h.logWebhookError(ctx, "payment_failure_processing_failed", paymentIntent.ID, requestID, "Failed to process payment failure", err, gin.H{
			"checkout_token": checkoutSession.CheckoutToken,
		})
		return fmt.Errorf("failed to process failed payment: %w", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		h.logWebhookError(ctx, "transaction_commit_failed", paymentIntent.ID, requestID, "Failed to commit failure transaction", err, nil)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	h.logWebhookSuccess(ctx, "payment_failed_completed", paymentIntent.ID, requestID, "Payment failure processing completed", gin.H{
		"checkout_session_id": checkoutSession.ID,
		"checkout_token":      checkoutSession.CheckoutToken,
	})

	return nil
}

// handlePaymentIntentCanceledSecure handles canceled payment events with comprehensive security
func (h *WebhookHandler) handlePaymentIntentCanceledSecure(ctx context.Context, data interface{}, webhookEventID uuid.UUID, requestID string) error {
	paymentIntent, ok := data.(*stripe.PaymentIntent)
	if !ok {
		return fmt.Errorf("invalid payment intent data")
	}

	h.logWebhookInfo(ctx, "payment_canceled_processing", paymentIntent.ID, requestID, "Processing canceled payment", gin.H{
		"webhook_event_id": webhookEventID,
	})

	// Start database transaction
	tx := h.ticketService.GetDB().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			h.logWebhookError(ctx, "payment_cancel_processing_panic", paymentIntent.ID, requestID, "Panic during payment cancel processing", fmt.Errorf("%v", r), nil)
		}
	}()

	// Find the checkout session using metadata from payment intent
	checkoutToken, ok := paymentIntent.Metadata["checkout_token"]
	if !ok || checkoutToken == "" {
		tx.Rollback()
		h.logWebhookError(ctx, "missing_checkout_token", paymentIntent.ID, requestID, "Checkout token not found in payment intent metadata", nil, gin.H{
			"metadata": paymentIntent.Metadata,
		})
		return fmt.Errorf("checkout token not found in payment intent metadata")
	}

	// Find the checkout session by checkout token
	var checkoutSession models.CheckoutSession
	if err := tx.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		h.logWebhookError(ctx, "checkout_session_not_found", paymentIntent.ID, requestID, "Checkout session not found for canceled payment", err, gin.H{
			"checkout_token":    checkoutToken,
			"payment_intent_id": paymentIntent.ID,
		})
		return fmt.Errorf("checkout session not found: %w", err)
	}

	// Update checkout session status
	checkoutSession.Status = "canceled"
	checkoutSession.GatewayData = map[string]interface{}{
		"payment_intent_id":   paymentIntent.ID,
		"cancellation_reason": "user_canceled",
		"canceled_at":         time.Now(),
	}

	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		h.logWebhookError(ctx, "checkout_session_update_failed", paymentIntent.ID, requestID, "Failed to update checkout session for cancellation", err, nil)
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// Log audit for checkout session cancellation
	h.logAudit(ctx, "checkout_session_canceled", "checkout_session", checkoutSession.ID, nil, gin.H{
		"payment_intent_id":   paymentIntent.ID,
		"checkout_token":      checkoutSession.CheckoutToken,
		"cancellation_reason": "user_canceled",
		"webhook_event_id":    webhookEventID,
	})

	// Process the canceled payment (release reserved tickets)
	if err := h.ticketService.ProcessCanceledPayment(checkoutSession.CheckoutToken); err != nil {
		tx.Rollback()
		h.logWebhookError(ctx, "payment_cancel_processing_failed", paymentIntent.ID, requestID, "Failed to process payment cancellation", err, gin.H{
			"checkout_token": checkoutSession.CheckoutToken,
		})
		return fmt.Errorf("failed to process canceled payment: %w", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		h.logWebhookError(ctx, "transaction_commit_failed", paymentIntent.ID, requestID, "Failed to commit cancellation transaction", err, nil)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	h.logWebhookSuccess(ctx, "payment_canceled_completed", paymentIntent.ID, requestID, "Payment cancellation processing completed", gin.H{
		"checkout_session_id": checkoutSession.ID,
		"checkout_token":      checkoutSession.CheckoutToken,
	})

	return nil
}

// handleCheckoutSessionCompletedSecure handles completed checkout session events with comprehensive security
func (h *WebhookHandler) handleCheckoutSessionCompletedSecure(ctx context.Context, data interface{}, webhookEventID uuid.UUID, requestID string) error {
	checkoutSession, ok := data.(*stripe.CheckoutSession)
	if !ok {
		return fmt.Errorf("invalid checkout session data")
	}

	h.logWebhookInfo(ctx, "checkout_session_processing", checkoutSession.ID, requestID, "Processing completed checkout session", gin.H{
		"payment_status":   checkoutSession.PaymentStatus,
		"webhook_event_id": webhookEventID,
	})

	// Start database transaction
	tx := h.ticketService.GetDB().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			h.logWebhookError(ctx, "checkout_session_processing_panic", checkoutSession.ID, requestID, "Panic during checkout session processing", fmt.Errorf("%v", r), nil)
		}
	}()

	// Find our checkout session by Stripe checkout session ID
	var dbCheckoutSession models.CheckoutSession
	if err := tx.Where("stripe_session_id = ?", checkoutSession.ID).First(&dbCheckoutSession).Error; err != nil {
		tx.Rollback()
		h.logWebhookError(ctx, "checkout_session_not_found", checkoutSession.ID, requestID, "Database checkout session not found", err, gin.H{
			"stripe_session_id": checkoutSession.ID,
		})
		return fmt.Errorf("checkout session not found: %w", err)
	}

	// Update checkout session with additional data
	dbCheckoutSession.GatewayData = map[string]interface{}{
		"stripe_session_id": checkoutSession.ID,
		"payment_status":    checkoutSession.PaymentStatus,
		"customer_email":    checkoutSession.CustomerEmail,
		"amount_total":      checkoutSession.AmountTotal,
		"currency":          checkoutSession.Currency,
		"processed_at":      time.Now(),
	}

	if err := tx.Save(&dbCheckoutSession).Error; err != nil {
		tx.Rollback()
		h.logWebhookError(ctx, "checkout_session_update_failed", checkoutSession.ID, requestID, "Failed to update checkout session", err, nil)
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// Log audit for checkout session update
	h.logAudit(ctx, "checkout_session_completed", "checkout_session", dbCheckoutSession.ID, nil, gin.H{
		"stripe_session_id": checkoutSession.ID,
		"payment_status":    checkoutSession.PaymentStatus,
		"webhook_event_id":  webhookEventID,
	})

	// If payment was successful, process the payment
	if checkoutSession.PaymentStatus == "paid" {
		if err := h.ticketService.ProcessSuccessfulPayment(dbCheckoutSession.CheckoutToken); err != nil {
			tx.Rollback()
			h.logWebhookError(ctx, "checkout_payment_processing_failed", checkoutSession.ID, requestID, "Failed to process successful checkout payment", err, gin.H{
				"checkout_token": dbCheckoutSession.CheckoutToken,
			})
			return fmt.Errorf("failed to process successful payment: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		h.logWebhookError(ctx, "transaction_commit_failed", checkoutSession.ID, requestID, "Failed to commit checkout session transaction", err, nil)
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	h.logWebhookSuccess(ctx, "checkout_session_completed", checkoutSession.ID, requestID, "Checkout session processing completed", gin.H{
		"checkout_session_id": dbCheckoutSession.ID,
		"payment_status":      checkoutSession.PaymentStatus,
	})

	return nil
}

// Helper methods for secure webhook processing

// extractFailureCode safely extracts failure code from Stripe error
func (h *WebhookHandler) extractFailureCode(error *stripe.Error) string {
	if error == nil {
		return "unknown"
	}
	return string(error.Code)
}

// extractFailureMessage safely extracts failure message from Stripe error
func (h *WebhookHandler) extractFailureMessage(error *stripe.Error) string {
	if error == nil {
		return "Unknown payment failure"
	}
	if error.Msg != "" {
		return error.Msg
	}
	return "Payment failed"
}

func (h *WebhookHandler) resolveStripeWebhookSecrets() []string {
	uniqueSecrets := make(map[string]struct{})
	resolved := make([]string, 0)

	appendSecrets := func(value string) {
		if value == "" {
			return
		}

		for _, part := range strings.Split(value, ",") {
			secret := strings.Trim(strings.TrimSpace(part), "\"'")
			if secret == "" {
				continue
			}
			if _, exists := uniqueSecrets[secret]; exists {
				continue
			}
			uniqueSecrets[secret] = struct{}{}
			resolved = append(resolved, secret)
		}
	}

	if h.config != nil {
		appendSecrets(h.config.Payment.Gateways.StripeWebhookSecret)
	}

	appendSecrets(os.Getenv("STRIPE_WEBHOOK_SECRETS"))
	appendSecrets(os.Getenv("STRIPE_WEBHOOK_SECRET"))
	appendSecrets(os.Getenv("STRIPE_WEBHOOK_SIGNING_SECRET"))
	appendSecrets(os.Getenv("STRIPE_WEBHOOK_SECRET_KEY"))
	appendSecrets(os.Getenv("STRIPE_SIGNING_SECRET"))

	return resolved
}

// logWebhookError logs webhook processing errors with comprehensive context
func (h *WebhookHandler) logWebhookError(ctx context.Context, action, entityID, requestID, message string, err error, metadata map[string]interface{}) {
	logData := gin.H{
		"level":      "error",
		"action":     action,
		"entity_id":  entityID,
		"request_id": requestID,
		"message":    message,
		"timestamp":  time.Now(),
		"service":    "webhook_handler",
	}

	if err != nil {
		logData["error"] = err.Error()
		logData["error_type"] = fmt.Sprintf("%T", err)
	}

	if metadata != nil {
		logData["metadata"] = metadata
	}

	// Create hash for error deduplication
	errorHash := h.generateErrorHash(action, entityID, message)
	logData["error_hash"] = errorHash

	log.Printf("[WEBHOOK_ERROR] %s: %s (Entity: %s, Request: %s)", action, message, entityID, requestID)
	if err != nil {
		log.Printf("[WEBHOOK_ERROR_DETAIL] Error: %v", err)
	}
}

// logWebhookInfo logs webhook processing info with context
func (h *WebhookHandler) logWebhookInfo(ctx context.Context, action, entityID, requestID, message string, metadata map[string]interface{}) {
	logData := gin.H{
		"level":      "info",
		"action":     action,
		"entity_id":  entityID,
		"request_id": requestID,
		"message":    message,
		"timestamp":  time.Now(),
		"service":    "webhook_handler",
	}

	if metadata != nil {
		logData["metadata"] = metadata
	}

	log.Printf("[WEBHOOK_INFO] %s: %s (Entity: %s, Request: %s)", action, message, entityID, requestID)
}

// logWebhookSuccess logs successful webhook processing
func (h *WebhookHandler) logWebhookSuccess(ctx context.Context, action, entityID, requestID, message string, metadata map[string]interface{}) {
	logData := gin.H{
		"level":      "success",
		"action":     action,
		"entity_id":  entityID,
		"request_id": requestID,
		"message":    message,
		"timestamp":  time.Now(),
		"service":    "webhook_handler",
	}

	if metadata != nil {
		logData["metadata"] = metadata
	}

	log.Printf("[WEBHOOK_SUCCESS] %s: %s (Entity: %s, Request: %s)", action, message, entityID, requestID)
}

// logAudit creates comprehensive audit log entries for webhook events
func (h *WebhookHandler) logAudit(ctx context.Context, action, entityType string, entityID uuid.UUID, actorID *uuid.UUID, changes map[string]interface{}) {
	audit := &models.PaymentAuditLog{
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		ActorID:    actorID,
		ActorType:  "webhook",
		Timestamp:  time.Now(),
		Metadata:   changes,
	}

	// Add request context if available
	if requestID, ok := ctx.Value("request_id").(string); ok {
		if audit.Metadata == nil {
			audit.Metadata = make(map[string]interface{})
		}
		audit.Metadata["request_id"] = requestID
	}

	if err := h.ticketService.GetDB().Create(audit).Error; err != nil {
		log.Printf("Failed to create audit log: %v", err)
	}
}

// generateErrorHash creates a hash for error deduplication
func (h *WebhookHandler) generateErrorHash(action, entityID, message string) string {
	hashInput := fmt.Sprintf("%s:%s:%s", action, entityID, message)
	hash := sha256.Sum256([]byte(hashInput))
	return fmt.Sprintf("%x", hash)[:16] // First 16 characters of hash
}
