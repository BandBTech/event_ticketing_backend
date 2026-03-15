-- Add stripe_session_id column to checkout_sessions table
ALTER TABLE checkout_sessions ADD COLUMN stripe_session_id VARCHAR(255);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_checkout_sessions_stripe_session_id ON checkout_sessions(stripe_session_id);
COMMENT ON COLUMN checkout_sessions.stripe_session_id IS 'Stripe checkout session ID for webhook lookup';