package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AuthService provides authentication functionality
type AuthService struct {
	db                *gorm.DB
	jwtConfig         *config.JWTConfig
	jwtService        *utils.JWTService
	emailQueueService *EmailQueueService
	otpService        *OTPService
}

// NewAuthService creates a new authentication service
func NewAuthService(cfg *config.Config) *AuthService {
	emailQueueService := NewEmailQueueService(cfg)
	return &AuthService{
		db:                database.DB,
		jwtConfig:         &cfg.JWT,
		jwtService:        utils.NewJWTService(&cfg.JWT),
		emailQueueService: emailQueueService,
		otpService:        NewOTPService(),
	}

}

// Register creates a new user account
func (s *AuthService) Register(req *models.CreateUserRequest) (*models.UserResponse, error) {
	// Check if user already exists
	var existingUser models.User
	if result := s.db.Where("email = ?", strings.ToLower(req.Email)).First(&existingUser); result.Error == nil {
		return nil, errors.New("User with this email already exists")
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, result.Error
	}

	// Create a new user
	user := models.User{
		Email:     strings.ToLower(req.Email),
		FirstName: req.FirstName,
		LastName:  req.LastName,
	}

	// Hash the password
	if err := user.HashPassword(req.Password); err != nil {
		return nil, err
	}

	// Get user role
	var userRole models.Role
	if err := s.db.Where("name = ?", "user").First(&userRole).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Create default user role if not exists
			userRole = models.Role{
				Name:        "user",
				Description: "Default user role",
			}
			if err := s.db.Create(&userRole).Error; err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	// Assign user role
	user.Roles = []*models.Role{&userRole}

	// Save user to database in a transaction
	tx := s.db.Begin()
	if err := tx.Create(&user).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	// Generate and send OTP for email verification
	otp := s.otpService.GenerateOTP(6) // 6-digit OTP
	if err := s.otpService.SaveOTP(user.Email, "registration", otp); err != nil {
		// Log the error but don't fail the registration
		fmt.Printf("Failed to save registration OTP: %v\n", err)
	}

	// Send verification email with OTP
	if err := s.sendVerificationOTPEmail(user.Email, otp); err != nil {
		// Log the error but don't fail the registration
		fmt.Printf("Failed to send verification email with OTP: %v\n", err)
	}

	// Return user data (excluding sensitive information)
	resp := user.ToResponse()
	return &resp, nil
}

// Login authenticates a user and returns JWT tokens
func (s *AuthService) Login(req *models.LoginRequest) (*models.TokenResponse, error) {
	// Find user by email
	var user models.User
	if err := s.db.Preload("Roles.Permissions").Where("email = ?", strings.ToLower(req.Email)).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("Invalid email or password")
		}
		return nil, err
	}

	// Verify password
	if !user.CheckPassword(req.Password) {
		return nil, errors.New("Invalid email or password")
	}

	// Generate tokens
	tokenResponse, err := s.jwtService.GenerateTokens(&user)
	if err != nil {
		return nil, err
	}

	// Store refresh token in database
	refreshTokenHash := utils.HashToken(tokenResponse.RefreshToken)
	refreshToken := models.Token{
		UserID:    user.ID,
		TokenHash: refreshTokenHash,
		Type:      models.RefreshToken,
		ExpiresAt: time.Now().Add(s.jwtConfig.RefreshTokenTTL),
	}
	if err := s.db.Create(&refreshToken).Error; err != nil {
		return nil, err
	}

	return tokenResponse, nil
}

