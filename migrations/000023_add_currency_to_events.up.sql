-- Migration: add_currency_to_events
-- Created: 2026-03-16 17:59:53

-- Add currency column to events table for multi-currency support
ALTER TABLE events
ADD COLUMN IF NOT EXISTS currency VARCHAR(3) NOT NULL DEFAULT 'USD';

