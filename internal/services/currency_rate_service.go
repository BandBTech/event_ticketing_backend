package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// CurrencyRateService handles currency exchange rate updates and caching
type CurrencyRateService struct {
	db          *gorm.DB
	redisClient *redis.Client
	cfg         *config.Config
	httpClient  *http.Client
}

// ExchangeRateAPIResponse represents the response from exchangerate-api.com API
type ExchangeRateAPIResponse struct {
	Provider        string                 `json:"provider"`
	Warning         string                 `json:"WARNING_UPGRADE_TO_V6,omitempty"`
	Terms           string                 `json:"terms"`
	Base            string                 `json:"base"`
	Date            string                 `json:"date"`
	TimeLastUpdated int64                  `json:"time_last_updated"`
	Rates           map[string]interface{} `json:"rates"`
}

// NewCurrencyRateService creates a new currency rate service instance
func NewCurrencyRateService(db *gorm.DB, redisClient *redis.Client, cfg *config.Config) *CurrencyRateService {
	return &CurrencyRateService{
		db:          db,
		redisClient: redisClient,
		cfg:         cfg,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// UpdateExchangeRates fetches and stores exchange rates with Redis locking
func (s *CurrencyRateService) UpdateExchangeRates(ctx context.Context) error {
	const lockKey = "currency_rates_update_lock"
	const lockTTL = 10 * time.Minute // Lock expires in 10 minutes

	// Try to acquire Redis lock
	lockAcquired, err := s.acquireLock(ctx, lockKey, lockTTL)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	if !lockAcquired {
		log.Printf("[CURRENCY] Another instance is already updating rates, skipping")
		return nil // Exit fast if lock not acquired
	}

	defer func() {
		// Release lock when done
		if err := s.releaseLock(ctx, lockKey); err != nil {
			log.Printf("[CURRENCY] Warning: Failed to release lock: %v", err)
		}
	}()

	// Create timeout context for the operation
	updateCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	log.Printf("[CURRENCY] Starting exchange rate update")

	// Fetch rates from API with retries
	rates, err := s.fetchExchangeRatesWithRetry(updateCtx)
	if err != nil {
		return fmt.Errorf("failed to fetch exchange rates: %w", err)
	}

	// Validate data
	if err := s.validateExchangeRates(rates); err != nil {
		return fmt.Errorf("invalid exchange rates data: %w", err)
	}

	// Store rates in database (idempotent operation)
	if err := s.storeExchangeRates(updateCtx, rates); err != nil {
		return fmt.Errorf("failed to store exchange rates: %w", err)
	}

	log.Printf("[CURRENCY] Successfully updated exchange rates")
	return nil
}

// acquireLock attempts to acquire a Redis lock
func (s *CurrencyRateService) acquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return s.redisClient.SetNX(ctx, key, "locked", ttl).Result()
}

// releaseLock releases a Redis lock
func (s *CurrencyRateService) releaseLock(ctx context.Context, key string) error {
	return s.redisClient.Del(ctx, key).Err()
}

// fetchExchangeRatesWithRetry fetches rates from API with retry logic
func (s *CurrencyRateService) fetchExchangeRatesWithRetry(ctx context.Context) (map[string]float64, error) {
	const maxRetries = 3
	const baseDelay = 2 * time.Second

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		rates, err := s.fetchExchangeRates(ctx)
		if err == nil {
			return rates, nil
		}

		lastErr = err
		if attempt < maxRetries {
			delay := time.Duration(attempt) * baseDelay
			log.Printf("[CURRENCY] Attempt %d failed, retrying in %v: %v", attempt, delay, err)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
				// Continue to next attempt
			}
		}
	}

	return nil, fmt.Errorf("all %d attempts failed, last error: %w", maxRetries, lastErr)
}

