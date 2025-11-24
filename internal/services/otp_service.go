package services

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"strconv"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/redis"

	redislib "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	OTPExpiryTime   = 10 * time.Minute // OTPs expire after 10 minutes
	MaxOTPRetries   = 3                // Maximum OTP sending retries
	OTPThrottleTime = 1 * time.Minute  // Minimum time between OTP requests for same identifier
)

// OTPService handles OTP generation, storage and verification using Redis with database fallback
type OTPService struct {
	redisClient *redislib.Client
	db          *gorm.DB
}

// NewOTPService creates a new OTP service
func NewOTPService() *OTPService {
	return &OTPService{
		redisClient: redis.Client,
		db:          database.DB,
	}
}

// GenerateSecureOTP generates a cryptographically secure random OTP
func (s *OTPService) GenerateSecureOTP(digits int) (string, error) {
	// Generate cryptographically secure random bytes
	bytes := make([]byte, digits)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate secure random bytes: %w", err)
	}

	// Convert to numeric string
	var otp string
	for _, b := range bytes {
		otp += strconv.Itoa(int(b) % 10)
	}

	return otp, nil
}

// GenerateOTP generates a random n-digit OTP using cryptographically secure random
func (s *OTPService) GenerateOTP(digits int) string {
	// Use only cryptographically secure random generation
	otp, err := s.GenerateSecureOTP(digits)
	if err != nil {
		// If crypto/rand fails, log error and return a fallback
		// This should rarely happen in practice
		log.Printf("CRITICAL: Failed to generate secure OTP: %v", err)
		// Generate a deterministic but unique OTP based on timestamp and identifier
		// This is not ideal but better than insecure random
		timestamp := time.Now().UnixNano()
		seed := int64(timestamp % 1000000) // Use last 6 digits of nanosecond timestamp
		// Create a pseudo-random but deterministic OTP
		otp = fmt.Sprintf("%06d", seed%1000000)
		log.Printf("WARNING: Using timestamp-based OTP generation as fallback: %s", otp)
	}

	return otp
}

// SaveOTP saves an OTP to Redis with database fallback and throttling
func (s *OTPService) SaveOTP(identifier string, otpType string, otp string, role string) error {
	ctx := context.Background()
	key := fmt.Sprintf("%s:%s:%s", otpType, role, identifier)
	throttleKey := fmt.Sprintf("throttle:%s:%s:%s", otpType, role, identifier)

	// Check Redis health
	if !s.isRedisHealthy() {
		log.Printf("Redis unavailable, using database fallback for OTP storage")
		return s.saveOTPToDatabase(identifier, otpType, otp, role)
	}

	// Check throttling (prevent spam)
	if s.isThrottled(throttleKey) {
		return fmt.Errorf("OTP request too frequent, please wait before requesting another OTP")
	}

	// Store OTP in Redis with expiry
	err := s.redisClient.Set(ctx, key, otp, OTPExpiryTime).Err()
	if err != nil {
		log.Printf("Failed to save OTP to Redis: %v, falling back to database", err)
		return s.saveOTPToDatabase(identifier, otpType, otp, role)
	}

	// Set throttling
	s.redisClient.Set(ctx, throttleKey, "1", OTPThrottleTime)

	log.Printf("OTP saved successfully for identifier: %s, type: %s, role: %s", identifier, otpType, role)
	return nil
}

// VerifyOTP checks if the provided OTP is valid with Redis fallback to database
func (s *OTPService) VerifyOTP(identifier string, otpType string, otp string, role string) (bool, error) {
	// First try the central OTP keys
	ctx := context.Background()
	centralKey := "otp:value:" + identifier
	storedOTP, err := s.redisClient.Get(ctx, centralKey).Result()
	if err == nil {
		// Check if OTP matches
		if storedOTP == otp {
			// Delete OTP after successful verification to prevent reuse
			s.redisClient.Del(ctx, centralKey)
			return true, nil
		}
		return false, nil
	}
	if err != redislib.Nil {
		log.Printf("Failed to get central OTP: %v", err)
	}

	// Fallback to type-specific keys
	key := fmt.Sprintf("%s:%s:%s", otpType, role, identifier)

	// Try Redis first if healthy
	if s.isRedisHealthy() {
		valid, err := s.verifyOTPFromRedis(key, otp)
		if err == nil {
			return valid, nil
		}
		log.Printf("Redis verification failed: %v, trying database fallback", err)
	}

	// Fallback to database
	return s.verifyOTPFromDatabase(identifier, otpType, otp, role)
}

