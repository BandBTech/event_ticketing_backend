-- Restore payment_intent_id column to tickets table (rollback)
-- This restores the redundant column if needed for rollback
-- Note: Data will be lost unless preserved before migration

ALTER TABLE tickets 
ADD COLUMN payment_intent_id TEXT;

CREATE INDEX idx_tickets_payment_intent_id ON tickets(payment_intent_id) WHERE payment_intent_id IS NOT NULL;
