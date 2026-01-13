-- Rollback Migration: Remove currency column from event_tiers table
-- Created: 2025-01-14

-- Drop currency column
ALTER TABLE event_tiers DROP COLUMN IF EXISTS currency;

