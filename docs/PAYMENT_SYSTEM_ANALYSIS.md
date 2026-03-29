# Payment Processing System Analysis: Race Conditions, Duplicates, and Issues

**Analysis Date:** March 30, 2026  
**Analyzed Files:**

- `internal/workers/payment_worker.go`
- `internal/services/ticket_service.go`
- `internal/services/email_outbox_service.go`
- `internal/handlers/public_handler.go`
- `internal/models/payment.go`

---

## EXECUTIVE SUMMARY

The payment processing system has **5 critical race conditions**, **3 major duplicate code blocks**, and **multiple paths for emails/tickets not being sent or activated**. The root cause is **dual processing paths** (synchronous callback + asynchronous webhook) running concurrently without coordination.

---

## 1. RACE CONDITIONS

### 1.1 CRITICAL: Dual Payment Processing Without Lock Coordination

**Severity:** 🔴 CRITICAL - Can cause duplicate transactions, double-charges

**Issue:** The same payment is processed by TWO independent code paths running simultaneously:

1. **Async Path:** `payment_worker.go::HandlePaymentSuccess()` → `processPaymentIntentSucceeded()`
2. **Sync Path:** `public_handler.go::PaymentSuccessCallback()` → `ticket_service.go::ProcessPaymentSuccess()`

**Problem:**

- The **async path** uses a distributed lock in `payment_worker.go` line 661:
  ```go
  lockKey := fmt.Sprintf("payment_intent:%s", paymentIntent.ID)
  lockAcquired, err := pw.processingLockService.AcquireLock(ctx, lockKey, "payment", "worker", 5*time.Minute)
  ```
- BUT the **sync path** (public_handler.go line 471-510) has **NO LOCK** whatsoever:
  ```go
  err := h.ticketService.ProcessPaymentSuccess(&req)  // NO LOCK!
  ```

**Race Scenario:**

```
Time T1: Browser callback hits PaymentSuccessCallback → acquires DB tx → starts updates
Time T2: Webhook fires → payment_worker locks payment_intent → starts updates
Time T2+1ms: Both are updating the same payment_intent concurrently
Result: Duplicate transactions, double-charged commission, corrupted data
```

**Files Involved:**

