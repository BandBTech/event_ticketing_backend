package gateways

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"gorm.io/gorm"
)

// Factory manages payment gateway instances and selection logic
type Factory struct {
	db       *gorm.DB
	gateways map[string]PaymentGateway
	mu       sync.RWMutex
	cfg      *config.Config
}

// NewFactory creates a new gateway factory
func NewFactory(db *gorm.DB, cfg *config.Config) *Factory {
	return &Factory{
		db:       db,
		gateways: make(map[string]PaymentGateway),
		cfg:      cfg,
	}
}

// RegisterGateway registers a payment gateway instance
func (f *Factory) RegisterGateway(name string, gateway PaymentGateway) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gateways[name] = gateway
}

// GetGateway retrieves a gateway by name
func (f *Factory) GetGateway(name string) (PaymentGateway, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	gateway, exists := f.gateways[name]
	if !exists {
		return nil, fmt.Errorf("gateway not found: %s", name)
	}
	return gateway, nil
}

// SelectGateway automatically selects the best payment gateway based on criteria
func (f *Factory) SelectGateway(ctx context.Context, criteria *GatewaySelectionCriteria) (PaymentGateway, *models.PaymentGatewayConfig, error) {
	// Get enabled gateways from database that support the country and currency
	var configs []models.PaymentGatewayConfig
	query := f.db.Where("is_enabled = ?", true)

	// Filter by country if specified
	if criteria.Country != "" {
		query = query.Where("? = ANY(supported_countries)", criteria.Country)
	}

	// Filter by currency if specified
	if criteria.Currency != "" {
		query = query.Where("? = ANY(supported_currencies)", criteria.Currency)
	}

	// Filter by amount limits
	if criteria.Amount > 0 {
		query = query.Where("(min_amount IS NULL OR min_amount <= ?) AND (max_amount IS NULL OR max_amount >= ?)",
			criteria.Amount, criteria.Amount)
	}

	// Order by priority (lower number = higher priority)
	if err := query.Order("priority ASC").Find(&configs).Error; err != nil {
		return nil, nil, fmt.Errorf("failed to query gateway configs: %w", err)
	}

	if len(configs) == 0 {
		// Try to fallback to Stripe if it's registered and enabled
		gateway, err := f.GetGateway("stripe")
		if err != nil {
			return nil, nil, errors.New("no payment gateway available for the specified criteria")
		}

		// Check if Stripe supports the currency
		if !contains(gateway.GetSupportedCurrencies(), criteria.Currency) {
			return nil, nil, fmt.Errorf("no payment gateway supports currency: %s", criteria.Currency)
		}

		// Return Stripe as fallback
		var stripeConfig models.PaymentGatewayConfig
		if err := f.db.Where("gateway_name = ? AND is_enabled = ?", "stripe", true).First(&stripeConfig).Error; err != nil {
			return nil, nil, fmt.Errorf("stripe gateway not configured: %w", err)
		}

		return gateway, &stripeConfig, nil
	}

	// Try to get the gateway instance for the highest priority config
	gatewayConfig := configs[0]
	gateway, err := f.GetGateway(gatewayConfig.GatewayName)
	if err != nil {
		return nil, nil, fmt.Errorf("gateway %s is configured but not registered: %w", gatewayConfig.GatewayName, err)
	}

	return gateway, &gatewayConfig, nil
}

// GetAvailableGateways returns all gateways available for a country and currency
func (f *Factory) GetAvailableGateways(ctx context.Context, country, currency string) ([]GatewayInfo, error) {
	var configs []models.PaymentGatewayConfig
	query := f.db.Where("is_enabled = ?", true)

	if country != "" {
		query = query.Where("? = ANY(supported_countries)", country)
	}
	if currency != "" {
		query = query.Where("? = ANY(supported_currencies)", currency)
	}

	if err := query.Order("priority ASC").Find(&configs).Error; err != nil {
		return nil, err
	}

	var gateways []GatewayInfo
	for _, config := range configs {
		// Check if gateway is registered
		if _, err := f.GetGateway(config.GatewayName); err != nil {
			continue // Skip if not registered
		}

		gateways = append(gateways, GatewayInfo{
			Name:             config.GatewayName,
			DisplayName:      config.DisplayName,
			Priority:         config.Priority,
			PercentageFee:    config.PercentageFee,
			FixedFee:         config.FixedFee,
			SupportedMethods: []string{}, // Can be extended from config
		})
	}

	return gateways, nil
}

// GatewayInitializer is a function type that creates a gateway instance from config
type GatewayInitializer func(config *models.PaymentGatewayConfig) (PaymentGateway, error)

