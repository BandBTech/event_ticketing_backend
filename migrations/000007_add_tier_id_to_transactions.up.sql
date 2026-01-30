-- Add tier_id column to transactions table for analytics
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS tier_id UUID;

-- Add foreign key constraint
ALTER TABLE transactions ADD CONSTRAINT fk_transactions_tier 
    FOREIGN KEY (tier_id) REFERENCES event_tiers(id) ON DELETE SET NULL;

-- Add index for performance
CREATE INDEX IF NOT EXISTS idx_transactions_tier_id ON transactions(tier_id);

-- Add composite index for analytics queries
CREATE INDEX IF NOT EXISTS idx_transactions_event_tier_status ON transactions(event_id, tier_id, status);
