-- +migrate Up
-- Ensure transaction_id is VARCHAR(100) not UUID
-- Drop and recreate if it's the wrong type
DO $$ 
BEGIN
    -- Check if transaction_id column exists and is not varchar
    IF EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'transactions' 
        AND column_name = 'transaction_id' 
        AND data_type != 'character varying'
    ) THEN
        -- Drop the column and recreate it
        ALTER TABLE transactions DROP COLUMN transaction_id;
        ALTER TABLE transactions ADD COLUMN transaction_id VARCHAR(100) NOT NULL UNIQUE;
    END IF;
END $$;
