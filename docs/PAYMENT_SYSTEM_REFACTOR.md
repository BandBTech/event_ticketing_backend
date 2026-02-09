# Payment & Transaction System Refactor Documentation

## Executive Summary

This document outlines a comprehensive refactoring of the payment and transaction system for the event ticketing platform, implementing a **multi-gateway architecture** with **Stripe as the primary payment gateway** and support for **multiple currencies** for global expansion. The system is designed to support both international (Stripe, PayPal) and local payment gateways (eSewa, Khalti, IME Pay, etc.) with seamless integration for both guest and logged-in users, proper transaction tracking, refund handling, and financial reconciliation.

### Key Design Principles

- 🌍 **Multi-Gateway Support**: Pluggable architecture for multiple payment providers
- 💱 **Multi-Currency**: Support for USD, EUR, GBP, NPR, and more
- 🔌 **Gateway Abstraction**: Common interface for all payment providers
- 🌏 **Global Ready**: Support for local payment methods by region
- 🎯 **Stripe First**: Primary focus on Stripe with easy extension to others
- 🔒 **PCI-Compliant**: Zero card data stored - all sensitive payment details handled by gateway
- 🛡️ **Secure by Design**: Only payment metadata stored (payment method type, last 4 digits, brand)

---

## Multi-Gateway Architecture

### Supported Payment Gateways

#### International Gateways

| Gateway      | Regions        | Currencies      | Payment Methods       | Priority    |
| ------------ | -------------- | --------------- | --------------------- | ----------- |
| **Stripe**   | Global         | 135+ currencies | Card, Wallet, Bank    | **Primary** |
| **PayPal**   | 200+ countries | 25+ currencies  | PayPal, Card, Venmo   | Secondary   |
| **Razorpay** | India          | INR, USD        | UPI, Card, NetBanking | Future      |

#### Local Payment Gateways (Nepal)

| Gateway        | Region | Currency | Payment Methods        | Status |
| -------------- | ------ | -------- | ---------------------- | ------ |
| **eSewa**      | Nepal  | NPR      | eSewa Wallet           | Future |
| **Khalti**     | Nepal  | NPR      | Khalti Wallet, Banking | Future |
| **IME Pay**    | Nepal  | NPR      | IME Wallet             | Future |
| **ConnectIPS** | Nepal  | NPR      | Bank Transfer          | Future |

### Gateway Abstraction Layer

All payment gateways implement a common interface:

```go
// PaymentGateway defines the interface that all payment gateways must implement
type PaymentGateway interface {
    // Create a new payment intent
    CreatePaymentIntent(ctx context.Context, req *PaymentIntentRequest) (*PaymentIntentResponse, error)

    // Get payment intent status
    GetPaymentIntent(ctx context.Context, gatewayPaymentID string) (*PaymentIntentResponse, error)

    // Cancel a payment intent
    CancelPaymentIntent(ctx context.Context, gatewayPaymentID string) error

    // Create a refund
    CreateRefund(ctx context.Context, req *RefundRequest) (*RefundResponse, error)

    // Get refund status
    GetRefund(ctx context.Context, gatewayRefundID string) (*RefundResponse, error)

    // Verify webhook signature
    VerifyWebhook(ctx context.Context, payload []byte, signature string) (*WebhookEvent, error)

    // Get supported currencies
    GetSupportedCurrencies() []string

    // Get supported countries
    GetSupportedCountries() []string

    // Get gateway name
    GetName() string

    // Calculate fees
    CalculateFees(amount float64, currency string) float64
}
```

### Security & Data Handling Model

#### ⚠️ CRITICAL: No Sensitive Data Storage

**What We NEVER Store:**

- ❌ Full card numbers
- ❌ CVV/CVC codes
- ❌ Card expiration dates (full)
- ❌ Card PINs
- ❌ Bank account numbers
- ❌ Wallet passwords
- ❌ Any raw payment credentials

**What We DO Store (Metadata Only):**

- ✅ Payment method type (card, wallet, bank_transfer, upi)
- ✅ Card brand (visa, mastercard, amex) - provided by gateway
- ✅ Card type (debit, credit) - provided by gateway
- ✅ Last 4 digits of card - provided by gateway
- ✅ Card expiry month/year (for display) - provided by gateway
- ✅ Gateway payment IDs and tokens
- ✅ Transaction status and amounts

**Payment Flow:**

```
[User on Your Site] → [Redirect/Embed Gateway Page]
                              ↓
                    [User Enters Card Details]
                    (Data goes directly to gateway)
                              ↓
                    [Gateway Processes Payment]
                              ↓
                    [Gateway Sends Webhook]
                              ↓
            [Your System: Store Metadata Only]
```

**PCI-DSS Compliance:**

- Platform is **PCI-DSS SAQ-A** compliant (lowest scope)
- Payment gateway (Stripe, PayPal, etc.) handles PCI-DSS SAQ-D compliance
- No card data ever touches your servers
- Gateway-provided hosted payment pages or secure iframes/elements

### Gateway Configuration Table

```sql
CREATE TABLE payment_gateway_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Gateway Info
    gateway_name VARCHAR(50) UNIQUE NOT NULL,
    -- stripe, paypal, esewa, khalti, imepay

    display_name VARCHAR(100) NOT NULL,
    -- Display name for UI

    is_enabled BOOLEAN DEFAULT false,
    is_test_mode BOOLEAN DEFAULT true,

    -- Priority & Region
    priority INT DEFAULT 0,
    -- Lower number = higher priority

    supported_countries TEXT[],
    -- ['US', 'GB', 'NP', 'IN']

    supported_currencies TEXT[],
    -- ['USD', 'EUR', 'GBP', 'NPR', 'INR']

    -- Credentials (Encrypted)
    api_key_encrypted TEXT,
    api_secret_encrypted TEXT,
    webhook_secret_encrypted TEXT,

    -- Additional Config
    config JSONB,
    -- Gateway-specific configuration

    -- Fee Structure
    percentage_fee DECIMAL(5, 2) DEFAULT 0,
    -- Gateway's percentage fee (e.g., 2.9 for Stripe)

    fixed_fee DECIMAL(10, 2) DEFAULT 0,
    -- Gateway's fixed fee per transaction (e.g., $0.30 for Stripe)

    -- Limits
    min_amount DECIMAL(10, 2),
    max_amount DECIMAL(10, 2),

    -- Timestamps
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP,

    -- Indexes
    INDEX idx_gateway_configs_name (gateway_name),
    INDEX idx_gateway_configs_enabled (is_enabled)
);
```

### Example Gateway Configurations

```sql
-- Stripe Configuration
INSERT INTO payment_gateway_configs (
    gateway_name, display_name, is_enabled, is_test_mode, priority,
    supported_countries, supported_currencies,
    percentage_fee, fixed_fee, min_amount, max_amount
) VALUES (
    'stripe', 'Stripe', true, false, 1,
    ARRAY['US', 'GB', 'CA', 'AU', 'NZ', 'EU', 'NP'],
    ARRAY['USD', 'EUR', 'GBP', 'CAD', 'AUD', 'NZD', 'NPR'],
    2.9, 0.30, 0.50, 999999.99
);

-- eSewa Configuration (Nepal)
INSERT INTO payment_gateway_configs (
    gateway_name, display_name, is_enabled, is_test_mode, priority,
    supported_countries, supported_currencies,
    percentage_fee, fixed_fee, min_amount, max_amount
) VALUES (
    'esewa', 'eSewa', false, true, 2,
    ARRAY['NP'],
    ARRAY['NPR'],
    2.0, 0, 10, 100000
);

-- Khalti Configuration (Nepal)
INSERT INTO payment_gateway_configs (
    gateway_name, display_name, is_enabled, is_test_mode, priority,
    supported_countries, supported_currencies,
    percentage_fee, fixed_fee, min_amount, max_amount
) VALUES (
    'khalti', 'Khalti', false, true, 2,
    ARRAY['NP'],
    ARRAY['NPR'],
    2.5, 0, 10, 100000
);
```

---

## Multi-Currency System

### Supported Currencies