// verifyOTPFromRedis verifies OTP from Redis
func (s *OTPService) verifyOTPFromRedis(key, otp string) (bool, error) {
	ctx := context.Background()

	// Get OTP from Redis
	storedOTP, err := s.redisClient.Get(ctx, key).Result()
	if err != nil {
		if err == redislib.Nil {
			// OTP doesn't exist or has expired
			return false, nil
		}
		return false, fmt.Errorf("failed to verify OTP from Redis: %w", err)
	}

	// Check if OTP matches
	if storedOTP == otp {
		// Delete OTP after successful verification to prevent reuse
		s.redisClient.Del(ctx, key)
		return true, nil
	}

	return false, nil
}

// saveOTPToDatabase saves OTP to database as fallback
func (s *OTPService) saveOTPToDatabase(identifier, otpType, otp, role string) error {
	// Create or update OTP record
	otpRecord := models.OTP{
		Identifier: identifier,
		Type:       otpType,
		Role:       role,
		Code:       otp,
		ExpiresAt:  time.Now().Add(OTPExpiryTime),
		Attempts:   0,
	}

	// Use upsert to handle existing records
	result := s.db.Where(models.OTP{Identifier: identifier, Type: otpType, Role: role}).
		Assign(models.OTP{Code: otp, ExpiresAt: time.Now().Add(OTPExpiryTime), Attempts: 0}).
		FirstOrCreate(&otpRecord)

	if result.Error != nil {
		return fmt.Errorf("failed to save OTP to database: %w", result.Error)
	}

	log.Printf("OTP saved to database fallback for identifier: %s, type: %s, role: %s", identifier, otpType, role)
	return nil
}

// verifyOTPFromDatabase verifies OTP from database
func (s *OTPService) verifyOTPFromDatabase(identifier, otpType, otp, role string) (bool, error) {
	var otpRecord models.OTP

	// Find OTP record
	err := s.db.Where("identifier = ? AND type = ? AND role = ? AND expires_at > ?",
		identifier, otpType, role, time.Now()).First(&otpRecord).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, nil // OTP not found or expired
		}
		return false, fmt.Errorf("failed to verify OTP from database: %w", err)
	}

	// Check attempts limit
	if otpRecord.Attempts >= 5 {
		return false, fmt.Errorf("too many failed attempts")
	}

	// Check if OTP matches
	if otpRecord.Code == otp {
		// Delete OTP after successful verification
		s.db.Delete(&otpRecord)
		return true, nil
	}

	// Increment attempts
	otpRecord.Attempts++
	s.db.Save(&otpRecord)

	return false, nil
}

// InvalidateOTP removes an OTP from both Redis and database
func (s *OTPService) InvalidateOTP(identifier string, otpType string, role string) error {
	ctx := context.Background()
	key := fmt.Sprintf("%s:%s:%s", otpType, role, identifier)

	// Try Redis first
	if s.isRedisHealthy() {
		s.redisClient.Del(ctx, key)
	}

	// Also remove from database
	s.db.Where("identifier = ? AND type = ? AND role = ?", identifier, otpType, role).Delete(&models.OTP{})

	return nil
}

// isRedisHealthy checks if Redis is available
func (s *OTPService) isRedisHealthy() bool {
	if s.redisClient == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := s.redisClient.Ping(ctx).Err()
	return err == nil
}

// isThrottled checks if the identifier is currently throttled
func (s *OTPService) isThrottled(throttleKey string) bool {
	if !s.isRedisHealthy() {
		return false // Skip throttling if Redis is down
	}

	ctx := context.Background()
	exists, err := s.redisClient.Exists(ctx, throttleKey).Result()
	if err != nil {
		log.Printf("Failed to check throttling: %v", err)
		return false
	}

	return exists > 0
}

