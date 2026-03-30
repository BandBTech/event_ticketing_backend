package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stripe/stripe-go/v74"
	"github.com/stripe/stripe-go/v74/paymentintent"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"
)

// PaymentWorker handles asynchronous payment processing tasks
type PaymentWorker struct {
	client                *asynq.Client
	server                *asynq.Server
	ticketService         *services.TicketService
	reservationService    *services.ReservationService
	emailOutboxService    *services.EmailOutboxService
	processingLockService *services.ProcessingLockService
	cfg                   *config.Config
	queueConfig           *config.QueueConfig
	mux                   *asynq.ServeMux
	isRunning             bool      // Health check flag
	lastHeartbeat         time.Time // Track last activity
	processingTasksCount  int64     // Concurrent tasks being processed
}

// PaymentTaskPayload represents the payload for payment processing tasks
type PaymentTaskPayload struct {
	StripeEventID  string                 `json:"stripe_event_id"`
	EventType      string                 `json:"event_type"`
	WebhookEventID uuid.UUID              `json:"webhook_event_id"`
	RequestID      string                 `json:"request_id"`
	EventData      map[string]interface{} `json:"event_data"`
	RawData        json.RawMessage        `json:"raw_data"`
}

const (
	// Task types
	TypePaymentSuccess  = "payment:success"
	TypePaymentFailed   = "payment:failed"
	TypePaymentCanceled = "payment:canceled"
	TypeChargeRefunded  = "charge:refunded"

	// Queue names
	QueueCritical = "critical"
	QueueDefault  = "default"
)

// NewPaymentWorker creates a new payment worker
func NewPaymentWorker(cfg *config.Config, ticketService *services.TicketService) *PaymentWorker {
	qc := config.NewQueueConfig(cfg)
	return &PaymentWorker{
		client:                asynq.NewClient(qc.GetRedisClientOpt()),
		ticketService:         ticketService,
		reservationService:    services.NewReservationService(ticketService.GetDB()),
		emailOutboxService:    services.NewEmailOutboxService(ticketService.GetDB()),
		processingLockService: services.NewProcessingLockService(ticketService.GetDB()),
		cfg:                   cfg,
		queueConfig:           qc,
		mux:                   asynq.NewServeMux(),
	}
}

// RegisterHandlers registers all payment task handlers
func (pw *PaymentWorker) RegisterHandlers() {
	pw.mux.HandleFunc(TypePaymentSuccess, pw.HandlePaymentSuccess)
	pw.mux.HandleFunc(TypePaymentFailed, pw.HandlePaymentFailed)
	pw.mux.HandleFunc(TypePaymentCanceled, pw.HandlePaymentCanceled)
	pw.mux.HandleFunc(TypeChargeRefunded, pw.HandleChargeRefunded)
}

// InitServer initializes the asynq server
func (pw *PaymentWorker) InitServer() error {
	serverCfg := pw.queueConfig.GetServerConfig()
	srv := asynq.NewServer(pw.queueConfig.GetRedisClientOpt(), serverCfg)
	pw.server = srv
	return nil
}

// Start starts the worker server
func (pw *PaymentWorker) Start(ctx context.Context) error {
	if pw.server == nil {
		if err := pw.InitServer(); err != nil {
			return fmt.Errorf("failed to initialize server: %w", err)
		}
	}
	pw.RegisterHandlers()

	pw.isRunning = true
	pw.lastHeartbeat = time.Now()
	log.Println("✅ Starting payment worker (asynq server)...")

	// Start the server - this blocks until it's shut down
	err := pw.server.Start(pw.mux)

	// If we get here, server stopped (either error or graceful shutdown)
	pw.isRunning = false
	if err != nil {
		log.Printf("⚠️  Payment worker stopped with error: %v\n", err)
		return fmt.Errorf("payment worker error: %w", err)
	}

	log.Println("Payment worker stopped gracefully")
	return nil
}

// EnqueuePaymentSuccess enqueues a successful payment task
func (pw *PaymentWorker) EnqueuePaymentSuccess(ctx context.Context, payload *PaymentTaskPayload) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	task := asynq.NewTask(
		TypePaymentSuccess,
		data,
		asynq.Queue(QueueCritical),
		asynq.Unique(10*time.Minute), // Idempotent within 10-minute window per Stripe event
	)
	info, err := pw.client.EnqueueContext(ctx, task)
	if err != nil {
		return "", fmt.Errorf("failed to enqueue task: %w", err)
	}

	log.Printf("Enqueued payment success task (ID: %s, EventID: %s)\n", info.ID, payload.StripeEventID)
	return info.ID, nil
}

// EnqueuePaymentFailed enqueues a failed payment task
func (pw *PaymentWorker) EnqueuePaymentFailed(ctx context.Context, payload *PaymentTaskPayload) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	task := asynq.NewTask(
		TypePaymentFailed,
		data,
		asynq.Queue(QueueDefault),
		asynq.Unique(10*time.Minute), // Idempotent within 10-minute window per Stripe event
	)
	info, err := pw.client.EnqueueContext(ctx, task)
	if err != nil {
		return "", fmt.Errorf("failed to enqueue task: %w", err)
	}

	log.Printf("Enqueued payment failed task (ID: %s, EventID: %s)\n", info.ID, payload.StripeEventID)
	return info.ID, nil
}

// EnqueueChargeRefunded enqueues a charge refunded task
func (pw *PaymentWorker) EnqueueChargeRefunded(ctx context.Context, payload *PaymentTaskPayload) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	task := asynq.NewTask(
		TypeChargeRefunded,
		data,
		asynq.Queue(QueueCritical),
		asynq.Unique(10*time.Minute), // Idempotent within 10-minute window per Stripe event
	)
	info, err := pw.client.EnqueueContext(ctx, task)
	if err != nil {
		return "", fmt.Errorf("failed to enqueue task: %w", err)
	}

	log.Printf("Enqueued charge refunded task (ID: %s, EventID: %s)\n", info.ID, payload.StripeEventID)
	return info.ID, nil
}

