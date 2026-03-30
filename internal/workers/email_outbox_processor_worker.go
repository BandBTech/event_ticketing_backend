package workers

import (
	"context"
	"log"
	"time"

	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
)

// EmailOutboxProcessorWorker processes emails from the email outbox periodically
type EmailOutboxProcessorWorker struct {
	emailOutboxService *services.EmailOutboxService
	emailService       *services.EmailService
	cfg                *config.Config
	ticker             *time.Ticker
	stopChan           chan struct{}
}

// NewEmailOutboxProcessorWorker creates a new email outbox processor worker
func NewEmailOutboxProcessorWorker(cfg *config.Config, emailOutboxService *services.EmailOutboxService, emailService *services.EmailService) *EmailOutboxProcessorWorker {
	return &EmailOutboxProcessorWorker{
		emailOutboxService: emailOutboxService,
		emailService:       emailService,
		cfg:                cfg,
		stopChan:           make(chan struct{}),
	}
}

// Start starts the email outbox processor worker
func (w *EmailOutboxProcessorWorker) Start() {
	log.Println("Starting email outbox processor worker...")

	// Process emails every 30 seconds
	w.ticker = time.NewTicker(30 * time.Second)

	go func() {
		defer w.ticker.Stop()

		for {
			select {
			case <-w.ticker.C:
				w.processEmailOutbox()
			case <-w.stopChan:
				log.Println("Email outbox processor worker stopped")
				return
			}
		}
	}()

	log.Println("Email outbox processor worker started successfully")
}

// Stop stops the email outbox processor worker
func (w *EmailOutboxProcessorWorker) Stop() {
	log.Println("Stopping email outbox processor worker...")
	close(w.stopChan)
	if w.ticker != nil {
		w.ticker.Stop()
	}
	log.Println("Email outbox processor worker stopped")
}

// processEmailOutbox processes pending emails from the outbox
func (w *EmailOutboxProcessorWorker) processEmailOutbox() {
	ctx := context.Background()

	if err := w.emailOutboxService.ProcessEmailOutbox(ctx, w.emailService); err != nil {
		log.Printf("Error processing email outbox: %v", err)
		return
	}

	// Log stats occasionally
	stats, err := w.emailOutboxService.GetEmailOutboxStats(ctx)
	if err != nil {
		log.Printf("Error getting email outbox stats: %v", err)
		return
	}

	// Only log if there are pending emails
	if stats["pending"] > 0 {
		log.Printf("Email outbox stats: pending=%d, processing=%d, sent=%d, failed=%d",
			stats["pending"], stats["processing"], stats["sent"], stats["failed"])
	}
}
