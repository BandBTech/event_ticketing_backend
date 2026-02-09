# Credential Security Guide

## Overview

This document outlines the security architecture for storing and managing credentials in the event ticketing system.

## Security Architecture

### Hybrid Approach (Industry Standard)

```
┌─────────────────────────────────────────────────────────────┐
│                    CREDENTIAL STORAGE                        │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  .env Files                    Database (Encrypted)         │
│  ├─ DB_PASSWORD               ├─ Stripe API Key             │
│  ├─ REDIS_PASSWORD            ├─ PayPal Secret              │
│  ├─ JWT_SECRET                ├─ eSewa Merchant Code        │
│  ├─ ENCRYPTION_KEY ⚡         ├─ Khalti Public Key          │
│  └─ APP_SECRET_KEY            └─ Gateway Webhook Secrets    │
│                                                              │
│  ⚡ Encryption key encrypts database credentials            │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

### Security Layers

1. **Infrastructure Secrets** → `.env` (file system permissions)
2. **Dynamic Secrets** → Database (AES-256-GCM encryption)
3. **Encryption Keys** → `.env` or AWS Secrets Manager
4. **Backups** → Encrypted with separate keys
5. **Transit** → TLS/HTTPS only

---

## Why This Approach?

### ✅ Infrastructure in .env

**Example: Database Password**

```bash
# .env
DB_PASSWORD=super_secure_password_123
```

**Reasons:**

- ✅ Needed to start the application (bootstrap)
- ✅ Changes rarely (maybe once per year)
- ✅ Managed by DevOps/SRE team
- ✅ Easy to manage with Docker secrets, K8s secrets
- ✅ No circular dependency

**Security:**

```bash
# File permissions (owner only)
chmod 600 .env
chown app:app .env

# Never commit to git
echo ".env*" >> .gitignore
```

### ✅ Payment Gateways in Database (Encrypted)

**Example: Stripe API Key**

```go
// Stored in database
type PaymentGatewayConfig struct {
    APIKeyEncrypted string  // Encrypted value
    // NOT: APIKey string   // ❌ Never plaintext
}
```

**Reasons:**

- ✅ Admins can add/update via admin panel
- ✅ Changes frequently (test → production keys)
- ✅ Multiple gateways per environment
- ✅ Audit trail (who changed what, when)
- ✅ Easy rotation without redeployment
- ✅ Can disable gateway without deleting credentials

**Security:**

```go
// Encryption before storage
encrypted := encryptAES256GCM(apiKey, encryptionKey)
config.APIKeyEncrypted = encrypted

// Decryption when needed
apiKey := decryptAES256GCM(config.APIKeyEncrypted, encryptionKey)
gateway := NewStripeGateway(apiKey, ...)
```

---

## Implementation

### 1. Add Encryption Key to .env

```bash
# .env
# CRITICAL: This key encrypts all gateway credentials
# Generate: openssl rand -base64 32
CREDENTIAL_ENCRYPTION_KEY=YourBase64EncodedKey123456789ABCDEFG=

# Alternative: Use multiple keys for rotation
CREDENTIAL_ENCRYPTION_KEY_V1=OldKeyForExistingCreds=
CREDENTIAL_ENCRYPTION_KEY_V2=NewKeyForNewCreds=
CREDENTIAL_ENCRYPTION_KEY_CURRENT=V2
```

### 2. Encryption Service

Create `pkg/utils/encryption.go`:

```go
package utils

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
    "errors"
    "io"
)

// EncryptAES256GCM encrypts plaintext using AES-256-GCM
func EncryptAES256GCM(plaintext, key string) (string, error) {
    keyBytes := []byte(key)
    if len(keyBytes) != 32 {
        return "", errors.New("encryption key must be 32 bytes")
    }

    block, err := aes.NewCipher(keyBytes)
    if err != nil {
        return "", err
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", err
    }

    nonce := make([]byte, gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", err
    }

    ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
    return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptAES256GCM decrypts ciphertext using AES-256-GCM
func DecryptAES256GCM(ciphertext, key string) (string, error) {
    keyBytes := []byte(key)
    if len(keyBytes) != 32 {
        return "", errors.New("encryption key must be 32 bytes")
    }

    data, err := base64.StdEncoding.DecodeString(ciphertext)
    if err != nil {
        return "", err
    }

    block, err := aes.NewCipher(keyBytes)
    if err != nil {
        return "", err
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return "", err
    }

    nonceSize := gcm.NonceSize()
    if len(data) < nonceSize {
        return "", errors.New("ciphertext too short")
    }

    nonce, ciphertext := data[:nonceSize], data[nonceSize:]
    plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return "", err
    }

    return string(plaintext), nil
}
```

### 3. Update Config

```go
// pkg/config/config.go
type Config struct {
    // ... existing fields
    Security SecurityConfig
}

