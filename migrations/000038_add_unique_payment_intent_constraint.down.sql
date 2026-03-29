-- Rollback: Remove unique constraint on payment_intent_id
DROP INDEX IF EXISTS idx_transactions_unique_payment_intent_id;
