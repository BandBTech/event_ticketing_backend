-- +migrate Down
-- Revert many-to-many relationship back to single bill relationship

-- Drop junction table
DROP TABLE IF EXISTS payout_request_bills;

-- Add back the payment_bill_id column
ALTER TABLE payout_requests ADD COLUMN payment_bill_id UUID REFERENCES payment_bills(id);