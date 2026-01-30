-- +migrate Down
-- Add back ticket_ids column to transactions table
ALTER TABLE transactions ADD COLUMN ticket_ids UUID[] NOT NULL DEFAULT '{}';

-- Drop index and column from tickets table
DROP INDEX IF EXISTS idx_tickets_transaction_id;
ALTER TABLE tickets DROP COLUMN IF EXISTS transaction_id;
