package workers

// WorkerManager manages all background workers
type WorkerManager struct {
	EmailWorker                *EmailWorker
	EmailOutboxProcessorWorker *EmailOutboxProcessorWorker
	OTPWorker                  *OTPWorker
	EventStatusWorker          *EventStatusWorker
	CurrencyRateWorker         *CurrencyRateWorker
}

// NewWorkerManager creates a new worker manager
func NewWorkerManager(emailWorker *EmailWorker, emailOutboxProcessorWorker *EmailOutboxProcessorWorker, otpWorker *OTPWorker, eventStatusWorker *EventStatusWorker, currencyRateWorker *CurrencyRateWorker) *WorkerManager {
	return &WorkerManager{
		EmailWorker:                emailWorker,
		EmailOutboxProcessorWorker: emailOutboxProcessorWorker,
		OTPWorker:                  otpWorker,
		EventStatusWorker:          eventStatusWorker,
		CurrencyRateWorker:         currencyRateWorker,
	}
}

// StartAll starts all background workers
func (m *WorkerManager) StartAll() {
	m.EmailWorker.Start()
	m.EmailOutboxProcessorWorker.Start()
	m.OTPWorker.Start()
	m.EventStatusWorker.Start()
	m.CurrencyRateWorker.Start()
}

// StopAll stops all background workers
func (m *WorkerManager) StopAll() {
	m.EmailWorker.Stop()
	m.EmailOutboxProcessorWorker.Stop()
	m.OTPWorker.Stop()
	m.EventStatusWorker.Stop()
	m.CurrencyRateWorker.Stop()
}
