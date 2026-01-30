-- Remove indexes
DROP INDEX IF EXISTS idx_transactions_event_tier_status;
DROP INDEX IF EXISTS idx_transactions_tier_id;

-- Remove foreign key constraint
ALTER TABLE transactions DROP CONSTRAINT IF EXISTS fk_transactions_tier;

-- Remove tier_id column
ALTER TABLE transactions DROP COLUMN IF EXISTS tier_id;
