package services

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"event-ticketing-backend/internal/models"
)

// GenerateAndSendOTP is a unified function for generating and sending OTPs with role validation
func (s *AuthService) GenerateAndSendOTP(req *models.OTPSendRequest) (*models.OTPResponse, error) {
	log.Printf("Generating and sending OTP for identifier: %s, type: %s, role: %s", req.Identifier, req.OTPType, req.Role)

	// Validate identifier and role
	if req.Identifier == "" {
		return nil, errors.New("identifier is required")
	}
	if req.Role == "" {
		return nil, errors.New("role is required")
	}

	// Validate OTP type
	validTypes := map[string]bool{
		"registration":       true,
		"password_reset":     true,
		"phone_verification": true,
		"2fa":                true,
	}
	if !validTypes[req.OTPType] {
		return nil, fmt.Errorf("invalid OTP type: %s", req.OTPType)
	}

	switch req.OTPType {
	case "password_reset":
		// Find user by email first
		var user models.User
		if err := s.db.Preload("Roles").Where("email = ?", strings.ToLower(req.Identifier)).First(&user).Error; err != nil {
			// For security, return generic success message
			return &models.OTPResponse{
				Success:   true,
				Message:   "If this account exists with the specified role, an OTP has been sent.",
				ExpiresIn: int(OTPExpiryTime.Seconds()),
			}, nil
		}

		// Check if user has access to the requested panel based on role
		hasAccess := false
		for _, userRole := range user.Roles {
			// Admin panel access: admin or subadmin
			if req.Role == "admin" && (userRole.Name == "admin" || userRole.Name == "subadmin") {
				hasAccess = true
				break
			}
			// Organizer panel access: organizer, staff, or manager
			if req.Role == "organizer" && (userRole.Name == "organizer" || userRole.Name == "staff" || userRole.Name == "manager") {
				hasAccess = true
				break
			}
			// User panel access: user only
			if req.Role == "user" && userRole.Name == "user" {
				hasAccess = true
				break
			}
		}

		if !hasAccess {
			// For security, return generic success message
			return &models.OTPResponse{
				Success:   true,
				Message:   "If this account exists with the specified role, an OTP has been sent.",
				ExpiresIn: int(OTPExpiryTime.Seconds()),
			}, nil
		}

		// Generate and save OTP with role
		otp := s.otpService.GenerateOTP(6)
		if err := s.otpService.SaveOTP(user.Email, "password_reset", otp, req.Role); err != nil {
			log.Printf("Failed to save password reset OTP: %v", err)
			return nil, fmt.Errorf("failed to save OTP: %w", err)
		}

		// Queue OTP for sending
		if err := s.otpQueueService.QueuePasswordResetOTP(user.Email, otp); err != nil {
			log.Printf("Failed to queue password reset OTP: %v", err)
			return nil, fmt.Errorf("failed to send OTP: %w", err)
		}

		return &models.OTPResponse{
			Success:   true,
			Message:   "Password reset OTP sent successfully.",
			ExpiresIn: int(OTPExpiryTime.Seconds()),
		}, nil

	case "registration":
		// Find user by email and validate panel access
		var user models.User
		if err := s.db.Preload("Roles").Where("email = ?", strings.ToLower(req.Identifier)).First(&user).Error; err != nil {
			// For security, return generic error
			return &models.OTPResponse{
				Success:   true,
				Message:   "If this account exists with the specified role, an OTP has been sent.",
				ExpiresIn: int(OTPExpiryTime.Seconds()),
			}, nil
		}

		// Check if user has access to the requested panel based on role
		hasAccess := false
		for _, userRole := range user.Roles {
			// Admin panel access: admin or subadmin
			if req.Role == "admin" && (userRole.Name == "admin" || userRole.Name == "subadmin") {
				hasAccess = true
				break
			}
			// Organizer panel access: organizer, staff, or manager
			if req.Role == "organizer" && (userRole.Name == "organizer" || userRole.Name == "staff" || userRole.Name == "manager") {
				hasAccess = true
				break
			}
			// User panel access: user only
			if req.Role == "user" && userRole.Name == "user" {
				hasAccess = true
				break
			}
		}

		if !hasAccess {
			// For security, return generic success message
			return &models.OTPResponse{
				Success:   true,
				Message:   "If this account exists with the specified role, an OTP has been sent.",
				ExpiresIn: int(OTPExpiryTime.Seconds()),
			}, nil
		}

		// Generate and save OTP with role
		otp := s.otpService.GenerateOTP(6)
		if err := s.otpService.SaveOTP(user.Email, "registration", otp, req.Role); err != nil {
			log.Printf("Failed to save registration OTP: %v", err)
			return nil, fmt.Errorf("failed to save OTP: %w", err)
		}

		// Queue OTP for sending
		if err := s.otpQueueService.QueueRegistrationOTP(user.Email, otp); err != nil {
			log.Printf("Failed to queue registration OTP: %v", err)
			return nil, fmt.Errorf("failed to send OTP: %w", err)
		}

		return &models.OTPResponse{
			Success:   true,
			Message:   "Registration OTP sent successfully",
			ExpiresIn: int(OTPExpiryTime.Seconds()),
		}, nil

	case "phone_verification":
		// Handle SMS OTP sending if implemented
		log.Printf("SMS OTP not yet implemented for identifier: %s", req.Identifier)
		return nil, fmt.Errorf("SMS OTP not yet implemented")

	case "2fa":
		// Find user by email and validate panel access for 2FA
		var user models.User
		if err := s.db.Preload("Roles").Where("email = ?", strings.ToLower(req.Identifier)).First(&user).Error; err != nil {
			return nil, errors.New("User not found")
		}

		// Check if user has access to the requested panel based on role
		hasAccess := false
		for _, userRole := range user.Roles {
			// Admin panel access: admin or subadmin
			if req.Role == "admin" && (userRole.Name == "admin" || userRole.Name == "subadmin") {
				hasAccess = true
				break
			}
			// Organizer panel access: organizer, staff, or manager
			if req.Role == "organizer" && (userRole.Name == "organizer" || userRole.Name == "staff" || userRole.Name == "manager") {
				hasAccess = true
				break
			}
			// User panel access: user only
			if req.Role == "user" && userRole.Name == "user" {
				hasAccess = true
				break
			}
		}

		if !hasAccess {
			return nil, errors.New("Invalid role for this user")
		}

		// Generate and save OTP with role
		otp := s.otpService.GenerateOTP(6)
		if err := s.otpService.SaveOTP(user.Email, "2fa", otp, req.Role); err != nil {
			return nil, fmt.Errorf("failed to save 2FA OTP: %w", err)
		}

		// Send 2FA OTP
		if err := s.sendTwoFactorOTPEmail(user.Email, otp); err != nil {
			return nil, fmt.Errorf("failed to send 2FA OTP: %w", err)
		}

		return &models.OTPResponse{
			Success:   true,
			Message:   "2FA OTP sent successfully",
			ExpiresIn: int(OTPExpiryTime.Seconds()),
		}, nil

	default:
		return nil, fmt.Errorf("unknown OTP type: %s", req.OTPType)
	}
}

// sendTwoFactorOTPEmail sends an email with 2FA OTP
func (s *AuthService) sendTwoFactorOTPEmail(email string, otp string) error {
	return s.emailQueueService.QueueOTPEmail(email, otp, "2fa")
}
