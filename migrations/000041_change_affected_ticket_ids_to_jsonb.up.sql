-- Change affected_ticket_ids column from uuid[] to jsonb to store string array
-- First, add a temporary column to store the converted data
ALTER TABLE refunds ADD COLUMN affected_ticket_ids_temp jsonb;

-- Convert uuid[] to jsonb array of strings
UPDATE refunds SET affected_ticket_ids_temp = (
    SELECT jsonb_agg(uuid::text)
    FROM unnest(affected_ticket_ids) AS uuid
) WHERE affected_ticket_ids IS NOT NULL;

-- Set empty array for NULL values
UPDATE refunds SET affected_ticket_ids_temp = '[]'::jsonb WHERE affected_ticket_ids IS NULL;

-- Drop the old column and rename the new one
ALTER TABLE refunds DROP COLUMN affected_ticket_ids;
ALTER TABLE refunds RENAME COLUMN affected_ticket_ids_temp TO affected_ticket_ids;