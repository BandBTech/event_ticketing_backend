-- +migrate Up
-- Enhanced Payment Billing System Migration
-- Add new columns to payment_bills table and create related tables

-- Add new columns to payment_bills table
ALTER TABLE payment_bills
ADD COLUMN IF NOT EXISTS total_owed_amount DOUBLE PRECISION NOT NULL DEFAULT 0,
ADD COLUMN IF NOT EXISTS billed_amount DOUBLE PRECISION NOT NULL DEFAULT 0,
ADD COLUMN IF NOT EXISTS paid_amount DOUBLE PRECISION NOT NULL DEFAULT 0,
ADD COLUMN IF NOT EXISTS remaining_amount DOUBLE PRECISION NOT NULL DEFAULT 0,
ADD COLUMN IF NOT EXISTS bill_type VARCHAR(50) NOT NULL DEFAULT 'manual',
ADD COLUMN IF NOT EXISTS priority VARCHAR(20) NOT NULL DEFAULT 'normal',
ADD COLUMN IF NOT EXISTS due_date TIMESTAMP NULL;

-- Update existing records to have consistent data
UPDATE payment_bills SET
    total_owed_amount = bill_amount,
    billed_amount = bill_amount,
    paid_amount = CASE WHEN status = 'paid' THEN bill_amount ELSE 0 END,
    remaining_amount = CASE WHEN status = 'paid' THEN 0 ELSE bill_amount END
WHERE total_owed_amount = 0;

-- Create payment_bill_events table for many-to-many relationship with events
CREATE TABLE IF NOT EXISTS payment_bill_events (
    id SERIAL PRIMARY KEY,
    payment_bill_id UUID NOT NULL REFERENCES payment_bills(id) ON DELETE CASCADE,
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    owed_amount DOUBLE PRECISION NOT NULL DEFAULT 0,
    billed_amount DOUBLE PRECISION NOT NULL DEFAULT 0,
    paid_amount DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(payment_bill_id, event_id)
);

-- Create payment_history table for tracking individual payments
CREATE TABLE IF NOT EXISTS payment_history (
    id SERIAL PRIMARY KEY,
    payment_bill_id UUID NOT NULL REFERENCES payment_bills(id) ON DELETE CASCADE,
    amount DOUBLE PRECISION NOT NULL,
    payment_method VARCHAR(50) NOT NULL,
    payment_ref VARCHAR(255),
    payment_date TIMESTAMP NOT NULL,
    processed_by_id UUID NOT NULL REFERENCES users(id),
    notes TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for better performance
CREATE INDEX IF NOT EXISTS idx_payment_bill_events_payment_bill_id ON payment_bill_events(payment_bill_id);
CREATE INDEX IF NOT EXISTS idx_payment_bill_events_event_id ON payment_bill_events(event_id);
CREATE INDEX IF NOT EXISTS idx_payment_history_payment_bill_id ON payment_history(payment_bill_id);
CREATE INDEX IF NOT EXISTS idx_payment_history_processed_by_id ON payment_history(processed_by_id);

-- Migrate existing single-event bills to the new structure
INSERT INTO payment_bill_events (payment_bill_id, event_id, owed_amount, billed_amount, paid_amount)
SELECT
    id,
    event_id,
    bill_amount,
    bill_amount,
    CASE WHEN status = 'paid' THEN bill_amount ELSE 0 END
FROM payment_bills
WHERE event_id IS NOT NULL
ON CONFLICT (payment_bill_id, event_id) DO NOTHING;

-- Remove the old event_id column from payment_bills (optional - keep for backward compatibility)
-- ALTER TABLE payment_bills DROP COLUMN IF EXISTS event_id;