-- Restore gateway_payment_id column with proper idempotency constraints
-- This is part of the production-grade payment redesign for handling peak load safely

-- 1. Add gateway_payment_id column to payment_intents (null for cash payments)
ALTER TABLE payment_intents ADD COLUMN IF NOT EXISTS gateway_payment_id VARCHAR(255);

-- 2. Create unique constraint on (gateway, gateway_payment_id)
-- This ensures: only one PaymentIntent per gateway payment (no duplicates)
-- Null values in gateway_payment_id are NOT unique (allows multiple cash/pending payments)
CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_intents_gateway_uniq 
  ON payment_intents(payment_gateway, gateway_payment_id) 
  WHERE gateway_payment_id IS NOT NULL;

-- 3. Create index for fast lookups by gateway_payment_id
CREATE INDEX IF NOT EXISTS idx_payment_intents_gateway_payment_id 
  ON payment_intents(gateway_payment_id);

-- 4. Add unique constraint on idempotency_key (already done, but ensuring it persists)
CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_intents_idempotency_key 
  ON payment_intents(idempotency_key);

-- 5. Improve transaction lookup by payment_intent_id
CREATE INDEX IF NOT EXISTS idx_transactions_payment_intent_id 
  ON transactions(payment_intent_id) 
  WHERE payment_intent_id IS NOT NULL;

-- 6. Composite index for fast status checks on payment intents
CREATE INDEX IF NOT EXISTS idx_payment_intents_status_created 
  ON payment_intents(status, created_at DESC);

-- 7. Composite index for ticket allocation (critical for peak load)
CREATE INDEX IF NOT EXISTS idx_event_tiers_status 
  ON event_tiers(event_id, available) 
  WHERE available > 0;

-- This migration maintains backward compatibility:
-- - Existing payment_intents keep their data
-- - New payments will have gateway_payment_id set
-- - Cash payments can have NULL gateway_payment_id
-- - No column name changes (preserves existing queries)