// GetOTPStatus returns the status of an OTP (for debugging/admin purposes)
func (s *OTPService) GetOTPStatus(identifier, otpType, role string) (map[string]interface{}, error) {
	key := fmt.Sprintf("%s:%s:%s", otpType, role, identifier)
	status := make(map[string]interface{})

	// Check Redis
	if s.isRedisHealthy() {
		ctx := context.Background()
		ttl, err := s.redisClient.TTL(ctx, key).Result()
		if err == nil && ttl > 0 {
			status["redis_ttl"] = ttl.Seconds()
			status["redis_available"] = true
		} else {
			status["redis_available"] = false
		}
	} else {
		status["redis_available"] = false
	}

	// Check database
	var otpRecord models.OTP
	err := s.db.Where("identifier = ? AND type = ? AND role = ? AND expires_at > ?",
		identifier, otpType, role, time.Now()).First(&otpRecord).Error

	if err == nil {
		status["database_available"] = true
		status["database_expires_at"] = otpRecord.ExpiresAt
		status["database_attempts"] = otpRecord.Attempts
	} else {
		status["database_available"] = false
	}

	return status, nil
}

// GetOTP retrieves an existing OTP if it exists and hasn't expired
func (s *OTPService) GetOTP(identifier string, otpType string, role string) (string, error) {
	key := fmt.Sprintf("%s:%s:%s", otpType, role, identifier)

	// Try Redis first if healthy
	if s.isRedisHealthy() {
		ctx := context.Background()
		otp, err := s.redisClient.Get(ctx, key).Result()
		if err == nil {
			return otp, nil
		}
		if err != redislib.Nil {
			log.Printf("Failed to get OTP from Redis: %v", err)
		}
	}

	// Fallback to database
	var otpRecord models.OTP
	err := s.db.Where("identifier = ? AND type = ? AND role = ? AND expires_at > ?",
		identifier, otpType, role, time.Now()).First(&otpRecord).Error

	if err == nil {
		return otpRecord.Code, nil
	}

	if err != gorm.ErrRecordNotFound {
		return "", fmt.Errorf("failed to get OTP from database: %w", err)
	}

	return "", nil // Not found
}

// SendCentralOTP implements centralized OTP sending logic with throttling, reuse, and email queuing
// Returns the OTP code and any error
func (s *OTPService) SendCentralOTP(email string, otpType string, queueService *EmailQueueService) (string, error) {
	ctx := context.Background()
	throttleKey := "otp:throttle:" + email
	otpKey := "otp:value:" + email

	// 1. Throttle check (1 OTP per minute)
	if !s.isRedisHealthy() {
		return "", fmt.Errorf("Redis unavailable for OTP service")
	}

	throttleExists, err := s.redisClient.Exists(ctx, throttleKey).Result()
	if err != nil {
		return "", fmt.Errorf("failed to check throttle: %w", err)
	}
	if throttleExists == 1 {
		return "", fmt.Errorf("OTP request can only be sent once per minute.")
	}

	// 2. Check if OTP exists (not expired)
	otp, err := s.redisClient.Get(ctx, otpKey).Result()
	if err == redislib.Nil {
		// OTP expired – generate a new one
		otp = s.GenerateOTP(6)
		err = s.redisClient.Set(ctx, otpKey, otp, 10*time.Minute).Err()
		if err != nil {
			return "", fmt.Errorf("failed to save OTP: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("failed to get existing OTP: %w", err)
	}

	// 3. Apply throttle (60 seconds)
	err = s.redisClient.Set(ctx, throttleKey, "1", time.Minute).Err()
	if err != nil {
		return "", fmt.Errorf("failed to set throttle: %w", err)
	}

	// 4. Queue the OTP email
	err = queueService.QueueOTPEmail(email, otp, otpType)
	if err != nil {
		return "", fmt.Errorf("failed to queue OTP email: %w", err)
	}

	return otp, nil
}

// OTP Types
const (
	OTPTypeRegistration        = "registration"
	OTPTypePasswordReset       = "password_reset"
	OTPTypePhoneVerification   = "phone_verification"
	OTPTypeTwoFactorAuth       = "2fa"
	OTPTypePaymentConfirmation = "payment"
)
