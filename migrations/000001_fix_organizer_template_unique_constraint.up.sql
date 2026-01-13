-- Drop the existing unique index on template_name alone
DROP INDEX IF EXISTS idx_organizer_template_name;

-- Create a composite unique index on organizer_id and template_name
-- This allows the same template name to be used by different organizers
CREATE UNIQUE INDEX idx_organizer_template_name ON organizer_tier_templates(organizer_id, template_name) WHERE deleted_at IS NULL;

-- Add a comment to document the constraint
COMMENT ON INDEX idx_organizer_template_name IS 'Ensures template names are unique per organizer, allowing different organizers to use the same template name';