// gatewayInitializers stores the initialization functions for each gateway type
var gatewayInitializers = map[string]GatewayInitializer{
	"stripe": func(config *models.PaymentGatewayConfig) (PaymentGateway, error) {
		apiKey := decryptCredential(config.APIKeyEncrypted)
		webhookSecret := decryptCredential(config.WebhookSecretEncrypted)
		return NewStripeGateway(apiKey, webhookSecret, config.IsTestMode), nil
	},
	// Add more gateways here as they're implemented:
	// "paypal": func(config *models.PaymentGatewayConfig) (PaymentGateway, error) {
	//     apiKey := decryptCredential(config.APIKeyEncrypted)
	//     apiSecret := decryptCredential(config.APISecretEncrypted)
	//     webhookSecret := decryptCredential(config.WebhookSecretEncrypted)
	//     return NewPayPalGateway(apiKey, apiSecret, webhookSecret, config.IsTestMode), nil
	// },
}

// RegisterGatewayInitializer allows external packages to register new gateway types dynamically
func RegisterGatewayInitializer(gatewayType string, initializer GatewayInitializer) {
	gatewayInitializers[gatewayType] = initializer
}

// InitializeGatewaysFromDB loads all enabled gateways from database and initializes them dynamically
func (f *Factory) InitializeGatewaysFromDB(ctx context.Context) error {
	var configs []models.PaymentGatewayConfig
	if err := f.db.Where("is_enabled = ?", true).Find(&configs).Error; err != nil {
		return fmt.Errorf("failed to load gateway configs: %w", err)
	}

	// Check if encryption key is configured
	encryptionKey := ""
	if f.cfg != nil {
		encryptionKey = f.cfg.Security.EncryptionKey
	}

	for _, config := range configs {
		// Decrypt credentials if encryption key is available
		if encryptionKey != "" {
			rotationKeys := []string{}
			if f.cfg != nil {
				rotationKeys = f.cfg.Security.RotationKeys
			}

			// Decrypt API key with rotation support
			if config.APIKeyEncrypted != "" {
				decrypted, err := utils.DecryptAES256GCMWithRotation(
					config.APIKeyEncrypted,
					encryptionKey,
					rotationKeys,
				)
				if err != nil {
					fmt.Printf("Error decrypting API key for %s: %v\n", config.GatewayName, err)
					continue
				}
				config.APIKeyEncrypted = decrypted // Use decrypted value
			}

			// Decrypt API secret with rotation support
			if config.APISecretEncrypted != "" {
				decrypted, err := utils.DecryptAES256GCMWithRotation(
					config.APISecretEncrypted,
					encryptionKey,
					rotationKeys,
				)
				if err != nil {
					fmt.Printf("Error decrypting API secret for %s: %v\n", config.GatewayName, err)
					continue
				}
				config.APISecretEncrypted = decrypted
			}

			// Decrypt webhook secret with rotation support
			if config.WebhookSecretEncrypted != "" {
				decrypted, err := utils.DecryptAES256GCMWithRotation(
					config.WebhookSecretEncrypted,
					encryptionKey,
					rotationKeys,
				)
				if err != nil {
					fmt.Printf("Error decrypting webhook secret for %s: %v\n", config.GatewayName, err)
					continue
				}
				config.WebhookSecretEncrypted = decrypted
			}
		}

		// Get the initializer for this gateway type
		initializer, exists := gatewayInitializers[config.GatewayName]
		if !exists {
			// Gateway type not implemented yet - log and continue
			fmt.Printf("Warning: Gateway '%s' is not implemented yet. Add it to gatewayInitializers map.\n", config.GatewayName)
			continue
		}

		// Initialize gateway dynamically
		gateway, err := initializer(&config)
		if err != nil {
			fmt.Printf("Error initializing gateway '%s': %v\n", config.GatewayName, err)
			continue
		}

		// Register the initialized gateway
		f.RegisterGateway(config.GatewayName, gateway)
		fmt.Printf("✓ Registered gateway: %s (test_mode=%v)\n", config.GatewayName, config.IsTestMode)
	}

	return nil
}

// GatewayInfo represents basic information about an available gateway
type GatewayInfo struct {
	Name             string   `json:"name"`
	DisplayName      string   `json:"display_name"`
	Priority         int      `json:"priority"`
	PercentageFee    float64  `json:"percentage_fee"`
	FixedFee         float64  `json:"fixed_fee"`
	SupportedMethods []string `json:"supported_methods"`
}

// Helper functions

// contains checks if a string slice contains a value
func contains(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}

// decryptCredential decrypts an encrypted credential
// TODO: Implement actual decryption based on your encryption strategy
func decryptCredential(encrypted string) string {
	// For now, return as-is
	// In production, implement proper decryption using AES-256 or similar
	// Example:
	// return crypto.Decrypt(encrypted, encryptionKey)
	return encrypted
}

// encryptCredential encrypts a credential for storage
// TODO: Implement actual encryption
func encryptCredential(plaintext string) string {
	// For now, return as-is
	// In production, implement proper encryption using AES-256 or similar
	// Example:
	// return crypto.Encrypt(plaintext, encryptionKey)
	return plaintext
}
