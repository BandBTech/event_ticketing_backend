# Adding New Payment Gateways

This document explains how to add new payment gateways to the ticket purchasing system.

## Current Setup

- **ActivePaymentGateways**: Single list of payment gateways allowed for purchases (fully integrated and tested only)

## How to Add a New Payment Gateway

### Step 1: Add Gateway Constant

Add the new gateway constant to `internal/models/constants.go`:

```go
const (
    PaymentGatewayCash   PaymentGateway = "cash"
    PaymentGatewayStripe PaymentGateway = "stripe"
    PaymentGatewayPayPal PaymentGateway = "paypal"  // NEW GATEWAY
    // ... other gateways
)
```

### Step 2: Add to Active Gateways (IMPORTANT)

**ONLY** add the gateway to `ActivePaymentGateways` when it is **fully integrated and tested**:

```go
var ActivePaymentGateways = []PaymentGateway{
    PaymentGatewayCash,   // Always allowed
    PaymentGatewayStripe, // Fully integrated
    PaymentGatewayPayPal, // Add here ONLY when fully ready for purchases
}
```

### Step 3: Implement Gateway Logic

Implement the actual payment processing logic in the payment services:

- `internal/services/payment_service.go`
- Gateway-specific handlers and webhooks
- Database models for gateway-specific data

### Step 4: Test Thoroughly

- Unit tests for gateway logic
- Integration tests for payment flow
- End-to-end testing with real payments (if possible)
- Error handling and edge cases

### Step 5: Deploy and Monitor

- Deploy to staging environment first
- Monitor payment success rates
- Have rollback plan ready

## Safety Features

- **Single Source of Truth**: Only `ActivePaymentGateways` controls what's allowed for purchases
- **Validation**: `validatePurchasePaymentGateway` checks against `ActivePaymentGateways`
- **Gradual Rollout**: Test thoroughly before adding to the active list

## Example: Adding PayPal

```go
// 1. Add constant
const PaymentGatewayPayPal PaymentGateway = "paypal"

// 2. Add to active gateways ONLY when fully ready
var ActivePaymentGateways = []PaymentGateway{
    PaymentGatewayCash,
    PaymentGatewayStripe,
    PaymentGatewayPayPal, // Ready for purchases
}
```

## Current Status

- ✅ **Cash**: Always allowed (no integration needed)
- ✅ **Stripe**: Fully integrated and tested
- ⏳ **PayPal**: Gateway constant exists, integration pending
- ⏳ **eSewa, Khalti, IME Pay**: Constants exist, integration pending

Implement the actual payment processing logic in the payment services:

- `internal/services/payment_service.go`
- Gateway-specific handlers and webhooks
- Database models for gateway-specific data

### Step 5: Test Thoroughly

- Unit tests for gateway logic
- Integration tests for payment flow
- End-to-end testing with real payments (if possible)
- Error handling and edge cases

### Step 6: Deploy and Monitor

- Deploy to staging environment first
- Monitor payment success rates
- Have rollback plan ready

## Safety Features

- **Validation**: `validatePurchasePaymentGateway` only allows gateways in `PurchaseAllowedPaymentGateways`
- **Separation**: General gateway availability ≠ Purchase permission
- **Gradual Rollout**: Can enable gateway for config before allowing purchases

## Example: Adding PayPal

```go
// 1. Add constant
const PaymentGatewayPayPal PaymentGateway = "paypal"

// 2. Add to active gateways (for admin use)
var ActivePaymentGateways = []PaymentGateway{
    PaymentGatewayCash,
    PaymentGatewayStripe,
    PaymentGatewayPayPal,  // Available for config
}

// 3. Add to purchase-allowed ONLY when ready
var PurchaseAllowedPaymentGateways = []PaymentGateway{
    PaymentGatewayCash,
    PaymentGatewayStripe,
    PaymentGatewayPayPal,  // Ready for purchases
}
```

## Current Status

- ✅ **Cash**: Always allowed (no integration needed)
- ✅ **Stripe**: Fully integrated and tested
- ⏳ **PayPal**: Gateway constant exists, integration pending
- ⏳ **eSewa, Khalti, IME Pay**: Constants exist, integration pending</content>
  <parameter name="filePath">/Users/aashbinsunar/Desktop/bandbtech/timro_ticket_system/event_ticketing_backend/docs/ADDING_PAYMENT_GATEWAYS.md
