# Encryption Setup Guide

## Quick Start

### 1. Generate Encryption Key

Run the automated script:

```bash
./scripts/generate-encryption-key.sh
```

Or manually:

```bash
# Generate a 32-byte key
openssl rand -base64 32

# Add to .env
echo "CREDENTIAL_ENCRYPTION_KEY=<your-generated-key>" >> .env
```

### 2. Verify Configuration

Check your `.env` file contains:

```bash
CREDENTIAL_ENCRYPTION_KEY=YourBase64Key12345678901234567890AB==
```

### 3. Test Encryption

Create a test gateway via admin panel:

```bash
curl -X POST http://localhost:8082/api/v1/admin/payment-gateways \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "gateway_name": "stripe",
    "display_name": "Stripe",
    "is_enabled": false,
    "is_test_mode": true,
    "api_key_encrypted": "pk_test_your_test_key",
    "api_secret_encrypted": "sk_test_your_test_secret"
  }'
```

The API will automatically encrypt these credentials before saving to database.

### 4. Verify Encryption

Check database - credentials should be encrypted:

```sql
SELECT gateway_name,
       LEFT(api_key_encrypted, 20) || '...' as encrypted_key
FROM payment_gateway_configs;
```

You should see base64 encrypted values, NOT plaintext keys.

---

## How It Works

### Encryption Flow

```
Admin Panel → API (CreateGatewayConfig)
                ↓
    Encrypt with master key (AES-256-GCM)
                ↓
         Save to Database
         (encrypted values)
```

### Decryption Flow

```
Server Startup → InitializeGatewaysFromDB
                      ↓
         Load from Database
         (encrypted values)
                      ↓
    Decrypt with master key
                      ↓
    Create Gateway Instances
    (use plaintext in memory only)
```

### Security Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    CREDENTIAL FLOW                           │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  Admin enters:              Stored in DB:                   │
│  "sk_test_abc123"    →    "eJ8k3mN...encrypted"            │
│  (plaintext)               (AES-256-GCM encrypted)          │
│                                                              │
│  Encryption Key:                                            │
│  .env file → CREDENTIAL_ENCRYPTION_KEY                      │
│                                                              │
│  Used at runtime:                                           │
│  Gateway initialization → decrypt → use in memory           │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

---

## API Examples

### Create Gateway (Auto-Encrypts)

```bash
POST /api/v1/admin/payment-gateways
Content-Type: application/json
Authorization: Bearer <admin_token>

{
  "gateway_name": "stripe",
  "display_name": "Stripe Payment Gateway",
  "is_enabled": false,
  "is_test_mode": true,
  "priority": 1,
  "api_key_encrypted": "pk_test_51234567890",     ← Plaintext input
  "api_secret_encrypted": "sk_test_abcdefg",      ← Plaintext input
  "webhook_secret_encrypted": "whsec_xyz123",     ← Plaintext input
  "percentage_fee": 2.9,
  "fixed_fee": 0.30
}
```

**What happens:**

1. API receives plaintext credentials
2. Encrypts each credential with `CREDENTIAL_ENCRYPTION_KEY`
3. Stores encrypted values in database
4. Returns success (no credentials in response)

### Reload Gateways (Auto-Decrypts)

```bash
PATCH /api/v1/admin/payment-gateways/all?action=reload
Authorization: Bearer <admin_token>
```

**What happens:**

1. Loads all enabled gateway configs from database (encrypted)
2. Decrypts credentials using `CREDENTIAL_ENCRYPTION_KEY`
3. Creates gateway instances with decrypted credentials
4. Registers gateways in factory (ready for payments)

---

## Configuration

### Environment Variables

```bash
# Required: Master encryption key (32 bytes, base64 encoded)
CREDENTIAL_ENCRYPTION_KEY=<generate-with-openssl-rand-base64-32>

# Optional: Encryption algorithm (default: AES-256-GCM)
# No need to set this, already implemented
```

### Encryption Strength

- **Algorithm:** AES-256-GCM (Authenticated Encryption)
- **Key Size:** 256 bits (32 bytes)
- **Mode:** Galois/Counter Mode (prevents tampering)
- **IV/Nonce:** Random 12 bytes per encryption
- **Output:** Base64 encoded (safe for database storage)

---

## Security Best Practices

### ✅ DO

- **Generate Strong Key:**

  ```bash
  openssl rand -base64 32
  ```

