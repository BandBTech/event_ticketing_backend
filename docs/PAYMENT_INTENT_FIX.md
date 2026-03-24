# PaymentIntent Creation Fix - Complete Solution

## Problem

- Only 1 PaymentIntent record in database despite 115+ transactions
- PaymentIntent was never created during Stripe purchase flow (guest or logged-in)
- No direct link between transactions and purchase intents

## Root Cause

The Stripe purchase flow was missing PaymentIntent creation:

1. `InitiatePaymentGatewayPurchase()` (guest purchases) - Created checkout session, but NOT PaymentIntent
2. `InitiateUserPaymentGatewayPurchase()` (logged-in users) - Same issue
3. Payment worker only updated existing PaymentIntent, didn't create new ones

## Solution Implemented

### 1. Added PaymentIntent Field to CheckoutSession Model

**File:** `internal/models/guest_user.go`

- Added `PaymentIntentID *uuid.UUID` field to link checkout session to payment intent
- Added foreign key relationship to PaymentIntent

### 2. PaymentIntent Creation in Guest Purchase Flow

**File:** `internal/services/ticket_service.go` - `InitiatePaymentGatewayPurchase()` (Line ~2010)

- Creates PaymentIntent record BEFORE Stripe checkout
- Stores all pricing breakdown, commission info
- Links to checkout session
- Status: "pending" (updated to "succeeded" when webhook arrives)

### 3. PaymentIntent Creation in Logged-In User Flow

**File:** `internal/services/ticket_service.go` - `InitiateUserPaymentGatewayPurchase()` (Line ~2010)

- Same logic as guest flow but with user context
- Properly links user ID instead of guest user ID

### 4. Updated Payment Worker

**File:** `internal/workers/payment_worker.go`

- Loads PaymentIntent by `checkout_token` (before webhook had only gateway payment ID)
- Updates PaymentIntent with `gateway_payment_id` when webhook arrives
- Updates checkout session gateway_data with payment_intent_id
- Transaction creation already links to PaymentIntent via `transaction.PaymentIntentID`

### 5. Database Migration

**Files:**

- `migrations/000026_add_payment_intent_id_to_checkout_sessions.up.sql`
- `migrations/000026_add_payment_intent_id_to_checkout_sessions.down.sql`

Adds `payment_intent_id` column to `checkout_sessions` table with foreign key to `payment_intents`.

## Data Flow Now

```
Purchase Flow:
┌─ Guest/Logged-in initiates purchase
│
├─ CreateCheckoutSession (in DB transaction)
│  ├─ Creates tickets (pending_payment status)
│  ├─ Reserves tier inventory
│  └─ Creates PaymentIntent (status: pending)
│
├─ UpdateCheckoutSession with PaymentIntentID
│
├─ Call Stripe API (initializeGatewayData)
│  ├─ Creates Stripe checkout session
│  ├─ Stripe generates payment_intent_id
│  └─ Stores in checkout session GatewayData
│
└─ Return checkout URL to frontend

Payment Flow:
┌─ User pays on Stripe
│
├─ Stripe webhook arrives
│  ├─ Verifies signature
│  └─ Enqueues payment task
│
├─ Payment Worker receives webhook
│  ├─ Loads PaymentIntent by checkout_token
│  ├─ Updates with gateway_payment_id (Stripe PI ID)
│  ├─ Confirms reservation
│  ├─ Creates tickets (status: confirmed)
│  ├─ Updates PaymentIntent (status: succeeded)
│  ├─ Creates single Transaction record
│  └─ Links Transaction to PaymentIntent
│
└─ Frontend can call /payment/success to get confirmation

Analytics Flow:
┌─ Dashboard queries transactions by payment_intent_id
├─ Can now properly count earnings per purchase
└─ Transactions always have PaymentIntent link
```

## How to Apply Changes

### 1. Update Database

```bash
cd /Users/aashbinsunar/Desktop/bandbtech/timro_ticket_system/event_ticketing_backend
make migrate-up
```

