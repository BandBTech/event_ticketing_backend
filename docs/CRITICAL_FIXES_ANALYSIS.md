/\*
CRITICAL PAYMENT SYSTEM FIXES
==============================

This document outlines the 5 critical issues and their implementation status.

█░ ISSUE #5: STATE MACHINE ENFORCEMENT (MOST CRITICAL)
═══════════════════════════════════════════════════════════════════════════════

PROBLEM:
Stripe may send webhook events out of order:

- payment_intent.succeeded
- payment_intent.payment_failed

Without proper state validation, this can lead to:
❌ Marking succeeded payment as failed (orphaned tickets!)
❌ Corrupting inventory (tickets created but payment failed)
❌ Accounting mismatch ($$ taken, tickets given, then marked failed)

SOLUTION IMPLEMENTED:
✅ Created state_machine.go with strict state transitions:

VALID TRANSITIONS:
├─ pending → succeeded (FINAL - cannot go back)
├─ pending → failed (FINAL - cannot go back)
├─ succeeded → IMMUTABLE (reject any further changes)
├─ failed → IMMUTABLE (reject any further changes)
└─ Any other transition → ERROR

USAGE:
sm := NewPaymentStateMachine(payment.Status)
if err := sm.TransitionToSucceeded(); err != nil {
// Handle error (out-of-order event, already terminal, etc.)
if err == ErrAlreadySucceeded {
// Idempotent - return success
} else {
// Invalid transition - reject webhook
log.Printf("SECURITY: Rejected invalid state transition: %v", err)
}
}

STATUS: ✅ READY TO INTEGRATE INTO payment_worker.go

█░ ISSUE #4: WORKER RETRY + ROW LOCK IDEMPOTENCY (CRITICAL)
═══════════════════════════════════════════════════════════════════════════════

PROBLEM:
asynq retries + SELECT FOR UPDATE can cause thundering herd:

T=0:00 Webhook enqueues job
T=0:10 Worker 1 dequeues, starts processing
T=0:11 Worker 1 holds row lock (FOR UPDATE)
T=0:15 asynq timeout → re-enqueues job
T=0:16 Worker 2 dequeues SAME job
T=0:17 Worker 2 tries SELECT FOR UPDATE → BLOCKED waiting for Worker 1
T=0:20 Worker 1 finishes, releases lock
T=0:21 Worker 2 acquires lock, processes AGAIN (duplicate!)

Result: Same webhook processed TWICE despite UNIQUE constraints

SOLUTION REQUIRED:
✅ Use webhook_events.status as idempotency gate BEFORE lock stage:

STEPS:

1. Check webhook_events.status
   IF status IN ('processing', 'succeeded'):
   - Log: "Webhook already processing/succeeded, skipping"
   - Return nil (idempotent - no error)

2. Update webhook_events.status = 'processing'
   (acquire logical lock before attempting row lock)

3. THEN proceed to SELECT...FOR UPDATE
   (now only one worker will acquire actual row lock)

BENEFIT:

- Prevents duplicate SQL INSERT before lock
- Reduces lock contention (worker 2 bails at step 1)
- asynq can still retry, but webhook_events check gates it

IMPLEMENTATION NEEDED:
□ In payment_worker.go, before INSERT PaymentIntent:

    // Check if already processing this webhook event
    var webhookEvent models.WebhookEvent
    if err := tx.Where("gateway_event_id = ?", stripeEventID).First(&webhookEvent).Error; err == nil {
        if webhookEvent.Status == "processing" || webhookEvent.Status == "succeeded" {
            log.Printf("IDEMPOTENT: Webhook already processing/done, skipping\n")
            tx.Commit()
            return nil  // Success - no duplicate execution
        }
    }

    // Update webhook status to 'processing'
    if err := tx.Model(&models.WebhookEvent{}).
        Where("gateway_event_id = ?", stripeEventID).
        Update("status", "processing").Error; err != nil {
        return err
    }

    // NOW safe to proceed with row lock attempts

█░ ISSUE #2: CLEANUP GUARD + INDEX (HIGH PRIORITY)
═══════════════════════════════════════════════════════════════════════════════

PROBLEM:
Current cleanup query:
SELECT \* FROM payment_intents
WHERE status = 'pending'
AND expires_at < NOW()

