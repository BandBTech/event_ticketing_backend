-- ========================================
-- MULTI-GATEWAY ARCHITECTURE MIGRATION
-- Future-proof design: Provider-agnostic
-- NO schema change needed when adding gateways
-- ========================================

-- 1. Create payment_attempts table (UNIVERSAL GATEWAY LAYER)
-- This is the key to multi-gateway support
CREATE TABLE IF NOT EXISTS payment_attempts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  
  -- 🔗 Core Links
  payment_intent_id UUID NOT NULL REFERENCES payment_intents(id) ON DELETE CASCADE,
  transaction_id UUID REFERENCES transactions(id) ON DELETE SET NULL,
  
  -- 🌍 Universal Provider Abstraction
  provider VARCHAR(50) NOT NULL, -- stripe, paypal, esewa, khalti, razorpay, paypay
  provider_reference_id VARCHAR(255),
  provider_session_id VARCHAR(255),
  provider_charge_id VARCHAR(255),
  
  -- 💰 Amount Snapshot
  amount BIGINT NOT NULL,
  currency VARCHAR(3) NOT NULL,
  
  -- 📊 Status
  status VARCHAR(50) NOT NULL DEFAULT 'initiated',
  -- initiated, pending, succeeded, failed, cancelled, expired, requires_action
  
  -- 🔐 Safe Metadata Only
  payment_method_type VARCHAR(50),
  payment_method_details JSONB,
  provider_data JSONB,
  provider_error_reason TEXT,
  
  -- ⏰ Timestamps
  initiated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  succeeded_at TIMESTAMP,
  failed_at TIMESTAMP,
  cancelled_at TIMESTAMP,
  expires_at TIMESTAMP,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMP,
  
  CONSTRAINT fk_payment_intent FOREIGN KEY(payment_intent_id) REFERENCES payment_intents(id),
  CONSTRAINT fk_transaction FOREIGN KEY(transaction_id) REFERENCES transactions(id)
);

CREATE INDEX idx_payment_attempts_payment_intent_id ON payment_attempts(payment_intent_id);
CREATE INDEX idx_payment_attempts_transaction_id ON payment_attempts(transaction_id);
CREATE INDEX idx_payment_attempts_provider ON payment_attempts(provider);
CREATE INDEX idx_payment_attempts_provider_reference ON payment_attempts(provider_reference_id);
CREATE INDEX idx_payment_attempts_status ON payment_attempts(status);
CREATE INDEX idx_payment_attempts_created_at ON payment_attempts(created_at);

-- 2. ALTER payment_intents table - SIMPLIFY to pure orchestration
ALTER TABLE payment_intents
  DROP COLUMN IF EXISTS payment_gateway,
  DROP COLUMN IF EXISTS gateway_payment_id,
  DROP COLUMN IF EXISTS gateway_charge_id,
  DROP COLUMN IF EXISTS payment_method_type,
  DROP COLUMN IF EXISTS payment_method_details,
  DROP COLUMN IF EXISTS gateway_response,
  DROP COLUMN IF EXISTS gateway_metadata,
  DROP COLUMN IF EXISTS customer_email,
  DROP COLUMN IF EXISTS customer_name,
  DROP COLUMN IF EXISTS customer_phone;

-- Add new orchestration fields if they don't exist
ALTER TABLE payment_intents
  ADD COLUMN IF NOT EXISTS tier_id UUID REFERENCES event_tiers(id),
  ADD COLUMN IF NOT EXISTS quantity INT;

-- 3. ALTER transactions table - Make GATEWAY-NEUTRAL
ALTER TABLE transactions
  RENAME COLUMN payment_gateway TO provider;

ALTER TABLE transactions
  RENAME COLUMN gateway_txn_id TO provider_txn_id;

ALTER TABLE transactions
  ADD COLUMN IF NOT EXISTS provider_reference_id VARCHAR(255),
  RENAME COLUMN gateway_data TO provider_data;

ALTER TABLE transactions
  ADD COLUMN IF NOT EXISTS is_paid_out BOOLEAN DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS paid_out_at TIMESTAMP;

CREATE INDEX idx_transactions_provider ON transactions(provider);
CREATE INDEX idx_transactions_provider_txn_id ON transactions(provider_txn_id);

-- 4. ALTER refunds table - Make GATEWAY-NEUTRAL
ALTER TABLE refunds
  DROP COLUMN IF EXISTS payment_gateway,
  ADD COLUMN IF NOT EXISTS provider VARCHAR(50) NOT NULL DEFAULT 'stripe';

ALTER TABLE refunds
  DROP COLUMN IF EXISTS gateway_refund_id,
  ADD COLUMN IF NOT EXISTS provider_refund_id VARCHAR(255) NOT NULL;

ALTER TABLE refunds
  DROP COLUMN IF EXISTS exchange_rate,
  DROP COLUMN IF EXISTS base_currency,
  DROP COLUMN IF EXISTS base_currency_amount,
  DROP COLUMN IF EXISTS gateway_fee_refund;

-- Make amount int64 (cents/paisa) instead of float
ALTER TABLE refunds
  ADD COLUMN IF NOT EXISTS amount_cents BIGINT;

-- If amount column exists and is float, set amount_cents from it
UPDATE refunds SET amount_cents = FLOOR(amount * 100) WHERE amount_cents IS NULL;

-- Drop old amount column if still float
ALTER TABLE refunds
  DROP COLUMN IF EXISTS amount CASCADE;

-- Rename to proper name
ALTER TABLE refunds
  RENAME COLUMN amount_cents TO amount;

-- Update refund fields
ALTER TABLE refunds
  ADD COLUMN IF NOT EXISTS platform_fee_refund BIGINT DEFAULT 0,
  ADD COLUMN IF NOT EXISTS organizer_refund BIGINT DEFAULT 0,
  RENAME COLUMN commission_refund TO platform_fee_refund;

-- Convert provider_data
ALTER TABLE refunds
  ADD COLUMN IF NOT EXISTS provider_data JSONB,
  ADD COLUMN IF NOT EXISTS provider_error_message TEXT;

-- Add organizer_refund if missing
ALTER TABLE refunds
  ADD COLUMN IF NOT EXISTS organizer_refund BIGINT DEFAULT 0;

CREATE INDEX idx_refunds_provider ON refunds(provider);
CREATE INDEX idx_refunds_provider_refund_id ON refunds(provider_refund_id);
CREATE INDEX idx_refunds_status ON refunds(status);

-- 5. Ensure WebhookEvent table uses universal provider field
ALTER TABLE IF EXISTS webhook_events
  RENAME COLUMN IF EXISTS payment_gateway TO provider;

-- Add any missing fields
ALTER TABLE IF EXISTS webhook_events
  ADD COLUMN IF NOT EXISTS provider VARCHAR(50),
  ADD COLUMN IF NOT EXISTS gateway_event_id VARCHAR(255);

-- Make sure it's unique on provider + gateway_event_id
CREATE UNIQUE INDEX IF NOT EXISTS idx_webhook_events_provider_gateway_event_id 
  ON webhook_events(provider, gateway_event_id);
