-- +migrate Up
-- Convert payout_requests to many-to-many relationship with payment_bills

-- Drop the existing payment_bill_id column
ALTER TABLE payout_requests DROP COLUMN IF EXISTS payment_bill_id;

-- Create junction table for many-to-many relationship
CREATE TABLE payout_request_bills (
    payout_request_id UUID NOT NULL REFERENCES payout_requests(id) ON DELETE CASCADE,
    payment_bill_id UUID NOT NULL REFERENCES payment_bills(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (payout_request_id, payment_bill_id)
);

-- Create indexes for better performance
CREATE INDEX idx_payout_request_bills_payout_request_id ON payout_request_bills(payout_request_id);
CREATE INDEX idx_payout_request_bills_payment_bill_id ON payout_request_bills(payment_bill_id);

-- +migrate Down
-- Revert many-to-many relationship back to single bill relationship

-- Drop junction table
DROP TABLE IF EXISTS payout_request_bills;

-- Add back the payment_bill_id column
ALTER TABLE payout_requests ADD COLUMN payment_bill_id UUID REFERENCES payment_bills(id);