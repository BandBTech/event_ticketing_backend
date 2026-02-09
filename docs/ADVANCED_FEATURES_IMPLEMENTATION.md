# Advanced Features Implementation Complete

## Overview

Successfully implemented 4 critical production-ready features for the event ticketing payment system:

1. ✅ **Zero-Downtime Key Rotation**
2. ✅ **Audit Log Query API**
3. ✅ **Webhook Replay System**
4. ✅ **Transaction Retry Mechanism**

---

## 1. Zero-Downtime Key Rotation

### What Was Implemented

**Files Modified:**

- `pkg/utils/encryption.go` - Added multi-key decryption functions
- `pkg/config/config.go` - Added rotation key configuration
- `internal/gateways/factory.go` - Updated to use rotation keys
- `internal/services/payment_service.go` - Updated to encrypt with key ID
- `.env.example` - Added rotation environment variables

**New Functions:**

```go
// Encrypt with key version ID
EncryptAES256GCMWithID(plaintext, key, keyID string) (string, error)

// Decrypt with primary key + fallback to rotation keys
DecryptAES256GCMWithRotation(ciphertext, primaryKey string, rotationKeys []string) (string, error)
```

**Configuration:**

```env
# Primary encryption key (for new encryptions)
CREDENTIAL_ENCRYPTION_KEY=<new-32-byte-key>

# Old keys (comma-separated, for decryption during rotation)
CREDENTIAL_ENCRYPTION_ROTATION_KEYS=<old-key-1>,<old-key-2>

# Key version identifier
CREDENTIAL_ENCRYPTION_KEY_ID=v2
```

### How to Perform Key Rotation

**Step 1: Generate New Key**

```bash
./scripts/generate-encryption-key.sh
# Output: g8x9YpQzW3vN5mKjH7sA2cR4tU6wE1oB9
```

**Step 2: Update Environment**

```env
# Move old key to rotation keys
CREDENTIAL_ENCRYPTION_KEY=g8x9YpQzW3vN5mKjH7sA2cR4tU6wE1oB9       # NEW
CREDENTIAL_ENCRYPTION_ROTATION_KEYS=old_key_abc123def456        # OLD KEY
CREDENTIAL_ENCRYPTION_KEY_ID=v2                                  # INCREMENT
```

**Step 3: Rolling Restart (Zero Downtime)**

```bash
# For Docker
docker-compose restart api

# For Kubernetes
kubectl rollout restart deployment/event-ticketing-backend

# For systemd
systemctl restart event-ticketing-api
```

**Step 4: Re-encrypt All Credentials**

```bash
curl -X POST http://localhost:8080/api/v1/admin/payment-gateways/reencrypt \
  -H "Authorization: Bearer <admin_token>"

# Response:
# {
#   "success": true,
#   "message": "Successfully re-encrypted 5 gateway configurations",
#   "data": {
#     "configs_updated": 5,
#     "new_key_version": "v2"
#   }
# }
```

**Step 5: Verify (check database)**

```sql
SELECT gateway_name,
       SUBSTRING(api_key_encrypted FROM 1 FOR 3) as key_prefix
FROM payment_gateway_configs;

-- Should see: v2:xxx for all gateways
```

**Step 6: Remove Old Key (After 30 Days)**

```env
CREDENTIAL_ENCRYPTION_KEY=g8x9YpQzW3vN5mKjH7sA2cR4tU6wE1oB9
CREDENTIAL_ENCRYPTION_ROTATION_KEYS=                             # EMPTY
CREDENTIAL_ENCRYPTION_KEY_ID=v2
```

### Benefits

- ✅ **No downtime** - Service continues running during rotation
- ✅ **Gradual migration** - Old data decrypts with rotation keys
- ✅ **Audit trail** - Key version tracked in encrypted data (v1:, v2:)
- ✅ **Rollback support** - Can revert if issues arise
- ✅ **Compliance** - Meets key rotation requirements (90-day recommended)

---

## 2. Audit Log Query API

### Endpoint

```
GET /api/v1/admin/payments/audit-logs
```

### Query Parameters

| Parameter   | Type   | Description                  | Example                       |
| ----------- | ------ | ---------------------------- | ----------------------------- |
| page        | int    | Page number (default: 1)     | `?page=2`                     |
| limit       | int    | Items per page (default: 50) | `?limit=100`                  |
| action      | string | Filter by action             | `?action=payment_created`     |
| entity_type | string | Filter by entity type        | `?entity_type=payment_intent` |
| entity_id   | UUID   | Filter by entity ID          | `?entity_id=uuid`             |
| actor_id    | UUID   | Filter by actor (user/admin) | `?actor_id=uuid`              |

