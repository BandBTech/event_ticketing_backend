package workers

import (
	"context"
	"log"
	"sync"
	"time"

	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
)

// EmailWorker represents a worker that processes email jobs
type EmailWorker struct {
	queueService *services.EmailQueueService
	workerCount  int
	stopCh       chan struct{}
	wg           sync.WaitGroup
	running      bool
	mu           sync.Mutex
}

// NewEmailWorker creates a new email worker
func NewEmailWorker(cfg *config.Config, emailService *services.EmailService) *EmailWorker {
	queueService := services.NewEmailQueueService(emailService)

	// Default to 2 workers, but can be configured
	workerCount := 2
	if cfg != nil {
		// You could add EMAIL_WORKER_COUNT to your config
		// workerCount = cfg.App.EmailWorkerCount
	}

	return &EmailWorker{
		queueService: queueService,
		workerCount:  workerCount,
		stopCh:       make(chan struct{}),
	}
}

// Start begins processing email jobs
func (w *EmailWorker) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return
	}

	w.running = true
	w.stopCh = make(chan struct{})

	log.Printf("Starting email worker with %d worker routines", w.workerCount)

	// Start worker goroutines
	for i := 0; i < w.workerCount; i++ {
		w.wg.Add(1)
		go w.worker(i)
	}
}

// Stop halts all email processing
func (w *EmailWorker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return
	}

	log.Println("Stopping email worker")

	close(w.stopCh)
	w.wg.Wait()
	w.running = false
}

// worker is a goroutine that processes email jobs
func (w *EmailWorker) worker(id int) {
	defer w.wg.Done()

	log.Printf("Email worker %d started", id)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a ticker for periodic health logging
	healthTicker := time.NewTicker(5 * time.Minute)
	defer healthTicker.Stop()

	for {
		select {
		case <-w.stopCh:
			log.Printf("Email worker %d shutting down", id)
			return

		case <-healthTicker.C:
			log.Printf("Email worker %d is healthy", id)

		default:
			// Process one job
			err := w.queueService.ProcessEmailQueue(ctx)
			if err != nil {
				log.Printf("Error processing email queue: %v", err)

				// Sleep briefly on error to prevent tight loop
				select {
				case <-time.After(time.Second):
				case <-w.stopCh:
					return
				}
			}
		}
	}
}

// QueueOTPEmail is a helper method to quickly queue OTP emails
func (w *EmailWorker) QueueOTPEmail(to, otp, otpType string) error {
	return w.queueService.QueueOTPEmail(to, otp, otpType)
}

// GetQueueService returns the underlying queue service
func (w *EmailWorker) GetQueueService() *services.EmailQueueService {
	return w.queueService
}