Issues:
❌ No guard on reserved amount (could over-release)
❌ Missing index on (status, expires_at) - full table scan!
❌ If worker fails, cleanup never runs

Race condition:
T=0:00 Worker marks payment = 'pending'
T=0:05 Cleanup worker is down
T=0:15 (15 min TTL expired) - but cleanup never ran
T=0:20 Worker comes back up
T=0:21 Manual query shows reserved=100+ (orphaned inventory!)

SOLUTION:
✅ Add stricter WHERE clause + index:

MIGRATION REQUIRED:
CREATE INDEX idx_payment_intents_status_expires_at
ON payment_intents(status, expires_at);

CLEANUP QUERY:
UPDATE event_tiers
WHERE id = ?
AND reserved >= ? ← NEW GUARD
AND expires_at < NOW() ← TTL check
SET reserved = reserved - qty;

IMPLEMENTATION NEEDED:
□ Update migration 000029 to include index
□ Update reservation_cleanup_worker.go with guard

█░ ISSUE #1: DOUBLE RESERVATION (EXPECTED BEHAVIOR)
═══════════════════════════════════════════════════════════════════════════════

PROBLEM:
T=0:00 Request A: Check available = 10 ✓
T=0:01 Request B: Check available = 10 ✓
T=0:02 Request A: UPDATE WHERE (qty-sold-reserved) >= 10 ✓
T=0:03 Request B: UPDATE WHERE (qty-sold-reserved) >= 10 → (0 >= 10)? NO
T=0:04 Request B: Fails with "Insufficient tickets"

UX Issue: User sees "10 available" then gets error

WHY THIS IS EXPECTED (NOT A BUG):
✅ Pre-check is UX hint ONLY
✅ Real gate is DB UPDATE with WHERE clause
✅ Database guarantee: ONLY 10 succeed
✅ This is correct behavior for high-load scenarios

SOLUTION:
✅ Add proper UX error handling:

CLIENT SIDE:
if reservation_fails {
// Don't show generic error
// Show: "Tickets just sold out. Refresh or try another tier."
if err.Code == "insufficient_tickets" {
show_sold_out_message()
suggest_other_tiers()
}
}

CODE COMMENT (Added):
// IMPORTANT: Pre-check is UX hint, not enforcement
// Real gate is atomic DB UPDATE with WHERE clause
// Expected behavior: Multiple rapid requests → only first N succeed
// This is CORRECT for concurrent scenarios

█░ ISSUE #3: TICKET CREATION MODEL (ARCHITECTURAL)
═══════════════════════════════════════════════════════════════════════════════

CURRENT MODEL (HYBRID):
Phase 1 (InitiatePayment):
└─ Only reserve inventory

Phase 4 (PaymentWorker):
└─ Create tickets from reservation

Complexity Issues:
❌ Need ticket matching logic (hard when async)
❌ Handle duplicate ticket generation risk
❌ Reconciliation harder (tickets ≠ reservations)

RECOMMENDATION: CURRENT APPROACH IS CLEAN

✅ Option B (what we're doing): Cleanest - Phase 1: Reserve ONLY (no ticket creation) - Phase 4: Create tickets AFTER payment confirmed

Benefits:
✓ No orphaned tickets from failed payments
✓ Single source of truth: payment status = ticket status
✓ No duplicate logic needed
✓ Easy reconciliation

Status: ✅ ALREADY CORRECTLY IMPLEMENTED
Just needs documentation

═══════════════════════════════════════════════════════════════════════════════
SUMMARY OF REQUIRED CHANGES
═══════════════════════════════════════════════════════════════════════════════

1. ✅ State Machine (state_machine.go created)
   → Integrate into payment_worker.go before state update

2. ⚠️ Idempotency Gate (payment_worker.go)
   → Check webhook_events.status BEFORE row lock
   → Prevents duplicate execution

3. ⚠️ Cleanup Guard + Index (migration 000029)
   → Add WHERE reserved >= qty
   → Add composite index on (status, expires_at)

4. ✅ Pre-check Documentation
   → Add code comments explaining UX vs DB enforcement
   → Add client-side error handling

5. ✅ Ticket Model Documentation
   → Document Option B (reserve → create) as chosen model
   → Already correctly implemented

\*/
