# Transaction Creation Analysis

## Summary

You have **110 transactions** with proper data because transactions ARE being created in some payment flows, but the **organizer dashboard shows 0** because those transactions are NOT linked to the correct `event_id` OR the payment_worker webhook handler is NOT creating transactions for Stripe webhook payments.

## Where Transactions Are Created

### 1. **TicketService.recordTransactionInTx()** (lines 2872-2960)

The main function that creates Transaction records. Called from:

- **Line 198**: Manual ticket purchase (non-webhook)
- **Line 1398**: Another manual purchase flow
- **Line 2524**: `ConfirmReservation()` - handles webhook payment confirmation
- **Line 3514**: Another flow

### 2. **Payment Flows**

#### ✅ WORKING (Creating Transactions):

- **Manual/Cash Payments**: Creates transactions via ConfirmReservation
- **Payment Service**: `HandlePaymentSuccess()` creates transactions directly

#### ❌ NOT WORKING (NOT Creating Transactions):

- **Payment Worker**: `processPaymentIntentSucceeded()` in webhook_handler → payment_worker path
  - Does NOT call `RecordTransaction()` or `recordTransactionInTx()`
  - No transaction records created for Stripe webhooks via this path

## The Problem

The 110 existing transactions likely come from:

- Manual cash payments
- Payments processed through other handlers
- But NOT from the webhook handler → payment_worker path

When you buy tickets via Stripe webhook:

1. Webhook hits webhook_handler
2. Enqueues job to payment_worker
3. Payment worker confirms reservation
4. **BUT**: Payment worker does NOT create transaction records ❌

Result: Transactions exist for old payments but new webhook payments don't create transactions, so dashboard has nothing to aggregate.

## Solution

The code I modified (`payment_worker.go`) now includes transaction creation. However, you should verify:

1. **Are the transactions being created** when you process a new Stripe payment?
   - Check logs for "[TRANSACTION_CREATED]" messages
2. **Do the transactions have correct data**?
   - EventID pointing to correct event
   - OrganizerShare calculated correctly
   - CommissionRate applied correctly

3. **Does the organizer dashboard query work correctly**?
   - It joins transactions → events → filter by organizer_id
   - If EventID is missing/wrong, no data will show up

## Next Steps

1. Test a new Stripe payment and verify transaction is created with correct EventID
2. Run: `SELECT * FROM transactions WHERE created_at > NOW() - INTERVAL '1 hour' ORDER BY created_at DESC LIMIT 5;`
3. Check that `event_id` is populated correctly
4. Check organizer dashboard again after a fresh payment

### Query to verify:

```sql
SELECT
    COUNT(*) as total_transactions,
    COUNT(DISTINCT event_id) as events_with_transactions,
    SUM(amount) as total_revenue,
    SUM(organizer_share) as total_organizer_earnings
FROM transactions
WHERE created_at > NOW() - INTERVAL '1 day';
```

If this shows 0 earnings, the issue is that EventID is not being set correctly when creating transactions in the payment_worker.
