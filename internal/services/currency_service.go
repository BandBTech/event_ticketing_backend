package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"event-ticketing-backend/pkg/config"

	"gorm.io/gorm"
)

// CurrencyService handles currency conversion and exchange rates
type CurrencyService struct {
	db     *gorm.DB
	cfg    *config.Config
	client *http.Client
}

// ExchangeRateResponse represents the response from exchangerate.host API
type ExchangeRateResponse struct {
	Motd    interface{}            `json:"motd"`
	Success bool                   `json:"success"`
	Base    string                 `json:"base"`
	Date    string                 `json:"date"`
	Rates   map[string]interface{} `json:"rates"`
}

// NewCurrencyService creates a new currency service instance
func NewCurrencyService(db *gorm.DB, cfg *config.Config) *CurrencyService {
	return &CurrencyService{
		db:     db,
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// GetExchangeRate fetches the current exchange rate from base currency to target currency
func (s *CurrencyService) GetExchangeRate(ctx context.Context, fromCurrency, toCurrency string) (float64, error) {
	if fromCurrency == toCurrency {
		return 1.0, nil
	}

	// Use exchangerate.host API (free tier)
	url := fmt.Sprintf("https://api.exchangerate.host/latest?base=%s&symbols=%s", fromCurrency, toCurrency)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch exchange rate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("exchange rate API returned status: %d", resp.StatusCode)
	}

	var apiResp ExchangeRateResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return 0, fmt.Errorf("failed to decode API response: %w", err)
	}

	if !apiResp.Success {
		return 0, fmt.Errorf("exchange rate API request failed")
	}

	rate, ok := apiResp.Rates[toCurrency]
	if !ok {
		return 0, fmt.Errorf("exchange rate for %s not found", toCurrency)
	}

	rateFloat, ok := rate.(float64)
	if !ok {
		return 0, fmt.Errorf("invalid exchange rate format")
	}

	return rateFloat, nil
}

// ConvertAmount converts an amount from one currency to another
func (s *CurrencyService) ConvertAmount(ctx context.Context, amount float64, fromCurrency, toCurrency string) (float64, float64, error) {
	if fromCurrency == toCurrency {
		return amount, 1.0, nil
	}

	rate, err := s.GetExchangeRate(ctx, fromCurrency, toCurrency)
	if err != nil {
		return 0, 0, err
	}

	convertedAmount := amount * rate
	return convertedAmount, rate, nil
}

// ConvertToBaseCurrency converts any amount to USD (base currency)
func (s *CurrencyService) ConvertToBaseCurrency(ctx context.Context, amount float64, fromCurrency string) (float64, float64, error) {
	return s.ConvertAmount(ctx, amount, fromCurrency, "USD")
}

// CacheExchangeRate stores exchange rate in cache/database for future use
func (s *CurrencyService) CacheExchangeRate(ctx context.Context, fromCurrency, toCurrency string, rate float64) error {
	// For now, we'll just log it. In production, you'd cache this in Redis or DB
	// This is a simplified implementation
	return nil
}

// ValidateCurrency checks if a currency code is valid
func (s *CurrencyService) ValidateCurrency(currency string) bool {
	validCurrencies := []string{
		"USD", "EUR", "GBP", "JPY", "CAD", "AUD", "CHF", "NOK", "SEK", "DKK",
		"PLN", "CZK", "HUF", "SGD", "HKD", "NZD", "MXN", "BRL", "ZAR", "THB",
		"MYR", "PHP", "TWD", "TRY", "INR", "RUB", "AED", "SAR", "ILS", "EGP",
		"KES", "MAD", "TND", "UGX", "XAF", "XOF", "BWP", "GHS", "MUR", "SCR",
		"CVE", "BSD", "BBD", "BZD", "BND", "FJD", "GYD", "JMD", "LRD", "NAD",
		"SBD", "SRD", "TTD", "VND", "AMD", "AZN", "BAM", "BGN", "BYN", "GEL",
		"HRK", "ISK", "KZT", "MKD", "MDL", "RON", "RSD", "UAH", "UZS", "NPR",
	}

	for _, valid := range validCurrencies {
		if currency == valid {
			return true
		}
	}
	return false
}

// GetCurrencySymbol returns the symbol for a currency code
func (s *CurrencyService) GetCurrencySymbol(currency string) string {
	symbols := map[string]string{
		"USD": "$", "EUR": "€", "GBP": "£", "JPY": "¥", "CAD": "C$", "AUD": "A$",
		"CHF": "CHF", "NOK": "kr", "SEK": "kr", "DKK": "kr", "PLN": "zł", "CZK": "Kč",
		"HUF": "Ft", "SGD": "S$", "HKD": "HK$", "NZD": "NZ$", "MXN": "$", "BRL": "R$",
		"ZAR": "R", "THB": "฿", "MYR": "RM", "PHP": "₱", "TWD": "NT$", "TRY": "₺",
		"INR": "₹", "RUB": "₽", "AED": "د.إ", "SAR": "﷼", "ILS": "₪", "EGP": "£",
		"KES": "KSh", "MAD": "د.م.", "TND": "د.ت", "UGX": "USh", "NPR": "₨",
	}

	if symbol, ok := symbols[currency]; ok {
		return symbol
	}
	return currency // Fallback to currency code
}
