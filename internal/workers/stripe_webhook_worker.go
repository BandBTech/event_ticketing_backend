package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stripe/stripe-go/v74"
	"gorm.io/gorm"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/redis"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
)

// StripeWebhookJob represents a job to be processed by the webhook worker
type StripeWebhookJob struct {
	WebhookEventID uuid.UUID              `json:"webhook_event_id"`
	EventID        string                 `json:"event_id"`
	EventType      string                 `json:"event_type"`
	EventData      string                 `json:"event_data"`
	APIVersion     string                 `json:"api_version"`
	Headers        map[string]interface{} `json:"headers"`
	ReceivedAt     time.Time              `json:"received_at"`
	RetryCount     int                    `json:"retry_count"`
	Status         string                 `json:"status"`
}

// StripeWebhookWorker processes Stripe webhook events from Redis queue
type StripeWebhookWorker struct {
	ticketService *services.TicketService
	config        *config.Config
	redisClient   *goredis.Client
	workerID      string
	stopChan      chan struct{}
}

// NewStripeWebhookWorker creates a new Stripe webhook worker
func NewStripeWebhookWorker(ticketService *services.TicketService, config *config.Config) *StripeWebhookWorker {
	if ticketService == nil {
		log.Fatal("[WEBHOOK_WORKER] TicketService cannot be nil")
	}
	if config == nil {
		log.Fatal("[WEBHOOK_WORKER] Config cannot be nil")
	}
	if redis.Client == nil {
		log.Fatal("[WEBHOOK_WORKER] Redis client not available")
	}

	workerID := fmt.Sprintf("stripe-webhook-worker-%s", uuid.New().String()[:8])

	return &StripeWebhookWorker{
		ticketService: ticketService,
		config:        config,
		redisClient:   redis.Client,
		workerID:      workerID,
		stopChan:      make(chan struct{}),
	}
}

// Start begins processing webhook events from the Redis queue
func (w *StripeWebhookWorker) Start() {
	if w == nil {
		log.Printf("[WEBHOOK_WORKER] Worker is nil")
		return
	}
	if w.redisClient == nil {
		log.Printf("[WEBHOOK_WORKER] Redis client is nil")
		return
	}

	log.Printf("[WEBHOOK_WORKER] Starting Stripe webhook worker: %s", w.workerID)

	// Test Redis connection
	if err := w.redisClient.Ping(context.Background()).Err(); err != nil {
		log.Printf("[WEBHOOK_WORKER] Redis connection failed: %v", err)
		return
	}
	log.Printf("[WEBHOOK_WORKER] Redis connection successful")

	// Test database connection
	if w.ticketService.GetDB() == nil {
		log.Printf("[WEBHOOK_WORKER] Database connection is nil")
		return
	}

	sqlDB, err := w.ticketService.GetDB().DB()
	if err != nil {
		log.Printf("[WEBHOOK_WORKER] Failed to get underlying SQL DB: %v", err)
		return
	}

	if err := sqlDB.Ping(); err != nil {
		log.Printf("[WEBHOOK_WORKER] Database ping failed: %v", err)
		return
	}
	log.Printf("[WEBHOOK_WORKER] Database connection successful")

	go w.processQueue()
}

// Stop gracefully stops the worker
func (w *StripeWebhookWorker) Stop() {
	log.Printf("[WEBHOOK_WORKER] Stopping Stripe webhook worker: %s", w.workerID)
	close(w.stopChan)
}

// processQueue continuously processes events from the Redis queue
func (w *StripeWebhookWorker) processQueue() {
	ctx := context.Background()
	queueName := "stripe_webhook_queue"

	for {
		select {
		case <-w.stopChan:
			log.Printf("[WEBHOOK_WORKER] Worker %s stopped", w.workerID)
			return
		default:
			// Use BLPOP to wait for jobs with timeout
			result := w.redisClient.BLPop(ctx, 5*time.Second, queueName)
			if result.Err() != nil {
				if result.Err() != goredis.Nil {
					log.Printf("[WEBHOOK_WORKER] Redis error: %v", result.Err())
				}
				continue
			}

			jobData := result.Val()[1] // BLPOP returns [queue_name, value]

			// Process the job with panic recovery
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[WEBHOOK_WORKER] PANIC in job processing: %v", r)
						// Re-queue the job for retry
						if err := w.redisClient.RPush(ctx, queueName, jobData).Err(); err != nil {
							log.Printf("[WEBHOOK_WORKER] Failed to re-queue job after panic: %v", err)
						}
					}
				}()

				if err := w.processJob(ctx, jobData); err != nil {
					log.Printf("[WEBHOOK_WORKER] Failed to process job: %v", err)
				}
			}()
		}
	}
}

