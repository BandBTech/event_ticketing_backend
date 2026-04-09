-- Restore redundant fields (for rollback purposes)
-- +migrate Down
-- Note: This rollback is not perfect as we cannot restore computed data

-- Restore price column to events table (will be NULL)
ALTER TABLE events ADD COLUMN price DECIMAL(10,2);

-- Restore computed fields to events table (will be NULL)
ALTER TABLE events ADD COLUMN total_sold_tickets INTEGER DEFAULT 0;
ALTER TABLE events ADD COLUMN total_revenue DECIMAL(10,2) DEFAULT 0;

-- Restore denormalized organizer_id to transactions table (will be NULL)
ALTER TABLE transactions ADD COLUMN organizer_id UUID;

-- Recreate event_sales table (empty)
CREATE TABLE IF NOT EXISTS event_sales (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL UNIQUE,
    organizer_id UUID NOT NULL,
    total_tickets_sold INTEGER NOT NULL DEFAULT 0,
    gross_revenue DECIMAL(10,2) NOT NULL DEFAULT 0,
    commission_rate DECIMAL(5,2) NOT NULL,
    commission_amount DECIMAL(10,2) NOT NULL DEFAULT 0,
    organizer_share DECIMAL(10,2) NOT NULL DEFAULT 0,
    paid_amount DECIMAL(10,2) NOT NULL DEFAULT 0,
    due_amount DECIMAL(10,2) NOT NULL DEFAULT 0,
    last_payment_date TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE
);

-- Add indexes for event_sales
CREATE INDEX IF NOT EXISTS idx_event_sales_event_id ON event_sales(event_id);
CREATE INDEX IF NOT EXISTS idx_event_sales_organizer_id ON event_sales(organizer_id);

-- Recreate individual_tickets table (empty)
CREATE TABLE IF NOT EXISTS individual_tickets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_number VARCHAR(50) UNIQUE NOT NULL,
    user_id UUID,
    guest_user_id UUID,
    event_id UUID NOT NULL,
    tier_id UUID NOT NULL,
    transaction_id UUID,
    payment_status VARCHAR(20) DEFAULT 'pending',
    paid_at TIMESTAMP WITH TIME ZONE,
    total_amount DECIMAL(10,2) NOT NULL,
    payment_gateway VARCHAR(20) NOT NULL,
    status VARCHAR(20) DEFAULT 'active',
    is_guest_purchase BOOLEAN DEFAULT FALSE,
    check_in_time TIMESTAMP WITH TIME ZONE,
    check_out_time TIMESTAMP WITH TIME ZONE,
    checked_in_by UUID,
    checked_out_by UUID,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE
);

-- Add indexes for individual_tickets
CREATE INDEX IF NOT EXISTS idx_individual_tickets_user_id ON individual_tickets(user_id);
CREATE INDEX IF NOT EXISTS idx_individual_tickets_guest_user_id ON individual_tickets(guest_user_id);
CREATE INDEX IF NOT EXISTS idx_individual_tickets_event_id ON individual_tickets(event_id);
CREATE INDEX IF NOT EXISTS idx_individual_tickets_tier_id ON individual_tickets(tier_id);
CREATE INDEX IF NOT EXISTS idx_individual_tickets_transaction_id ON individual_tickets(transaction_id);