Or manually run the migration:

```bash
psql -U postgres -d ticketing_db -f migrations/000026_add_payment_intent_id_to_checkout_sessions.up.sql
```

### 2. Rebuild and Deploy

```bash
go build ./cmd/api
./bin/api
```

### 3. Optional: Backfill Existing Transactions

For existing 115 transactions that don't have PaymentIntent records:

```sql
-- Create PaymentIntent records for existing transactions
INSERT INTO payment_intents (
    id, payment_gateway, idempotency_key, gateway_payment_id,
    user_id, guest_user_id, customer_email,
    event_id, tier_id, quantity,
    currency, currency_symbol, exchange_rate, base_currency, base_currency_amount,
    unit_price, subtotal, platform_fee, gateway_fee, total_amount,
    status, commission_rate, commission_amount, organizer_net_amount,
    created_at, updated_at
)
SELECT
    gen_random_uuid(),
    t.payment_gateway,
    'legacy_' || t.id::text,
    t.gateway_txn_id,
    t.user_id,
    t.guest_user_id,
    COALESCE(gu.email, u.email, 'unknown@example.com'),
    t.event_id,
    (SELECT tier_id FROM tickets WHERE id = t.id LIMIT 1),
    t.quantity,
    t.currency,
    CASE t.currency WHEN 'USD' THEN '$' WHEN 'NPR' THEN 'Rs.' ELSE t.currency END,
    1.0,
    'USD',
    t.amount,
    0,
    t.amount,
    t.commission_amount,
    0,
    t.amount + t.commission_amount,
    'succeeded',
    t.commission_rate,
    t.commission_amount,
    t.organizer_share,
    t.created_at,
    t.updated_at
FROM transactions t
LEFT JOIN guests gu ON t.guest_user_id = gu.id
LEFT JOIN users u ON t.user_id = u.id
WHERE NOT EXISTS (
    SELECT 1 FROM payment_intents pi WHERE pi.id = t.payment_intent_id
)
AND t.status = 'completed';

-- Link existing transactions to new PaymentIntent records
UPDATE transactions t
SET payment_intent_id = pi.id
FROM payment_intents pi
WHERE t.payment_intent_id IS NULL
AND pi.gateway_payment_id = t.gateway_txn_id
AND t.status = 'completed';
```

## Expected Results After Fix

**Before:**

- 115 transactions, 1 payment intent
- ❌ Can't trace purchases to payment intents
- ❌ Analytics broken

**After:**

- 115+ transactions, 115+ payment intents
- ✅ Each purchase creates 1 PaymentIntent at start
- ✅ PaymentIntent tracks from pending → succeeded
- ✅ Transactions linked to PaymentIntent
- ✅ Analytics can properly aggregate by purchase

## Testing the Fix

### Test 1: Guest Purchase Flow

1. Create event tickets
2. Guest makes purchase
3. Check database:
   ```sql
   SELECT id, status, checkout_token, gateway_payment_id
   FROM payment_intents
   WHERE status = 'pending'
   ORDER BY created_at DESC LIMIT 5;
   ```
   Should show multiple pending PaymentIntents for recent purchases

### Test 2: Complete Payment

1. Complete Stripe payment
2. Check database:
   ```sql
   SELECT pi.id, pi.status, pi.gateway_payment_id, t.id, t.status
   FROM payment_intents pi
   LEFT JOIN transactions t ON pi.id = t.payment_intent_id
   WHERE pi.gateway_payment_id IS NOT NULL
   ORDER BY pi.succeeded_at DESC LIMIT 5;
   ```
   Should show succeeded PaymentIntents with linked transactions

### Test 3: Dashboard

1. Organizer dashboard should now show accurate:
   - Total revenue (from transactions)
   - Commission amount (should be > 0)
   - Net earnings (revenue - commission)

## Rollback (if needed)

```bash
# Rollback the migration
make migrate-down

# This will remove the payment_intent_id column from checkout_sessions
```
