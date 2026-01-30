-- Revert performance indexes

DROP INDEX IF EXISTS idx_users_account_status;
DROP INDEX IF EXISTS idx_users_organizer_status;
DROP INDEX IF EXISTS idx_users_email_lower;

DROP INDEX IF EXISTS idx_events_status_start_date;
DROP INDEX IF EXISTS idx_events_is_featured;
DROP INDEX IF EXISTS idx_events_organizer_status;
DROP INDEX IF EXISTS idx_events_is_cancelled;
DROP INDEX IF EXISTS idx_events_category;

DROP INDEX IF EXISTS idx_transactions_status;
DROP INDEX IF EXISTS idx_transactions_event_id_status;
DROP INDEX IF EXISTS idx_transactions_user_id_status;
DROP INDEX IF EXISTS idx_transactions_created_at;

DROP INDEX IF EXISTS idx_tickets_status;
DROP INDEX IF EXISTS idx_tickets_event_id_status;
DROP INDEX IF EXISTS idx_tickets_user_id;
DROP INDEX IF EXISTS idx_tickets_guest_user_id;
DROP INDEX IF EXISTS idx_tickets_qr_code;

DROP INDEX IF EXISTS idx_payment_bills_status;
DROP INDEX IF EXISTS idx_payment_bills_organizer_id_status;

DROP INDEX IF EXISTS idx_event_tiers_event_id;
DROP INDEX IF EXISTS idx_event_tiers_sales_dates;

DROP INDEX IF EXISTS idx_otps_email_type_verified;

DROP INDEX IF EXISTS idx_organizer_onboarding_organizer_id;

DROP INDEX IF EXISTS idx_categories_is_active;
