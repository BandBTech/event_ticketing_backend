package workers

// WorkerManager manages all background workers
type WorkerManager struct {
	EmailWorker         *EmailWorker
	OTPWorker           *OTPWorker
	EventStatusWorker   *EventStatusWorker
	StripeWebhookWorker *StripeWebhookWorker
}

// NewWorkerManager creates a new worker manager without webhook worker
func NewWorkerManager(emailWorker *EmailWorker, otpWorker *OTPWorker, eventStatusWorker *EventStatusWorker) *WorkerManager {
	return &WorkerManager{
		EmailWorker:       emailWorker,
		OTPWorker:         otpWorker,
		EventStatusWorker: eventStatusWorker,
	}
}

// NewWorkerManagerWithWebhookWorker creates a new worker manager with webhook worker
func NewWorkerManagerWithWebhookWorker(emailWorker *EmailWorker, otpWorker *OTPWorker, eventStatusWorker *EventStatusWorker, stripeWebhookWorker *StripeWebhookWorker) *WorkerManager {
	return &WorkerManager{
		EmailWorker:         emailWorker,
		OTPWorker:           otpWorker,
		EventStatusWorker:   eventStatusWorker,
		StripeWebhookWorker: stripeWebhookWorker,
	}
}

// StartAll starts all background workers
func (m *WorkerManager) StartAll() {
	m.EmailWorker.Start()
	m.OTPWorker.Start()
	m.EventStatusWorker.Start()
	if m.StripeWebhookWorker != nil {
		m.StripeWebhookWorker.Start()
	}
}

// StopAll stops all background workers
func (m *WorkerManager) StopAll() {
	m.EmailWorker.Stop()
	m.OTPWorker.Stop()
	m.EventStatusWorker.Stop()
	if m.StripeWebhookWorker != nil {
		m.StripeWebhookWorker.Stop()
	}
}
