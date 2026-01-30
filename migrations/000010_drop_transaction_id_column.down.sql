-- +migrate Down
-- Add back transaction_id column if needed for rollback
ALTER TABLE transactions ADD COLUMN transaction_id UUID DEFAULT gen_random_uuid() UNIQUE;
