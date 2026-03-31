-- Revert affected_ticket_ids column from jsonb back to uuid[]
-- First, add a temporary column to store the converted data
ALTER TABLE refunds ADD COLUMN affected_ticket_ids_temp uuid[];

-- Convert jsonb array of strings back to uuid[]
UPDATE refunds SET affected_ticket_ids_temp = (
    SELECT array_agg(uuid::uuid)
    FROM jsonb_array_elements_text(affected_ticket_ids) AS uuid
) WHERE affected_ticket_ids IS NOT NULL AND affected_ticket_ids != '[]'::jsonb;

-- Set NULL for empty arrays
UPDATE refunds SET affected_ticket_ids_temp = NULL WHERE affected_ticket_ids = '[]'::jsonb;

-- Drop the old column and rename the new one
ALTER TABLE refunds DROP COLUMN affected_ticket_ids;
ALTER TABLE refunds RENAME COLUMN affected_ticket_ids_temp TO affected_ticket_ids;