-- Rollback multi-gateway architecture migration
-- This restores the old gateway-specific design

-- Drop payment_attempts table
DROP TABLE IF EXISTS payment_attempts CASCADE;

-- Restore payment_intents fields
ALTER TABLE payment_intents
  ADD COLUMN IF NOT EXISTS payment_gateway VARCHAR(50),
  ADD COLUMN IF NOT EXISTS gateway_payment_id VARCHAR(255),
  ADD COLUMN IF NOT EXISTS gateway_charge_id VARCHAR(255),
  ADD COLUMN IF NOT EXISTS payment_method_type VARCHAR(50),
  ADD COLUMN IF NOT EXISTS payment_method_details JSONB,
  ADD COLUMN IF NOT EXISTS gateway_response JSONB,
  ADD COLUMN IF NOT EXISTS gateway_metadata JSONB,
  ADD COLUMN IF NOT EXISTS customer_email VARCHAR(255),
  ADD COLUMN IF NOT EXISTS customer_name VARCHAR(255),
  ADD COLUMN IF NOT EXISTS customer_phone VARCHAR(50);

-- Restore transactions table
ALTER TABLE transactions
  RENAME COLUMN provider TO payment_gateway;

ALTER TABLE transactions
  RENAME COLUMN provider_txn_id TO gateway_txn_id;

ALTER TABLE transactions
  DROP COLUMN IF EXISTS provider_reference_id,
  RENAME COLUMN provider_data TO gateway_data;

ALTER TABLE transactions
  DROP COLUMN IF EXISTS is_paid_out,
  DROP COLUMN IF EXISTS paid_out_at;

DROP INDEX IF EXISTS idx_transactions_provider;
DROP INDEX IF EXISTS idx_transactions_provider_txn_id;

-- Restore refunds table
ALTER TABLE refunds
  ADD COLUMN IF NOT EXISTS payment_gateway VARCHAR(50),
  DROP COLUMN IF EXISTS provider;

ALTER TABLE refunds
  ADD COLUMN IF NOT EXISTS gateway_refund_id VARCHAR(255),
  DROP COLUMN IF EXISTS provider_refund_id;

-- Restore amount as float and copy from int64
ALTER TABLE refunds
  ADD COLUMN IF NOT EXISTS amount_float DECIMAL(10,2);

UPDATE refunds SET amount_float = amount / 100.0 WHERE amount_float IS NULL;

ALTER TABLE refunds
  DROP COLUMN IF EXISTS amount,
  RENAME COLUMN amount_float TO amount;

-- Restore other fields
ALTER TABLE refunds
  ADD COLUMN IF NOT EXISTS exchange_rate DECIMAL(10,6) DEFAULT 1.000000,
  ADD COLUMN IF NOT EXISTS base_currency VARCHAR(3) DEFAULT 'USD',
  ADD COLUMN IF NOT EXISTS base_currency_amount DECIMAL(10,2),
  ADD COLUMN IF NOT EXISTS gateway_fee_refund DECIMAL(10,2);

ALTER TABLE refunds
  DROP COLUMN IF EXISTS provider_data,
  DROP COLUMN IF EXISTS provider_error_message;

-- Restore is_full_transaction_refund field
ALTER TABLE refunds
  ADD COLUMN IF NOT EXISTS is_full_transaction_refund BOOLEAN DEFAULT FALSE;

ALTER TABLE refunds
  DROP COLUMN IF EXISTS platform_fee_refund,
  ADD COLUMN IF NOT EXISTS commission_refund DECIMAL(10,2);

DROP INDEX IF EXISTS idx_refunds_provider;
DROP INDEX IF EXISTS idx_refunds_provider_refund_id;
DROP INDEX IF EXISTS idx_refunds_status;

-- Restore WebhookEvent table
ALTER TABLE IF EXISTS webhook_events
  RENAME COLUMN IF EXISTS provider TO payment_gateway;

ALTER TABLE IF EXISTS webhook_events
  DROP COLUMN IF EXISTS provider,
  DROP COLUMN IF EXISTS gateway_event_id;

DROP INDEX IF EXISTS idx_webhook_events_provider_gateway_event_id;
