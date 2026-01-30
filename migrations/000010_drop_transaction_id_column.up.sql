-- +migrate Up
-- Drop the transaction_id column as we're using id (primary key) instead
ALTER TABLE transactions DROP COLUMN IF EXISTS transaction_id;