| Currency          | Code | Symbol | Regions   | Status      |
| ----------------- | ---- | ------ | --------- | ----------- |
| US Dollar         | USD  | $      | Global    | **Primary** |
| Euro              | EUR  | €      | Europe    | Active      |
| British Pound     | GBP  | £      | UK        | Active      |
| Nepalese Rupee    | NPR  | रू     | Nepal     | Active      |
| Indian Rupee      | INR  | ₹      | India     | Future      |
| Canadian Dollar   | CAD  | C$     | Canada    | Future      |
| Australian Dollar | AUD  | A$     | Australia | Future      |

### Currency Exchange Table

```sql
CREATE TABLE currency_exchange_rates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Currency Pair
    from_currency VARCHAR(3) NOT NULL,
    to_currency VARCHAR(3) NOT NULL,

    -- Exchange Rate
    rate DECIMAL(12, 6) NOT NULL,
    -- e.g., 1 USD = 132.50 NPR

    -- Source & Validity
    source VARCHAR(50) NOT NULL,
    -- exchangerate-api, openexchangerates, manual

    valid_from TIMESTAMP NOT NULL DEFAULT NOW(),
    valid_until TIMESTAMP,

    -- Metadata
    is_active BOOLEAN DEFAULT true,
    metadata JSONB,

    -- Timestamps
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),

    -- Unique constraint on active rates
    UNIQUE(from_currency, to_currency, valid_from),

    -- Indexes
    INDEX idx_currency_rates_pair (from_currency, to_currency),
    INDEX idx_currency_rates_active (is_active, valid_from DESC)
);
```

### Currency Conversion Service

```go
type CurrencyService interface {
    // Convert amount from one currency to another
    Convert(ctx context.Context, amount float64, fromCurrency, toCurrency string) (float64, error)

    // Get current exchange rate
    GetRate(ctx context.Context, fromCurrency, toCurrency string) (float64, error)

    // Update exchange rates (cron job)
    UpdateRates(ctx context.Context) error

    // Get supported currencies
    GetSupportedCurrencies() []string
}
```

### Example Currency Conversions

```sql
-- USD to NPR (1 USD = 132.50 NPR)
INSERT INTO currency_exchange_rates (from_currency, to_currency, rate, source)
VALUES ('USD', 'NPR', 132.500000, 'exchangerate-api');

-- EUR to USD (1 EUR = 1.09 USD)
INSERT INTO currency_exchange_rates (from_currency, to_currency, rate, source)
VALUES ('EUR', 'USD', 1.090000, 'exchangerate-api');

-- GBP to USD (1 GBP = 1.27 USD)
INSERT INTO currency_exchange_rates (from_currency, to_currency, rate, source)
VALUES ('GBP', 'USD', 1.270000, 'exchangerate-api');
```

### Dynamic Currency Selection

**Business Rules:**

1. **Event Currency**: Organizers set their preferred currency when creating events
2. **User Currency**: Users can select their preferred display currency
3. **Gateway Currency**: Payment processed in gateway's supported currency
4. **Base Currency**: All financial reports use platform's base currency (USD)

**Example Flow:**

```
Event Price: $50 USD
User Location: Nepal
User Preference: NPR

Display Price: रू 6,625 NPR (50 × 132.50)
Payment Gateway: Stripe (supports NPR)
Process Payment: NPR 6,625
Store in DB:
  - amount: 6625
  - currency: NPR
  - exchange_rate: 132.50
  - base_currency_amount: 50 (USD)
```

---

## Current System Analysis

### Existing Tables

1. **transactions** - Transaction records
2. **tickets** - Individual ticket records
3. **payment_bills** - Organizer payout tracking
4. **checkout_sessions** - Temporary payment sessions
5. **users** - Registered users
6. **guest_users** - Guest purchase records
7. **events** - Event information
8. **event_tiers** - Pricing tiers

### Current Issues Identified

1. ❌ No proper payment intent tracking
2. ❌ Limited refund workflow
3. ❌ No invoice/receipt generation
4. ❌ Insufficient payment status handling
5. ❌ No webhook verification for payment gateways
6. ❌ Limited audit trail for financial operations
7. ❌ No idempotency keys for payment retries
8. ❌ Manual organizer payout tracking is disconnected
9. ❌ No multi-gateway abstraction layer
10. ❌ No multi-currency support
11. ❌ Hard-coded payment gateway logic

### Security & Data Storage Model

| Data Type                | Stored?      | Location                 | Purpose                 |
| ------------------------ | ------------ | ------------------------ | ----------------------- |
| Full Card Number         | ❌ **NEVER** | -                        | PCI Violation           |
| CVV/CVC                  | ❌ **NEVER** | -                        | PCI Violation           |
| Card PIN                 | ❌ **NEVER** | -                        | Security                |
| Card Brand (Visa/MC)     | ✅ Yes       | `payment_method_details` | Display, Reports        |
| Card Type (Credit/Debit) | ✅ Yes       | `payment_method_details` | Records, Analytics      |
| Last 4 Digits            | ✅ Yes       | `payment_method_details` | User Reference          |
| Expiry Month/Year        | ✅ Yes       | `payment_method_details` | Display Only            |
| Payment Gateway Token    | ✅ Yes       | `gateway_payment_id`     | Refunds, Reconciliation |
| Transaction Amount       | ✅ Yes       | `payment_intents`        | Financial Records       |
| Payment Status           | ✅ Yes       | `payment_intents`        | Order Fulfillment       |

**Key Principle:** Payment gateway handles ALL sensitive data collection and processing. Your platform only receives and stores non-sensitive metadata.

---

## Proposed Database Schema

### 1. **payment_intents** (NEW)

**Purpose**: Gateway-agnostic payment intent tracking with full lifecycle management

```sql
CREATE TABLE payment_intents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Gateway Integration (Gateway-Agnostic)
    payment_gateway VARCHAR(50) NOT NULL,
    -- stripe, paypal, esewa, khalti, imepay, razorpay, etc.

    gateway_payment_id VARCHAR(255) UNIQUE NOT NULL,
    -- Stores: stripe_payment_intent_id, paypal_order_id, esewa_transaction_id, etc.

    gateway_client_secret VARCHAR(500),
    -- For client-side payment completion (if applicable)

    idempotency_key VARCHAR(255) UNIQUE NOT NULL,
    -- Ensures payment retry safety across all gateways

    -- Customer Info
    user_id UUID REFERENCES users(id),
    guest_user_id UUID REFERENCES guest_users(id),
    customer_email VARCHAR(255) NOT NULL,
    customer_name VARCHAR(255),
    customer_phone VARCHAR(50),

    -- Event & Pricing
    event_id UUID NOT NULL REFERENCES events(id),
    tier_id UUID NOT NULL REFERENCES event_tiers(id),
    quantity INT NOT NULL CHECK (quantity > 0),

    -- Multi-Currency Support
    currency VARCHAR(3) NOT NULL,
    -- USD, EUR, GBP, NPR, INR, etc.

    currency_symbol VARCHAR(10),
    -- $, €, £, रू, ₹

    exchange_rate DECIMAL(10, 6) DEFAULT 1.000000,
    -- Exchange rate at time of purchase (for reporting in base currency)

    base_currency VARCHAR(3) DEFAULT 'USD',
    -- Platform's base currency for financial reporting

    base_currency_amount DECIMAL(10, 2),
    -- Converted amount in base currency

    -- Pricing Breakdown
    unit_price DECIMAL(10, 2) NOT NULL,
    subtotal DECIMAL(10, 2) NOT NULL,
    platform_fee DECIMAL(10, 2) NOT NULL,
    gateway_fee DECIMAL(10, 2) DEFAULT 0,
    -- Actual fee charged by payment gateway

    tax_amount DECIMAL(10, 2) DEFAULT 0,
    -- VAT/GST if applicable

    total_amount DECIMAL(10, 2) NOT NULL,

    -- Status Management
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    -- pending, processing, requires_action, succeeded, failed, canceled, refunded, partially_refunded

    -- Financial Tracking
    commission_rate DECIMAL(5, 2) NOT NULL,
    commission_amount DECIMAL(10, 2) NOT NULL,
    organizer_net_amount DECIMAL(10, 2) NOT NULL,

    -- Gateway-Specific Data
    payment_method_type VARCHAR(50),
    -- card, bank_transfer, wallet, upi, etc.

    payment_method_details JSONB,
    -- NON-SENSITIVE payment metadata ONLY from gateway
    -- Example for card: {"brand": "visa", "type": "credit", "last4": "4242", "exp_month": 12, "exp_year": 2025, "country": "US"}
    -- Example for wallet: {"wallet_type": "esewa", "wallet_id": "98XXXXX45"}
    -- Example for UPI: {"vpa": "user@****"}
    -- NEVER contains: full card number, CVV, PIN, passwords

    gateway_response JSONB,
    -- Full response from payment gateway (for reconciliation)

    gateway_metadata JSONB,
    -- Additional gateway-specific data

    capture_method VARCHAR(20) DEFAULT 'automatic',
    -- automatic, manual

    -- Region & Localization
    country_code VARCHAR(2),
    -- US, GB, NP, IN, etc.

    locale VARCHAR(10),
    -- en-US, en-GB, ne-NP, etc.

    -- Timestamps
    succeeded_at TIMESTAMP,
    failed_at TIMESTAMP,
    canceled_at TIMESTAMP,
    expires_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT user_or_guest_check CHECK (
        (user_id IS NOT NULL AND guest_user_id IS NULL) OR
        (user_id IS NULL AND guest_user_id IS NOT NULL)
    ),

    -- Indexes
    INDEX idx_payment_intents_gateway_id (gateway_payment_id),
    INDEX idx_payment_intents_gateway (payment_gateway),
    INDEX idx_payment_intents_user (user_id),
    INDEX idx_payment_intents_guest (guest_user_id),
    INDEX idx_payment_intents_event (event_id),
    INDEX idx_payment_intents_status (status),
    INDEX idx_payment_intents_currency (currency),
    INDEX idx_payment_intents_country (country_code),
    INDEX idx_payment_intents_created (created_at DESC)
);
```

