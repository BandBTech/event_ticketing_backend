package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"event-ticketing-backend/internal/gateways"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/workers"
)

// WebhookHandler is the production-grade webhook handler with proper idempotency
// and job enqueuing for safe async processing under peak load.
//
// CRITICAL ARCHITECTURE: Does NOT do complex work inline.
// Instead, enqueues jobs to PaymentWorker for processing with proper retries.
type WebhookHandler struct {
	db            *gorm.DB
	paymentWorker interface{} // PaymentWorker - using interface to avoid circular imports
	stripeGateway *gateways.StripeGateway
}

// NewWebhookHandler creates a new production webhook handler that enqueues jobs
// instead of doing complex work inline (which would make retries unsafe)
func NewWebhookHandler(db *gorm.DB, paymentWorker interface{}, stripeGateway *gateways.StripeGateway) *WebhookHandler {
	return &WebhookHandler{
		db:            db,
		paymentWorker: paymentWorker,
		stripeGateway: stripeGateway,
	}
}

// HandleStripeWebhook handles Stripe webhooks with comprehensive failure tracking
// CRITICAL: Always returns 200 to Stripe to prevent retry loops
// BUT: Records ALL failures to database for internal debugging
//
// IMPORTANT: Function never returns HTTP error codes (except rare cases).
// Instead, records failures in webhook_events table with detailed error messages.
// This allows Stripe to stop retrying while we track what went wrong internally.
//
// godoc
// @Summary Handle Stripe webhook events
// @Description Receives and processes Stripe webhook events with full failure tracking
// @Tags Webhooks
// @Accept json
// @Produce json
// @Success 200 {object} utils.Response "Webhook processed or logged"
// @Failure 400 {object} utils.Response "Invalid payload or signature (rare - only for critical errors)"
// @Router /api/v1/webhooks/stripe [post]
func (h *WebhookHandler) HandleStripeWebhook(c *gin.Context) {
	requestID := c.GetString("request-id") // Added by middleware
	if requestID == "" {
		requestID = uuid.New().String()
	}

	// Step 1: Read webhook payload
	// This is the only step that returns 400 - malformed HTTP is not a Stripe issue
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("[WEBHOOK] [%s] ❌ Failed to read payload: %v", requestID, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	// Step 2: SECURITY - Verify Stripe signature
	// If signature fails, this is a security issue - return 400
	sig := c.GetHeader("Stripe-Signature")
	if sig == "" {
		log.Printf("[WEBHOOK] [%s] ❌ Missing Stripe signature", requestID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing signature"})
		return
	}

	webhookEvent, err := h.stripeGateway.VerifyWebhook(c.Request.Context(), payload, sig)
	if err != nil {
		log.Printf("[WEBHOOK] [%s] ❌ Signature verification failed: %v", requestID, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid signature"})
		return
	}

	log.Printf("[WEBHOOK] [%s] ✓ Signature verified | event_id=%s | type=%s", requestID, webhookEvent.EventID, webhookEvent.Type)

	// Step 3: Create WebhookEvent record in database EARLY (for tracking)
	// This is the source of truth for what happened to this event
	webhookEventRecord := &models.WebhookEvent{
		PaymentGateway: "stripe",
		GatewayEventID: webhookEvent.EventID, // UNIQUE constraint prevents duplicates
		EventType:      webhookEvent.Type,
		Status:         "pending",
		Payload:        webhookEvent.Data,
		Headers: map[string]interface{}{
			"request-id": requestID,
			"timestamp":  time.Now().Unix(),
		},
	}

	// Record in database - if duplicate, it's OK (idempotent)
	if err := h.db.Create(webhookEventRecord).Error; err != nil {
		// Don't fail on duplicate - just log and continue
		if !isForeignKeyError(err) && !isUniqueConstraintError(err) {
			log.Printf("[WEBHOOK] [%s] ⚠️  Failed to record webhook event: %v", requestID, err)
			// Still continue - we'll track failure below
		}
	} else {
		log.Printf("[WEBHOOK] [%s] ✓ Recorded webhook event: id=%s", requestID, webhookEventRecord.ID)
	}

	// Step 4: Check event type - process payment_intent.succeeded, checkout.session.completed, payment_intent.payment_failed, and charge.refunded
	if webhookEvent.Type != "payment_intent.succeeded" &&
		webhookEvent.Type != "checkout.session.completed" &&
		webhookEvent.Type != "payment_intent.payment_failed" &&
		webhookEvent.Type != "charge.refunded" {
		log.Printf("[WEBHOOK] [%s] ℹ️  Ignoring event type: %s (not in critical webhook list)", requestID, webhookEvent.Type)

		// Update status in database
		h.updateWebhookEventStatus(requestID, webhookEvent.EventID, "ignored", "Event type not processed")

		// Return 200 to Stripe (we've acknowledged it)
		c.JSON(http.StatusOK, gin.H{"status": "ignored"})
		return
	}

	log.Printf("[WEBHOOK] [%s] ✓ Processing: %s", requestID, webhookEvent.Type)

	// Step 5: Extract event-specific data and validate
	var paymentIntentID string

	// For charge.refunded events, we don't strictly need payment_intent_id upfront
	// The worker will extract it from the charge data
	if webhookEvent.Type != "charge.refunded" {
		paymentIntentID = webhookEvent.PaymentIntentID
		if paymentIntentID == "" {
			log.Printf("[WEBHOOK] [%s] ❌ CRITICAL: Missing payment_intent_id in webhook for %s", requestID, webhookEvent.Type)

			// Record the failure
			h.updateWebhookEventStatus(requestID, webhookEvent.EventID, "failed", "Missing payment_intent_id")

			// Return 200 to Stripe anyway - this is not Stripe's fault
			c.JSON(http.StatusOK, gin.H{"status": "processed"})
			return
		}
		log.Printf("[WEBHOOK] [%s] ✓ Payment intent ID extracted: %s", requestID, paymentIntentID)
	}

	// Step 6: Validate PaymentWorker type
	paymentWorker, ok := h.paymentWorker.(*workers.PaymentWorker)
	if !ok {
		log.Printf("[WEBHOOK] [%s] ❌ CRITICAL: Invalid payment worker type", requestID)

		h.updateWebhookEventStatus(requestID, webhookEvent.EventID, "failed", "Invalid payment worker type")

		c.JSON(http.StatusOK, gin.H{"status": "processed"})
		return
	}

	log.Printf("[WEBHOOK] [%s] ✓ Payment worker validated", requestID)

	// Step 7: Marshal webhook data for job payload
	rawData, err := json.Marshal(webhookEvent.Data)
	if err != nil {
		log.Printf("[WEBHOOK] [%s] ❌ Failed to marshal webhook data: %v", requestID, err)

		h.updateWebhookEventStatus(requestID, webhookEvent.EventID, "failed", fmt.Sprintf("Marshal error: %v", err))

		c.JSON(http.StatusOK, gin.H{"status": "processed"})
		return
	}

	log.Printf("[WEBHOOK] [%s] ✓ Webhook data marshaled", requestID)

	// Step 8: Build job payload
	eventData := map[string]interface{}{
		"stripe_event_id":  webhookEvent.EventID,
		"event_type":       webhookEvent.Type,
		"webhook_event_id": webhookEventRecord.ID,
		"request_id":       requestID,
		"event_data":       webhookEvent.Data,
	}

	if paymentIntentID != "" {
		eventData["payment_intent_id"] = paymentIntentID
	}

	taskPayload := &workers.PaymentTaskPayload{
		StripeEventID:  webhookEvent.EventID,
		EventType:      webhookEvent.Type,
		WebhookEventID: webhookEventRecord.ID,
		RequestID:      requestID,
		EventData:      eventData,
		RawData:        rawData,
	}

	log.Printf("[WEBHOOK] [%s] ✓ Job payload constructed", requestID)

	// Step 9: ENQUEUE JOB - Route based on event type
	var taskID string
	var enqueueErr error

	switch webhookEvent.Type {
	case "payment_intent.succeeded", "checkout.session.completed":
		taskID, enqueueErr = paymentWorker.EnqueuePaymentSuccess(c.Request.Context(), taskPayload)

	case "payment_intent.payment_failed":
		taskID, enqueueErr = paymentWorker.EnqueuePaymentFailed(c.Request.Context(), taskPayload)

	case "charge.refunded":
		taskID, enqueueErr = paymentWorker.EnqueueChargeRefunded(c.Request.Context(), taskPayload)

	default:
		enqueueErr = fmt.Errorf("unhandled event type: %s", webhookEvent.Type)
	}

	if enqueueErr != nil {
		log.Printf("[WEBHOOK] [%s] ❌ CRITICAL: Failed to enqueue job: %v", requestID, enqueueErr)

		// This is a system failure - record it
		h.updateWebhookEventStatus(requestID, webhookEvent.EventID, "failed", fmt.Sprintf("Enqueue error: %v", enqueueErr))

		// Return 200 to Stripe - this is not their problem, but we need to fix our queue
		c.JSON(http.StatusOK, gin.H{"status": "processed", "error": "job_enqueue_failed"})
		return
	}

	log.Printf("[WEBHOOK] [%s] ✓ Job enqueued successfully | task_id=%s | event_type=%s", requestID, taskID, webhookEvent.Type)

	// Step 10: Update status to queued
	h.updateWebhookEventStatus(requestID, webhookEvent.EventID, "queued", "")

	// Return 200 to Stripe - everything is queued for async processing
	c.JSON(http.StatusOK, gin.H{"status": "success", "task_id": taskID})
}

// updateWebhookEventStatus updates a webhook event's processing status in the database
// This tracks what happened to each webhook for debugging and audit trails
func (h *WebhookHandler) updateWebhookEventStatus(requestID, gatewayEventID, status, errMsg string) {
	// Status values: pending, queued, succeeded, failed, ignored
	update := h.db.Model(&models.WebhookEvent{}).
		Where("gateway_event_id = ?", gatewayEventID).
		Updates(map[string]interface{}{
			"status":     status,
			"last_error": errMsg,
		})

	if update.Error != nil {
		log.Printf("[WEBHOOK] [%s] ⚠️  Failed to update webhook event status: %v", requestID, update.Error)
		return
	}

	logMsg := fmt.Sprintf("[WEBHOOK] [%s] Updated webhook status: %s", requestID, status)
	if errMsg != "" {
		logMsg += fmt.Sprintf(" | error: %s", errMsg)
	}
	log.Printf("%s", logMsg)
}

// isForeignKeyError checks if error is a foreign key constraint error
func isForeignKeyError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "foreign key") ||
		strings.Contains(errStr, "FOREIGN KEY") ||
		strings.Contains(errStr, "23503") // PostgreSQL FK error code
}

// isUniqueConstraintError checks if error is a unique constraint error
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "unique") ||
		strings.Contains(errStr, "UNIQUE") ||
		strings.Contains(errStr, "23505") // PostgreSQL unique error code
}