// processJob processes a single webhook job
func (w *StripeWebhookWorker) processJob(ctx context.Context, jobData string) error {
	log.Printf("[WEBHOOK_WORKER] Received job data: %s", jobData)

	// Parse the job
	var job StripeWebhookJob
	if err := json.Unmarshal([]byte(jobData), &job); err != nil {
		log.Printf("[WEBHOOK_WORKER] Failed to unmarshal job data: %v", err)
		return fmt.Errorf("failed to unmarshal job: %w", err)
	}

	log.Printf("[WEBHOOK_WORKER] Successfully parsed job: EventID=%s, EventType=%s", job.EventID, job.EventType)

	requestID := fmt.Sprintf("worker-%s-%s", w.workerID, uuid.New().String()[:8])
	startTime := time.Now()

	// Add nil checks
	if w.ticketService == nil {
		log.Printf("[WEBHOOK_WORKER] ERROR: ticketService is nil")
		return fmt.Errorf("ticketService is nil")
	}
	if w.ticketService.GetDB() == nil {
		log.Printf("[WEBHOOK_WORKER] ERROR: database connection is nil")
		return fmt.Errorf("database connection is nil")
	}

	log.Printf("[WEBHOOK_WORKER] Processing event %s (type: %s, retry: %d)", job.EventID, job.EventType, job.RetryCount)

	// Check if event was already processed (idempotency)
	var webhookEvent models.WebhookEvent
	if err := w.ticketService.GetDB().Where("id = ?", job.WebhookEventID).First(&webhookEvent).Error; err != nil {
		log.Printf("[WEBHOOK_WORKER] Webhook event not found: %v", err)
		return fmt.Errorf("webhook event not found: %w", err)
	}

	log.Printf("[WEBHOOK_WORKER] Found webhook event with status: %s, ID: %s", webhookEvent.Status, webhookEvent.ID)

	if webhookEvent.Status == "processed" {
		log.Printf("[WEBHOOK_WORKER] Event %s already processed, skipping", job.EventID)
		return nil
	}

	// Parse the Stripe event from the stored data
	event, parsedData, err := w.parseStripeEvent(job)
	if err != nil {
		log.Printf("[WEBHOOK_WORKER] Failed to parse Stripe event: %v", err)
		return w.handleJobError(ctx, job, webhookEvent, err, requestID)
	}

	// Process the event with error handling
	err = w.processStripeEventSecure(ctx, event, parsedData, webhookEvent.ID, requestID)
	if err != nil {
		log.Printf("[WEBHOOK_WORKER] Failed to process Stripe event: %v", err)
		return w.handleJobError(ctx, job, webhookEvent, err, requestID)
	}

	// Mark as processed
	webhookEvent.Status = "processed"
	webhookEvent.ProcessedAt = &startTime
	webhookEvent.ProcessedCount++

	if err := w.ticketService.GetDB().Save(&webhookEvent).Error; err != nil {
		log.Printf("[WEBHOOK_WORKER] Failed to update webhook event status: %v", err)
		// Don't return error here as the processing was successful
	}

	processingDuration := time.Since(startTime)
	log.Printf("[WEBHOOK_WORKER] Successfully processed event %s in %v", job.EventID, processingDuration)

	return nil
}

