# Payment URL Configuration

## Overview

Payment callback URLs (success, failed, cancel) are now fully centralized and configurable via environment variables. This eliminates hardcoded URLs and makes the system adaptable to different environments (development, staging, production).

## Configuration

### Environment Variables

Add these to your `.env` file:

```bash
# Payment Gateway Callback URLs (used by all payment gateways)
# These URLs are appended with /{checkout_token} when redirecting after payment
PAYMENT_SUCCESS_URL=https://user.timroticket.com/payment/success
PAYMENT_FAILED_URL=https://user.timroticket.com/payment/failed
PAYMENT_CANCEL_URL=https://user.timroticket.com/payment/cancel
```

### Default Behavior

If these environment variables are **not set**, the system falls back to:

```
PAYMENT_SUCCESS_URL = {FRONTEND_BASE_URL}/payment/success
PAYMENT_FAILED_URL  = {FRONTEND_BASE_URL}/payment/failed
PAYMENT_CANCEL_URL  = {FRONTEND_BASE_URL}/payment/cancel
```

Where `FRONTEND_BASE_URL` defaults to `https://user.timroticket.com`

## How It Works

### 1. Configuration Loading

```go
// pkg/config/config.go
type PaymentConfig struct {
    CashAllowedEmails []string
    SuccessURL        string
    FailedURL         string
    CancelURL         string
}

// Loaded in config.Load()
Payment: PaymentConfig{
    CashAllowedEmails: getEnvAsSlice("CASH_ALLOWED_EMAILS", []string{}),
    SuccessURL:        getEnv("PAYMENT_SUCCESS_URL", ...),
    FailedURL:         getEnv("PAYMENT_FAILED_URL", ...),
    CancelURL:         getEnv("PAYMENT_CANCEL_URL", ...),
}
```

### 2. Service Layer Access

```go
// internal/services/ticket_service.go
func (s *TicketService) getPaymentSuccessURL() string {
    if s.cfg != nil {
        return s.cfg.Payment.SuccessURL
    }
    return s.getBaseURL() + "/payment/success"
}

func (s *TicketService) getPaymentFailedURL() string {
    if s.cfg != nil {
        return s.cfg.Payment.FailedURL
    }
    return s.getBaseURL() + "/payment/failed"
}

func (s *TicketService) getPaymentCancelURL() string {
    if s.cfg != nil {
        return s.cfg.Payment.CancelURL
    }
    return s.getBaseURL() + "/payment/cancel"
}
```

### 3. Usage in Payment Gateway Initialization

**Stripe:**

```go
case models.PaymentGatewayStripe:
    checkoutSession.GatewayData = map[string]interface{}{
        "success_url": fmt.Sprintf("%s/%s", s.getPaymentSuccessURL(), checkoutToken),
        "cancel_url":  fmt.Sprintf("%s/%s", s.getPaymentCancelURL(), checkoutToken),
        // ...
    }
```

**PayPal:**

```go
case models.PaymentGatewayPayPal:
    checkoutSession.GatewayData = map[string]interface{}{
        "application_context": map[string]interface{}{
            "return_url": fmt.Sprintf("%s/%s", s.getPaymentSuccessURL(), checkoutToken),
            "cancel_url": fmt.Sprintf("%s/%s", s.getPaymentCancelURL(), checkoutToken),
        },
    }
```

**eSewa:**

```go
case models.PaymentGatewayEsewa:
    checkoutSession.GatewayData = map[string]interface{}{
        "su": fmt.Sprintf("%s/%s", s.getPaymentSuccessURL(), checkoutToken),
        "fu": fmt.Sprintf("%s/%s", s.getPaymentFailedURL(), checkoutToken),
    }
```

## Gateway-Specific URL Patterns

All payment gateways now use the same URL pattern:

