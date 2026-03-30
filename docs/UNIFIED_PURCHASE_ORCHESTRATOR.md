# Unified Ticket Purchase Orchestrator - Implementation Guide

**Date Created**: March 30, 2026
**Purpose**: Centralized ticket purchase system for both guest and logged-in users with all payment gateways (Stripe, Cash, etc.)

---

## Architecture Overview

### Previous Architecture (Fragmented)

```
Guest Purchase:
  ├─ Cash → handleCashGuestPurchase()
  └─ Stripe → InitiatePaymentGatewayPurchase()

Logged-in User Purchase:
  ├─ Cash → PurchaseTicket() with cash logic
  └─ Stripe → InitiateUserPaymentGatewayPurchase()

Problems:
  ❌ 4 separate code paths
  ❌ Code duplication
  ❌ Different validation logic
  ❌ Inconsistent email handling
  ❌ Hard to maintain or extend
```

### New Unified Architecture ✅

```
┌─────────────────────────────────────────────────────────────────┐
│          UnifiedPurchaseOrchestrator (Single Entry Point)        │
│                                                                   │
│  ProcessUnifiedPurchase(ctx, UnifiedPurchaseRequest)            │
│                                                                   │
│  1. Normalize & Validate Identity                               │
│  2. Validate Purchase Request                                   │
│  3. Get Currency                                                │
│  4. Route to Payment Handler                                    │
└─────────────────────────────────────────────────────────────────┘
                           ↙                ↘
        ┌──────────────────────┐    ┌──────────────────────┐
        │ processCashPayment() │    │ processStripePayment()
        │                      │    │                      │
        │ ✅ Both user types   │    │ ✅ Both user types   │
        │ ✅ Identical logic   │    │ ✅ Identical logic   │
        └──────────────────────┘    └──────────────────────┘
```

**Benefits:**

- ✅ Single source of truth for all payments
- ✅ Identity data preserved (UserID or GuestUserID)
- ✅ Consistent validation across user types
- ✅ Easy to add new payment gateways
- ✅ Maintainable and testable
- ✅ No code duplication

---

## Key Components

### 1. UnifiedPurchaseRequest (Identity-Aware)

```go
type UnifiedPurchaseRequest struct {
    // Identity (one of these must be provided)
    UserID      *uuid.UUID           // For logged-in users
    GuestUserID *uuid.UUID           // For existing guests

    // Customer Details
    Email       string               // Always required
    FirstName   string               // Optional
    LastName    string               // Optional
    Phone       string               // Optional
    CountryCode string               // Optional

    // Event & Ticket Details
    EventID uuid.UUID
    Tiers   []TicketTierSelection    // Multi-tier support

    // Payment
    PaymentGateway models.PaymentGateway
    Currency       string             // Auto-detect if empty
}
```

**Identity Preservation Logic:**

```
IF UserID != nil:
    → Logged-in user purchase
    → Use UserID for ticket.UserID
    → GuestUserID = nil

ELSE IF GuestUserID != nil:
    → Existing guest purchase
    → Use GuestUserID for ticket.GuestUserID
    → UserID = nil

ELSE IF Email != "":
    → New guest purchase
    → Create GuestUser or find by email
    → GuestUserID = uuid
    → UserID = nil
```

### 2. UnifiedPurchaseResponse (Gateway-Agnostic)

```go
type UnifiedPurchaseResponse struct {
    CheckoutToken       string                  // Unique identifier

    // Identity (preserved from request)
    UserID              *uuid.UUID              // Set if logged-in
    GuestUserID         *uuid.UUID              // Set if guest
    Email               string

    // Payment Details
    PaymentGateway      string
    Amount              float64
    Currency            string
    Status              string                  // "completed" or "pending"

    // Ticket Info
    TicketCount         int

    // Completion Status
    ImmediateCompletion bool   // true=cash, false=gateway
    RedirectURL         string // For Stripe/PayPal redirect

    ExpiresAt           time.Time
}
```

### 3. Quantity Limits (Per User Type)

- **Guests**: Maximum 6 tickets per purchase
- **Logged-in Users**: Maximum 10 tickets per purchase

These limits are enforced **after** identity normalization, so users get consistent behavior.

---

