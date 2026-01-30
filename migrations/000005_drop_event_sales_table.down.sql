-- +migrate Up
-- Recreate EventSales table (for rollback purposes)
CREATE TABLE IF NOT EXISTS event_sales (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    organizer_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    total_tickets_sold INTEGER NOT NULL DEFAULT 0,
    gross_revenue DECIMAL(10,2) NOT NULL DEFAULT 0,
    commission_rate DECIMAL(5,2) NOT NULL,
    commission_amount DECIMAL(10,2) NOT NULL DEFAULT 0,
    organizer_share DECIMAL(10,2) NOT NULL DEFAULT 0,
    paid_amount DECIMAL(10,2) NOT NULL DEFAULT 0,
    due_amount DECIMAL(10,2) NOT NULL DEFAULT 0,
    last_payment_date TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_event_sales_event_id ON event_sales(event_id);
CREATE INDEX IF NOT EXISTS idx_event_sales_organizer_id ON event_sales(organizer_id);

-- +migrate Down
-- Drop the redundant EventSales table
DROP TABLE IF EXISTS event_sales;