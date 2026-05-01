-- Down migration for removing checkout_sessions table
-- NOTE: This migration is NOT REVERSIBLE per clean architecture spec
-- CheckoutSession functionality has been completely removed and replaced with PaymentIntent + Transaction

-- This down migration does nothing as CheckoutSession removal is permanent