- **Different Keys Per Environment:**

  ```bash
  # .env.local
  CREDENTIAL_ENCRYPTION_KEY=DevKey123...

  # .env.production
  CREDENTIAL_ENCRYPTION_KEY=ProdKey456...
  ```

- **Backup the Key Securely:**
  - Password manager (1Password, LastPass)
  - AWS Secrets Manager
  - Encrypted vault

- **File Permissions:**

  ```bash
  chmod 600 .env
  chown app:app .env
  ```

- **Never Commit:**
  ```bash
  # .gitignore
  .env
  .env.local
  .env.production
  ```

### ❌ DON'T

- **Don't use weak keys:**

  ```bash
  # ❌ Too short
  CREDENTIAL_ENCRYPTION_KEY=abc123

  # ❌ Not random
  CREDENTIAL_ENCRYPTION_KEY=my-secret-key-2024

  # ✅ Proper key
  CREDENTIAL_ENCRYPTION_KEY=$(openssl rand -base64 32)
  ```

- **Don't share the key via:**
  - Email
  - Slack/Discord
  - SMS
  - Public repos

- **Don't use same key for all environments**

- **Don't store key in database** (chicken-and-egg problem)

---

## Key Rotation

### When to Rotate

- **Annually:** Best practice (every 365 days)
- **After breach:** Immediately if key is compromised
- **After personnel changes:** When admins with key access leave

### How to Rotate

**Step 1: Generate New Key**

```bash
openssl rand -base64 32
# Save as CREDENTIAL_ENCRYPTION_KEY_NEW
```

**Step 2: Add to .env**

```bash
CREDENTIAL_ENCRYPTION_KEY=OldKey123...
CREDENTIAL_ENCRYPTION_KEY_NEW=NewKey456...
```

**Step 3: Re-encrypt All Credentials**

Create a migration script:

```go
// scripts/rotate-encryption-key.go
func main() {
    cfg, _ := config.Load()
    db := database.GetDB()

    oldKey := cfg.Security.EncryptionKey
    newKey := os.Getenv("CREDENTIAL_ENCRYPTION_KEY_NEW")

    var configs []models.PaymentGatewayConfig
    db.Find(&configs)

    for _, config := range configs {
        // Decrypt with old key
        apiKey, _ := utils.DecryptAES256GCM(config.APIKeyEncrypted, oldKey)
        apiSecret, _ := utils.DecryptAES256GCM(config.APISecretEncrypted, oldKey)
        webhookSecret, _ := utils.DecryptAES256GCM(config.WebhookSecretEncrypted, oldKey)

        // Re-encrypt with new key
        config.APIKeyEncrypted, _ = utils.EncryptAES256GCM(apiKey, newKey)
        config.APISecretEncrypted, _ = utils.EncryptAES256GCM(apiSecret, newKey)
        config.WebhookSecretEncrypted, _ = utils.EncryptAES256GCM(webhookSecret, newKey)

        // Update database
        db.Save(&config)
        fmt.Printf("✓ Re-encrypted %s\n", config.GatewayName)
    }
}
```

**Step 4: Run Migration**

```bash
go run scripts/rotate-encryption-key.go
```

**Step 5: Update Production**

```bash
# Replace old key with new key
CREDENTIAL_ENCRYPTION_KEY=NewKey456...
# Remove CREDENTIAL_ENCRYPTION_KEY_NEW
```

**Step 6: Restart Server**

```bash
docker-compose restart api
```

---

## Troubleshooting

### Error: "CREDENTIAL_ENCRYPTION_KEY not configured"

**Cause:** Missing encryption key in .env

**Solution:**

```bash
./scripts/generate-encryption-key.sh
# Or manually:
echo "CREDENTIAL_ENCRYPTION_KEY=$(openssl rand -base64 32)" >> .env
```

### Error: "encryption key must be 32 bytes"

**Cause:** Invalid key length

**Solution:**

```bash
# Must be exactly 32 bytes (base64 encoded)
openssl rand -base64 32
```

### Error: "Error decrypting API key for stripe"

**Possible Causes:**

1. **Wrong encryption key** - Key changed after credentials were encrypted
2. **Corrupted data** - Database value corrupted
3. **Not encrypted** - Credentials stored as plaintext

**Solution:**

```bash
# 1. Check if key is correct
echo $CREDENTIAL_ENCRYPTION_KEY

# 2. Re-enter credentials via admin panel
# Delete and recreate the gateway configuration

# 3. Check database
psql event_ticketing -c "SELECT gateway_name, api_key_encrypted FROM payment_gateway_configs;"
```

