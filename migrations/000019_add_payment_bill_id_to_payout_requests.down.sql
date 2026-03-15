-- +migrate Down
DROP INDEX IF EXISTS idx_payout_requests_payment_bill_id;
ALTER TABLE payout_requests DROP COLUMN payment_bill_id;

-- +migrate Up
ALTER TABLE payout_requests ADD COLUMN payment_bill_id UUID;
CREATE INDEX idx_payout_requests_payment_bill_id ON payout_requests(payment_bill_id);