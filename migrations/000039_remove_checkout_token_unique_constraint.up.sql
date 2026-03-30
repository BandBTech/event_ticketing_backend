-- Migration: Remove unique constraint on checkout_token to allow multiple reservations per checkout
-- Created: 2026-03-30

-- Drop the unique constraint on checkout_token to allow multiple ticket reservations with the same token
ALTER TABLE ticket_reservations DROP CONSTRAINT IF EXISTS ticket_reservations_checkout_token_key;

