-- +migrate Up
-- Add organizer_id column to transactions table (nullable first)
ALTER TABLE transactions ADD COLUMN organizer_id UUID;

-- Create index for performance
CREATE INDEX IF NOT EXISTS idx_transactions_organizer_id ON transactions(organizer_id);

-- Populate organizer_id from events table for existing transactions
UPDATE transactions
SET organizer_id = events.organizer_id
FROM events
WHERE transactions.event_id = events.id AND transactions.organizer_id IS NULL;

-- Now make the column NOT NULL
ALTER TABLE transactions ALTER COLUMN organizer_id SET NOT NULL;

-- +migrate Down
-- Remove the organizer_id column
DROP INDEX IF EXISTS idx_transactions_organizer_id;
ALTER TABLE transactions DROP COLUMN organizer_id;