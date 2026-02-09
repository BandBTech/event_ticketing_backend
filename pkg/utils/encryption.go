package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// EncryptAES256GCM encrypts plaintext using AES-256-GCM
// Returns base64 encoded ciphertext
func EncryptAES256GCM(plaintext, key string) (string, error) {
	if plaintext == "" {
		return "", nil // Empty string remains empty
	}

	keyBytes := []byte(key)
	if len(keyBytes) != 32 {
		return "", errors.New("encryption key must be 32 bytes (use: openssl rand -base64 32)")
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// Create a nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// Encrypt and prepend nonce
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptAES256GCM decrypts base64 encoded ciphertext using AES-256-GCM
// Returns plaintext
func DecryptAES256GCM(ciphertext, key string) (string, error) {
	if ciphertext == "" {
		return "", nil // Empty string remains empty
	}

	keyBytes := []byte(key)
	if len(keyBytes) != 32 {
		return "", errors.New("encryption key must be 32 bytes")
	}

	// Decode base64
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

	// Extract nonce and ciphertext
	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// EncryptAES256GCMWithID encrypts plaintext with key ID prefix for rotation support
// Returns: "v1:base64_encrypted_data"
func EncryptAES256GCMWithID(plaintext, key, keyID string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	encrypted, err := EncryptAES256GCM(plaintext, key)
	if err != nil {
		return "", err
	}

	// Prefix with key ID for rotation tracking
	return keyID + ":" + encrypted, nil
}

// DecryptAES256GCMWithRotation attempts decryption with primary key and falls back to rotation keys
// Supports both new format (keyID:data) and legacy format (data)
func DecryptAES256GCMWithRotation(ciphertext, primaryKey string, rotationKeys []string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	// Check if ciphertext has key ID prefix (new format: "v1:encrypted_data")
	parts := strings.SplitN(ciphertext, ":", 2)

	var encryptedData string
	if len(parts) == 2 {
		// New format with key ID
		encryptedData = parts[1]
	} else {
		// Legacy format without key ID
		encryptedData = ciphertext
	}

	// Try primary key first
	plaintext, err := DecryptAES256GCM(encryptedData, primaryKey)
	if err == nil {
		return plaintext, nil
	}

	// Try rotation keys
	for i, oldKey := range rotationKeys {
		if oldKey == "" {
			continue
		}
		plaintext, err := DecryptAES256GCM(encryptedData, oldKey)
		if err == nil {
			return plaintext, nil
		}
		// Log rotation key attempt (optional)
		_ = i // Use index for logging if needed
	}

	return "", fmt.Errorf("decryption failed with all available keys (tried %d keys)", len(rotationKeys)+1)
}