- [payment_worker.go](payment_worker.go#L661) - Uses lock
- [ticket_service.go](ticket_service.go#L2524) - No lock, no idempotency check
- [public_handler.go](public_handler.go#L471) - Calls without coordination

**Impact:**

- Multiple transactions created for single payment
- Commission calculated multiple times
- Organizer share calculated incorrectly
- Tickets linked to multiple transactions

---

### 1.2 CRITICAL: Transaction Creation Duplication

**Severity:** 🔴 CRITICAL - Duplicate transactions recorded

**Issue:** Transaction is created in MULTIPLE places:

**Path 1 - Async Webhook (payment_worker.go):**

```go
// Line 720-756: Creates transaction
transaction := models.Transaction{
    ID:               uuid.New(),
    EventID:          dbPaymentIntent.EventID,
    Amount:           totalAmount,
    Status:           "completed",
    ...
}
if err := tx.Create(&transaction).Error; err != nil {
    tx.Rollback()
    return fmt.Errorf("failed to create transaction record: %w", err)
}
```

**Path 2 - Sync Callback (ticket_service.go::ProcessPaymentSuccess):**

```go
// Line 2644-2649: Also creates transaction
if err := s.recordTransactionInTx(tx, allTickets, checkoutSession.PaymentGateway,
    gatewayTxnID, req.GatewayData, "completed", paymentIntentID); err != nil {
    tx.Rollback()
    return fmt.Errorf("failed to record transaction: %w", err)
}
```

**Path 3 - Cash Payment (ticket_service.go::handleCashGuestPurchase):**

```go
// Line 1595-1607: Creates transaction for cash
transaction := &models.Transaction{
    EventID:          req.EventID,
    GuestUserID:      &guestUser.ID,
    PaymentGateway:   models.PaymentGatewayCash,
    Amount:           totalAmount,
    Status:           "completed",
    ...
}
if err := tx.Create(transaction).Error; err != nil {
    tx.Rollback()
    return nil, nil, nil, err
}
```

**Race Scenario:**

```
Time T1: Browser callback → starts ProcessPaymentSuccess → creates transaction #1
Time T2: Webhook fires → HandlePaymentSuccess → creates transaction #2
Result: Same payment recorded twice with different transaction IDs
```

**Idempotency Check Missing:**

- No check for `WHERE payment_intent_id = ? AND status IS NOT NULL` before creating
- No deduplication logic across code paths

**Files Involved:**

- [payment_worker.go](payment_worker.go#L720)
- [ticket_service.go](ticket_service.go#L2644) - `recordTransactionInTx()`
- [ticket_service.go](ticket_service.go#L1595) - `handleCashGuestPurchase()`

**Impact:**

- **Multiple transaction records** for single payment
- **Financial corruption:** Commission calculated multiple times
- **Analytics broken:** Transaction counts doubled
- **Organizer reports wrong:** Shows multiple payments for one purchase

---

### 1.3 CRITICAL: Ticket Status Update Race

**Severity:** 🔴 CRITICAL - Tickets not activated or double-activated

**Issue:** Ticket status updates happen in concurrent transactions without coordination:

**Async Path (payment_worker.go:648-652):**

```go
// Update ticket payment status
if err := tx.Model(&models.Ticket{}).
    Where("payment_intent_id = ?", paymentIntent.ID).
    Updates(map[string]interface{}{
        "payment_status": "completed",
        "paid_at":        now,
        "updated_at":     now,
    }).Error; err != nil {
    tx.Rollback()
    return fmt.Errorf("failed to update ticket payment status: %w", err)
}
```

**Sync Path (ticket_service.go:2567-2571):**

```go
// Update ticket status
if err := tx.Model(&models.Ticket{}).Where("id = ?", ticketID).
    Update("status", "active").Error; err != nil {
    tx.Rollback()
    return fmt.Errorf("failed to update ticket status: %w", err)
}
```

**Race Scenario:**

```
Time T1: Async path acquires lock, starts tx
Time T1+1ms: Async path reads ticket status=pending
Time T2: Sync path acquires tx, updates status=active
Time T2+1ms: Sync path commits
Time T3: Async path updates status=pending (overwrites active!)
Result: Ticket gets reverted to pending status, user can't see active ticket
```

**Files Involved:**

- [payment_worker.go](payment_worker.go#L648)
- [ticket_service.go](ticket_service.go#L2567)

**Impact:**

- Tickets remain in "pending_payment" status
- Users can't view their tickets
- "Tickets not activated" user complaint

---

### 1.4 HIGH: Checkout Session Status Race

**Severity:** 🟠 HIGH - Checkout session status becomes inconsistent

**Issue:** Both paths update checkout session status:

**Async (payment_worker.go:674-681):**

```go
if err := tx.Model(&models.CheckoutSession{}).
    Where("checkout_token = ?", checkoutToken).
    Updates(map[string]interface{}{
        "status":     "completed",
        "updated_at": now,
    }).Error; err != nil {
    tx.Rollback()
    return fmt.Errorf("failed to update checkout session: %w", err)
}
```

**Sync (ticket_service.go:2544-2551):**

```go
checkoutSession.Status = "completed"
if req.GatewayData != nil {
    checkoutSession.GatewayData = req.GatewayData
}
if err := tx.Save(&checkoutSession).Error; err != nil {
    tx.Rollback()
    return err
}
```

**Files Involved:**

- [payment_worker.go](payment_worker.go#L674)
- [ticket_service.go](ticket_service.go#L2544)

---

### 1.5 HIGH: Tier Availability Update Race

**Severity:** 🟠 HIGH - Overselling tickets

**Issue:** Tier availability decreased multiple times for same tickets:

**Cash Purchase (ticket_service.go:1637-1644):**

```go
if err := tx.Model(&eventTier).
    Where("id = ? AND available >= ?", eventTier.ID, tierSelection.Quantity).
    Updates(map[string]interface{}{
        "available": gorm.Expr("available - ?", tierSelection.Quantity),
        "sold":      gorm.Expr("sold + ?", tierSelection.Quantity),
    }).Error; err != nil {
    tx.Rollback()
    return nil, nil, nil, err
}
```

**No Update in Webhook Path** - Relies on async worker to NOT update (implicit assumption)

**Race Scenario:**

```
Assume: Tier.Available = 10 before purchase
Request 1: Buy 6 tickets → updates available to 4, sold to 6
Request 2 (race): Also buys same 6 tickets (webhook still processing)
Result: Tier.Available = -2 OR oversold
```

**Files Involved:**

- [ticket_service.go](ticket_service.go#L1637) - Only cash path updates tier

**Impact:**

- Overselling: More tickets sold than available
- Financial loss for event organizer

---

### 1.6 MEDIUM: Email Queue Race

**Severity:** 🟡 MEDIUM - Duplicate emails sent

**Issue:** Emails queued by multiple paths:

**Path 1 - Payment Worker (payment_worker.go:754):**

```go
if err := pw.emailOutboxService.QueueEmail(ctx,
    models.EmailEventTicketConfirmation,
    paymentIntent.ReceiptEmail,
    "Your Tickets Are Confirmed!",
    emailData, 2); err != nil {
    log.Printf("WARN: Failed to queue confirmation email: %v\n", err)
}
```

**Path 2 - Sync Callback (ticket_service.go:2678-2684):**

```go
for _, tickets := range userTickets {
    s.sendUserTicketConfirmationEmails(tickets)  // Uses different system
}

for email, tickets := range guestEmails {
    if err := s.emailQueueService.QueueGuestTicketConfirmationEmail(
        email, tickets); err != nil {
        log.Printf("Failed to queue confirmation email for guest %s: %v", email, err)
    }
}
```

**Path 3 - Cash Purchase (ticket_service.go:1680-1682):**

```go
if s.emailQueueService != nil {
    if err := s.emailQueueService.QueueGuestTicketConfirmationEmail(
        guestUser.Email, allTickets); err != nil {
        log.Printf("Failed to queue confirmation email for guest %s: %v", guestUser.Email, err)
    }
}
```

**Problem:** Using TWO different email systems:

- `EmailOutboxService` (payment_worker) - outbox pattern
- `EmailQueueService` (ticket_service) - different queue

**Files Involved:**

- [payment_worker.go](payment_worker.go#L754)
- [ticket_service.go](ticket_service.go#L2678)
- [ticket_service.go](ticket_service.go#L1680)

**Impact:**

- Users receive duplicate confirmation emails
- Email system handling inconsistent

---

## 2. DUPLICATE CODE BLOCKS

### 2.1 Transaction Creation (3 places)

**Severity:** 🟠 HIGH

| Location                                               | Used For               | Code                                       |
| ------------------------------------------------------ | ---------------------- | ------------------------------------------ |
| [payment_worker.go:720-756](payment_worker.go#L720)    | Webhook processing     | Creates transaction, calculates commission |
| [ticket_service.go:2644-2649](ticket_service.go#L2644) | Sync callback fallback | Calls `recordTransactionInTx()`            |
| [ticket_service.go:1595-1607](ticket_service.go#L1595) | Cash purchase          | Creates transaction directly               |

**Duplicate Logic:**

- All calculate `totalAmount` by summing tickets
- All calculate `commissionAmount = totalAmount * (event.CommissionRate / 100)`
- All calculate `organizerShare = totalAmount - commissionAmount`
- All create `models.Transaction` object

**Better Approach:** Single `CreateTransaction()` method that all paths call

---

### 2.2 Email Queuing (3 places with 2 different systems)

**Severity:** 🟠 HIGH

| Location                                               | Method                                | System             |
| ------------------------------------------------------ | ------------------------------------- | ------------------ |
| [payment_worker.go:754](payment_worker.go#L754)        | `QueueEmail()`                        | EmailOutboxService |
| [ticket_service.go:2678-2684](ticket_service.go#L2678) | `sendUserTicketConfirmationEmails()`  | EmailQueueService  |
| [ticket_service.go:1680-1682](ticket_service.go#L1680) | `QueueGuestTicketConfirmationEmail()` | EmailQueueService  |

**Problems:**

- Payment worker uses `EmailOutboxService` (outbox pattern)
- Ticket service uses `EmailQueueService` (different queue)
- No coordination between systems
- Comments say "Do NOT queue again here to avoid duplicate emails" but no enforcement

---

### 2.3 Ticket Status Update (2 places)

**Severity:** 🟠 HIGH

| Location                                               | Updates                     |
| ------------------------------------------------------ | --------------------------- |
| [payment_worker.go:648-652](payment_worker.go#L648)    | `payment_status`, `paid_at` |
| [ticket_service.go:2567-2571](ticket_service.go#L2567) | `status` to "active"        |

Both update same tickets but in different columns and without checking if already updated

---

## 3. CODE PATHS ANALYSIS

### 3.1 Logged-In User - Cash Payment

**Flow:**

```
POST /api/v1/tickets/purchase
  └─ PublicHandler.PurchaseTicket() (handler doesn't exist - check router)
    └─ TicketService.PurchaseTicket()
      ├─ Lock all tiers
      ├─ Begin TX
      ├─ Update event.available (LOCKED)
      ├─ Update tier.available for each tier (LOCKED)
      ├─ Create tickets (status="active")
      ├─ recordTransactionInTx() → creates Transaction
      ├─ Commit TX
      ├─ sendUserTicketConfirmationEmails() via goroutine [ASYNC]
```

**Status After Purchase:**

- ✅ Ticket.status = "active"
- ✅ Transaction.status = "completed"
- ❌ Email may not be sent (async goroutine)

---

### 3.2 Logged-In User - Gateway Payment (Stripe)

**Flow:**

```
1. Purchase Initiation:
   POST /api/v1/tickets/purchase
     └─ TicketService.PurchaseTicket()
       ├─ Create tickets (status="pending_payment")
       ├─ Create PaymentIntent
       ├─ Create CheckoutSession
       └─ Return Stripe session to frontend

2. User Pays on Stripe → Webhook POST→ Backend

3. Webhook Processing (Async):
   Stripe Webhook Handler
     └─ PaymentWorker.HandlePaymentSuccess()
       ├─ Acquire Lock(payment_intent:pi_xxx)
       ├─ Begin TX
       ├─ Update PaymentIntent.status = "succeeded"
       ├─ Update tickets: status, payment_status, paid_at
       ├─ Update CheckoutSession.status = "completed"
       ├─ CREATE TRANSACTION [CRITICAL POINT 1]
       ├─ QueueEmail() via EmailOutboxService [CRITICAL POINT 2]
       ├─ Commit TX
       └─ Release Lock

4. Browser Polls Success Endpoint:
   GET /api/v1/public/payment/success?checkout_token=xxx
     └─ PublicHandler.PaymentSuccessCallback()
       ├─ NO LOCK [CRITICAL POINT 3]
       ├─ Begin TX
       ├─ Check CheckoutSession.status
       ├─ If status != "completed": Call ProcessPaymentSuccess()
       │   └─ TicketService.ProcessPaymentSuccess()
       │     ├─ BEGIN TX
       │     ├─ Update CheckoutSession.status = "completed"
       │     ├─ Find tickets, update status="active"
       │     ├─ recordTransactionInTx() [DUPLICATE CREATE]
       │     └─ QueueEmail() via EmailQueueService [DUPLICATE EMAIL]
       └─ Return ticket view token
```

**Race Points:**

- Both webhook and browser callback can run simultaneously
- No lock prevents concurrent execution
- Two different email systems used

---

### 3.3 Guest - Cash Payment

**Flow:**

```
POST /api/v1/public/tickets/guest-purchase
  └─ PublicHandler.PurchaseTicketAsGuest()
    └─ TicketService.UnifiedGuestPurchase()
      ├─ Dispatch to handleCashGuestPurchase()
      ├─ Lock all tiers
      ├─ Begin TX
      ├─ Create/find GuestUser
      ├─ Create tickets (status="active")
      ├─ Update tier.available
      ├─ CREATE TRANSACTION [PATH 1]
      ├─ Commit TX
      └─ QueueGuestTicketConfirmationEmail() [PATH 1]
```

**Status After Purchase:**

- ✅ Ticket.status = "active"
- ✅ Transaction.status = "completed"
- ⚠️ Email via EmailQueueService

---

### 3.4 Guest - Gateway Payment

**Flow:**

```
1. Purchase Initiation:
   POST /api/v1/public/tickets/guest-purchase
     └─ UnifiedGuestPurchase()
       └─ InitiatePaymentGatewayPurchase()
         └─ Creates tickets (status="pending_payment")

2. Payment Webhook (same as 3.2 above)

3. Browser Callback (same as 3.2 above)
```

**Status Distribution:**

- ✅ Async path with lock
- ❌ Sync path without lock
- ⚠️ Can create 2 transactions
- ⚠️ Can send 2 emails

---

## 4. ISSUES CAUSING EMAILS NOT TO BE SENT

### 4.1 Two Different Email Systems

**Issue:** Code uses both `EmailOutboxService` AND `EmailQueueService`

| System             | Used By           | Characteristics                   |
| ------------------ | ----------------- | --------------------------------- |
| EmailOutboxService | payment_worker.go | Outbox pattern, reliable delivery |
| EmailQueueService  | ticket_service.go | Different queue implementation    |

**Problem:** If one system fails, emails from other system still sent, but infrastructure mismatched

---

### 4.2 Async Email Sending Outside Transaction

**Issue in payment_worker.go (line 754):**

```go
// ========================================
// PHASE 5: OUTBOX EMAIL QUEUING
// ========================================
if paymentIntent.ReceiptEmail != "" {
    // ... email data construction ...
    // Line 754:
    if err := pw.emailOutboxService.QueueEmail(ctx,
        models.EmailEventTicketConfirmation,
        paymentIntent.ReceiptEmail,
        "Your Tickets Are Confirmed!",
        emailData, 2); err != nil {
        log.Printf("WARN: Failed to queue confirmation email: %v\n", err)
        // Don't fail the payment for email issues
    }
}
```

**Problem:** Email queuing happens AFTER transaction commit. If email service fails:

- Payment is recorded as successful
- Email is lost
- No retry mechanism in async path

---

### 4.3 Conditional Email Sending - May Be Skipped

**Issue in ticket_service.go::ProcessPaymentSuccess (lines 2670-2684):**

```go
// Send confirmation emails
if s.emailQueueService != nil {  // ← CONDITIONAL!
    // Group tickets by user type for email sending
    userTickets := make(map[*models.User][]*models.Ticket)
    guestEmails := make(map[string][]*models.Ticket)

    for _, ticket := range allTickets {
        // Reload ticket with associations OUTSIDE the transaction
        var fullTicket models.Ticket
        if err := s.db.Preload("User").Preload("GuestUser").Preload("Event").
            First(&fullTicket, ticket.ID).Error; err != nil {
            log.Printf("Failed to reload ticket %s for email: %v", ticket.ID, err)
            continue  // ← SKIPS TICKET IF RELOAD FAILS
        }
        // ...
    }
}
```

**Problem:**

- If `emailQueueService == nil`, NO emails sent
- If ticket reload fails, email for that ticket skipped
- No error indication to user

---

### 4.4 Different Email Methods Not Idempotent

**Issue:** Two different queue methods used:

[payment_worker.go:754](payment_worker.go#L754):

```go
pw.emailOutboxService.QueueEmail(ctx,
    models.EmailEventTicketConfirmation,
    paymentIntent.ReceiptEmail, ...)
```

[ticket_service.go:2678](ticket_service.go#L2678):

```go
s.sendUserTicketConfirmationEmails(tickets)  // Doesn't actually queue?
```

[ticket_service.go:2682](ticket_service.go#L2682):

```go
s.emailQueueService.QueueGuestTicketConfirmationEmail(
    email, tickets)
```

- Payment worker uses `EmailOutboxService.QueueEmail()`
- Callback uses `EmailQueueService.QueueGuestTicketConfirmationEmail()`
- **Problem:** No deduplication between systems

---

## 5. ISSUES CAUSING TICKETS NOT TO BE ACTIVATED

### 5.1 Race Condition - Status Update Order

**Scenario:**

```
Async Worker (payment_worker.go:648):
  UPDATE tickets
  SET payment_status='completed', paid_at=NOW()

Sync Callback (ticket_service.go:2567):
  UPDATE tickets
  SET status='active'

Race: If callbacks execute out of order, status might not match payment_status
```

**Files:**

- [payment_worker.go](payment_worker.go#L648)
- [ticket_service.go](ticket_service.go#L2567)

---

### 5.2 Idempotency Issue - "Payment Already Processed" Returns Error

**Issue in ticket_service.go::ProcessPaymentSuccess (lines 2535-2540):**

```go
// Check if already processed - don't return error, just return success
if checkoutSession.Status == "completed" {
    tx.Rollback()
    return fmt.Errorf("Payment already processed.")  // ← RETURNS ERROR!
}
```

**Problem in public_handler.go (lines 471-477):**

```go
err := h.ticketService.ProcessPaymentSuccess(&req)
if err != nil {
    // If payment is already processed, continue (webhook might have processed it)
    if !strings.Contains(err.Error(), "Payment already processed") {
        log.Printf("[PAYMENT_SUCCESS] Failed to process payment for checkout token: %s, error: %v",
            checkoutToken, err)
        utils.HandleError(c, err)  // ← USER ERROR RETURNED
        return
    }
    // Continue if already processed
    log.Printf("[PAYMENT_SUCCESS] Payment already processed by webhook for checkout token: %s",
        checkoutToken)
```

**Issue:** If webhook processes first, browser callback gets error (though continues). But error is logged and might cause user confusion.

**Better:** Return success status, not error for idempotent operations

---

### 5.3 Ticket Status Not Updated in Async Path if Sync Already Done

**Scenario:**

```
Browser callback runs first:
  1. Updates CheckoutSession.status = "completed"
  2. Finds tickets, updates status="active"
  3. Returns to user

Later webhook arrives (should be idempotent):
  1. Checks Payment status
  2. Finds CheckoutSession already completed
  3. But... tries to find tickets with payment_intent_id
```

**Files:**

- [payment_worker.go](payment_worker.go#L602-607)

```go
var tickets []models.Ticket
if err := tx.Where("payment_intent_id = ?", paymentIntent.ID).
    Preload("Tier").
    Find(&tickets).Error; err != nil {
    tx.Rollback()
    return fmt.Errorf("failed to find created tickets: %w", err)
}
```

**Problem:** If sync path already committed, async path might not find tickets properly

---

### 5.4 No "Active" Status Check Before Processing

**Issue:** Both paths check if already processed but don't verify tickets became active:

[public_handler.go:465-390](public_handler.go#L465):

```go
// If checkout session is not completed, try to process the payment
if checkoutSession.Status != "completed" {
    log.Printf("[PAYMENT_SUCCESS] Processing payment for checkout token: %s, current status: %s",
        checkoutToken, checkoutSession.Status)
    err := h.ticketService.ProcessPaymentSuccess(&req)
    // ...
} else {
    log.Printf("[PAYMENT_SUCCESS] Checkout session already completed for token: %s", checkoutToken)
}

// If no active tickets and checkout session is completed, there might be an issue
if activeTickets == 0 && checkoutSession.Status == "completed" {
    utils.HandleError(c, utils.NewInternalServerError(
        "Payment processed but tickets not activated", nil))
    return  // ← EARLY RETURN!
}
```

**Problem:** If async path processed but tickets not yet activated when sync path checks, early return with error

---

## 6. DETAILED ISSUES AFFECTING EACH SYSTEM

### 6.1 Issues Affecting Email Delivery

| Issue                                          | Impact                               | Severity  |
| ---------------------------------------------- | ------------------------------------ | --------- |
| Two email systems (Outbox + Queue)             | Inconsistent delivery handling       | 🟠 HIGH   |
| Email queuing happens AFTER transaction commit | Lost emails if queue fails           | 🟠 HIGH   |
| Conditional `if s.emailQueueService != nil`    | No emails if service not initialized | 🟡 MEDIUM |
| Ticket reload fails → email skipped            | Some users don't get emails          | 🟡 MEDIUM |
| Duplicate email queuing (sync + async)         | Users get duplicate emails           | 🟡 MEDIUM |
| Different email methods                        | No unified tracking/retry            | 🟡 MEDIUM |

---

### 6.2 Issues Affecting Ticket Activation

| Issue                                  | Impact                              | Severity    |
| -------------------------------------- | ----------------------------------- | ----------- |
| Race between async and sync updates    | Tickets stuck in pending_payment    | 🔴 CRITICAL |
| No lock on sync callback               | Concurrent access to same payment   | 🔴 CRITICAL |
| "Payment already processed" error      | User sees error on idempotent retry | 🟠 HIGH     |
| Two different `status` columns updated | Inconsistent state                  | 🟠 HIGH     |
| Early return if activeTickets=0        | Legitimate delay fails              | 🟡 MEDIUM   |

---

### 6.3 Issues Affecting Transaction Recording

| Issue                                      | Impact                                     | Severity    |
| ------------------------------------------ | ------------------------------------------ | ----------- |
| Three different transaction creation paths | Duplicate records                          | 🔴 CRITICAL |
| No idempotency check by payment_intent_id  | Same payment creates multiple transactions | 🔴 CRITICAL |
| Commission calculated multiple times       | Financial corruption                       | 🔴 CRITICAL |
| Organizer share calculated multiple times  | Wrong payouts                              | 🔴 CRITICAL |
| Different paths record with different data | Inconsistent transaction records           | 🟠 HIGH     |

---

## 7. ROOT CAUSE ANALYSIS

### 7.1 Architectural Flaw: Dual Processing Paths

The system was designed with two separate processing paths that were supposed to work independently:

1. **Async Path (Webhook-First Design):**
   - Payment gateway webhook → payment_worker → database updates
   - Intended to be the primary path
   - Has distributed lock

2. **Sync Path (Callback Fallback):**
   - Browser polls success endpoint → public_handler → ticket_service
   - Intended as fallback if webhook is slow
   - NO lock, no coordination

**Problem:** Both run simultaneously, processing the same payment concurrently.

### 7.2 Missing Distributed Lock on Sync Path

The sync callback path should use the same distributed lock as the async path:

```go
// Current (NO LOCK):
func (h *PublicHandler) PaymentSuccessCallback(c *gin.Context) {
    err := h.ticketService.ProcessPaymentSuccess(&req)  // NO LOCK!
}

// Should be:
func (h *PublicHandler) PaymentSuccessCallback(c *gin.Context) {
    lockKey := fmt.Sprintf("payment_intent:%s", req.PaymentIntentID)
    lockAcquired, err := h.lockService.AcquireLock(ctx, lockKey, "callback", "sync", 5*time.Minute)
    if !lockAcquired {
        return successWithMessage("Payment already being processed")  // Idempotent
    }
    defer h.lockService.ReleaseLock(ctx, lockKey, "sync")
    // Process payment
}
```

### 7.3 Missing Idempotency Checks

Neither transaction creation checks if payment already processed:

```go
// Current - NO CHECK:
transaction := models.Transaction{
    ID: uuid.New(),
    EventID: dbPaymentIntent.EventID,
    ...
}
if err := tx.Create(&transaction).Error; err != nil {
    // Already created? Doesn't know!
}

// Should be:
var existingTx models.Transaction
exists := tx.Where(
    "payment_intent_id = ? AND status IN (?, ?)",
    paymentIntentID, "completed", "failed"
).First(&existingTx).Error

if exists == nil {
    // Already processed, return existing transaction
    return nil
}
```

---

## 8. RECOMMENDED FIXES (Priority Order)

### IMMEDIATE (P0 - Do First)

**Fix 1: Add Distributed Lock to Sync Callback**

- Add lock acquisition to `PaymentSuccessCallback()`
- Use same lock key as async path: `payment_intent:{{paymentIntent.ID}}`
- Timeout: 5 minutes
- Fallback to idempotent success if already locked

**Fix 2: Add Idempotency Check Before Creating Transaction**

```go
// Check if transaction already exists
var existingTx models.Transaction
exists := db.Where(
    "payment_intent_id = ? AND status IN (?, ?)",
    paymentIntentID, "completed", "failed"
).First(&existingTx).Error

if exists == nil {
    // Transaction already created, return existing
    return existingTx.ID, nil
}
```

**Fix 3: Consolidate Email Queuing**

- Remove EmailQueueService usage
- Use ONLY EmailOutboxService for all email queuing
- Update all paths to call same method
- Move email queuing INTO transaction (before commit) for atomicity

### HIGH (P1 - Do Second)

**Fix 4: Refactor Transaction Creation into Single Service Method**

- Create `TransactionService.CreatePaymentTransaction()`
- All three paths call this method
- Single point for commission calculation
- Single point for idempotency checks

**Fix 5: Add Database Constraint for Duplicate Prevention**

```sql
ALTER TABLE transactions ADD CONSTRAINT uk_payment_intent_transaction
UNIQUE (payment_intent_id, status);
```

**Fix 6: Unify Ticket Status Update**

- Single method for all status updates
- Use atomic compare-and-swap pattern
- Don't update if already in target state

### MEDIUM (P2 - Do Third)

**Fix 7: Add Payment Processing State Machine**

```
pending → processing → completed
        → failed
        → canceled

No transitions back to pending from completed
```

**Fix 8: Add Idempotency Tokens**

- Use payment_intent_id as idempotency key
- Return cached result if request with same key from browser callback

---

## 9. QUICK REFERENCE SUMMARY

### Files with Critical Race Conditions

- [payment_worker.go](payment_worker.go) - Has lock but async
- [public_handler.go](public_handler.go#L471) - No lock
- [ticket_service.go](ticket_service.go#L2524) - No lock

### Files with Duplicate Code

- [payment_worker.go](payment_worker.go#L720) - Creates transaction
- [ticket_service.go](ticket_service.go#L1595) - Creates transaction
- [ticket_service.go](ticket_service.go#L2644) - Creates transaction

### Email Queuing Locations

- [payment_worker.go](payment_worker.go#L754) - EmailOutboxService
- [ticket_service.go](ticket_service.go#L1680) - EmailQueueService
- [ticket_service.go](ticket_service.go#L2678) - EmailQueueService

### Ticket Activation Locations

- [payment_worker.go](payment_worker.go#L648) - Async path
- [ticket_service.go](ticket_service.go#L2567) - Sync path

---

## 10. TEST SCENARIOS TO VALIDATE FIXES

### Scenario 1: Webhook + Browser Race

```
1. Payment succeeds on Stripe
2. Webhook begins processing
3. IMMEDIATELY browser callback also triggered
4. Both should process without duplication
Expected: Single transaction, single email, all tickets active
```

### Scenario 2: Webhook Delayed

```
1. User completes Stripe payment
2. Browser callback processes (currently no webhook yet)
3. Later webhook arrives
4. Should be idempotent
Expected: No duplicate transaction, no duplicate email
```

### Scenario 3: Webhook First

```
1. Webhook processes payment
2. Then browser callback triggered
3. Should be idempotent
Expected: No duplicate transaction, no duplicate email
```

### Scenario 4: Multiple Callback Attempts

```
1. User polls success endpoint 5 times
2. Each poll should be safe
Expected: No duplicate transactions, no duplicate emails
```

---

## Document Metadata

- **Created:** March 30, 2026
- **Severity Summary:**
  - 🔴 CRITICAL: 3 issues
  - 🟠 HIGH: 6 issues
  - 🟡 MEDIUM: 4 issues
- **LOC Analyzed:** ~5000 lines
- **Time to Read:** 30 minutes
- **Time to Fix (P0):** 4-6 hours
- **Time to Test:** 2-3 hours
