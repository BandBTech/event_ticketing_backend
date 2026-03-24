# Webhook Processing Flow - Fixed ✅

## Overview

Complete end-to-end webhook processing flow with proper tracking and state management.

## Flow Diagram

```
STRIPE WEBHOOK
      ↓
[Webhook Handler]
      ↓
1. Verify signature ✓
      ↓
2. Create WebhookEvent record (status: pending) ✓
      ↓
3. Check event type (payment_intent.succeeded, checkout.session.completed)
      ↓
4. Extract payload data
      ↓
5. Enqueue job to Redis (async task) ✓
      ↓
6. Update WebhookEvent → status: "queued" ✓
      ↓
7. Return 200 OK to Stripe immediately ✓
      ↓
═══════════════════════════════════════════════════════
BACKGROUND WORKER (asynq)
      ↓
[Worker picks up job from Redis]
      ↓
8. Deserialize payload
      ↓
9. Handle based on event type:
   - payment_intent.succeeded → HandlePaymentSuccess
   - checkout.session.completed → HandlePaymentSuccess
   - payment_intent.payment_failed → HandlePaymentFailed
   - charge.dispute.created → HandlePaymentCanceled
      ↓
10. Process payment atomically:
    - Verify idempotency (distributed lock)
    - Record event for reconciliation
    - Confirm reservation
    - Update PaymentIntent status → "succeeded"
    - Create/update Tickets → "completed"
    - Create single Transaction record
    - Link all tickets to transaction
    - Update CheckoutSession → "completed"
    - Queue confirmation email
    - Mark reconciliation as processed
      ↓
11. ✅ Payment processing succeeds
      ↓
12. Update WebhookEvent:
    - status: "succeeded"
    - processed_count: +1
    - processed_at: now
    ✓
      ↓
FLOW COMPLETE - Payment is fully processed
═══════════════════════════════════════════════════════
```

## State Transitions

### WebhookEvent States

| State       | Meaning                                   | Who Sets It                            | Next State              |
| ----------- | ----------------------------------------- | -------------------------------------- | ----------------------- |
| `pending`   | Webhook received, not yet processed       | Webhook Handler (step 2)               | `queued`                |
| `queued`    | Job enqueued to Redis, waiting for worker | Webhook Handler (step 6)               | `succeeded` or `failed` |
| `succeeded` | Worker processed successfully             | **Payment Worker** (NEW - step 12)     | -                       |
| `failed`    | Error during processing                   | Payment Worker (on exception)          | -                       |
| `ignored`   | Event type not relevant                   | Webhook Handler (for irrelevant types) | -                       |

### PaymentIntent States

| Status       | Meaning                               | Set By                         | Next State              |
| ------------ | ------------------------------------- | ------------------------------ | ----------------------- |
| `pending`    | Created, awaiting Stripe confirmation | Client (checkout)              | `processing`            |
| `processing` | Being processed by worker             | -                              | `succeeded` or `failed` |
| `succeeded`  | ✅ Payment confirmed, tickets created | **Payment Worker** (now fixed) | -                       |
| `failed`     | ❌ Payment declined                   | Webhook Handler/Worker         | -                       |

## Key Fixes Applied

### ✅ 1. Webhook Event Status Tracking (FIXED)

**Before:** WebhookEvent stayed `queued` forever with ProcessedCount=0
**After:**

- After successful processing: `status = "succeeded"` + `processed_count = 1` + `processed_at = now`
- On error: `status = "failed"` + `last_error = "error message"`

### ✅ 2. Handler Updates

Updated all three handlers to update webhook_events table:

- `HandlePaymentSuccess()` → marks as succeeded ✓
- `HandlePaymentFailed()` → marks as succeeded (webhook was processed) ✓
- `HandlePaymentCanceled()` → marks as succeeded (webhook was processed) ✓

### ✅ 3. Error Tracking

Each handler now captures and logs errors:

- JSON unmarshal errors
- Missing data validation errors
- Processing errors
  All stored in `webhook_events.last_error`

### ✅ 4. New Method

Added `updateWebhookEventStatus()` to PaymentWorker for atomic status updates.

## How to Verify It Works

### 1. Check Redis Queue

```bash
redis-cli -n 0
> KEYS "*"  # Should see asynq keys
> LLEN asynq:queues:critical  # Task count
```

### 2. Check Webhook Events Table

```sql
-- Should transition:
-- pending → queued → succeeded
SELECT id, event_type, status, processed_count, processed_at, last_error, created_at
FROM webhook_events
ORDER BY created_at DESC
LIMIT 10;
```

### 3. Check Payment Intent Status

```sql
-- Status should be "succeeded" after webhook processing
SELECT id, payment_gateway, status, succeeded_at, created_at
FROM payment_intents
ORDER BY created_at DESC
LIMIT 5;
```

### 4. Check Server Logs

Look for these patterns:

```
✅ Payment success processed (EventID: ..., WebhookID: ...)
[WEBHOOK_STATUS] Updated webhook event ... to status: succeeded
[TRANSACTION_CREATED] Single transaction for entire purchase: ...
```

## Testing Checklist

- [ ] Start server: `go run cmd/api/main.go`
- [ ] Check payment worker started:
  ```
  logs | grep "Starting payment worker"
  ```
- [ ] Trigger webhook (test payment)
- [ ] Verify webhook arrives:
  ```
  logs | grep "Signature verified"
  ```
- [ ] Check webhook enqueued:
  ```
  logs | grep "Job enqueued successfully"
  ```
- [ ] Wait 2-5 seconds for worker
- [ ] Verify webhook processed:
  ```
  logs | grep "✅ Payment success processed"
  logs | grep "[WEBHOOK_STATUS] Updated webhook event"
  ```
- [ ] Check database:
  ```
  SELECT status, processed_count FROM webhook_events
  WHERE id = '...' LIMIT 1;
  -- Should show: status='succeeded', processed_count=1
  ```

## Troubleshooting

### Webhook stuck in "queued"

**Symptoms:**

- webhook_events.status = "queued"
- ProcessedCount = 0
- Logs don't show "✅ Payment success processed"

**Causes:**

1. **Worker not running**: Check for "Starting payment worker (asynq server)..." in logs
2. **Redis not connected**: Check logs for Redis connection errors
3. **Job failed**: Check `last_error` in webhook_events table

**Fix:**

- Ensure `docker-compose up` includes Redis
- Check worker initialization in main.go
- Look for "ERROR: Payment worker error:" in logs

### Payment stays "pending"

**Symptoms:**

- PaymentIntent.status = "pending"
- PaymentIntent.succeeded_at = NULL

**Causes:**

1. Webhook not received by your endpoint
2. Webhook failed signature verification
3. Worker didn't update PaymentIntent

**Fix:**

- Check Stripe webhook deliveries in dashboard
- Verify webhook endpoint URL is accessible
- Check worker logs for update errors

## Architecture Benefits

✅ **Idempotent**: Distributed lock prevents duplicate processing
✅ **Auditable**: All state changes logged in webhook_events
✅ **Stable**: 200 OK returned immediately, webhook retries don't cause duplicate charges
✅ **Observable**: Clear state transitions in database
✅ **Atomic**: All payment updates in single transaction
✅ **Recoverable**: Failed webhooks can be manually replayed
