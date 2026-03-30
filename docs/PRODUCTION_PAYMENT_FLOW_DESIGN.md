# Production-Grade Event Ticketing Payment Flow Design

**Version**: 1.0  
**Date**: March 30, 2026  
**Principles**: PostgreSQL as source of truth • Reservations are temporary (15-min TTL) • Webhook = final authority • Everything idempotent • Atomic seat deduction

---

## Table of Contents

1. [Core Principles](#core-principles)
2. [System Components](#system-components)
3. [Data Model](#data-model)
4. [Complete Lifecycle Flow](#complete-lifecycle-flow)
5. [Concurrency & Locking Strategy](#concurrency--locking-strategy)
6. [Idempotency Mechanisms](#idempotency-mechanisms)
7. [Failure Scenarios & Recovery](#failure-scenarios--recovery)
8. [Race Condition Prevention](#race-condition-prevention)
9. [Scalability Considerations](#scalability-considerations)

---

## Core Principles

### 1. PostgreSQL is Source of Truth

- All critical state lives in PostgreSQL
- Redis acts as auxiliary cache/lock manager only
- No business logic depends on Redis state persistence
- Database provides ACID guarantees

### 2. Reservations are Temporary (TTL)

- Created when user selects tickets
- 15-minute expiry time
- Background worker or migration cleanup process removes expired entries
- Released inventory automatically when TTL expires

### 3. Webhook is Final Authority (3-Webhook System)

**Three Critical Stripe Webhooks** (minimum set):

| Webhook Event                   | Authority          | Purpose                                            | Queue    |
| ------------------------------- | ------------------ | -------------------------------------------------- | -------- |
| `payment_intent.succeeded`      | ✅ Source of Truth | Payment confirmed → create tickets + transaction   | CRITICAL |
| `payment_intent.payment_failed` | ✅ Source of Truth | Payment failed → release reservation               | DEFAULT  |
| `charge.refunded`               | ✅ Source of Truth | Customer refunded → cancel tickets + mark refunded | CRITICAL |

**Key Rules:**

- Stripe webhooks are the ONLY authority on payment status
- Browser callback/redirect is informational only
- Frontend **never** finalizes orders based on redirect alone
- All three events are processed idempotently
- If webhook fails to deliver: system automatically retries with exponential backoff
- If webhook replayed: zero duplicate effects (idempotent guarantee)

### 4. Everything Must Be Idempotent

- Same webhook event processed multiple times = same result
- Checkout_token uniqueness prevents duplicate orders
- Payment_intent_id uniqueness prevents duplicate transactions
- Timestamps + composite keys detect replay attempts

### 5. Seat Deduction Must Be Atomic

- Inventory updates happen in single database transaction
- Locking prevents overselling under concurrent load
- Row-level pessimistic locks on tier inventory

### 6. Tickets ONLY Created After Webhook Confirms Payment

- **NO tickets created during reservation phase**
- **NO tickets created during checkout session creation**
- **NO tickets created in frontend success page**
- **Tickets created EXCLUSIVELY in webhook processor AFTER payment_intent.succeeded**
- Browser callback/success page is informational only — does not create tickets
- If webhook fails to deliver: tickets do not exist (user can retry or contact support)
- If webhook retried: tickets created exactly once (idempotent)
- This ensures: payment always precedes ticket issuance

---

## System Components

```
┌─────────────────────────────────────────────────────────┐
│                    Frontend (Web/App)                    │
│           - Ticket Selection UI                          │
│           - Stripe Checkout Form                         │
│           - Success Confirmation (informational only)    │
└──────────────────┬──────────────────────────────────────┘
                   │
        ┌──────────┴──────────┐
        │                     │
┌───────▼─────────┐  ┌────────▼────────┐
│  REST API       │  │  Stripe SDK     │
│  (Browser)      │  │  (Client Key)   │
└────────┬────────┘  └─────────────────┘
         │
┌────────▼────────────────────────────────────────────────┐
│            Backend (Go/Gin Application)                  │
│                                                           │
│  ┌─────────────────────────────────────────────────────┐│
│  │ HTTP Handlers                                        ││
│  │ - CreateReservation                                  ││
│  │ - CreateCheckoutSession                              ││
│  │ - PaymentSuccessCallback                             ││
│  │ - ViewTicket                                         ││
│  └─────────────────────────────────────────────────────┘│
│                                                           │
│  ┌─────────────────────────────────────────────────────┐│
│  │ Business Logic Services                              ││
│  │ - ReservationService (create, validate, expire)      ││
│  │ - CheckoutService (create sessions, generate tokens) ││
│  │ - PaymentService (webhook processing, finalization)  ││
│  │ - TicketService (create orders, finalize tickets)    ││
│  │ - InventoryService (atomic deduction, release)       ││
│  └─────────────────────────────────────────────────────┘│
│                                                           │
│  ┌─────────────────────────────────────────────────────┐│
│  │ Workers (Background Jobs - asynq)                    ││
│  │ - PaymentWorker:                                     ││
│  │   • HandlePaymentSuccess (payment_intent.succeeded)  ││
│  │   • HandlePaymentFailed (payment_intent.payment_failed)
│  │   • HandleChargeRefunded (charge.refunded)           ││
│  │ - ExpiryWorker (cleanup expired reservations)        ││
│  │ - EmailWorker (async email delivery)                 ││
│  └─────────────────────────────────────────────────────┘│
└──────┬──────────────────────────┬───────────────────────┘
       │                          │
       │                          │
   ┌───▼───────────┐      ┌───────▼──────┐
   │  PostgreSQL   │      │    Redis     │
   │  (Source of   │      │  (Auxiliary: │
   │   Truth)      │      │   Locks,     │
   │               │      │   Cache,     │
   │  Tables:      │      │   Queues)    │
   │  - events     │      │              │
   │  - tiers      │      │  Locks By:   │
   │  - tier_inventory    │  - tier_id   │
   │  - reservations      │  - checkout  │
   │  - users       │      │  - event    │
   │  - orders      │      │              │
   │  - order_items │      │ Expire:     │
   │  - transactions       │  - reserv.  │
   │  - webhooks    │      │  - checkout │
   └───────────────┘      └──────────────┘
       ▲
       │
┌──────┴────────────────────────────────────┐
│     Stripe (External Service)              │
│                                             │
│ - Payment Intent Creation                  │
│ - Checkout Session Management              │
│ - Webhook Events (payment_intent.succeeded)│
└─────────────────────────────────────────────┘
```

---

## Data Model

### Critical Tables

```sql
-- Source of Truth for inventory
CREATE TABLE tier_inventory (
  id BIGSERIAL PRIMARY KEY,
  tier_id BIGINT NOT NULL UNIQUE,
  available INT NOT NULL,
  reserved INT NOT NULL DEFAULT 0,
  sold INT NOT NULL DEFAULT 0,
  updated_at TIMESTAMP,
  FOREIGN KEY (tier_id) REFERENCES tiers(id)
);

-- Temporary placeholder for selections
CREATE TABLE reservations (
  id BIGSERIAL PRIMARY KEY,
  checkout_token VARCHAR(255) NOT NULL UNIQUE,
  event_id BIGINT NOT NULL,
  user_id BIGINT,  -- NULL for guests
  guest_email VARCHAR(255),  -- populated for guests
  total_items INT NOT NULL,
  quantity_per_tier JSONB,  -- { "tier_id": quantity }
  status VARCHAR(50) DEFAULT 'active',  -- active, completed, expired, cancelled
  tier_lock_key VARCHAR(255) UNIQUE,  -- Redis lock identifier
  created_at TIMESTAMP DEFAULT NOW(),
  expires_at TIMESTAMP,  -- TTL checkpoint
  completed_at TIMESTAMP,
  FOREIGN KEY (event_id) REFERENCES events(id)
);

-- Checkout session tracking for Stripe
CREATE TABLE checkout_sessions (
  id BIGSERIAL PRIMARY KEY,
  checkout_token VARCHAR(255) NOT NULL UNIQUE,
  event_id BIGINT NOT NULL,
  user_id BIGINT,
  guest_email VARCHAR(255),
  stripe_session_id VARCHAR(255) UNIQUE,  -- Stripe Checkout Session ID
  stripe_payment_intent_id VARCHAR(255),  -- Payment Intent ID (set after payment)
  status VARCHAR(50) DEFAULT 'pending',  -- pending, succeeded, failed, expired
  quantity_per_tier JSONB,
  total_amount BIGINT,  -- in cents
  commission_rate DECIMAL(5,2),
  commission_amount BIGINT,
  organizer_amount BIGINT,
  payment_method VARCHAR(50),  -- stripe, manual, etc.
  created_at TIMESTAMP DEFAULT NOW(),
  completed_at TIMESTAMP,
  expires_at TIMESTAMP,
  FOREIGN KEY (event_id) REFERENCES events(id)
);

-- Confirmed orders (created when webhook succeeds)
CREATE TABLE orders (
  id BIGSERIAL PRIMARY KEY,
  checkout_token VARCHAR(255) NOT NULL UNIQUE,
  event_id BIGINT NOT NULL,
  user_id BIGINT,
  guest_email VARCHAR(255),
  total_items INT NOT NULL,
  total_amount BIGINT,  -- in cents
  commission_amount BIGINT,
  organizer_amount BIGINT,
  order_status VARCHAR(50) DEFAULT 'confirmed',  -- confirmed, refunded, disputed
  created_at TIMESTAMP DEFAULT NOW(),
  FOREIGN KEY (event_id) REFERENCES events(id)
);

-- Individual tickets from an order
CREATE TABLE order_items (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT NOT NULL,
  reservation_id BIGINT,
  tier_id BIGINT NOT NULL,
  ticket_number VARCHAR(50),
  seat_identifier VARCHAR(100),  -- seat number if assigned
  ticket_status VARCHAR(50) DEFAULT 'issued',  -- issued, used, refunded
  created_at TIMESTAMP DEFAULT NOW(),
  FOREIGN KEY (order_id) REFERENCES orders(id),
  FOREIGN KEY (reservation_id) REFERENCES reservations(id),
  FOREIGN KEY (tier_id) REFERENCES tiers(id)
);

-- Transactions recorded from Stripe
CREATE TABLE transactions (
  id BIGSERIAL PRIMARY KEY,
  order_id BIGINT NOT NULL UNIQUE,
  checkout_token VARCHAR(255) NOT NULL UNIQUE,
  stripe_payment_intent_id VARCHAR(255) NOT NULL UNIQUE,
  stripe_charge_id VARCHAR(255),  -- captured charge ID
  gateway_transaction_id VARCHAR(255),  -- payment gateway identifier
  amount BIGINT,  -- in cents
  currency VARCHAR(3),
  payment_method VARCHAR(50),
  payment_status VARCHAR(50),  -- succeeded, failed, refunded
  created_at TIMESTAMP DEFAULT NOW(),
  FOREIGN KEY (order_id) REFERENCES orders(id)
);

-- Webhook events for tracking and replay prevention
CREATE TABLE webhook_events (
  id BIGSERIAL PRIMARY KEY,
  stripe_event_id VARCHAR(255) NOT NULL UNIQUE,
  event_type VARCHAR(100),  -- payment_intent.succeeded, etc.
  stripe_payment_intent_id VARCHAR(255),
  amount BIGINT,
  currency VARCHAR(3),
  metadata JSONB,  -- all Stripe metadata
  webhook_signature VARCHAR(255),
  processed BOOLEAN DEFAULT FALSE,
  processed_at TIMESTAMP,
  error_log TEXT,
  created_at TIMESTAMP DEFAULT NOW(),
  UNIQUE(stripe_event_id, event_type)  -- prevent duplicates
);

-- Inventory ledger for auditing
CREATE TABLE inventory_ledger (
  id BIGSERIAL PRIMARY KEY,
  tier_id BIGINT NOT NULL,
  transaction_type VARCHAR(50),  -- reserve, release, sold, refund
  quantity INT,
  reference_id BIGINT,  -- reservation_id or order_id
  before_available INT,
  after_available INT,
  before_reserved INT,
  after_reserved INT,
  before_sold INT,
  after_sold INT,
  created_at TIMESTAMP DEFAULT NOW(),
  FOREIGN KEY (tier_id) REFERENCES tiers(id)
);

-- Refunds and disputes
CREATE TABLE refunds (
  id BIGSERIAL PRIMARY KEY,
  transaction_id BIGINT NOT NULL,
  order_id BIGINT NOT NULL,
  stripe_refund_id VARCHAR(255) UNIQUE,
  reason VARCHAR(255),
  amount BIGINT,
  refund_status VARCHAR(50),  -- pending, succeeded, failed
  initiated_at TIMESTAMP DEFAULT NOW(),
  completed_at TIMESTAMP,
  FOREIGN KEY (transaction_id) REFERENCES transactions(id),
  FOREIGN KEY (order_id) REFERENCES orders(id)
);

-- Payouts ledger
CREATE TABLE payouts (
  id BIGSERIAL PRIMARY KEY,
  event_id BIGINT NOT NULL,
  organizer_id BIGINT NOT NULL,
  total_gross BIGINT,
  total_commission BIGINT,
  total_net BIGINT,
  refund_hold_window_until TIMESTAMP,  -- holds payout until refund window closes
  stripe_payout_id VARCHAR(255),
  payout_status VARCHAR(50),  -- pending, processing, paid, failed
  created_at TIMESTAMP DEFAULT NOW(),
  completed_at TIMESTAMP,
  FOREIGN KEY (event_id) REFERENCES events(id)
);
```

### Indexes for Performance

```sql
-- Reservation lookup
CREATE INDEX idx_reservations_checkout_token ON reservations(checkout_token);
CREATE INDEX idx_reservations_event_expires ON reservations(event_id, expires_at);
CREATE INDEX idx_reservations_status ON reservations(status);

-- Checkout session lookup
CREATE INDEX idx_checkout_sessions_token ON checkout_sessions(checkout_token);
CREATE INDEX idx_checkout_sessions_pi ON checkout_sessions(stripe_payment_intent_id);
CREATE INDEX idx_checkout_sessions_event ON checkout_sessions(event_id, created_at);

-- Order lookup
CREATE INDEX idx_orders_checkout_token ON orders(checkout_token);
CREATE INDEX idx_orders_user_event ON orders(user_id, event_id, created_at);
CREATE INDEX idx_orders_guest_email ON orders(guest_email, event_id);

-- Webhook deduplication
CREATE INDEX idx_webhook_events_stripe_id ON webhook_events(stripe_event_id);
CREATE INDEX idx_webhook_events_pi_id ON webhook_events(stripe_payment_intent_id);
CREATE INDEX idx_webhook_events_processed ON webhook_events(processed, created_at);

-- Transaction lookup
CREATE INDEX idx_transactions_pi_id ON transactions(stripe_payment_intent_id);
CREATE INDEX idx_transactions_checkout_token ON transactions(checkout_token);

-- Inventory ledger audit
CREATE INDEX idx_inventory_ledger_tier ON inventory_ledger(tier_id, created_at);
```

---

## Quick Reference: When Objects Are Created

| Object                    | Phase 1    | Phase 2    | Phase 3    | Phase 4    | Phase 5 |
| ------------------------- | ---------- | ---------- | ---------- | ---------- | ------- |
| **Reservation**           | ✅ Created | -          | -          | -          | -       |
| **CheckoutSession**       | -          | ✅ Created | -          | -          | -       |
| **Order**                 | ❌ NOT YET | ❌ NOT YET | ❌ NOT YET | ✅ Created | -       |
| **Order Items (Tickets)** | ❌ NOT YET | ❌ NOT YET | ❌ NOT YET | ✅ Created | -       |
| **Transaction**           | -          | -          | -          | ✅ Created | -       |

### Rule: Tickets exist ONLY after webhook succeeds

- If payment succeeds & webhook processes: Tickets created ✅
- If payment succeeds but webhook fails: NO tickets until retry ❌
- If browser redirects to success page: NO tickets created ❌
- If user abandons at Stripe: NO tickets (reservation expires) ✅

---

## Complete Lifecycle Flow

### Phase 1: Ticket Selection & Reservation Creation

**Goal**: Capture user intent and reserve inventory without locking permanently

**Steps**:

1. **User Selects Tickets (Frontend)**
   - User specifies quantity and tier preferences
   - Frontend displays available inventory (from previous API call or real-time)
   - No database changes yet

2. **Generate Checkout Token (Backend)**
   - Create unique `checkout_token = UUID()`
   - Store at application layer (memory/cache) temporarily
   - Token identifies this booking session uniquely
   - TTL: 30 minutes (longer than reservation for UI browsing time)

3. **Create Reservation (HTTP Handler)**
   - **Lock Acquisition**: Acquire distributed lock on tier
     - Redis key: `reserve:<event_id>:<tier_id>`
     - Lock duration: 5 seconds (to account for DB ops)
     - If lock fails after 3 retries with exponential backoff → return error
   - **Validate Event & Tier**
     - Fetch event with FOR UPDATE lock (prevent concurrent event modifications)
     - Fetch tier configuration
     - Return error if event ended or tier removed
   - **Check Inventory** (READ with lock held)
     - SELECT available, reserved FROM tier_inventory WHERE tier_id = ? FOR UPDATE
     - available > 0 OR return HTTP 409 (conflict)
   - **Deduct from Available** (single transaction)
     - BEGIN TRANSACTION
     - INSERT INTO reservations (checkout_token, event_id, user_id, status, expires_at, ...)
     - UPDATE tier_inventory SET available = available - quantity, reserved = reserved + quantity WHERE tier_id = ?
     - INSERT INTO inventory_ledger (... reserve operation ...)
     - COMMIT
   - **Release Lock**
     - Delete Redis lock key
   - **Response to Frontend**
     - Return: checkout_token, reservation_id, expires_at timestamps
     - Frontend stores checkout_token in session/localStorage

**Failure Scenarios**:

- Lock acquisition timeout → Retry with exponential backoff, then error
- Insufficient inventory → HTTP 409, suggest alternative quantity
- Event ended → HTTP 410 (gone)
- DB transaction fails → Automatic rollback, retry handler

**Idempotency**: checkout_token is UNIQUE in database, so duplicate requests:

- If checkout_token exists in reservation → return existing reservation
- If checkout_token exists in order → return error (already completed)

**🎫 TICKETS: NONE CREATED IN PHASE 1**

- Reservation is a placeholder only
- No order_items inserted
- No tickets in database
- Inventory just moved from "available" to "reserved"

---

### Phase 2: Checkout Session Creation

**Goal**: Generate Stripe session without creating final orders

**Steps**:

1. **Validate Reservation (Backend)**
   - Fetch reservation by checkout_token
   - Status must be 'active'
   - expires_at > NOW() (not expired)
   - Return 410 if expired or completed

2. **Calculate Total Amount**
   - Sum all tier prices \* quantities
   - Apply event-level discounts if applicable
   - Calculate commission: total \* (commission_rate / 100)
   - Organizer amount = total - commission
   - All in cents for Stripe

3. **Create Stripe Session/PaymentIntent (Backend)**
   - Call Stripe API with:
     - Amount (in cents, includes commission)
     - Currency, event_id, tier_id
     - **Metadata**:
       - `checkout_token` = checkout_token (critical for webhook lookup)
       - `event_id` = event_id
       - `user_id` = user_id or guest identifier
       - `commission_rate` = commission_rate
       - `gateway_transaction_id` = placeholder (filled on success)
     - Success/Cancel URLs
   - Receive: `stripe_payment_intent_id` and `stripe_session_url`

4. **Store Checkout Session (Backend)**
   - INSERT INTO checkout_sessions:
     - checkout_token (UNIQUE)
     - stripe_payment_intent_id (UNIQUE)
     - event_id, user_id, guest_email
     - total_amount, commission_amount, organizer_amount
     - status = 'pending'
     - expires_at = NOW() + 1 hour (session expiry)

5. **Response to Frontend**
   - Return: stripe_session_url, checkout_token
   - Frontend redirects user to Stripe Hosted Checkout OR displays embedded form

**Important**:

- **No orders are created yet**
- **No tickets are issued yet**
- Reservation holds inventory—if expires after 30 min, inventory released by cleanup worker

---

### Phase 3: Payment Processing (Stripe)

**Goal**: User completes payment; Stripe verifies funds

**Steps**:

1. **User Completes Payment (Stripe)**
   - User enters card details in Stripe
   - Stripe verifies and charges card
   - Stripe returns result to user browser

2. **Stripe Webhook Generated**
   - Stripe creates `payment_intent.succeeded` event
   - Event includes:
     - `id` = stripe_event_id
     - `data.object.id` = payment_intent_id
     - `data.object.metadata.checkout_token` = checkout_token
     - `data.object.amount` = charged amount (in cents)
     - `data.object.charges.data[0].id` = charge_id (actual credit card charge)

3. **Frontend Receives Success**
   - Browser redirected to success URL with payment_intent_id
   - Frontend displays confirmation page (informational only)
   - Frontend does NOT finalize anything yet
   - Frontend can fetch reservation status but cannot create tickets

**🚫 CRITICAL - NO TICKETS CREATED IN THIS PHASE**:

- Frontend success page is informational only
- No orders finalized at this point
- No tickets issued at this point
- **The webhook is the ONLY pathway for ticket creation**
- Webhook is the source of truth, not frontend redirect

---

### Phase 4: Webhook Processing (Source of Truth)

**🎯 THIS IS THE ONLY PLACE TICKETS ARE CREATED**

**Goal**: Idempotently finalize order when Stripe confirms payment

**Critical Design Rule**:

- ✅ Tickets created HERE (inside webhook transaction)
- ❌ NOT created in Phase 1 (reservation)
- ❌ NOT created in Phase 2 (checkout session)
- ❌ NOT created in Phase 3 (frontend success page)
- ❌ NOT created via browser callback
- ❌ NOT created until webhook successfully processes

**Entry Point**: Backend webhook handler receives `payment_intent.succeeded` event from Stripe

**Deterministic Step-by-Step Webhook Flow**:

#### 1. **Webhook Signature Validation**

- Stripe sends X-Stripe-Signature header with request
- Verify signature using Stripe signing secret
- Return HTTP 200 with empty body if invalid signature (don't process)
- Log: `[WEBHOOK_VALIDATION] Signature verified for payment_intent: {id}`

#### 2. **Extract Payment Details**

- Extract from Stripe webhook event:
  - `stripe_event_id` = webhook event ID (unique per Stripe event)
  - `payment_intent_id` = from `event.data.object.id` (pi_xxxxx)
  - `checkout_token` = from `event.data.object.metadata["checkout_token"]` (required)
  - `stripe_charge_id` = from `event.data.object.charges.data[0].id`
  - `amount` = total charged (in cents)
- If checkout_token missing → return HTTP 400 (invalid webhook data)

#### 3. **Acquire Distributed Lock**

- Lock key: `payment_intent:{payment_intent_id}`
- TTL: 5 minutes (to handle long processing)
- Purpose: Prevent concurrent processing of same payment
- If lock already held → return HTTP 409 (retry later)
- Log: `[LOCK_ACQUIRED] Processing payment {payment_intent_id}`

#### 4. **Idempotency Check** (prevent replay attacks) ⭐ CRITICAL

- Query: `SELECT * FROM webhook_events WHERE stripe_event_id = ?`
- If found with `processed = TRUE`:
  - Log: `[IDEMPOTENT] Webhook already processed`
  - Return HTTP 200 (success, don't process again)
- If found with `processed = FALSE`:
  - Continue to process (retry scenario)
- If not found:
  - INSERT webhook_event with `processed = FALSE`
  - Continue processing

#### 5. **Fetch Reservations & Lock**

- Query (with FOR UPDATE lock):
  ```
  SELECT * FROM reservations
  WHERE checkout_token = ?
  AND status = 'reserved'
  FOR UPDATE
  ```
- Verify reservation exists:
  - If not found → log error, return HTTP 410 (reservation doesn't exist)
  - If status ≠ 'reserved' → log, continue (already processed)
  - If expires_at < NOW() → **Expired Reservation Recovery Logic** (see section 5b)
- Log: `[RESERVATION_LOCKED] checkout_token={checkout_token}, quantities={qty_per_tier}`

**5b. Expired Reservation Recovery (if reservation expired)**:

- Search for existing transaction with composite key (within 10-min window):
  - `event_id = ?` + `user_id = ? OR guest_user_id = ?` + `amount ≈ webhook_amount` + `created_at > NOW() - 10min`
- If found:
  - Log: `[IDEMPOTENT_RECOVERY] Found existing transaction {txn_id}`
  - Update: `payment_intent_id = {pi_xxx}` where transaction exists
  - UPDATE webhook_event: `processed = TRUE, status = 'succeeded'`
  - Return HTTP 200 (idempotent success)
- If not found:
  - Log: `[ERROR] Reservation expired, no recovery transaction found`
  - UPDATE webhook_event: `processed = FALSE, status = 'failed'`
  - Return HTTP 422 (unprocessable, requires manual intervention)

#### 6. **Create Single Transaction Record** ⭐ CRITICAL

- Begin atomic transaction: `BEGIN TRANSACTION ISOLATION LEVEL SERIALIZABLE`
- INSERT into transactions table:
  ```
  transaction {
    id = UUID(),
    checkout_token = {checkout_token},
    payment_intent_id = {pi_xxx},
    gateway_txn_id = {stripe_payment_intent_id},
    stripe_charge_id = {charge_id},
    event_id = {reservation.event_id},
    user_id = {reservation.user_id},
    guest_user_id = {reservation.guest_user_id},
    amount = {sum of all tier prices},
    quantity = {total ticket count},
    commission_rate = {event.commission_rate},
    commission_amount = {amount * commission_rate / 100},
    organizer_share = {amount - commission_amount},
    payment_gateway = 'stripe',
    status = 'succeeded',
    processed_at = NOW()
  }
  ```
- Verify INSERT succeeded (get transaction ID)
- Log: `[TRANSACTION_CREATED] txn_id={txn_id}, amount={amount}, tickets={qty}`

#### 7. **Create Individual Tickets** ⭐ CRITICAL - LOOP INSERTION

- FOR EACH tier in reservation.quantity_per_tier:
  - FOR i = 1 TO quantity_in_tier:
    - INSERT into tickets table:
      ```
      ticket {
        id = UUID(),
        event_id = {reservation.event_id},
        tier_id = {tier_id},
        checkout_token = {checkout_token},
        transaction_id = {txn_id},  ← Links to transaction
        user_id = {reservation.user_id},
        guest_user_id = {reservation.guest_user_id},
        ticket_number = GenerateTicketNumber(tier_name, year),
        status = 'valid',           ← Ticket is immediately usable
        payment_status = 'completed',
        paid_at = NOW()
      }
      ```
    - All inserts within same atomic transaction
- Verify all inserts succeeded (RowsAffected = total_quantity)
- Log: `[TICKETS_CREATED] tier_id={tier_id}, created={quantity}, txn_id={txn_id}`

#### 8. **Update Tier Inventory** ⭐ CRITICAL - ATOMIC

- For each tier in purchase:
  ```
  UPDATE tier_inventory
  SET reserved = reserved - quantity,
      sold = sold + quantity
  WHERE tier_id = ? AND available >= 0
  ```
- Verify update succeeded (RowsAffected > 0)
- INSERT into inventory_ledger (for audit):
  - transaction_type = 'sold'
  - before/after counts
- Log: `[INVENTORY_UPDATED] tier={tier_id}, sold_qty={qty}`

#### 9. **Update Reservation Status**

- UPDATE reservations SET status = 'completed', completed_at = NOW()
- Log: `[RESERVATION_COMPLETED] checkout_token={checkout_token}`

#### 10. **Mark Webhook as Processed** ⭐ CRITICAL - IDEMPOTENCY KEY

- UPDATE webhook_events:
  - `processed = TRUE`
  - `processed_at = NOW()`
  - `status = 'succeeded'`
  - `transaction_id = {txn_id}`
  - `payment_intent_id = {pi_xxx}`
- This prevents duplicate processing on webhook replay
- Log: `[WEBHOOK_PROCESSED] stripe_event_id={event_id}, status=succeeded`

#### 11. **Commit Atomic Transaction**

- `COMMIT TRANSACTION`
- If commit fails → ROLLBACK all changes, return HTTP 500
- Log: `[ATOMIC_COMMIT_SUCCESS] All changes persisted`

#### 12. **Post-Commit Operations** (outside transaction):

- Queue confirmation email (async, to outbox/email queue):
  - Include: transaction_id, ticket details, checkout_token
  - Email worker will fetch full details and send asynchronously
- Log transaction finalization:
  - `[ORDER_FINALIZED] checkout_token={token}, txn_id={txn_id}, amount={amount}, tickets={qty}`
- Release distributed lock

#### 13. **Return Response**

- HTTP 200 with empty body (Stripe expects 200 for success)
- Stripe marks webhook as "successfully delivered"
- If any error occurs before commit → HTTP 500/422 (Stripe retries)
- Log: `[WEBHOOK_RESPONSE] HTTP 200, payment_intent={pi_xxx}, txn_id={txn_id}`

---

**Failure Scenarios During Webhook**:

| Scenario                                        | Action                                      | Result                                                  |
| ----------------------------------------------- | ------------------------------------------- | ------------------------------------------------------- |
| Invalid signature                               | Return 400                                  | Webhook not processed (Stripe may retry)                |
| Webhook already processed                       | Return 200                                  | Idempotent: no changes, payment already finalized       |
| Reservation expired (browser callback scenario) | Search for existing transaction             | If found: link and return 200; if not found: return 422 |
| Checkout token not found                        | Log error, return 410                       | Requires manual review                                  |
| Database locks timeout                          | Retry with backoff                          | HTTP 500, Stripe retries in 5 min                       |
| One ticket insert fails                         | ROLLBACK entire transaction                 | No partial state, Stripe retries from scratch           |
| Tier inventory goes negative                    | Log warning, continue                       | Inventory updated, but flagged for audit                |
| Email queue fails                               | Log but don't fail webhook                  | Email retry independent of webhook                      |
| Network dies mid-transaction                    | Transaction rolls back automatically (ACID) | Webhook marked incomplete, Stripe retries               |

---

**Idempotency Guarantees**:

1. **stripe_event_id is UNIQUE in webhook_events**
   - If webhook replayed: lookup finds existing event with processed=TRUE → return HTTP 200

2. **payment_intent_id is UNIQUE in transactions**
   - If webhook replayed: transaction lookup finds existing txn → already processed → return HTTP 200

3. **checkout_token is UNIQUE in reservations**
   - If webhook replayed: reservation already marked 'completed' → skip retry → return HTTP 200

4. **Composite Key for Recovery (expired reservation scenario)**
   - event_id + user_id + amount + 10-min window
   - If reservation expired but transaction exists: link existing txn to webhook and return 200

**Result**: Same webhook event processed 1, 10, or 100 times = identical result (exactly one transaction, exactly one set of tickets, all with proper relationships) ✅

---

1. **Validate Webhook Signature**
   - Stripe sends X-Stripe-Signature header
   - Verify signature with Stripe signing secret
   - Return HTTP 200 with empty body if invalid (tell Stripe to retry, but don't process)

2. **Check Webhook Idempotency** (prevent replay attacks)
   - SELECT \* FROM webhook_events WHERE stripe_event_id = ? LIMIT 1
   - If exists and processed = TRUE → log and return HTTP 200 (already processed)
   - If exists and processed = FALSE → continue (retry scenario)
   - If not exists → INSERT with processed = FALSE

3. **Extract Payment Details**
   - Extract:
     - stripe_event_id
     - payment_intent_id (from metadata)
     - checkout_token (from metadata)
     - amount (total charged)
     - charge_id (captured charge)

4. **Fetch Checkout Session**
   - SELECT \* FROM checkout_sessions WHERE stripe_payment_intent_id = ?
   - If not found → check if transaction already exists:
     - SELECT \* FROM transactions WHERE stripe_payment_intent_id = ?
     - If found → order already completed → UPDATE webhook_events as processed, return HTTP 200
     - If not found → log error, UPDATE webhook_events with error_log, return HTTP 500 (retry)

5. **Fetch & Validate Reservation**
   - SELECT \* FROM reservations WHERE checkout_token = ? FOR UPDATE
   - If not found → log error, return HTTP 500
   - If status = 'completed' → already finalized, return HTTP 200
   - If status = 'expired' OR status = 'cancelled' → inventory was released
     - **Recovery Logic**: Check if transaction exists with same checkout_token
       - If yes → return HTTP 200 (already finalized before expiry)
       - If no → ERROR: cannot restore expired reservation
         - Try to find similar transaction in 10-min window (composite key: event_id, user_id, amount, timestamp)
         - If found → link this webhook to existing transaction, return HTTP 200
         - If not found → return HTTP 422 (unprocessable, mark for manual review)
   - If status = 'active' → validate TTL not exceeded:
     - If expires_at < NOW() → ERROR: reservation expired, follow recovery logic above

6. **Atomic Order Finalization (Single Transaction)**
   - BEGIN TRANSACTION (or use transaction with isolation level SERIALIZABLE)
   - **Lock inventory tier**
     - SELECT \* FROM tier_inventory WHERE tier_id = ? FOR UPDATE (all tiers in reservation)
   - **Verify inventory still available**
     - Confirm reserved count includes this reservation
   - **Create Order**
     - INSERT INTO orders:
       - checkout_token
       - event_id, user_id, guest_email
       - total_items = sum of quantities
       - total_amount, commission_amount, organizer_amount
       - order_status = 'confirmed'
   - **Update Reservation**
     - UPDATE reservations SET status = 'completed', completed_at = NOW() WHERE checkout_token = ?
   - **Update Inventory**
     - UPDATE tier_inventory SET reserved = reserved - quantity, sold = sold + quantity WHERE tier_id = ?
     - INSERT INTO inventory_ledger (tier_id, 'sold', quantity, reserved→sold transition...)
   - **Create Individual Tickets/Order Items** ← _ONLY HAPPENS HERE, AFTER PAYMENT CONFIRMED_
     - FOR each tier with quantity in reservation:
       - FOR i = 1 to quantity:
         - INSERT INTO order_items (order_id, tier_id, ticket_number, ticket_status='issued')
     - All inserts within same transaction
     - Example: 3 tickets purchased → 3 rows inserted with ticket_status='issued'
     - Tickets now exist in database and are valid
   - **Create Transaction Record**
     - INSERT INTO transactions:
       - order_id
       - checkout_token
       - stripe_payment_intent_id (UNIQUE)
       - stripe_charge_id
       - gateway_transaction_id = payment_intent_id
       - amount, currency
       - payment_status = 'succeeded'
   - **Mark Webhook as Processed**
     - UPDATE webhook_events SET processed = TRUE, processed_at = NOW() WHERE stripe_event_id = ?
   - **COMMIT TRANSACTION**

7. **Post-Commit Operations** (outside transaction):
   - Queue confirmation email (async, using Asynq or similar)
     - Include: order_id, checkout_token, ticket details
     - Email worker will fetch necessary details and send
   - Log transaction finalization
     - [ORDER_FINALIZED] checkout_token, order_id, total_amount, tier breakdown

8. **Return Response**
   - HTTP 200 with empty body (Stripe expects 200 for success)
   - Stripe marks webhook as delivered
   - If any error occurs → HTTP 500/422 (Stripe retries after 5 minutes, up to 3 days)

**Failure Scenarios During Webhook**:

- Database connectivity lost → HTTP 500, Stripe retries
- Transaction violates unique constraint → likely duplicate, log and treat as idempotent success
- Reservation expired mid-processing → roll back transaction, attempt recovery logic, if fails return 422
- One ticket insert fails → entire transaction rolls back, Stripe retries
- Email queue fails → log but don't fail webhook (email worker can retry independently)

**Idempotency Guarantee**:

- stripe_payment_intent_id is UNIQUE in transactions table
- If webhook replayed: transaction lookup succeeds, webhook marked as already processed, HTTP 200 returned
- Order is created exactly once, even if webhook delivered twice

---

## Webhook Source of Truth - VISUAL FLOW

```
┌─────────────────────────────────────────────────────────┐
│ User Selects Tickets → Reserves Inventory               │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│ Create Checkout Session (NO tickets created yet)        │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│ User Completes Payment on Stripe                        │
└─────────────────────────────────────────────────────────┘
                         ↓
┌─────────────────────────────────────────────────────────┐
│ Browser Callback Handler                                │
├─────────────────────────────────────────────────────────┤
│ ✅ Fetch checkout session                               │
│ ✅ Mark status = "awaiting_webhook" (NOT "completed")   │
│ ❌ NO tickets created                                   │
│ ❌ NO transaction created                               │
│ ✅ Return HTTP 200 to frontend (informational)          │
└─────────────────────────────────────────────────────────┘
                         ↓
        [Frontend displays success page]
        [Shows "Your tickets are being processed"]
                         ↓
════════════════════════════════════════════════════════════
          STRIPE WEBHOOK DELIVERS EVENT
          (payment_intent.succeeded)
════════════════════════════════════════════════════════════
                         ↓
┌─────────────────────────────────────────────────────────┐
│ Webhook Handler (payment_worker)                        │
├─────────────────────────────────────────────────────────┤
│ STEP 1: Validate Stripe signature                       │
│         ↓                                               │
│ STEP 2: Idempotency check (already processed?)          │
│         ↓ YES → return HTTP 200 (idempotent)            │
│         ↓ NO  → continue                                │
│         ↓                                               │
│ STEP 3: Acquire distributed lock                        │
│         ↓                                               │
│ STEP 4: Fetch & lock reservation                        │
│         ↓                                               │
│ STEP 5: ⭐ CREATE TICKETS ← ONLY HERE                   │
│   • FOR each reservation quantity:                      │
│   • INSERT ticket rows (status='valid')                 │
│   • Tickets immediately usable                          │
│   • Log: [TICKETS_CREATED]                              │
│         ↓                                               │
│ STEP 6: ⭐ CREATE TRANSACTION                           │
│   • Single transaction for entire purchase              │
│   • Link all tickets to transaction.id                  │
│   • Store stripe_payment_intent_id (UNIQUE)             │
│   • Log: [TRANSACTION_CREATED]                          │
│         ↓                                               │
│ STEP 7: UPDATE TIER INVENTORY                           │
│   • reserved -= quantity                                │
│   • sold += quantity                                    │
│   • Log: [INVENTORY_UPDATED]                            │
│         ↓                                               │
│ STEP 8: MARK RESERVATION COMPLETED                      │
│         ↓                                               │
│ STEP 9: COMMIT TRANSACTION (all changes atomic)         │
│         ↓                                               │
│ STEP 10: MARK WEBHOOK AS PROCESSED                      │
│   • webhook_events.processed = TRUE                     │
│   • Prevents replay                                     │
│         ↓                                               │
│ STEP 11: POST-COMMIT OPERATIONS                         │
│   • Queue confirmation email (async)                    │
│   • Release distributed lock                            │
│         ↓                                               │
│ STEP 12: Return HTTP 200 to Stripe                      │
│          (webhook marked as successfully delivered)     │
└─────────────────────────────────────────────────────────┘
                         ↓
════════════════════════════════════════════════════════════

RESULT:
✅ Tickets created ONLY after payment confirmed
✅ Transaction created with all tickets linked
✅ Inventory atomically moved from reserved → sold
✅ Customer can now use tickets
✅ Email sent to customer
✅ If webhook replayed: HTTP 200 (idempotent, no changes)

If any step fails before commit:
❌ ENTIRE transaction rolls back
❌ NO partial state (no tickets, no transaction, inventory unchanged)
❌ Stripe retries webhook in 5 minutes
✅ Next attempt starts from scratch and succeeds
```

### Key Design Rules

**Rule 1: Webhook is the SOLE source of truth for business logic**

- Browser callback = informational only
- No critical operations (tickets, transactions) in browser callback
- Webhook = authority on whether payment succeeded

**Rule 2: Tickets created ONLY in webhook (Step 5)**

- Never created in browser callback
- Never created in reservation phase
- Never created in checkout phase
- Created atomically in single transaction

**Rule 3: Atomic all-or-nothing commitment**

- If ticket #5 of 10 fails to insert → rollback all 10
- If transaction creation fails → rollback all tickets
- If inventory update fails → rollback everything
- Database guarantees consistency (ACID)

**Rule 4: Idempotency on every replay**

- Webhook delivered twice? HTTP 200 (already processed)
- Browser callback clicks "retry"? HTTP 409 (already finalized)
- Network hiccup? Stripe retries → same result
- Same payment_intent_id = same transaction (never duplicated)

---

### Phase 5: Ticket Access & Verification

**Goal**: Allow customer to view/use confirmed tickets

**Steps**:

1. **Frontend Requests Ticket View**
   - POST /api/view-ticket with checkout_token
   - OR GET /api/tickets/{ticket_access_token}

2. **Lookup Order (Backend)**
   - SELECT \* FROM orders WHERE checkout_token = ?
   - If not found → HTTP 404
   - If order_status != 'confirmed' → HTTP 403 (order not ready)

3. **Fetch Associated Tickets**
   - SELECT \* FROM order_items WHERE order_id = ?
   - Return ticket details: ticket_number, tier_name, event_details, QR code

4. **Generate Access Token (Optional)**
   - Create JWT token with claims:
     - order_id
     - checkout_token
     - ticket_count
     - expiry = order expires_at OR event date
   - Return token to frontend for sharing/offline access

---

### Phase 6: Reservation Expiry & Cleanup

**Goal**: Release expired inventory back to available pool

**Trigger**: Background worker runs every 2 minutes

**Steps**:

1. **Find Expired Reservations**
   - SELECT \* FROM reservations
     WHERE status = 'active' AND expires_at < NOW()
     LIMIT 100 (batch to avoid overwhelming DB)

2. **For Each Expired Reservation**:
   - **Check if Already Completed**
     - SELECT \* FROM orders WHERE checkout_token = reservation.checkout_token
     - If found → UPDATE reservations SET status = 'completed' (avoid double-release)
     - Continue to next reservation
   - **Acquire Lock** (distributed, same as Phase 1)
     - Redis lock on each tier: reserve:<event_id>:<tier_id>
   - **Release Inventory** (single transaction per reservation):
     - BEGIN TRANSACTION
     - FOR each tier in reservation.quantity_per_tier:
       - UPDATE tier_inventory SET available = available + quantity, reserved = reserved - quantity WHERE tier_id = ?
       - INSERT INTO inventory_ledger (tier_id, 'release', quantity, ...)
     - UPDATE reservations SET status = 'expired', expires_at = NOW() WHERE checkout_token = ?
     - COMMIT
   - **Release Lock**
     - Delete Redis lock
   - **Log Expiry**
     - [RESERVATION_EXPIRED] checkout_token, released_quantities, event_id

3. **Monitor Cleanup Metrics**
   - Track: average expiry rate, number of concurrent expired reservations
   - Alert if expiry rate abnormally high (indicates systemic issue)

---

### Phase 7: Refund Processing

**Goal**: Handle refunds without double-refunding or duplicating inventory

**Trigger**: Refund initiated via Stripe dashboard OR API call

**Steps**:

1. **Stripe Webhook: charge.refunded (or refund event)**
   - Extract: payment_intent_id, refund_id, amount
   - Similar webhook validation as Phase 4

2. **Validate Refund**
   - SELECT \* FROM transactions WHERE stripe_payment_intent_id = ?
   - If not found → HTTP 500 (retry)
   - If payment_status = 'refunded' → idempotent, log and return HTTP 200
   - If payment_amount < refund_amount → log error (Stripe shouldn't allow, but verify)

3. **Create Refund Record** (single transaction):
   - BEGIN TRANSACTION
   - SELECT \* FROM orders WHERE id = transaction.order_id FOR UPDATE
   - INSERT INTO refunds:
     - transaction_id
     - order_id
     - stripe_refund_id
     - reason (from webhook or manual)
     - amount
     - refund_status = 'pending'
   - UPDATE transactions SET payment_status = 'refunded' WHERE id = ?
   - UPDATE orders SET order_status = 'refunded' WHERE id = ?
   - COMMIT

4. **Release Tickets & Inventory**
   - SELECT \* FROM order_items WHERE order_id = ? (all tickets for this order)
   - SELECT DISTINCT tier_id FROM order_items WHERE order_id = ?
   - **Single Transaction**:
     - BEGIN TRANSACTION
     - FOR each tier:
       - UPDATE tier_inventory SET sold = sold - count, available = available + count WHERE tier_id = ?
       - INSERT INTO inventory_ledger (tier_id, 'refund', count, ...)
     - UPDATE order_items SET ticket_status = 'refunded' WHERE order_id = ?
     - COMMIT

5. **Prevent Duplicate Refunds**
   - stripe_refund_id is UNIQUE in refunds table
   - If webhook replayed: lookup finds existing refund record, marks as already processed, returns HTTP 200

6. **Payout Impact**
   - Mark associated payout as "on hold" if refund window not closed
   - Deduct refund amount from organizer ledger
   - Set organizer.payout_status = 'recalculating' for next batch

---

### Phase 8: Payout Preparation & Execution

**Goal**: Calculate organizer earnings after refund window closes

**Trigger**: Background job or manual trigger (e.g., end of week)

**Prerequisites**: Refund window closed (typically 7-14 days post-payment)

**Steps**:

1. **Filter Orders in Payout Window**
   - SELECT \* FROM orders
     WHERE event_id = ?
     AND created_at BETWEEN ? AND ?
     AND order_status != 'disputed'
     AND NOT EXISTS (SELECT 1 FROM refunds WHERE order_id = orders.id AND refund_status = 'pending')

2. **Calculate Organizer Amount**
   - SUM(organizer_amount) for all non-refunded, non-disputed orders
   - SUM(refunded_amount) for refunded orders (subtract)
   - Final net = gross - commissions - refunds

3. **Create Payout Record** (single transaction):
   - BEGIN TRANSACTION
   - INSERT INTO payouts:
     - event_id
     - organizer_id
     - total_gross
     - total_commission
     - total_net
     - refund_hold_window_until = NOW() + 7 days (or configurable)
     - payout_status = 'pending'
   - UPDATE orders SET payout_status = 'included_in_payout' WHERE id IN (selected orders)
   - COMMIT

4. **Initiate Stripe Payout**
   - Call Stripe Transfers API to move funds to organizer's bank account
   - Receive: transfer_id
   - UPDATE payouts SET stripe_payout_id = transfer_id, payout_status = 'processing'

5. **Track Payout Status**
   - Stripe sends payout.paid event when funds cleared
   - Update payouts SET payout_status = 'paid', completed_at = NOW()

6. **Prevent Double Payouts**
   - payouts table has (event_id, payout_period) UNIQUE constraint
   - Duplicate payout requests fail gracefully

---

## Concurrency & Locking Strategy

### 1. Distributed Tier-Level Locking

**Purpose**: Prevent overselling when multiple concurrent users select from the same tier

**Mechanism**: Redis Distributed Lock

```
Lock Key Format: reserve:<event_id>:<tier_id>
Lock TTL: 5 seconds (duration of DB operation)
Retry Strategy: Exponential backoff (50ms, 100ms, 200ms, stop after 3 retries = 350ms total)
CAS (Compare-And-Set): Use Redis SET with NX flag and random token for safety
```

**Flow**:

1. Before updating tier_inventory table:
   - Acquire lock: SET reserve:{event_id}:{tier_id} {token} EX 5 NX
   - If not acquired: wait 50ms, retry (up to 3 times)
   - If still not acquired: return HTTP 429 (too many requests)

2. After lock acquired:
   - Execute single DB transaction (BEGIN → UPDATE tier_inventory → INSERT INTO ledger → COMMIT)
   - Lock held for ~100-200ms

3. After transaction committed:
   - Release lock: DEL reserve:{event_id}:{tier_id} (if token matches)

**Why Distributed Lock?**

- PostgreSQL row-level FOR UPDATE locks prevent concurrent updates to same tier
- But PostgreSQL locks don't coordinate across multiple API instances
- Redis lock coordinates all instances in the cluster

**When Locking Isn't Enough**:

- If lock acquisition fails repeatedly → indicate system overload
- Implement queue (Asynq) for requests during peak load
- Workers process reservations serially per tier

---

### 2. Database Row-Level Locking

**Purpose**: Ensure atomic inventory updates within a single database transaction

**Mechanism**: PostgreSQL SELECT FOR UPDATE

```sql
BEGIN TRANSACTION ISOLATION LEVEL SERIALIZABLE;
  SELECT * FROM tier_inventory WHERE tier_id = ? FOR UPDATE;
  -- At this point, no other transaction can modify this row
  UPDATE tier_inventory SET available = available - ?, reserved = reserved + ? WHERE tier_id = ?;
COMMIT;
```

**Why SERIALIZABLE?**

- When multiple tiers selected: ensures no phantom reads
- Prevents: T1 reads tier1 (available=100), T2 reads tier1 (available=100), both deduct → available=50 (should be 0)
- SERIALIZABLE forces conflict detection: one transaction aborts with serialization error
- Default REPEATABLE READ might miss updates to other tiers in same transaction

**Handling Serialization Conflicts**:

- Application catches serialization error from driver
- Logs conflict
- Retries transaction (up to 3 times)
- After 3 retries: returns HTTP 500, user sees "system busy" message

---

### 3. Webhook Idempotency

**Purpose**: Same webhook event processed multiple times = same result

**Mechanisms**:

**A. Webhook Event Deduplication**:

```
Table: webhook_events (stripe_event_id UNIQUE, processed BOOLEAN)

On webhook arrival:
1. SELECT * FROM webhook_events WHERE stripe_event_id = ? AND processed = TRUE;
2. If found: log "already processed", return HTTP 200
3. If not found: continue processing, mark as processed when complete
```

**B. Transaction Payment Intent ID Uniqueness**:

```
Table: transactions (stripe_payment_intent_id UNIQUE)

If webhook replayed:
1. Lookup transaction by stripe_payment_intent_id
2. If found with same checkout_token: idempotent success
3. If found with different checkout_token: potential fraud, log and alert
```

**C. Composite Key Idempotency** (for error recovery):

```
Composite key: (event_id, user_id_or_guest_email, amount, created_at_window_5min)

When "reservation expired" error occurs:
- Search transactions table for matching composite key
- If found: assume this transaction already succeeded, link webhook to it
- If not found: genuine error, require manual intervention
```

---

### 4. Checkout Token Uniqueness

**Purpose**: Prevent creating multiple orders from single session

**Mechanism**: Database UNIQUE constraint

```sql
ALTER TABLE reservations ADD CONSTRAINT unique_checkout_token UNIQUE (checkout_token);
ALTER TABLE checkout_sessions ADD CONSTRAINT unique_checkout_token UNIQUE (checkout_token);
ALTER TABLE orders ADD CONSTRAINT unique_checkout_token UNIQUE (checkout_token);
ALTER TABLE transactions ADD CONSTRAINT unique_checkout_token UNIQUE (checkout_token);
```

**Idempotency**:

- If frontend retries CreateReservation with same checkout_token:
  - Database constraint violation is caught
  - Handler returns existing reservation (HTTP 200 + data)
  - Frontend treats as normal success

---

## Idempotency Mechanisms

### 1. Idempotent Reservation Creation

**Scenario**: User clicks "Reserve" button twice in quick succession

**Mechanism**:

- checkout_token is generated once on frontend
- Both HTTP requests include same checkout_token
- First request: INSERT succeeds, returns reservation
- Second request: UNIQUE constraint violation → catch and return existing reservation

**Code Logic**:

```
TRY:
  INSERT INTO reservations (checkout_token, ...) VALUES (...)
  CATCH duplicate key:
    SELECT * FROM reservations WHERE checkout_token = ?
    RETURN existing reservation
```

---

### 2. Idempotent Webhook Processing

**Scenario**: Stripe webhook delivered, network error occurs, Stripe retries 5 minutes later

**Mechanisms**:

**A. Webhook Event Table**:

```
UNIQUE(stripe_event_id, event_type)

On first processing:
INSERT INTO webhook_events (stripe_event_id, processed=FALSE)
Process order, create transaction
UPDATE webhook_events SET processed=TRUE

On retry (same stripe_event_id):
SELECT * FROM webhook_events WHERE stripe_event_id = ?
Check processed=TRUE → skip processing, return HTTP 200
```

**B. Transaction Payment Intent ID**:

```
UNIQUE(stripe_payment_intent_id)

On first processing:
INSERT INTO transactions (stripe_payment_intent_id, order_id)

On retry:
SELECT * FROM transactions WHERE stripe_payment_intent_id = ?
Found → already processed → return HTTP 200
```

**C. Order Checkout Token**:

```
UNIQUE(checkout_token) in orders table

On first webhook:
INSERT INTO orders (checkout_token)

On retry:
SELECT * FROM orders WHERE checkout_token = ?
Found → already created → return HTTP 200
```

---

### 3. Idempotent Refund Processing

**Scenario**: Refund webhook arrives twice

**Mechanism**:

```
UNIQUE(stripe_refund_id) in refunds table

On first refund webhook:
INSERT INTO refunds (stripe_refund_id, payment_status='pending')
Process refund, release tickets
UPDATE refunds SET refund_status='succeeded'

On retry:
SELECT * FROM refunds WHERE stripe_refund_id = ?
Found → already processed → HTTP 200
```

---

## Failure Scenarios & Recovery

### Scenario 1: User Reserve, Network Error, No Reservation Created

**Detection**:

- Frontend doesn't receive reservation_id
- User retries with same checkout_token

**Recovery**:

- Backend receives same checkout_token in new request
- Queries: SELECT \* FROM reservations WHERE checkout_token = ?
- If found: return existing (idempotent)
- If not found: process as new reservation

**Outcome**: Either way, user gets single reservation ✅

---

### Scenario 2: Reservation Expires While User Filling Checkout

**Detection**:

- User tries to create checkout session
- Backend fetches reservation → expires_at < NOW()

**Recovery**:

- Backend detects expired status
- Returns HTTP 410 (Gone) to frontend
- Frontend displays: "Your reservation expired, please try again"
- Frontend submits new reservation request (fresh checkout_token)

**Outcome**: User restarts, but lost inventory is released to others ✅

---

### Scenario 3: Webhook Arrives After Reservation Expires

**Detection**:

- Webhook processor fetches reservation
- Status = 'expired' OR expires_at < NOW()

**Recovery** (3-tier approach):

1. Check if order already exists:
   - SELECT \* FROM orders WHERE checkout_token = ?
   - If found → order already converted before expiry → link webhook to existing order, return HTTP 200

2. If no order, search for existing transaction (5-min composite key):
   - SELECT \* FROM transactions WHERE event_id = ? AND amount = ? AND gateway_transaction_id LIKE ? AND created_at > NOW() - 5min
   - If found (likely duplicate webhook) → link webhook event to transaction, return HTTP 200

3. If no transaction found:
   - Log error with full details
   - Mark webhook event as processed with error_log: "reservation_expired_no_recovery"
   - Alert operations team (email or Slack)
   - Return HTTP 422 (Unprocessable Entity) — don't retry

**Outcome**:

- If transaction exists elsewhere → proper recovery ✅
- If truly lost → manual review by ops team ✅

---

### Scenario 4: Stripe Charges User, Webhook Lost Before Delivery

**Detection**:

- User never receives confirmation email
- User checks account, sees charge on card

**Recovery** (requires manual process):

- User contacts support: "I was charged but didn't get tickets"
- Support tickets search:
  - SELECT \* FROM stripe charges WHERE amount = user_amount AND created_at ~= user_payment_date
  - Extract payment_intent_id
  - Query internal DB: SELECT \* FROM orders WHERE stripe_payment_intent_id = ?
- If order found: send confirmation email + ticket URLs to user
- If order not found:
  - Manually trigger webhook retry via Stripe dashboard
  - Monitor logs to confirm processing
  - If still fails: manually create order record (with notes) and send tickets

**Prevention**:

- Implement dead-letter queue for failed email deliveries
- Email worker retries up to 5 times over 24 hours
- Webhook processing is fast (~2 seconds), extremely reliable

---

### Scenario 5: Database Serialization Conflict

**Scenario**: Two users select same ticket simultaneously, tier_inventory update conflict

**Detection**:

- Transaction preparation encounters serialization error from PostgreSQL

**Recovery**:

- Application layer catches SQLSTATE 40001 (serialization_failure)
- Implements exponential backoff retry (50ms, 100ms, 200ms)
- Maximum 3 retries

**Outcome**:

- After 3 retries (350ms total), if still failing → return HTTP 500 "System busy, please retry"
- One user gets tickets, other retries and eventually succeeds (with available inventory)

---

### Scenario 6: Duplicate Checkout Sessions Created

**Scenario**: Frontend network hiccup causes duplicate webhook from Stripe for same payment

**Detection**:

- Webhook processor receives payment_intent.succeeded twice with same payment_intent_id

**Recovery**:

- First webhook: processes normally, creates order + transaction
- Second webhook:
  - SELECT \* FROM webhook_events WHERE stripe_event_id = ?
  - Found with processed=TRUE → return HTTP 200
  - OR SELECT \* FROM transactions WHERE stripe_payment_intent_id = ?
  - Found → idempotent: return HTTP 200

**Outcome**:

- Order created exactly once ✅
- User never charged twice ✅
- Tickets never double-issued ✅

---

### Scenario 7: Order Item Insert Fails Mid-Transaction

**Scenario**: Creating 100 order_items, 50 inserted, network dies

**Recovery**:

- Transaction sees COMMIT fail
- Database automatically rolls back all changes (ACID guarantee)
- Webhook processor catches error, returns HTTP 500
- Stripe retries in 5 minutes
- Next attempt: same payment_intent_id lookup finds no transaction (rollback succeeded)
- Processes again, completes

**Outcome**: Order created exactly once on successful retry ✅

---

## Race Condition Prevention

### Race Condition 1: Concurrent Inventory Deduction

**Scenario**:

```
Session 1: SELECT available FROM tier_inventory → 10
Session 2: SELECT available FROM tier_inventory → 10
Session 1: available -= 8 → UPDATE to 2
Session 2: available -= 5 → UPDATE to 5  ← WRONG! Should be -3
```

**Prevention**:

_Approach 1: Pessimistic Locking (Recommended)_

```sql
SELECT * FROM tier_inventory WHERE tier_id = ? FOR UPDATE;
UPDATE tier_inventory SET available = available - 8;
-- Session 2 waits on FOR UPDATE lock
-- Session 2 then SELECT sees available=2, UPDATE to -3
-- Proceeds or fails atomically
```

Outcome: Correct available inventory, no race ✅

_Approach 2: Atomic Operations_

```sql
UPDATE tier_inventory SET available = available - 8 WHERE tier_id = ? AND available >= 8;
-- If this completes: guaranteed atomic reduction
-- If fails: insufficient inventory, return error
```

Outcome: No race, but doesn't prevent overselling if condition checked separately ⚠️

**Our Implementation**: Approach 1 (FOR UPDATE in BEGIN...COMMIT block)

---

### Race Condition 2: Ticket Linking to Wrong Transaction

**Scenario**:

```
Webhook 1 (payment_intent_id: pi_1234): Creates transaction T1
Webhook 2 (payment_intent_id: pi_5678): Creates transaction T2
Both webhooks updating same checkout_token's reservation
Tickets get linked to T2, JWT references T1 → mismatch
```

**Prevention**:

- Each webhook uses unique payment_intent_id as key
- Reservation links to ONE order via checkout_token
- Order links to ONE transaction via order_id
- Transaction has unique payment_intent_id and order_id
- Chain: reservation → checkout_session → order → transaction → order_items
- Each link is explicit and immutable

Outcome: Tickets always linked to correct transaction ✅

---

### Race Condition 3: Reservation Released Twice

**Scenario**:

```
Cleanup worker 1: Finds expired reservation, releases inventory
Cleanup worker 2: Also finds same reservation (race), releases inventory AGAIN
Inventory goes negative
```

**Prevention**:

- Cleanup worker acquires distributed lock on tier before processing
- Only one worker can release per tier at a time
- UPDATE statement includes status check:
  ```sql
  UPDATE reservations SET status='expired' WHERE checkout_token = ? AND status='active'
  ```
- If already expired: RowsAffected=0, worker detects and skips release

Outcome: Inventory released exactly once ✅

---

### Race Condition 4: Concurrent Refunds

**Scenario**:

```
Support admin: Initiates refund via Stripe dashboard
Webhook: Stripe sends refund event (async)
Both processes try to UPDATE order_status = 'refunded' and release inventory
```

**Prevention**:

- Stripe ensures refund is idempotent (refund_id is unique)
- Database refund record has stripe_refund_id UNIQUE
- First refund process inserts record
- Second attempt hits unique constraint, detects duplicate, skips

Outcome: Inventory released exactly once ✅

---

### Race Condition 5: Reservation-to-Order Conversion Under Expiry

**Scenario**:

```
Webhook processor: Converting reservation to order (in transaction)
Cleanup worker: Trying to expire same reservation
Cleanup worker locked out by FOR UPDATE
Cleanup worker waits ~500ms for webhook transaction to complete
After commit: Cleanup worker finds status='completed', skips release
```

**Prevention**:

- Both processes use same FOR UPDATE lock on reservations table
- Both update status field: one to 'completed', other to 'expired'
- Only one can succeed due to transaction isolation
- Cleanup worker checks final status, doesn't double-release

Outcome: No double release, inventory consistent ✅

---

## Scalability Considerations

### 1. Database Scaling

**Write Scaling**:

- Partition tier_inventory by event_id
  - Each event's tiers on dedicated partition server
  - Reduces lock contention across events
  - Example: 100 events · 10 tiers each = 1000 partitions

- Inventory ledger in separate cluster/schema (append-only)
  - Faster scaling for audit logs
  - Doesn't compete with transactional tier_inventory

**Read Scaling**:

- Replicate reservations, orders, transactions tables to read replicas
- Frontend reads (list orders, check history) hit replicas
- Webhook processing hits primary (for consistency)

**Connection Pooling**:

- PgBouncer or pgpool in transaction mode
- Pool size = (num_api_instances × avg_connections_per_instance) + 20% overhead
- Min 50, Max 200 connections depending on workload

---

### 2. Redis Scaling

**Lock Scaling**:

- Redis cluster with 6 nodes (3 primary + 3 replica pairs)
- Shard distribution:
  - Hash slot = CRC16(lock_key) % 16384
  - reserve:<event_id>:<tier_id> spreads evenly by event_id
- Lock keys expire automatically (5s TTL + SET EX)

**Monitoring**:

- Alert if lock acquisition fails > 5% of requests
- Indicates: system overload or lock contention
- Action: increase lock TTL or implement queue with async workers

---

### 3. API Horizontal Scaling

**Stateless Design**:

- Each API instance is identical (no session affinity)
- Checkout_token stored in frontend (browser session/localStorage)
- Reservation created in DB (not in-memory)
- Load balancer can route any request to any instance

**Request Routing**:

- Load balancer: HAProxy or AWS ALB
- Health check: GET /health every 10 seconds
- Remove unhealthy instances within 30 seconds
- Sticky sessions: NOT required (stateless)

**Burst Handling**:

- During high load:
  - Reservation requests queue (via Asynq)
  - Workers process serially per tier at ~100 reservations/sec
  - Frontend shows queue position ("You're #47 in line")
  - Acceptable latency: users wait 30-60 seconds

---

### 4. Webhook Processing Scaling

**Async Processing**:

- Stripe webhooks delivered to an HTTP endpoint
- Endpoint acknowledges immediately (HTTP 200)
- Actual processing queued in Asynq/Bull
- Workers: 10-20 instances, each processing 1 webhook/second
- Throughput: 10-20 webhooks/second

**Concurrency Control**:

- Each webhook job locks on (event_id, tier_id)
- Multiple webhooks for different tiers process in parallel
- Multiple webhooks for same tier queued serially
- Example: 100 orders across 50 tiers simultaneously:
  - 50 parallel webhook processing
  - 100 serialized into 50 queues
  - Total latency: 1-2 seconds per webhook

---

### 5. Cleanup Worker Scaling

**Batch Processing**:

- Runs every 2 minutes
- Processes 100 expired reservations per run
- With 1000 concurrent expired per cycle:
  - Takes 10 cycles = 20 minutes to clear
  - Users see inventory released within 20 minutes of expiry
  - Acceptable for 15-minute TTL use case

**Parallel Workers**:

- Run 5 cleanup instances in parallel
- Each processes different event partitions
- 100 reservations × 5 workers = 500 per cycle
- Clears backlog in 2-4 cycles = 4-8 minutes

---

### 6. Email Queue Scaling

**Async Delivery**:

- Confirmation emails queued immediately post-webhook
- Separate email worker fleet (5-10 workers)
- Processing rate: 1000 emails/second per worker
- Can handle all-day event ticket rush

**Retry Logic**:

- Email sent to queue service (Asynq/SendGrid/etc)
- Service retries up to 5 times over 24 hours
- Failure rate: < 0.1% (SLA guarantee)

---

### 7. Monitoring & Alerting

**Key Metrics**:

- Reservation creation latency: target < 500ms (P99)
- Webhook processing latency: target < 2 seconds (P99)
- Inventory accuracy: 100% (query sum(sold) = sum of confirmed orders)
- Duplicate transactions: 0 (queried daily)
- Failed webhook retries: < 1% (monitored hourly)

**Alerts**:

- Inventory negative: triggers immediate incident
- Lock acquisition failure rate > 5%: scale resources
- Webhook failures after 3 retries: ticket for manual review
- Duplicate transaction detected: alert security team

---

## Production Deployment Checklist

### Pre-Launch

- [ ] Load test: 1000+ concurrent users reserving
- [ ] Webhook simulator: test all failure scenarios
- [ ] Database backup: snapshot before launch
- [ ] Monitoring: all metrics dashboards active
- [ ] On-call: escalation contacts confirmed

### Database

- [ ] All indexes created (prevent N+1 queries)
- [ ] Partitioning strategy deployed (if multi-region)
- [ ] Replica lag monitored (< 100ms preferred)
- [ ] Backup retention: 30 days minimum

### Application

- [ ] Error handling: comprehensive logging for all scenarios
- [ ] Rate limiting: per IP, per user_id, per tier
- [ ] Request timeout: 30 seconds for critical operations
- [ ] Graceful degradation: queue-based processing under load

### Stripe Integration

- [ ] Webhook endpoints registered (dev + production)
- [ ] Signing secret deployed in secure store (not in code)
- [ ] Webhook retry tested: simulate network failures
- [ ] Event filtering: only listen to relevant events

### Data Validation

- [ ] Inventory reconciliation: daily report (expected vs actual)
- [ ] Transaction audit: weekly report (total_amount vs Stripe records)
- [ ] Refund tracking: cross-check with Stripe refund list
- [ ] User-facing tickets: verify JWT claims match DB records

---

## Comparison to Implementation (Your Current System)

Your system already implements most of these principles. Here's the alignment:

| Principle             | Your Implementation                             | Status |
| --------------------- | ----------------------------------------------- | ------ |
| DB as source of truth | PostgreSQL transactional                        | ✅     |
| Reservations with TTL | 15-min expiration in reservations table         | ✅     |
| Webhook authority     | payment_intent.succeeded processed              | ✅     |
| Idempotent webhook    | stripe_payment_intent_id UNIQUE in transactions | ✅     |
| Atomic seat deduction | FOR UPDATE + BEGIN...COMMIT                     | ✅     |
| Distributed locking   | Redis tier-level locks                          | ✅     |
| Compensation logic    | Payment worker handles expired reservations     | ✅     |
| Email async           | Asynq queue service                             | ✅     |
| Transaction recording | First in flow (Phase 1 fix)                     | ✅     |
| Organizer ledger      | commission_amount field tracked                 | ✅     |

**Remaining enhancements**:

1. Add webhook_events table for better tracking
2. Add refunds table for refund processing
3. Add payouts table for organizer settlement
4. Add inventory_ledger for audit trails
5. Implement cleanup worker for expired reservations
6. Add composite-key idempotency for recovery scenarios
7. Enhanced monitoring dashboard for operations team

---

## Key Takeaways

1. **PostgreSQL is the source of truth** — All critical state lives here; Redis is auxiliary only
2. **Reservations are temporary holders** — Released after 15 min; frees inventory for others
3. **Webhook is the final authority** — Frontend redirect is informational; orders created ONLY when webhook succeeds
4. **Idempotency at every level** — Checkout_token, payment_intent_id, stripe_event_id UNIQUE constraints prevent duplicates
5. **Atomic transactions prevent race conditions** — FOR UPDATE + BEGIN...COMMIT ensures inventory never negative
6. **Distributed locking coordinates scale** — Redis locks prevent overselling across multiple API instances
7. **Compensation logic handles failures** — Expired reservations, failed webhooks, duplicate events all have recovery paths
8. **Everything is auditable** — Ledger tables track every state change for compliance and debugging
