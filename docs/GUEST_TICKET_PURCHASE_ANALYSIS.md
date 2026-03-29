# Guest Ticket Purchase Handler - Analysis & Issues

## 1. Handler Function Location

**File:** [internal/handlers/public_handler.go](internal/handlers/public_handler.go#L333)  
**Endpoint:** `POST /api/v1/public/tickets/guest-purchase`  
**Handler:** `PurchaseTicketAsGuest`

## 2. Handler Code Flow

```go
// Lines 333-395
@Router /api/v1/public/tickets/guest-purchase [post]
func (h *PublicHandler) PurchaseTicketAsGuest(c *gin.Context) {
    var req models.GuestPurchaseRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        utils.HandleError(c, err)
        return
    }

    // Validate event purchase eligibility
    if err := h.validateEventPurchaseEligibility(req.EventID, req.Tiers); err != nil {
        utils.HandleError(c, err)
        return
    }

    // Set default values
    if req.FirstName == "" {
        req.FirstName = "Guest"
    }
    if req.LastName == "" {
        req.LastName = "User"
    }

    // Cash payment validation
    if req.PaymentGateway == models.PaymentGatewayCash {
        cfg, err := config.Load()
        if err != nil {
            utils.HandleError(c, err)
            return
        }
        // Check allowed emails list (if configured)
        if len(cfg.Payment.CashAllowedEmails) > 0 {
            allowed := false
            for _, allowedEmail := range cfg.Payment.CashAllowedEmails {
                if strings.TrimSpace(allowedEmail) == req.Email {
                    allowed = true
                    break
                }
            }
            if !allowed {
                utils.HandleError(c, utils.NewBusinessLogicError("Cash payments not available for this email..."))
                return
            }
        }
    }

    // MAIN SERVICE CALL - Line 382
    tickets, _, checkoutSession, err := h.ticketService.UnifiedGuestPurchase(&req)
    if err != nil {
        utils.HandleError(c, err)
        return
    }

    // ISSUE LOCATION - Line 388-395
    if req.PaymentGateway == models.PaymentGatewayCash {
        utils.SuccessResponse(c, http.StatusCreated,
            fmt.Sprintf("Successfully purchased %d tickets! Confirmation email sent to: %s", len(tickets), req.Email), nil)
        return
    }

    // ⚠️ POTENTIAL NIL DEREFERENCE HERE ⚠️
    utils.SuccessResponse(c, http.StatusCreated,
        "Payment initiated successfully. Please complete payment using the provided gateway data.",
        checkoutSession.ToResponse())  // Line 395
}
```

## 3. Service Methods Called

### 3.1 `UnifiedGuestPurchase()`

**Location:** [internal/services/ticket_service.go](internal/services/ticket_service.go#L1516)

Unified entry point for both cash and gateway guest purchases:

```go
func (s *TicketService) UnifiedGuestPurchase(req *models.GuestPurchaseRequest) ([]*models.Ticket, *models.GuestUser, *models.CheckoutSession, error) {
    // For cash payments: creates tickets immediately
    if req.PaymentGateway == models.PaymentGatewayCash {
        return s.handleCashGuestPurchase(req)
    }

    // For gateway payments: creates checkout session
    checkoutSession, tickets, guestUser, err := s.InitiatePaymentGatewayPurchase(req)
    return tickets, guestUser, checkoutSession, err
}
```

**Returns for cash:** `(tickets, guestUser, nil, nil)`  
**Returns for gateway:** `(tickets, guestUser, checkoutSession, error)`

---

### 3.2 `handleCashGuestPurchase()`

**Location:** [internal/services/ticket_service.go](internal/services/ticket_service.go#L1528)

Handles immediate cash payments. Creates tickets and marks as `"active"` immediately.

**Key operations:**

1. Validates total quantity ≤ 6 tickets
2. Acquires tier-level inventory locks (prevents race conditions)
3. Starts database transaction
4. **Creates or finds guest user** via `createOrFindGuestUser()`
5. Fetches event for ticket number generation
6. For each tier selection:
   - Locks event tier for update
   - Validates tier is active
   - Checks availability
   - **Creates individual tickets** (one per person)
   - **Generates ticket number** using `utils.GenerateEventTicketNumber()`
   - Preloads `Event` data on ticket
   - Updates tier availability (`available - quantity, sold + quantity`)
7. Creates transaction record with status `"completed"`
8. Associates tickets with transaction
9. Commits transaction
10. Queues confirmation email via `emailQueueService.QueueGuestTicketConfirmationEmail()`
11. **Returns:** `(allTickets, guestUser, nil, nil)`

---

### 3.3 `InitiatePaymentGatewayPurchase()`

**Location:** [internal/services/ticket_service.go](internal/services/ticket_service.go#L2103)

Handles deferred gateway payments (Stripe, PayPal, etc.). Creates checkout session for frontend to complete payment.

**Key operations:**

1. Validates total quantity ≤ 6 tickets
2. Acquires tier-level inventory locks
3. Creates/finds guest user
4. Creates **pending** tickets with status `"pending_payment"` (NOT active yet)
5. Creates checkout session with status `"pending"`
6. Creates PaymentIntent record for tracking
7. **Calls `initializeGatewayData()`** to initialize gateway-specific data (Stripe API call, etc.)
   - **If this fails, performs cleanup:**
     - Marks tickets as cancelled
     - Deletes checkout session
     - **Returns:** `(nil, nil, nil, error)`
8. Preloads associations on tickets
9. **Returns:** `(checkoutSession, tickets, guestUser, nil)`

---

## 4. Ticket Model Fields

**Location:** [internal/models/ticket.go](internal/models/ticket.go#L1)

### Critical Fields for Guest Purchase:

```go
type Ticket struct {
    ID              uuid.UUID      // Primary key
    TicketNumber    string         // ✅ MUST be set before Create() (validated in BeforeCreate hook)
    UserID          *uuid.UUID     // nil for guest purchases
    GuestUserID     *uuid.UUID     // ✅ Set to &guestUser.ID for guest purchases
    EventID         uuid.UUID      // ✅ Required
    TierID          uuid.UUID      // ✅ Required
    TransactionID   *uuid.UUID     // Set after transaction creation
    PaymentStatus   string         // default: "pending" (database default)
    TotalAmount     float64        // ✅ Required (tier price)
    PaymentGateway  PaymentGateway // ✅ Required (cash, stripe, etc.)
    Status          string         // ✅ Required
                                   // Cash: "active"
                                   // Gateway: "pending_payment"
    IsGuestPurchase bool           // ✅ true for guest purchases
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

// VALIDATION HOOK - BeforeCreate()
func (t *Ticket) BeforeCreate(tx *gorm.DB) error {
    if t.TicketNumber == "" {
        return fmt.Errorf("ticket_number is required: use utils.GenerateEventTicketNumber to generate it")
    }
    return nil
}
```

---

## 5. IDENTIFIED ISSUES

### 🔴 Issue #1: Nil Pointer Dereference in Handler (Line 395)

**Severity:** HIGH - Causes 500 errors on specific payment gateway failures

**Location:** [internal/handlers/public_handler.go](internal/handlers/public_handler.go#L395)

**Problem:**

```go
// No nil check before calling ToResponse()
utils.SuccessResponse(c, http.StatusCreated,
    "Payment initiated successfully...",
    checkoutSession.ToResponse())  // PANIC if checkoutSession is nil!
```

**When It Occurs:**

1. For cash payments: `checkoutSession` is nil, but handler returns early (safe)
2. For gateway payments: If `InitiatePaymentGatewayPurchase()` has a logic error and returns `(nil, nil, nil, error)`, error is caught
3. **Edge case:** If there's a race condition or logic bug where `checkoutSession` is nil but `err` is also nil, this would panic with null pointer dereference

**Error Manifestation:**

```
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation]
...
goroutine ... [running]:
models.(*CheckoutSession).ToResponse(...)
```

---

### 🔴 Issue #2: Missing Nil Check in CheckoutSession.ToResponse()

**Severity:** MEDIUM - Vulnerable to nil receiver

**Location:** [internal/models/guest_user.go](internal/models/guest_user.go#L109)

**Current Code:**

```go
func (cs *CheckoutSession) ToResponse() CheckoutSessionResponse {
    return CheckoutSessionResponse{
        ID:             cs.ID,           // ❌ nil dereference if cs is nil
        CheckoutToken:  cs.CheckoutToken,
        PaymentGateway: cs.PaymentGateway,
        Amount:         cs.Amount,
        Currency:       cs.Currency,
        Status:         cs.Status,
        GatewayData:    cs.GatewayData,
        ExpiresAt:      cs.ExpiresAt,
        CreatedAt:      cs.CreatedAt,
    }
}
```

**Comparison to Ticket.ToResponse()** - Which correctly handles nil:

```go
func (t *Ticket) ToResponse() TicketResponse {
    var guestUserResp *GuestUserResponse
    if t.GuestUser != nil {  // ✅ Proper nil check
        resp := t.GuestUser.ToResponse()
        guestUserResp = &resp
    }
    ...
}
```

---

### 🟡 Issue #3: Indirect Nil Dereference Path in GetCheckoutSession Handler

**Severity:** LOW - Unlikely to occur with proper service implementation

**Location:** [internal/handlers/public_handler.go](internal/handlers/public_handler.go#L760)

**Related Code:**

```go
func (h *PublicHandler) GetCheckoutSession(c *gin.Context) {
    checkoutToken := c.Param("checkout_token")

    checkoutSession, err := h.ticketService.GetCheckoutSessionByToken(checkoutToken)
    if err != nil {
        utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
        return
    }

    // No additional nil check on checkoutSession
    utils.SuccessResponse(c, http.StatusOK, "Checkout session retrieved",
        checkoutSession.ToResponse())
}
```

If `GetCheckoutSessionByToken()` somehow returns `(nil, nil)`, this would panic.

---

## 6. Root Cause Analysis

### Why 500 Status Occurs:

1. **Scenario A - Cash Payment (Working):**

   ```
   Request (cash)
   → UnifiedGuestPurchase (cash path)
   → handleCashGuestPurchase()
   → Returns (tickets, guestUser, nil, nil)
   ↓
   Handler checks if req.PaymentGateway == cash
   → Returns early (SAFE - no checkoutSession.ToResponse() call)
   ✅ Success 201 response
   ```

2. **Scenario B - Stripe Payment (Working):**

   ```
   Request (stripe)
   → UnifiedGuestPurchase (gateway path)
   → InitiatePaymentGatewayPurchase()
   → initializeGatewayData() succeeds
   → Returns (checkoutSession, tickets, guestUser, nil)
   ↓
   Handler checks if req.PaymentGateway == cash (false)
   → Calls checkoutSession.ToResponse() (non-nil, works)
   ✅ Success 201 response
   ```

3. **Scenario C - Gateway Failure (Triggers 500):**

   ```
   Request (stripe)
   → UnifiedGuestPurchase (gateway path)
   → InitiatePaymentGatewayPurchase()
   → initializeGatewayData() FAILS (Stripe API error, network timeout, etc.)
   → Cleanup executed, returns (nil, nil, nil, error)
   → Error caught in handler, proper 400/500 response sent
   ✅ Error handled correctly
   ```

4. **Scenario D - EDGE CASE Nil Leak (Causes 500):**
   ```
   Request (stripe)
   → UnifiedGuestPurchase (gateway path)
   → InitiatePaymentGatewayPurchase() logic bug
   → Somehow returns (nil, ..., nil)  <- ERROR but success path
   ↓
   Handler checks if req.PaymentGateway == cash (false)
   → Calls checkoutSession.ToResponse()
   → PANIC: nil pointer dereference
   ❌ Unhandled panic → 500 Internal Server Error
   ```

---

## 7. Data Flow Diagram

```
POST /api/v1/public/tickets/guest-purchase
    ↓
PurchaseTicketAsGuest Handler
    ├─ Parse request (GuestPurchaseRequest)
    ├─ Validate event/tiers
    ├─ Set defaults (FirstName, LastName)
    ├─ If cash: validate email allowlist
    └─ Call UnifiedGuestPurchase
        ↓
    UnifiedGuestPurchase (dispatches based on gateway)
        ├─ CASH PATH:
        │   └─ handleCashGuestPurchase()
        │       ├─ Lock tiers
        │       ├─ Create/find guest user
        │       ├─ Create ACTIVE tickets (status="active")
        │       ├─ Generate ticket numbers
        │       ├─ Update tier availability
        │       ├─ Create completed transaction
        │       ├─ Queue confirmation email
        │       └─ Return (tickets, guestUser, nil, nil)
        │
        └─ GATEWAY PATH:
            └─ InitiatePaymentGatewayPurchase()
                ├─ Create/find guest user
                ├─ Create PENDING tickets (status="pending_payment")
                ├─ Generate ticket numbers
                ├─ Create pending checkout session
                ├─ Create PaymentIntent record
                ├─ Call initializeGatewayData()
                │   ├─ Build gateway-specific data (Stripe params, etc.)
                │   └─ On FAILURE: cleanup & return (nil, nil, nil, error)
                └─ Return (checkoutSession, tickets, guestUser, nil)
                    ↓
                Handler response
                    ├─ If err: 400/500 error response (safe)
                    ├─ If cash: 201 with message (safe, no ToResponse())
                    ├─ If gateway:
                    │   └─ checkoutSession.ToResponse()
                    │       ❌ VULNERABLE: No nil check!
                    └─ 201 with checkout session data
```

---

## 8. Summary of Vulnerabilities

| #   | Issue                      | Severity | Location              | Root Cause                                   | Impact                   |
| --- | -------------------------- | -------- | --------------------- | -------------------------------------------- | ------------------------ |
| 1   | Nil dereference in handler | HIGH     | Line 395              | Missing nil check before `.ToResponse()`     | 500 error on edge cases  |
| 2   | Nil check in ToResponse()  | MEDIUM   | guest_user.go:109     | Pointer receiver method doesn't validate nil | Panic if receiver is nil |
| 3   | Missing validation         | LOW      | public_handler.go:760 | Relies on service properly handling errors   | Potential future bugs    |

---

## 9. Recommended Fixes

### Fix #1: Add Nil Check in Handler (Immediate)

```go
// Line 395 - ADD nil check
if checkoutSession == nil {
    utils.HandleError(c, utils.NewInternalServerError("Failed to initiate payment session", nil))
    return
}
utils.SuccessResponse(c, http.StatusCreated,
    "Payment initiated successfully. Please complete payment using the provided gateway data.",
    checkoutSession.ToResponse())
```

### Fix #2: Add Nil Receiver Check in ToResponse()

```go
func (cs *CheckoutSession) ToResponse() CheckoutSessionResponse {
    if cs == nil {
        return CheckoutSessionResponse{}  // Return empty response
    }
    return CheckoutSessionResponse{
        ID:             cs.ID,
        CheckoutToken:  cs.CheckoutToken,
        PaymentGateway: cs.PaymentGateway,
        Amount:         cs.Amount,
        Currency:       cs.Currency,
        Status:         cs.Status,
        GatewayData:    cs.GatewayData,
        ExpiresAt:      cs.ExpiresAt,
        CreatedAt:      cs.CreatedAt,
    }
}
```

### Fix #3: Defensive Check in GetCheckoutSession Handler

```go
func (h *PublicHandler) GetCheckoutSession(c *gin.Context) {
    checkoutSession, err := h.ticketService.GetCheckoutSessionByToken(checkoutToken)
    if err != nil || checkoutSession == nil {  // Add nil check
        utils.HandleError(c, utils.NewInternalServerError("Checkout session not found", nil))
        return
    }
    utils.SuccessResponse(c, http.StatusOK, "Checkout session retrieved",
        checkoutSession.ToResponse())
}
```

---

## Key Ticket Creation Details for Guest Purchases

### Cash Payment Ticket Creation (handleCashGuestPurchase):

```go
ticket := &models.Ticket{
    GuestUserID:     &guestUser.ID,      // Links to guest user
    EventID:         req.EventID,         // Event reference
    TierID:          tierSelection.TierID,// Tier reference
    TotalAmount:     eventTier.Price,     // Ticket price
    PaymentGateway:  req.PaymentGateway,  // "cash" in this case
    Status:          "active",            // ✅ Immediately active
    IsGuestPurchase: true,                // Marks as guest purchase
    // PaymentStatus: (uses default: "pending" from DB)
}
```

### Gateway Payment Ticket Creation (InitiatePaymentGatewayPurchase):

```go
ticket := &models.Ticket{
    GuestUserID:     &guestUser.ID,
    EventID:         req.EventID,
    TierID:          tierSelection.TierID,
    TotalAmount:     eventTier.Price,
    PaymentGateway:  req.PaymentGateway,  // "stripe", "paypal", etc.
    Status:          "pending_payment",   // ❌ Awaiting payment
    IsGuestPurchase: true,
    // PaymentStatus: (defaults to "pending")
}
```

---

## References

- Handler: [internal/handlers/public_handler.go](internal/handlers/public_handler.go#L333)
- Service: [internal/services/ticket_service.go](internal/services/ticket_service.go#L1516)
- Models: [internal/models/guest_user.go](internal/models/guest_user.go#L28)
- Models: [internal/models/ticket.go](internal/models/ticket.go#L1)