type SecurityConfig struct {
    EncryptionKey string
    EncryptionKeyVersion string // For key rotation
}

// In Load()
Security: SecurityConfig{
    EncryptionKey: getEnv("CREDENTIAL_ENCRYPTION_KEY", ""),
    EncryptionKeyVersion: getEnv("CREDENTIAL_ENCRYPTION_KEY_CURRENT", "V1"),
}
```

### 4. Update Payment Service

```go
// internal/services/payment_service.go
import "event-ticketing-backend/pkg/utils"

func (s *PaymentService) CreateGatewayConfig(
    ctx context.Context,
    req *models.PaymentGatewayConfig,
    adminID uuid.UUID,
) (*models.PaymentGatewayConfig, error) {
    // Encrypt credentials before saving
    encryptionKey := s.cfg.Security.EncryptionKey

    if req.APIKeyEncrypted != "" {
        encrypted, err := utils.EncryptAES256GCM(req.APIKeyEncrypted, encryptionKey)
        if err != nil {
            return nil, fmt.Errorf("failed to encrypt API key: %w", err)
        }
        req.APIKeyEncrypted = encrypted
    }

    if req.APISecretEncrypted != "" {
        encrypted, err := utils.EncryptAES256GCM(req.APISecretEncrypted, encryptionKey)
        if err != nil {
            return nil, fmt.Errorf("failed to encrypt API secret: %w", err)
        }
        req.APISecretEncrypted = encrypted
    }

    if req.WebhookSecretEncrypted != "" {
        encrypted, err := utils.EncryptAES256GCM(req.WebhookSecretEncrypted, encryptionKey)
        if err != nil {
            return nil, fmt.Errorf("failed to encrypt webhook secret: %w", err)
        }
        req.WebhookSecretEncrypted = encrypted
    }

    // Save to database
    if err := s.db.Create(req).Error; err != nil {
        return nil, err
    }

    // Audit log
    s.createAuditLog(adminID, "CREATE_GATEWAY", req.GatewayName)

    return req, nil
}
```

### 5. Update Gateway Factory

```go
// internal/gateways/factory.go
import "event-ticketing-backend/pkg/utils"

func (f *Factory) InitializeGatewaysFromDB(ctx context.Context) error {
    var configs []models.PaymentGatewayConfig
    if err := f.db.Where("is_enabled = ?", true).Find(&configs).Error; err != nil {
        return fmt.Errorf("failed to load gateway configs: %w", err)
    }

    encryptionKey := f.cfg.Security.EncryptionKey

    for _, config := range configs {
        // Decrypt credentials
        apiKey, err := utils.DecryptAES256GCM(config.APIKeyEncrypted, encryptionKey)
        if err != nil {
            fmt.Printf("Error decrypting API key for %s: %v\n", config.GatewayName, err)
            continue
        }

        apiSecret, _ := utils.DecryptAES256GCM(config.APISecretEncrypted, encryptionKey)
        webhookSecret, _ := utils.DecryptAES256GCM(config.WebhookSecretEncrypted, encryptionKey)

        // Get initializer
        initializer, exists := gatewayInitializers[config.GatewayName]
        if !exists {
            fmt.Printf("Warning: Gateway '%s' not implemented\n", config.GatewayName)
            continue
        }

        // Create config with decrypted values
        decryptedConfig := config
        decryptedConfig.APIKeyEncrypted = apiKey
        decryptedConfig.APISecretEncrypted = apiSecret
        decryptedConfig.WebhookSecretEncrypted = webhookSecret

        // Initialize gateway
        gateway, err := initializer(&decryptedConfig)
        if err != nil {
            fmt.Printf("Error initializing gateway '%s': %v\n", config.GatewayName, err)
            continue
        }

        f.RegisterGateway(config.GatewayName, gateway)
        fmt.Printf("✓ Registered gateway: %s (test_mode=%v)\n", config.GatewayName, config.IsTestMode)
    }

    return nil
}
```

---

## Security Checklist

### ✅ Development Environment

```bash
# .env.local
CREDENTIAL_ENCRYPTION_KEY=DevKey123ForLocalTestingOnly==

# File permissions
chmod 600 .env.local
```

### ✅ Production Environment

```bash
# Using AWS Secrets Manager (RECOMMENDED)
aws secretsmanager get-secret-value \
    --secret-id prod/event-ticketing/encryption-key \
    --query SecretString --output text

