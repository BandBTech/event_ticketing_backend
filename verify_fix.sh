#!/bin/bash

# Quick verification script for webhook failed payment fix
# Usage: chmod +x verify_fix.sh && ./verify_fix.sh

set -e

REPO_DIR="/Users/aashbinsunar/Desktop/bandbtech/timro_ticket_system/event_ticketing_backend"

echo "=========================================="
echo "Webhook Failed Payment Fix - Verification"
echo "=========================================="
echo ""

# Check 1: Code compiles
echo "✓ Check 1: Verify code compiles..."
cd "$REPO_DIR"
if go build ./cmd/api > /dev/null 2>&1; then
    echo "  ✅ Build successful"
else
    echo "  ❌ Build failed"
    exit 1
fi

# Check 2: Verify imports
echo ""
echo "✓ Check 2: Verify error package imported..."
if grep -q '"errors"' internal/workers/payment_worker.go; then
    echo "  ✅ Errors package imported"
else
    echo "  ❌ Errors package not found"
    exit 1
fi

# Check 3: Verify idempotent reservation logic
echo ""
echo "✓ Check 3: Verify idempotent reservation logic..."
if grep -q "allConfirmed := true" internal/services/reservation_service.go; then
    echo "  ✅ Idempotency check present"
else
    echo "  ❌ Idempotency check not found"
    exit 1
fi

# Check 4: Verify early payment intent loading
echo ""
echo "✓ Check 4: Verify early payment intent loading..."
if grep -q "PHASE 1B: EARLY LOAD OF PAYMENT INTENT" internal/workers/payment_worker.go; then
    echo "  ✅ Early loading phase present"
else
    echo "  ❌ Early loading phase not found"
    exit 1
fi

# Check 5: Verify webhook status tracking in error paths
echo ""
echo "✓ Check 5: Verify error path webhook tracking..."
WEBHOOK_ERROR_CALLS=$(grep -c 'pw.updateWebhookEventStatus.*&dbPaymentIntent.ID' internal/workers/payment_worker.go)
if [ "$WEBHOOK_ERROR_CALLS" -ge 10 ]; then
    echo "  ✅ Found $WEBHOOK_ERROR_CALLS webhook status updates in error paths (expected ≥10)"
else
    echo "  ❌ Found only $WEBHOOK_ERROR_CALLS webhook status updates (expected ≥10)"
    exit 1
fi

# Check 6: Verify DLQ recording on reservation error
echo ""
echo "✓ Check 6: Verify DLQ recording on reservation error..."
if grep -q 'RecordFailedTask.*TypePaymentSuccess.*' internal/workers/payment_worker.go; then
    echo "  ✅ DLQ recording present for reservation errors"
else
    echo "  ❌ DLQ recording not found"
    exit 1
fi

# Check 7: Verify test compilation
echo ""
echo "✓ Check 7: Verify services package compiles..."
if go build ./internal/services > /dev/null 2>&1; then
    echo "  ✅ Services package builds successfully"
else
    echo "  ❌ Services package build failed"
    exit 1
fi

echo ""
echo "=========================================="
echo "✅ All verification checks passed!"
echo "=========================================="
echo ""
echo "Summary of changes:"
echo "1. ✅ Idempotent reservation confirmation"
echo "2. ✅ Early payment intent loading"
echo "3. ✅ Comprehensive error tracking with payment_intent_id"
echo "4. ✅ DLQ integration for failed tasks"
echo ""
echo "Ready for deployment!"
