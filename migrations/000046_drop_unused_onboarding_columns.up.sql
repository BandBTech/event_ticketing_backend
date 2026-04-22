-- +goose Up
-- Drop unused onboarding status columns from organizer_onboardings table
ALTER TABLE organizer_onboardings DROP COLUMN IF EXISTS is_profile_complete;
ALTER TABLE organizer_onboardings DROP COLUMN IF EXISTS is_business_info_complete;
ALTER TABLE organizer_onboardings DROP COLUMN IF EXISTS is_categories_selected;
ALTER TABLE organizer_onboardings DROP COLUMN IF EXISTS is_onboarding_complete;