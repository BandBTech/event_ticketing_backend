package workers

// WorkerManager manages all background workers
type WorkerManager struct {
	EmailWorker *EmailWorker
}

// NewWorkerManager creates a new worker manager and initializes all workers
func NewWorkerManager(emailWorker *EmailWorker) *WorkerManager {
	return &WorkerManager{
		EmailWorker: emailWorker,
	}
}

// StartAll starts all background workers
func (m *WorkerManager) StartAll() {
	m.EmailWorker.Start()
}

// StopAll stops all background workers
func (m *WorkerManager) StopAll() {
	m.EmailWorker.Stop()
}