// RefreshToken generates new access and refresh tokens using a valid refresh token
func (s *AuthService) RefreshToken(req *models.RefreshTokenRequest) (*models.TokenResponse, error) {
	// Validate refresh token
	claims, err := s.jwtService.ValidateToken(req.RefreshToken)
	if err != nil {
		return nil, err
	}

	// Check if token exists in database and is not revoked
	refreshTokenHash := utils.HashToken(req.RefreshToken)
	var token models.Token
	if err := s.db.Where("token_hash = ? AND type = ? AND revoked = ? AND expires_at > ?",
		refreshTokenHash,
		models.RefreshToken,
		false,
		time.Now()).First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("Invalid or expired refresh token")
		}
		return nil, err
	}

	// Get user
	var user models.User
	if err := s.db.Preload("Roles.Permissions").Where("id = ?", claims.UserID).First(&user).Error; err != nil {
		return nil, err
	}

	// Generate new tokens
	tokenResponse, err := s.jwtService.GenerateTokens(&user)
	if err != nil {
		return nil, err
	}

	// Revoke old refresh token
	if err := s.db.Model(&token).Update("revoked", true).Error; err != nil {
		return nil, err
	}

	// Store new refresh token
	newRefreshTokenHash := utils.HashToken(tokenResponse.RefreshToken)
	newRefreshToken := models.Token{
		UserID:    user.ID,
		TokenHash: newRefreshTokenHash,
		Type:      models.RefreshToken,
		ExpiresAt: time.Now().Add(s.jwtConfig.RefreshTokenTTL),
	}
	if err := s.db.Create(&newRefreshToken).Error; err != nil {
		return nil, err
	}

	return tokenResponse, nil
}

// VerifyEmail verifies a user's email using the verification code
func (s *AuthService) VerifyEmail(req *models.VerifyEmailRequest) error {
	// This method is kept for backward compatibility
	// New code should use VerifyOTP instead

	// Find user by verification code
	var user models.User
	if err := s.db.Where("verification_code = ?", req.VerificationCode).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("Invalid verification code")
		}
		return err
	}

	// Mark email as verified and clear verification code
	user.IsEmailVerified = true
	user.VerificationCode = ""

	if err := s.db.Save(&user).Error; err != nil {
		return err
	}

	return nil
}

// VerifyOTP verifies an OTP for a given purpose
func (s *AuthService) VerifyOTP(req *models.OTPVerifyRequest) error {
	// Verify OTP
	valid, err := s.otpService.VerifyOTP(req.Identifier, req.OTPType, req.OTPCode)
	if err != nil {
		return fmt.Errorf("error verifying OTP: %w", err)
	}

	if !valid {
		return errors.New("Invalid or expired OTP")
	}

	// Handle specific OTP types
	switch req.OTPType {
	case "registration":
		return s.handleRegistrationOTPVerification(req.Identifier)
	case "password_reset":
		return nil // Password reset requires additional steps, handled separately
	default:
		return nil // Other OTP types may not need further handling
	}
}

// handleRegistrationOTPVerification marks the user's email as verified after OTP validation
func (s *AuthService) handleRegistrationOTPVerification(email string) error {
	var user models.User
	if err := s.db.Where("email = ?", email).First(&user).Error; err != nil {
		return err
	}

	// Mark email as verified
	user.IsEmailVerified = true

	if err := s.db.Save(&user).Error; err != nil {
		return err
	}

	return nil
}

// SendPasswordResetEmail sends a password reset OTP to the user's email
func (s *AuthService) SendPasswordResetEmail(req *models.ResetPasswordRequest) error {
	// Find user by email
	var user models.User
	if err := s.db.Where("email = ?", strings.ToLower(req.Email)).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// For security reasons, don't reveal that the email doesn't exist
			return nil
		}
		return err
	}

	// Generate OTP for password reset
	otp := s.otpService.GenerateOTP(6) // 6-digit OTP

	// Save OTP to Redis with password_reset type
	if err := s.otpService.SaveOTP(user.Email, "password_reset", otp); err != nil {
		return fmt.Errorf("failed to save password reset OTP: %w", err)
	}

	// Send password reset email with OTP
	if err := s.sendPasswordResetOTPEmail(user.Email, otp); err != nil {
		return err
	}

	return nil
}

// sendPasswordResetOTPEmail sends an email with the password reset OTP
func (s *AuthService) sendPasswordResetOTPEmail(email string, otp string) error {
	return s.emailQueueService.QueuePasswordResetOTP(email, otp)
}

