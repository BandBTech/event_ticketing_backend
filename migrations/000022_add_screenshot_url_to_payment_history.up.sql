-- Migration: add_screenshot_url_to_payment_history
-- Created: 2026-03-16 14:54:47

-- Add screenshot_url column to payment_history for individual payment proof uploads
ALTER TABLE payment_history
ADD COLUMN IF NOT EXISTS screenshot_url VARCHAR(500);

