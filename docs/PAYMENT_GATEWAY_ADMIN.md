# Payment Gateway Admin Management System

## Overview

A clean, simplified payment gateway management system for administrators. Configure payment gateways (Stripe, PayPal, eSewa, Khalti, Razorpay) dynamically through a RESTful API.

**Key Features:**

- ✅ Dynamic country/currency support - gateways handle automatic conversion
- ✅ Single consolidated action endpoint - no route duplication
- ✅ Query parameter-based filtering - flexible data retrieval
- ✅ Clean, reusable code architecture

## API Endpoints

### Base Path: `/api/v1/admin/payment-gateways`

**Required Permission:** `manage:payment_gateway`

| Method | Endpoint       | Purpose                           | Query Params                             |
| ------ | -------------- | --------------------------------- | ---------------------------------------- |
| GET    | `/`            | List configs or get gateway types | `?type=supported`, `?page=1&limit=10`    |
| POST   | `/`            | Create new gateway                | -                                        |
| GET    | `/:gateway_id` | Get specific gateway              | -                                        |
| PUT    | `/:gateway_id` | Update gateway config             | -                                        |
| DELETE | `/:gateway_id` | Delete gateway                    | -                                        |
| PATCH  | `/:gateway_id` | Perform actions                   | `?action=toggle\|test\|reload\|validate` |

## Usage Examples

### 1. List All Configured Gateways (Paginated)

```bash
GET /api/v1/admin/payment-gateways?page=1&limit=10
```

**Response:**

```json
{
  "success": true,
  "message": "Gateways retrieved successfully",
  "data": [...],
  "pagination": {
    "page": 1,
    "limit": 10,
    "total": 5,
    "total_pages": 1
  }
}
```

### 2. Get Supported Gateway Types

```bash
GET /api/v1/admin/payment-gateways?type=supported
```

**Response:**

```json
{
  "success": true,
  "data": [
    {
      "name": "stripe",
      "display_name": "Stripe",
      "description": "Global payment processing. Auto-converts 135+ currencies.",
      "features": ["cards", "wallets", "auto_conversion"],
      "requires_webhook": true,
      "setup_guide_url": "https://stripe.com/docs/keys"
    }
  ]
}
```

### 3. Create New Gateway

```bash
POST /api/v1/admin/payment-gateways
Content-Type: application/json

{
  "gateway_name": "stripe",
  "display_name": "Stripe",
  "is_enabled": false,
  "is_test_mode": true,
  "priority": 1,
  "api_key_encrypted": "pk_test_...",
  "api_secret_encrypted": "sk_test_...",
  "percentage_fee": 2.9,
  "fixed_fee": 0.30
}
```

**Note:** Leave `supported_countries` and `supported_currencies` empty for universal support.

### 4. Update Gateway Configuration

```bash
PUT /api/v1/admin/payment-gateways/{gateway_id}
Content-Type: application/json

{
  "api_key_encrypted": "pk_live_...",
  "api_secret_encrypted": "sk_live_..."
}
```

### 5. Gateway Actions (Single Unified Endpoint)

#### Enable/Disable Gateway

```bash
PATCH /api/v1/admin/payment-gateways/{gateway_id}?action=toggle
Content-Type: application/json

{
  "is_enabled": true
}
```

#### Toggle Sandbox/Production Mode

```bash
PATCH /api/v1/admin/payment-gateways/{gateway_id}?action=toggle
Content-Type: application/json

{
  "is_test_mode": false
}
```

#### Test Gateway Connection

```bash
PATCH /api/v1/admin/payment-gateways/{gateway_id}?action=test
```

**Response:**

```json
{
  "success": true,
  "message": "Connection test completed",
  "data": {
    "success": true,
    "gateway": "stripe",
    "mode": { "test_mode": true },
    "tested_at": "2026-02-06T10:00:00Z"
  }
}
```

#### Validate Gateway Configuration

```bash
PATCH /api/v1/admin/payment-gateways/{gateway_id}?action=validate
```

**Response:**

```json
{
  "data": {
    "is_valid": true,
    "errors": [],
    "warnings": ["gateway is in TEST/SANDBOX mode"]
  }
}
```

#### Reload All Gateways

```bash
PATCH /api/v1/admin/payment-gateways/all?action=reload
```

### 6. Delete Gateway

```bash
DELETE /api/v1/admin/payment-gateways/{gateway_id}
```

**Note:** Cannot delete if payments exist using this gateway.

## Dynamic Features

