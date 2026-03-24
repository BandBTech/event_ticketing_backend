-- Rollback: Remove idempotency constraints and indexes
DROP INDEX IF EXISTS idx_payment_intents_pending_timeout;
DROP INDEX IF EXISTS idx_payment_intents_status_created;
DROP INDEX IF EXISTS idx_payment_intents_checkout_token_status;
DROP INDEX IF EXISTS idx_payment_intents_idempotency_key_status;
ALTER TABLE payment_intents
DROP CONSTRAINT IF EXISTS unique_gateway_payment_id;
