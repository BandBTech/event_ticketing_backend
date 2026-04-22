-- +goose Down
-- Add back the dropped columns (for rollback)
ALTER TABLE organizer_onboardings ADD COLUMN IF NOT EXISTS is_profile_complete BOOLEAN DEFAULT false;
ALTER TABLE organizer_onboardings ADD COLUMN IF NOT EXISTS is_business_info_complete BOOLEAN DEFAULT false;
ALTER TABLE organizer_onboardings ADD COLUMN IF NOT EXISTS is_categories_selected BOOLEAN DEFAULT false;
ALTER TABLE organizer_onboardings ADD COLUMN IF NOT EXISTS is_onboarding_complete BOOLEAN DEFAULT false;