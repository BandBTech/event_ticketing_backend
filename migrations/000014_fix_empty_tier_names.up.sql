-- +migrate Up
-- Fix empty tier_name in event_tiers by populating from tier templates
UPDATE event_tiers
SET tier_name = organizer_tier_templates.template_name
FROM organizer_tier_templates
WHERE event_tiers.tier_template_id = organizer_tier_templates.id
AND (event_tiers.tier_name IS NULL OR event_tiers.tier_name = '');

-- +migrate Down
-- Revert tier names back to empty (this is a data migration, so down migration is not meaningful)
-- In a real scenario, you might want to store the original values, but for this fix, we'll leave them as-is