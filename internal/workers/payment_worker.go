package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stripe/stripe-go/v74"
	"gorm.io/gorm/clause"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
)

// PaymentWorker handles asynchronous payment processing tasks
type PaymentWorker struct {
	client                     *asynq.Client
	server                     *asynq.Server
	ticketService              *services.TicketService
	reservationService         *services.ReservationService
	emailOutboxService         *services.EmailOutboxService
	processingLockService      *services.ProcessingLockService
	eventReconciliationService *services.EventReconciliationService
	cfg                        *config.Config
	queueConfig                *config.QueueConfig
	mux                        *asynq.ServeMux
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

	// Queue names
	QueueCritical = "critical"
	QueueDefault  = "default"
)

// NewPaymentWorker creates a new payment worker
func NewPaymentWorker(cfg *config.Config, ticketService *services.TicketService) *PaymentWorker {
	qc := config.NewQueueConfig(cfg)
	return &PaymentWorker{
		client:                     asynq.NewClient(qc.GetRedisClientOpt()),
		ticketService:              ticketService,
		reservationService:         services.NewReservationService(ticketService.GetDB()),
		emailOutboxService:         services.NewEmailOutboxService(ticketService.GetDB()),
		processingLockService:      services.NewProcessingLockService(ticketService.GetDB()),
		eventReconciliationService: services.NewEventReconciliationService(ticketService.GetDB()),
		cfg:                        cfg,
		queueConfig:                qc,
		mux:                        asynq.NewServeMux(),
	}
}

// RegisterHandlers registers all payment task handlers
func (pw *PaymentWorker) RegisterHandlers() {
	pw.mux.HandleFunc(TypePaymentSuccess, pw.HandlePaymentSuccess)
	pw.mux.HandleFunc(TypePaymentFailed, pw.HandlePaymentFailed)
	pw.mux.HandleFunc(TypePaymentCanceled, pw.HandlePaymentCanceled)
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

	log.Println("Starting payment worker (asynq server)...")
	return pw.server.Start(pw.mux)
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

// HandlePaymentSuccess processes successful payment webhook asynchronously
func (pw *PaymentWorker) HandlePaymentSuccess(ctx context.Context, t *asynq.Task) error {
	var payload PaymentTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
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
			return fmt.Errorf("failed to unmarshal payment intent: %w", err)
		}

	case "checkout.session.completed":
		session := &stripe.CheckoutSession{}
		if err := json.Unmarshal(payload.RawData, session); err != nil {
			log.Printf("ERROR: Failed to unmarshal checkout session: %v\n", err)
			return fmt.Errorf("failed to unmarshal checkout session: %w", err)
		}

		// Extract payment intent from session
		if session.PaymentIntent == nil {
			return fmt.Errorf("checkout session missing payment intent")
		}

		// We need to get the full PaymentIntent object, not just the ID
		// For now, we'll create a minimal PaymentIntent from the session data
		paymentIntent = &stripe.PaymentIntent{
			ID:       session.PaymentIntent.ID,
			Status:   stripe.PaymentIntentStatusSucceeded,
			Amount:   session.AmountTotal,
			Currency: session.Currency,
			Metadata: session.Metadata,
		}

	default:
		return fmt.Errorf("unsupported event type: %s", payload.EventType)
	}

	// Process payment in database
	stripeEventUUID, err := uuid.Parse(payload.StripeEventID)
	if err != nil {
		return fmt.Errorf("invalid stripe event ID: %w", err)
	}
	if err := pw.processPaymentIntentSucceeded(ctx, paymentIntent, stripeEventUUID, payload.RequestID); err != nil {
		log.Printf("ERROR: Failed to process payment success: %v\n", err)
		return fmt.Errorf("payment processing failed: %w", err)
	}

	log.Printf("Payment success processed (EventID: %s)\n", payload.StripeEventID)
	return nil
}

// HandlePaymentFailed processes failed payment webhook asynchronously
func (pw *PaymentWorker) HandlePaymentFailed(ctx context.Context, t *asynq.Task) error {
	var payload PaymentTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	log.Printf("Processing payment failed task (EventID: %s, WebhookID: %s)\n", payload.StripeEventID, payload.WebhookEventID)

	paymentIntent := &stripe.PaymentIntent{}
	if err := json.Unmarshal(payload.RawData, paymentIntent); err != nil {
		log.Printf("ERROR: Failed to unmarshal payment intent: %v\n", err)
		return fmt.Errorf("failed to unmarshal payment intent: %w", err)
	}

	// Process payment failure in database
	if err := pw.processPaymentIntentFailed(ctx, paymentIntent, payload.WebhookEventID, payload.RequestID); err != nil {
		log.Printf("ERROR: Failed to process payment failure: %v\n", err)
		return fmt.Errorf("payment failure processing failed: %w", err)
	}

	log.Printf("Payment failure processed (EventID: %s)\n", payload.StripeEventID)
	return nil
}