// fetchExchangeRates fetches current exchange rates from exchangerate-api.com
func (s *CurrencyRateService) fetchExchangeRates(ctx context.Context) (map[string]float64, error) {
	// Use exchangerate-api.com API - get rates with USD as base (free tier)
	url := "https://api.exchangerate-api.com/v4/latest/USD"
	log.Printf("[CURRENCY] Fetching rates from URL: %s", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch exchange rates: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("[CURRENCY] API response status: %d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exchange rate API returned status: %d", resp.StatusCode)
	}

	var apiResp ExchangeRateAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode API response: %w", err)
	}

	// Log the API response for debugging
	log.Printf("[CURRENCY] API Response: provider=%s, base=%s, date=%s, rates_count=%d",
		apiResp.Provider, apiResp.Base, apiResp.Date, len(apiResp.Rates))

	// Convert rates to map[string]float64
	rates := make(map[string]float64)
	for currency, rate := range apiResp.Rates {
		if rateFloat, ok := rate.(float64); ok {
			rates[currency] = rateFloat
		}
	}

	return rates, nil
}

// validateExchangeRates validates the fetched exchange rates
func (s *CurrencyRateService) validateExchangeRates(rates map[string]float64) error {
	if len(rates) == 0 {
		return fmt.Errorf("no exchange rates received")
	}

	// Check for USD rate (should be 1.0)
	if usdRate, exists := rates["USD"]; !exists || usdRate != 1.0 {
		return fmt.Errorf("invalid USD rate: expected 1.0, got %v", usdRate)
	}

	// Check for some major currencies
	requiredCurrencies := []string{"EUR", "GBP", "JPY", "CAD", "AUD"}
	for _, currency := range requiredCurrencies {
		if rate, exists := rates[currency]; !exists || rate <= 0 {
			return fmt.Errorf("missing or invalid rate for %s: %v", currency, rate)
		}
	}

	return nil
}

// storeExchangeRates stores exchange rates in database (idempotent)
func (s *CurrencyRateService) storeExchangeRates(ctx context.Context, rates map[string]float64) error {
	now := time.Now()

	// Begin transaction
	tx := s.db.Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Deactivate previous rates
	if err := tx.Model(&models.CurrencyRate{}).
		Where("is_active = ? AND valid_until IS NULL", true).
		Updates(map[string]interface{}{
			"is_active":   false,
			"valid_until": now,
			"updated_at":  now,
		}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to deactivate old rates: %w", err)
	}

	// Insert new rates
	for targetCurrency, rate := range rates {
		currencyRate := models.CurrencyRate{
			BaseCurrency:   "USD",
			TargetCurrency: targetCurrency,
			Rate:           rate,
			Source:         "exchangerate.host",
			ValidFrom:      now,
			IsActive:       true,
			CreatedAt:      now,
			UpdatedAt:      now,
		}

		// Upsert: insert or update if exists
		if err := tx.Where(models.CurrencyRate{
			BaseCurrency:   "USD",
			TargetCurrency: targetCurrency,
			ValidFrom:      now,
		}).Assign(models.CurrencyRate{
			Rate:      rate,
			IsActive:  true,
			UpdatedAt: now,
		}).FirstOrCreate(&currencyRate).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to upsert rate for %s: %w", targetCurrency, err)
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("[CURRENCY] Stored %d exchange rates", len(rates))
	return nil
}

// GetExchangeRateFromCache retrieves the latest exchange rate from database cache
func (s *CurrencyRateService) GetExchangeRateFromCache(fromCurrency, toCurrency string) (float64, error) {
	if fromCurrency == toCurrency {
		return 1.0, nil
	}

	var rate models.CurrencyRate
	err := s.db.Where("base_currency = ? AND target_currency = ? AND is_active = ?",
		fromCurrency, toCurrency, true).
		Order("valid_from DESC").
		First(&rate).Error

	if err != nil {
		return 0, fmt.Errorf("exchange rate not found for %s to %s: %w", fromCurrency, toCurrency, err)
	}

	return rate.Rate, nil
}

// ConvertToUSD converts any amount to USD using cached rates
func (s *CurrencyRateService) ConvertToUSD(amount float64, fromCurrency string) (float64, float64, error) {
	if fromCurrency == "USD" {
		return amount, 1.0, nil
	}

	rate, err := s.GetExchangeRateFromCache("USD", fromCurrency)
	if err != nil {
		return 0, 0, err
	}

	// If we have USD to target rate, we need the inverse for target to USD
	usdAmount := amount / rate
	return usdAmount, rate, nil
}
