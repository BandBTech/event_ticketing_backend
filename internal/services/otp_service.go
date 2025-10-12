package services

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"event-ticketing-backend/internal/redis"

	redislib "github.com/redis/go-redis/v9"
)

const (
	OTPExpiryTime = 10 * time.Minute // OTPs expire after 10 minutes
)

// OTPService handles OTP generation, storage and verification using Redis
type OTPService struct {
	redisClient *redislib.Client
}

// NewOTPService creates a new OTP service
func NewOTPService() *OTPService {
	return &OTPService{
		redisClient: redis.Client,
	}
}

// GenerateOTP generates a random n-digit OTP
func (s *OTPService) GenerateOTP(digits int) string {
	// Use crypto/rand for secure random number generation
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Define min and max values for the given number of digits
	min := int(pow10(digits - 1))
	max := int(pow10(digits) - 1)

	// Generate a random number in the range
	otp := r.Intn(max-min+1) + min

	return strconv.Itoa(otp)
}

// SaveOTP saves an OTP to Redis with an expiry time
func (s *OTPService) SaveOTP(identifier string, otpType string, otp string) error {
	ctx := context.Background()
	key := fmt.Sprintf("%s:%s", otpType, identifier)

	// Store OTP in Redis with expiry
	err := s.redisClient.Set(ctx, key, otp, OTPExpiryTime).Err()
	if err != nil {
		return fmt.Errorf("failed to save OTP: %w", err)
	}

	return nil
}

// VerifyOTP checks if the provided OTP is valid
func (s *OTPService) VerifyOTP(identifier string, otpType string, otp string) (bool, error) {
	ctx := context.Background()
	key := fmt.Sprintf("%s:%s", otpType, identifier)

	// Get OTP from Redis
	storedOTP, err := s.redisClient.Get(ctx, key).Result()
	if err != nil {
		if err == redislib.Nil {
			// OTP doesn't exist or has expired
			return false, nil
		}
		return false, fmt.Errorf("failed to verify OTP: %w", err)
	}

	// Check if OTP matches
	if storedOTP == otp {
		// Delete OTP after successful verification to prevent reuse
		s.redisClient.Del(ctx, key)
		return true, nil
	}

	return false, nil
}

// InvalidateOTP removes an OTP from Redis
func (s *OTPService) InvalidateOTP(identifier string, otpType string) error {
	ctx := context.Background()
	key := fmt.Sprintf("%s:%s", otpType, identifier)

	// Delete OTP from Redis
	err := s.redisClient.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to invalidate OTP: %w", err)
	}

	return nil
}

// Helper function to calculate powers of 10
func pow10(n int) int64 {
	result := int64(1)
	for i := 0; i < n; i++ {
		result *= 10
	}
	return result
}

// OTP Types
const (
	OTPTypeRegistration        = "registration"
	OTPTypePasswordReset       = "password_reset"
	OTPTypePhoneVerification   = "phone_verification"
	OTPTypeTwoFactorAuth       = "2fa"
	OTPTypePaymentConfirmation = "payment"
)
