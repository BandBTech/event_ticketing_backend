package services

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"event-ticketing-backend/pkg/config"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

const RefundProcessTaskType = "refund:process"

type RefundProcessTaskPayload struct {
	RefundID uuid.UUID `json:"refund_id"`
}

type RefundQueueService struct {
	client *asynq.Client
}

func NewRefundQueueService(cfg *config.Config) *RefundQueueService {
	dbInt := 0
	if cfg.Redis.DB != "" {
		if parsed, err := strconv.Atoi(cfg.Redis.DB); err == nil {
			dbInt = parsed
		}
	}
	redisOpts := asynq.RedisClientOpt{
		Addr:         fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password:     cfg.Redis.Password,
		DB:           dbInt,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		PoolSize:     10,
	}
	return &RefundQueueService{client: asynq.NewClient(redisOpts)}
}

func (s *RefundQueueService) EnqueueRefundProcessing(refundID uuid.UUID) error {
	payload, err := json.Marshal(RefundProcessTaskPayload{RefundID: refundID})
	if err != nil {
		return err
	}
	task := asynq.NewTask(RefundProcessTaskType, payload)
	_, err = s.client.Enqueue(task,
		asynq.MaxRetry(10),
		asynq.Queue("queue:refund:normal"),
		asynq.Timeout(60*time.Second),
		asynq.TaskID(refundID.String()),
	)
	return err
}

func (s *RefundQueueService) Close() error {
	return s.client.Close()
}