### Response

```json
{
  "success": true,
  "message": "Audit logs retrieved successfully",
  "data": {
    "logs": [
      {
        "id": "uuid",
        "action": "payment_created",
        "entity_type": "payment_intent",
        "entity_id": "payment-uuid",
        "actor": {
          "id": "user-uuid",
          "name": "John Doe",
          "email": "john@example.com"
        },
        "actor_type": "user",
        "changes_before": null,
        "changes_after": {
          "amount": 199.99,
          "currency": "USD",
          "status": "pending"
        },
        "ip_address": "192.168.1.1",
        "user_agent": "Mozilla/5.0...",
        "timestamp": "2024-02-06T10:30:00Z"
      }
    ],
    "pagination": {
      "total": 1234,
      "page": 1,
      "limit": 50,
      "total_pages": 25
    }
  }
}
```

### Use Cases

**1. View All Payment Operations**

```bash
GET /api/v1/admin/payments/audit-logs?action=payment_created&limit=100
```

**2. Track Gateway Configuration Changes**

```bash
GET /api/v1/admin/payments/audit-logs?entity_type=gateway_config
```

**3. View Specific Admin's Actions**

```bash
GET /api/v1/admin/payments/audit-logs?actor_id=<admin_uuid>
```

**4. Investigate Failed Payments**

```bash
GET /api/v1/admin/payments/audit-logs?action=payment_failed
```

**5. Audit Refund Approvals**

```bash
GET /api/v1/admin/payments/audit-logs?action=refund_approved
```

---

## 3. Webhook Replay System

### Endpoint

```
POST /api/v1/admin/payments/webhooks/{webhook_id}/retry
```

### Use Case

When a webhook from a payment gateway (Stripe, PayPal, etc.) fails to process, admins can manually retry it.

### Example

**Step 1: Find Failed Webhooks**

```sql
SELECT id, payment_gateway, event_type, processed_count, last_error, received_at
FROM webhook_events
WHERE status = 'failed'
ORDER BY received_at DESC
LIMIT 20;
```

**Step 2: Retry Webhook**

```bash
curl -X POST http://localhost:8080/api/v1/admin/payments/webhooks/{webhook_id}/retry \
  -H "Authorization: Bearer <admin_token>"
```

**Response:**

```json
{
  "success": true,
  "message": "Webhook retry initiated successfully",
  "data": null
}
```

### What Happens

1. **Fetches webhook** from `webhook_events` table
2. **Validates** webhook is retryable (not already processed, < 5 attempts)
3. **Gets payment gateway** instance (Stripe, PayPal, etc.)
4. **Re-verifies signature** and processes webhook payload
5. **Updates status** to "processed" or "failed" with error details

### Automatic Retry

Webhooks automatically retry with **exponential backoff**:

- Attempt 1: Immediate
- Attempt 2: After 5 minutes
- Attempt 3: After 15 minutes
- Attempt 4: After 1 hour
- Attempt 5: After 4 hours

After 5 failed attempts, admin manual retry is required.

---

## 4. Transaction Retry Mechanism

### Endpoint

```
POST /api/v1/admin/payments/transactions/{payment_intent_id}/retry
```

### Use Case

When a payment intent fails (card declined, network error, etc.), admins can manually check the latest status from the gateway and update the system.

### Example

**Step 1: Find Failed Payments**

```sql
SELECT id, gateway_payment_id, payment_gateway, customer_email,
       total_amount, status, failed_at
FROM payment_intents
WHERE status = 'failed'
ORDER BY failed_at DESC
LIMIT 20;
```

**Step 2: Retry Transaction**

```bash
curl -X POST http://localhost:8080/api/v1/admin/payments/transactions/{payment_intent_id}/retry \
  -H "Authorization: Bearer <admin_token>"
```

**Response:**

```json
{
  "success": true,
  "message": "Transaction retry initiated successfully",
  "data": null
}
```

### What Happens

1. **Fetches payment intent** from database
2. **Validates** payment is retryable (failed or canceled status)
3. **Queries gateway** for latest payment intent status (via gateway API)
4. **Syncs status** - Updates local database with latest gateway status
5. **Issues tickets** if payment succeeded on gateway but not reflected locally