// parseStripeEvent parses the Stripe event from job data
func (w *StripeWebhookWorker) parseStripeEvent(job StripeWebhookJob) (stripe.Event, interface{}, error) {
	var event stripe.Event
	var parsedData interface{}

	// Parse the event data
	switch job.EventType {
	case "payment_intent.succeeded", "payment_intent.payment_failed", "payment_intent.canceled":
		var paymentIntent stripe.PaymentIntent
		if err := json.Unmarshal([]byte(job.EventData), &paymentIntent); err != nil {
			return event, nil, fmt.Errorf("failed to unmarshal payment intent: %w", err)
		}
		parsedData = &paymentIntent

	case "checkout.session.completed":
		var checkoutSession stripe.CheckoutSession
		if err := json.Unmarshal([]byte(job.EventData), &checkoutSession); err != nil {
			return event, nil, fmt.Errorf("failed to unmarshal checkout session: %w", err)
		}
		parsedData = &checkoutSession

	default:
		return event, nil, fmt.Errorf("unsupported event type: %s", job.EventType)
	}

	event.ID = job.EventID
	event.Type = job.EventType
	event.APIVersion = job.APIVersion
	event.Data.Raw = json.RawMessage(job.EventData)

	return event, parsedData, nil
}

// handleJobError handles job processing errors with retry logic
func (w *StripeWebhookWorker) handleJobError(ctx context.Context, job StripeWebhookJob, webhookEvent models.WebhookEvent, err error, requestID string) error {
	job.RetryCount++

	// Log detailed error for debugging
	log.Printf("[WEBHOOK_WORKER] 🔥 ERROR processing event %s (attempt %d): %+v", job.EventID, job.RetryCount, err)

	// Update webhook event with error
	webhookEvent.LastError = err.Error()
	webhookEvent.ProcessedCount++

	if job.RetryCount >= 5 {
		// Max retries reached, mark as failed
		webhookEvent.Status = "failed"
		log.Printf("[WEBHOOK_WORKER] Event %s failed permanently after %d retries: %v", job.EventID, job.RetryCount, err)
	} else {
		// Re-queue with delay
		webhookEvent.Status = "retry_pending"
		delay := time.Duration(job.RetryCount) * 30 * time.Second // Exponential backoff

		job.Status = "retry_pending"
		jobData, _ := json.Marshal(job)

		go func() {
			time.Sleep(delay)
			if err := w.redisClient.RPush(ctx, "stripe_webhook_queue", jobData).Err(); err != nil {
				log.Printf("[WEBHOOK_WORKER] Failed to re-queue job: %v", err)
			} else {
				log.Printf("[WEBHOOK_WORKER] Re-queued event %s for retry %d", job.EventID, job.RetryCount)
			}
		}()
	}

	if updateErr := w.ticketService.GetDB().Save(&webhookEvent).Error; updateErr != nil {
		log.Printf("[WEBHOOK_WORKER] Failed to update webhook event: %v", updateErr)
	}

	return err
}

// processStripeEventSecure processes different types of Stripe webhook events with comprehensive security
func (w *StripeWebhookWorker) processStripeEventSecure(ctx context.Context, event stripe.Event, parsedData interface{}, webhookEventID uuid.UUID, requestID string) error {
	// Validate event type
	if !w.isValidEventType(event.Type) {
		log.Printf("[WEBHOOK_WORKER] Ignoring unsupported event type: %s", event.Type)
		return nil
	}

	// Process based on event type
	switch event.Type {
	case "payment_intent.succeeded":
		return w.handlePaymentIntentSucceededSecure(ctx, parsedData, webhookEventID, requestID)

	case "payment_intent.payment_failed":
		return w.handlePaymentIntentFailedSecure(ctx, parsedData, webhookEventID, requestID)

	case "payment_intent.canceled":
		return w.handlePaymentIntentCanceledSecure(ctx, parsedData, webhookEventID, requestID)

	case "checkout.session.completed":
		return w.handleCheckoutSessionCompletedSecure(ctx, parsedData, webhookEventID, requestID)

	default:
		log.Printf("[WEBHOOK_WORKER] Unhandled webhook event type: %s", event.Type)
		return nil // Don't return error for unhandled events
	}
}

// isValidEventType validates that the event type is one we handle
func (w *StripeWebhookWorker) isValidEventType(eventType string) bool {
	validTypes := map[string]bool{
		"payment_intent.succeeded":      true,
		"payment_intent.payment_failed": true,
		"payment_intent.canceled":       true,
		"checkout.session.completed":    true,
	}
	return validTypes[eventType]
}

