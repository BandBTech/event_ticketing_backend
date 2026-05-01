package currency

import (
	"fmt"
	"math"
)

// CurrencyConfig defines currency behavior
type CurrencyConfig struct {
	Code          string
	Name          string
	Symbol        string
	DecimalPlaces int
}

// SupportedCurrencies is the central currency registry
var SupportedCurrencies = map[string]CurrencyConfig{
	"USD": {
		Code:          "USD",
		Name:          "US Dollar",
		Symbol:        "$",
		DecimalPlaces: 2,
	},
	"EUR": {
		Code:          "EUR",
		Name:          "Euro",
		Symbol:        "€",
		DecimalPlaces: 2,
	},
	"GBP": {
		Code:          "GBP",
		Name:          "British Pound",
		Symbol:        "£",
		DecimalPlaces: 2,
	},
	"JPY": {
		Code:          "JPY",
		Name:          "Japanese Yen",
		Symbol:        "¥",
		DecimalPlaces: 0,
	},
	"NPR": {
		Code:          "NPR",
		Name:          "Nepalese Rupee",
		Symbol:        "Rs",
		DecimalPlaces: 2,
	},
	"INR": {
		Code:          "INR",
		Name:          "Indian Rupee",
		Symbol:        "₹",
		DecimalPlaces: 2,
	},
	"AUD": {
		Code:          "AUD",
		Name:          "Australian Dollar",
		Symbol:        "A$",
		DecimalPlaces: 2,
	},
	"CAD": {
		Code:          "CAD",
		Name:          "Canadian Dollar",
		Symbol:        "C$",
		DecimalPlaces: 2,
	},
	"SGD": {
		Code:          "SGD",
		Name:          "Singapore Dollar",
		Symbol:        "S$",
		DecimalPlaces: 2,
	},
}

// Exists checks if currency is supported
func Exists(code string) bool {
	_, ok := SupportedCurrencies[code]
	return ok
}

// Get returns currency config
func Get(code string) (CurrencyConfig, error) {
	cfg, ok := SupportedCurrencies[code]
	if !ok {
		return CurrencyConfig{}, fmt.Errorf("unsupported currency: %s", code)
	}

	return cfg, nil
}

// ToSmallestUnit converts display amount to smallest unit
//
// Examples:
// 100.50 USD -> 10050
// 1000 JPY -> 1000
func ToSmallestUnit(amount float64, currency string) (int64, error) {
	cfg, err := Get(currency)
	if err != nil {
		return 0, err
	}

	multiplier := math.Pow10(cfg.DecimalPlaces)

	return int64(math.Round(amount * multiplier)), nil
}

// FromSmallestUnit converts smallest unit to display amount
//
// Examples:
// 10050 USD -> 100.50
// 1000 JPY -> 1000
func FromSmallestUnit(amount int64, currency string) (float64, error) {
	cfg, err := Get(currency)
	if err != nil {
		return 0, err
	}

	divisor := math.Pow10(cfg.DecimalPlaces)

	return float64(amount) / divisor, nil
}

// FormatAmount formats display amount
//
// Examples:
// Rs 1000.00
// ¥1000
func FormatAmount(amount int64, currency string) (string, error) {
	cfg, err := Get(currency)
	if err != nil {
		return "", err
	}

	displayAmount, err := FromSmallestUnit(amount, currency)
	if err != nil {
		return "", err
	}

	format := fmt.Sprintf("%%s %%.%df", cfg.DecimalPlaces)

	return fmt.Sprintf(format, cfg.Symbol, displayAmount), nil
}
