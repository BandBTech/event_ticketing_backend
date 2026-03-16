-- Migration: make_payment_method_optional
-- Created: 2026-03-16 14:44:48

-- Make payment_method column nullable in payment_bills table
ALTER TABLE payment_bills ALTER COLUMN payment_method DROP NOT NULL;