### Credentials Not Working

**Check:**

1. **Encryption key set?**

   ```bash
   grep CREDENTIAL_ENCRYPTION_KEY .env
   ```

2. **Gateway enabled?**

   ```bash
   curl http://localhost:8082/api/v1/admin/payment-gateways
   # Check is_enabled: true
   ```

3. **Gateways loaded?**

   ```bash
   # Check server logs for:
   # ✓ Registered gateway: stripe (test_mode=true)
   ```

4. **Test credentials valid?**
   ```bash
   # Stripe test key format:
   # pk_test_... (publishable)
   # sk_test_... (secret)
   ```

---

## Production Deployment

### AWS Secrets Manager (Recommended)

**Store encryption key in AWS:**

```bash
# Create secret
aws secretsmanager create-secret \
    --name prod/event-ticketing/encryption-key \
    --secret-string "$(openssl rand -base64 32)"

# Retrieve at runtime
aws secretsmanager get-secret-value \
    --secret-id prod/event-ticketing/encryption-key \
    --query SecretString --output text
```

**Update config.go:**

```go
import "github.com/aws/aws-sdk-go/service/secretsmanager"

func getEncryptionKey() string {
    if os.Getenv("APP_ENV") == "production" {
        // Fetch from AWS Secrets Manager
        svc := secretsmanager.New(session.New())
        result, err := svc.GetSecretValue(&secretsmanager.GetSecretValueInput{
            SecretId: aws.String("prod/event-ticketing/encryption-key"),
        })
        if err != nil {
            log.Fatal("Failed to retrieve encryption key from AWS")
        }
        return *result.SecretString
    }
    // Development: from .env
    return os.Getenv("CREDENTIAL_ENCRYPTION_KEY")
}
```

### Docker Secrets

```bash
# Create secret file
echo "$(openssl rand -base64 32)" > encryption_key.txt

# Docker Compose
docker secret create encryption_key encryption_key.txt

# In docker-compose.yml
services:
  api:
    secrets:
      - encryption_key
    environment:
      CREDENTIAL_ENCRYPTION_KEY_FILE: /run/secrets/encryption_key

secrets:
  encryption_key:
    external: true
```

### Kubernetes Secrets

```bash
# Create secret
kubectl create secret generic app-secrets \
    --from-literal=encryption-key=$(openssl rand -base64 32)

# In deployment.yaml
env:
  - name: CREDENTIAL_ENCRYPTION_KEY
    valueFrom:
      secretKeyRef:
        name: app-secrets
        key: encryption-key
```

---

## Testing

### Test Encryption Utility

```go
// Test in Go
package utils_test

import "testing"

func TestEncryptDecrypt(t *testing.T) {
    key := "12345678901234567890123456789012" // 32 bytes
    plaintext := "sk_test_abc123"

    // Encrypt
    encrypted, err := EncryptAES256GCM(plaintext, key)
    if err != nil {
        t.Fatal(err)
    }

    // Decrypt
    decrypted, err := DecryptAES256GCM(encrypted, key)
    if err != nil {
        t.Fatal(err)
    }

    if decrypted != plaintext {
        t.Errorf("Expected %s, got %s", plaintext, decrypted)
    }
}
```

### Integration Test

```bash
# 1. Create gateway
curl -X POST http://localhost:8082/api/v1/admin/payment-gateways \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"gateway_name":"stripe","api_key_encrypted":"pk_test_123"}'

# 2. Check database (should be encrypted)
psql -c "SELECT api_key_encrypted FROM payment_gateway_configs WHERE gateway_name='stripe';"
# Should NOT show: pk_test_123

# 3. Reload gateways
curl -X PATCH http://localhost:8082/api/v1/admin/payment-gateways/all?action=reload

# 4. Check logs
# Should see: ✓ Registered gateway: stripe (test_mode=true)
```

---

## Summary

✅ **Master key in .env** - Simple, secure, industry standard  
✅ **AES-256-GCM encryption** - Strong authenticated encryption  
✅ **Auto-encrypt on save** - Transparent to admin users  
✅ **Auto-decrypt on load** - Transparent to system  
✅ **No plaintext storage** - All credentials encrypted at rest  
✅ **Production ready** - Works with AWS Secrets Manager

**Your system is now secure!** 🔐

Gateway credentials are encrypted before database storage and decrypted only when needed in memory. The master key in .env encrypts/decrypts everything.