# Or secure .env with strict permissions
chmod 400 .env.production
chown app:app .env.production

# Docker secrets (better)
docker secret create encryption_key encryption_key.txt
```

### ✅ Key Rotation Strategy

**Step 1: Add new key**

```bash
CREDENTIAL_ENCRYPTION_KEY_V1=OldKey==
CREDENTIAL_ENCRYPTION_KEY_V2=NewKey==
CREDENTIAL_ENCRYPTION_KEY_CURRENT=V2
```

**Step 2: Re-encrypt existing credentials**

```go
func (s *PaymentService) RotateEncryptionKey() error {
    oldKey := s.cfg.Security.EncryptionKeyV1
    newKey := s.cfg.Security.EncryptionKeyV2

    var configs []models.PaymentGatewayConfig
    s.db.Find(&configs)

    for _, config := range configs {
        // Decrypt with old key
        apiKey, _ := utils.DecryptAES256GCM(config.APIKeyEncrypted, oldKey)

        // Encrypt with new key
        encrypted, _ := utils.EncryptAES256GCM(apiKey, newKey)

        // Update database
        s.db.Model(&config).Update("api_key_encrypted", encrypted)
    }

    return nil
}
```

**Step 3: Remove old key** (after all credentials re-encrypted)

### ✅ Database Backup Security

```bash
# Encrypt backups
pg_dump event_ticketing | \
    openssl enc -aes-256-cbc -salt -pbkdf2 \
    -out backup_$(date +%Y%m%d).sql.enc

# Store encryption passphrase separately
# (AWS Secrets Manager, 1Password, etc.)
```

### ✅ Access Control

```sql
-- Database: Restrict access to credentials table
REVOKE ALL ON payment_gateway_configs FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON payment_gateway_configs TO app_user;
GRANT SELECT ON payment_gateway_configs TO readonly_user;

-- Admin Panel: Only users with manage:payment_gateway permission
-- Already implemented in middleware
```

### ✅ Audit Logging

```go
// Log all credential access (not the values!)
type CredentialAuditLog struct {
    ID          uuid.UUID
    AdminID     uuid.UUID
    Action      string  // CREATE, UPDATE, DELETE, VIEW, ROTATE
    GatewayName string
    IPAddress   string
    UserAgent   string
    Timestamp   time.Time
}

// Log on every credential operation
s.auditLog.Create(&CredentialAuditLog{
    AdminID:     adminID,
    Action:      "UPDATE_GATEWAY_CREDENTIALS",
    GatewayName: config.GatewayName,
    IPAddress:   c.ClientIP(),
    Timestamp:   time.Now(),
})
```

---

## Production Deployment

### Option 1: Environment Variables (Simple)

```bash
# .env.production
CREDENTIAL_ENCRYPTION_KEY=<32-byte-base64-key>

# Docker Compose
docker-compose --env-file .env.production up -d

# Kubernetes Secret
kubectl create secret generic app-secrets \
    --from-env-file=.env.production
```

### Option 2: AWS Secrets Manager (Recommended)

```go
// pkg/config/config.go
import "github.com/aws/aws-sdk-go/service/secretsmanager"

func getEncryptionKeyFromAWS() string {
    svc := secretsmanager.New(session.New())

    result, err := svc.GetSecretValue(&secretsmanager.GetSecretValueInput{
        SecretId: aws.String("prod/event-ticketing/encryption-key"),
    })
    if err != nil {
        log.Fatal("Failed to retrieve encryption key from AWS Secrets Manager")
    }

    return *result.SecretString
}

// In Load()
Security: SecurityConfig{
    EncryptionKey: getEncryptionKeyFromAWS(),
}
```

### Option 3: HashiCorp Vault (Enterprise)

```go
import "github.com/hashicorp/vault/api"