### Currency & Country Support

- **No hardcoded lists** - gateways handle all currencies/countries
- Payment gateway providers perform automatic currency conversion
- Optional filtering via `supported_countries` and `supported_currencies` fields
- Leave empty for universal support

### Gateway Types

Currently supported (dynamically extensible):

- **Stripe**: Global, 135+ currencies, auto-conversion
- **PayPal**: Global, 25+ currencies, auto-conversion
- **eSewa**: Nepal (NPR primary), handles conversions
- **Khalti**: Nepal (NPR primary), handles conversions
- **Razorpay**: India (INR primary), handles conversions

## Key Improvements

### Before (11 Separate Routes)

```
POST /payment-gateways/:id/toggle
POST /payment-gateways/:id/test
POST /payment-gateways/:id/mode
POST /payment-gateways/reload
POST /payment-gateways/:id/validate
GET  /payment-gateways/supported
```

### After (1 Unified Route)

```
PATCH /payment-gateways/:id?action=toggle|test|reload|validate
GET   /payment-gateways?type=supported
```

**Benefits:**

- ✅ 6 routes eliminated
- ✅ Cleaner API surface
- ✅ Easier to maintain
- ✅ Consistent patterns
- ✅ Query-based filtering

## Security

- **Permission Required:** `manage:payment_gateway` (admin-only)
- **Encrypted Storage:** All credentials encrypted at rest
- **No Sensitive Data:** API responses never expose secrets
- **Audit Logging:** All changes logged to `payment_audit_logs`
- **Test Mode First:** Always start with sandbox credentials

## Workflow

### Initial Setup

```bash
# 1. Get supported gateway types
GET /payment-gateways?type=supported

# 2. Create gateway (test mode)
POST /payment-gateways
{
  "gateway_name": "stripe",
  "is_test_mode": true,
  "api_key_encrypted": "pk_test_...",
  "api_secret_encrypted": "sk_test_..."
}

# 3. Test connection
PATCH /payment-gateways/{id}?action=test

# 4. Enable gateway
PATCH /payment-gateways/{id}?action=toggle
{"is_enabled": true}
```

### Going Live

```bash
# 1. Update to production credentials
PUT /payment-gateways/{id}
{
  "api_key_encrypted": "pk_live_...",
  "api_secret_encrypted": "sk_live_..."
}

# 2. Switch to production mode
PATCH /payment-gateways/{id}?action=toggle
{"is_test_mode": false}

# 3. Test connection
PATCH /payment-gateways/{id}?action=test

# 4. Reload all gateways
PATCH /payment-gateways/all?action=reload
```

## Error Handling

### Common Errors

- **400:** Invalid action parameter, missing required fields
- **404:** Gateway not found
- **409:** Duplicate gateway name
- **422:** Validation failed
- **500:** Server error, connection test failed

### Validation

- At least one API credential required
- Webhook secret recommended for production
- Cannot delete gateway with existing payments

## Future Enhancements

- Database-backed gateway types (full dynamic support)
- Automatic credential rotation
- Gateway health monitoring
- Cost optimization routing
- Failover to backup gateways

## Features

### 1. **Dynamic Gateway Configuration**

- Add new payment gateways via API
- Update API keys and credentials
- Toggle between sandbox and production modes
- Enable/disable gateways on the fly
- Set gateway priority for auto-selection
- **Dynamic country/currency support** - gateways handle conversion

### 2. **Multi-Gateway Support**

Currently supported gateways:

- **Stripe**: Global payment platform (auto-converts 135+ currencies)
- **PayPal**: Digital wallet (auto-converts 25+ currencies)
- **eSewa**: Nepal's leading digital wallet (primarily NPR, gateway handles conversions)
- **Khalti**: Nepal's digital payment service (primarily NPR, gateway handles conversions)
- **Razorpay**: India's payment gateway (primarily INR, gateway handles conversions)

### 3. **Security Features**

- Encrypted credential storage (API keys, secrets, webhook secrets)
- Test mode for safe testing with real credentials
- Connection testing before enabling
- Audit logging of all configuration changes
- **Admin-only access** (requires `manage:payment_gateway` permission)

### 4. **Gateway Management Operations**

#### List All Gateways (Paginated)

```
GET /api/v1/admin/payment-gateways?page=1&limit=10
```

Returns paginated list of configured payment gateways.

#### Create New Gateway