## Usage Examples

### Example 1: Guest Purchase (Cash)

```go
// Handler receives request from frontend
unifiedReq := &services.UnifiedPurchaseRequest{
    Email:          "guest@example.com",
    FirstName:      "John",
    LastName:       "Doe",
    Phone:          "+1234567890",
    CountryCode:    "+1",
    EventID:        eventID,
    Tiers: []TicketTierSelection{
        {TierID: tierID, Quantity: 2},
    },
    PaymentGateway: models.PaymentGatewayCash,
}

// Process via orchestrator
response, err := orchestrator.ProcessUnifiedPurchase(ctx, unifiedReq)

// Response will have:
// - ImmediateCompletion: true (cash done immediately)
// - Status: "completed"
// - GuestUserID: set
// - UserID: nil
```

### Example 2: Logged-in User Purchase (Stripe)

```go
unifiedReq := &services.UnifiedPurchaseRequest{
    UserID:         &userID,              // Logged-in user ID
    Email:          "user@example.com",   // From user database
    FirstName:      "Jane",               // From user database
    LastName:       "Smith",
    EventID:        eventID,
    Tiers: []TicketTierSelection{
        {TierID: tierID1, Quantity: 2},
        {TierID: tierID2, Quantity: 1},  // Multi-tier supported
    },
    PaymentGateway: models.PaymentGatewayStripe,
}

response, err := orchestrator.ProcessUnifiedPurchase(ctx, unifiedReq)

// Response will have:
// - ImmediateCompletion: false (Stripe pending)
// - Status: "pending"
// - RedirectURL: Stripe checkout URL
// - UserID: set
// - GuestUserID: nil
```

### Example 3: Existing Guest Converting to User

```go
// Guest already purchased before
guestUserID := existingGuestID

unifiedReq := &services.UnifiedPurchaseRequest{
    UserID:         &newUserID,           // Converted to user
    GuestUserID:    &guestUserID,         // Link to previous purchase
    Email:          "user@example.com",
    EventID:        eventID,
    Tiers: []TicketTierSelection{
        {TierID: tierID, Quantity: 1},
    },
    PaymentGateway: models.PaymentGatewayStripe,
}

response, err := orchestrator.ProcessUnifiedPurchase(ctx, unifiedReq)
```

---

## Handler Integration

### Payment Handler (For logged-in users)

```go
type PaymentHandler struct {
    unifiedPurchaseOrchestrator *services.UnifiedPurchaseOrchestrator
    // ... other fields
}

func (h *PaymentHandler) InitiatePayment(c *gin.Context) {
    var req services.InitiatePaymentRequest
    c.ShouldBindJSON(&req)

    // Get logged-in user from context
    userID := c.Get("userID").(uuid.UUID)

    // Convert to unified request
    unifiedReq := &services.UnifiedPurchaseRequest{
        UserID:         &userID,
        Email:          req.CustomerEmail,
        EventID:        req.EventID,
        Tiers:          /* convert single tier to slice */,
        PaymentGateway: models.PaymentGateway(req.PaymentGateway),
    }

    // Process via orchestrator
    response, err := h.unifiedPurchaseOrchestrator.ProcessUnifiedPurchase(ctx, unifiedReq)
}
```

### Public Handler (For guests)

```go
type PublicHandler struct {
    unifiedPurchaseOrchestrator *services.UnifiedPurchaseOrchestrator
    // ... other fields
}

func (h *PublicHandler) PurchaseTicketAsGuest(c *gin.Context) {
    var req models.GuestPurchaseRequest
    c.ShouldBindJSON(&req)

    // Convert directly to unified request
    unifiedReq := &services.UnifiedPurchaseRequest{
        Email:          req.Email,
        FirstName:      req.FirstName,
        LastName:       req.LastName,
        Phone:          req.Phone,
        CountryCode:    req.CountryCode,
        EventID:        req.EventID,
        Tiers:          req.Tiers,
        PaymentGateway: req.PaymentGateway,
        // UserID and GuestUserID omitted (nil)
    }

    // Process via orchest rator
    response, err := h.unifiedPurchaseOrchestrator.ProcessUnifiedPurchase(ctx, unifiedReq)
}
```

---