// handlePaymentIntentSucceededSecure handles successful payment events
func (w *StripeWebhookWorker) handlePaymentIntentSucceededSecure(ctx context.Context, data interface{}, webhookEventID uuid.UUID, requestID string) error {
	paymentIntent, ok := data.(*stripe.PaymentIntent)
	if !ok {
		return fmt.Errorf("invalid payment intent data")
	}

	log.Printf("[WEBHOOK_WORKER] Processing successful payment: %s", paymentIntent.ID)

	// Start database transaction
	tx := w.ticketService.GetDB().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("[WEBHOOK_WORKER] Panic during payment processing: %v", r)
		}
	}()

	// Find checkout session using robust lookup
	checkoutSession, err := w.findCheckoutSessionByEvent(tx, "payment_intent.succeeded", data, requestID)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("checkout session not found: %w", err)
	}

	// Update checkout session with payment details
	updates := map[string]interface{}{
		"payment_intent_id": paymentIntent.ID,
		"amount_received":   paymentIntent.AmountReceived,
		"currency":          paymentIntent.Currency,
		"payment_method":    paymentIntent.PaymentMethod,
		"processed_at":      time.Now(),
	}
	checkoutSession.GatewayData = mergeGatewayData(checkoutSession.GatewayData, updates)

	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// Create PaymentIntent record
	ticketIDs := w.getCheckoutSessionTicketIDs(checkoutSession)
	if len(ticketIDs) == 0 {
		tx.Rollback()
		return fmt.Errorf("no tickets found for checkout session")
	}

	// Get first ticket for event/tier info
	var firstTicket models.Ticket
	if err := tx.Where("id = ?", ticketIDs[0]).First(&firstTicket).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("first ticket not found: %w", err)
	}

	now := time.Now()

	// Check if PaymentIntent record already exists
	var existingPaymentIntent models.PaymentIntent
	if err := tx.Where("idempotency_key = ?", paymentIntent.ID).First(&existingPaymentIntent).Error; err == nil {
		// PaymentIntent already exists, skip creation
		log.Printf("[WEBHOOK_WORKER] PaymentIntent record already exists for: %s", paymentIntent.ID)
	} else if err != gorm.ErrRecordNotFound {
		tx.Rollback()
		return fmt.Errorf("error checking existing payment intent: %w", err)
	} else {
		// Create PaymentIntent record
		paymentIntentRecord := &models.PaymentIntent{
			ID:                uuid.New(),
			PaymentGateway:    string(models.PaymentGatewayStripe),
			IdempotencyKey:    paymentIntent.ID,
			UserID:            checkoutSession.UserID,
			GuestUserID:       checkoutSession.GuestUserID,
			CustomerEmail:     paymentIntent.ReceiptEmail,
			EventID:           firstTicket.EventID,
			TierID:            firstTicket.TierID,
			Quantity:          len(ticketIDs),
			Currency:          strings.ToUpper(string(paymentIntent.Currency)),
			TotalAmount:       float64(paymentIntent.Amount) / 100,
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
				"event_id":       firstTicket.EventID.String(),
				"ticket_count":   len(ticketIDs),
			},
			SucceededAt: &now,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		if err := tx.Create(paymentIntentRecord).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to create payment intent record: %w", err)
		}
	}

	// Process the successful payment
	if err := w.ticketService.ProcessSuccessfulPayment(checkoutSession.CheckoutToken); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to process successful payment: %w", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// handlePaymentIntentFailedSecure handles failed payment events
func (w *StripeWebhookWorker) handlePaymentIntentFailedSecure(ctx context.Context, data interface{}, webhookEventID uuid.UUID, requestID string) error {
	paymentIntent, ok := data.(*stripe.PaymentIntent)
	if !ok {
		return fmt.Errorf("invalid payment intent data")
	}

	log.Printf("[WEBHOOK_WORKER] Processing failed payment: %s", paymentIntent.ID)

	// Start database transaction
	tx := w.ticketService.GetDB().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("[WEBHOOK_WORKER] Panic during payment failure processing: %v", r)
		}
	}()

	// Find the checkout session
	checkoutToken, ok := paymentIntent.Metadata["checkout_token"]
	if !ok || checkoutToken == "" {
		tx.Rollback()
		return fmt.Errorf("checkout token not found in payment intent metadata")
	}

	var checkoutSession models.CheckoutSession
	if err := tx.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("checkout session not found: %w", err)
	}

	// Update checkout session status
	checkoutSession.Status = "failed"
	updates := map[string]interface{}{
		"payment_intent_id": paymentIntent.ID,
		"failure_reason":    "payment_failed",
		"failed_at":         time.Now(),
	}
	checkoutSession.GatewayData = mergeGatewayData(checkoutSession.GatewayData, updates)

	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// Process the failed payment
	if err := w.ticketService.ProcessFailedPayment(checkoutSession.CheckoutToken); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to process failed payment: %w", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// handlePaymentIntentCanceledSecure handles canceled payment events