```
POST /api/v1/admin/payment-gateways
Content-Type: application/json

{
  "gateway_name": "stripe",
  "display_name": "Stripe",
  "is_enabled": false,
  "is_test_mode": true,
  "priority": 1,
  "supported_countries": [],  // Optional: Leave empty for all countries
  "supported_currencies": [], // Optional: Leave empty for all currencies (gateway handles conversion)
  "api_key_encrypted": "pk_test_...",
  "api_secret_encrypted": "sk_test_...",
  "webhook_secret_encrypted": "whsec_...",
  "percentage_fee": 2.9,
  "fixed_fee": 0.30,
  "min_amount": 50,
  "config": {
    "capture_method": "automatic"
  }
}
```

**Note**: `supported_countries` and `supported_currencies` are optional. If not specified, the gateway will handle all countries and currencies with automatic conversion.

#### Get Specific Gateway

```
GET /api/v1/admin/payment-gateways/{gateway_id}
```

#### Update Gateway

```
PUT /api/v1/admin/payment-gateways/{gateway_id}
Content-Type: application/json

{
  "api_key_encrypted": "pk_live_...",
  "api_secret_encrypted": "sk_live_...",
  "is_test_mode": false
}
```

#### Delete Gateway

```
DELETE /api/v1/admin/payment-gateways/{gateway_id}
```

Note: Cannot delete if payments exist using this gateway.

#### Enable/Disable Gateway

```
POST /api/v1/admin/payment-gateways/{gateway_id}/toggle
Content-Type: application/json

{
  "is_enabled": true
}
```

#### Toggle Sandbox/Production Mode

```
POST /api/v1/admin/payment-gateways/{gateway_id}/mode
Content-Type: application/json

{
  "is_test_mode": false
}
```

#### Test Gateway Connection

```
POST /api/v1/admin/payment-gateways/{gateway_id}/test
```

Creates a test payment intent and immediately cancels it to verify credentials.

Response:

```json
{
  "success": true,
  "gateway": "stripe",
  "message": "Connection test successful",
  "mode": { "test_mode": true },
  "tested_at": "2024-01-30T10:00:00Z"
}
```

#### Reload All Gateways

```
POST /api/v1/admin/payment-gateways/reload
```

Reloads all enabled gateway configurations from the database.

#### Get Supported Gateways

```
GET /api/v1/admin/payment-gateways/supported
```

Returns list of all supported gateway types with their features. Countries and currencies are dynamic - gateways handle conversion automatically.

Response:

```json
{
  "data": [
    {
      "name": "stripe",
      "display_name": "Stripe",
      "description": "Global payment processing platform. Supports 135+ currencies and countries. Gateway handles currency conversion.",
      "features": [
        "cards",
        "wallets",
        "bank_transfers",
        "subscriptions",
        "auto_conversion"
      ],
      "features": ["cards", "wallets", "bank_transfers", "subscriptions"],
      "requires_webhook": true,
      "setup_guide_url": "https://stripe.com/docs/keys"
    }
  ]
}
```

#### Validate Gateway Configuration

```
POST /api/v1/admin/payment-gateways/{gateway_id}/validate
```

Validates gateway configuration before enabling.

Response:

```json
{
  "is_valid": true,
  "errors": [],
  "warnings": ["gateway is in TEST/SANDBOX mode"]
}
```

## Gateway Configuration Fields

### Required Fields

| Field                  | Type   | Description                                                 |
| ---------------------- | ------ | ----------------------------------------------------------- |
| `gateway_name`         | string | Unique identifier (stripe, paypal, esewa, khalti, razorpay) |
| `display_name`         | string | Human-readable name                                         |
| `api_key_encrypted`    | string | API public key or client ID (encrypted)                     |
| `api_secret_encrypted` | string | API secret key (encrypted)                                  |

### Optional Fields

| Field                      | Type     | Default | Description                                      |
| -------------------------- | -------- | ------- | ------------------------------------------------ |
| `is_enabled`               | boolean  | false   | Whether gateway is active                        |
| `is_test_mode`             | boolean  | true    | Sandbox vs production                            |
| `priority`                 | integer  | 0       | Lower = higher priority for auto-selection       |
| `supported_countries`      | string[] | []      | ISO country codes (US, GB, NP, IN)               |
| `supported_currencies`     | string[] | []      | ISO currency codes (USD, NPR, INR)               |
| `webhook_secret_encrypted` | string   | -       | Webhook signature verification secret            |
| `percentage_fee`           | float    | 0       | Gateway's percentage fee (e.g., 2.9)             |
| `fixed_fee`                | float    | 0       | Gateway's fixed fee per transaction (e.g., 0.30) |
| `min_amount`               | float    | -       | Minimum transaction amount                       |
| `max_amount`               | float    | -       | Maximum transaction amount                       |
| `config`                   | object   | {}      | Gateway-specific configuration                   |

