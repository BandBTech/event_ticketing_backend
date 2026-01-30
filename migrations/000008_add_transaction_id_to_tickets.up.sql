-- +migrate Up
-- Add transaction_id column to tickets table
ALTER TABLE tickets ADD COLUMN transaction_id UUID REFERENCES transactions(id) ON DELETE SET NULL;

-- Create index for better query performance
CREATE INDEX IF NOT EXISTS idx_tickets_transaction_id ON tickets(transaction_id);

-- Drop ticket_ids column from transactions table since we're using the reverse relationship
ALTER TABLE transactions DROP COLUMN IF EXISTS ticket_ids;