### 2. **transactions** (REFACTORED)

**Purpose**: Immutable record of completed financial transactions

```sql
CREATE TABLE transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_number VARCHAR(50) UNIQUE NOT NULL,

    -- Links
    payment_intent_id UUID NOT NULL REFERENCES payment_intents(id),
    user_id UUID REFERENCES users(id),
    guest_user_id UUID REFERENCES guest_users(id),
    event_id UUID NOT NULL REFERENCES events(id),
    tier_id UUID NOT NULL REFERENCES event_tiers(id),

    -- Financial Details
    amount DECIMAL(10, 2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    quantity INT NOT NULL,

    -- Commission & Revenue Split
    commission_rate DECIMAL(5, 2) NOT NULL,
    commission_amount DECIMAL(10, 2) NOT NULL,
    organizer_share DECIMAL(10, 2) NOT NULL,
    stripe_fee DECIMAL(10, 2) DEFAULT 0,
    net_revenue DECIMAL(10, 2) NOT NULL, -- amount - stripe_fee

    -- Status
    status VARCHAR(50) NOT NULL,
    -- completed, refunded, partially_refunded, disputed

    -- Gateway Info
    payment_gateway VARCHAR(50) NOT NULL DEFAULT 'stripe',
    gateway_transaction_id VARCHAR(255),

    -- Receipt & Invoice
    receipt_url TEXT,
    invoice_url TEXT,
    receipt_number VARCHAR(50),

    -- Metadata
    gateway_data JSONB,
    metadata JSONB,

    -- Timestamps
    processed_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP,

    -- Indexes
    INDEX idx_transactions_payment_intent (payment_intent_id),
    INDEX idx_transactions_user (user_id),
    INDEX idx_transactions_guest (guest_user_id),
    INDEX idx_transactions_event (event_id),
    INDEX idx_transactions_status (status),
    INDEX idx_transactions_created (created_at DESC),
    INDEX idx_transactions_number (transaction_number)
);
```

### 3. **refunds** (NEW)

**Purpose**: Gateway-agnostic refund tracking with full audit trail

```sql
CREATE TABLE refunds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    refund_number VARCHAR(50) UNIQUE NOT NULL,

    -- Links
    transaction_id UUID NOT NULL REFERENCES transactions(id),
    payment_intent_id UUID NOT NULL REFERENCES payment_intents(id),

    -- Gateway Integration (Gateway-Agnostic)
    payment_gateway VARCHAR(50) NOT NULL,
    -- stripe, paypal, esewa, khalti, etc.

    gateway_refund_id VARCHAR(255) UNIQUE NOT NULL,
    -- Stores: stripe_refund_id, paypal_refund_id, esewa_refund_id, etc.

    -- Refund Details
    amount DECIMAL(10, 2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',

    -- Multi-Currency Support
    exchange_rate DECIMAL(10, 6) DEFAULT 1.000000,
    base_currency VARCHAR(3) DEFAULT 'USD',
    base_currency_amount DECIMAL(10, 2),

    reason VARCHAR(255) NOT NULL,
    refund_type VARCHAR(50) NOT NULL,
    -- full, partial, event_cancellation, customer_request, fraudulent, duplicate

    -- Status
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    -- pending, processing, succeeded, failed, canceled

    -- Ticket Impact
    affected_ticket_ids UUID[] NOT NULL,
    ticket_count INT NOT NULL,

    -- Financial Impact
    commission_refund DECIMAL(10, 2),
    organizer_refund DECIMAL(10, 2),
    gateway_fee_refund DECIMAL(10, 2),
    -- Refunded gateway fee (if applicable)

    -- Admin Control
    initiated_by UUID REFERENCES users(id),
    approved_by UUID REFERENCES users(id),
    rejection_reason TEXT,

    -- Gateway-Specific Data
    gateway_response JSONB,
    -- Full gateway response for audit

    gateway_metadata JSONB,
    -- Gateway-specific metadata

    -- Metadata
    metadata JSONB,
    notes TEXT,

    -- Timestamps
    requested_at TIMESTAMP NOT NULL DEFAULT NOW(),
    approved_at TIMESTAMP,
    processed_at TIMESTAMP,
    failed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),

    -- Indexes
    INDEX idx_refunds_transaction (transaction_id),
    INDEX idx_refunds_gateway (payment_gateway),
    INDEX idx_refunds_gateway_id (gateway_refund_id),
    INDEX idx_refunds_status (status),
    INDEX idx_refunds_created (created_at DESC)
);
```

### 4. **tickets** (ENHANCED)

**Purpose**: Individual scannable tickets linked to transactions

```sql
-- Add new columns to existing tickets table
ALTER TABLE tickets ADD COLUMN payment_intent_id UUID REFERENCES payment_intents(id);
ALTER TABLE tickets ADD COLUMN refund_id UUID REFERENCES refunds(id);
ALTER TABLE tickets ADD COLUMN is_refunded BOOLEAN DEFAULT false;
ALTER TABLE tickets ADD COLUMN refunded_at TIMESTAMP;

-- Update status enum to include more states
ALTER TABLE tickets ALTER COLUMN status TYPE VARCHAR(50);
-- Status values: pending_payment, active, used, expired, canceled, refunded

-- Add indexes
CREATE INDEX idx_tickets_payment_intent ON tickets(payment_intent_id);
CREATE INDEX idx_tickets_refund ON tickets(refund_id);
CREATE INDEX idx_tickets_status ON tickets(status);
```

### 5. **organizer_payouts** (REFACTORED from payment_bills)

**Purpose**: Track admin-to-organizer settlements

