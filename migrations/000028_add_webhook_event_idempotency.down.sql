-- Drop UNIQUE constraint on webhook_events
ALTER TABLE webhook_events
DROP CONSTRAINT unique_webhook_event_id;

-- Drop indices
DROP INDEX IF EXISTS idx_webhook_events_status_received;
DROP INDEX IF EXISTS idx_webhook_events_failed;
