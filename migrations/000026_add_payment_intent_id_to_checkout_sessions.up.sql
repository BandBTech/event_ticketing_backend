-- Add PaymentIntentID to checkout_sessions table for proper linking
ALTER TABLE checkout_sessions 
ADD COLUMN IF NOT EXISTS payment_intent_id uuid REFERENCES payment_intents(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_checkout_sessions_payment_intent_id 
ON checkout_sessions(payment_intent_id);