// HandlePaymentSuccess processes successful payment webhook asynchronously
func (pw *PaymentWorker) HandlePaymentSuccess(ctx context.Context, t *asynq.Task) error {
	var payload PaymentTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		log.Printf("ERROR: Failed to unmarshal payload: %v\n", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	log.Printf("Processing payment success task (EventID: %s, WebhookID: %s, EventType: %s)\n", payload.StripeEventID, payload.WebhookEventID, payload.EventType)

	var paymentIntent *stripe.PaymentIntent

	// Handle different event types
	switch payload.EventType {
	case "payment_intent.succeeded":
		paymentIntent = &stripe.PaymentIntent{}
		if err := json.Unmarshal(payload.RawData, paymentIntent); err != nil {
			log.Printf("ERROR: Failed to unmarshal payment intent: %v\n", err)
			pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "failed", fmt.Sprintf("Unmarshal error: %v", err), nil, nil)
			return fmt.Errorf("failed to unmarshal payment intent: %w", err)
		}

	case "checkout.session.completed":
		session := &stripe.CheckoutSession{}
		if err := json.Unmarshal(payload.RawData, session); err != nil {
			log.Printf("ERROR: Failed to unmarshal checkout session: %v\n", err)
			pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "failed", fmt.Sprintf("Unmarshal error: %v", err), nil, nil)
			return fmt.Errorf("failed to unmarshal checkout session: %w", err)
		}

		// Extract payment intent from session
		if session.PaymentIntent == nil {
			errMsg := "checkout session missing payment intent"
			pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "failed", errMsg, nil, nil)
			return fmt.Errorf(errMsg)
		}

		// Fetch the full PaymentIntent object from Stripe API to get complete data
		// This ensures all fields are populated (PaymentMethod, Customer, ClientSecret, ReceiptEmail, etc.)
		piID := session.PaymentIntent.ID
		stripePI, err := paymentintent.Get(piID, nil)
		if err != nil {
			errMsg := fmt.Sprintf("failed to fetch payment intent from stripe: %v", err)
			log.Printf("ERROR: %s\n", errMsg)
			pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "failed", errMsg, nil, nil)
			return fmt.Errorf(errMsg)
		}
		paymentIntent = stripePI

	default:
		errMsg := fmt.Sprintf("unsupported event type: %s", payload.EventType)
		pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "failed", errMsg, nil, nil)
		return fmt.Errorf(errMsg)
	}

	// Process payment in database
	if err := pw.processPaymentIntentSucceeded(ctx, payload.WebhookEventID, paymentIntent, payload.StripeEventID, payload.RequestID, payload.RawData); err != nil {
		log.Printf("ERROR: Failed to process payment success: %v\n", err)
		pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "failed", err.Error(), nil, nil)
		return fmt.Errorf("payment processing failed: %w", err)
	}

	// Update heartbeat for health monitoring
	pw.UpdateHeartbeat()

	log.Printf("✅ Payment success processed (EventID: %s, WebhookID: %s)\n", payload.StripeEventID, payload.WebhookEventID)
	return nil
}

// HandlePaymentFailed processes failed payment webhook asynchronously
func (pw *PaymentWorker) HandlePaymentFailed(ctx context.Context, t *asynq.Task) error {
	var payload PaymentTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		log.Printf("ERROR: Failed to unmarshal payload: %v\n", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	log.Printf("Processing payment failed task (EventID: %s, WebhookID: %s)\n", payload.StripeEventID, payload.WebhookEventID)

	paymentIntent := &stripe.PaymentIntent{}
	if err := json.Unmarshal(payload.RawData, paymentIntent); err != nil {
		log.Printf("ERROR: Failed to unmarshal payment intent: %v\n", err)
		pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "failed", fmt.Sprintf("Unmarshal error: %v", err), nil, nil)
		return fmt.Errorf("failed to unmarshal payment intent: %w", err)
	}

	// Process payment failure in database
	if err := pw.processPaymentIntentFailed(ctx, paymentIntent, payload.WebhookEventID, payload.RequestID); err != nil {
		log.Printf("ERROR: Failed to process payment failure: %v\n", err)
		pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "failed", err.Error(), nil, nil)
		return fmt.Errorf("payment failure processing failed: %w", err)
	}

	// Update webhook event status to succeeded (webhook was processed, even though payment failed)
	pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "succeeded", "", nil, nil)

	// Update heartbeat for health monitoring
	pw.UpdateHeartbeat()

	log.Printf("✅ Payment failure processed (EventID: %s, WebhookID: %s)\n", payload.StripeEventID, payload.WebhookEventID)
	return nil
}

// HandlePaymentCanceled processes canceled payment webhook asynchronously
func (pw *PaymentWorker) HandlePaymentCanceled(ctx context.Context, t *asynq.Task) error {
	var payload PaymentTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		log.Printf("ERROR: Failed to unmarshal payload: %v\n", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	log.Printf("Processing payment canceled task (EventID: %s, WebhookID: %s)\n", payload.StripeEventID, payload.WebhookEventID)

	paymentIntent := &stripe.PaymentIntent{}
	if err := json.Unmarshal(payload.RawData, paymentIntent); err != nil {
		log.Printf("ERROR: Failed to unmarshal payment intent: %v\n", err)
		pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "failed", fmt.Sprintf("Unmarshal error: %v", err), nil, nil)
		return fmt.Errorf("failed to unmarshal payment intent: %w", err)
	}

	// Process payment cancel in database
	if err := pw.processPaymentIntentCanceled(ctx, paymentIntent, payload.WebhookEventID, payload.RequestID); err != nil {
		log.Printf("ERROR: Failed to process payment cancel: %v\n", err)
		pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "failed", err.Error(), nil, nil)
		return fmt.Errorf("payment cancel processing failed: %w", err)
	}

	// Update webhook event status to succeeded (webhook was processed, even though payment was canceled)
	pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "succeeded", "", nil, nil)

	// Update heartbeat for health monitoring
	pw.UpdateHeartbeat()

	log.Printf("✅ Payment canceled processed (EventID: %s, WebhookID: %s)\n", payload.StripeEventID, payload.WebhookEventID)
	return nil
}

// HandleChargeRefunded processes refunded charge webhook asynchronously
func (pw *PaymentWorker) HandleChargeRefunded(ctx context.Context, t *asynq.Task) error {
	var payload PaymentTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		log.Printf("ERROR: Failed to unmarshal payload: %v\n", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	log.Printf("Processing charge refunded task (EventID: %s, WebhookID: %s)\n", payload.StripeEventID, payload.WebhookEventID)

	charge := &stripe.Charge{}
	if err := json.Unmarshal(payload.RawData, charge); err != nil {
		log.Printf("ERROR: Failed to unmarshal charge: %v\n", err)
		pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "failed", fmt.Sprintf("Unmarshal error: %v", err), nil, nil)
		return fmt.Errorf("failed to unmarshal charge: %w", err)
	}

	// Process charge refund in database
	if err := pw.processChargeRefunded(ctx, charge, payload.WebhookEventID, payload.RequestID); err != nil {
		log.Printf("ERROR: Failed to process charge refund: %v\n", err)
		pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "failed", err.Error(), nil, nil)
		return fmt.Errorf("charge refund processing failed: %w", err)
	}

	// Update webhook event status to succeeded
	pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "succeeded", "", nil, nil)

	// Update heartbeat for health monitoring
	pw.UpdateHeartbeat()

	log.Printf("✅ Charge refund processed (EventID: %s, WebhookID: %s)\n", payload.StripeEventID, payload.WebhookEventID)
	return nil
}

