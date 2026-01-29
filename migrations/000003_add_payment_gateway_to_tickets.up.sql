-- +migrate Up
-- Add payment_gateway column as nullable first
ALTER TABLE tickets ADD COLUMN payment_gateway VARCHAR(50);

-- Update existing records with default value 'cash'
UPDATE tickets SET payment_gateway = 'cash' WHERE payment_gateway IS NULL;

-- Make the column NOT NULL
ALTER TABLE tickets ALTER COLUMN payment_gateway SET NOT NULL;

-- +migrate Down
ALTER TABLE tickets DROP COLUMN payment_gateway;