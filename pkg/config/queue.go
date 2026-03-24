package config

import (
	"fmt"

	"github.com/hibiken/asynq"
)

// Queue configuration for asynq
type QueueConfig struct {
	RedisAddr       string
	StrictPriority  bool
	Concurrency     int
	DefaultQueue    string
	CriticalQueue   string
	HealthCheckPort string
}

// NewQueueConfig creates a new queue configuration
func NewQueueConfig(cfg *Config) *QueueConfig {
	return &QueueConfig{
		RedisAddr:       fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		StrictPriority:  true,
		Concurrency:     20, // Process 20 tasks concurrently
		DefaultQueue:    "default",
		CriticalQueue:   "critical",
		HealthCheckPort: ":8081",
	}
}

// GetRedisClientOpt returns redis client options for asynq
func (qc *QueueConfig) GetRedisClientOpt() asynq.RedisClientOpt {
	return asynq.RedisClientOpt{
		Addr: qc.RedisAddr,
	}
}

// GetServerConfig returns server configuration for asynq
func (qc *QueueConfig) GetServerConfig() asynq.Config {
	return asynq.Config{
		Concurrency:    qc.Concurrency,
		StrictPriority: qc.StrictPriority,
		Queues: map[string]int{
			qc.CriticalQueue: 5, // 5x priority for critical payments
			qc.DefaultQueue:  1,
		},
		LogLevel: asynq.InfoLevel,
	}
}

// GetClientConfig returns client configuration for enqueueing tasks
func (qc *QueueConfig) GetClientConfig() *asynq.Client {
	return asynq.NewClient(qc.GetRedisClientOpt())
}
