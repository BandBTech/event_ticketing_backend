-- +migrate Down
-- Remove the organizer_id column
DROP INDEX IF EXISTS idx_transactions_organizer_id;
ALTER TABLE transactions DROP COLUMN organizer_id;