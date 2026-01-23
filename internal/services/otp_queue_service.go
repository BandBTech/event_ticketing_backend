package services

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/hibiken/asynq"
)

// OTPQueueService handles OTP job queuing with retry mechanisms
type OTPQueueService struct {
	client *asynq.Client
}

// NewOTPQueueService creates a new OTP queue service
func NewOTPQueueService(cfg *config.Config) *OTPQueueService {
	// Convert DB string to int for Asynq
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

	client := asynq.NewClient(redisOpts)

	return &OTPQueueService{
		client: client,
	}
}

// QueueOTP queues an OTP sending job with retry logic
func (s *OTPQueueService) QueueOTP(identifier, otp, otpType string) error {
	otpJob := &models.OTPJob{
		Identifier: identifier,
		OTP:        otp,
		Type:       otpType,
		Attempts:   0,
		MaxRetries: 3,
		CreatedAt:  time.Now(),
	}

	// Serialize the OTP job
	payload, err := json.Marshal(otpJob)
	if err != nil {
		return utils.NewInternalServerError("Failed to marshal OTP job.", err)
	}

	// Create Asynq task
	task := asynq.NewTask("otp:send", payload)

	// Set task options with high priority for OTPs
	opts := []asynq.Option{
		asynq.MaxRetry(otpJob.MaxRetries),
		asynq.Queue("queue:otp:urgent"), // Highest priority queue for OTPs
		asynq.Timeout(30 * time.Second), // Timeout for OTP sending
	}

	// Add process after time if specified (for rate limiting)
	if otpJob.ProcessAfter.IsZero() {
		// Add a small delay to prevent immediate processing
		opts = append(opts, asynq.ProcessIn(2*time.Second))
	} else {
		opts = append(opts, asynq.ProcessAt(otpJob.ProcessAfter))
	}

	// Enqueue the task
	info, err := s.client.Enqueue(task, opts...)
	if err != nil {
		return utils.NewExternalServiceError("Asynq Queue", "Failed to enqueue OTP task.", err)
	}

	log.Printf("OTP job queued successfully: ID=%s, Queue=%s, Type=%s, To=%s",
		info.ID, info.Queue, otpJob.Type, otpJob.Identifier)

	return nil
}

// QueueRegistrationOTP queues a registration OTP
func (s *OTPQueueService) QueueRegistrationOTP(identifier, otp string) error {
	return s.QueueOTP(identifier, otp, "registration")
}

// QueuePasswordResetOTP queues a password reset OTP
func (s *OTPQueueService) QueuePasswordResetOTP(identifier, otp string) error {
	return s.QueueOTP(identifier, otp, "password_reset")
}

// QueueTwoFactorOTP queues a 2FA OTP
func (s *OTPQueueService) QueueTwoFactorOTP(identifier, otp string) error {
	return s.QueueOTP(identifier, otp, "2fa")
}

// Close closes the client connection
func (s *OTPQueueService) Close() error {
	return s.client.Close()
}
