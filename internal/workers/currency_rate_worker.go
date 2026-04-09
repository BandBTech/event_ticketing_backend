package workers

import (
	"context"
	"log"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"

	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// CurrencyRateWorker handles periodic currency exchange rate updates
type CurrencyRateWorker struct {
	db                  *gorm.DB
	redisClient         *redis.Client
	currencyRateService *services.CurrencyRateService
	cronScheduler       *cron.Cron
	cfg                 *config.Config
	running             bool
}

// NewCurrencyRateWorker creates a new currency rate worker
func NewCurrencyRateWorker(db *gorm.DB, redisClient *redis.Client, cfg *config.Config) *CurrencyRateWorker {
	currencyRateService := services.NewCurrencyRateService(db, redisClient, cfg)

	return &CurrencyRateWorker{
		db:                  db,
		redisClient:         redisClient,
		currencyRateService: currencyRateService,
		cronScheduler:       cron.New(),
		cfg:                 cfg,
		running:             false,
	}
}

// Start starts the currency rate worker with scheduled tasks
func (w *CurrencyRateWorker) Start() {
	if w.running {
		log.Println("[CurrencyRateWorker] Worker is already running")
		return
	}

	w.running = true
	log.Println("[CurrencyRateWorker] Starting currency rate worker...")

	// Schedule currency rate updates every 12 hours
	// Cron format: "0 */12 * * *" = At minute 0 past every 12th hour
	// For development/testing, you can change to "*/5 * * * *" for every 5 minutes
	_, err := w.cronScheduler.AddFunc("0 */12 * * *", w.updateCurrencyRates)
	if err != nil {
		log.Printf("[CurrencyRateWorker] Failed to schedule currency rate updates: %v", err)
		return
	}

	w.cronScheduler.Start()
	log.Println("[CurrencyRateWorker] Currency rate worker started successfully - Updates every 12 hours")

	// Check if we have recent currency data (less than 12 hours old)
	hasRecentData, err := w.hasRecentCurrencyData()
	if err != nil {
		log.Printf("[CurrencyRateWorker] Error checking for recent currency data: %v", err)
		log.Println("[CurrencyRateWorker] Performing initial currency rate fetch...")
		w.updateCurrencyRates()
		return
	}

	if hasRecentData {
		log.Println("[CurrencyRateWorker] Recent currency data found, skipping initial fetch")
	} else {
		log.Println("[CurrencyRateWorker] No recent currency data found, performing initial fetch...")
		w.updateCurrencyRates()
	}
}

// Stop stops the currency rate worker
func (w *CurrencyRateWorker) Stop() {
	if !w.running {
		return
	}

	log.Println("[CurrencyRateWorker] Stopping currency rate worker...")
	w.cronScheduler.Stop()
	w.running = false
	log.Println("[CurrencyRateWorker] Currency rate worker stopped")
}

// hasRecentCurrencyData checks if there's currency data that's less than 12 hours old
func (w *CurrencyRateWorker) hasRecentCurrencyData() (bool, error) {
	var count int64
	twelveHoursAgo := time.Now().Add(-12 * time.Hour)

	err := w.db.Model(&models.CurrencyRate{}).
		Where("is_active = ? AND valid_from >= ?", true, twelveHoursAgo).
		Count(&count).Error

	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// updateCurrencyRates fetches and stores current currency exchange rates
func (w *CurrencyRateWorker) updateCurrencyRates() {
	ctx := context.Background()
	log.Println("[CurrencyRateWorker] Running scheduled currency rate updates...")

	startTime := time.Now()

	// Update exchange rates with Redis locking and error handling
	err := w.currencyRateService.UpdateExchangeRates(ctx)
	if err != nil {
		log.Printf("[CurrencyRateWorker] Error updating currency rates: %v", err)
		return
	}

	duration := time.Since(startTime)
	log.Printf("[CurrencyRateWorker] Currency rate update completed successfully in %v", duration)
}

// ManualUpdate allows manual triggering of currency rate updates (for admin/testing)
func (w *CurrencyRateWorker) ManualUpdate(ctx context.Context) error {
	log.Println("[CurrencyRateWorker] Manual currency rate update triggered")

	startTime := time.Now()

	err := w.currencyRateService.UpdateExchangeRates(ctx)
	if err != nil {
		log.Printf("[CurrencyRateWorker] Manual update failed: %v", err)
		return err
	}

	duration := time.Since(startTime)
	log.Printf("[CurrencyRateWorker] Manual currency rate update completed successfully in %v", duration)
	return nil
}
