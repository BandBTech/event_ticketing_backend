-- Rollback: Remove PaymentIntentID from checkout_sessions
DROP INDEX IF EXISTS idx_checkout_sessions_payment_intent_id;

ALTER TABLE checkout_sessions 
DROP COLUMN IF EXISTS payment_intent_id;
