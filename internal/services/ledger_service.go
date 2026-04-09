package services

import (
	"context"
	"fmt"
	"time"

	"event-ticketing-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// LedgerService handles all ledger operations for financial tracking
type LedgerService struct {
	db *gorm.DB
}

// NewLedgerService creates a new ledger service instance
func NewLedgerService(db *gorm.DB) *LedgerService {
	return &LedgerService{db: db}
}

// CreateLedgerEntry creates a new ledger entry
func (s *LedgerService) CreateLedgerEntry(ctx context.Context, entry *models.LedgerEntry) error {
	return s.db.WithContext(ctx).Create(entry).Error
}

// RecordSale records a sale transaction in the ledger
func (s *LedgerService) RecordSale(ctx context.Context, transactionID uuid.UUID, organizerID uuid.UUID, amountLocal float64, currency string, amountBase float64, exchangeRate float64) error {
	entry := &models.LedgerEntry{
		OrganizerID:   organizerID,
		TransactionID: &transactionID,
		Type:          "SALE",
		AmountLocal:   amountLocal,
		Currency:      currency,
		AmountBase:    amountBase,
		BaseCurrency:  "USD",
		ExchangeRate:  exchangeRate,
		Description:   "Ticket sale",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	return s.CreateLedgerEntry(ctx, entry)
}

// RecordRefund records a refund transaction in the ledger
func (s *LedgerService) RecordRefund(ctx context.Context, transactionID uuid.UUID, organizerID uuid.UUID, amountLocal float64, currency string, amountBase float64, exchangeRate float64) error {
	entry := &models.LedgerEntry{
		OrganizerID:   organizerID,
		TransactionID: &transactionID,
		Type:          "REFUND",
		AmountLocal:   -amountLocal, // Negative for refunds
		Currency:      currency,
		AmountBase:    -amountBase, // Negative for refunds
		BaseCurrency:  "USD",
		ExchangeRate:  exchangeRate,
		Description:   "Ticket refund",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	return s.CreateLedgerEntry(ctx, entry)
}

// RecordStripeFee records Stripe processing fees
func (s *LedgerService) RecordStripeFee(ctx context.Context, transactionID uuid.UUID, organizerID uuid.UUID, feeAmount float64, currency string, feeAmountBase float64, exchangeRate float64) error {
	entry := &models.LedgerEntry{
		OrganizerID:   organizerID,
		TransactionID: &transactionID,
		Type:          "STRIPE_FEE",
		AmountLocal:   -feeAmount, // Negative for fees
		Currency:      currency,
		AmountBase:    -feeAmountBase,
		BaseCurrency:  "USD",
		ExchangeRate:  exchangeRate,
		Description:   "Stripe processing fee",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	return s.CreateLedgerEntry(ctx, entry)
}

// RecordPlatformFee records platform commission fees
func (s *LedgerService) RecordPlatformFee(ctx context.Context, transactionID uuid.UUID, organizerID uuid.UUID, feeAmount float64, currency string, feeAmountBase float64, exchangeRate float64) error {
	entry := &models.LedgerEntry{
		OrganizerID:   organizerID,
		TransactionID: &transactionID,
		Type:          "PLATFORM_FEE",
		AmountLocal:   -feeAmount, // Negative for fees
		Currency:      currency,
		AmountBase:    -feeAmountBase,
		BaseCurrency:  "USD",
		ExchangeRate:  exchangeRate,
		Description:   "Platform commission fee",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	return s.CreateLedgerEntry(ctx, entry)
}

// RecordPayout records a payout to organizer
func (s *LedgerService) RecordPayout(ctx context.Context, organizerID uuid.UUID, amountLocal float64, currency string, amountBase float64, exchangeRate float64, description string) error {
	entry := &models.LedgerEntry{
		OrganizerID:  organizerID,
		Type:         "PAYOUT",
		AmountLocal:  -amountLocal, // Negative for payouts
		Currency:     currency,
		AmountBase:   -amountBase,
		BaseCurrency: "USD",
		ExchangeRate: exchangeRate,
		Description:  description,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	return s.CreateLedgerEntry(ctx, entry)
}

// GetOrganizerBalance calculates the current balance for an organizer
func (s *LedgerService) GetOrganizerBalance(ctx context.Context, organizerID uuid.UUID, currency string) (*models.LedgerBalance, error) {
	var result struct {
		TotalLocal float64
		Currency   string
	}

	// Sum all ledger entries for the organizer in the specified currency
	err := s.db.WithContext(ctx).Model(&models.LedgerEntry{}).
		Select("COALESCE(SUM(amount_local), 0) as total_local, ? as currency", currency).
		Where("organizer_id = ? AND currency = ?", organizerID, currency).
		Scan(&result).Error

	if err != nil {
		return nil, fmt.Errorf("failed to calculate organizer balance: %w", err)
	}

	return &models.LedgerBalance{
		OrganizerID: organizerID,
		Currency:    result.Currency,
		Balance:     result.TotalLocal,
	}, nil
}

// GetOrganizerBalanceUSD calculates the current balance in USD
func (s *LedgerService) GetOrganizerBalanceUSD(ctx context.Context, organizerID uuid.UUID) (float64, error) {
	var result struct {
		TotalBase float64
	}

	err := s.db.WithContext(ctx).Model(&models.LedgerEntry{}).
		Select("COALESCE(SUM(amount_base), 0) as total_base").
		Where("organizer_id = ?", organizerID).
		Scan(&result).Error

	if err != nil {
		return 0, fmt.Errorf("failed to calculate organizer USD balance: %w", err)
	}

	return result.TotalBase, nil
}

// GetLedgerEntries retrieves ledger entries for an organizer with pagination
func (s *LedgerService) GetLedgerEntries(ctx context.Context, organizerID uuid.UUID, limit, offset int) ([]models.LedgerEntry, error) {
	var entries []models.LedgerEntry

	err := s.db.WithContext(ctx).
		Where("organizer_id = ?", organizerID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&entries).Error

	if err != nil {
		return nil, fmt.Errorf("failed to retrieve ledger entries: %w", err)
	}

	return entries, nil
}

// GetLedgerSummary provides a summary of ledger activity for an organizer
func (s *LedgerService) GetLedgerSummary(ctx context.Context, organizerID uuid.UUID) (map[string]float64, error) {
	var results []struct {
		Type  string
		Total float64
	}

	err := s.db.WithContext(ctx).Model(&models.LedgerEntry{}).
		Select("type, COALESCE(SUM(amount_local), 0) as total").
		Where("organizer_id = ?", organizerID).
		Group("type").
		Scan(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get ledger summary: %w", err)
	}

	summary := make(map[string]float64)
	for _, result := range results {
		summary[result.Type] = result.Total
	}

	return summary, nil
}