// processPaymentIntentSucceeded processes successful payment intents using the new production architecture
func (pw *PaymentWorker) processPaymentIntentSucceeded(ctx context.Context, webhookEventID uuid.UUID, paymentIntent *stripe.PaymentIntent, stripeEventID string, requestID string, rawData []byte) error {
	// ========================================
	// LOG ALL STRIPE RESPONSE DATA
	// ========================================
	log.Printf("[PAYMENT_SUCCESS] Processing payment intent: %s (request_id: %s)\n", paymentIntent.ID, requestID)

	// Log all Stripe response data
	log.Printf("[STRIPE_RESPONSE] ===== FULL STRIPE PAYMENT INTENT DATA =====")
	log.Printf("[STRIPE_RESPONSE] PaymentIntent ID: %s", paymentIntent.ID)
	log.Printf("[STRIPE_RESPONSE] Status: %s", paymentIntent.Status)
	log.Printf("[STRIPE_RESPONSE] Amount: %d %s", paymentIntent.Amount, paymentIntent.Currency)
	log.Printf("[STRIPE_RESPONSE] ClientSecret: %s", paymentIntent.ClientSecret)
	log.Printf("[STRIPE_RESPONSE] ReceiptEmail: %s", paymentIntent.ReceiptEmail)
	log.Printf("[STRIPE_RESPONSE] Created: %d", paymentIntent.Created)
	log.Printf("[STRIPE_RESPONSE] PaymentMethod: %v", paymentIntent.PaymentMethod)
	log.Printf("[STRIPE_RESPONSE] Description: %s", paymentIntent.Description)
	log.Printf("[STRIPE_RESPONSE] Customer: %v", paymentIntent.Customer)
	log.Printf("[STRIPE_RESPONSE] Metadata: %v", paymentIntent.Metadata)
	log.Printf("[STRIPE_RESPONSE] ===== END STRIPE DATA =====")

	// ========================================
	// EXTRACT ESSENTIAL DATA FROM RAW WEBHOOK
	// ========================================
	var chargeID string
	var paymentMethodType string
	var paymentMethodDetails map[string]interface{}
	var captureMethod string

	if rawData != nil {
		var rawPaymentIntent map[string]interface{}
		if err := json.Unmarshal(rawData, &rawPaymentIntent); err == nil {
			// Extract Charge ID - try multiple locations
			// First try: latest_charge field (direct reference on PaymentIntent)
			if latestCharge, ok := rawPaymentIntent["latest_charge"].(string); ok && latestCharge != "" {
				chargeID = latestCharge
				log.Printf("[STRIPE_RESPONSE] Charge ID (from latest_charge): %s", chargeID)
			}

			// Second try: charges.data[0].id (in case latest_charge not available)
			if chargeID == "" {
				if charges, ok := rawPaymentIntent["charges"].(map[string]interface{}); ok {
					if data, ok := charges["data"].([]interface{}); ok && len(data) > 0 {
						if charge, ok := data[0].(map[string]interface{}); ok {
							if id, ok := charge["id"].(string); ok {
								chargeID = id
								log.Printf("[STRIPE_RESPONSE] Charge ID (from charges.data): %s", chargeID)
							}
						}
					}
				}
			}

			// NOTE: For checkout.session.completed events, charge ID might not be available
			// The payment_intent.ID (stored as GatewayTxnID) is the primary transaction identifier
			if chargeID == "" {
				log.Printf("[STRIPE_RESPONSE] INFO: Charge ID not found in webhook (may be normal for checkout.session events)")
			}

			// Extract Capture Method
			if cm, ok := rawPaymentIntent["capture_method"].(string); ok {
				captureMethod = cm
				log.Printf("[STRIPE_RESPONSE] Capture Method: %s", captureMethod)
			}

			// Extract Payment Method Type and Details
			if paymentMethods, ok := rawPaymentIntent["payment_method_types"].([]interface{}); ok && len(paymentMethods) > 0 {
				if pmType, ok := paymentMethods[0].(string); ok {
					paymentMethodType = pmType
					log.Printf("[STRIPE_RESPONSE] Payment Method Type: %s", paymentMethodType)
				}
			}

			// Extract detailed payment method info from payment_method_options or charges
			paymentMethodDetails = make(map[string]interface{})
			if pmOptions, ok := rawPaymentIntent["payment_method_options"].(map[string]interface{}); ok {
				if card, ok := pmOptions["card"].(map[string]interface{}); ok {
					paymentMethodDetails["network"] = card["network"]
					paymentMethodDetails["three_d_secure"] = card["request_three_d_secure"]
					log.Printf("[STRIPE_RESPONSE] Card Options: network=%v, 3ds=%v",
						card["network"], card["request_three_d_secure"])
				}
			}

			// Extract payment method ID
			if pmID, ok := rawPaymentIntent["payment_method"].(string); ok {
				paymentMethodDetails["id"] = pmID
				log.Printf("[STRIPE_RESPONSE] Payment Method ID: %s", pmID)
			}

			// Extract from charges for last4, brand, etc.
			if charges, ok := rawPaymentIntent["charges"].(map[string]interface{}); ok {
				if data, ok := charges["data"].([]interface{}); ok && len(data) > 0 {
					if charge, ok := data[0].(map[string]interface{}); ok {
						if paymentDetails, ok := charge["payment_method_details"].(map[string]interface{}); ok {
							if card, ok := paymentDetails["card"].(map[string]interface{}); ok {
								paymentMethodDetails["brand"] = card["brand"]
								paymentMethodDetails["last4"] = card["last4"]
								paymentMethodDetails["exp_month"] = card["exp_month"]
								paymentMethodDetails["exp_year"] = card["exp_year"]
								paymentMethodDetails["fingerprint"] = card["fingerprint"]
								log.Printf("[STRIPE_RESPONSE] Card Details: brand=%v, last4=%v, exp=%v/%v",
									card["brand"], card["last4"], card["exp_month"], card["exp_year"])
							}
						}
					}
				}
			}
		}
	}
	if chargeID == "" {
		log.Printf("[STRIPE_RESPONSE] WARNING: Charge ID not found in webhook data")
	}

	// ========================================
	// PHASE 1: DISTRIBUTED LOCK ACQUISITION
	// ========================================
	lockKey := fmt.Sprintf("payment_intent:%s", paymentIntent.ID)
	lockAcquired, err := pw.processingLockService.AcquireLock(ctx, lockKey, "payment", "worker", 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to acquire processing lock: %w", err)
	}
	if !lockAcquired {
		log.Printf("[LOCK_CONFLICT] Payment already being processed: %s\n", paymentIntent.ID)
		return nil
	}
	defer func() {
		if unlockErr := pw.processingLockService.ReleaseLock(ctx, lockKey, "worker"); unlockErr != nil {
			log.Printf("WARN: Failed to release processing lock: %v\n", unlockErr)
		}
	}()

	// ========================================
	// PHASE 1B: EARLY LOAD OF PAYMENT INTENT FOR ERROR TRACKING
	// ========================================
	// Load early so we can track payment_intent_id even if later steps fail
	checkoutToken, _ := paymentIntent.Metadata["checkout_token"]
	if checkoutToken == "" {
		return fmt.Errorf("checkout token not found in payment intent metadata")
	}

	db := pw.ticketService.GetDB()

	// EARLY: Load the database PaymentIntent to get EventID and track it for webhook
	var dbPaymentIntent models.PaymentIntent
	if err := db.Where("checkout_token = ?", checkoutToken).First(&dbPaymentIntent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("[ERROR] Payment intent not found in database for token: %s\n", checkoutToken)
		}
		return fmt.Errorf("failed to load payment intent from database: %w", err)
	}
	log.Printf("[DB_PAYMENT_INTENT_LOADED_EARLY] ID=%s, EventID=%s for Stripe payment %s\n", dbPaymentIntent.ID, dbPaymentIntent.EventID, paymentIntent.ID)

	// ========================================
	// PHASE 2: RESERVATION CONFIRMATION
	// ========================================
	// Confirm the reservation atomically
	ticketIDs, err := pw.reservationService.ConfirmReservation(ctx, checkoutToken, paymentIntent.ID)
	if err != nil {
		// Check if this is a "reservation expired" error (common in idempotent webhook scenarios)
		if strings.Contains(err.Error(), "reservation expired") || strings.Contains(err.Error(), "expired") {
			log.Printf("[RESERVATION_EXPIRED_IDEMPOTENT] Reservation expired - checking if transaction already exists (browser callback scenario): %v\n", err)

			// Try to find existing transaction that was created by browser callback
			// If found, just update it with payment_intent_id and return success
			var existingTxn models.Transaction
			fiveMinutesAgo := time.Now().Add(-10 * time.Minute) // Extended window for expired reservations

			if !errors.Is(db.Where("event_id = ? AND created_at >= ?", dbPaymentIntent.EventID, fiveMinutesAgo).
				First(&existingTxn).Error, gorm.ErrRecordNotFound) {
				// Found! Update with payment_intent_id
				log.Printf("[IDEMPOTENT_RECOVERY] Found existing transaction %s, updating with payment_intent_id\n", existingTxn.ID)
				if err := db.Model(&existingTxn).Updates(map[string]interface{}{
					"payment_intent_id": &dbPaymentIntent.ID,
					"gateway_txn_id":    paymentIntent.ID,
				}).Error; err != nil {
					log.Printf("[IDEMPOTENT_RECOVERY] WARNING: Failed to update transaction: %v\n", err)
				}
				pw.updateWebhookEventStatus(ctx, webhookEventID, "succeeded", "", &dbPaymentIntent.ID, &existingTxn.ID)
				log.Printf("[PAYMENT_SUCCESS] Webhook idempotent (expired reservation): Transaction %s updated\n", existingTxn.ID)
				return nil
			}
		}

		// IMPORTANT: Even on reservation error, we have dbPaymentIntent now to track in webhook
		log.Printf("[RESERVATION_ERROR] Failed to confirm reservation: %v (will record in webhook with payment_intent_id)\n", err)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", err.Error(), &dbPaymentIntent.ID, nil)
		return fmt.Errorf("failed to confirm reservation: %w", err)
	}

	log.Printf("[RESERVATION] Confirmed reservation for checkout token %s\n", checkoutToken)

	// ========================================
	// PHASE 3: TRANSACTION RECORDING
	// ========================================
	if len(ticketIDs) > 0 {
		// Get created tickets for transaction recording
		var createdTickets []*models.Ticket
		if err := db.Where("id IN ?", ticketIDs).Find(&createdTickets).Error; err != nil {
			log.Printf("[TRANSACTION_ERROR] Failed to find created tickets: %v\n", err)
			pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", fmt.Sprintf("Failed to find tickets: %v", err), &dbPaymentIntent.ID, nil)
			return fmt.Errorf("failed to find created tickets: %w", err)
		}

		// Record transaction
		paymentIntentUUID := dbPaymentIntent.ID
		if err := pw.ticketService.RecordTransaction(createdTickets, models.PaymentGatewayStripe, paymentIntent.ID, nil, &paymentIntentUUID); err != nil {
			log.Printf("[TRANSACTION_ERROR] Failed to record transaction: %v\n", err)
			pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", fmt.Sprintf("Failed to record transaction: %v", err), &dbPaymentIntent.ID, nil)
			return fmt.Errorf("failed to record transaction: %w", err)
		}
		log.Printf("[TRANSACTION] Recorded transaction for %d tickets\n", len(createdTickets))
	}

	// ========================================
	// PHASE 4: ATOMIC PAYMENT PROCESSING + PAYMENT INTENT UPDATE
	// ========================================

	// Update PaymentIntent with gateway payment ID
	if err := db.Model(&models.PaymentIntent{}).
		Where("id = ?", dbPaymentIntent.ID).
		Update("gateway_payment_id", paymentIntent.ID).Error; err != nil {
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", fmt.Sprintf("failed to update gateway_payment_id: %v", err), &dbPaymentIntent.ID, nil)
		return fmt.Errorf("failed to update payment intent gateway_payment_id: %w", err)
	}

	// Load event with tiers for commission calculation and currency
	var event models.Event
	if err := db.Preload("Tiers").Where("id = ?", dbPaymentIntent.EventID).First(&event).Error; err != nil {
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", fmt.Sprintf("failed to load event: %v", err), &dbPaymentIntent.ID, nil)
		return fmt.Errorf("failed to load event: %w", err)
	}

	tx := db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("ERROR: Panic during atomic payment processing: %v\n", r)
		}
	}()

	// Update payment intent status
	now := time.Now()

	// Build comprehensive gateway response data
	gatewayResponse := map[string]interface{}{
		"payment_intent_id":   paymentIntent.ID,
		"charge_id":           chargeID,
		"status":              string(paymentIntent.Status),
		"amount":              paymentIntent.Amount,
		"currency":            paymentIntent.Currency,
		"created":             paymentIntent.Created,
		"receipt_email":       paymentIntent.ReceiptEmail,
		"description":         paymentIntent.Description,
		"client_secret":       paymentIntent.ClientSecret,
		"payment_method_id":   paymentIntent.PaymentMethod,
		"payment_method_type": paymentMethodType,
		"capture_method":      captureMethod,
		"metadata":            paymentIntent.Metadata,
		"processed_at":        now,
	}

	log.Printf("[GATEWAY_RESPONSE] Saved Stripe response to DB: %v", gatewayResponse)

	paymentUpdate := map[string]interface{}{
		"status":                 "succeeded",
		"succeeded_at":           now,
		"updated_at":             now,
		"gateway_response":       gatewayResponse,
		"gateway_charge_id":      chargeID,
		"payment_method_type":    paymentMethodType,
		"payment_method_details": paymentMethodDetails,
		"capture_method":         captureMethod,
	}

	if err := tx.Model(&models.PaymentIntent{}).
		Where("gateway_payment_id = ?", paymentIntent.ID).
		Updates(paymentUpdate).Error; err != nil {
		tx.Rollback()
		errMsg := fmt.Sprintf("failed to update payment intent status: %v", err)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	// Get ticket IDs from checkout session (unified purchase system)
	// checkoutToken is already extracted from paymentIntent.Metadata at the beginning of this function

	if checkoutToken == "" {
		tx.Rollback()
		errMsg := "checkout_token not found in payment intent metadata"
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	// Find checkout session by checkout_token
	var checkoutSession models.CheckoutSession
	if err := tx.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		errMsg := fmt.Sprintf("failed to find checkout session: %v", err)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	// ========================================
	// PHASE 3A: CREATE TICKETS (if not already created)
	// This is the ONLY place tickets are created
	// ========================================
	var tickets []models.Ticket

	// Check if ticket_ids already exist in checkout session (idempotent case)
	if checkoutSession.GatewayData != nil {
		if ticketIDsData, ok := checkoutSession.GatewayData["ticket_ids"]; ok && ticketIDsData != nil {
			switch v := ticketIDsData.(type) {
			case []uuid.UUID:
				ticketIDs = v
			case []interface{}:
				for _, id := range v {
					if idStr, ok := id.(string); ok {
						if parsedID, err := uuid.Parse(idStr); err == nil {
							ticketIDs = append(ticketIDs, parsedID)
						}
					}
				}
			}
		}
	}

	// If no existing tickets, create them from reservations
	if len(ticketIDs) == 0 {
		log.Printf("[TICKETS_CREATION] No existing tickets found - creating from reservations for checkout=%s\n", checkoutToken)

		// Find reservations for this checkout
		var reservations []models.TicketReservation
		if err := tx.Where("checkout_token = ? AND status = ?", checkoutToken, models.ReservationStatusReserved).
			Find(&reservations).Error; err != nil {
			tx.Rollback()
			errMsg := fmt.Sprintf("failed to find reservations: %v", err)
			pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
			return fmt.Errorf(errMsg)
		}

		if len(reservations) == 0 {
			tx.Rollback()
			errMsg := "no reservations found for checkout token"
			pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
			return fmt.Errorf(errMsg)
		}

		// Create individual tickets for each reservation
		for _, reservation := range reservations {
			// Get tier for pricing and currency
			var tier models.EventTier
			if err := tx.Where("id = ?", reservation.TierID).First(&tier).Error; err != nil {
				tx.Rollback()
				errMsg := fmt.Sprintf("failed to get tier for ticket creation: %v", err)
				pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
				return fmt.Errorf(errMsg)
			}

			// Create quantity tickets for this tier
			for i := 0; i < reservation.Quantity; i++ {
				// Generate unique ticket number
				ticketNumber, err := utils.GenerateEventTicketNumber(tx, tier.TierName, time.Now().Year())
				if err != nil {
					tx.Rollback()
					errMsg := fmt.Sprintf("failed to generate ticket number: %v", err)
					pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
					return fmt.Errorf(errMsg)
				}

				// Create ticket record
				ticket := &models.Ticket{
					ID:              uuid.New(),
					EventID:         reservation.EventID,
					TierID:          reservation.TierID,
					UserID:          reservation.UserID,
					GuestUserID:     reservation.GuestUserID,
					TicketNumber:    ticketNumber,
					TotalAmount:     tier.Price, // Individual ticket price
					PaymentGateway:  models.PaymentGatewayStripe,
					Status:          "active", // Immediately active after webhook confirms payment
					PaymentStatus:   "completed",
					IsGuestPurchase: reservation.GuestUserID != nil,
					PaidAt:          &now,
					CreatedAt:       now,
					UpdatedAt:       now,
				}

				if err := tx.Create(ticket).Error; err != nil {
					tx.Rollback()
					errMsg := fmt.Sprintf("failed to create ticket: %v", err)
					pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
					return fmt.Errorf(errMsg)
				}

				tickets = append(tickets, *ticket)
				ticketIDs = append(ticketIDs, ticket.ID)
				log.Printf("[TICKET_CREATED] id=%s, tier=%s, number=%s\n", ticket.ID, tier.TierName, ticket.TicketNumber)
			}

			// Mark reservation as confirmed (not completed yet - that's after entire webhook succeeds)
			if err := tx.Model(&reservation).Update("status", models.ReservationStatusConfirmed).Error; err != nil {
				tx.Rollback()
				errMsg := fmt.Sprintf("failed to update reservation: %v", err)
				pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
				return fmt.Errorf(errMsg)
			}
		}

		// Store ticket_ids in checkout session for future reference
		if checkoutSession.GatewayData == nil {
			checkoutSession.GatewayData = make(map[string]interface{})
		}
		checkoutSession.GatewayData["ticket_ids"] = ticketIDs
		if err := tx.Save(&checkoutSession).Error; err != nil {
			tx.Rollback()
			errMsg := fmt.Sprintf("failed to update checkout session with ticket_ids: %v", err)
			pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
			return fmt.Errorf(errMsg)
		}

		log.Printf("[TICKETS_CREATED_SUMMARY] checkout=%s, total_tickets=%d, ticket_ids=%v\n", checkoutToken, len(ticketIDs), ticketIDs)
	} else {
		// Tickets already exist - fetch them by ID
		if err := tx.Where("id IN ?", ticketIDs).
			Preload("Tier").
			Find(&tickets).Error; err != nil {
			tx.Rollback()
			errMsg := fmt.Sprintf("failed to find existing tickets: %v", err)
			pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
			return fmt.Errorf(errMsg)
		}

		log.Printf("[TICKETS_IDEMPOTENT] Tickets already exist for checkout=%s, count=%d\n", checkoutToken, len(tickets))
	}

	// ========================================
	// Verify we have tickets
	// ========================================
	if len(tickets) == 0 {
		tx.Rollback()
		errMsg := "no tickets found after creation/lookup"
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	// Load full tier data if not already loaded (for idempotent case)
	if len(tickets) > 0 && tickets[0].Tier == nil {
		if err := tx.Preload("Tier").Find(&tickets, "id IN ?", ticketIDs).Error; err != nil {
			tx.Rollback()
			errMsg := fmt.Sprintf("failed to load tier data: %v", err)
			pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
			return fmt.Errorf(errMsg)
		}
	}

	// ========================================
	// PHASE 3B: UPDATE TICKET PAYMENT STATUS
	// ========================================
	if err := tx.Model(&models.Ticket{}).
		Where("id IN (?)", ticketIDs).
		Updates(map[string]interface{}{
			"payment_status": "completed",
			"paid_at":        now,
			"updated_at":     now,
		}).Error; err != nil {
		tx.Rollback()
		errMsg := fmt.Sprintf("failed to update ticket payment status: %v", err)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	// ========================================
	// UPDATE TIER AVAILABILITY - CRITICAL: Decrease available count
	// ========================================
	// Group tickets by tier to update availability
	tierQuantities := make(map[uuid.UUID]int)
	for _, ticket := range tickets {
		tierQuantities[ticket.TierID]++
	}

	// Update each tier's available count
	for tierID, quantity := range tierQuantities {
		if err := tx.Model(&models.EventTier{}).
			Where("id = ? AND available >= ?", tierID, quantity).
			Update("available", gorm.Expr("available - ?", quantity)).Error; err != nil {
			tx.Rollback()
			log.Printf("WARN: Failed to update tier %s availability for %d tickets: %v\n", tierID, quantity, err)
			// Don't fail the entire transaction for availability update - log and continue
		}
		log.Printf("[TIER_AVAILABILITY_UPDATE] Updated tier %s: decreased available by %d\n", tierID, quantity)
	}

	// Update checkout session
	if err := tx.Model(&models.CheckoutSession{}).
		Where("checkout_token = ?", checkoutToken).
		Updates(map[string]interface{}{
			"status":     "completed",
			"updated_at": now,
		}).Error; err != nil {
		tx.Rollback()
		errMsg := fmt.Sprintf("failed to update checkout session: %v", err)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	// ========================================
	// IDEMPOTENCY CHECK: Prevent duplicate transaction creation
	// ========================================
	// Check if transaction already exists for this purchase (created by browser callback)
	// Use composite key: event_id + (user_id OR guest_user_id) + amount + payment_gateway + created_at within 5 minutes
	var existingTxn models.Transaction
	fiveMinutesAgo := now.Add(-5 * time.Minute)

	// Calculate total amount from tickets
	totalAmount := 0.0
	for _, ticket := range tickets {
		totalAmount += ticket.Tier.Price
	}

	// Build query based on user type
	query := tx.Where("event_id = ?", dbPaymentIntent.EventID).
		Where("payment_gateway = ?", models.PaymentGatewayStripe).
		Where("amount = ?", totalAmount).
		Where("created_at >= ?", fiveMinutesAgo)

	if dbPaymentIntent.UserID != nil {
		query = query.Where("user_id = ?", *dbPaymentIntent.UserID)
	} else if dbPaymentIntent.GuestUserID != nil {
		query = query.Where("guest_user_id = ?", *dbPaymentIntent.GuestUserID)
	}

	checkErr := query.First(&existingTxn).Error
	if checkErr == nil {
		// Transaction already exists - update it with PaymentIntent link and gateway info
		log.Printf("[IDEMPOTENCY] Transaction already exists for payment %s (created by browser callback): %s\n", paymentIntent.ID, existingTxn.ID)

		// Update existing transaction with payment_intent_id and gateway details
		updates := map[string]interface{}{
			"payment_intent_id": &dbPaymentIntent.ID,
			"gateway_txn_id":    paymentIntent.ID,
			"status":            "completed",
			"processed_at":      now,
		}
		if err := tx.Model(&existingTxn).Updates(updates).Error; err != nil {
			tx.Rollback()
			errMsg := fmt.Sprintf("failed to update existing transaction: %v", err)
			pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, &existingTxn.ID)
			return fmt.Errorf(errMsg)
		}

		tx.Commit()
		pw.updateWebhookEventStatus(ctx, webhookEventID, "succeeded", "", &dbPaymentIntent.ID, &existingTxn.ID)
		log.Printf("[PAYMENT_SUCCESS] Idempotent: Updated existing transaction %s with payment_intent_id: %s\n", existingTxn.ID, dbPaymentIntent.ID)
		return nil
	} else if checkErr != gorm.ErrRecordNotFound {
		// Database error
		tx.Rollback()
		errMsg := fmt.Sprintf("failed to check for existing transaction: %v", checkErr)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}
	// No existing transaction found - proceed with creation

	// ========================================
	// CREATE SINGLE TRANSACTION RECORD FOR ENTIRE PURCHASE
	// ========================================
	// Calculate totals across all tiers and tickets (reuse from idempotency check or recalculate to be safe)
	totalTickets := len(tickets)
	commissionAmount := totalAmount * (event.CommissionRate / 100)
	organizerShare := totalAmount - commissionAmount

	// Create a SINGLE transaction record for the entire purchase (all tiers combined)
	// TierID is NULL for multi-tier purchases, tickets are related via Tickets relationship
	transaction := models.Transaction{
		ID:               uuid.New(),
		EventID:          dbPaymentIntent.EventID,
		TierID:           nil, // NULL for multi-tier purchases (no single tier)
		UserID:           dbPaymentIntent.UserID,
		GuestUserID:      dbPaymentIntent.GuestUserID,
		PaymentIntentID:  &dbPaymentIntent.ID,
		PaymentGateway:   models.PaymentGatewayStripe,
		Amount:           totalAmount,
		Currency:         event.Tiers[0].Currency, // Use first tier's currency (all same event)
		Quantity:         totalTickets,            // Total tickets purchased
		Status:           "completed",
		GatewayTxnID:     paymentIntent.ID,
		CommissionRate:   event.CommissionRate,
		CommissionAmount: commissionAmount,
		OrganizerShare:   organizerShare,
		ProcessedAt:      &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := tx.Create(&transaction).Error; err != nil {
		tx.Rollback()
		errMsg := fmt.Sprintf("failed to create transaction record: %v", err)
		log.Printf("ERROR: %s\n", errMsg)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	// Link all tickets to this single transaction
	if err := tx.Model(&models.Ticket{}).
		Where("id IN ?", ticketIDs).
		Update("transaction_id", transaction.ID).Error; err != nil {
		tx.Rollback()
		errMsg := fmt.Sprintf("failed to link tickets to transaction: %v", err)
		log.Printf("ERROR: %s\n", errMsg)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	log.Printf("[TRANSACTION_CREATED] Single transaction for entire purchase: ID=%s, EventID=%s, TotalAmount=%.2f, TotalTickets=%d, Commission=%.2f, OrganizerShare=%.2f\n",
		transaction.ID, dbPaymentIntent.EventID, totalAmount, totalTickets, commissionAmount, organizerShare)

	// Commit the atomic transaction
	if err := tx.Commit().Error; err != nil {
		errMsg := fmt.Sprintf("failed to commit payment processing transaction: %v", err)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	log.Printf("[PAYMENT_SUCCESS] Atomic processing completed for payment %s\n", paymentIntent.ID)

	// ========================================
	// STORE COMPLETE RESPONSE FOR FRONTEND POLLING
	// ========================================
	// Generate ticket view token and create complete response for frontend polling
	if len(ticketIDs) > 0 {
		var firstTicket models.Ticket
		if err := pw.ticketService.GetDB().Where("id = ?", ticketIDs[0]).First(&firstTicket).Error; err != nil {
			log.Printf("[COMPLETE_RESPONSE] WARNING: Failed to load first ticket for token generation: %v\n", err)
		} else {
			// Generate JWT token for ticket viewing
			jwtService := utils.NewJWTService(&pw.cfg.JWT)
			ticketViewToken, err := jwtService.GenerateTicketAccessToken(&firstTicket)
			if err != nil {
				log.Printf("[COMPLETE_RESPONSE] WARNING: Failed to generate ticket view token: %v\n", err)
			} else {
				// Create ticket view URL
				ticketViewURL := fmt.Sprintf("%s/tickets/view?token=%s", pw.cfg.URLs.UserBaseURL, ticketViewToken)

				// Create complete response data
				completeResponse := map[string]interface{}{
					"success": true,
					"message": "Payment processed successfully",
					"data": map[string]interface{}{
						"checkout_token": checkoutToken,
						"payment_info": map[string]interface{}{
							"payment_intent_id": paymentIntent.ID,
							"amount":            paymentIntent.Amount,
							"currency":          paymentIntent.Currency,
							"status":            string(paymentIntent.Status),
							"receipt_email":     paymentIntent.ReceiptEmail,
						},
						"ticket_count":      totalTickets,
						"ticket_view_token": ticketViewToken,
						"ticket_view_url":   ticketViewURL,
					},
					"timestamp":  time.Now().Format(time.RFC3339),
					"request_id": requestID,
				}

				// Load and update checkout session with complete response
				var checkoutSession models.CheckoutSession
				if err := pw.ticketService.GetDB().Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
					log.Printf("[COMPLETE_RESPONSE] WARNING: Failed to load checkout session: %v\n", err)
				} else {
					if checkoutSession.GatewayData == nil {
						checkoutSession.GatewayData = make(map[string]interface{})
					}
					checkoutSession.GatewayData["complete_response"] = completeResponse

					if err := pw.ticketService.GetDB().Save(&checkoutSession).Error; err != nil {
						log.Printf("[COMPLETE_RESPONSE] WARNING: Failed to store complete response: %v\n", err)
					} else {
						log.Printf("[COMPLETE_RESPONSE] Stored complete response for checkout %s\n", checkoutToken)
					}
				}
			}
		}
	} else {
		log.Printf("[COMPLETE_RESPONSE] No tickets found, skipping complete response storage\n")
	}

	// ========================================
	// WEBHOOK EVENT TRACKING - Link payment intent and transaction
	// ========================================
	// Update webhook_events with payment_intent_id and transaction_id
	pw.updateWebhookEventStatus(ctx, webhookEventID, "succeeded", "", &dbPaymentIntent.ID, &transaction.ID)

	// ========================================
	// AUDIT LOGGING FOR PAYMENT SUCCESS
	// ========================================
	pw.logAuditAsync(ctx, "payment_succeeded", "transaction", transaction.ID, dbPaymentIntent.UserID, "user", &dbPaymentIntent.EventID, map[string]interface{}{
		"payment_gateway":        paymentIntent.ID,
		"charge_id":              chargeID,
		"payment_method_type":    paymentMethodType,
		"payment_method_details": paymentMethodDetails,
		"capture_method":         captureMethod,
		"receipt_email":          paymentIntent.ReceiptEmail,
		"total_amount":           totalAmount,
		"total_tickets":          totalTickets,
		"commission":             commissionAmount,
		"organizer_share":        organizerShare,
		"status":                 "succeeded",
		"processed_at":           now,
	})

	// ========================================
	// PHASE 5: OUTBOX EMAIL QUEUING
	// ========================================
	// NOTE: Email queuing is now handled in ProcessPaymentSuccess when tickets are marked active
	// This prevents duplicate emails and ensures consistent email delivery timing
	log.Printf("[PHASE_5_EMAIL] Skipping email phase - emails handled by ProcessPaymentSuccess\n")

	log.Printf("[PAYMENT_SUCCESS] Successfully processed payment intent: %s\n", paymentIntent.ID)
	return nil
}

// processPaymentIntentFailed processes failed payment
func (pw *PaymentWorker) processPaymentIntentFailed(ctx context.Context, paymentIntent *stripe.PaymentIntent, webhookEventID uuid.UUID, requestID string) error {
	// ========================================
	// LOG ALL STRIPE FAILURE DATA
	// ========================================
	log.Printf("[PAYMENT_FAILED] Processing failed payment intent: %s (request_id: %s)\n", paymentIntent.ID, requestID)
	log.Printf("[STRIPE_FAILURE] ===== FULL STRIPE PAYMENT INTENT FAILURE DATA =====")
	log.Printf("[STRIPE_FAILURE] PaymentIntent ID: %s", paymentIntent.ID)
	log.Printf("[STRIPE_FAILURE] Status: %s", paymentIntent.Status)
	log.Printf("[STRIPE_FAILURE] Amount: %d %s", paymentIntent.Amount, paymentIntent.Currency)
	log.Printf("[STRIPE_FAILURE] LastPaymentError: %v", paymentIntent.LastPaymentError)
	if paymentIntent.LastPaymentError != nil {
		log.Printf("[STRIPE_FAILURE] Error Code: %s", paymentIntent.LastPaymentError.Code)
		log.Printf("[STRIPE_FAILURE] Error Type: %s", paymentIntent.LastPaymentError.Type)
		log.Printf("[STRIPE_FAILURE] Error Param: %s", paymentIntent.LastPaymentError.Param)
	}
	log.Printf("[STRIPE_FAILURE] ===== END FAILURE DATA =====")

	checkoutToken, ok := paymentIntent.Metadata["checkout_token"]
	if !ok || checkoutToken == "" {
		return fmt.Errorf("checkout token not found in payment intent metadata")
	}

	tx := pw.ticketService.GetDB().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("ERROR: Panic during payment failure processing: %v\n", r)
		}
	}()

	var checkoutSession models.CheckoutSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("checkout_token = ?", checkoutToken).
		Preload("Ticket").
		First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("checkout session not found: %w", err)
	}

	// Get the event ID from the ticket
	ticket := checkoutSession.Ticket

	// Extract failure reason from Stripe for internal logging
	// SECURITY: Only pass generic reason codes to SSE, not detailed Stripe error info
	failureReason := "payment_failed"

	if paymentIntent.LastPaymentError != nil {
		// Log detailed Stripe error internally
		log.Printf("[STRIPE_ERROR_DETAILS] Code: %s, Type: %s, Param: %s",
			paymentIntent.LastPaymentError.Code,
			paymentIntent.LastPaymentError.Type,
			paymentIntent.LastPaymentError.Param)
	}

	// Update checkout session
	// SECURITY: Store detailed Stripe error internally for debugging, not exposed to client
	checkoutSession.Status = "failed"
	updates := map[string]interface{}{
		"payment_intent_id": paymentIntent.ID,
		"failure_reason":    failureReason,
		"failed_at":         time.Now(),
		"stripe_status":     string(paymentIntent.Status),
	}

	if checkoutSession.GatewayData == nil {
		checkoutSession.GatewayData = make(map[string]interface{})
	}
	for k, v := range updates {
		checkoutSession.GatewayData[k] = v
	}

	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// Process the failed payment (release tickets, send email, etc.)
	if err := pw.ticketService.ProcessFailedPayment(checkoutToken); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to process payment failure: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("[PAYMENT_FAILED] Payment failure processing completed for token: %s (reason: %s)\n", checkoutToken, failureReason)

	// ========================================
	// AUDIT LOGGING FOR PAYMENT FAILURE
	// ========================================
	pw.logAuditAsync(ctx, "payment_failed", "checkout_session", checkoutSession.ID, checkoutSession.UserID, "user", &ticket.EventID, map[string]interface{}{
		"payment_gateway": paymentIntent.ID,
		"failure_reason":  failureReason,
	})

	return nil
}

// processPaymentIntentCanceled processes canceled payment
func (pw *PaymentWorker) processPaymentIntentCanceled(ctx context.Context, paymentIntent *stripe.PaymentIntent, webhookEventID uuid.UUID, requestID string) error {
	checkoutToken, ok := paymentIntent.Metadata["checkout_token"]
	if !ok || checkoutToken == "" {
		return fmt.Errorf("checkout token not found in payment intent metadata")
	}

	tx := pw.ticketService.GetDB().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("ERROR: Panic during payment cancel processing: %v\n", r)
		}
	}()

	var checkoutSession models.CheckoutSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("checkout_token = ?", checkoutToken).
		Preload("Ticket").
		First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("checkout session not found: %w", err)
	}

	// Get the event ID from the ticket
	ticket := checkoutSession.Ticket

	// Update checkout session
	checkoutSession.Status = "canceled"
	updates := map[string]interface{}{
		"payment_intent_id": paymentIntent.ID,
		"failure_reason":    "payment_canceled",
		"canceled_at":       time.Now(),
	}
	if checkoutSession.GatewayData == nil {
		checkoutSession.GatewayData = make(map[string]interface{})
	}
	for k, v := range updates {
		checkoutSession.GatewayData[k] = v
	}

	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// Process the canceled payment
	if err := pw.ticketService.ProcessFailedPayment(checkoutToken); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to process payment cancel: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Payment cancel processing completed for token: %s\n", checkoutToken)

	// ========================================
	// AUDIT LOGGING FOR PAYMENT CANCELLATION
	// ========================================
	pw.logAuditAsync(ctx, "payment_canceled", "checkout_session", checkoutSession.ID, checkoutSession.UserID, "user", &ticket.EventID, map[string]interface{}{
		"payment_gateway":     paymentIntent.ID,
		"cancellation_reason": "user_canceled_or_timeout",
	})

	return nil
}

