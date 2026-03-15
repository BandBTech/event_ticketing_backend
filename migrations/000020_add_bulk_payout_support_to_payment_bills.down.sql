-- +migrate Down
-- Allow event_id to be NULL (for potential future bulk payout feature)
ALTER TABLE payment_bills ALTER COLUMN event_id DROP NOT NULL;

-- +migrate Up
-- Ensure event_id is NOT NULL for single event per bill requirement
ALTER TABLE payment_bills ALTER COLUMN event_id SET NOT NULL;