func (w *StripeWebhookWorker) handlePaymentIntentCanceledSecure(ctx context.Context, data interface{}, webhookEventID uuid.UUID, requestID string) error {
	paymentIntent, ok := data.(*stripe.PaymentIntent)
	if !ok {
		return fmt.Errorf("invalid payment intent data")
	}

	log.Printf("[WEBHOOK_WORKER] Processing canceled payment: %s", paymentIntent.ID)

	// Start database transaction
	tx := w.ticketService.GetDB().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("[WEBHOOK_WORKER] Panic during payment cancel processing: %v", r)
		}
	}()

	// Find the checkout session
	checkoutToken, ok := paymentIntent.Metadata["checkout_token"]
	if !ok || checkoutToken == "" {
		tx.Rollback()
		return fmt.Errorf("checkout token not found in payment intent metadata")
	}

	var checkoutSession models.CheckoutSession
	if err := tx.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("checkout session not found: %w", err)
	}

	// Update checkout session status
	checkoutSession.Status = "canceled"
	updates := map[string]interface{}{
		"payment_intent_id":   paymentIntent.ID,
		"cancellation_reason": "user_canceled",
		"canceled_at":         time.Now(),
	}
	checkoutSession.GatewayData = mergeGatewayData(checkoutSession.GatewayData, updates)

	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// Process the canceled payment
	if err := w.ticketService.ProcessCanceledPayment(checkoutSession.CheckoutToken); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to process canceled payment: %w", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// handleCheckoutSessionCompletedSecure handles completed checkout session events
