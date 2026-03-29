-- Drop index on updated_at
DROP INDEX IF EXISTS idx_webhook_events_updated_at;

-- Remove updated_at column from webhook_events table
ALTER TABLE webhook_events DROP COLUMN IF EXISTS updated_at;
