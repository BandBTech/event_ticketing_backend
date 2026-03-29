-- Add updated_at column to webhook_events table
ALTER TABLE webhook_events ADD COLUMN updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;

-- Create index on updated_at for query optimization
CREATE INDEX idx_webhook_events_updated_at ON webhook_events(updated_at);