| Gateway      | Success URL                              | Cancel/Failed URL                       |
| ------------ | ---------------------------------------- | --------------------------------------- |
| **Stripe**   | `{PAYMENT_SUCCESS_URL}/{checkout_token}` | `{PAYMENT_CANCEL_URL}/{checkout_token}` |
| **PayPal**   | `{PAYMENT_SUCCESS_URL}/{checkout_token}` | `{PAYMENT_CANCEL_URL}/{checkout_token}` |
| **eSewa**    | `{PAYMENT_SUCCESS_URL}/{checkout_token}` | `{PAYMENT_FAILED_URL}/{checkout_token}` |
| **Khalti**   | `{PAYMENT_SUCCESS_URL}/{checkout_token}` | `{PAYMENT_CANCEL_URL}/{checkout_token}` |
| **Razorpay** | `{PAYMENT_SUCCESS_URL}/{checkout_token}` | `{PAYMENT_CANCEL_URL}/{checkout_token}` |

**Example:**

```
Success: https://user.timroticket.com/payment/success/abc123xyz
Failed:  https://user.timroticket.com/payment/failed/abc123xyz
Cancel:  https://user.timroticket.com/payment/cancel/abc123xyz
```

## Benefits

### ✅ Environment-Specific URLs

**Development:**

```bash
PAYMENT_SUCCESS_URL=http://localhost:3000/payment/success
PAYMENT_FAILED_URL=http://localhost:3000/payment/failed
PAYMENT_CANCEL_URL=http://localhost:3000/payment/cancel
```

**Staging:**

```bash
PAYMENT_SUCCESS_URL=https://staging.timroticket.com/payment/success
PAYMENT_FAILED_URL=https://staging.timroticket.com/payment/failed
PAYMENT_CANCEL_URL=https://staging.timroticket.com/payment/cancel
```

**Production:**

```bash
PAYMENT_SUCCESS_URL=https://user.timroticket.com/payment/success
PAYMENT_FAILED_URL=https://user.timroticket.com/payment/failed
PAYMENT_CANCEL_URL=https://user.timroticket.com/payment/cancel
```

### ✅ No Code Changes Needed

Change URLs without modifying code or redeploying:

1. Update `.env` file
2. Restart server (or use hot reload if implemented)
3. Done!

### ✅ Consistent Across All Gateways

All payment gateways use the same base URLs - no gateway-specific hardcoding.

### ✅ Testing Flexibility

**Mock Payment Gateway Testing:**

```bash
PAYMENT_SUCCESS_URL=http://localhost:8082/test/payment/success
PAYMENT_FAILED_URL=http://localhost:8082/test/payment/failed
```

**Ngrok Testing:**

```bash
PAYMENT_SUCCESS_URL=https://abc123.ngrok.io/payment/success
```

## Migration Guide

### Before (Hardcoded)

```go
// ❌ Old way - hardcoded
baseURL := "https://user.timroticket.com"
successURL := fmt.Sprintf("%s/payment/success/%s", baseURL, token)
cancelURL := fmt.Sprintf("%s/payment/cancel/%s", baseURL, token)
```

### After (Configured)

```go
// ✅ New way - from config
successURL := fmt.Sprintf("%s/%s", s.getPaymentSuccessURL(), token)
cancelURL := fmt.Sprintf("%s/%s", s.getPaymentCancelURL(), token)
```

## Frontend Integration

Your frontend should handle these URL patterns:

```typescript
// Success page
// Route: /payment/success/:checkout_token
app.get("/payment/success/:checkout_token", async (req, res) => {
  const { checkout_token } = req.params;

  // Verify payment status with backend
  const response = await fetch(`${API_BASE}/payments/verify/${checkout_token}`);

  if (response.ok) {
    // Show success page
    res.render("payment-success", { checkout_token });
  }
});

// Failed page
// Route: /payment/failed/:checkout_token
app.get("/payment/failed/:checkout_token", (req, res) => {
  const { checkout_token } = req.params;
  res.render("payment-failed", { checkout_token });
});

// Cancel page
// Route: /payment/cancel/:checkout_token
app.get("/payment/cancel/:checkout_token", (req, res) => {
  const { checkout_token } = req.params;
  res.render("payment-cancel", { checkout_token });
});
```

## Webhook vs Redirect URLs

**Important Distinction:**

- **Redirect URLs** (this document): Where users are redirected after payment
- **Webhook URLs**: Where payment gateway sends server-to-server notifications

