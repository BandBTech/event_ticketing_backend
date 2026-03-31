-- +migrate Down
ALTER TABLE events DROP COLUMN IF EXISTS refund_policy;
ALTER TABLE events DROP COLUMN IF EXISTS is_refundable;

-- +migrate Up
ALTER TABLE events ADD COLUMN is_refundable BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE events ADD COLUMN refund_policy TEXT;