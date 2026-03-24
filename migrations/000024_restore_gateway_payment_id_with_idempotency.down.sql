-- Rollback: Remove gateway_payment_id and related indexes
DROP INDEX IF EXISTS idx_payment_intents_gateway_uniq;
DROP INDEX IF EXISTS idx_payment_intents_gateway_payment_id;
DROP INDEX IF EXISTS idx_payment_intents_idempotency_key;
DROP INDEX IF EXISTS idx_transactions_payment_intent_id;
DROP INDEX IF EXISTS idx_payment_intents_status_created;
DROP INDEX IF EXISTS idx_event_tiers_status;

-- Remove the column
ALTER TABLE payment_intents DROP COLUMN IF EXISTS gateway_payment_id;