## Payment Gateway Implementation

### Adding a New Payment Gateway

All gateway logic is centralized in the orchestrator. To add PayPal/Khalti:

1. **Add payment method to enum** (`models/payment.go`):

```go
const (
    PaymentGatewayStripe = PaymentGateway("stripe")
    PaymentGatewayCash   = PaymentGateway("cash")
    PaymentGatewayPayPal = PaymentGateway("paypal")  // NEW
    PaymentGatewayKhalti = PaymentGateway("khalti")  // NEW
)
```

2. **Add handler in orchestrator**:

```go
func (uo *UnifiedPurchaseOrchestrator) ProcessUnifiedPurchase(...) {
    switch req.PaymentGateway {
    case models.PaymentGatewayCash:
        return uo.processCashPayment(...)
    case models.PaymentGatewayStripe:
        return uo.processStripePayment(...)
    case models.PaymentGatewayPayPal:
        return uo.processPayPalPayment(...)  // NEW
    case models.PaymentGatewayKhalti:
        return uo.processKhaltiPayment(...)  // NEW
    default:
        return nil, fmt.Errorf("unsupported gateway")
    }
}
```

3. **Implement gateway handler** (follows same pattern for all user types):

```go
func (uo *UnifiedPurchaseOrchestrator) processPayPalPayment(
    ctx context.Context,
    userID *uuid.UUID,
    guestUserID *uuid.UUID,
    customerEmail string,
    req *UnifiedPurchaseRequest,
    currency string,
) (*UnifiedPurchaseResponse, error) {
    // 1. Create reservation
    // 2. Create checkout session
    // 3. Initialize PayPal session
    // 4. Return response with redirect URL
    // Same identical pattern for both user types!
}
```

---

## Flow Diagrams

### Cash Payment Flow

```
┌─────────────────────────────┐
│ ProcessUnifiedPurchase()    │
│ (Identity: UserID or Guest) │
└──────────────┬──────────────┘
               │
               ↓
┌──────────────────────────────┐
│ processCashPayment()         │
│ Single implementation        │
│ Works for both user types    │
└──────────────┬───────────────┘
               │
        ┌──────┼──────┐
        ↓             ↓
   ┌─────────┐  ┌─────────────┐
   │ Lock    │  │ Validate    │
   │ Tiers   │  │ Quantities  │
   └────┬────┘  └─────────────┘
        │
        ↓
   ┌─────────────────────────┐
   │ Get/Create User         │
   │ Logged-in: find by ID   │
   │ Guest: create or find   │
   └────────────┬────────────┘
                │
                ↓
   ┌─────────────────────────┐
   │ Create Tickets (active) │
   │ Create Transaction      │
   │ Queue Emails            │
   └────────────┬────────────┘
                │
                ↓
   ┌─────────────────────────┐
   │ Return Response         │
   │ ImmediateCompletion:true│
   │ Status: completed       │
   │ UserID/GuestUserID set  │
   └─────────────────────────┘
```

### Stripe Payment Flow

```
┌─────────────────────────────┐
│ ProcessUnifiedPurchase()    │
│ (Identity: UserID or Guest) │
└──────────────┬──────────────┘
               │
               ↓
┌──────────────────────────────┐
│ processStripePayment()       │
│ Single implementation        │
│ Works for both user types    │
└──────────────┬───────────────┘
               │
        ┌──────┼──────┐
        ↓             ↓
   ┌─────────┐  ┌──────────────┐
   │ Lock    │  │ Validate     │
   │ Tiers   │  │ Quantities   │
   └────┬────┘  └──────────────┘
        │
        ↓
   ┌────────────────────────────┐
   │ Create Reservation         │
   │ (TicketReservation table)  │
   │ 15-minute TTL              │
   └────────────┬───────────────┘
                │
                ↓
   ┌────────────────────────────┐
   │ Create PaymentIntent       │
   │ Create CheckoutSession     │
   └────────────┬───────────────┘
                │
                ↓
   ┌────────────────────────────┐
   │ Initialize Stripe Session  │
   │ (via initializeGatewayData)│
   │ Get Stripe Checkout URL    │
   └────────────┬───────────────┘
                │
                ↓
   ┌────────────────────────────┐
   │ Return Response            │
   │ ImmediateCompletion: false │
   │ Status: pending            │
   │ RedirectURL: Stripe URL    │
   │ UserID/GuestUserID set     │
   └────────────────────────────┘
                │
                ↓
        (Frontend redirects to Stripe)
                │
                ↓
        (Webhook or Callback)
                │
                ↓
   ┌────────────────────────────┐
   │ ProcessPaymentSuccess()    │
   │ Create actual tickets      │
   │ Create Transaction         │
   │ Queue Emails               │
   │ (Same for both user types) │
   └────────────────────────────┘
```

