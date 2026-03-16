-- Rollback: add_currency_to_events
-- Created: 2026-03-16 17:59:53

-- Drop currency column from events table
ALTER TABLE events
DROP COLUMN IF EXISTS currency;

