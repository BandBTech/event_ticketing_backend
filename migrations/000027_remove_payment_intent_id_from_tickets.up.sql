-- Remove redundant payment_intent_id from tickets table
-- Payment intent ID is now only stored in payment_intents table and accessed via PaymentIntent.gateway_payment_id
-- Tickets can access it via: Transaction -> PaymentIntent relationship

DROP INDEX IF EXISTS idx_tickets_payment_intent_id;

ALTER TABLE tickets 
DROP COLUMN IF EXISTS payment_intent_id;
