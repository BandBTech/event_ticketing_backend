package workers

// WorkerManager manages all background workers
type WorkerManager struct {
	EmailWorker       *EmailWorker
	OTPWorker         *OTPWorker
	EventStatusWorker *EventStatusWorker
}

// NewWorkerManager creates a new worker manager
func NewWorkerManager(emailWorker *EmailWorker, otpWorker *OTPWorker, eventStatusWorker *EventStatusWorker) *WorkerManager {
	return &WorkerManager{
		EmailWorker:       emailWorker,
		OTPWorker:         otpWorker,
		EventStatusWorker: eventStatusWorker,
	}
}

// StartAll starts all background workers
func (m *WorkerManager) StartAll() {
	m.EmailWorker.Start()
	m.OTPWorker.Start()
	m.EventStatusWorker.Start()
}

// StopAll stops all background workers
func (m *WorkerManager) StopAll() {
	m.EmailWorker.Stop()
	m.OTPWorker.Stop()
	m.EventStatusWorker.Stop()
}
