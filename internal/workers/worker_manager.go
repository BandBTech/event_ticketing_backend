package workers

// WorkerManager manages all background workers
type WorkerManager struct {
	EmailWorker                *EmailWorker
	EmailOutboxProcessorWorker *EmailOutboxProcessorWorker
	OTPWorker                  *OTPWorker
	EventStatusWorker          *EventStatusWorker
	RefundWorker               *RefundWorker
}

// NewWorkerManager creates a new worker manager
func NewWorkerManager(emailWorker *EmailWorker, emailOutboxProcessorWorker *EmailOutboxProcessorWorker, otpWorker *OTPWorker, eventStatusWorker *EventStatusWorker, refundWorker *RefundWorker) *WorkerManager {
	return &WorkerManager{
		EmailWorker:                emailWorker,
		EmailOutboxProcessorWorker: emailOutboxProcessorWorker,
		OTPWorker:                  otpWorker,
		EventStatusWorker:          eventStatusWorker,
		RefundWorker:               refundWorker,
	}
}

// StartAll starts all background workers
func (m *WorkerManager) StartAll() {
	m.EmailWorker.Start()
	m.EmailOutboxProcessorWorker.Start()
	m.OTPWorker.Start()
	m.EventStatusWorker.Start()
	if m.RefundWorker != nil {
		m.RefundWorker.Start()
	}
}

// StopAll stops all background workers
func (m *WorkerManager) StopAll() {
	m.EmailWorker.Stop()
	m.EmailOutboxProcessorWorker.Stop()
	m.OTPWorker.Stop()
	m.EventStatusWorker.Stop()
	if m.RefundWorker != nil {
		m.RefundWorker.Stop()
	}
}
