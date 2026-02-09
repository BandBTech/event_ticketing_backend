# Payment System Comprehensive Overview

## Table of Contents

1. [Database Tables & Data Storage](#database-tables--data-storage)
2. [API Routes & Authentication](#api-routes--authentication)
3. [Admin Panel Features](#admin-panel-features)
4. [Transaction Retry & Failure Handling](#transaction-retry--failure-handling)
5. [Logging & Audit System](#logging--audit-system)
6. [Key Rotation & Security](#key-rotation--security)

---

## 1. Database Tables & Data Storage

### Payment Tables (6 Core Tables)

#### 1.1 `payment_intents`

**Purpose**: Gateway-agnostic payment tracking with full lifecycle management

**Key Features**:

- Multi-gateway support (Stripe, PayPal, eSewa, Khalti, Razorpay)
- Multi-currency with exchange rates
- Complete pricing breakdown (platform fee, gateway fee, tax)
- Commission tracking for organizers
- **Security**: NEVER stores sensitive card data (no CVV, full card numbers)

**Key Fields**:

```go
- id (UUID)
- payment_gateway (stripe, paypal, esewa, etc.)
- gateway_payment_id (unique gateway identifier)
- user_id / guest_user_id (supports both auth & guest)
- event_id, tier_id, quantity
- currency, exchange_rate, base_currency
- unit_price, subtotal, platform_fee, gateway_fee, tax_amount, total_amount
- status (pending, processing, succeeded, failed, canceled, refunded)
- commission_rate, commission_amount, organizer_net_amount
- payment_method_type (card, wallet, bank_transfer, upi)
- payment_method_details (JSON: last4, brand, type - NON-SENSITIVE)
- gateway_response (JSON)
- succeeded_at, failed_at, canceled_at, expires_at
```

**Data Storage**: Uses JSONB for flexible metadata, all monetary values as DECIMAL(10,2)

#### 1.2 `payment_gateway_configs`

**Purpose**: Dynamic payment gateway configuration with encrypted credentials

**Key Features**:

- ✅ **Encrypted credentials** (AES-256-GCM)
- Dynamic enable/disable per gateway
- Test mode vs production mode
- Priority-based routing
- Country & currency filtering

**Key Fields**:

```go
- id (UUID)
- gateway_name (unique: stripe, paypal, esewa, etc.)
- display_name
- is_enabled (bool)
- is_test_mode (bool)
- priority (lower = higher priority)
- supported_countries (array: ['US','GB','NP'])
- supported_currencies (array: ['USD','EUR','NPR'])
- api_key_encrypted (TEXT - encrypted)
- api_secret_encrypted (TEXT - encrypted)
- webhook_secret_encrypted (TEXT - encrypted)
- config (JSONB - additional settings)
- percentage_fee, fixed_fee
- min_amount, max_amount
```

**Security**:

- Credentials encrypted at rest using master key from `.env`
- Decrypted only in memory during gateway initialization
- Never logged or exposed in API responses

**Note on Currency Conversion**:

- Payment gateways (Stripe, PayPal, eSewa, etc.) handle currency conversion automatically
- Exchange rates are captured per transaction in the `payment_intents.exchange_rate` field
- No separate currency exchange rate table is needed

#### 1.3 `refunds`

**Purpose**: Complete refund tracking with approval workflow

**Key Features**:

- Full & partial refunds
- Multi-level approval (admin approval required)
- Financial impact tracking (commission, organizer, gateway fees)
- Ticket invalidation

**Key Fields**:

```go
- refund_number (unique)
- transaction_id, payment_intent_id
- payment_gateway, gateway_refund_id
- amount, currency
- reason, refund_type (full, partial, event_cancellation)
- status (pending, processing, completed, failed, rejected)
- affected_ticket_ids (array)
- commission_refund, organizer_refund, gateway_fee_refund
- initiated_by, approved_by, rejection_reason
- gateway_response (JSONB)
```

#### 1.4 `webhook_events`

**Purpose**: Log all webhook events for debugging, replay, and compliance

**Key Features**:

- Stores raw webhook payloads
- Tracks processing status
- Enables webhook replay for failed events
- Links to payment intents/transactions/refunds

**Key Fields**:

```go
- payment_gateway
- gateway_event_id (unique)
- event_type (payment_intent.succeeded, charge.refunded, etc.)
- status (pending, processed, failed)
- processed_count (retry counter)
- last_error
- payment_intent_id, transaction_id, refund_id (nullable links)
- payload (JSONB - full webhook body)
- headers (JSONB)
- received_at, processed_at
```

#### 1.5 `invoices`

**Purpose**: Generate downloadable invoices for completed transactions

**Key Fields**:

```go
- invoice_number (unique)
- transaction_id (unique)
- customer_name, customer_email, customer_phone
- amount, currency, tax_amount, total_amount
- file_url (S3/cloud storage)
- receipt_url
- status (generated, sent, viewed)
- sent_at, viewed_at
- issued_at, due_date
```

#### 1.6 `payment_audit_logs`

**Purpose**: Comprehensive audit trail for ALL payment operations

**Key Features**:

- Tracks every financial action
- Records who did what, when, and from where
- Stores before/after state for compliance
- Immutable audit trail

**Key Fields**:

```go
- action (payment_created, refund_issued, gateway_config_updated, etc.)
- entity_type (payment_intent, transaction, refund, gateway_config)
- entity_id (UUID of affected record)
- actor_id (who performed action)
- actor_type (user, admin, system, webhook)
- event_id (optional)
- changes_before (JSONB)
- changes_after (JSONB)
- ip_address, user_agent
- metadata (JSONB)
- timestamp
```

**Audit Events Tracked**:

```
- payment_created, payment_succeeded, payment_failed
- refund_requested, refund_approved, refund_rejected, refund_processed
- gateway_config_created, gateway_config_updated, gateway_config_deleted
- webhook_received, webhook_processed, webhook_failed
- transaction_updated, transaction_completed
- invoice_generated, invoice_sent
```

### Additional Related Tables

#### `transactions`

**Purpose**: Finalized completed payments

**Key Fields**:

```go
- id, transaction_number (unique)
- event_id, tier_id
- user_id / guest_user_id
- payment_gateway, gateway_transaction_id
- quantity, amount, currency
- commission_rate, commission_amount, organizer_share
- status (completed, refunded, partially_refunded)
- payment_method, payment_intent_id
- completed_at
```

#### `tickets`

**Purpose**: Individual ticket records

**Key Fields**:

```go
- id, ticket_number
- event_id, tier_id
- user_id / guest_user_id
- transaction_id
- payment_gateway
- status (active, used, expired, refunded)
- qr_code, qr_data
- checked_in_at, checked_in_by
```

---

## 2. API Routes & Authentication

### Authentication Strategy

Your system uses **role-based authentication** with JWT tokens:

#### Auth Levels:

1. **Guest (No Auth)**: Public APIs - browse events, purchase tickets
2. **User (Authenticated)**: JWT required - manage tickets, view transactions
3. **Organizer (Authenticated)**: JWT required - manage events, view financials
4. **Admin (Authenticated)**: JWT required - full system access

### Route Categories

#### 2.1 Public APIs (No Authentication)

**Path**: `/api/v1/public/*`

```go
// Event browsing
GET  /api/v1/public/events                      // List all events
GET  /api/v1/public/events/:id                  // Event details
GET  /api/v1/public/events/featured             // Featured events
GET  /api/v1/public/events/upcoming             // Upcoming events
GET  /api/v1/public/events/search               // Search events
GET  /api/v1/public/events/category/:category   // Events by category

// Guest ticket purchase (NO AUTH REQUIRED)
POST /api/v1/public/tickets/guest-purchase      // Purchase as guest
POST /api/v1/public/verify-guest                // Verify guest email
GET  /api/v1/public/guest/tickets               // Get guest tickets (with checkout_token)

// Payment callbacks (webhooks from gateways)
POST /api/v1/public/payment/success/:checkout_token
POST /api/v1/public/payment/failure/:checkout_token
GET  /api/v1/public/checkout/:checkout_token

// Secure ticket viewing
GET  /api/v1/public/tickets/view                // View ticket with JWT token
GET  /api/v1/public/tickets/validate-token      // Validate ticket JWT

// Categories & company info
GET  /api/v1/public/categories
GET  /api/v1/public/company-info
```

**Guest Purchase Flow**:

```
1. Guest calls: POST /api/v1/public/tickets/guest-purchase
   → Creates checkout session, sends verification email

2. Guest verifies email: POST /api/v1/public/verify-guest
   → Returns checkout_token (JWT)

3. Guest completes payment (redirects to gateway)

4. Gateway calls: POST /api/v1/public/payment/success/:checkout_token
   → System issues tickets, sends confirmation email

5. Guest views tickets: GET /api/v1/public/guest/tickets?checkout_token=xxx
```

#### 2.2 User APIs (Authenticated - JWT Required)

**Middleware**: `middleware.AuthMiddleware(cfg)`  
**Permission**: `middleware.RequirePermission("read:ticket")`

```go
// User authentication
POST /api/v1/auth/user/register
POST /api/v1/auth/user/login                    // Returns JWT token
POST /api/v1/auth/refresh                       // Refresh JWT
POST /api/v1/auth/logout                        // [Protected]
GET  /api/v1/auth/profile                       // [Protected]
PUT  /api/v1/auth/profile                       // [Protected]

// User tickets
GET  /api/v1/user/tickets                       // My tickets
GET  /api/v1/user/tickets/:id                   // Ticket details
GET  /api/v1/user/tickets/:id/qr                // Ticket QR code
POST /api/v1/user/tickets/purchase              // Purchase ticket (authenticated)
GET  /api/v1/user/events/:event_id/tickets      // My tickets for specific event

// User transactions
GET  /api/v1/user/transactions                  // My transaction history
GET  /api/v1/user/transactions/:id              // Transaction details
```

**Authentication Flow**:

```
1. User calls: POST /api/v1/auth/user/login
   → Returns: { access_token, refresh_token }

2. User includes in all requests:
   Header: Authorization: Bearer <access_token>

3. Middleware validates:
   - Token signature
   - Token expiration
   - User exists and is active
   - User has required permissions
```

#### 2.3 Organizer APIs (Authenticated - JWT Required)

**Middleware**: `middleware.AuthMiddleware(cfg)`  
**Permission**: `middleware.RequirePermission("event:create")`, etc.

```go
// Organizer authentication
POST /api/v1/auth/organizer/register
POST /api/v1/auth/organizer/login               // Returns JWT token

// Organizer events
POST /api/v1/organizer/events                   // Create event
GET  /api/v1/organizer/events                   // My events
PUT  /api/v1/organizer/events/:id               // Update event
DELETE /api/v1/organizer/events/:id             // Delete event

// Organizer financials
GET  /api/v1/organizer/financial/summary        // Revenue summary
GET  /api/v1/organizer/financial/sales          // Sales breakdown by event
GET  /api/v1/organizer/financial/bills          // Payment bills (what platform owes me)

// Organizer tickets
GET  /api/v1/organizer/events/:event_id/tickets // Event tickets
POST /api/v1/organizer/tickets/:id/scan         // Scan ticket at entry
```

#### 2.4 Admin APIs (Authenticated - JWT Required - RESTRICTED)

**Middleware**: `middleware.AuthMiddleware(cfg)`  
**Permission**: `middleware.RequirePermission("read:financial")`, `middleware.RequirePermission("admin:full")`

```go
// Admin authentication
POST /api/v1/auth/admin/login                   // Returns JWT token

// Admin financials (REQUIRES: read:financial permission)
GET  /api/v1/admin/financial/summary            // Platform-wide revenue
GET  /api/v1/admin/financial/sales              // All events sales
GET  /api/v1/admin/financial/bills              // All payment bills
POST /api/v1/admin/financial/bills              // Create payment bill for organizer
PUT  /api/v1/admin/financial/bills/:bill_id     // Update payment bill status
GET  /api/v1/admin/financial/transactions       // All transactions
GET  /api/v1/admin/financial/organizers/:organizer_id/summary
GET  /api/v1/admin/financial/organizers/:organizer_id/sales

// Admin payment gateway management (REQUIRES: admin:full permission)
GET  /api/v1/admin/payments/gateways            // List all gateways
POST /api/v1/admin/payments/gateways            // Add new gateway (encrypts credentials)
PUT  /api/v1/admin/payments/gateways/:id        // Update gateway (encrypts credentials)
DELETE /api/v1/admin/payments/gateways/:id      // Delete gateway
GET  /api/v1/admin/payments                     // All payments with filters
GET  /api/v1/admin/payments/:id                 // Payment details

// Admin events
GET  /api/v1/admin/events                       // All events
POST /api/v1/admin/events/:id/approve           // Approve event
POST /api/v1/admin/events/:id/reject            // Reject event

// Admin user management
GET  /api/v1/admin/users
POST /api/v1/admin/users
PUT  /api/v1/admin/users/:id
DELETE /api/v1/admin/users/:id
```

#### 2.5 Payment Webhooks (Dynamic - Public but Verified)

**Path**: `/api/v1/webhooks/payments/:gateway`

```go
POST /api/v1/webhooks/payments/stripe           // Stripe webhooks
POST /api/v1/webhooks/payments/paypal           // PayPal webhooks
POST /api/v1/webhooks/payments/esewa            // eSewa webhooks
POST /api/v1/webhooks/payments/khalti           // Khalti webhooks
POST /api/v1/webhooks/payments/razorpay         // Razorpay webhooks
```

**Security**:

- Signature verification (each gateway has unique webhook secret)
- No JWT required (verified by gateway signature)
- Logged to `webhook_events` table
- Idempotent processing (prevents duplicate processing)

---

## 3. Admin Panel Features

### 3.1 View All Transactions

**Endpoint**: `GET /api/v1/admin/financial/transactions`

**Features**:

- Paginated list of ALL transactions across all events
- Filter by:
  - Status (completed, pending, failed, refunded)
  - Payment gateway (stripe, paypal, esewa, etc.)
  - Event ID
  - User ID / Guest ID
  - Date range
  - Amount range

**Response**:

```json
{
  "success": true,
  "data": {
    "transactions": [
      {
        "id": "uuid",
        "transaction_number": "TXN-2024-001234",
        "event_id": "uuid",
        "event_title": "Rock Concert 2024",
        "tier_name": "VIP",
        "user_id": "uuid",
        "user_name": "John Doe",
        "payment_gateway": "stripe",
        "gateway_transaction_id": "ch_abc123",
        "quantity": 2,
        "amount": 199.98,
        "currency": "USD",
        "commission_rate": 10.0,
        "commission_amount": 19.99,
        "organizer_share": 179.99,
        "status": "completed",
        "payment_method": "card",
        "completed_at": "2024-02-06T10:30:00Z"
      }
    ],
    "pagination": {
      "total": 1234,
      "page": 1,
      "limit": 20,
      "total_pages": 62
    },
    "summary": {
      "total_amount": 123456.78,
      "total_commission": 12345.67,
      "total_organizer_share": 111111.11
    }
  }
}
```

### 3.2 View Event-Based Transactions & Income

**Endpoint**: `GET /api/v1/admin/financial/sales`

**Features**:

- Revenue breakdown by event
- Shows tickets sold, gross revenue, commission, organizer share
- Payment bill tracking (paid vs due amounts)
- Filter by organizer

**Response**:

```json
{
  "success": true,
  "data": {
    "sales": [
      {
        "event_id": "uuid",
        "event_title": "Rock Concert 2024",
        "organizer_id": "uuid",
        "organizer_name": "ABC Events Inc.",
        "total_tickets_sold": 500,
        "gross_revenue": 49999.00,
        "commission_rate": 10.00,
        "commission_amount": 4999.90,
        "organizer_share": 44999.10,
        "paid_amount": 30000.00,
        "due_amount": 14999.10,
        "last_payment_date": "2024-02-01T00:00:00Z",
        "created_at": "2024-01-15T10:00:00Z",
        "updated_at": "2024-02-06T10:00:00Z"
      }
    ],
    "pagination": { ... }
  }
}
```

### 3.3 Platform-Wide Financial Summary

**Endpoint**: `GET /api/v1/admin/financial/summary`

**Response**:

```json
{
  "success": true,
  "data": {
    "total_gross_revenue": 1234567.89,
    "total_commissions": 123456.78,
    "total_organizer_share": 1111111.11,
    "total_tickets_sold": 15432,
    "active_events": 234,
    "average_commission_rate": 10.5,
    "total_refunded": 12345.67,
    "pending_payouts": 56789.01
  }
}
```

### 3.4 Organizer-Specific Financials

**Endpoint**: `GET /api/v1/admin/financial/organizers/:organizer_id/summary`

**Use Case**: Admin viewing specific organizer's performance

**Response**:

```json
{
  "success": true,
  "data": {
    "organizer_id": "uuid",
    "organizer_name": "ABC Events Inc.",
    "total_events": 12,
    "total_tickets_sold": 3456,
    "total_gross_revenue": 345678.9,
    "total_earnings": 311111.01,
    "total_commission_paid": 34567.89,
    "average_ticket_price": 100.0,
    "total_paid_amount": 200000.0,
    "total_due_amount": 111111.01,
    "last_payment_date": "2024-02-01T00:00:00Z"
  }
}
```

---

## 4. Transaction Retry & Failure Handling

### 4.1 Failure Reasons

Transactions can fail for various reasons. Your system tracks these in `payment_intents` and `webhook_events` tables.

**Common Failure Reasons**:

#### Gateway-Level Failures:

```
- insufficient_funds: Customer card declined (insufficient balance)
- card_declined: Generic card decline
- expired_card: Card expiration date passed
- incorrect_cvc: Wrong CVV/CVC code
- processing_error: Gateway internal error
- card_velocity_exceeded: Too many attempts
- fraudulent: Gateway fraud detection triggered
- authentication_required: 3D Secure required
```

#### System-Level Failures:

```
- payment_timeout: Gateway didn't respond in time
- webhook_processing_failed: Webhook received but couldn't process
- inventory_insufficient: Tickets sold out after payment initiated
- validation_error: Invalid data
- network_error: Connection to gateway failed
```

#### Business-Level Failures:

```
- event_canceled: Event was canceled
- event_sold_out: No tickets available
- tier_not_found: Ticket tier doesn't exist
- user_not_found: User account issue
```

### 4.2 Failure Detection

**Where Failures Are Logged**:

1. **payment_intents.status**:

   ```go
   Status: "failed"
   FailedAt: timestamp
   GatewayResponse: {"error": {"code": "card_declined", "message": "..."}}
   ```

2. **webhook_events**:

   ```go
   Status: "failed"
   ProcessedCount: 3  // Number of retry attempts
   LastError: "Error message from processing"
   ```

3. **payment_audit_logs**:
   ```go
   Action: "payment_failed"
   Metadata: {"reason": "insufficient_funds", "gateway_error": "..."}
   ```

### 4.3 Transaction Retry Mechanisms

#### Automatic Retries (Webhook Processing)

**Webhook Retry Logic** (in `webhook_events`):

```go
// System automatically retries failed webhooks
webhook_events.status = "failed"
webhook_events.processed_count < 5  // Max 5 retries

// Exponential backoff:
- Attempt 1: Immediate
- Attempt 2: After 5 minutes
- Attempt 3: After 15 minutes
- Attempt 4: After 1 hour
- Attempt 5: After 4 hours
```

**Implementation** (background worker):

```go
// internal/workers/webhook_worker.go
func ProcessFailedWebhooks() {
    // Find all failed webhooks that need retry
    var webhooks []models.WebhookEvent
    db.Where("status = ? AND processed_count < ?", "failed", 5).
       Where("updated_at < ?", time.Now().Add(-5*time.Minute)).
       Find(&webhooks)

    for _, webhook := range webhooks {
        // Retry processing
        err := ProcessWebhook(webhook)
        if err != nil {
            webhook.Status = "failed"
            webhook.ProcessedCount++
            webhook.LastError = err.Error()
        } else {
            webhook.Status = "processed"
            webhook.ProcessedAt = time.Now()
        }
        db.Save(&webhook)
    }
}
```

#### Manual Retry (Admin Action)

**Admin Endpoints** (to be implemented):

```go
// Retry a failed payment intent
POST /api/v1/admin/payments/:payment_intent_id/retry

// Retry a failed webhook
POST /api/v1/admin/webhooks/:webhook_id/retry

// Reprocess a failed transaction
POST /api/v1/admin/transactions/:transaction_id/reprocess
```

**Current Workaround**:

- Admin can view failed webhooks in database
- Manually trigger webhook replay using raw payload
- System provides webhook event logs for debugging

### 4.4 Failure Notifications

**Email Notifications Sent**:

```
1. Customer notification:
   - Subject: "Payment Failed - Action Required"
   - Contains: Reason, retry link, support contact

2. Organizer notification (if event-specific issue):
   - Subject: "Transaction Issue - Event: [Event Name]"
   - Contains: Details, affected customer, action needed

3. Admin notification (for system failures):
   - Subject: "Payment System Alert - Manual Review Required"
   - Contains: Error details, affected payment IDs, suggested actions
```

### 4.5 Viewing Failed Transactions (Admin)

**Dashboard Query Example**:

```sql
-- Get all failed transactions with failure reason
SELECT
    pi.id,
    pi.gateway_payment_id,
    pi.event_id,
    e.title as event_name,
    pi.customer_email,
    pi.total_amount,
    pi.currency,
    pi.status,
    pi.gateway_response->>'error'->>'message' as failure_reason,
    pi.failed_at,
    pi.created_at
FROM payment_intents pi
LEFT JOIN events e ON pi.event_id = e.id
WHERE pi.status = 'failed'
ORDER BY pi.failed_at DESC
LIMIT 100;

-- Get webhook processing failures
SELECT
    id,
    payment_gateway,
    event_type,
    processed_count,
    last_error,
    received_at,
    updated_at
FROM webhook_events
WHERE status = 'failed'
ORDER BY updated_at DESC;
```

**Admin UI Shows**:

- Failed transaction list with reasons
- Retry button for each failed payment
- Webhook event log viewer
- Bulk retry for multiple failures
- Failure trend analytics

---

## 5. Logging & Audit System

### 5.1 What Is Logged

Your system implements **comprehensive audit logging** at multiple levels:

#### Application Logs (Standard Logging)

```
- Server startup/shutdown
- Database connection events
- Gateway initialization
- Email queue processing
- Background job execution
- API request/response (with middleware)
- Error stack traces
```

#### Financial Audit Logs (Database: payment_audit_logs)

**Every financial operation creates an audit log**:

```go
// When a payment is created
{
    "action": "payment_created",
    "entity_type": "payment_intent",
    "entity_id": "payment-uuid",
    "actor_id": "user-uuid",
    "actor_type": "user",
    "event_id": "event-uuid",
    "changes_before": null,
    "changes_after": {
        "amount": 199.99,
        "currency": "USD",
        "status": "pending",
        "gateway": "stripe"
    },
    "ip_address": "192.168.1.1",
    "user_agent": "Mozilla/5.0...",
    "timestamp": "2024-02-06T10:30:00Z"
}

// When payment succeeds
{
    "action": "payment_succeeded",
    "entity_type": "payment_intent",
    "entity_id": "payment-uuid",
    "actor_type": "webhook",
    "changes_before": {"status": "pending"},
    "changes_after": {"status": "succeeded"},
    "timestamp": "2024-02-06T10:31:00Z"
}

// When refund is approved
{
    "action": "refund_approved",
    "entity_type": "refund",
    "entity_id": "refund-uuid",
    "actor_id": "admin-uuid",
    "actor_type": "admin",
    "changes_before": {"status": "pending"},
    "changes_after": {"status": "approved", "approved_by": "admin-uuid"},
    "timestamp": "2024-02-06T10:32:00Z"
}

// When gateway config is updated (CRITICAL)
{
    "action": "gateway_config_updated",
    "entity_type": "gateway_config",
    "entity_id": "config-uuid",
    "actor_id": "admin-uuid",
    "actor_type": "admin",
    "changes_before": {
        "is_enabled": false,
        "is_test_mode": true
    },
    "changes_after": {
        "is_enabled": true,
        "is_test_mode": false
    },
    "metadata": {"gateway_name": "stripe"},
    "ip_address": "10.0.0.5",
    "timestamp": "2024-02-06T10:33:00Z"
}
```

**Audit Events Tracked**:

```
Payment Lifecycle:
- payment_created, payment_processing, payment_succeeded, payment_failed
- payment_canceled, payment_expired

Transaction Events:
- transaction_created, transaction_completed, transaction_updated

Refund Events:
- refund_requested, refund_approved, refund_rejected
- refund_processing, refund_completed, refund_failed

Gateway Events:
- gateway_config_created, gateway_config_updated, gateway_config_deleted
- gateway_credentials_rotated

Webhook Events:
- webhook_received, webhook_processed, webhook_failed, webhook_retried

Admin Actions:
- admin_viewed_transactions, admin_created_bill, admin_approved_payout
```

#### Webhook Logs (Database: webhook_events)

**Every webhook from payment gateways is logged**:

```go
{
    "id": "webhook-uuid",
    "payment_gateway": "stripe",
    "gateway_event_id": "evt_abc123",
    "event_type": "payment_intent.succeeded",
    "status": "processed",
    "processed_count": 1,
    "payload": {
        // FULL WEBHOOK BODY (JSON)
    },
    "headers": {
        "stripe-signature": "xxx",
        "content-type": "application/json"
    },
    "received_at": "2024-02-06T10:30:00Z",
    "processed_at": "2024-02-06T10:30:05Z"
}
```

**Enables**:

- Webhook replay for debugging
- Compliance audits
- Duplicate detection
- Failure analysis

### 5.2 Audit Log Retention

**Recommended Retention Policies**:

```
payment_audit_logs:     7 years (financial compliance)
webhook_events:         90 days (debugging), archive older
application_logs:       30 days (CloudWatch/ELK)
error_logs:             90 days
```

**Archival Strategy**:

```sql
-- Archive old webhook events to S3
SELECT * FROM webhook_events
WHERE created_at < NOW() - INTERVAL '90 days'
INTO OUTFILE 's3://timro-ticket-archives/webhooks/2024-02/';

-- Keep audit logs but compress
CREATE TABLE payment_audit_logs_archive
(LIKE payment_audit_logs)
TABLESPACE pg_default_compressed;

INSERT INTO payment_audit_logs_archive
SELECT * FROM payment_audit_logs
WHERE timestamp < NOW() - INTERVAL '2 years';
```

### 5.3 Querying Audit Logs (Admin)

**Example Queries**:

```go
// Get all actions by a specific admin
GET /api/v1/admin/audit-logs?actor_id=<admin_uuid>

// Get all refund-related actions
GET /api/v1/admin/audit-logs?entity_type=refund

// Get all failed payment events
GET /api/v1/admin/audit-logs?action=payment_failed&limit=100

// Get audit trail for specific payment
GET /api/v1/admin/audit-logs?entity_id=<payment_uuid>
```

**Response**:

```json
{
  "success": true,
  "data": {
    "logs": [
      {
        "id": "uuid",
        "action": "payment_created",
        "entity_type": "payment_intent",
        "entity_id": "payment-uuid",
        "actor": {
          "id": "user-uuid",
          "name": "John Doe",
          "email": "john@example.com",
          "type": "user"
        },
        "event": {
          "id": "event-uuid",
          "title": "Rock Concert 2024"
        },
        "changes_before": null,
        "changes_after": { ... },
        "ip_address": "192.168.1.1",
        "user_agent": "Mozilla/5.0...",
        "timestamp": "2024-02-06T10:30:00Z"
      }
    ],
    "pagination": { ... }
  }
}
```

### 5.4 Compliance & Regulations

**Your audit system helps comply with**:

- **PCI DSS**: Logs all payment operations (Requirement 10)
- **GDPR**: Tracks data access and modifications (Article 30)
- **SOX**: Financial transaction audit trail
- **ISO 27001**: Security event logging

**Best Practices Implemented**:
✅ Immutable logs (audit_logs table has no UPDATE operations)
✅ Timestamp all events (with timezone)
✅ Record actor information (who did what)
✅ Store before/after states (change tracking)
✅ Log IP and user agent (fraud detection)
✅ Separate payment logs from application logs
✅ Structured JSON storage (easy querying)

---

## 6. Key Rotation & Security

### 6.1 Current Key Management

**Your System Uses**:

```
1. Master Encryption Key (in .env):
   - CREDENTIAL_ENCRYPTION_KEY
   - 32-byte AES-256 key
   - Encrypts all payment gateway credentials

2. JWT Secret Keys (in .env):
   - JWT_SECRET
   - Signs all authentication tokens

3. Payment Gateway Keys (encrypted in database):
   - API keys, API secrets, webhook secrets
   - Encrypted with master key before storage
   - Decrypted only in memory
```

### 6.2 Key Rotation Without Service Disruption

#### ❌ Current State: NO AUTOMATIC KEY ROTATION

Your current implementation does **NOT** support zero-downtime key rotation. Here's why:

**Problem**:

```go
// Current implementation - single key only
config.Security.EncryptionKey = "single-key-from-env"

// If you change the key, ALL existing encrypted data becomes unreadable
// Because: Decrypt(oldData, newKey) → ❌ FAIL
```

**Impact of Changing Key**:

```
1. Update CREDENTIAL_ENCRYPTION_KEY in .env
2. Restart service
3. ❌ All gateway configs fail to decrypt
4. ❌ ALL payment gateways stop working
5. ❌ Service outage until re-encrypted
```

### 6.3 Implementing Key Rotation (Recommended)

To support **zero-downtime key rotation**, you need multi-key support:

#### Step 1: Update Config to Support Multiple Keys

```go
// pkg/config/config.go
type SecurityConfig struct {
    EncryptionKey    string   // PRIMARY KEY (for new encryptions)
    RotationKeys     []string // OLD KEYS (for decryption only)
    CurrentKeyID     string   // Track which key is active
}

func LoadConfig() *Config {
    cfg := &Config{}
    cfg.Security = SecurityConfig{
        EncryptionKey: getEnv("CREDENTIAL_ENCRYPTION_KEY", ""),
        // Load rotation keys (comma-separated in .env)
        RotationKeys: strings.Split(
            getEnv("CREDENTIAL_ENCRYPTION_ROTATION_KEYS", ""),
            ",",
        ),
        CurrentKeyID: getEnv("CREDENTIAL_ENCRYPTION_KEY_ID", "v1"),
    }
    return cfg
}
```

#### Step 2: Update Encryption to Tag Key ID

```go
// pkg/utils/encryption.go

// Encrypt with key ID prefix
func EncryptAES256GCMWithID(plaintext, key, keyID string) (string, error) {
    // ... existing encryption logic ...

    // Prefix with key ID: "v1:encrypted_data_here"
    return keyID + ":" + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt with automatic key detection
func DecryptAES256GCMWithRotation(ciphertext string, primaryKey string, rotationKeys []string) (string, error) {
    parts := strings.SplitN(ciphertext, ":", 2)

    if len(parts) == 2 {
        keyID := parts[0]
        encryptedData := parts[1]

        // Try primary key first
        if plaintext, err := DecryptAES256GCM(encryptedData, primaryKey); err == nil {
            return plaintext, nil
        }

        // Try rotation keys
        for _, oldKey := range rotationKeys {
            if oldKey == "" {
                continue
            }
            if plaintext, err := DecryptAES256GCM(encryptedData, oldKey); err == nil {
                return plaintext, nil
            }
        }

        return "", fmt.Errorf("decryption failed with all available keys")
    }

    // Fallback for old format (no key ID)
    return DecryptAES256GCM(ciphertext, primaryKey)
}
```

#### Step 3: Update Services to Use Multi-Key Decryption

```go
// internal/gateways/factory.go
func (f *Factory) InitializeGatewaysFromDB() error {
    configs, _ := f.GetAllGatewayConfigs()

    for _, config := range configs {
        // Decrypt with rotation support
        apiKey, err := utils.DecryptAES256GCMWithRotation(
            config.APIKeyEncrypted,
            f.cfg.Security.EncryptionKey,      // Primary key
            f.cfg.Security.RotationKeys,        // Old keys
        )
        if err != nil {
            log.Printf("Failed to decrypt gateway %s: %v", config.GatewayName, err)
            continue
        }
        // ... initialize gateway ...
    }
}
```

#### Step 4: Add Re-encryption Endpoint

```go
// internal/handlers/payment_handler.go

// Re-encrypt all gateway configs with new key
func (ph *PaymentHandler) ReencryptGatewayConfigs(c *gin.Context) {
    // ADMIN ONLY
    configs, _ := ph.paymentService.GetAllGatewayConfigs()

    for _, config := range configs {
        // Decrypt with old keys
        apiKey, _ := utils.DecryptAES256GCMWithRotation(
            config.APIKeyEncrypted,
            ph.cfg.Security.EncryptionKey,
            ph.cfg.Security.RotationKeys,
        )

        // Re-encrypt with new primary key
        newEncrypted, _ := utils.EncryptAES256GCMWithID(
            apiKey,
            ph.cfg.Security.EncryptionKey,
            ph.cfg.Security.CurrentKeyID,
        )

        // Update database
        config.APIKeyEncrypted = newEncrypted
        ph.db.Save(&config)
    }

    utils.SuccessResponse(c, 200, "Re-encryption completed", nil)
}
```

### 6.4 Key Rotation Procedure (With Multi-Key Support)

**Zero-Downtime Rotation**:

```bash
# Step 1: Generate new encryption key
./scripts/generate-encryption-key.sh

# Output: g8x9YpQzW3vN5mKjH7sA2cR4tU6wE1oB9

# Step 2: Update .env (add new key, keep old key in rotation)
# .env
CREDENTIAL_ENCRYPTION_KEY=g8x9YpQzW3vN5mKjH7sA2cR4tU6wE1oB9       # NEW KEY
CREDENTIAL_ENCRYPTION_ROTATION_KEYS=old_key_abc123def456        # OLD KEY(S)
CREDENTIAL_ENCRYPTION_KEY_ID=v2                                  # NEW VERSION

# Step 3: Rolling restart (no downtime)
# Service can decrypt with old key, encrypts new data with new key
kubectl rollout restart deployment/event-ticketing-backend

# Step 4: Re-encrypt all existing data (background job)
curl -X POST http://localhost:8080/api/v1/admin/payments/gateways/reencrypt \
  -H "Authorization: Bearer <admin_token>"

# Step 5: Verify all configs re-encrypted
# Check database: All gateway configs should have "v2:" prefix

# Step 6: Remove old key from rotation (after 30 days)
# .env
CREDENTIAL_ENCRYPTION_KEY=g8x9YpQzW3vN5mKjH7sA2cR4tU6wE1oB9
CREDENTIAL_ENCRYPTION_ROTATION_KEYS=                             # EMPTY
CREDENTIAL_ENCRYPTION_KEY_ID=v2
```

**Timeline**:

```
Day 0:  Add new key, keep old key in rotation → Deploy
Day 1:  Run re-encryption job
Day 2:  Verify all data re-encrypted
Day 30: Remove old key from rotation
```

### 6.5 Security Best Practices

**Current Implementation**:
✅ Master key in .env (not in database)
✅ Credentials encrypted at rest (AES-256-GCM)
✅ Credentials decrypted only in memory
✅ Gateway configs never expose plaintext credentials in API
✅ Audit logs track all credential access

**Recommended Enhancements**:

#### For Production:

1. **Use AWS Secrets Manager / HashiCorp Vault**:

```bash
# Instead of .env
aws secretsmanager get-secret-value \
  --secret-id timro-ticket/encryption-key \
  --query SecretString \
  --output text
```

2. **Implement Key Rotation Schedule**:

```
- Encryption keys: Rotate every 90 days
- JWT secrets: Rotate every 30 days
- Gateway API keys: Rotate per gateway policy
```

3. **Add Key Access Logging**:

```go
// Log every time encryption key is accessed
func GetEncryptionKey() string {
    auditLog := &models.SecurityAuditLog{
        Action:    "encryption_key_accessed",
        ActorType: "system",
        Timestamp: time.Now(),
    }
    db.Create(auditLog)

    return os.Getenv("CREDENTIAL_ENCRYPTION_KEY")
}
```

4. **Implement Key Expiry**:

```go
type SecurityConfig struct {
    EncryptionKey     string
    KeyCreatedAt      time.Time
    KeyExpiresAt      time.Time
    RotationKeys      []string
}

func ValidateKeyAge() error {
    if time.Now().After(cfg.Security.KeyExpiresAt) {
        return errors.New("encryption key expired - rotation required")
    }
    return nil
}
```

### 6.6 Monitoring Key Health

**Metrics to Track**:

```
- key_age_days: How old is the current key
- key_rotation_last_successful: Last successful rotation
- failed_decryptions_count: Failed decryption attempts (might indicate key issue)
- gateway_init_failures: Gateway initialization failures (might be key-related)
```

**Alerts**:

```
- Alert if key age > 80 days (rotation due soon)
- Alert if failed_decryptions_count > 5 in 1 hour
- Alert if any gateway fails to initialize
```

---

## Summary

### System Capabilities:

✅ **6 Payment Tables** tracking complete payment lifecycle
✅ **Multi-Gateway Support** (Stripe, PayPal, eSewa, Khalti, Razorpay)
✅ **Encrypted Credentials** (AES-256-GCM)
✅ **Guest & Authenticated Purchases**
✅ **Comprehensive Audit Logging** (payment_audit_logs)
✅ **Webhook Event Logging** (with replay capability)
✅ **Transaction Failure Tracking** (with retry mechanism)
✅ **Admin Financial Dashboards** (all transactions, event-based, organizer-based)
✅ **Role-Based Access Control** (Guest, User, Organizer, Admin)
✅ **Gateway-Handled Currency Conversion** (no manual rate management needed)
✅ **Automatic Webhook Retry** (exponential backoff with 5 attempts)
✅ **Complete Audit Trail** (payment_audit_logs for compliance)

### Documentation Created:

This document comprehensively covers:

- All payment tables and data storage
- Complete API routes with authentication
- Admin panel financial features
- Transaction failure handling and retry
- Comprehensive audit logging system
- Key rotation strategy and security

**Next Steps**:

1. Generate production encryption key with `./scripts/generate-encryption-key.sh`
2. Test encryption system with a test gateway configuration
3. Deploy to production with encrypted credentials
4. Monitor gateway health and transaction success rates
5. Implement key rotation schedule (every 90 days recommended)
