package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

// getStringFromGatewayData safely extracts string values from gateway data map
func getStringFromGatewayData(gatewayData map[string]interface{}, key string) string {
	if gatewayData == nil {
		return ""
	}
	if value, ok := gatewayData[key]; ok && value != nil {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return ""
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
	log.Println("[PAYMENT_WORKER] ============================================")
	log.Println("[PAYMENT_WORKER] ✅ STARTING PAYMENT WORKER (asynq server)...")
	log.Printf("[PAYMENT_WORKER] Redis: %s\n", pw.queueConfig.RedisAddr)
	log.Printf("[PAYMENT_WORKER] Concurrency: %d | StrictPriority: %v\n",
		pw.queueConfig.Concurrency,
		pw.queueConfig.StrictPriority)
	log.Println("[PAYMENT_WORKER]   • TypeChargeRefunded (charge:refunded)")
	log.Println("[PAYMENT_WORKER] ============================================")

	// Start the server - this blocks until it's shut down
	err := pw.server.Start(pw.mux)

	// If we get here, server stopped (either error or graceful shutdown)
	pw.isRunning = false
	if err != nil {
		log.Printf("[PAYMENT_WORKER] ❌ PAYMENT WORKER STOPPED WITH ERROR: %v\n", err)
		return fmt.Errorf("payment worker error: %w", err)
	}

	log.Println("[PAYMENT_WORKER] ✓ Payment worker stopped gracefully")
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
		log.Printf("[PAYMENT_WORKER] ❌ ERROR: Failed to unmarshal payload: %v\n", err)
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	log.Printf("[PAYMENT_WORKER] ============================================")
	log.Printf("[PAYMENT_WORKER] 🔄 PROCESSING PAYMENT SUCCESS")
	log.Printf("[PAYMENT_WORKER] EventID: %s", payload.StripeEventID)
	log.Printf("[PAYMENT_WORKER] WebhookID: %s", payload.WebhookEventID)
	log.Printf("[PAYMENT_WORKER] EventType: %s", payload.EventType)
	log.Printf("[PAYMENT_WORKER] ============================================")

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
		log.Printf("[PAYMENT_WORKER] ❌ ERROR: Failed to process payment success: %v\n", err)
		pw.updateWebhookEventStatus(ctx, payload.WebhookEventID, "failed", err.Error(), nil, nil)
		return fmt.Errorf("payment processing failed: %w", err)
	}

	// Update heartbeat for health monitoring
	pw.UpdateHeartbeat()

	log.Printf("[PAYMENT_WORKER] ✅ PAYMENT SUCCESS PROCESSED SUCCESSFULLY!")
	log.Printf("[PAYMENT_WORKER] EventID: %s | WebhookID: %s\n", payload.StripeEventID, payload.WebhookEventID)
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
	// PHASE 2: ATOMIC PROCESSING - RESERVATION CONFIRMATION, TICKET CREATION, TRANSACTION CREATION
	// ========================================
	// ALL OPERATIONS IN SINGLE ATOMIC TRANSACTION: Confirm reservations → Create tickets → Create transaction → Update inventory
	// If ANY step fails, rollback ALL changes

	tx := pw.ticketService.GetDB().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("ERROR: Panic during atomic payment processing: %v\n", r)
		}
	}()

	// STEP 2A: Confirm reservations and create tickets atomically
	ticketIDs, err := pw.confirmReservationsAndCreateTicketsAtomic(ctx, tx, checkoutToken, paymentIntent.ID, dbPaymentIntent.EventID)
	if err != nil {
		tx.Rollback()
		errMsg := fmt.Sprintf("failed to confirm reservations and create tickets: %v", err)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	if len(ticketIDs) == 0 {
		tx.Rollback()
		errMsg := "no tickets created from reservations"
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	log.Printf("[TICKETS_CREATED] Created %d tickets from reservations for checkout token %s\n", len(ticketIDs), checkoutToken)

	// STEP 2B: Get created tickets for transaction recording
	var createdTickets []*models.Ticket
	if err := tx.Where("id IN ?", ticketIDs).Preload("Tier").Find(&createdTickets).Error; err != nil {
		tx.Rollback()
		errMsg := fmt.Sprintf("failed to find created tickets: %v", err)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	// STEP 2C: Create transaction record
	totalAmount := 0.0
	for _, ticket := range createdTickets {
		totalAmount += ticket.TotalAmount
	}

	// Load event for commission calculation
	var event models.Event
	if err := tx.Preload("Tiers").Where("id = ?", dbPaymentIntent.EventID).First(&event).Error; err != nil {
		tx.Rollback()
		errMsg := fmt.Sprintf("failed to load event: %v", err)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	commissionAmount := totalAmount * (event.CommissionRate / 100)
	organizerShare := totalAmount - commissionAmount

	now := time.Now()
	transaction := models.Transaction{
		ID:               uuid.New(),
		EventID:          dbPaymentIntent.EventID,
		TierID:           nil, // NULL for multi-tier purchases
		UserID:           dbPaymentIntent.UserID,
		GuestUserID:      dbPaymentIntent.GuestUserID,
		PaymentIntentID:  &dbPaymentIntent.ID,
		PaymentGateway:   models.PaymentGatewayStripe,
		Amount:           totalAmount,
		Currency:         event.Tiers[0].Currency,
		Quantity:         len(createdTickets),
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
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	// Link all tickets to this transaction
	if err := tx.Model(&models.Ticket{}).
		Where("id IN ?", ticketIDs).
		Update("transaction_id", transaction.ID).Error; err != nil {
		tx.Rollback()
		errMsg := fmt.Sprintf("failed to link tickets to transaction: %v", err)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	log.Printf("[TRANSACTION_CREATED] ID=%s, Amount=%.2f, Tickets=%d, Commission=%.2f\n",
		transaction.ID, totalAmount, len(createdTickets), commissionAmount)

	// STEP 2D: Update payment intent status and gateway data
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

	paymentUpdate := map[string]interface{}{
		"status":                 "succeeded",
		"succeeded_at":           now,
		"updated_at":             now,
		"gateway_response":       gatewayResponse,
		"gateway_charge_id":      chargeID,
		"gateway_payment_id":     paymentIntent.ID,
		"payment_method_type":    paymentMethodType,
		"payment_method_details": paymentMethodDetails,
		"capture_method":         captureMethod,
	}

	if err := tx.Model(&models.PaymentIntent{}).
		Where("id = ?", dbPaymentIntent.ID).
		Updates(paymentUpdate).Error; err != nil {
		tx.Rollback()
		errMsg := fmt.Sprintf("failed to update payment intent: %v", err)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	// STEP 2E: Update checkout session status
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

	// COMMIT THE ATOMIC TRANSACTION
	if err := tx.Commit().Error; err != nil {
		errMsg := fmt.Sprintf("failed to commit atomic payment processing: %v", err)
		pw.updateWebhookEventStatus(ctx, webhookEventID, "failed", errMsg, &dbPaymentIntent.ID, nil)
		return fmt.Errorf(errMsg)
	}

	log.Printf("[ATOMIC_SUCCESS] All operations completed atomically for payment %s\n", paymentIntent.ID)

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

				// Load checkout session to get additional payment info
				var checkoutSession models.CheckoutSession
				if err := pw.ticketService.GetDB().Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
					log.Printf("[COMPLETE_RESPONSE] WARNING: Failed to load checkout session for payment info: %v\n", err)
					checkoutSession = models.CheckoutSession{} // Use empty struct as fallback
				}

				// Create complete response data (just the data portion, not the full response)
				completeResponseData := map[string]interface{}{
					"success": true,
					"message": "Tickets generated successfully",
					"status":  "completed",
					"ticket": map[string]interface{}{
						"count": len(ticketIDs),
						"token": ticketViewToken,
						"url":   ticketViewURL,
					},
				}

				// Load and update checkout session with complete response
				if checkoutSession.GatewayData == nil {
					checkoutSession.GatewayData = make(map[string]interface{})
				}
				checkoutSession.GatewayData["complete_response"] = completeResponseData

				if err := pw.ticketService.GetDB().Save(&checkoutSession).Error; err != nil {
					log.Printf("[COMPLETE_RESPONSE] WARNING: Failed to store complete response: %v\n", err)
				} else {
					log.Printf("[COMPLETE_RESPONSE] Stored complete response for checkout %s\n", checkoutToken)
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
		"total_tickets":          len(ticketIDs),
		"commission":             commissionAmount,
		"organizer_share":        organizerShare,
		"status":                 "succeeded",
		"processed_at":           now,
	})

	// ========================================
	// PHASE 5: OUTBOX EMAIL QUEUING
	// ========================================
	// Queue ticket confirmation emails for both guest and user purchases
	if len(ticketIDs) > 0 {
		// Get the first ticket to determine purchase type and get event details
		var firstTicket models.Ticket
		if err := pw.ticketService.GetDB().Where("id = ?", ticketIDs[0]).Preload("Event").Preload("Event.Organizer").Preload("Event.Organizer.OrganizerOnboarding").First(&firstTicket).Error; err != nil {
			log.Printf("[EMAIL_QUEUE_ERROR] Failed to load ticket for email: %v\n", err)
		} else {
			// Determine recipient email and name using CustomerEmail as single source of truth
			var recipientEmail, recipientName string
			var isGuestPurchase bool

			// Use CustomerEmail as the single source of truth (per EMAIL_NOT_FIRING_FIX)
			recipientEmail = dbPaymentIntent.CustomerEmail
			isGuestPurchase = dbPaymentIntent.GuestUserID != nil

			// Get recipient name from appropriate source
			if dbPaymentIntent.GuestUserID != nil {
				// Guest purchase - get name from guest user
				var guestUser models.GuestUser
				if err := pw.ticketService.GetDB().Where("id = ?", *dbPaymentIntent.GuestUserID).First(&guestUser).Error; err != nil {
					log.Printf("[EMAIL_QUEUE_ERROR] Failed to load guest user for name: %v\n", err)
					recipientName = "Guest User" // Fallback
				} else {
					recipientName = guestUser.FirstName + " " + guestUser.LastName
				}
			} else if dbPaymentIntent.UserID != nil {
				// User purchase - get name from user
				var user models.User
				if err := pw.ticketService.GetDB().Where("id = ?", *dbPaymentIntent.UserID).First(&user).Error; err != nil {
					log.Printf("[EMAIL_QUEUE_ERROR] Failed to load user for name: %v\n", err)
					recipientName = "User" // Fallback
				} else {
					recipientName = user.FirstName + " " + user.LastName
				}
			}

			if recipientEmail != "" {
				// Load all tickets for this transaction
				var allTickets []models.Ticket
				if err := pw.ticketService.GetDB().Where("transaction_id = ?", transaction.ID).Find(&allTickets).Error; err != nil {
					log.Printf("[EMAIL_QUEUE_ERROR] Failed to load tickets for email: %v\n", err)
				} else {
					// Generate JWT tokens and prepare ticket data
					var ticketData []map[string]interface{}
					for _, ticket := range allTickets {
						// Generate secure view URL
						jwtService := utils.NewJWTService(&pw.cfg.JWT)
						ticketViewToken, err := jwtService.GenerateTicketAccessToken(&ticket)
						if err != nil {
							log.Printf("[EMAIL_QUEUE_ERROR] Failed to generate ticket token for %s: %v\n", ticket.ID, err)
							continue
						}

						ticketViewURL := fmt.Sprintf("%s/tickets/view?token=%s", pw.cfg.URLs.UserBaseURL, ticketViewToken)

						ticketData = append(ticketData, map[string]interface{}{
							"ticket_number": ticket.TicketNumber,
							"view_url":      ticketViewURL,
						})
					}

					// Generate calendar data
					calendarEvent := utils.ICalendarEvent{
						UID:         firstTicket.Event.ID.String(),
						Summary:     firstTicket.Event.Title,
						Description: utils.FormatEventDescription(firstTicket.Event.Title, firstTicket.TicketNumber, "", len(allTickets)),
						Location:    fmt.Sprintf("%s, %s", firstTicket.Event.VenueName, firstTicket.Event.Address),
						StartTime:   firstTicket.Event.StartDate,
						EndTime:     firstTicket.Event.EndDate,
						Organizer:   firstTicket.Event.Organizer.FirstName + " " + firstTicket.Event.Organizer.LastName,
						URL:         fmt.Sprintf("%s/events/%s", pw.cfg.URLs.UserBaseURL, firstTicket.Event.ID),
					}

					icsContent := utils.GenerateICS(calendarEvent)
					icsDataURL := utils.GenerateAddToCalendarURL(icsContent)
					googleCalURL := utils.GenerateGoogleCalendarURL(calendarEvent)
					calendarFilename := utils.GetCalendarFilename(firstTicket.Event.Title)

					// Prepare email template data
					templateData := map[string]interface{}{
						"recipient_name":      recipientName,
						"recipient_email":     recipientEmail,
						"event_name":          firstTicket.Event.Title,
						"event_date":          firstTicket.Event.StartDate.Format("January 2, 2006"),
						"event_time":          firstTicket.Event.StartDate.Format("3:04 PM"),
						"venue":               firstTicket.Event.VenueName,
						"organizer_name":      firstTicket.Event.Organizer.FirstName + " " + firstTicket.Event.Organizer.LastName,
						"tickets":             ticketData,
						"total_tickets":       len(allTickets),
						"total_amount":        totalAmount,
						"payment_gateway":     "Stripe",
						"base_url":            pw.cfg.URLs.UserBaseURL,
						"calendar_ics_url":    icsDataURL,
						"google_calendar_url": googleCalURL,
						"calendar_filename":   calendarFilename,
						"year":                time.Now().Year(),
						"is_guest":            isGuestPurchase,
					}

					// Queue the email with high priority (immediate)
					subject := fmt.Sprintf("Your Tickets for %s", firstTicket.Event.Title)
					if err := pw.emailOutboxService.QueueEmail(ctx, models.EmailEventTicketConfirmation, recipientEmail, subject, templateData, 2); err != nil {
						log.Printf("[EMAIL_QUEUE_ERROR] Failed to queue ticket confirmation email: %v\n", err)
					} else {
						log.Printf("[EMAIL_QUEUE_SUCCESS] Queued ticket confirmation email to %s for %d tickets\n", recipientEmail, len(allTickets))
					}
				}
			}
		}
	}

	log.Printf("[PAYMENT_SUCCESS] Successfully processed payment intent: %s\n", paymentIntent.ID)
	return nil
}

func (pw *PaymentWorker) confirmReservationsAndCreateTicketsAtomic(ctx context.Context, tx *gorm.DB, checkoutToken string, paymentIntentID string, eventID uuid.UUID) ([]uuid.UUID, error) {
	// 1. Find all reservations for this checkout token
	var reservations []models.TicketReservation
	if err := tx.Where("checkout_token = ?", checkoutToken).
		Preload("Tier").Preload("Event").
		Find(&reservations).Error; err != nil {
		return nil, fmt.Errorf("failed to find reservations: %w", err)
	}

	if len(reservations) == 0 {
		return nil, fmt.Errorf("no reservations found for token: %s", checkoutToken)
	}

	// 2. IDEMPOTENCY CHECK: If all reservations are already confirmed, return existing ticket IDs
	allConfirmed := true
	var existingTicketIDs []uuid.UUID
	for _, r := range reservations {
		if r.Status != models.ReservationStatusConfirmed {
			allConfirmed = false
			break
		}
	}
	if allConfirmed {
		log.Printf("[IDEMPOTENT] Reservation already confirmed for token: %s", checkoutToken)
		// Find existing tickets for this reservation set
		var existingTickets []models.Ticket
		reservationIDs := make([]uuid.UUID, len(reservations))
		for i, r := range reservations {
			reservationIDs[i] = r.ID
		}
		if err := tx.Where("reservation_id IN ?", reservationIDs).Find(&existingTickets).Error; err != nil {
			return nil, fmt.Errorf("failed to find existing tickets for idempotent case: %w", err)
		}
		for _, ticket := range existingTickets {
			existingTicketIDs = append(existingTicketIDs, ticket.ID)
		}
		return existingTicketIDs, nil
	}

	// 3. Check if any reservations have expired
	now := time.Now()
	for _, reservation := range reservations {
		if reservation.Status == models.ReservationStatusReserved && reservation.ExpiresAt.Before(now) {
			return nil, fmt.Errorf("reservation expired for tier %s", reservation.TierID)
		}
	}

	// 4. Create tickets and update inventory atomically
	var ticketIDs []uuid.UUID
	totalQuantityByTier := make(map[uuid.UUID]int)

	for _, reservation := range reservations {
		// Skip already-confirmed reservations (idempotency)
		if reservation.Status == models.ReservationStatusConfirmed {
			log.Printf("[IDEMPOTENT] Skipping already-confirmed reservation: tier=%s, quantity=%d", reservation.TierID, reservation.Quantity)
			continue
		}

		if reservation.Status != models.ReservationStatusReserved {
			log.Printf("[WARN] Skipping reservation with unexpected status: %s", reservation.Status)
			continue
		}

		// Create tickets for this reservation
		for i := 0; i < reservation.Quantity; i++ {
			// Generate unique ticket number
			ticketNumber, err := utils.GenerateEventTicketNumber(tx, reservation.Tier.TierName, reservation.Event.StartDate.Year())
			if err != nil {
				return nil, fmt.Errorf("failed to generate ticket number: %w", err)
			}

			ticket := &models.Ticket{
				ID:              uuid.New(),
				TicketNumber:    ticketNumber,
				EventID:         reservation.EventID,
				TierID:          reservation.TierID,
				UserID:          reservation.UserID,
				GuestUserID:     reservation.GuestUserID,
				TotalAmount:     reservation.Tier.Price,
				PaymentGateway:  models.PaymentGatewayStripe,
				Status:          "active",
				PaymentStatus:   "completed",
				IsGuestPurchase: reservation.GuestUserID != nil,
				PaidAt:          &now,
				CreatedAt:       now,
				UpdatedAt:       now,
			}

			if err := tx.Create(ticket).Error; err != nil {
				return nil, fmt.Errorf("failed to create ticket: %w", err)
			}

			ticketIDs = append(ticketIDs, ticket.ID)
			totalQuantityByTier[reservation.TierID]++
			log.Printf("[TICKET_CREATED] id=%s, tier=%s, number=%s\n", ticket.ID, reservation.Tier.TierName, ticket.TicketNumber)

			// Audit log for ticket creation
			pw.logAuditAsync(ctx, "ticket_created", "ticket", ticket.ID, reservation.UserID, "user", &reservation.EventID, map[string]interface{}{
				"ticket_number":     ticket.TicketNumber,
				"tier_name":         reservation.Tier.TierName,
				"total_amount":      ticket.TotalAmount,
				"payment_gateway":   ticket.PaymentGateway,
				"is_guest_purchase": ticket.IsGuestPurchase,
				"status":            ticket.Status,
				"payment_status":    ticket.PaymentStatus,
			})
		}

		// Mark reservation as confirmed
		if err := tx.Model(&reservation).Update("status", models.ReservationStatusConfirmed).Error; err != nil {
			return nil, fmt.Errorf("failed to update reservation status: %w", err)
		}
	}

	// 5. Update tier inventory (decrease available count)
	for tierID, quantity := range totalQuantityByTier {
		if err := tx.Model(&models.EventTier{}).
			Where("id = ? AND available >= ?", tierID, quantity).
			Update("available", gorm.Expr("available - ?", quantity)).Error; err != nil {
			return nil, fmt.Errorf("failed to update tier %s inventory: %w", tierID, err)
		}
		log.Printf("[INVENTORY_UPDATED] Tier %s: decreased available by %d\n", tierID, quantity)
	}

	log.Printf("[ATOMIC_SUCCESS] Confirmed %d reservations, created %d tickets, updated %d tiers\n",
		len(reservations), len(ticketIDs), len(totalQuantityByTier))

	return ticketIDs, nil
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
	checkoutSessionFound := true
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Joins("JOIN payment_intents pi ON checkout_sessions.payment_intent_id = pi.id").
		Where("pi.gateway_payment_id = ?", paymentIntentID).
		Preload("Ticket").
		First(&checkoutSession).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Checkout session not found - this might be an admin-initiated refund
			// Continue processing refunds but skip checkout session updates
			log.Printf("[CHARGE_REFUNDED] No checkout session found for payment intent %s - processing refunds only\n", paymentIntentID)
			checkoutSessionFound = false
		} else {
			tx.Rollback()
			return fmt.Errorf("error finding checkout session for payment intent %s: %w", paymentIntentID, err)
		}
	}

	// Update checkout session if found
	if checkoutSessionFound {
		// For checkout sessions, we only update gateway data to track the refund
		// We don't automatically mark as "refunded" or cancel tickets here
		// That logic is handled by processRefundWebhookConfirmation based on refund type
		if checkoutSession.GatewayData == nil {
			checkoutSession.GatewayData = make(map[string]interface{})
		}

		// Add refund tracking data without changing status
		if checkoutSession.GatewayData["refunds"] == nil {
			checkoutSession.GatewayData["refunds"] = make([]map[string]interface{}, 0)
		}

		refundData := map[string]interface{}{
			"stripe_charge_id": charge.ID,
			"refund_amount":    charge.AmountRefunded,
			"refunded_at":      time.Now(),
			"processed_via":    "webhook",
		}

		// Append to refunds array
		refunds := checkoutSession.GatewayData["refunds"].([]map[string]interface{})
		refunds = append(refunds, refundData)
		checkoutSession.GatewayData["refunds"] = refunds

		if err := tx.Save(&checkoutSession).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update checkout session gateway data: %w", err)
		}

		// NOTE: We do NOT call ProcessRefundedPayment here anymore
		// Individual refund processing is handled by processRefundWebhookConfirmation
		// which knows whether it's a full refund (cancel all tickets) or partial refund (cancel specific tickets)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// ========================================
	// CHECK FOR PENDING REFUNDS WAITING FOR WEBHOOK CONFIRMATION
	// ========================================
	if err := pw.processRefundWebhookConfirmation(ctx, charge, paymentIntentID); err != nil {
		log.Printf("[CHARGE_REFUNDED] Warning: Failed to process refund webhook confirmation: %v\n", err)
		// Don't fail the entire webhook processing for this
	}

	log.Printf("[CHARGE_REFUNDED] Charge refund processing completed for charge: %s (amount: %d)\n", charge.ID, charge.AmountRefunded)

	return nil
}

// processRefundWebhookConfirmation checks for and updates refunds waiting for webhook confirmation
func (pw *PaymentWorker) processRefundWebhookConfirmation(ctx context.Context, charge *stripe.Charge, paymentIntentID string) error {
	// Find refunds that are processing and awaiting webhook confirmation for this charge
	var refunds []models.Refund
	if err := pw.ticketService.GetDB().
		Where("payment_intent_id IN (SELECT id FROM payment_intents WHERE gateway_charge_id = ?)", charge.ID).
		Where("status = ?", "processing").
		Where("gateway_response->>'awaiting_webhook' = 'true'").
		Preload("PaymentIntent").
		Find(&refunds).Error; err != nil {
		return fmt.Errorf("failed to find pending refunds: %w", err)
	}

	if len(refunds) == 0 {
		log.Printf("[REFUND_WEBHOOK] No refunds found awaiting webhook confirmation for charge: %s\n", charge.ID)
		return nil
	}

	for _, refund := range refunds {
		// Update refund status to succeeded since we received the charge.refunded webhook
		now := time.Now()
		gatewayData := refund.GatewayResponse
		if gatewayData == nil {
			gatewayData = make(map[string]interface{})
		}
		gatewayData["webhook_confirmed_at"] = now
		gatewayData["webhook_confirmed"] = true
		gatewayData["awaiting_webhook"] = false

		// Calculate commission and organizer refund amounts
		var commissionRefund, organizerRefund float64
		if refund.PaymentIntent != nil && refund.PaymentIntent.EventID != uuid.Nil {
			// Get event commission rate
			var event models.Event
			if err := pw.ticketService.GetDB().Select("commission_rate").First(&event, refund.PaymentIntent.EventID).Error; err == nil {
				commissionRate := event.CommissionRate / 100.0
				commissionRefund = refund.Amount * commissionRate
				organizerRefund = refund.Amount - commissionRefund
			}
		}

		updates := map[string]interface{}{
			"status":            "succeeded",
			"processed_at":      &now,
			"gateway_response":  gatewayData,
			"commission_refund": commissionRefund,
			"organizer_refund":  organizerRefund,
		}

		if err := pw.ticketService.GetDB().Model(&refund).Updates(updates).Error; err != nil {
			log.Printf("[REFUND_WEBHOOK] Failed to update refund %s status: %v\n", refund.ID, err)
			continue
		}

		// Log status change to refund_status_history table
		statusHistory := &models.RefundStatusHistory{
			RefundID:      refund.ID,
			OldStatus:     "processing",
			NewStatus:     "succeeded",
			ChangedByType: "system",
			Remarks:       "Refund confirmed via webhook",
			Metadata: map[string]interface{}{
				"webhook_event_id": charge.ID,
				"request_id":       paymentIntentID,
				"source":           "webhook_confirmation",
			},
			ChangedAt: now,
		}

		if err := pw.ticketService.GetDB().Create(statusHistory).Error; err != nil {
			log.Printf("[REFUND_WEBHOOK] Failed to log status change for refund %s: %v\n", refund.ID, err)
		}

		// Send success notification (simplified - just log for now)
		log.Printf("[REFUND_WEBHOOK] ✅ Refund %s completed successfully via webhook confirmation\n", refund.ID)

		// Process ticket refund: update ONLY the affected ticket statuses and restore inventory
		// For full transaction refunds, also mark transaction and checkout session as refunded
		if refund.IsFullTransactionRefund {
			if err := pw.processFullTransactionRefund(&refund); err != nil {
				log.Printf("[REFUND_WEBHOOK] Warning: Failed to process full transaction refund for refund %s: %v\n", refund.ID, err)
			} else {
				log.Printf("[REFUND_WEBHOOK] ✅ Full transaction refund completed for refund %s\n", refund.ID)
			}
		} else {
			if err := pw.processPartialTicketRefund(&refund); err != nil {
				log.Printf("[REFUND_WEBHOOK] Warning: Failed to process partial ticket refund for refund %s: %v\n", refund.ID, err)
			} else {
				log.Printf("[REFUND_WEBHOOK] ✅ Affected tickets refunded and inventory restored for refund %s\n", refund.ID)
			}
		}

		// Audit log
		var eventID *uuid.UUID
		if refund.PaymentIntent != nil {
			eventID = &refund.PaymentIntent.EventID
		}
		pw.logAuditAsync(ctx, "refund_succeeded_webhook", "refund", refund.ID, refund.InitiatedBy, "admin", eventID, map[string]interface{}{
			"charge_id":         charge.ID,
			"gateway_refund_id": refund.GatewayRefundID,
			"webhook_confirmed": true,
		})
	}

	log.Printf("[REFUND_WEBHOOK] Processed webhook confirmation for %d refunds\n", len(refunds))
	return nil
}

// processFullTransactionRefund handles full transaction refunds (all tickets in transaction)
func (pw *PaymentWorker) processFullTransactionRefund(refund *models.Refund) error {
	if len(refund.AffectedTicketIDs) == 0 {
		log.Printf("[FULL_REFUND] No affected tickets found for full transaction refund %s\n", refund.ID)
		return nil
	}

	tx := pw.ticketService.GetDB().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("[FULL_REFUND] Panic during full transaction refund processing: %v\n", r)
		}
	}()

	// Convert string IDs to UUIDs
	var ticketUUIDs []uuid.UUID
	for _, ticketIDStr := range refund.AffectedTicketIDs {
		ticketUUID, err := uuid.Parse(ticketIDStr)
		if err != nil {
			log.Printf("[FULL_REFUND] Invalid ticket ID %s in refund %s\n", ticketIDStr, refund.ID)
			continue
		}
		ticketUUIDs = append(ticketUUIDs, ticketUUID)
	}

	if len(ticketUUIDs) == 0 {
		tx.Rollback()
		return fmt.Errorf("no valid ticket IDs found in refund")
	}

	// Update all affected tickets to "refunded" status
	for _, ticketID := range ticketUUIDs {
		if err := tx.Model(&models.Ticket{}).Where("id = ?", ticketID).Update("status", "refunded").Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update ticket %s status: %w", ticketID, err)
		}
		log.Printf("[FULL_REFUND] Ticket %s marked as refunded\n", ticketID)
	}

	// Restore inventory: group affected tickets by tier and update sold count
	tierQuantities := make(map[uuid.UUID]int)
	for _, ticketID := range ticketUUIDs {
		var ticket models.Ticket
		if err := tx.Where("id = ?", ticketID).First(&ticket).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to find ticket %s for inventory restoration: %w", ticketID, err)
		}
		tierQuantities[ticket.TierID]++
	}

	// Update tier sold counts (decrease sold count = restore inventory)
	for tierID, qty := range tierQuantities {
		result := tx.Model(&models.EventTier{}).
			Where("id = ?", tierID).
			Updates(map[string]interface{}{
				"sold": gorm.Expr("GREATEST(sold - ?, 0)", qty), // Prevent negative
			})

		if result.Error != nil {
			tx.Rollback()
			return fmt.Errorf("failed to restore inventory for tier %s: %w", tierID, result.Error)
		}
		log.Printf("[FULL_REFUND] Restored %d tickets to inventory for tier %s\n", qty, tierID)
	}

	// For FULL transaction refunds, mark transaction and checkout session as refunded
	if err := tx.Model(&models.Transaction{}).Where("id = ?", refund.TransactionID).Update("status", "refunded").Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update transaction status: %w", err)
	}
	log.Printf("[FULL_REFUND] Transaction %s marked as refunded\n", refund.TransactionID)

	// Find and update checkout sessions for this payment intent (all tickets in transaction share same payment intent)
	if err := tx.Model(&models.CheckoutSession{}).
		Where("gateway_data->>'payment_intent_id' = ?", refund.PaymentIntentID.String()).
		Updates(map[string]interface{}{
			"status":     "refunded",
			"updated_at": time.Now(),
		}).Error; err != nil {
		log.Printf("[FULL_REFUND] Warning: Failed to update checkout sessions: %v\n", err)
		// Don't fail the entire refund for this
	} else {
		log.Printf("[FULL_REFUND] Checkout sessions for payment intent %s marked as refunded\n", refund.PaymentIntentID)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit full transaction refund: %w", err)
	}

	log.Printf("[FULL_REFUND] Successfully processed full transaction refund for %d tickets\n", len(ticketUUIDs))
	return nil
}

