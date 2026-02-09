#!/bin/bash

# Generate Encryption Key Script
# This script generates a secure 32-byte encryption key for credential encryption

echo "🔐 Generating Encryption Key for Payment Gateway Credentials"
echo "============================================================"
echo ""

# Check if openssl is available
if ! command -v openssl &> /dev/null; then
    echo "❌ Error: openssl is not installed"
    echo "   Install it first: brew install openssl (macOS) or apt-get install openssl (Linux)"
    exit 1
fi

# Generate the key
ENCRYPTION_KEY=$(openssl rand -base64 32)

echo "✅ Generated Encryption Key:"
echo ""
echo "CREDENTIAL_ENCRYPTION_KEY=$ENCRYPTION_KEY"
echo ""
echo "============================================================"
echo "📋 Next Steps:"
echo ""
echo "1. Add this key to your .env file:"
echo "   echo 'CREDENTIAL_ENCRYPTION_KEY=$ENCRYPTION_KEY' >> .env"
echo ""
echo "2. IMPORTANT: Keep this key secret!"
echo "   - Never commit to git"
echo "   - Back it up securely (password manager, AWS Secrets Manager, etc.)"
echo "   - Use different keys for dev/staging/production"
echo ""
echo "3. If you lose this key, you'll need to:"
echo "   - Generate a new key"
echo "   - Re-enter all payment gateway credentials via admin panel"
echo ""
echo "🔒 Security Tips:"
echo "   - Store in AWS Secrets Manager for production"
echo "   - Rotate keys yearly"
echo "   - Never share via email/chat"
echo "============================================================"

# Optionally add to .env file
read -p "Do you want to add this to your .env file now? (y/N) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    if [ -f .env ]; then
        # Check if key already exists
        if grep -q "CREDENTIAL_ENCRYPTION_KEY=" .env; then
            echo "⚠️  CREDENTIAL_ENCRYPTION_KEY already exists in .env"
            read -p "Do you want to REPLACE it? This will break existing encrypted credentials! (y/N) " -n 1 -r
            echo
            if [[ $REPLY =~ ^[Yy]$ ]]; then
                # Replace existing key
                if [[ "$OSTYPE" == "darwin"* ]]; then
                    # macOS
                    sed -i '' "s|^CREDENTIAL_ENCRYPTION_KEY=.*|CREDENTIAL_ENCRYPTION_KEY=$ENCRYPTION_KEY|" .env
                else
                    # Linux
                    sed -i "s|^CREDENTIAL_ENCRYPTION_KEY=.*|CREDENTIAL_ENCRYPTION_KEY=$ENCRYPTION_KEY|" .env
                fi
                echo "✅ Updated CREDENTIAL_ENCRYPTION_KEY in .env"
                echo "⚠️  WARNING: Existing encrypted credentials will no longer work!"
                echo "   You need to re-enter all payment gateway credentials."
            else
                echo "❌ Cancelled. Keeping existing key."
            fi
        else
            # Add new key
            echo "" >> .env
            echo "# Credential Encryption" >> .env
            echo "CREDENTIAL_ENCRYPTION_KEY=$ENCRYPTION_KEY" >> .env
            echo "✅ Added CREDENTIAL_ENCRYPTION_KEY to .env"
        fi
    else
        echo "❌ .env file not found. Please create it first or run from project root."
        exit 1
    fi
else
    echo "⏭️  Skipped. Copy the key manually to your .env file."
fi

echo ""
echo "✅ Done! Your encryption key is ready to use."
