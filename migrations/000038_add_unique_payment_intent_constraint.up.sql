-- Add unique constraint on payment_intent_id in transactions table
-- This ensures one transaction per payment intent (prevents duplicates)

-- First, check if the constraint already exists, and if not, create an index
-- This index ensures payment_intent_id uniqueness for non-null values
CREATE UNIQUE INDEX IF NOT EXISTS idx_transactions_unique_payment_intent_id 
ON transactions(payment_intent_id) 
WHERE payment_intent_id IS NOT NULL;

-- Log migration message
-- This prevents duplicate transactions when webhook and callback both process the same payment
