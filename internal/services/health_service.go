package services

import (
	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/redis"
	"runtime"
	"time"
)

// HealthService provides methods to check the health of various components
type HealthService struct {
	startTime time.Time
}

// HealthStatus represents the overall health status of the API
type HealthStatus struct {
	Status      string       `json:"status"`
	Uptime      string       `json:"uptime"`
	Server      ServerStatus `json:"server"`
	Database    Status       `json:"database"`
	Redis       Status       `json:"redis"`
	Environment string       `json:"environment"`
}

// SimpleHealthStatus represents a simplified health status with component statuses and messages
type SimpleHealthStatus struct {
	Status   string            `json:"status"`
	Uptime   string            `json:"uptime"`
	Services map[string]string `json:"services"`
}

// ServerStatus represents the server health status
type ServerStatus struct {
	Status       string  `json:"status"`
	GoVersion    string  `json:"goVersion"`
	NumGoroutine int     `json:"numGoroutine"`
	HeapInUse    float64 `json:"heapInUse"` // In MB
}

// Status represents the health status of a component
type Status struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// NewHealthService creates a new health service
func NewHealthService() *HealthService {
	return &HealthService{
		startTime: time.Now(),
	}
}

// CheckHealth checks the health of all components
func (s *HealthService) CheckHealth() *HealthStatus {
	dbStatus := s.checkDBHealth()
	redisStatus := s.checkRedisHealth()
	serverStatus := s.checkServerHealth()

	// Overall status is determined by the status of all components
	status := "healthy"
	if dbStatus.Status == "unhealthy" || redisStatus.Status == "unhealthy" {
		status = "degraded"
	}

	return &HealthStatus{
		Status:      status,
		Uptime:      time.Since(s.startTime).String(),
		Server:      serverStatus,
		Database:    dbStatus,
		Redis:       redisStatus,
		Environment: "production", // This should be dynamically determined from config
	}
}

// CheckSimpleHealth provides a simplified health check for all components
func (s *HealthService) CheckSimpleHealth() *SimpleHealthStatus {
	dbStatus := s.checkDBHealth()
	redisStatus := s.checkRedisHealth()

	// Create services status map with detailed messages
	services := map[string]string{
		"server":   "up and running",
		"database": "up and running",
		"redis":    "up and running",
	}

	// Update status based on component checks
	overallStatus := "healthy"

	if dbStatus.Status == "unhealthy" {
		services["database"] = dbStatus.Message
		overallStatus = "degraded"
	}

	if redisStatus.Status == "unhealthy" {
		services["redis"] = redisStatus.Message
		overallStatus = "degraded"
	}

	return &SimpleHealthStatus{
		Status:   overallStatus,
		Uptime:   time.Since(s.startTime).String(),
		Services: services,
	}
}

// CheckDBHealth checks the health of the database
func (s *HealthService) CheckDBHealth() Status {
	return s.checkDBHealth()
}

// CheckRedisHealth checks the health of Redis
func (s *HealthService) CheckRedisHealth() Status {
	return s.checkRedisHealth()
}

// CheckServerHealth checks the health of the server
func (s *HealthService) CheckServerHealth() ServerStatus {
	return s.checkServerHealth()
}

// Private helper methods

func (s *HealthService) checkDBHealth() Status {
	if database.IsHealthy() {
		return Status{
			Status:  "healthy",
			Message: "Database connection is healthy",
		}
	}
	return Status{
		Status:  "unhealthy",
		Message: "Database connection failed",
	}
}

func (s *HealthService) checkRedisHealth() Status {
	if redis.IsHealthy() {
		return Status{
			Status:  "healthy",
			Message: "Redis connection is healthy",
		}
	}
	return Status{
		Status:  "unhealthy",
		Message: "Redis connection failed",
	}
}

func (s *HealthService) checkServerHealth() ServerStatus {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	return ServerStatus{
		Status:       "healthy",
		GoVersion:    runtime.Version(),
		NumGoroutine: runtime.NumGoroutine(),
		HeapInUse:    float64(mem.HeapInuse) / 1024 / 1024, // Convert bytes to MB
	}
}