// HandlePaymentCanceled processes canceled payment webhook asynchronously
func (pw *PaymentWorker) HandlePaymentCanceled(ctx context.Context, t *asynq.Task) error {
	var payload PaymentTaskPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	log.Printf("Processing payment canceled task (EventID: %s, WebhookID: %s)\n", payload.StripeEventID, payload.WebhookEventID)

	paymentIntent := &stripe.PaymentIntent{}
	if err := json.Unmarshal(payload.RawData, paymentIntent); err != nil {
		log.Printf("ERROR: Failed to unmarshal payment intent: %v\n", err)
		return fmt.Errorf("failed to unmarshal payment intent: %w", err)
	}

	// Process payment cancel in database
	if err := pw.processPaymentIntentCanceled(ctx, paymentIntent, payload.WebhookEventID, payload.RequestID); err != nil {
		log.Printf("ERROR: Failed to process payment cancel: %v\n", err)
		return fmt.Errorf("payment cancel processing failed: %w", err)
	}

	log.Printf("Payment canceled processed (EventID: %s)\n", payload.StripeEventID)
	return nil
}

// processPaymentIntentSucceeded processes successful payment intents using the new production architecture
func (pw *PaymentWorker) processPaymentIntentSucceeded(ctx context.Context, paymentIntent *stripe.PaymentIntent, stripeEventID uuid.UUID, requestID string) error {
	log.Printf("[PAYMENT_SUCCESS] Processing payment intent: %s (request_id: %s)\n", paymentIntent.ID, requestID)

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
	// PHASE 2: EVENT RECONCILIATION
	// ========================================
	reconciliation, err := pw.eventReconciliationService.RecordStripeEvent(ctx, stripeEventID.String(), "payment_intent.succeeded", paymentIntent.ID, map[string]interface{}{
		"payment_intent_id": paymentIntent.ID,
		"amount":            paymentIntent.Amount,
		"currency":          paymentIntent.Currency,
		"status":            paymentIntent.Status,
	})
	if err != nil {
		return fmt.Errorf("failed to record event for reconciliation: %w", err)
	}

	// Check if this event was already processed
	if reconciliation.Status == "processed" {
		log.Printf("[RECONCILIATION] Event already processed: %s\n", stripeEventID)
		return nil
	}

	// ========================================
	// PHASE 3: RESERVATION CONFIRMATION
	// ========================================
	checkoutToken, _ := paymentIntent.Metadata["checkout_token"]
	if checkoutToken == "" {
		return fmt.Errorf("checkout token not found in payment intent metadata")
	}

	// Confirm the reservation atomically
	err = pw.reservationService.ConfirmReservation(ctx, checkoutToken, paymentIntent.ID)
	if err != nil {
		// Mark reconciliation as failed
		if markErr := pw.eventReconciliationService.MarkEventAsFailed(ctx, stripeEventID.String(), err.Error()); markErr != nil {
			log.Printf("WARN: Failed to mark reconciliation as failed: %v\n", markErr)
		}
		return fmt.Errorf("failed to confirm reservation: %w", err)
	}

	log.Printf("[RESERVATION] Confirmed reservation for checkout token %s\n", checkoutToken)

	// ========================================
	// PHASE 4: ATOMIC PAYMENT PROCESSING + TRANSACTION CREATION
	// ========================================
	db := pw.ticketService.GetDB()

	// FIRST: Load the database PaymentIntent to get EventID
	var dbPaymentIntent models.PaymentIntent
	if err := db.Where("checkout_token = ?", checkoutToken).First(&dbPaymentIntent).Error; err != nil {
		return fmt.Errorf("failed to load payment intent from database: %w", err)
	}
	log.Printf("[DB_PAYMENT_INTENT_LOADED] EventID=%s for payment %s\n", dbPaymentIntent.EventID, paymentIntent.ID)

	// Update PaymentIntent with gateway payment ID
	if err := db.Model(&models.PaymentIntent{}).
		Where("id = ?", dbPaymentIntent.ID).
		Update("gateway_payment_id", paymentIntent.ID).Error; err != nil {
		return fmt.Errorf("failed to update payment intent gateway_payment_id: %w", err)
	}

	// Load event with tiers for commission calculation and currency
	var event models.Event
	if err := db.Preload("Tiers").Where("id = ?", dbPaymentIntent.EventID).First(&event).Error; err != nil {
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
	paymentUpdate := map[string]interface{}{
		"status":       "succeeded",
		"succeeded_at": now,
		"updated_at":   now,
	}

	if err := tx.Model(&models.PaymentIntent{}).
		Where("gateway_payment_id = ?", paymentIntent.ID).
		Updates(paymentUpdate).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update payment intent status: %w", err)
	}

	// Get ticket IDs and details from created tickets (they have PaymentIntentID set)
	var tickets []models.Ticket
	if err := tx.Where("payment_intent_id = ?", paymentIntent.ID).
		Preload("Tier").
		Find(&tickets).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to find created tickets: %w", err)
	}

	var ticketIDs []uuid.UUID
	for _, ticket := range tickets {
		ticketIDs = append(ticketIDs, ticket.ID)
	}

	// Update ticket payment status
	if err := tx.Model(&models.Ticket{}).
		Where("id IN (?)", ticketIDs).
		Updates(map[string]interface{}{
			"payment_status": "completed",
			"paid_at":        now,
			"updated_at":     now,
		}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update ticket payment status: %w", err)
	}

	// Update checkout session
	if err := tx.Model(&models.CheckoutSession{}).
		Where("checkout_token = ?", checkoutToken).
		Updates(map[string]interface{}{
			"status":     "completed",
			"updated_at": now,
		}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// ========================================
	// CREATE SINGLE TRANSACTION RECORD FOR ENTIRE PURCHASE
	// ========================================
	// Calculate totals across all tiers and tickets
	totalAmount := 0.0
	totalTickets := len(tickets)

	for _, ticket := range tickets {
		totalAmount += ticket.Tier.Price
	}

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
		log.Printf("ERROR: Failed to create transaction: %v\n", err)
		return fmt.Errorf("failed to create transaction record: %w", err)
	}

	// Link all tickets to this single transaction
	if err := tx.Model(&models.Ticket{}).
		Where("payment_intent_id = ?", paymentIntent.ID).
		Update("transaction_id", transaction.ID).Error; err != nil {
		tx.Rollback()
		log.Printf("ERROR: Failed to link tickets to transaction: %v\n", err)
		return fmt.Errorf("failed to link tickets to transaction: %w", err)
	}

	log.Printf("[TRANSACTION_CREATED] Single transaction for entire purchase: ID=%s, EventID=%s, TotalAmount=%.2f, TotalTickets=%d, Commission=%.2f, OrganizerShare=%.2f\n",
		transaction.ID, dbPaymentIntent.EventID, totalAmount, totalTickets, commissionAmount, organizerShare)

	// Commit the atomic transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit payment processing transaction: %w", err)
	}

	log.Printf("[PAYMENT_SUCCESS] Atomic processing completed for payment %s\n", paymentIntent.ID)

	// ========================================
	// AUDIT LOGGING FOR PAYMENT SUCCESS
	// ========================================
	pw.logAuditAsync(ctx, "payment_succeeded", "transaction", transaction.ID, dbPaymentIntent.UserID, "user", &dbPaymentIntent.EventID, map[string]interface{}{
		"payment_gateway": paymentIntent.ID,
		"total_amount":    totalAmount,
		"total_tickets":   totalTickets,
		"commission":      commissionAmount,
		"organizer_share": organizerShare,
	})

	// ========================================
	// PHASE 5: OUTBOX EMAIL QUEUING
	// ========================================
	if paymentIntent.ReceiptEmail != "" {
		emailData := map[string]interface{}{
			"checkout_token": checkoutToken,
			"payment_id":     paymentIntent.ID,
			"event_id":       "placeholder", // Will be filled by service
			"customer_email": paymentIntent.ReceiptEmail,
			"total_amount":   0, // Will be calculated
			"currency":       string(paymentIntent.Currency),
			"ticket_count":   0, // Will be calculated
		}

		if err := pw.emailOutboxService.QueueEmail(ctx, models.EmailEventTicketConfirmation, paymentIntent.ReceiptEmail, "Your Tickets Are Confirmed!", emailData, 2); err != nil {
			log.Printf("WARN: Failed to queue confirmation email: %v\n", err)
			// Don't fail the payment for email issues
		}
	}

	// ========================================
	// PHASE 6: RECONCILIATION MARKING
	// ========================================
	if err := pw.eventReconciliationService.MarkEventAsProcessed(ctx, stripeEventID.String()); err != nil {
		log.Printf("WARN: Failed to mark reconciliation as processed: %v\n", err)
		// Don't fail the payment for reconciliation issues
	}

	log.Printf("[PAYMENT_SUCCESS] Successfully processed payment intent: %s\n", paymentIntent.ID)
	return nil
}

// processPaymentIntentFailed processes failed payment
func (pw *PaymentWorker) processPaymentIntentFailed(ctx context.Context, paymentIntent *stripe.PaymentIntent, webhookEventID uuid.UUID, requestID string) error {
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

	// Update checkout session
	checkoutSession.Status = "failed"
	updates := map[string]interface{}{
		"payment_intent_id": paymentIntent.ID,
		"failure_reason":    "payment_failed",
		"failed_at":         time.Now(),
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

	log.Printf("Payment failure processing completed for token: %s\n", checkoutToken)

	// ========================================
	// AUDIT LOGGING FOR PAYMENT FAILURE
	// ========================================
	pw.logAuditAsync(ctx, "payment_failed", "checkout_session", checkoutSession.ID, checkoutSession.UserID, "user", &ticket.EventID, map[string]interface{}{
		"payment_gateway": paymentIntent.ID,
		"failure_reason":  "payment_declined_or_failed",
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

// Close closes the payment worker and client
func (pw *PaymentWorker) Close() error {
	if pw.client != nil {
		pw.client.Close()
	}
	if pw.server != nil {
		pw.server.Stop()
	}
	return nil
}
