-- Add UNIQUE constraint on gateway payment ID for idempotency
-- This ensures that the same payment from Stripe can only be processed once
-- even if the webhook is retried multiple times

ALTER TABLE payment_intents
ADD CONSTRAINT unique_gateway_payment_id
UNIQUE (payment_gateway, gateway_payment_id) WHERE gateway_payment_id IS NOT NULL;

-- Add index on idempotency_key for faster deduplication checks
CREATE INDEX idx_payment_intents_idempotency_key_status 
ON payment_intents(idempotency_key, status);

-- Add index for quick lookup by checkout token (UI fallback endpoint)
CREATE INDEX idx_payment_intents_checkout_token_status
ON payment_intents(checkout_token, status) WHERE checkout_token != '';

-- Add index for status queries to speed up "find by status" lookups
CREATE INDEX idx_payment_intents_status_created
ON payment_intents(status, created_at DESC);

-- Add index for finding pending payments that might have timed out
CREATE INDEX idx_payment_intents_pending_timeout
ON payment_intents(created_at) WHERE status = 'pending';