### Common Scenarios

**Scenario 1: Payment Succeeded on Gateway, Failed Locally**

- Gateway shows "succeeded" but local DB shows "failed"
- Retry fetches status, updates to "succeeded", issues tickets

**Scenario 2: Temporary Network Error**

- Payment was initiated but webhook never arrived
- Retry fetches current status and updates accordingly

**Scenario 3: Customer Completed Payment Later**

- Payment intent created, customer left, returned later to complete
- Retry checks if payment now succeeded

---

## New Admin API Endpoints Summary

| Endpoint                                        | Method | Permission               | Description                    |
| ----------------------------------------------- | ------ | ------------------------ | ------------------------------ |
| `/api/v1/admin/payments/audit-logs`             | GET    | `read:financial`         | Query audit logs               |
| `/api/v1/admin/payments/webhooks/:id/retry`     | POST   | `read:financial`         | Retry failed webhook           |
| `/api/v1/admin/payments/transactions/:id/retry` | POST   | `read:financial`         | Retry failed transaction       |
| `/api/v1/admin/payment-gateways/reencrypt`      | POST   | `manage:payment_gateway` | Re-encrypt all gateway configs |

---

## Security Considerations

### Key Rotation

- ✅ Master key in `.env` (environment variable)
- ✅ Rotation keys for backward compatibility
- ✅ Key version tracking (v1, v2, v3)
- ✅ Gradual migration (30-day overlap recommended)
- ✅ AWS Secrets Manager support (documented)

### Audit Logging

- ✅ Immutable logs (no UPDATE operations)
- ✅ Complete change tracking (before/after states)
- ✅ Actor identification (user, admin, system, webhook)
- ✅ IP address and user agent logging
- ✅ 7-year retention for compliance

### Webhook Replay

- ✅ Signature re-verification before processing
- ✅ Idempotent processing (prevents duplicates)
- ✅ Rate limiting (max 5 retry attempts)
- ✅ Admin-only access

### Transaction Retry

- ✅ Validates payment state before retry
- ✅ Queries authoritative source (gateway API)
- ✅ No duplicate payment creation
- ✅ Admin-only access

---

## Testing

### 1. Test Key Rotation

```bash
# Generate test keys
KEY1=$(openssl rand -base64 32)
KEY2=$(openssl rand -base64 32)

# Create test gateway with KEY1
export CREDENTIAL_ENCRYPTION_KEY=$KEY1
export CREDENTIAL_ENCRYPTION_KEY_ID=v1
curl -X POST http://localhost:8080/api/v1/admin/payment-gateways \
  -H "Authorization: Bearer <admin_token>" \
  -d '{"gateway_name": "test", ...}'

# Rotate to KEY2
export CREDENTIAL_ENCRYPTION_KEY=$KEY2
export CREDENTIAL_ENCRYPTION_ROTATION_KEYS=$KEY1
export CREDENTIAL_ENCRYPTION_KEY_ID=v2

# Re-encrypt
curl -X POST http://localhost:8080/api/v1/admin/payment-gateways/reencrypt \
  -H "Authorization: Bearer <admin_token>"

# Verify gateway still works
curl http://localhost:8080/api/v1/admin/payment-gateways
```

### 2. Test Audit Logs

```bash
# Query all payment actions
curl "http://localhost:8080/api/v1/admin/payments/audit-logs?action=payment_created" \
  -H "Authorization: Bearer <admin_token>"

# Query specific entity
curl "http://localhost:8080/api/v1/admin/payments/audit-logs?entity_type=refund" \
  -H "Authorization: Bearer <admin_token>"
```

### 3. Test Webhook Retry

```bash
# List failed webhooks
psql -d event_ticketing -c "SELECT id FROM webhook_events WHERE status='failed' LIMIT 1;"

# Retry
curl -X POST "http://localhost:8080/api/v1/admin/payments/webhooks/{id}/retry" \
  -H "Authorization: Bearer <admin_token>"
```

### 4. Test Transaction Retry

```bash
# List failed payments
psql -d event_ticketing -c "SELECT id FROM payment_intents WHERE status='failed' LIMIT 1;"

# Retry
curl -X POST "http://localhost:8080/api/v1/admin/payments/transactions/{id}/retry" \
  -H "Authorization: Bearer <admin_token>"
```

---

## Production Deployment Checklist

### Before Deployment