func (w *StripeWebhookWorker) handleCheckoutSessionCompletedSecure(ctx context.Context, data interface{}, webhookEventID uuid.UUID, requestID string) error {
	if w == nil {
		return fmt.Errorf("webhook worker is nil")
	}
	if w.ticketService == nil {
		return fmt.Errorf("ticket service is nil")
	}

	checkoutSession, ok := data.(*stripe.CheckoutSession)
	if !ok {
		return fmt.Errorf("invalid checkout session data")
	}
	if checkoutSession == nil {
		return fmt.Errorf("checkout session is nil")
	}

	log.Printf("[WEBHOOK_WORKER] Processing completed checkout session: %s", checkoutSession.ID)

	// Start database transaction
	tx := w.ticketService.GetDB().Begin()
	if tx == nil {
		return fmt.Errorf("failed to begin database transaction")
	}
	if tx == nil {
		return fmt.Errorf("failed to begin transaction")
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("[WEBHOOK_WORKER] Panic during checkout session processing: %v", r)
		}
	}()

	// Find our checkout session by Stripe checkout session ID
	var dbCheckoutSession models.CheckoutSession
	if err := tx.Where("stripe_session_id = ?", checkoutSession.ID).First(&dbCheckoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("checkout session not found: %w", err)
	}

	// Validate dbCheckoutSession
	if dbCheckoutSession.ID == uuid.Nil {
		tx.Rollback()
		return fmt.Errorf("database checkout session ID is nil")
	}

	// Update checkout session with additional data
	updates := map[string]interface{}{
		"stripe_session_id": checkoutSession.ID,
		"payment_status":    checkoutSession.PaymentStatus,
		"customer_email":    checkoutSession.CustomerEmail,
		"amount_total":      checkoutSession.AmountTotal,
		"currency":          checkoutSession.Currency,
		"processed_at":      time.Now(),
	}
	dbCheckoutSession.GatewayData = mergeGatewayData(dbCheckoutSession.GatewayData, updates)

	if err := tx.Save(&dbCheckoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// If payment was successful, process the payment
	if checkoutSession.PaymentStatus == "paid" {
		if err := w.ticketService.ProcessSuccessfulPayment(dbCheckoutSession.CheckoutToken); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to process successful payment: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// findCheckoutSessionByEvent finds a checkout session using multiple lookup strategies
func (w *StripeWebhookWorker) findCheckoutSessionByEvent(tx *gorm.DB, eventType string, eventData interface{}, requestID string) (*models.CheckoutSession, error) {
	var checkoutSession models.CheckoutSession

	switch eventType {
	case "payment_intent.succeeded", "payment_intent.payment_failed", "payment_intent.canceled":
		paymentIntent, ok := eventData.(*stripe.PaymentIntent)
		if !ok {
			return nil, fmt.Errorf("invalid payment intent data")
		}

		// Strategy 1: Try checkout_token from metadata first
		if checkoutToken, ok := paymentIntent.Metadata["checkout_token"]; ok && checkoutToken != "" {
			if err := tx.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err == nil {
				return &checkoutSession, nil
			}
			log.Printf("[WEBHOOK_WORKER] Checkout session not found by checkout_token: %s", checkoutToken)
		}

		// Strategy 2: Try to find by payment_intent_id in gateway_data
		if err := tx.Where("gateway_data->>'payment_intent_id' = ?", paymentIntent.ID).First(&checkoutSession).Error; err == nil {
			return &checkoutSession, nil
		}

		return nil, fmt.Errorf("checkout session not found for payment_intent: %s", paymentIntent.ID)

	case "checkout.session.completed":
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

// getCheckoutSessionTicketIDs extracts ticket IDs from checkout session
func (w *StripeWebhookWorker) getCheckoutSessionTicketIDs(checkoutSession *models.CheckoutSession) []uuid.UUID {
	var ids []uuid.UUID

	if checkoutSession.GatewayData != nil {
		if ticketIDsRaw, ok := checkoutSession.GatewayData["ticket_ids"]; ok {
			ids = w.parseTicketIDsFromGatewayData(ticketIDsRaw)
		}
	}

	if len(ids) == 0 && checkoutSession.TicketID != uuid.Nil {
		ids = append(ids, checkoutSession.TicketID)
	}

	seen := make(map[uuid.UUID]bool)
	uniqueIDs := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		uniqueIDs = append(uniqueIDs, id)
	}

	return uniqueIDs
}

// parseTicketIDsFromGatewayData parses ticket IDs from various gateway data formats
func (w *StripeWebhookWorker) parseTicketIDsFromGatewayData(raw interface{}) []uuid.UUID {
	switch value := raw.(type) {
	case []uuid.UUID:
		return value
	case []string:
		return w.extractUUIDsFromValues(value)
	case []interface{}:
		ids := make([]uuid.UUID, 0, len(value))
		for _, item := range value {
			switch typedItem := item.(type) {
			case uuid.UUID:
				ids = append(ids, typedItem)
			case string:
				parsedID, err := uuid.Parse(strings.TrimSpace(typedItem))
				if err == nil {
					ids = append(ids, parsedID)
				}
			}
		}
		return ids
	case string:
		trimmed := strings.TrimSpace(value)
		if parsedID, err := uuid.Parse(trimmed); err == nil {
			return []uuid.UUID{parsedID}
		}

		var asStrings []string
		if err := json.Unmarshal([]byte(trimmed), &asStrings); err == nil {
			return w.extractUUIDsFromValues(asStrings)
		}
	case []byte:
		var asStrings []string
		if err := json.Unmarshal(value, &asStrings); err == nil {
			return w.extractUUIDsFromValues(asStrings)
		}

		var asInterfaces []interface{}
		if err := json.Unmarshal(value, &asInterfaces); err == nil {
			return w.parseTicketIDsFromGatewayData(asInterfaces)
		}
	}

	return nil
}

// extractUUIDsFromValues extracts UUIDs from string slice
func (w *StripeWebhookWorker) extractUUIDsFromValues(values []string) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if parsedID, err := uuid.Parse(trimmed); err == nil {
			ids = append(ids, parsedID)
		}
	}
	return ids
}

// mergeGatewayData safely merges new gateway data with existing data
func mergeGatewayData(existing map[string]interface{}, updates map[string]interface{}) map[string]interface{} {
	if existing == nil {
		existing = make(map[string]interface{})
	}

	result := make(map[string]interface{})
	for k, v := range existing {
		result[k] = v
	}

	for k, v := range updates {
		result[k] = v
	}

	return result
}
