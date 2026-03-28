# Stripe Response Capture Analysis

## 1. PaymentIntent Model Fields for Stripe Response Data

**Location:** [internal/models/payment.go](internal/models/payment.go#L1-L100)

### Available Fields for Storing Stripe Response:

```go
// Gateway Integration Fields
GatewayPaymentID *string                 // Stripe PI ID (pi_xxx) ✓ CAPTURED
GatewayChargeID  *string                 // Stripe Charge ID (ch_xxx) ✓ CAPTURED

// Gateway Response Storage (CURRENTLY EMPTY - NOT BEING POPULATED)
GatewayResponse  map[string]interface{}  // JSONB - For full Stripe PaymentIntent response
GatewayMetadata  map[string]interface{}  // JSONB - Custom metadata from Stripe

// Payment Method Information (PARTIALLY CAPTURED)
PaymentMethodType    string                 // card, wallet, bank_transfer, upi
PaymentMethodDetails map[string]interface{} // {"brand":"visa","type":"credit","last4":"4242"}
```

**Status:** ❌ **GatewayResponse field exists but is NOT being populated with Stripe response data**

---

## 2. Stripe Response Fields That Should Be Captured

From Stripe PaymentIntent object, these fields should be stored in `GatewayResponse`:

```json
{
  "id": "pi_xxx",
  "object": "payment_intent",
  "amount": 2000,
  "amount_capturable": 0,
  "amount_received": 2000,
  "charge": "ch_xxx",
  "client_secret": "pi_xxx_secret_xxx",
  "currency": "usd",
  "customer": null,
  "description": "Event ticket description",
  "last_payment_error": null,
  "livemode": true,
  "metadata": {
    "checkout_token": "token_xxx",
    "event_id": "uuid"
  },
  "next_action": null,
  "payment_method": "pm_xxx",
  "payment_method_types": ["card"],
  "receipt_email": "customer@example.com",
  "setup_future_usage": null,
  "statement_descriptor": null,
  "status": "succeeded",
  "created": 1234567890,
  "confirmation_method": "automatic",
  "processing": null
}
```

---

## 3. Current Code That Handles Stripe Responses

### A. Payment Intent Creation

**File:** [internal/gateways/stripe_gateway.go](internal/gateways/stripe_gateway.go#L42-L87)

```go
func (sg *StripeGateway) CreatePaymentIntent(ctx context.Context, req *PaymentIntentRequest) (*PaymentIntentResponse, error) {
    // ... creates Stripe PaymentIntent
    pi, err := paymentintent.New(params)
    if err != nil {
        log.Printf("[STRIPE] Failed to create payment intent: %v", err)
        return nil, fmt.Errorf("failed to create payment intent: %w", err)
    }

    resp := &PaymentIntentResponse{
        ClientSecret: pi.ClientSecret,
        Status:       string(pi.Status),
        Amount:       req.Amount,
        Currency:     req.Currency,
        CreatedAt:    time.Now(),
        Metadata: map[string]interface{}{
            "stripe_payment_intent_id": pi.ID,
        },
    }
    return resp, nil
}
```

**Problem:** ❌ Only stores `ClientSecret`, `Status`, and `pi.ID` in metadata. Full `pi` object is NOT captured.

---

### B. Webhook Event Processing

**File:** [internal/gateways/stripe_gateway.go](internal/gateways/stripe_gateway.go#L143-L196)

```go
func (sg *StripeGateway) VerifyWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error) {
    // ... signature verification

    var eventData map[string]interface{}
    if err := json.Unmarshal(event.Data.Raw, &eventData); err != nil {
        log.Printf("[STRIPE_WEBHOOK] Failed to unmarshal event data: %v", err)
        return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
    }

    webhookEvent := &WebhookEvent{
        Gateway:   "stripe",
        EventID:   event.ID,
        Type:      event.Type,
        Data:      eventData,  // ✓ Full Stripe response data is here!
        CreatedAt: time.Unix(event.Created, 0),
    }

    switch event.Type {
    case "payment_intent.succeeded":
        var pi stripe.PaymentIntent
        if err := json.Unmarshal(event.Data.Raw, &pi); err == nil {
            webhookEvent.PaymentIntentID = pi.ID  // ✓ Extracts PI ID
        }
    }
    return webhookEvent, nil
}
```

**Status:** ✓ Full event data is captured in `eventData` and stored in `WebhookEvent.Payload`
**Problem:** ❌ This data is NOT transferred to `PaymentIntent.GatewayResponse`

---

### C. Webhook Handler Entry Point

**File:** [internal/handlers/webhook_handler.go](internal/handlers/webhook_handler.go#L59-L224)

```go
func (h *WebhookHandler) HandleStripeWebhook(c *gin.Context) {
    // Step 1-2: Read and verify payload
    payload, err := io.ReadAll(c.Request.Body)
    sig := c.GetHeader("Stripe-Signature")

    webhookEvent, err := h.stripeGateway.VerifyWebhook(c.Request.Context(), payload, sig)
    log.Printf("[WEBHOOK] [%s] ✓ Signature verified | event_id=%s | type=%s", requestID, webhookEvent.EventID, webhookEvent.Type)

    // Step 3: Create WebhookEvent record in database
    webhookEventRecord := &models.WebhookEvent{
        PaymentGateway: "stripe",
        GatewayEventID: webhookEvent.EventID,
        EventType:      webhookEvent.Type,
        Status:         "pending",
        Payload:        webhookEvent.Data,  // ✓ Stripe response captured here
        Headers: map[string]interface{}{
            "request-id": requestID,
            "timestamp":  time.Now().Unix(),
        },
    }

    if err := h.db.Create(webhookEventRecord).Error; err != nil {
        log.Printf("[WEBHOOK] [%s] ⚠️  Failed to record webhook event: %v", requestID, err)
    }

    // Step 9: Enqueue for async processing
    taskID, err := paymentWorker.EnqueuePaymentSuccess(c.Request.Context(), taskPayload)
}
```

**Status:** ✓ Stores full Stripe response in `WebhookEvent.Payload` with request ID tracking

---

### D. Payment Worker - Async Processing

**File:** [internal/workers/payment_worker.go](internal/workers/payment_worker.go#L161-L233) and [L304-L560](internal/workers/payment_worker.go#L304-L560)

```go
// HandlePaymentSuccess - Async payment processing
func (pw *PaymentWorker) HandlePaymentSuccess(ctx context.Context, t *asynq.Task) error {
    var payload PaymentTaskPayload
    if err := json.Unmarshal(t.Payload(), &payload); err != nil {
        log.Printf("ERROR: Failed to unmarshal payload: %v\n", err)
        return fmt.Errorf("failed to unmarshal payload: %w", err)
    }

    log.Printf("Processing payment success task (EventID: %s, WebhookID: %s, EventType: %s)\n",
        payload.StripeEventID, payload.WebhookEventID, payload.EventType)

    var paymentIntent *stripe.PaymentIntent

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
        paymentIntent = &stripe.PaymentIntent{
            ID:       session.PaymentIntent.ID,
            Status:   stripe.PaymentIntentStatusSucceeded,
            Amount:   session.AmountTotal,
            Currency: session.Currency,
            Metadata: session.Metadata,
        }
    }

    // Process payment
    if err := pw.processPaymentIntentSucceeded(ctx, payload.WebhookEventID, paymentIntent, stripeEventUUID, payload.RequestID); err != nil {
        log.Printf("ERROR: Failed to process payment success: %v\n", err)
        return fmt.Errorf("payment processing failed: %w", err)
    }

    log.Printf("✅ Payment success processed (EventID: %s, WebhookID: %s)\n",
        payload.StripeEventID, payload.WebhookEventID)
    return nil
}

// Main processing: extracting and updating payment data
func (pw *PaymentWorker) processPaymentIntentSucceeded(ctx context.Context, webhookEventID uuid.UUID,
        paymentIntent *stripe.PaymentIntent, stripeEventID uuid.UUID, requestID string) error {

    log.Printf("[PAYMENT_SUCCESS] Processing payment intent: %s (request_id: %s)\n",
        paymentIntent.ID, requestID)

    // Update PaymentIntent with gateway payment ID
    if err := db.Model(&models.PaymentIntent{}).
        Where("id = ?", dbPaymentIntent.ID).
        Update("gateway_payment_id", paymentIntent.ID).Error; err != nil {
        return fmt.Errorf("failed to update payment intent gateway_payment_id: %w", err)
    }

    // Update payment intent status to succeeded
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
}
```

**Issues:**

- ❌ `paymentIntent` object from Stripe is NOT stored in `PaymentIntent.GatewayResponse`
- ❌ Only `gateway_payment_id` and `status` are captured
- ❌ No logging statements for capturing full Stripe response

---

### E. Payment Service

**File:** [internal/services/payment_service.go](internal/services/payment_service.go#L1258-L1318)

```go
func (s *PaymentService) HandlePaymentSuccess(ctx context.Context, gatewayPaymentID string, gateway string) (*models.PaymentIntent, error) {
    // Lock, check idempotency, update status
    paymentIntent.Status = "succeeded"
    now := time.Now()
    paymentIntent.SucceededAt = &now

    if err := tx.Save(&paymentIntent).Error; err != nil {
        tx.Rollback()
        return nil, fmt.Errorf("failed to update payment intent: %w", err)
    }

    s.logAudit(ctx, "payment_succeeded", "payment_intent", paymentIntent.ID, paymentIntent.UserID, nil)
    return &paymentIntent, nil
}
```

**Status:** ✓ Updates status and timestamp
**Problem:** ❌ No Stripe response data captured

---

## 4. Webhook Event Model

**File:** [internal/models/payment.go](internal/models/payment.go#L145-L180)

```go
type WebhookEvent struct {
    ID             uuid.UUID                 // Unique webhook event ID
    PaymentGateway string                    // "stripe"
    GatewayEventID string                    // Unique Stripe event ID (evt_xxx)
    EventType      string                    // "payment_intent.succeeded", etc.
    APIVersion     string                    // Stripe API version

    // Processing Status
    Status         string                    // pending, queued, succeeded, failed, ignored
    ProcessedCount int                       // How many times processed
    LastError      string                    // Last error message

    // Relations
    PaymentIntentID *uuid.UUID                // Link to PaymentIntent
    TransactionID   *uuid.UUID                // Link to Transaction

    // Raw Data (✓ FULL STRIPE RESPONSE CAPTURED HERE)
    Payload        map[string]interface{}    // ✓ Complete Stripe event payload

    CreatedAt      time.Time
    UpdatedAt      time.Time
    ProcessedAt    *time.Time
}
```

**Status:** ✓ Stores full Stripe response in `Payload` field
**Problem:** ❌ Not being transferred to `PaymentIntent.GatewayResponse`

---

## 5. Current Logging Statements

### A. Webhook Verification Logs

- `[STRIPE_WEBHOOK] Webhook secret not configured`
- `[STRIPE_WEBHOOK] Signature verification failed`
- `[STRIPE_WEBHOOK] Failed to unmarshal event data`

### B. Webhook Handler Logs

- `[WEBHOOK] ✓ Signature verified | event_id=XXX | type=XXX`
- `[WEBHOOK] ✓ Recorded webhook event: id=XXX`
- `[WEBHOOK] ℹ️  Ignoring event type: XXX`
- `[WEBHOOK] ✓ Processing: payment_intent.succeeded`

### C. Payment Worker Logs

- `Processing payment success task (EventID: XXX, WebhookID: XXX, EventType: XXX)`
- `ERROR: Failed to unmarshal payment intent: XXX`
- `[PAYMENT_SUCCESS] Processing payment intent: XXX (request_id: XXX)`
- `[DB_PAYMENT_INTENT_LOADED] EventID=XXX for payment XXX`

**Status:** ❌ **NO Logging of Stripe response fields captured**

---

## 6. Summary: What's Missing

### Currently Captured:

✓ `GatewayPaymentID` (pi_xxx)
✓ Full Stripe response stored in `WebhookEvent.Payload`
✓ Request ID tracking
✓ Webhook event lifecycle tracking

### NOT Captured/Stored:

❌ `PaymentIntent.GatewayResponse` - Empty, never populated
❌ `GatewayChargeID` (ch_xxx) - Not extracted from Stripe response
❌ Payment method details (brand, last4, type)
❌ Stripe payment status from response
❌ Processing information from Stripe
❌ Logging of response data being captured

---

## 7. Where to Add Response Logging

### **BEFORE** - Current code (payment_worker.go, line ~307):

```go
func (pw *PaymentWorker) processPaymentIntentSucceeded(...) error {
    log.Printf("[PAYMENT_SUCCESS] Processing payment intent: %s (request_id: %s)\n",
        paymentIntent.ID, requestID)

    // Update PaymentIntent with gateway payment ID
    if err := db.Model(&models.PaymentIntent{}).
        Where("id = ?", dbPaymentIntent.ID).
        Update("gateway_payment_id", paymentIntent.ID).Error; err != nil {
        return fmt.Errorf("failed to update payment intent gateway_payment_id: %w", err)
    }
}
```

### **AFTER** - Proposed enhancement:

```go
func (pw *PaymentWorker) processPaymentIntentSucceeded(...) error {
    log.Printf("[PAYMENT_SUCCESS] Processing payment intent: %s (request_id: %s)\n",
        paymentIntent.ID, requestID)

    // ✅ NEW: Log Stripe response data
    log.WithFields(log.Fields{
        "payment_intent_id": paymentIntent.ID,
        "stripe_status":     paymentIntent.Status,
        "amount":            paymentIntent.Amount,
        "currency":          paymentIntent.Currency,
        "charge_id":         paymentIntent.Charges.Data[0].ID,  // Charge ID
        "payment_method":    paymentIntent.PaymentMethod,
        "receipt_email":     paymentIntent.ReceiptEmail,
        "request_id":        requestID,
    }).Info("[STRIPE_RESPONSE] Payment intent response captured")

    // ✅ NEW: Store full response in GatewayResponse
    gatewayResponseData := map[string]interface{}{
        "stripe_payment_intent_id": paymentIntent.ID,
        "stripe_status":            paymentIntent.Status,
        "amount":                   paymentIntent.Amount,
        "currency":                 paymentIntent.Currency,
        "receipt_email":            paymentIntent.ReceiptEmail,
        "payment_method":           paymentIntent.PaymentMethod,
        "created":                  paymentIntent.Created,
        "client_secret":            paymentIntent.ClientSecret,
        "metadata":                 paymentIntent.Metadata,
    }

    if len(paymentIntent.Charges.Data) > 0 {
        gatewayResponseData["charge_id"] = paymentIntent.Charges.Data[0].ID
    }

    // Update PaymentIntent with gateway payment ID AND response data
    if err := db.Model(&models.PaymentIntent{}).
        Where("id = ?", dbPaymentIntent.ID).
        Updates(map[string]interface{}{
            "gateway_payment_id": paymentIntent.ID,
            "gateway_response":   gatewayResponseData,  // ✅ NEW
        }).Error; err != nil {
        return fmt.Errorf("failed to update payment intent: %w", err)
    }
}
```

---

## 8. Code Locations Summary

| Component                   | File                                   | Lines     | Status                    |
| --------------------------- | -------------------------------------- | --------- | ------------------------- |
| PaymentIntent Model         | `internal/models/payment.go`           | 1-100     | ✓ Has fields, ❌ Not used |
| Stripe Gateway Creation     | `internal/gateways/stripe_gateway.go`  | 42-87     | ❌ Response not captured  |
| Webhook Verification        | `internal/gateways/stripe_gateway.go`  | 143-196   | ✓ Full data verified      |
| Webhook Handler             | `internal/handlers/webhook_handler.go` | 59-224    | ✓ Stores in WebhookEvent  |
| Payment Worker - Entry      | `internal/workers/payment_worker.go`   | 161-233   | ❌ Response not saved     |
| Payment Worker - Processing | `internal/workers/payment_worker.go`   | 304-560   | ❌ Response not saved     |
| WebhookEvent Update         | `internal/workers/payment_worker.go`   | 726-760   | ✓ Tracks webhook status   |
| Payment Service             | `internal/services/payment_service.go` | 1258-1318 | ❌ Response not captured  |

---

## 9. What Needs to Be Done

1. **Populate `PaymentIntent.GatewayResponse`** with full Stripe response in `payment_worker.processPaymentIntentSucceeded()`
2. **Extract `GatewayChargeID`** from `paymentIntent.Charges.Data[0].ID`
3. **Add structured logging** for Stripe response fields
4. **Transfer data** from `WebhookEvent.Payload` to `PaymentIntent.GatewayResponse` during processing
5. **Log key metrics** like charge ID, payment method, receipt email, Stripe status