- [ ] Generate production encryption key: `openssl rand -base64 32`
- [ ] Store key in AWS Secrets Manager or secure vault
- [ ] Update `.env` with production key
- [ ] Test key rotation in staging environment
- [ ] Verify audit logs are being created
- [ ] Test webhook retry with test gateway
- [ ] Test transaction retry with test payment

### After Deployment

- [ ] Monitor failed webhooks: `SELECT COUNT(*) FROM webhook_events WHERE status='failed';`
- [ ] Monitor failed payments: `SELECT COUNT(*) FROM payment_intents WHERE status='failed';`
- [ ] Set up alerts for high failure rates
- [ ] Schedule key rotation (every 90 days recommended)
- [ ] Review audit logs weekly for suspicious activity
- [ ] Test re-encryption endpoint works

### Key Rotation Schedule

**Recommended Timeline:**

```
Day 0:   Generate new key, add to rotation keys, deploy
Day 1:   Run re-encryption endpoint
Day 2:   Verify all gateways work
Day 30:  Remove old key from rotation keys
Day 90:  Repeat rotation process
```

---

## Troubleshooting

### Issue: Decryption Failed After Key Rotation

**Cause:** Old key not in rotation keys or typo in key

**Solution:**

```bash
# Check current config
echo $CREDENTIAL_ENCRYPTION_KEY
echo $CREDENTIAL_ENCRYPTION_ROTATION_KEYS

# Add old key back to rotation keys
export CREDENTIAL_ENCRYPTION_ROTATION_KEYS="old-key-1,old-key-2"

# Restart service
systemctl restart event-ticketing-api
```

### Issue: Webhook Retry Fails with "Already Processed"

**Cause:** Webhook already succeeded

**Solution:**

```sql
-- Check webhook status
SELECT status, processed_count FROM webhook_events WHERE id = 'webhook-uuid';

-- If stuck in "processing", reset to "failed"
UPDATE webhook_events SET status = 'failed' WHERE id = 'webhook-uuid';
```

### Issue: Transaction Retry Returns "Already Succeeded"

**Cause:** Payment already completed

**Solution:**
Check payment intent status in database and on gateway dashboard to confirm state.

### Issue: Audit Logs Not Appearing

**Cause:** Audit log creation might have failed

**Solution:**

```sql
-- Check recent audit logs
SELECT COUNT(*), action FROM payment_audit_logs
WHERE timestamp > NOW() - INTERVAL '1 hour'
GROUP BY action;

-- If none, check application logs for errors
journalctl -u event-ticketing-api -f | grep "audit"
```

---

## Performance Considerations

### Audit Logs

- **Index on timestamp**: Already created for fast queries
- **Index on entity_id**: For entity-specific queries
- **Archive old logs**: Move logs older than 2 years to cold storage
- **Pagination**: Always use pagination for large result sets

### Webhook Retry

- **Rate limit**: 5 max attempts prevents infinite loops
- **Background job**: Consider moving to queue for large retry batches

### Transaction Retry

- **Gateway API calls**: Each retry calls gateway API (rate limits apply)
- **Batch retries**: Avoid retrying 1000s of transactions at once

### Re-encryption

- **CPU intensive**: AES-256 encryption takes CPU time
- **Run during low traffic**: Schedule re-encryption during maintenance window
- **Progress tracking**: Consider adding progress callback for large datasets

---

## Documentation References

1. **[ENCRYPTION_SETUP.md](./ENCRYPTION_SETUP.md)** - Complete encryption guide
2. **[PAYMENT_SYSTEM_OVERVIEW.md](./PAYMENT_SYSTEM_OVERVIEW.md)** - Full system architecture
3. **[DYNAMIC_GATEWAY_SYSTEM.md](./DYNAMIC_GATEWAY_SYSTEM.md)** - Gateway abstraction
4. **[PAYMENT_GATEWAY_ADMIN.md](./PAYMENT_GATEWAY_ADMIN.md)** - Admin management guide

---

## Summary

All 4 features are now **production-ready**:

✅ **Zero-Downtime Key Rotation** - Rotate encryption keys without service interruption
✅ **Audit Log Query API** - Full visibility into all payment operations  
✅ **Webhook Replay System** - Recover from failed webhook processing
✅ **Transaction Retry Mechanism** - Sync failed payments with gateway status

**Build Status:** ✅ Successful  
**Tests:** Ready for staging deployment  
**Documentation:** Complete
