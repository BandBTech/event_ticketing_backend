-- Migration: Add currency column to event_tiers table
-- Created: 2025-01-14

-- Add currency column with default USD
ALTER TABLE event_tiers ADD COLUMN currency VARCHAR(3) NOT NULL DEFAULT 'USD';