func getEncryptionKeyFromVault() string {
    client, _ := api.NewClient(api.DefaultConfig())
    client.SetToken(os.Getenv("VAULT_TOKEN"))

    secret, err := client.Logical().Read("secret/data/event-ticketing/encryption-key")
    if err != nil {
        log.Fatal("Failed to retrieve encryption key from Vault")
    }

    return secret.Data["value"].(string)
}
```

---

## Migration Plan

### Phase 1: Add Encryption (Current)

1. ✅ Add `CREDENTIAL_ENCRYPTION_KEY` to `.env`
2. ✅ Implement encryption utilities
3. ✅ Update `CreateGatewayConfig` to encrypt before save
4. ✅ Update `InitializeGatewaysFromDB` to decrypt on load

### Phase 2: Migrate Existing Credentials

```bash
# One-time migration script
go run scripts/migrate_encrypt_credentials.go
```

```go
// scripts/migrate_encrypt_credentials.go
func main() {
    cfg, _ := config.Load()
    db := database.GetDB()

    var configs []models.PaymentGatewayConfig
    db.Find(&configs)

    for _, config := range configs {
        // If already encrypted, skip
        if isEncrypted(config.APIKeyEncrypted) {
            continue
        }

        // Encrypt plaintext credentials
        encrypted, _ := utils.EncryptAES256GCM(config.APIKeyEncrypted, cfg.Security.EncryptionKey)

        // Update database
        db.Model(&config).Update("api_key_encrypted", encrypted)

        fmt.Printf("Encrypted credentials for %s\n", config.GatewayName)
    }
}
```

### Phase 3: AWS Secrets Manager (Production)

1. Store encryption key in AWS Secrets Manager
2. Update `config.Load()` to fetch from AWS
3. Remove `CREDENTIAL_ENCRYPTION_KEY` from `.env`

### Phase 4: Key Rotation (Yearly)

1. Generate new encryption key
2. Run re-encryption script
3. Update AWS Secrets Manager
4. Deploy new version

---

## Security Comparison Table

| Aspect                 | .env Files          | Database (Encrypted) | AWS Secrets Manager   |
| ---------------------- | ------------------- | -------------------- | --------------------- |
| **Encryption at Rest** | ❌ Plaintext        | ✅ AES-256-GCM       | ✅ AWS KMS            |
| **Access Control**     | ⚠️ File permissions | ✅ Database roles    | ✅ IAM policies       |
| **Audit Logging**      | ❌ No               | ✅ Application logs  | ✅ CloudTrail         |
| **Rotation**           | ❌ Manual redeploy  | ✅ Via admin panel   | ✅ Automatic          |
| **Cost**               | ✅ Free             | ✅ Free              | ⚠️ $0.40/secret/month |
| **Complexity**         | ✅ Simple           | ⚠️ Medium            | ⚠️ Medium             |
| **Bootstrap**          | ✅ No dependency    | ❌ Needs DB          | ⚠️ Needs AWS creds    |
| **Best For**           | Infrastructure      | Dynamic configs      | Production secrets    |

---

## Recommendations

### For Your System

**Infrastructure Secrets (.env):**

```bash
DB_PASSWORD=<secure-password>
REDIS_PASSWORD=<secure-password>
JWT_SECRET=<secure-random-string>
CREDENTIAL_ENCRYPTION_KEY=<32-byte-base64>  # ⚡ Master key
```

**Payment Gateway Credentials (Database):**

- Stripe API keys
- PayPal secrets
- eSewa merchant codes
- Khalti public keys
- Webhook secrets

**Why?**

1. ✅ Admins can manage gateways via UI
2. ✅ Easy to rotate without redeployment
3. ✅ Audit trail of changes
4. ✅ Encrypted with master key from .env
5. ✅ No circular dependency

### Security Enhancements

**Immediate (Now):**

1. Generate encryption key: `openssl rand -base64 32`
2. Add to `.env`: `CREDENTIAL_ENCRYPTION_KEY=...`
3. Implement encryption utilities (code above)
4. Update gateway creation/loading to use encryption

**Short-term (1-2 weeks):**

1. Add credential audit logging
2. Implement key rotation mechanism
3. Add integration tests for encryption
4. Document key management for team

**Long-term (Production):**

1. Migrate to AWS Secrets Manager for encryption key
2. Implement automatic key rotation
3. Add encryption key backup strategy
4. Set up monitoring for decryption failures

---

## Conclusion

**Answer: Both are secure IF implemented correctly**

**Current Best Practice:**

```
.env (Infrastructure) + Database (Dynamic) + Encryption + AWS Secrets Manager
```

**For your payment gateway admin system:**

- ✅ Store gateway credentials in **database (encrypted)**
- ✅ Store encryption key in **.env** (or AWS Secrets Manager for production)
- ✅ Use **AES-256-GCM** encryption
- ✅ Implement **audit logging**
- ✅ Plan for **key rotation**

This gives you the best of both worlds:

- Easy admin panel management
- Secure encryption
- Production-ready architecture
- Industry-standard security

**Next Steps:**

1. Implement encryption utilities (provided above)
2. Add encryption key to `.env`
3. Update payment service to encrypt/decrypt
4. Test thoroughly in development
5. Deploy to production with AWS Secrets Manager
