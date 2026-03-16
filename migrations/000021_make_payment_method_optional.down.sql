-- Rollback: make_payment_method_optional
-- Created: 2026-03-16 14:44:48

-- Make payment_method column NOT NULL again in payment_bills table
-- Note: This will fail if there are NULL values, so ensure data is cleaned up first
ALTER TABLE payment_bills ALTER COLUMN payment_method SET NOT NULL;

