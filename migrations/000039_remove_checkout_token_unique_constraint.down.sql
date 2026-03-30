-- Rollback Migration: Add back unique constraint on checkout_token
-- Created: 2026-03-30

-- Add back the unique constraint on checkout_token
ALTER TABLE ticket_reservations ADD CONSTRAINT ticket_reservations_checkout_token_key UNIQUE (checkout_token);