```
Redirect URLs (User-facing):
✅ PAYMENT_SUCCESS_URL - User sees success page
✅ PAYMENT_FAILED_URL  - User sees error page
✅ PAYMENT_CANCEL_URL  - User cancels payment

Webhook URLs (Server-to-server):
✅ /api/v1/webhooks/stripe  - Stripe sends events
✅ /api/v1/webhooks/paypal  - PayPal sends events
✅ /api/v1/webhooks/{gateway} - Dynamic webhook endpoint
```

**Both are required** for proper payment processing:

1. Webhooks update payment status in database
2. Redirect URLs show appropriate page to user

## Testing

### Local Development

```bash
# .env.local
PAYMENT_SUCCESS_URL=http://localhost:3000/payment/success
PAYMENT_FAILED_URL=http://localhost:3000/payment/failed
PAYMENT_CANCEL_URL=http://localhost:3000/payment/cancel

# Frontend running on http://localhost:3000
npm run dev
```

### Docker Compose

```bash
# .env (Docker)
PAYMENT_SUCCESS_URL=http://host.docker.internal:3000/payment/success
PAYMENT_FAILED_URL=http://host.docker.internal:3000/payment/failed
PAYMENT_CANCEL_URL=http://host.docker.internal:3000/payment/cancel
```

### Gateway Testing

**Stripe Test Mode:**

```bash
# Backend receives webhook at: http://localhost:8082/api/v1/webhooks/stripe
# User redirected to: http://localhost:3000/payment/success/{token}
```

Test with Stripe CLI:

```bash
stripe listen --forward-to localhost:8082/api/v1/webhooks/stripe
stripe trigger payment_intent.succeeded
```

## Security Considerations

### ✅ No Sensitive Data in URLs

URLs only contain the checkout token - no payment details:

```
✅ Good: https://example.com/payment/success/abc123xyz
❌ Bad:  https://example.com/payment/success?card=1234&cvv=123
```

### ✅ Token-Based Verification

Frontend must verify payment status with backend:

```javascript
// ❌ Don't trust URL alone
if (window.location.pathname.includes("success")) {
  showSuccessMessage(); // WRONG!
}

// ✅ Verify with backend
const token = params.checkout_token;
const status = await verifyPayment(token);
if (status === "completed") {
  showSuccessMessage(); // CORRECT!
}
```

### ✅ HTTPS in Production

Always use HTTPS for payment redirect URLs:

```bash
# ❌ Development only
PAYMENT_SUCCESS_URL=http://localhost:3000/payment/success

# ✅ Production
PAYMENT_SUCCESS_URL=https://user.timroticket.com/payment/success
```

## Troubleshooting

### Issue: URLs Not Working

**Check:**

1. Environment variables loaded correctly

   ```bash
   echo $PAYMENT_SUCCESS_URL
   ```

2. Server restarted after .env changes

   ```bash
   docker-compose restart api
   ```

3. Frontend routes handle the URL pattern
   ```javascript
   // Must have route: /payment/success/:checkout_token
   ```

### Issue: Gateway Redirects to Wrong URL

**Solution:**

1. Check gateway configuration in admin panel
2. Reload gateways: `PATCH /admin/payment-gateways/all?action=reload`
3. Test connection: `PATCH /admin/payment-gateways/{id}?action=test`

### Issue: Different URLs for Different Gateways

**Not Needed!** All gateways use the same URLs from config:

```go
// ✅ All gateways use these
s.getPaymentSuccessURL()  // Same for Stripe, PayPal, eSewa, etc.
s.getPaymentFailedURL()   // Same for all
s.getPaymentCancelURL()   // Same for all
```

## Summary

**Before:**

- ❌ Hardcoded URLs in multiple places
- ❌ Different URLs for different gateways
- ❌ Code changes needed for URL updates
- ❌ No environment-specific URLs

**After:**

- ✅ Centralized configuration via .env
- ✅ Same URLs for all gateways
- ✅ Zero code changes for URL updates
- ✅ Environment-specific URLs supported
- ✅ Graceful fallbacks to defaults

**Files Modified:**

- `pkg/config/config.go` - Added PaymentConfig fields
- `internal/services/ticket_service.go` - Added URL helper methods
- `.env` - Added PAYMENT\_\*\_URL variables
- `.env.example` - Documented new variables

All payment gateways now use centralized, configurable callback URLs! 🎉
