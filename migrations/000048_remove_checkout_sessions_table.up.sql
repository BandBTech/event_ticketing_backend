-- Remove CheckoutSession table completely per clean architecture spec
-- All checkout functionality now handled by PaymentIntent + Transaction

-- Drop the checkout_sessions table
DROP TABLE IF EXISTS checkout_sessions CASCADE;

-- Drop related indexes (these will be automatically dropped with CASCADE, but being explicit)
DROP INDEX IF EXISTS idx_checkout_sessions_expires;
DROP INDEX IF EXISTS idx_checkout_sessions_stripe_session_id;
DROP INDEX IF EXISTS idx_checkout_sessions_payment_intent_id;