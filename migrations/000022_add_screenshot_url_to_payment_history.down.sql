-- Rollback: add_screenshot_url_to_payment_history
-- Created: 2026-03-16 14:54:47

-- Drop screenshot_url column from payment_history
ALTER TABLE payment_history
DROP COLUMN IF EXISTS screenshot_url;

