-- +migrate Up
-- Fix payment_bills table: set DEFAULT 0 on legacy bill_amount column so new inserts
-- (which no longer populate this column) do not hit the NOT NULL constraint.
-- The canonical columns are now: billed_amount, paid_amount, remaining_amount.

ALTER TABLE payment_bills
    ALTER COLUMN bill_amount SET DEFAULT 0;

-- Back-fill bill_amount for any existing rows that may have 0 and a non-zero billed_amount
UPDATE payment_bills
SET bill_amount = billed_amount
WHERE bill_amount = 0 AND billed_amount > 0;
