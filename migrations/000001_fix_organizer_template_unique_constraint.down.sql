-- Drop the composite unique index
DROP INDEX IF EXISTS idx_organizer_template_name;

-- Recreate the original unique index on template_name alone
CREATE UNIQUE INDEX idx_organizer_template_name ON organizer_tier_templates(template_name) WHERE deleted_at IS NULL;