---

## Database Consistency

### Transactional Guarantees

All operations use database transactions with proper locking:

```go
// Cash payments - atomic ticket creation
tx := db.Begin()
defer tx.Rollback()

// Lock tiers with UPDATE lock
for each tier:
    tx.Clauses(Locking{Strength: "UPDATE"}).First(&tier)

// Create tickets
// Update tier.available and tier.sold
// Create transaction
// Create audit logs

tx.Commit() // All-or-nothing
```

### Identity Preservation in Database

```sql
-- Tickets table
CREATE TABLE tickets (
    id UUID PRIMARY KEY,
    user_id UUID,           -- Set for logged-in users
    guest_user_id UUID,     -- Set for guests
    -- user_id XOR guest_user_id (one is NULL, other is set)
    -- Foreign keys ensure referential integrity
    CONSTRAINT user_xor_guest CHECK (
        (user_id IS NOT NULL AND guest_user_id IS NULL) OR
        (user_id IS NULL AND guest_user_id IS NOT NULL)
    )
);

-- Transactions table
CREATE TABLE transactions (
    id UUID PRIMARY KEY,
    user_id UUID,
    guest_user_id UUID,
    -- Same XOR constraint
);

-- PaymentIntent table
CREATE TABLE payment_intents (
    id UUID PRIMARY KEY,
    user_id UUID,
    guest_user_id UUID,
    -- Same XOR constraint
);
```

---

## Comparison: Before vs After

### Before (Fragmented)

```go
// Guest Cash
func (s *TicketService) handleCashGuestPurchase() {
    // ~150 lines of code
}

// Guest Stripe
func (s *TicketService) InitiatePaymentGatewayPurchase() {
    // ~200 lines of code
}

// User Cash
func (s *TicketService) PurchaseTicket() {
    // Contains cash + Stripe logic
    // ~500 lines of code
}

// User Stripe
func (s *TicketService) InitiateUserPaymentGatewayPurchase() {
    // ~200 lines of code
}

Total: ~1000+ lines with duplication
```

### After (Unified)

```go
// ALL purchases (guest + user, cash + Stripe)
func (uo *UnifiedPurchaseOrchestrator) ProcessUnifiedPurchase() {
    // ~100 lines routing logic
}

func (uo *UnifiedPurchaseOrchestrator) processCashPayment() {
    // ~200 lines, works for BOTH user types
}

func (uo *UnifiedPurchaseOrchestrator) processStripePayment() {
    // ~150 lines, works for BOTH user types
}

Total: ~450 lines, NO duplication, FULLY extensible
```

**Code Reduction**: ~55% reduction in duplicated logic

---

## Migration Path

### Step 1: Deploy Orchestrator (Done)

- [x] Create `unified_purchase_orchestrator.go`
- [x] Update PaymentHandler to use orchestrator
- [x] Update PublicHandler to use orchestrator

### Step 2: Deprecate Old Methods

```go
// Mark old functions as deprecated
// Deprecated: Use ProcessUnifiedPurchase via UnifiedPurchaseOrchestrator
func (s *TicketService) InitiatePaymentGatewayPurchase() {
    log.Warn("DEPRECATED: Use UnifiedPurchaseOrchestrator.ProcessUnifiedPurchase()")
}
```

### Step 3: Remove Old Code

- Remove `InitiatePaymentGatewayPurchase()`
- Remove `InitiateUserPaymentGatewayPurchase()`
- Remove `handleCashGuestPurchase()`
- Simplify `PurchaseTicket()` to remove payment logic

### Step 4: Testing

- Update unit tests to test orchestrator
- Integration tests for all gateway types
- E2E tests for guest + user flows

