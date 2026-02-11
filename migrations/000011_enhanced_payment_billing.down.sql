-- +migrate Down
-- Rollback Enhanced Payment Billing System Migration

-- Drop new tables
DROP TABLE IF EXISTS payment_history;
DROP TABLE IF EXISTS payment_bill_events;

-- Remove new columns from payment_bills table
ALTER TABLE payment_bills
DROP COLUMN IF EXISTS total_owed_amount,
DROP COLUMN IF EXISTS billed_amount,
DROP COLUMN IF EXISTS paid_amount,
DROP COLUMN IF EXISTS remaining_amount,
DROP COLUMN IF EXISTS bill_type,
DROP COLUMN IF EXISTS priority,
DROP COLUMN IF EXISTS due_date;