```sql
CREATE TABLE organizer_payouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payout_number VARCHAR(50) UNIQUE NOT NULL,

    -- Parties
    organizer_id UUID NOT NULL REFERENCES users(id),
    event_id UUID NOT NULL REFERENCES events(id),
    admin_id UUID NOT NULL REFERENCES users(id),

    -- Financial Details
    gross_revenue DECIMAL(10, 2) NOT NULL, -- Total sales for event
    platform_commission DECIMAL(10, 2) NOT NULL,
    refund_adjustments DECIMAL(10, 2) DEFAULT 0,
    net_payout_amount DECIMAL(10, 2) NOT NULL, -- What organizer receives

    currency VARCHAR(3) NOT NULL DEFAULT 'USD',

    -- Payment Method
    payout_method VARCHAR(50) NOT NULL,
    -- stripe_transfer, bank_transfer, check, cash, other

    -- Stripe Connect (if applicable)
    stripe_transfer_id VARCHAR(255),
    stripe_connected_account_id VARCHAR(255),

    -- Status
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    -- pending, processing, completed, failed, reversed

    -- Metadata
    payment_reference VARCHAR(255),
    bank_details JSONB,
    notes TEXT,
    metadata JSONB,

    -- Transaction Coverage
    transaction_ids UUID[] NOT NULL, -- Which transactions are included
    transaction_count INT NOT NULL,

    -- Timestamps
    payout_date DATE,
    scheduled_date DATE,
    processed_at TIMESTAMP,
    failed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),

    -- Indexes
    INDEX idx_payouts_organizer (organizer_id),
    INDEX idx_payouts_event (event_id),
    INDEX idx_payouts_status (status),
    INDEX idx_payouts_created (created_at DESC)
);
```

### 6. **webhook_events** (NEW)

**Purpose**: Gateway-agnostic webhook event logging for debugging and replay

```sql
CREATE TABLE webhook_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Gateway Info
    payment_gateway VARCHAR(50) NOT NULL,
    -- stripe, paypal, esewa, khalti, etc.

    gateway_event_id VARCHAR(255) NOT NULL,
    -- Stripe: evt_xxx, PayPal: WH-xxx, etc.

    event_type VARCHAR(100) NOT NULL,
    -- payment_intent.succeeded, charge.refunded, etc.

    api_version VARCHAR(50),
    -- Gateway API version

    -- Processing
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    -- pending, processing, processed, failed, ignored

    processed_count INT DEFAULT 0,
    -- Number of processing attempts

    last_error TEXT,
    -- Last error message if processing failed

    -- Related Records
    payment_intent_id UUID REFERENCES payment_intents(id),
    transaction_id UUID REFERENCES transactions(id),
    refund_id UUID REFERENCES refunds(id),

    -- Raw Data
    payload JSONB NOT NULL,
    -- Full webhook payload for replay

    headers JSONB,
    -- HTTP headers (for signature verification)

    -- Timestamps
    received_at TIMESTAMP NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    -- Unique constraint per gateway
    UNIQUE(payment_gateway, gateway_event_id),

    -- Indexes
    INDEX idx_webhook_events_gateway (payment_gateway),
    INDEX idx_webhook_events_gateway_id (gateway_event_id),
    INDEX idx_webhook_events_type (event_type),
    INDEX idx_webhook_events_status (status),
    INDEX idx_webhook_events_received (received_at DESC)
);
```

### 7. **invoices** (NEW)

**Purpose**: Generate and store invoice records

```sql
CREATE TABLE invoices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_number VARCHAR(50) UNIQUE NOT NULL,

    -- Links
    transaction_id UUID NOT NULL REFERENCES transactions(id),
    user_id UUID REFERENCES users(id),
    guest_user_id UUID REFERENCES guest_users(id),

    -- Invoice Details
    invoice_type VARCHAR(50) NOT NULL,
    -- purchase, refund

    billing_name VARCHAR(255) NOT NULL,
    billing_email VARCHAR(255) NOT NULL,
    billing_address JSONB,

    -- Line Items
    line_items JSONB NOT NULL,

    -- Amounts
    subtotal DECIMAL(10, 2) NOT NULL,
    tax_amount DECIMAL(10, 2) DEFAULT 0,
    total_amount DECIMAL(10, 2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',

    -- Status
    status VARCHAR(50) NOT NULL DEFAULT 'draft',
    -- draft, issued, paid, refunded, void

    -- Files
    pdf_url TEXT,
    pdf_generated_at TIMESTAMP,

    -- Metadata
    notes TEXT,
    metadata JSONB,

    -- Timestamps
    issued_at TIMESTAMP,
    due_date DATE,
    paid_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),

    -- Indexes
    INDEX idx_invoices_transaction (transaction_id),
    INDEX idx_invoices_number (invoice_number),
    INDEX idx_invoices_user (user_id),
    INDEX idx_invoices_guest (guest_user_id)
);
```

### 8. **payment_audit_logs** (NEW)

**Purpose**: Comprehensive audit trail for all payment operations

```sql
CREATE TABLE payment_audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- What happened
    action VARCHAR(100) NOT NULL,
    -- payment_initiated, payment_succeeded, payment_failed, refund_initiated,
    -- refund_completed, payout_created, payout_completed, etc.

    entity_type VARCHAR(50) NOT NULL,
    -- payment_intent, transaction, refund, payout

    entity_id UUID NOT NULL,

    -- Who did it
    actor_id UUID REFERENCES users(id),
    actor_type VARCHAR(50),
    -- user, guest, admin, system, stripe_webhook

    actor_ip VARCHAR(45),

    -- Details
    before_state JSONB,
    after_state JSONB,
    changes JSONB,

    -- Context
    event_id UUID REFERENCES events(id),
    metadata JSONB,
    notes TEXT,

    -- Timestamp
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    -- Indexes
    INDEX idx_audit_logs_entity (entity_type, entity_id),
    INDEX idx_audit_logs_actor (actor_id),
    INDEX idx_audit_logs_event (event_id),
    INDEX idx_audit_logs_created (created_at DESC)
);
```

---

## Payment Flow Architecture

### Gateway Selection Logic

**Automatic Gateway Selection:**

```
1. Determine user's country (from IP or profile)
2. Get event's currency
3. Query enabled gateways for (country, currency)
4. Select gateway with highest priority
5. If no match, fallback to Stripe (if supported)
```

**Example Gateway Selection:**

```go
func SelectPaymentGateway(country, currency string) (*PaymentGateway, error) {
    // Get enabled gateways for country and currency
    gateways, err := db.GetEnabledGateways(country, currency)
    if err != nil {
        return nil, err
    }

    if len(gateways) == 0 {
        // Fallback to Stripe if supported
        if stripe.SupportsCurrency(currency) {
            return stripe, nil
        }
        return nil, errors.New("no payment gateway available")
    }

    // Return highest priority gateway
    return gateways[0], nil
}
```

**Gateway Priority Examples:**

- **Nepal (NPR)**: eSewa (priority 1) → Khalti (priority 2) → Stripe (priority 3)
- **USA (USD)**: Stripe (priority 1) → PayPal (priority 2)
- **India (INR)**: Razorpay (priority 1) → Stripe (priority 2)
- **UK (GBP)**: Stripe (priority 1) → PayPal (priority 2)

### Phase 1: Payment Initiation

```
[User/Guest] --> [FE: Select Tickets] --> [BE: Determine Gateway + Currency]
                                                    |
                                                    v
                                        [BE: Select Payment Gateway]
                                                    |
                                                    v
                                        [BE: Create Payment Intent]
                                                    |
                                                    v
                            [Gateway: Create Payment Intent/Order]
                                                    |
                                                    v
                            [BE: Save payment_intents record]
                                                    |
                                                    v
                            [BE: Create pending tickets]
                                                    |
                                                    v
                            [FE: Return gateway details + client_secret]
```

**Backend Logic:**

```go
// 1. Validate request (event, tier, quantity, user)
// 2. Determine user's country and preferred currency
// 3. Select appropriate payment gateway
// 4. Get current exchange rate (if currency conversion needed)
// 5. Calculate amounts (subtotal, fees, commission)
// 6. Generate idempotency key
// 7. Create Gateway Payment Intent with metadata
// 8. Save payment_intents record (status: pending, gateway: stripe/paypal/esewa)
// 9. Create tickets (status: pending_payment)
// 10. Return gateway name, client_secret, and amount to frontend
```

### Phase 2: Payment Processing

```
[FE: Gateway SDK] --> [Gateway: Process Payment]
                                    |
                                    v
                        [Gateway: Send Webhook Event]
                                    |
                                    v
                        [BE: Webhook Handler (Gateway-Specific)]
                                    |
                                    v
                        [BE: Verify Webhook Signature]
                                    |
                    +---------------+---------------+
                    |                               |
                    v                               |
            [Success Handler]              [Failure Handler]
                    |                               |
                    v                               v
        [Update payment_intent]          [Update payment_intent]
        [Create transaction]              [Cancel tickets]
        [Activate tickets]                [Restore inventory]
        [Send email w/ tickets]           [Send failure email]
        [Create invoice]                  [Log audit trail]
        [Log audit trail]
```

