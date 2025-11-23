package services

import (
	"context"
	"encoding/json"
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
	otpQueueService   *OTPQueueService
	otpService        *OTPService
}

// NewAuthService creates a new authentication service
func NewAuthService(cfg *config.Config) *AuthService {
	emailQueueService := NewEmailQueueService(cfg)
	otpQueueService := NewOTPQueueService(cfg)
	return &AuthService{
		db:                database.DB,
		jwtConfig:         &cfg.JWT,
		jwtService:        utils.NewJWTService(&cfg.JWT),
		emailQueueService: emailQueueService,
		otpQueueService:   otpQueueService,
		otpService:        NewOTPService(),
	}

}

// Register creates a new user account with temporary storage and OTP sending
func (s *AuthService) Register(req *models.CreateUserRequest) error {
	// Check if user already exists
	var existingUser models.User
	if result := s.db.Where("email = ?", strings.ToLower(req.Email)).First(&existingUser); result.Error == nil {
		return errors.New("User with this email already exists")
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}

	// Use centralized OTP sending logic
	_, err := s.otpService.SendCentralOTP(strings.ToLower(req.Email), "registration", s.emailQueueService)
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	// Store temporary registration data in Redis
	tempData := map[string]interface{}{
		"firstName":   req.FirstName,
		"lastName":    req.LastName,
		"email":       strings.ToLower(req.Email),
		"phone":       req.Phone,
		"countryCode": req.CountryCode,
		"verified":    false,
		"type":        "user",
	}
	tempKey := fmt.Sprintf("temp:register:user:%s", strings.ToLower(req.Email))

	jsonData, err := json.Marshal(tempData)
	if err != nil {
		return fmt.Errorf("failed to marshal temp data: %w", err)
	}

	// Store in Redis with 10 minute expiry
	err = s.otpService.redisClient.Set(context.Background(), tempKey, jsonData, 10*time.Minute).Err()
	if err != nil {
		return fmt.Errorf("failed to store temp data: %w", err)
	}

	return nil
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

// LoginWithRoleCheck authenticates a user with role validation and returns JWT tokens
func (s *AuthService) LoginWithRoleCheck(req *models.LoginRequest, requiredRoles ...string) (*models.TokenResponse, error) {
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

	// Check if user has one of the required roles
	hasRole := false
	for _, userRole := range user.Roles {
		for _, requiredRole := range requiredRoles {
			if userRole.Name == requiredRole {
				hasRole = true
				break
			}
		}
		if hasRole {
			break
		}
	}
	if !hasRole {
		return nil, errors.New("Access denied: insufficient permissions")
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
	// Check if token exists in database and is not revoked (primary validation)
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

	// Get user using token's user ID
	var user models.User
	if err := s.db.Preload("Roles.Permissions").Where("id = ?", token.UserID).First(&user).Error; err != nil {
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
	valid, err := s.otpService.VerifyOTP(req.Identifier, req.OTPType, req.OTPCode, "auth")
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

// handleRegistrationOTPVerification marks the user's email as verified in temp data
func (s *AuthService) handleRegistrationOTPVerification(email string) error {
	// Try user first
	tempKey := fmt.Sprintf("temp:register:user:%s", strings.ToLower(email))
	tempJSON, err := s.otpService.redisClient.Get(context.Background(), tempKey).Result()
	if err != nil {
		// Try organizer
		tempKey = fmt.Sprintf("temp:register:organizer:%s", strings.ToLower(email))
		tempJSON, err = s.otpService.redisClient.Get(context.Background(), tempKey).Result()
		if err != nil {
			return fmt.Errorf("temp registration data not found: %w", err)
		}
	}

	var tempData map[string]interface{}
	if err := json.Unmarshal([]byte(tempJSON), &tempData); err != nil {
		return fmt.Errorf("failed to unmarshal temp data: %w", err)
	}

	// Set verified
	tempData["verified"] = true

	// Save back
	updatedJSON, err := json.Marshal(tempData)
	if err != nil {
		return fmt.Errorf("failed to marshal updated temp data: %w", err)
	}

	err = s.otpService.redisClient.Set(context.Background(), tempKey, updatedJSON, 10*time.Minute).Err()
	if err != nil {
		return fmt.Errorf("failed to update temp data: %w", err)
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

	// Use centralized OTP sending logic
	_, err := s.otpService.SendCentralOTP(strings.ToLower(req.Email), "password_reset", s.emailQueueService)
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}

// ResendRegistrationOTP resends the existing registration OTP or generates a new one if expired
func (s *AuthService) ResendRegistrationOTP(email string) error {
	// Check if temp data exists
	tempKey := fmt.Sprintf("temp:register:user:%s", strings.ToLower(email))
	_, err := s.otpService.redisClient.Get(context.Background(), tempKey).Result()
	if err != nil {
		// Try organizer
		tempKey = fmt.Sprintf("temp:register:organizer:%s", strings.ToLower(email))
		_, err = s.otpService.redisClient.Get(context.Background(), tempKey).Result()
		if err != nil {
			return errors.New("No registration session found")
		}
	}

	// Use centralized OTP sending logic
	_, err = s.otpService.SendCentralOTP(strings.ToLower(email), "registration", s.emailQueueService)
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}

// HasTempRegistrationData checks if temp registration data exists for the email
func (s *AuthService) HasTempRegistrationData(email string) bool {
	tempKey := fmt.Sprintf("temp:register:user:%s", strings.ToLower(email))
	exists, err := s.otpService.redisClient.Exists(context.Background(), tempKey).Result()
	if err == nil && exists > 0 {
		return true
	}
	tempKey = fmt.Sprintf("temp:register:organizer:%s", strings.ToLower(email))
	exists, err = s.otpService.redisClient.Exists(context.Background(), tempKey).Result()
	return err == nil && exists > 0
}

// sendPasswordResetOTPEmail sends an email with the password reset OTP
func (s *AuthService) sendPasswordResetOTPEmail(email string, otp string) error {
	return s.otpQueueService.QueuePasswordResetOTP(email, otp)
}

// ResetPassword resets a user's password using email (OTP verification happens separately)
func (s *AuthService) ResetPassword(req *models.UpdatePasswordRequest) error {

	// Email is required for password reset
	if req.EmailToken == "" {
		return errors.New("Email is required for password reset")
	}

	// Find user by email
	var user models.User
	if err := s.db.Where("email = ?", req.EmailToken).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("User not found")
		}
		return err
	}

	// Check if email is verified
	if !user.IsEmailVerified {
		return errors.New("Email not verified, please verify your email first")
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

// GetUserByEmail retrieves a user by email
func (s *AuthService) GetUserByEmail(email string) (*models.User, error) {
	var user models.User
	if err := s.db.Preload("Roles").Where("email = ?", strings.ToLower(email)).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// UpdateProfile updates user profile information
func (s *AuthService) UpdateProfile(userID uuid.UUID, req *models.UpdateProfileRequest) error {
	// Get user first
	var user models.User
	if err := s.db.Preload("Organization").Where("id = ?", userID).First(&user).Error; err != nil {
		return err
	}

	// Update user fields (email cannot be changed via this endpoint)
	user.FirstName = req.FirstName
	user.LastName = req.LastName
	user.Phone = req.Phone
	user.CountryCode = req.CountryCode

	// Save user
	if err := s.db.Save(&user).Error; err != nil {
		return err
	}

	return nil
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
	return s.otpQueueService.QueueRegistrationOTP(email, otp)
}

// RegisterOrganizer creates a new organizer account with temporary storage and OTP sending
func (s *AuthService) RegisterOrganizer(req *models.OrganizerRegistrationRequest) error {
	// Check if user already exists
	var existingUser models.User
	if result := s.db.Where("email = ?", strings.ToLower(req.Email)).First(&existingUser); result.Error == nil {
		return errors.New("User with this email already exists")
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}

	// Use centralized OTP sending logic
	_, err := s.otpService.SendCentralOTP(strings.ToLower(req.Email), "registration", s.emailQueueService)
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	// Store temporary registration data in Redis
	tempData := map[string]interface{}{
		"firstName":   req.FirstName,
		"lastName":    req.LastName,
		"email":       strings.ToLower(req.Email),
		"phone":       req.Phone,
		"countryCode": req.CountryCode,
		"verified":    false,
		"type":        "organizer",
	}
	tempKey := fmt.Sprintf("temp:register:organizer:%s", strings.ToLower(req.Email))

	jsonData, err := json.Marshal(tempData)
	if err != nil {
		return fmt.Errorf("failed to marshal temp data: %w", err)
	}

	// Store in Redis with 10 minute expiry
	err = s.otpService.redisClient.Set(context.Background(), tempKey, jsonData, 10*time.Minute).Err()
	if err != nil {
		return fmt.Errorf("failed to store temp data: %w", err)
	}

	return nil
}

// ApproveOrganizer allows admin/subadmin to approve or reject organizers
func (s *AuthService) ApproveOrganizer(userID, adminID uuid.UUID, req *models.OrganizerApprovalRequest) error {
	// Check if admin has permission
	var admin models.User
	if err := s.db.Preload("Roles").Where("id = ?", adminID).First(&admin).Error; err != nil {
		return fmt.Errorf("admin not found")
	}

	// Check if admin has admin or subadmin role
	hasPermission := false
	for _, role := range admin.Roles {
		if role.Name == "admin" || role.Name == "subadmin" {
			hasPermission = true
			break
		}
	}

	if !hasPermission {
		return fmt.Errorf("insufficient permissions: only admin or subadmin can approve organizers")
	}

	// Get the user to approve
	var user models.User
	if err := s.db.Preload("Roles").Preload("Organization").Where("id = ?", userID).First(&user).Error; err != nil {
		return fmt.Errorf("user not found")
	}

	// Validate current status
	if user.OrganizerStatus != "pending" {
		return fmt.Errorf("organizer cannot be modified, current status: %s", user.OrganizerStatus)
	}

	// Update organizer status and remark
	user.OrganizerStatus = req.Status
	user.AdminRemark = req.AdminRemark

	if err := s.db.Save(&user).Error; err != nil {
		return fmt.Errorf("failed to update organizer status: %w", err)
	}

	return nil
}

// GetPendingOrganizers gets organizers with pending status
func (s *AuthService) GetPendingOrganizers(page, limit int, sortParam string) ([]models.UserResponse, int64, error) {
	var users []models.User
	var total int64
	offset := (page - 1) * limit

	// Get users with organizer role and pending status
	db := s.db.Model(&models.User{}).
		Joins("JOIN user_roles ON users.id = user_roles.user_id").
		Joins("JOIN roles ON user_roles.role_id = roles.id").
		Where("roles.name = ? AND users.organizer_status = ?", "organizer", "pending").
		Preload("Roles").
		Preload("Organization")

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Parse and apply sorting
	validSortFields := map[string]bool{
		"first_name": true, "last_name": true, "email": true, "created_at": true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, validSortFields, "created_at", "desc")
	orderClause := fmt.Sprintf("%s %s", sortBy, sortOrder)

	if err := db.Order(orderClause).Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, err
	}

	// Convert to response format
	responses := make([]models.UserResponse, len(users))
	for i, user := range users {
		responses[i] = user.ToResponse()
	}

	return responses, total, nil
}

// GetAllOrganizers gets all organizers with their approval status
func (s *AuthService) GetAllOrganizers(page, limit int, sortParam string) ([]models.UserResponse, int64, error) {
	var users []models.User
	var total int64
	offset := (page - 1) * limit

	// Get users with organizer role (any status)
	db := s.db.Model(&models.User{}).
		Joins("JOIN user_roles ON users.id = user_roles.user_id").
		Joins("JOIN roles ON user_roles.role_id = roles.id").
		Where("roles.name = ?", "organizer").
		Preload("Roles").
		Preload("Organization")

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Parse and apply sorting
	validSortFields := map[string]bool{
		"first_name": true, "last_name": true, "email": true, "created_at": true, "organizer_status": true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, validSortFields, "created_at", "desc")
	orderClause := fmt.Sprintf("%s %s", sortBy, sortOrder)

	if err := db.Order(orderClause).Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, err
	}

	// Convert to response format
	responses := make([]models.UserResponse, len(users))
	for i, user := range users {
		responses[i] = user.ToResponse()
	}

	return responses, total, nil
}

// GetOTPStatus returns the status of an OTP for debugging purposes
func (s *AuthService) GetOTPStatus(identifier, otpType string) (map[string]interface{}, error) {
	return s.otpService.GetOTPStatus(identifier, otpType, "auth")
}

// CheckUserRole checks if the user with the given email has one of the required roles
func (s *AuthService) CheckUserRole(email string, requiredRoles ...string) error {
	user, err := s.GetUserByEmail(email)
	if err != nil {
		return err
	}

	for _, userRole := range user.Roles {
		for _, req := range requiredRoles {
			if userRole.Name == req {
				return nil
			}
		}
	}

	return errors.New("Access denied: insufficient permissions")
}

// SetUserPassword completes user registration by setting password after OTP verification
func (s *AuthService) SetUserPassword(email, password string) error {
	tempKey := fmt.Sprintf("temp:register:user:%s", strings.ToLower(email))

	// Get temp data
	tempJSON, err := s.otpService.redisClient.Get(context.Background(), tempKey).Result()
	if err != nil {
		return errors.New("Registration session expired or not found")
	}

	var tempData map[string]interface{}
	if err := json.Unmarshal([]byte(tempJSON), &tempData); err != nil {
		return fmt.Errorf("failed to unmarshal temp data: %w", err)
	}

	// Check if verified
	if verified, ok := tempData["verified"].(bool); !ok || !verified {
		return errors.New("Email not verified, please verify OTP first")
	}

	// Create user
	user := models.User{
		Email:           strings.ToLower(email),
		FirstName:       tempData["firstName"].(string),
		LastName:        tempData["lastName"].(string),
		Phone:           tempData["phone"].(string),
		CountryCode:     tempData["countryCode"].(string),
		IsEmailVerified: true, // Already verified
	}

	// Hash password
	if err := user.HashPassword(password); err != nil {
		return err
	}

	// Get user role
	var userRole models.Role
	if err := s.db.Where("name = ?", "user").First(&userRole).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			userRole = models.Role{Name: "user", Description: "Default user role"}
			if err := s.db.Create(&userRole).Error; err != nil {
				return err
			}
		} else {
			return err
		}
	}

	user.Roles = []*models.Role{&userRole}

	// Save user
	if err := s.db.Create(&user).Error; err != nil {
		return err
	}

	// Delete temp data
	s.otpService.redisClient.Del(context.Background(), tempKey)

	return nil
}

// SetOrganizerPassword completes organizer registration by setting password after OTP verification
func (s *AuthService) SetOrganizerPassword(email, password string) error {
	tempKey := fmt.Sprintf("temp:register:organizer:%s", strings.ToLower(email))

	// Get temp data
	tempJSON, err := s.otpService.redisClient.Get(context.Background(), tempKey).Result()
	if err != nil {
		return errors.New("Registration session expired or not found")
	}

	var tempData map[string]interface{}
	if err := json.Unmarshal([]byte(tempJSON), &tempData); err != nil {
		return fmt.Errorf("failed to unmarshal temp data: %w", err)
	}

	// Check if verified
	if verified, ok := tempData["verified"].(bool); !ok || !verified {
		return errors.New("Email not verified, please verify OTP first")
	}

	// Create user
	user := models.User{
		Email:           strings.ToLower(email),
		FirstName:       tempData["firstName"].(string),
		LastName:        tempData["lastName"].(string),
		Phone:           tempData["phone"].(string),
		CountryCode:     tempData["countryCode"].(string),
		IsEmailVerified: true,      // Already verified
		OrganizerStatus: "pending", // Set to pending for approval
	}

	// Hash password
	if err := user.HashPassword(password); err != nil {
		return err
	}

	// Get organizer role
	var organizerRole models.Role
	if err := s.db.Where("name = ?", "organizer").First(&organizerRole).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			organizerRole = models.Role{Name: "organizer", Description: "Organizer role"}
			if err := s.db.Create(&organizerRole).Error; err != nil {
				return err
			}
		} else {
			return err
		}
	}

	user.Roles = []*models.Role{&organizerRole}

	// Save user
	if err := s.db.Create(&user).Error; err != nil {
		return err
	}

	// Delete temp data
	s.otpService.redisClient.Del(context.Background(), tempKey)

	return nil
}