// processPartialTicketRefund handles partial refunds by updating only affected tickets
func (pw *PaymentWorker) processPartialTicketRefund(refund *models.Refund) error {
	if len(refund.AffectedTicketIDs) == 0 {
		log.Printf("[PARTIAL_REFUND] No affected tickets found for refund %s\n", refund.ID)
		return nil
	}

	tx := pw.ticketService.GetDB().Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("[PARTIAL_REFUND] Panic during partial refund processing: %v\n", r)
		}
	}()

	// Convert string IDs to UUIDs
	var ticketUUIDs []uuid.UUID
	for _, ticketIDStr := range refund.AffectedTicketIDs {
		ticketUUID, err := uuid.Parse(ticketIDStr)
		if err != nil {
			log.Printf("[PARTIAL_REFUND] Invalid ticket ID %s in refund %s\n", ticketIDStr, refund.ID)
			continue
		}
		ticketUUIDs = append(ticketUUIDs, ticketUUID)
	}

	if len(ticketUUIDs) == 0 {
		tx.Rollback()
		return fmt.Errorf("no valid ticket IDs found in refund")
	}

	// Update only the affected tickets to "refunded" status
	for _, ticketID := range ticketUUIDs {
		if err := tx.Model(&models.Ticket{}).Where("id = ?", ticketID).Update("status", "refunded").Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update ticket %s status: %w", ticketID, err)
		}
		log.Printf("[PARTIAL_REFUND] Ticket %s marked as refunded\n", ticketID)
	}

	// Restore inventory: group affected tickets by tier and update sold count
	tierQuantities := make(map[uuid.UUID]int)
	for _, ticketID := range ticketUUIDs {
		var ticket models.Ticket
		if err := tx.Where("id = ?", ticketID).First(&ticket).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to find ticket %s for inventory restoration: %w", ticketID, err)
		}
		tierQuantities[ticket.TierID]++
	}

	// Update tier sold counts (decrease sold count = restore inventory)
	for tierID, qty := range tierQuantities {
		result := tx.Model(&models.EventTier{}).
			Where("id = ?", tierID).
			Updates(map[string]interface{}{
				"sold": gorm.Expr("GREATEST(sold - ?, 0)", qty), // Prevent negative
			})

		if result.Error != nil {
			tx.Rollback()
			return fmt.Errorf("failed to restore inventory for tier %s: %w", tierID, result.Error)
		}
		log.Printf("[PARTIAL_REFUND] Restored %d tickets to inventory for tier %s\n", qty, tierID)
	}

	// NOTE: We do NOT update checkout session status to "refunded" because:
	// - This is a partial refund, not a full transaction refund
	// - Checkout session represents the original purchase transaction
	// - Multiple partial refunds may exist for the same transaction
	// - Checkout session status should remain "completed"

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit partial refund transaction: %w", err)
	}

	log.Printf("[PARTIAL_REFUND] Successfully processed partial refund for %d tickets\n", len(ticketUUIDs))
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
	return "http://localhost:3000"
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