// processChargeRefunded processes a refunded charge from Stripe webhook
// This handles refunds of payments that were previously charged
func (pw *PaymentWorker) processChargeRefunded(ctx context.Context, charge *stripe.Charge, webhookEventID uuid.UUID, requestID string) error {
	// ========================================
	// LOG ALL STRIPE REFUND DATA
	// ========================================
	log.Printf("[CHARGE_REFUNDED] Processing refunded charge: %s (request_id: %s)\n", charge.ID, requestID)
	log.Printf("[STRIPE_REFUND] ===== FULL STRIPE CHARGE REFUND DATA =====")
	log.Printf("[STRIPE_REFUND] Charge ID: %s", charge.ID)
	log.Printf("[STRIPE_REFUND] Amount: %d %s", charge.Amount, charge.Currency)
	log.Printf("[STRIPE_REFUND] Amount Refunded: %d", charge.AmountRefunded)
	log.Printf("[STRIPE_REFUND] Refunded: %v", charge.Refunded)
	log.Printf("[STRIPE_REFUND] Payment Intent: %v", charge.PaymentIntent)
	log.Printf("[STRIPE_REFUND] ===== END REFUND DATA =====")

	// Get payment intent ID from charge
	var paymentIntentID string
	if charge.PaymentIntent != nil {
		paymentIntentID = charge.PaymentIntent.ID
	}

	if paymentIntentID == "" {
		return fmt.Errorf("payment intent not found in charge")
	}

	tx := pw.ticketService.GetDB().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("ERROR: Panic during charge refund processing: %v\n", r)
		}
	}()

	// Find checkout session by payment intent ID
	var checkoutSession models.CheckoutSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("gateway_data->>'payment_intent_id' = ?", paymentIntentID).
		Preload("Ticket").
		First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("checkout session not found for payment intent %s: %w", paymentIntentID, err)
	}

	// Get the event ID from the ticket
	ticket := checkoutSession.Ticket

	// Update checkout session status to refunded
	checkoutSession.Status = "refunded"
	updates := map[string]interface{}{
		"refunded_at":      time.Now(),
		"refund_amount":    charge.AmountRefunded,
		"refund_reason":    "charge_refunded",
		"stripe_charge_id": charge.ID,
	}

	if checkoutSession.GatewayData == nil {
		checkoutSession.GatewayData = make(map[string]interface{})
	}
	for k, v := range updates {
		checkoutSession.GatewayData[k] = v
	}

	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// Process the refunded payment (cancel tickets + update transaction)
	if err := pw.ticketService.ProcessRefundedPayment(checkoutSession.CheckoutToken); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to process refunded payment: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("[CHARGE_REFUNDED] Charge refund processing completed for charge: %s (amount: %d)\n", charge.ID, charge.AmountRefunded)

	// ========================================
	// AUDIT LOGGING FOR REFUND
	// ========================================
	pw.logAuditAsync(ctx, "charge_refunded", "checkout_session", checkoutSession.ID, checkoutSession.UserID, "user", &ticket.EventID, map[string]interface{}{
		"charge_id":      charge.ID,
		"refund_amount":  charge.AmountRefunded,
		"payment_intent": paymentIntentID,
	})

	return nil
}