---

## Testing Strategy

### Unit Tests

```go
func TestUnifiedPurchaseOrchestrator_ProcessUnifiedPurchase_GuestCash(t *testing.T) {
    // Guest cash payment
    req := &UnifiedPurchaseRequest{
        Email: "guest@test.com",
        PaymentGateway: models.PaymentGatewayCash,
    }
    resp, err := uo.ProcessUnifiedPurchase(ctx, req)
    assert.Nil(err)
    assert.True(resp.ImmediateCompletion)
    assert.Nil(resp.UserID)
    assert.NotNil(resp.GuestUserID)
}

func TestUnifiedPurchaseOrchestrator_ProcessUnifiedPurchase_UserStripe(t *testing.T) {
    // Logged-in user Stripe payment
    req := &UnifiedPurchaseRequest{
        UserID: &userID,
        PaymentGateway: models.PaymentGatewayStripe,
    }
    resp, err := uo.ProcessUnifiedPurchase(ctx, req)
    assert.Nil(err)
    assert.False(resp.ImmediateCompletion)
    assert.NotNil(resp.UserID)
    assert.Nil(resp.GuestUserID)
}
```

### Integration Tests

```go
func TestIntegration_GuestCashPurchase(t *testing.T) {
    // End-to-end guest cash flow
    // Verify tickets created
    // Verify emails queued
    // Verify transaction recorded
}

func TestIntegration_UserStripePurchase(t *testing.T) {
    // End-to-end logged-in user Stripe flow
    // Verify reservation created
    // Verify checkout session initialized
}
```

---

## Monitoring & Logging

### Key Metrics

```go
// Log entry point
log.Printf("[UNIFIED_PURCHASE] Processing payment - userID=%v, email=%s, event=%s\n",
    userID, customerEmail, req.EventID)

// Log payment type selection
log.Printf("[UNIFIED_PURCHASE] Processing %s payment - method=%s\n",
    userType, req.PaymentGateway)

// Log completion
log.Printf("[UNIFIED_PURCHASE] Payment completed - status=%s, tickets=%d\n",
    resp.Status, resp.TicketCount)
```

### Metrics to Track

- Purchase success rate by gateway
- Average purchase time (cash vs Stripe)
- Guest vs User purchase ratio
- Quantity distribution
- Error rates by gateway

---

## Future Enhancements

### 1. Support Dynamic Quantity Limits

```go
type UnifiedPurchaseRequest struct {
    // ... existing fields
    MaxQuantityOverride *int // Admin can override limits
}
```

### 2. Group Purchase Support

```go
type GroupPurchaseRequest struct {
    PrimaryContact UnifiedPurchaseRequest
    GroupMembers  []UnifiedPurchaseRequest
    // Bulk purchase with team management
}
```

### 3. Refund Integration

```go
func (uo *UnifiedPurchaseOrchestrator) ProcessRefund(
    transactionID uuid.UUID,
    reason string,
) error {
    // Use same identity preservation pattern
}
```

### 4. Payment Plan Support

```go
type UnifiedPurchaseRequest struct {
    PaymentPlan *PaymentPlanConfig
    // e.g., 3 installments over 3 months
}
```

---

## Troubleshooting

### Issue: Identity not preserved

**Cause**: UserID and GuestUserID both nil  
**Solution**: Ensure one is always set via normalizeAndValidateIdentity()

### Issue: Quantity limit error for logged-in users

**Cause**: Using guest limit (6) instead of user limit (10)  
**Solution**: Limits are set AFTER identity normalization, so verify UserID is set

### Issue: Duplicate emails sent

**Cause**: Both synchronous and async email queuing  
**Solution**: For Stripe, only payment_worker queues emails. For cash, orchestrator queues.

---

## Summary

The **Unified Purchase Orchestrator** provides:

✅ **Single entry point** for all purchases  
✅ **Identity preservation** throughout flow  
✅ **No code duplication** across user types  
✅ **Easy gateway extension** (add new payment method in one place)  
✅ **Consistent validation** and error handling  
✅ **Better maintainability** and testing  
✅ **55% code reduction** in payment logic

All future changes to ticket purchase logic go in ONE place.