**Webhook Event Types to Handle:**

**Stripe:**

- `payment_intent.succeeded`
- `payment_intent.payment_failed`
- `payment_intent.canceled`
- `payment_intent.requires_action`
- `charge.refunded`
- `charge.dispute.created`

**PayPal:**

- `CHECKOUT.ORDER.APPROVED`
- `PAYMENT.CAPTURE.COMPLETED`
- `PAYMENT.CAPTURE.DENIED`
- `CUSTOMER.DISPUTE.CREATED`

**eSewa/Khalti (Nepal):**

- Custom webhook formats or polling mechanisms
- Transaction status verification via API

### Phase 3: Post-Payment

```
[Transaction Complete] --> [Generate Invoice PDF]
                                    |
                                    v
                        [Send Email with Tickets + Receipt]
                                    |
                                    v
                        [Tickets are scannable]
                                    |
                                    v
                        [Update Analytics]
```

### Payment Security Flow

**No Card Data Ever Stored:**

1. **Initiation Phase:**
   - Backend creates payment intent with gateway
   - Gateway returns `client_secret` or redirect URL
   - Frontend receives only gateway token (no card data)

2. **Payment Collection Phase:**

   ```
   Option A: Embedded (Stripe Elements, PayPal SDK)
   [Your Site] → [Gateway's Secure iFrame/Element]
                          ↓
                 [User enters card details]
                          ↓
              [Data goes directly to gateway]
                 (Never touches your server)

   Option B: Redirect (eSewa, Khalti)
   [Your Site] → [Redirect to Gateway Page]
                          ↓
                 [User enters payment details]
                          ↓
                 [Gateway processes payment]
                          ↓
              [Redirect back with status]
   ```

3. **Confirmation Phase:**
   - Gateway sends webhook with payment metadata
   - Your system stores: payment method type, card brand, last 4 digits
   - Gateway handles: full card number, CVV, expiry (securely)

4. **Record Keeping:**
   - Store gateway-provided metadata only
   - Example stored data:
     ```json
     {
       "payment_method_type": "card",
       "card_brand": "visa",
       "card_type": "credit",
       "last4": "4242",
       "exp_month": 12,
       "exp_year": 2025,
       "funding": "credit",
       "country": "US"
     }
     ```
   - Used for: Display on receipts, transaction history, reporting

---

## Refund Flow

### Refund Scenarios

1. **Event Cancellation by Organizer**
   - Admin initiates full refund for all tickets
   - Automatic approval
   - Stripe refund with reason: "event_canceled"

2. **Customer Request**
   - Customer requests refund before event
   - Requires admin approval
   - Partial refund possible based on policy

3. **Fraudulent Transaction**
   - Admin flags and refunds
   - Immediate processing

4. **Duplicate Purchase**
   - System detection or customer report
   - Admin verification and refund

### Refund Process

```
[Refund Request] --> [Admin Review] --> [Approval]
                                            |
                                            v
                                [Create refund record]
                                            |
                                            v
                        [Identify Payment Gateway from transaction]
                                            |
                                            v
                        [Gateway: Create Refund]
                        (Stripe/PayPal/eSewa/Khalti)
                                            |
                                            v
                        [Update payment_intent status]
                                            |
                                            v
                        [Update transaction status]
                                            |
                                            v
                        [Mark tickets as refunded]
                                            |
                                            v
                        [Adjust organizer balance]
                                            |
                                            v
                        [Send refund confirmation email]
                                            |
                                            v
                        [Log audit trail]
```

**Business Rules:**

- Full refund: 100% returned if >7 days before event
- Partial refund: 50% if 3-7 days before event
- No refund: <3 days before event (admin override possible)
- Platform fee: Non-refundable by default

---

## Organizer Payout Flow

### Payout Calculation

```
Organizer Payout = (Gross Revenue - Platform Commission - Refund Adjustments)
```

### Payout Methods

1. **Stripe Connect (Recommended)**
   - Automated transfers
   - Real-time tracking
   - Built-in reporting

2. **Manual Methods**
   - Bank transfer
   - Check
   - Cash
   - Other

### Payout Process

```
[Event Complete] --> [Admin: Calculate Payout]
                                |
                                v
                    [Create organizer_payout record]
                                |
                                v
                    [Admin: Select Method]
                                |
                +---------------+---------------+
                |                               |
                v                               v
        [Stripe Connect]                [Manual Payment]
                |                               |
                v                               v
    [Stripe: Create Transfer]       [Admin: Mark as paid]
                |                               |
                v                               v
        [Update payout status]          [Update payout status]
                |                               |
                +---------------+---------------+
                                |
                                v
                    [Send payout notification]
                                |
                                v
                    [Update organizer balance]
                                |
                                v
                    [Log audit trail]
```

---

## Security Measures

### 1. Idempotency

```go
// Generate idempotency key for payment retries
idempotencyKey := fmt.Sprintf("purchase-%s-%d", userID, time.Now().Unix())
```

### 2. Webhook Verification

**Gateway-Agnostic Webhook Verification:**

```go
// Verify webhook signature based on gateway
func VerifyWebhook(gateway string, payload []byte, signature string) (*WebhookEvent, error) {
    switch gateway {
    case "stripe":
        return verifyStripeWebhook(payload, signature)
    case "paypal":
        return verifyPayPalWebhook(payload, signature)
    case "esewa":
        return verifyEsewaWebhook(payload, signature)
    case "khalti":
        return verifyKhaltiWebhook(payload, signature)
    default:
        return nil, errors.New("unsupported gateway")
    }
}

// Stripe webhook verification
func verifyStripeWebhook(payload []byte, signature string) (*WebhookEvent, error) {
    event, err := webhook.ConstructEvent(
        payload,
        signature,
        stripeWebhookSecret,
    )
    if err != nil {
        return nil, err
    }
    return &WebhookEvent{
        Gateway: "stripe",
        EventID: event.ID,
        Type: event.Type,
        Data: event.Data.Object,
    }, nil
}
```

### 3. Amount Verification

```go
// Always recalculate amounts server-side
calculatedAmount := quantity * unitPrice
if req.Amount != calculatedAmount {
    return errors.New("amount mismatch")
}
```

### 4. Race Condition Prevention

```go
// Use database locks for inventory
tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).
    First(&tier, tierID)
```

### 5. Audit Logging

```go
// Log every financial operation
CreateAuditLog(action, entityType, entityID, actorID, changes)
```

---

## API Endpoints

### Payment Endpoints

#### 1. Get Available Payment Gateways

```
GET /api/v1/payments/gateways?country={country}&currency={currency}
```

**Response:**

```json
{
  "gateways": [
    {
      "name": "stripe",
      "display_name": "Credit/Debit Card",
      "supported_methods": ["card", "wallet"],
      "priority": 1,
      "fees": {
        "percentage": 2.9,
        "fixed": 0.3
      }
    },
    {
      "name": "esewa",
      "display_name": "eSewa Wallet",
      "supported_methods": ["wallet"],
      "priority": 2,
      "fees": {
        "percentage": 2.0,
        "fixed": 0
      }
    }
  ],
  "default_gateway": "stripe"
}
```

#### 2. Initiate Payment

```
POST /api/v1/payments/initiate
```

**Request:**

```json
{
  "event_id": "uuid",
  "tier_id": "uuid",
  "quantity": 2,
  "customer_email": "user@example.com",
  "payment_gateway": "stripe",
  "currency": "USD"
}
```

**Response:**

```json
{
  "payment_intent_id": "uuid",
  "payment_gateway": "stripe",
  "gateway_payment_id": "pi_xxx",
  "client_secret": "pi_xxx_secret_xxx",
  "amount": 5000,
  "currency": "USD",
  "exchange_rate": 1.0,
  "ticket_ids": ["uuid1", "uuid2"]
}
```

#### 3. Confirm Payment (Webhooks)