// logAuditAsync creates audit log entries asynchronously
func (pw *PaymentWorker) logAuditAsync(ctx context.Context, action, entityType string, entityID uuid.UUID, actorID *uuid.UUID, actorType string, eventID *uuid.UUID, changes map[string]interface{}) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PAYMENT_WORKER] Panic in async audit logging: %v\n", r)
			}
		}()

		audit := &models.PaymentAuditLog{
			Action:       action,
			EntityType:   entityType,
			EntityID:     entityID,
			ActorID:      actorID,
			ActorType:    actorType,
			EventID:      eventID,
			ChangesAfter: changes,
			Timestamp:    time.Now(),
		}

		if err := pw.ticketService.GetDB().Create(audit).Error; err != nil {
			log.Printf("[PAYMENT_WORKER] Failed to create audit log: %v\n", err)
		}
	}()
}

// updateWebhookEventStatus updates the webhook event processing status in the database
// Also links payment_intent_id and transaction_id when available
func (pw *PaymentWorker) updateWebhookEventStatus(ctx context.Context, webhookEventID uuid.UUID, status, errMsg string, paymentIntentID, transactionID *uuid.UUID) {
	db := pw.ticketService.GetDB()
	now := time.Now()

	updates := map[string]interface{}{
		"status":     status,
		"updated_at": now,
	}

	if status == "succeeded" {
		updates["processed_at"] = now
		// Increment ProcessedCount
		updates["processed_count"] = gorm.Expr("processed_count + 1")
	}

	if errMsg != "" {
		updates["last_error"] = errMsg
	}

	// Link payment intent and transaction to webhook event
	if paymentIntentID != nil {
		updates["payment_intent_id"] = paymentIntentID
	}
	if transactionID != nil {
		updates["transaction_id"] = transactionID
	}

	if err := db.Model(&models.WebhookEvent{}).
		Where("id = ?", webhookEventID).
		Updates(updates).Error; err != nil {
		log.Printf("[ERROR] Failed to update webhook event status: %v\n", err)
	} else {
		log.Printf("[WEBHOOK_STATUS] Updated webhook event %s to status: %s (payment_intent_id: %v, transaction_id: %v)\n", webhookEventID, status, paymentIntentID, transactionID)
	}
}

