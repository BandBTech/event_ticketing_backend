-- Remove redundant fields from events table
-- +migrate Up
ALTER TABLE events DROP COLUMN IF EXISTS price;
ALTER TABLE events DROP COLUMN IF EXISTS total_sold_tickets;
ALTER TABLE events DROP COLUMN IF EXISTS total_revenue;

-- Remove denormalized organizer_id from transactions table
ALTER TABLE transactions DROP COLUMN IF EXISTS organizer_id;

-- Remove redundant event_sales table (data can be calculated from transactions)
DROP TABLE IF EXISTS event_sales CASCADE;

-- Remove individual_tickets table if it exists (appears redundant with tickets)
DROP TABLE IF EXISTS individual_tickets CASCADE;