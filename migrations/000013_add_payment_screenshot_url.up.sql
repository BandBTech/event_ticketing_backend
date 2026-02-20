-- +migrate Up
-- Add payment_screenshot_url column to payment_bills for proof-of-payment uploads
ALTER TABLE payment_bills
    ADD COLUMN IF NOT EXISTS payment_screenshot_url VARCHAR(500) NOT NULL DEFAULT '';
