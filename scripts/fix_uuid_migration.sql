-- Fix Events table to use UUID instead of integer ID
-- This script should be run when events table has integer ID but models expect UUID

-- Enable UUID extension if not already enabled
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Check if events table exists and has integer ID
DO $$ 
BEGIN
    -- Check if events table has integer ID column
    IF EXISTS (
        SELECT 1 
        FROM information_schema.columns 
        WHERE table_name = 'events' 
        AND column_name = 'id' 
        AND data_type = 'integer'
    ) THEN
        RAISE NOTICE 'Converting events table from integer ID to UUID...';
        
        -- Drop foreign key constraints temporarily
        ALTER TABLE IF EXISTS event_tiers DROP CONSTRAINT IF EXISTS fk_events_tiers;

ALTER TABLE IF EXISTS discounts
DROP CONSTRAINT IF EXISTS fk_events_discounts;

ALTER TABLE IF EXISTS promocodes
DROP CONSTRAINT IF EXISTS fk_events_promocodes;

ALTER TABLE IF EXISTS individual_tickets
DROP CONSTRAINT IF EXISTS fk_events_individual_tickets;

-- Create new events table with UUID
CREATE TABLE events_new (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4 (),
    title VARCHAR(200) NOT NULL,
    description TEXT,
    banner_image VARCHAR(500),
    category TEXT [],
    venue_name VARCHAR(200),
    address TEXT,
    location VARCHAR(200),
    start_date TIMESTAMP NOT NULL,
    end_date TIMESTAMP NOT NULL,
    timezone VARCHAR(50) DEFAULT 'UTC',
    capacity INTEGER NOT NULL,
    available INTEGER,
    price DECIMAL NOT NULL DEFAULT 0,
    commission_rate DECIMAL NOT NULL DEFAULT 10,
    status VARCHAR(50) NOT NULL DEFAULT 'draft',
    sales_status VARCHAR(50) NOT NULL DEFAULT 'active',
    is_featured BOOLEAN NOT NULL DEFAULT false,
    is_cancelled BOOLEAN NOT NULL DEFAULT false,
    cancelled_at TIMESTAMP,
    cancel_reason TEXT,
    organizer_id UUID,
    admin_remark TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Copy data from old table to new table (if there's existing data)
INSERT INTO
    events_new (
        title,
        description,
        banner_image,
        category,
        venue_name,
        address,
        location,
        start_date,
        end_date,
        timezone,
        capacity,
        available,
        price,
        commission_rate,
        status,
        sales_status,
        is_featured,
        is_cancelled,
        cancelled_at,
        cancel_reason,
        organizer_id,
        admin_remark,
        created_at,
        updated_at
    )
SELECT
    title,
    description,
    banner_image,
    category,
    venue_name,
    address,
    location,
    start_date,
    end_date,
    timezone,
    capacity,
    COALESCE(available, capacity) as available,
    price,
    COALESCE(commission_rate, 10) as commission_rate,
    COALESCE(status, 'draft') as status,
    COALESCE(sales_status, 'active') as sales_status,
    COALESCE(is_featured, false) as is_featured,
    COALESCE(is_cancelled, false) as is_cancelled,
    cancelled_at,
    cancel_reason,
    organizer_id,
    admin_remark,
    COALESCE(created_at, NOW()) as created_at,
    COALESCE(updated_at, NOW()) as updated_at
FROM events
WHERE
    EXISTS (
        SELECT 1
        FROM events
        LIMIT 1
    );

-- Drop old table and rename new one
DROP TABLE events CASCADE;

ALTER TABLE events_new RENAME TO events;

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_events_organizer_id ON events (organizer_id);

CREATE INDEX IF NOT EXISTS idx_events_status ON events (status);

CREATE INDEX IF NOT EXISTS idx_events_start_date ON events (start_date);

CREATE INDEX IF NOT EXISTS idx_events_created_at ON events (created_at);

RAISE NOTICE 'Events table converted to UUID successfully!';

ELSE RAISE NOTICE 'Events table already uses UUID or does not exist.';

END IF;

END $$;