```
POST /api/v1/webhooks/stripe
POST /api/v1/webhooks/paypal
POST /api/v1/webhooks/esewa
POST /api/v1/webhooks/khalti
```

**Each webhook endpoint handles gateway-specific events**

#### 4. Get Payment Status

```
GET /api/v1/payments/{payment_intent_id}
```

**Response:**

```json
{
  "payment_intent_id": "uuid",
  "payment_gateway": "stripe",
  "status": "succeeded",
  "amount": 5000,
  "currency": "USD",
  "tickets": [...],
  "transaction": {...}
}
```

### Refund Endpoints

#### 1. Request Refund

```
POST /api/v1/refunds
```

**Request:**

```json
{
  "transaction_id": "uuid",
  "amount": 5000,
  "reason": "customer_request",
  "ticket_ids": ["uuid1", "uuid2"]
}
```

#### 2. Approve/Reject Refund (Admin)

```
PUT /api/v1/admin/refunds/{refund_id}/status
```

### Transaction Endpoints

#### 1. List User Transactions

```
GET /api/v1/user/transactions
```

#### 2. Get Transaction Details

```
GET /api/v1/transactions/{transaction_id}
```

#### 3. Download Invoice

```
GET /api/v1/transactions/{transaction_id}/invoice
```

### Payout Endpoints (Admin)

#### 1. Create Payout

```
POST /api/v1/admin/payouts
```

#### 2. List Payouts

```
GET /api/v1/admin/payouts
```

#### 3. Process Payout

```
POST /api/v1/admin/payouts/{payout_id}/process
```

---

## Email Templates

### 1. Payment Success Email

**Subject:** Your Tickets for [Event Name] - Order #[Transaction Number]

**Content:**

- Order confirmation
- Event details
- Ticket download links (JWT-protected)
- QR codes (embedded or linked)
- Receipt/Invoice download
- Cancellation policy
- Contact information

### 2. Payment Failed Email

**Subject:** Payment Issue for [Event Name]

**Content:**

- Payment failure notification
- Reason (if available)
- Retry payment link
- Support contact

### 3. Refund Confirmation Email

**Subject:** Refund Processed - Order #[Transaction Number]

**Content:**

- Refund confirmation
- Refund amount
- Expected arrival time
- Original order details

### 4. Payout Notification Email (Organizer)

**Subject:** Payout Processed - [Event Name]

**Content:**

- Payout confirmation
- Amount details
- Payment method
- Expected arrival
- Transaction summary

---

## Frontend Requirements

### 1. Payment Page (Stripe Elements)

```jsx
// Required Components
- CardElement
- PaymentIntentProvider
- Error handling
- Loading states
- Success/Failure redirects
```

### 2. Transaction History Page

```jsx
// Features
- Paginated transaction list
- Filter by status, date, event
- Transaction details modal
- Download invoice button
- Refund request button
```

### 3. Admin Payout Dashboard

```jsx
// Features
- Organizer list with balance
- Payout creation form
- Payout history
- Status tracking
- Export reports
```

---

## Testing Strategy

### Unit Tests

- Payment calculation logic
- Refund calculation
- Commission calculation
- Status transition validation

### Integration Tests

- Stripe API mock responses
- Webhook event simulation
- Database transaction rollback
- Race condition scenarios

### End-to-End Tests

- Complete purchase flow
- Payment success webhook
- Payment failure webhook
- Refund workflow
- Payout creation

---

## Monitoring & Alerting

### Metrics to Track

1. **Payment Success Rate**
   - Target: >98%
2. **Average Processing Time**
   - Target: <3 seconds

3. **Refund Rate**
   - Target: <5%

4. **Webhook Processing Success**
   - Target: >99%

5. **Failed Payments by Reason**

### Alerts

- Payment intent created but not processed >1 hour
- Webhook processing failures
- High refund rate for specific events
- Payout processing failures
- Amount discrepancies

---

## Migration Strategy

### Phase 1: Database Setup (Week 1)

1. Create new tables
2. Add columns to existing tables
3. Create indexes
4. Test migrations in staging

### Phase 2: Backend Implementation (Week 2-3)

1. Implement payment intent creation
2. Set up Stripe webhooks
3. Build refund system
4. Create payout management
5. Add audit logging

### Phase 3: Frontend Integration (Week 4)

1. Integrate Stripe Elements
2. Build transaction pages
3. Create admin dashboards
4. Test end-to-end flows

### Phase 4: Testing & Validation (Week 5)

1. Unit test coverage
2. Integration testing
3. Security audit
4. Performance testing

### Phase 5: Deployment (Week 6)

1. Staging deployment
2. Production deployment with feature flags
3. Gradual rollout
4. Monitor metrics

---

## Rollback Plan

### If Issues Arise

1. Feature flags to disable new system
2. Fallback to old checkout flow
3. Data reconciliation scripts
4. Support team notification

---

## Cost Analysis

### Payment Gateway Fees Comparison

#### Stripe

- **Card payments:** 2.9% + $0.30 per transaction
- **International cards:** +1.5%
- **Currency conversion:** +1%
- **Refunds:** No fee (original fee returned)
- **Payouts (Connect):** Free for standard accounts

#### PayPal

- **Domestic:** 2.9% + $0.30 per transaction
- **International:** 4.4% + fixed fee
- **Currency conversion:** 2.5-4%
- **Refunds:** Fee returned if refunded within 60 days

#### eSewa (Nepal)

- **Transaction fee:** 2% (negotiable for high volume)
- **No fixed fee**
- **NPR only**
- **Instant settlement**

#### Khalti (Nepal)

- **Transaction fee:** 2.5%
- **No fixed fee**
- **NPR only**
- **T+1 settlement**

### Example Calculations

#### Stripe (USD)

```
Ticket Price: $50
Quantity: 2
Subtotal: $100

Stripe Fee: (100 * 0.029) + 0.30 = $3.20
Platform Commission (10%): $10.00
Organizer Share: $86.80
```

#### eSewa (NPR)

```
Ticket Price: रू 1,000
Quantity: 2
Subtotal: रू 2,000

eSewa Fee: 2,000 * 0.02 = रू 40
Platform Commission (10%): रू 200
Organizer Share: रू 1,760
```

#### Multi-Currency Example

```
Event Currency: USD $50
User Location: Nepal
Display Price: रू 6,625 (exchange rate: 132.50)
Payment Gateway: eSewa (NPR)

Payment Processed: रू 6,625
eSewa Fee: रू 132.50 (2%)
Platform Commission: रू 662.50 (10%)
Organizer Share: रू 5,830

Base Currency (USD): $44 (after fees)
```

---

## Best Practices Checklist

### Payment Gateway Management

✅ Use gateway abstraction layer for all payment operations
✅ Store gateway name with every transaction for proper routing
✅ Implement fallback gateway selection
✅ Test each gateway thoroughly in sandbox/test mode
✅ Monitor gateway uptime and switch if needed

### Multi-Currency Handling

✅ Update exchange rates daily via automated job
✅ Store exchange rate used at transaction time
✅ Display prices in user's preferred currency
✅ Process payment in gateway's supported currency
✅ Record base currency amount for financial reporting

### Security & Reliability

✅ Use idempotency keys for retries (gateway-agnostic)
✅ Verify webhook signatures for each gateway
✅ Always recalculate amounts server-side
✅ Log all financial operations to audit trail
✅ Use database transactions for atomic operations
✅ Handle concurrent requests with row-level locking
✅ Store all gateway IDs for reconciliation
✅ Encrypt gateway credentials in database

### Payment Processing

✅ Generate invoices for every transaction
✅ Email tickets only after payment confirmation
✅ Implement graceful error handling per gateway
✅ Set up comprehensive monitoring and alerting
✅ Regular reconciliation with gateway dashboards
✅ Test refund scenarios for each gateway
✅ Document all financial calculations
✅ Handle partial payments appropriately
✅ Support payment retries with proper state management

### Testing Requirements

✅ Unit tests for each gateway adapter
✅ Integration tests with test/sandbox credentials
✅ Multi-currency conversion testing
✅ Webhook handling for all event types
✅ Refund flow testing per gateway
✅ Load testing for high-volume events
✅ Security penetration testing
✅ Cross-browser payment form testing

