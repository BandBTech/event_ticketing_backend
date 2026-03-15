-- Remove stripe_session_id column from checkout_sessions table
DROP INDEX CONCURRENTLY IF EXISTS idx_checkout_sessions_stripe_session_id;
ALTER TABLE checkout_sessions DROP COLUMN IF EXISTS stripe_session_id;