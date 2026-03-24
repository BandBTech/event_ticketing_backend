-- Create transaction_items table for multi-tier order support
-- One transaction can have multiple items (one per tier)
-- This enables: multi-tier checkout, future cart system, detailed order breakdowns

CREATE TABLE IF NOT EXISTS transaction_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE RESTRICT,
    tier_id UUID NOT NULL REFERENCES event_tiers(id) ON DELETE RESTRICT,
    
    -- Item details
    quantity INT NOT NULL CHECK (quantity > 0),
    unit_price DECIMAL(12, 2) NOT NULL CHECK (unit_price >= 0),
    subtotal DECIMAL(12, 2) NOT NULL CHECK (subtotal >= 0),
    
    -- Financial breakdown
    commission_rate DECIMAL(5, 2) NOT NULL DEFAULT 0 CHECK (commission_rate >= 0 AND commission_rate <= 100),
    commission_amount DECIMAL(12, 2) NOT NULL DEFAULT 0 CHECK (commission_amount >= 0),
    organizer_share DECIMAL(12, 2) NOT NULL DEFAULT 0 CHECK (organizer_share >= 0),
    
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    status VARCHAR(50) NOT NULL DEFAULT 'completed' CHECK (status IN ('completed', 'pending', 'refunded')),
    
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP,
    
    CONSTRAINT fk_transaction_items_transaction FOREIGN KEY (transaction_id) REFERENCES transactions(id) ON DELETE CASCADE,
    CONSTRAINT fk_transaction_items_event FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE RESTRICT,
    CONSTRAINT fk_transaction_items_tier FOREIGN KEY (tier_id) REFERENCES event_tiers(id) ON DELETE RESTRICT
);

-- Performance indexes for common queries
-- Index 1: Find all items in a transaction (display order details)
CREATE INDEX idx_transaction_items_transaction_id ON transaction_items(transaction_id);

-- Index 2: Find all items for an event (event analytics, reconciliation)
CREATE INDEX idx_transaction_items_event_id ON transaction_items(event_id);

-- Index 3: Find all items for a specific tier (tier sales analytics)
CREATE INDEX idx_transaction_items_tier_id ON transaction_items(tier_id);

-- Index 4: Composite index for transaction + status (common query pattern)
CREATE INDEX idx_transaction_items_txn_status ON transaction_items(transaction_id, status);

-- Index 5: Find items by status for reconciliation queries
CREATE INDEX idx_transaction_items_status ON transaction_items(status);

-- Index 6: Find all items for an event from a given date (daily/weekly reports)
CREATE INDEX idx_transaction_items_event_created ON transaction_items(event_id, created_at DESC);

-- Index 7: Soft delete filter (common for queries excluding deleted items)
CREATE INDEX idx_transaction_items_deleted_at ON transaction_items(deleted_at);
