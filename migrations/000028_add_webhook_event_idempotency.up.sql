-- Add UNIQUE constraint on webhook_events for idempotency
-- Prevents the same webhook event from being processed multiple times
-- This is the primary defense against Stripe webhook retries

ALTER TABLE webhook_events
ADD CONSTRAINT unique_webhook_event_id
UNIQUE (payment_gateway, gateway_event_id);

-- Add index for quick lookup of unprocessed events (for replay)
CREATE INDEX idx_webhook_events_status_received
ON webhook_events(status, received_at DESC)
WHERE status != 'processed';

-- Add index for finding failed webhooks that need retry
CREATE INDEX idx_webhook_events_failed
ON webhook_events(status, last_error)
WHERE status = 'failed';
