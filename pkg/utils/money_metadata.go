package utils

import (
	"strings"

	pkgcurrency "event-ticketing-backend/pkg/currency"
)

// MoneyMetadata is centralized country/currency metadata for API responses.
type MoneyMetadata struct {
	Currency       string `json:"currency"`
	Country        string `json:"country"`
	CurrencySymbol string `json:"currency_symbol,omitempty"`
}

var countryAliases = map[string]string{
	"NP":            "NP",
	"NEPAL":         "NP",
	"JP":            "JP",
	"JAPAN":         "JP",
	"US":            "US",
	"USA":           "US",
	"UNITED STATES": "US",
}

// NormalizeCountry returns a normalized country code when known, otherwise upper-cased input.
func NormalizeCountry(country string) string {
	normalized := strings.ToUpper(strings.TrimSpace(country))
	if normalized == "" {
		return ""
	}
	if code, ok := countryAliases[normalized]; ok {
		return code
	}
	return normalized
}

// ResolveMoneyMetadata returns normalized money metadata from event currency and country values.
func ResolveMoneyMetadata(currencyCode, country string) MoneyMetadata {
	code := strings.ToUpper(strings.TrimSpace(currencyCode))
	countryCode := NormalizeCountry(country)

	metadata := MoneyMetadata{
		Currency: code,
		Country:  countryCode,
	}

	if cfg, err := pkgcurrency.Get(code); err == nil {
		metadata.Currency = cfg.Code
		metadata.CurrencySymbol = cfg.Symbol
	}

	return metadata
}