// ResetPassword resets a user's password using a reset token or OTP
func (s *AuthService) ResetPassword(req *models.UpdatePasswordRequest) error {
	// Check if this is a token-based reset (legacy)
	var token models.Token
	tokenErr := s.db.Where("token_hash = ? AND type = ? AND revoked = ? AND expires_at > ?",
		req.ResetToken,
		"reset",
		false,
		time.Now()).First(&token).Error

	// If token is found, proceed with legacy method
	if tokenErr == nil {
		// Find user
		var user models.User
		if err := s.db.Where("id = ?", token.UserID).First(&user).Error; err != nil {
			return err
		}

		// Update password
		if err := user.HashPassword(req.NewPassword); err != nil {
			return err
		}

		// Start transaction
		tx := s.db.Begin()

		// Save user
		if err := tx.Save(&user).Error; err != nil {
			tx.Rollback()
			return err
		}

		// Revoke token
		if err := tx.Model(&token).Update("revoked", true).Error; err != nil {
			tx.Rollback()
			return err
		}

		// Commit transaction
		if err := tx.Commit().Error; err != nil {
			return err
		}

		return nil
	}

	// For OTP-based reset, we need to verify the OTP first
	// The OTP code is in req.ResetToken and email is in req.EmailToken
	if req.EmailToken == "" || req.ResetToken == "" {
		return errors.New("Email and OTP code are required for password reset")
	}

	// Verify the OTP before proceeding with password reset
	otpReq := &models.OTPVerifyRequest{
		Identifier: req.EmailToken,
		OTPCode:    req.ResetToken,
		OTPType:    "password_reset",
	}

	if err := s.VerifyOTP(otpReq); err != nil {
		return errors.New("Invalid or expired OTP code")
	}

	// OTP is valid, now proceed with password reset
	var user models.User
	if err := s.db.Where("email = ?", req.EmailToken).First(&user).Error; err != nil {
		return errors.New("User not found")
	}

	// Update password
	if err := user.HashPassword(req.NewPassword); err != nil {
		return err
	}

	// Save user
	if err := s.db.Save(&user).Error; err != nil {
		return err
	}

	return nil
}

// Logout revokes a user's refresh tokens
func (s *AuthService) Logout(userID uuid.UUID, all bool) error {
	if all {
		// Revoke all refresh tokens for the user
		if err := s.db.Model(&models.Token{}).
			Where("user_id = ? AND type = ? AND revoked = ?", userID, models.RefreshToken, false).
			Update("revoked", true).Error; err != nil {
			return err
		}
	} else {
		// If token hash is provided, only revoke that specific token
		// This feature would require passing the refresh token to the logout endpoint
	}

	return nil
}

// GetUserByID retrieves a user by ID
func (s *AuthService) GetUserByID(userID uuid.UUID) (*models.User, error) {
	var user models.User
	if err := s.db.Preload("Roles.Permissions").Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// UpdateProfile updates user profile information
func (s *AuthService) UpdateProfile(userID uuid.UUID, req *models.UpdateProfileRequest) (*models.UserProfileResponse, error) {
	// Get user first
	var user models.User
	if err := s.db.Preload("Organization").Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}

	// Update user fields (email cannot be changed via this endpoint)
	user.FirstName = req.FirstName
	user.LastName = req.LastName
	user.Phone = req.Phone

	// Save user
	if err := s.db.Save(&user).Error; err != nil {
		return nil, err
	}

	response := user.ToProfileResponse()
	return &response, nil
}

// ChangePassword changes user password (for authenticated users)
func (s *AuthService) ChangePassword(userID uuid.UUID, req *models.ChangePasswordRequest) error {
	// Get user
	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		return err
	}

	// Verify current password
	if !user.CheckPassword(req.CurrentPassword) {
		return errors.New("Current password is incorrect")
	}

	// Hash new password
	if err := user.HashPassword(req.NewPassword); err != nil {
		return err
	}

	// Save user
	if err := s.db.Save(&user).Error; err != nil {
		return err
	}

	return nil
}

// Send verification email with OTP
func (s *AuthService) sendVerificationOTPEmail(email string, otp string) error {
	return s.emailQueueService.QueueRegistrationOTP(email, otp)
}
