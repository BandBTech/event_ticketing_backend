package workers

// WorkerManager manages all background workers
type WorkerManager struct {
	EmailWorker *EmailWorker
	OTPWorker   *OTPWorker
}

// NewWorkerManager creates a new worker manager
func NewWorkerManager(emailWorker *EmailWorker, otpWorker *OTPWorker) *WorkerManager {
	return &WorkerManager{
		EmailWorker: emailWorker,
		OTPWorker:   otpWorker,
	}
}

// StartAll starts all background workers
func (m *WorkerManager) StartAll() {
	m.EmailWorker.Start()
	m.OTPWorker.Start()
}

// StopAll stops all background workers
func (m *WorkerManager) StopAll() {
	m.EmailWorker.Stop()
	m.OTPWorker.Stop()
}