## Workflow

### Initial Setup

1. **Admin Login**: Login as admin user
2. **View Supported Gateways**: `GET /api/v1/admin/payment-gateways/supported`
3. **Create Gateway Config**: `POST /api/v1/admin/payment-gateways`
   - Start in test mode
   - Add sandbox credentials
4. **Test Connection**: `POST /api/v1/admin/payment-gateways/{id}/test`
5. **Enable Gateway**: `POST /api/v1/admin/payment-gateways/{id}/toggle` with `is_enabled: true`

### Going Live

1. **Update Credentials**: `PUT /api/v1/admin/payment-gateways/{id}`
   - Add production API keys
2. **Toggle to Production**: `POST /api/v1/admin/payment-gateways/{id}/mode` with `is_test_mode: false`
3. **Test Connection**: Verify production credentials work
4. **Reload Gateways**: `POST /api/v1/admin/payment-gateways/reload`

### Credential Management by Gateway

#### Stripe

- `api_key_encrypted`: Publishable Key (pk*test*... or pk*live*...)
- `api_secret_encrypted`: Secret Key (sk*test*... or sk*live*...)
- `webhook_secret_encrypted`: Webhook Signing Secret (whsec\_...)

#### PayPal

- `api_key_encrypted`: Client ID
- `api_secret_encrypted`: Client Secret
- Use `is_test_mode` to toggle sandbox vs live

#### eSewa

- `api_key_encrypted`: Merchant ID
- `api_secret_encrypted`: Secret Key
- `supported_currencies`: ["NPR"]

#### Khalti

- `api_key_encrypted`: Public Key
- `api_secret_encrypted`: Secret Key
- `supported_currencies`: ["NPR"]

#### Razorpay

- `api_key_encrypted`: Key ID
- `api_secret_encrypted`: Key Secret
- `supported_currencies`: ["INR"]

## Security Best Practices

1. **Always start in test mode** - Use sandbox credentials first
2. **Test connections** - Verify credentials before enabling
3. **Limit access** - Only full admins can manage gateways
4. **Audit trail** - All changes are logged in `payment_audit_logs`
5. **Encrypted storage** - Credentials are encrypted at rest
6. **No exposure** - Secrets never returned in API responses

## Initialization Behavior

- On server startup, the system checks for enabled gateways
- If no gateways configured: Logs info message (non-blocking)
- If gateways configured: Initializes enabled ones
- Can reload gateways at runtime without restart

## Error Handling

### Common Errors

- **Duplicate Gateway**: Cannot create multiple configs for same gateway type
- **Missing Credentials**: Validation fails if required keys missing
- **Invalid Credentials**: Connection test will fail
- **Delete Protection**: Cannot delete gateway with existing payments

### Validation

- Required field checks
- Credential format validation
- Supported currency/country validation
- Test mode warnings

## Monitoring & Audit

All gateway management operations are logged to `payment_audit_logs`:

- Who performed the action (admin user)
- What changed (before/after values)
- When it happened (timestamp)
- Context (IP address, user agent)

Example audit log entry:

```json
{
  "action": "gateway_config_updated",
  "entity_type": "payment_gateway_config",
  "entity_id": "uuid",
  "actor_id": "admin_uuid",
  "actor_type": "admin",
  "metadata": {
    "gateway_name": "stripe",
    "updated_fields": ["api_secret_encrypted", "is_test_mode"]
  },
  "timestamp": "2024-01-30T10:00:00Z"
}
```

## Future Enhancements

- [ ] Encryption/decryption implementation for credentials
- [ ] Automatic credential rotation
- [ ] Gateway health monitoring
- [ ] Transaction routing based on success rates
- [ ] A/B testing between gateways
- [ ] Cost optimization (auto-select cheapest gateway)
- [ ] Gateway-specific webhook URL configuration
- [ ] Rate limiting per gateway
- [ ] Failover to backup gateway

## Related Documentation

- [API.md](./API.md) - Complete API reference
- [PAYMENT_FLOW.md](./PAYMENT_FLOW.md) - Payment processing flow
- [SECURITY.md](./SECURITY.md) - Security guidelines
- [DEPLOYMENT.md](./DEPLOYMENT.md) - Production deployment guide