---

## Next Steps

### Immediate Actions (Week 1)

1. **Review this document** with development and finance teams
2. **Get approval** for database schema changes and multi-gateway architecture
3. **Set up gateway accounts:**
   - Stripe (test + production) - **Priority 1**
   - PayPal sandbox account - **Priority 2** (optional)
   - eSewa/Khalti test accounts - **Future**
4. **Create feature branch** for implementation
5. **Set up currency exchange rate API** (e.g., exchangerate-api.com)

### Phase 1: Foundation (Week 2-3)

1. Create `payment_gateway_configs` table
2. Create `currency_exchange_rates` table
3. Implement gateway abstraction interface
4. Build Stripe gateway adapter
5. Create gateway factory/registry
6. Implement currency conversion service

### Phase 2: Core Features (Week 4-5)

1. Update payment initiation to use gateway selection
2. Refactor webhook handlers to be gateway-aware
3. Update transaction recording with gateway info
4. Implement multi-currency display and processing
5. Add gateway configuration admin panel
6. Create exchange rate update cron job

### Phase 3: Testing & Deployment (Week 6-7)

1. Comprehensive testing (unit, integration, E2E)
2. Test multi-currency flows
3. Test Stripe in sandbox mode
4. Security audit
5. Staging deployment
6. Gradual production rollout with feature flags
7. Monitor metrics and error rates

### Phase 4: Future Enhancements (Week 8+)

1. Add PayPal integration (if needed)
2. Research and add local payment gateways (eSewa, Khalti)
3. Implement split payments for multi-organizer events
4. Add subscription-based ticketing
5. Implement payment plans/installments
6. Add Apple Pay / Google Pay support

---

## Implementation Guide

### Phase 1: Gateway Abstraction Layer (Week 1-2)

1. **Create Gateway Interface**
   - Define `PaymentGateway` interface
   - Create base implementations for Stripe
   - Add gateway factory/registry pattern

2. **Database Schema**
   - Create `payment_gateway_configs` table
   - Migrate existing data to new multi-gateway schema
   - Add gateway-agnostic fields to existing tables

3. **Gateway Configuration**
   - Add gateway config management (admin panel)
   - Implement encrypted credential storage
   - Add gateway enable/disable functionality

### Phase 2: Stripe Implementation (Week 3-4)

1. **Refactor Existing Stripe Code**
   - Move Stripe logic to implement `PaymentGateway` interface
   - Update payment initiation to use abstraction layer
   - Update webhook handler to be gateway-aware

2. **Multi-Currency Support**
   - Implement currency conversion service
   - Add exchange rate management
   - Update pricing calculations

3. **Testing**
   - Unit tests for gateway abstraction
   - Integration tests with Stripe test mode
   - Multi-currency flow testing

### Phase 3: Additional Gateways (Week 5+)

1. **PayPal Integration** (Optional)
   - Implement PayPal gateway adapter
   - Add PayPal webhook handler
   - Test PayPal flows

2. **Local Gateway (eSewa/Khalti)** (Future)
   - Research gateway APIs
   - Implement adapter for chosen gateway
   - Add webhook/callback handlers
   - Regional testing

### Gateway Implementation Examples

#### Stripe Gateway Adapter

```go
package gateways

import (
    "context"
    "github.com/stripe/stripe-go/v74"
    "github.com/stripe/stripe-go/v74/paymentintent"
)

type StripeGateway struct {
    apiKey        string
    webhookSecret string
}

func NewStripeGateway(apiKey, webhookSecret string) *StripeGateway {
    stripe.Key = apiKey
    return &StripeGateway{
        apiKey:        apiKey,
        webhookSecret: webhookSecret,
    }
}

func (g *StripeGateway) CreatePaymentIntent(ctx context.Context, req *PaymentIntentRequest) (*PaymentIntentResponse, error) {
    params := &stripe.PaymentIntentParams{
        Amount:   stripe.Int64(int64(req.Amount * 100)),
        Currency: stripe.String(req.Currency),
        Metadata: req.Metadata,
    }
    params.SetIdempotencyKey(req.IdempotencyKey)

    pi, err := paymentintent.New(params)
    if err != nil {
        return nil, err
    }

    return &PaymentIntentResponse{
        GatewayPaymentID: pi.ID,
        ClientSecret:     pi.ClientSecret,
        Status:           string(pi.Status),
        Amount:           req.Amount,
        Currency:         req.Currency,
    }, nil
}

func (g *StripeGateway) GetName() string {
    return "stripe"
}

func (g *StripeGateway) CalculateFees(amount float64, currency string) float64 {
    return (amount * 0.029) + 0.30 // 2.9% + $0.30
}
```

#### Gateway Factory

```go
package gateways

type GatewayFactory struct {
    gateways map[string]PaymentGateway
}

func NewGatewayFactory() *GatewayFactory {
    return &GatewayFactory{
        gateways: make(map[string]PaymentGateway),
    }
}

func (f *GatewayFactory) RegisterGateway(name string, gateway PaymentGateway) {
    f.gateways[name] = gateway
}

func (f *GatewayFactory) GetGateway(name string) (PaymentGateway, error) {
    gateway, exists := f.gateways[name]
    if !exists {
        return nil, errors.New("gateway not found")
    }
    return gateway, nil
}
```

---

## Appendix A: Status Flow Diagrams

### Payment Intent Status Flow

```
pending → processing → succeeded
                    → requires_action → succeeded
                    → failed
                    → canceled
```

### Transaction Status Flow

```
completed → refunded
         → partially_refunded
         → disputed
```

### Refund Status Flow

```
pending → processing → succeeded
                    → failed
                    → canceled
```

### Ticket Status Flow

```
pending_payment → active → used
                        → expired
               → canceled
               → refunded
```

---

## Appendix B: Sample Code Snippets

### Create Payment Intent

```go
func (s *PaymentService) CreatePaymentIntent(req *CreatePaymentRequest) (*PaymentIntent, error) {
    // 1. Validate and calculate
    tier, err := s.getTier(req.TierID)
    if err != nil {
        return nil, err
    }

    subtotal := tier.Price * float64(req.Quantity)
    platformFee := subtotal * (s.commissionRate / 100)
    total := subtotal + platformFee

    // 2. Generate idempotency key
    idempotencyKey := fmt.Sprintf("purchase-%s-%d", req.UserID, time.Now().UnixNano())

    // 3. Create Stripe Payment Intent
    params := &stripe.PaymentIntentParams{
        Amount:   stripe.Int64(int64(total * 100)), // Convert to cents
        Currency: stripe.String(string(stripe.CurrencyUSD)),
        Metadata: map[string]string{
            "event_id":  req.EventID.String(),
            "tier_id":   req.TierID.String(),
            "quantity":  strconv.Itoa(req.Quantity),
            "user_id":   req.UserID.String(),
        },
    }
    params.SetIdempotencyKey(idempotencyKey)

    stripePI, err := paymentintent.New(params)
    if err != nil {
        return nil, err
    }

    // 4. Save to database
    pi := &PaymentIntent{
        StripePaymentIntentID: stripePI.ID,
        StripeClientSecret:    stripePI.ClientSecret,
        IdempotencyKey:        idempotencyKey,
        UserID:                &req.UserID,
        EventID:               req.EventID,
        TierID:                req.TierID,
        Quantity:              req.Quantity,
        Subtotal:              subtotal,
        PlatformFee:           platformFee,
        TotalAmount:           total,
        Status:                "pending",
    }

    if err := s.db.Create(pi).Error; err != nil {
        return nil, err
    }

    // 5. Create pending tickets
    tickets, err := s.createPendingTickets(pi)
    if err != nil {
        return nil, err
    }

    // 6. Log audit
    s.auditLog("payment_initiated", pi.ID, req.UserID)

    return pi, nil
}
```

---

## Frequently Asked Questions

### General Questions

**Q: What happens if a webhook is delayed?**  
A: We implement multiple fallback mechanisms:

- Poll payment intent status after 5 minutes if no webhook received
- Background job checks pending payments >1 hour old
- Manual webhook replay via admin panel
- Direct gateway API polling for critical transactions