// getBaseURL returns the base URL for the application from config
func (pw *PaymentWorker) getBaseURL() string {
	if pw.cfg != nil {
		return pw.cfg.URLs.FrontendBaseURL
	}
	return "https://user.timroticket.com"
}

// IsHealthy returns true if the payment worker is running and responsive
func (pw *PaymentWorker) IsHealthy() bool {
	if !pw.isRunning {
		return false
	}

	// Check if heartbeat is recent (within 30 seconds)
	lastActivity := time.Since(pw.lastHeartbeat)
	if lastActivity > 30*time.Second {
		log.Printf("[HEALTH_CHECK] Payment worker may be stuck (last activity: %v ago)\n", lastActivity)
		return false
	}

	return true
}

// GetWorkerStatus returns detailed status information about the payment worker
func (pw *PaymentWorker) GetWorkerStatus() map[string]interface{} {
	return map[string]interface{}{
		"is_running":           pw.isRunning,
		"last_heartbeat":       pw.lastHeartbeat,
		"time_since_heartbeat": time.Since(pw.lastHeartbeat).String(),
		"processing_tasks":     pw.processingTasksCount,
		"is_healthy":           pw.IsHealthy(),
	}
}

// UpdateHeartbeat updates the last activity timestamp (called after task processing)
func (pw *PaymentWorker) UpdateHeartbeat() {
	pw.lastHeartbeat = time.Now()
}

// Close closes the payment worker and client
func (pw *PaymentWorker) Close() error {
	pw.isRunning = false
	if pw.client != nil {
		pw.client.Close()
	}
	if pw.server != nil {
		pw.server.Stop()
	}
	return nil
}
