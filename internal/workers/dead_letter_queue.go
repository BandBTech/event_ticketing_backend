package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DeadLetterQueue represents a failed task that couldn't be processed
type DeadLetterQueue struct {
	ID         uuid.UUID       `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	TaskType   string          `gorm:"not null;size:100;index" json:"task_type"`
	Payload    json.RawMessage `gorm:"type:jsonb;serializer:json" json:"payload"`
	Error      string          `gorm:"type:text" json:"error"`
	Retries    int             `gorm:"default:0" json:"retries"`
	MaxRetry   int             `gorm:"default:5" json:"max_retry"`
	Status     string          `gorm:"not null;default:'pending';index;size:20" json:"status"` // pending, manual_review, resolved, discarded
	FailedAt   time.Time       `gorm:"not null" json:"failed_at"`
	ResolvedAt *time.Time      `json:"resolved_at,omitempty"`
	CreatedAt  time.Time       `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time       `gorm:"autoUpdateTime" json:"updated_at"`

	// Metadata for debugging
	StripeEventID string                 `gorm:"size:255;index" json:"stripe_event_id,omitempty"`
	RequestID     string                 `gorm:"size:255;index" json:"request_id,omitempty"`
	Metadata      map[string]interface{} `gorm:"type:jsonb;serializer:json" json:"metadata,omitempty"`
}

// TableName specifies the table name for Dead Letter Queue
func (DeadLetterQueue) TableName() string {
	return "dead_letter_queues"
}

// RecoverableError checks if a task error is recoverable
func RecoverableError(err string) bool {
	// Network/timeout errors are recoverable
	recoverable := []string{
		"context deadline exceeded",
		"connection refused",
		"connection reset",
		"i/o timeout",
		"temporary failure",
		"database connection",
	}

	for _, pattern := range recoverable {
		if len(err) > 0 && pattern != "" {
			// Simple substring check - in production, use regexp
			for i := 0; i < len(err); i++ {
				match := true
				for j := 0; j < len(pattern); j++ {
					if i+j >= len(err) || (err[i+j] != pattern[j] && err[i+j] != pattern[j]-32 && err[i+j] != pattern[j]+32) {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
		}
	}
	return false
}

// RecordFailedTask stores a failed task in the dead letter queue
func RecordFailedTask(db *gorm.DB, taskType string, payload json.RawMessage, err error, stripeEventID, requestID string) error {
	dlq := &DeadLetterQueue{
		ID:            uuid.New(),
		TaskType:      taskType,
		Payload:       payload,
		Error:         err.Error(),
		Retries:       0,
		MaxRetry:      5,
		Status:        "pending",
		FailedAt:      time.Now(),
		StripeEventID: stripeEventID,
		RequestID:     requestID,
		Metadata: map[string]interface{}{
			"error_type":  fmt.Sprintf("%T", err),
			"recoverable": RecoverableError(err.Error()),
			"recorded_at": time.Now().UTC().Format(time.RFC3339),
		},
	}

	if err := db.Create(dlq).Error; err != nil {
		log.Printf("ERROR: Failed to record dead letter queue task: %v\n", err)
		return err
	}

	log.Printf("CRITICAL: Task recorded in DLQ (ID: %s, Type: %s, StripeEventID: %s)\n", dlq.ID, taskType, stripeEventID)
	return nil
}

// GetPendingDeadLetterTasks retrieves tasks that need manual review
func GetPendingDeadLetterTasks(db *gorm.DB, limit int) ([]DeadLetterQueue, error) {
	var tasks []DeadLetterQueue
	if err := db.Where("status = ?", "pending").
		Order("created_at DESC").
		Limit(limit).
		Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// RetryDeadLetterTask attempts to retry a failed task
func RetryDeadLetterTask(db *gorm.DB, dlqID uuid.UUID, paymentWorker *PaymentWorker) error {
	var dlq DeadLetterQueue
	if err := db.First(&dlq, "id = ?", dlqID).Error; err != nil {
		return fmt.Errorf("dead letter queue task not found: %w", err)
	}

	if dlq.Retries >= dlq.MaxRetry {
		return fmt.Errorf("task has exceeded max retries (%d)", dlq.MaxRetry)
	}

	// Deserialize and retry
	var payload PaymentTaskPayload
	if err := json.Unmarshal(dlq.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	ctx := context.Background()
	var taskID string
	var retryErr error

	switch dlq.TaskType {
	case TypePaymentSuccess:
		taskID, retryErr = paymentWorker.EnqueuePaymentSuccess(ctx, &payload)
	case TypePaymentFailed:
		taskID, retryErr = paymentWorker.EnqueuePaymentFailed(ctx, &payload)
	default:
		return fmt.Errorf("unknown task type: %s", dlq.TaskType)
	}

	if retryErr != nil {
		// Increment retry count
		if err := db.Model(&dlq).Updates(map[string]interface{}{
			"retries": dlq.Retries + 1,
			"error":   retryErr.Error(),
		}).Error; err != nil {
			log.Printf("ERROR: Failed to update DLQ retry count: %v\n", err)
		}
		return retryErr
	}

	// Mark as resolved
	now := time.Now()
	if err := db.Model(&dlq).Updates(map[string]interface{}{
		"status":      "resolved",
		"resolved_at": now,
		"metadata": map[string]interface{}{
			"retried_task_id": taskID,
			"resolved_at":     now.UTC().Format(time.RFC3339),
		},
	}).Error; err != nil {
		log.Printf("ERROR: Failed to mark DLQ task as resolved: %v\n", err)
		return err
	}

	log.Printf("RESOLVED: DLQ task retried successfully (ID: %s, NewTaskID: %s)\n", dlq.ID, taskID)
	return nil
}

// DiscardDeadLetterTask marks a task as discarded (unable to recover)
func DiscardDeadLetterTask(db *gorm.DB, dlqID uuid.UUID, reason string) error {
	now := time.Now()
	if err := db.Model(&DeadLetterQueue{}).Where("id = ?", dlqID).Updates(map[string]interface{}{
		"status":      "discarded",
		"resolved_at": now,
		"metadata": map[string]interface{}{
			"discard_reason": reason,
			"discarded_at":   now.UTC().Format(time.RFC3339),
		},
	}).Error; err != nil {
		return err
	}

	log.Printf("DISCARDED: DLQ task marked as discarded (ID: %s, Reason: %s)\n", dlqID, reason)
	return nil
}