**Q: How do we handle partial refunds?**  
A: Admin specifies ticket IDs to refund. System:

- Calculates pro-rated amounts per ticket
- Creates refund record with affected ticket IDs
- Calls gateway-specific refund API with calculated amount
- Updates transaction status to `partially_refunded`
- Marks individual tickets as refunded

**Q: How do we prevent double-spending?**  
A: Multiple layers of protection:

- Idempotency keys for payment retries (prevents duplicate payment intents)
- Database row-level locks on inventory (`SELECT FOR UPDATE NOWAIT`)
- Transaction isolation level: `READ COMMITTED`
- Unique constraints on gateway payment IDs
- Atomic ticket creation within payment transaction

**Q: When is the invoice generated?**  
A: Generated asynchronously after payment success:

- Webhook confirms payment → Queue invoice generation job
- Invoice PDF generated and stored in S3/cloud storage
- Invoice URL stored in transaction record
- Email sent with invoice attachment
- Typical generation time: 5-30 seconds

### Multi-Gateway Questions

**Q: How do we choose which payment gateway to use?**  
A: Automatic gateway selection based on:

1. User's country (detected from IP or profile)
2. Event's currency (set by organizer)
3. Enabled gateways that support (country, currency) combination
4. Gateway priority (configurable per region)
5. Fallback to Stripe if available

**Q: Can users choose their preferred payment gateway?**  
A: Yes, optionally:

- Frontend displays available gateways for user's location
- User selects preferred method (card, wallet, bank transfer)
- System routes to appropriate gateway
- Some regions may have only one available gateway

**Q: What happens if a gateway goes down during checkout?**  
A: Graceful degradation:

- Frontend catches gateway initialization errors
- Offer alternative gateway if available
- Clear error message to user
- Retry mechanism with exponential backoff
- Admin alert for extended outages
- Option to manually switch default gateway

**Q: How do we handle currency conversion?**  
A: Multi-step process:

- Display event price in organizer's currency (e.g., $50 USD)
- Detect user's location and preferred currency
- Fetch current exchange rate from API
- Display converted price (e.g., रू 6,625 NPR)
- Process payment in gateway's supported currency
- Store exchange rate used at transaction time
- Financial reports use base currency (USD) for consistency

**Q: Can organizers choose their payout currency?**  
A: Yes:

- Organizers set preferred payout currency in profile
- System converts ticket sales to payout currency
- Exchange rate applied at payout time (not purchase time)
- Multi-currency balance tracking per organizer
- Settlement in organizer's bank account currency

### Refund Questions

**Q: What if Stripe Connect account is not set up for organizer?**  
A: System supports multiple payout methods:

- Stripe Connect: Automated (preferred)
- Bank transfer: Manual entry by admin
- Check: Manual tracking
- Cash: Manual confirmation
- Other: Custom methods

**Q: How are gateway fees handled during refunds?**  
A: Depends on gateway and refund type:

- **Stripe**: Original fee returned on full refund
- **PayPal**: Fee returned if refunded within 60 days
- **eSewa/Khalti**: Fee typically non-refundable
- Partial refunds: Pro-rated fee calculation
- Platform commission: Configurable (refunded or retained)

**Q: Can we refund to a different payment method?**  
A: Generally no:

- Refunds must go back to original payment method (gateway requirement)
- Security and anti-fraud measure
- Exception: Manual refund via bank transfer (admin approval required)
- Alternative: Issue credit/voucher for future purchase

### Security Questions

**Q: How are gateway credentials stored?**  
A: Secure storage approach:

- Encrypted at rest using AES-256
- Stored in `payment_gateway_configs` table
- Decrypted only when needed for API calls
- Never exposed in API responses
- Separate encryption keys per environment
- Key rotation policy every 90 days

**Q: How do we verify webhook authenticity?**  
A: Gateway-specific signature verification:

- **Stripe**: HMAC signature with webhook secret
- **PayPal**: Certificate validation + signature check
- **eSewa**: Shared secret verification
- Failed verification = reject webhook
- Log all verification attempts for audit

**Q: What about PCI compliance?**  
A: **Zero PCI burden** - gateway-handled compliance:

**What This Means for Your Platform:**

- ✅ **No card data ever stored** on your servers
- ✅ **No card data transmission** through your servers
- ✅ **Gateway-provided payment forms** (Stripe Elements, PayPal SDK, etc.)
- ✅ **Card details entered directly** on gateway's secure page/iframe
- ✅ **Your servers never see** full card numbers, CVV, or PINs
- ✅ **Platform is PCI-DSS SAQ-A compliant** (simplest level)

**What You Do Store (Safe Metadata):**

- Payment method type (card, wallet, bank)
- Card brand (Visa, Mastercard, Amex) - provided by gateway
- Card type (credit, debit) - provided by gateway
- Last 4 digits - provided by gateway after payment
- Expiry month/year - provided by gateway for display
- Gateway tokens and payment IDs

**Integration Methods:**

1. **Stripe**: Stripe Elements (iframes) or Checkout Session (redirect)
2. **PayPal**: PayPal SDK (popup) or redirect to PayPal
3. **eSewa/Khalti**: Redirect to gateway payment page
4. **Result**: Card data never touches your infrastructure

**Compliance Responsibility:**

- **Gateway (Stripe/PayPal/etc.)**: PCI-DSS Level 1 certified
- **Your Platform**: SAQ-A (annual self-assessment questionnaire)
- **No need for**: Security audits, penetration testing for card data, extensive PCI documentation

**Q: What payment method information do we store for records?**  
A: Only **non-sensitive metadata** provided by the gateway after successful payment:

**For Card Payments:**

```json
{
  "payment_method_type": "card",
  "card_brand": "visa", // visa, mastercard, amex, discover
  "card_type": "credit", // credit, debit, prepaid
  "last4": "4242", // Last 4 digits only
  "exp_month": 12, // For display/reference
  "exp_year": 2025,
  "funding": "credit",
  "country": "US", // Card issuing country
  "bank": "Chase" // Optional
}
```

**For Wallet Payments (eSewa, Khalti, PayPal):**

```json
{
  "payment_method_type": "wallet",
  "wallet_type": "esewa",
  "wallet_id": "98XXXXX45", // Masked wallet ID
  "wallet_name": "John Doe"
}
```

**For Bank Transfer:**

```json
{
  "payment_method_type": "bank_transfer",
  "bank_name": "Nepal Bank Limited",
  "account_last4": "1234" // Last 4 digits only
}
```

**Usage of This Data:**

- Display on transaction receipts ("Paid with Visa ••••4242")
- Transaction history for users
- Financial reporting and analytics
- Refund routing (refund to same payment method)
- Fraud detection patterns (unusual payment methods)

**What We NEVER Store:**

- ❌ Full card numbers (16 digits)
- ❌ CVV/CVC security codes
- ❌ Full expiry dates in payment processing
- ❌ Card PINs or passwords
- ❌ Full bank account numbers
- ❌ Wallet login credentials

### Technical Questions

**Q: What if we need to add a new payment gateway?**  
A: Simple extension process:

1. Create new gateway adapter implementing `PaymentGateway` interface
2. Add gateway configuration to database
3. Register gateway in factory
4. Create webhook endpoint for new gateway
5. Add tests for new gateway
6. Deploy and enable via admin panel
7. No changes to core payment logic required

**Q: How do we handle multiple currencies in financial reports?**  
A: Standardized reporting:

- All transactions store `base_currency_amount` (USD)
- Exchange rate recorded at transaction time
- Reports generated in base currency for consistency
- Optional: Multi-currency breakdown available
- Reconciliation uses base currency amounts

**Q: What about webhook retry mechanisms?**  
A: Robust retry handling:

- Store all webhook events in `webhook_events` table
- Track processing status (pending, processing, processed, failed)
- Retry failed webhooks with exponential backoff
- Maximum 5 retry attempts
- Admin dashboard for manual webhook replay
- Alert after 3 failed attempts

---

**Document Version:** 2.0  
**Last Updated:** February 5, 2026  
**Status:** ✅ Ready for Review & Implementation  
**Architecture:** Multi-Gateway | Multi-Currency | Globally Scalable
