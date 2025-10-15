package services

import (
	"errors"
	"fmt"

	"event-ticketing-backend/internal/models"
)

// OTPExpiryTime is defined in otp_service.go and used here

// GenerateAndSendOTP is a unified function for generating and sending OTPs
func (s *AuthService) GenerateAndSendOTP(req *models.OTPSendRequest) (*models.OTPResponse, error) {
	// Validate identifier
	if req.Identifier == "" {
		return nil, errors.New("Identifier is required")
	}

	// Generate OTP
	otp := s.otpService.GenerateOTP(6) // 6-digit OTP

	// Save OTP to Redis
	if err := s.otpService.SaveOTP(req.Identifier, req.OTPType, otp); err != nil {
		return nil, fmt.Errorf("failed to save OTP: %w", err)
	}

	// Handle sending OTP based on type
	var err error
	switch req.OTPType {
	case "registration":
		err = s.sendVerificationOTPEmail(req.Identifier, otp)
	case "password_reset":
		err = s.sendPasswordResetOTPEmail(req.Identifier, otp)
	case "phone_verification":
		// Handle SMS OTP sending if implemented
		err = fmt.Errorf("SMS OTP not yet implemented")
	case "2fa":
		err = s.sendTwoFactorOTPEmail(req.Identifier, otp)
	default:
		err = fmt.Errorf("unknown OTP type: %s", req.OTPType)
	}

	if err != nil {
		return nil, err
	}

	// Return success response with expiry time
	return &models.OTPResponse{
		Success:   true,
		Message:   fmt.Sprintf("OTP sent to %s", req.Identifier),
		ExpiresIn: int(OTPExpiryTime.Seconds()),
	}, nil
}

// sendTwoFactorOTPEmail sends an email with 2FA OTP
func (s *AuthService) sendTwoFactorOTPEmail(email string, otp string) error {
	return s.emailQueueService.QueueOTPEmail(email, otp, "2fa")
}
