package services

import (
	"errors"
	"fmt"
	"log"
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
	email := strings.ToLower(req.Email)

	// Check if user already exists
	var existingUser models.User
	if result := s.db.Where("email = ?", email).First(&existingUser); result.Error == nil {
		return utils.NewBusinessLogicError("User with this email already exists.")
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}

	// Check if active registration request already exists
	var existingRequest models.RegistrationRequest
	if result := s.db.Where("email = ?", email).First(&existingRequest); result.Error == nil {
		// If already verified, tell them to set password
		if existingRequest.IsVerified {
			return errors.New("Registration already verified, please set your password")
		}
		// If not verified, resend OTP
		_, err := s.otpService.SendCentralOTP(email, "registration", s.emailQueueService)
		if err != nil {
			return fmt.Errorf("%w", err)
		}
		return nil
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}

	// Use centralized OTP sending logic
	_, err := s.otpService.SendCentralOTP(email, "registration", s.emailQueueService)
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	// Store temporary registration data in database
	registrationRequest := models.RegistrationRequest{
		Email:       email,
		FirstName:   req.FirstName,
		LastName:    req.LastName,
		Phone:       req.Phone,
		CountryCode: req.CountryCode,
		Password:    "", // Will be set later in SetUserPassword
		UserType:    "user",
		IsVerified:  false,
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	if err := s.db.Create(&registrationRequest).Error; err != nil {
		return utils.NewDatabaseError("failed to create registration request", err)
	}

	return nil
}

// Login authenticates a user and returns JWT tokens
func (s *AuthService) Login(req *models.LoginRequest) (*models.TokenResponse, error) {
	// Find user by email
	var user models.User
	if err := s.db.Preload("Roles.Permissions").Where("email = ?", strings.ToLower(req.Email)).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewInvalidCredentialsError()
		}
		return nil, err
	}

	// Check if account is active
	if user.AccountStatus != "active" {
		switch user.AccountStatus {
		case "inactive":
			return nil, utils.NewAccountInactiveError()
		case "suspended":
			return nil, utils.NewAccountSuspendedError()
		default:
			return nil, utils.NewAccountInactiveError()
		}
	}

	// Verify password
	if !user.CheckPassword(req.Password) {
		return nil, utils.NewInvalidCredentialsError()
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
			return nil, utils.NewInvalidCredentialsError()
		}
		return nil, err
	}

	// Check if account is active
	if user.AccountStatus != "active" {
		switch user.AccountStatus {
		case "inactive":
			return nil, utils.NewAccountInactiveError()
		case "suspended":
			return nil, utils.NewAccountSuspendedError()
		default:
			return nil, utils.NewAccountInactiveError()
		}
	}

	// Verify password
	if !user.CheckPassword(req.Password) {
		return nil, utils.NewInvalidCredentialsError()
	}

	// Check if user has one of the required roles
	hasRole := false
	userRoleNames := make([]string, 0, len(user.Roles))
	for _, userRole := range user.Roles {
		userRoleNames = append(userRoleNames, userRole.Name)
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
		// Provide specific error message based on user's actual roles and required roles
		return nil, s.generateCrossLoginErrorMessage(userRoleNames, requiredRoles)
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
			return nil, utils.NewTokenExpiredError()
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

// handleRegistrationOTPVerification marks the user's email as verified in temp data and creates user account
func (s *AuthService) handleRegistrationOTPVerification(email string) error {
	// Find the registration request
	var registrationRequest models.RegistrationRequest
	if err := s.db.Where("email = ?", strings.ToLower(email)).First(&registrationRequest).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("Registration request not found")
		}
		return fmt.Errorf("failed to get registration request: %w", err)
	}

	// Check if user already exists (shouldn't happen, but safety check)
	var existingUser models.User
	if result := s.db.Where("email = ?", strings.ToLower(email)).First(&existingUser); result.Error == nil {
		return errors.New("User already exists")
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}

	// Create user account
	user := models.User{
		Email:           strings.ToLower(email),
		FirstName:       registrationRequest.FirstName,
		LastName:        registrationRequest.LastName,
		Phone:           registrationRequest.Phone,
		CountryCode:     registrationRequest.CountryCode,
		IsEmailVerified: true, // OTP verified
	}

	// Set default password (empty for now, will be set later)
	if err := user.HashPassword(""); err != nil {
		return err
	}

	// Get appropriate role based on user type
	var roleName string
	if registrationRequest.UserType == "organizer" {
		roleName = "organizer"
		user.OrganizerStatus = "pending" // Set to pending for approval
	} else {
		roleName = "user"
	}

	var userRole models.Role
	if err := s.db.Where("name = ?", roleName).First(&userRole).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			userRole = models.Role{Name: roleName, Description: roleName + " role"}
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

	// Mark registration request as verified and completed
	registrationRequest.IsVerified = true
	if err := s.db.Save(&registrationRequest).Error; err != nil {
		return fmt.Errorf("failed to update registration request: %w", err)
	}

	return nil
}

// SendPasswordResetEmail sends a password reset OTP to the user's email
func (s *AuthService) SendPasswordResetEmail(req *models.ResetPasswordRequest) error {
	// Find user by email
	var user models.User
	if err := s.db.Where("email = ?", strings.ToLower(req.Email)).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("Email doesn't exist in the system")
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

// ResendRegistrationOTP resends the existing registration OTP
func (s *AuthService) ResendRegistrationOTP(email string) error {
	// Check if temp registration request exists
	var registrationRequest models.RegistrationRequest
	if err := s.db.Where("email = ?", strings.ToLower(email)).First(&registrationRequest).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("No registration session found")
		}
		return fmt.Errorf("failed to get registration request: %w", err)
	}

	// Use centralized OTP sending logic
	_, err := s.otpService.SendCentralOTP(strings.ToLower(email), "registration", s.emailQueueService)
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}

// HasTempRegistrationData checks if temp registration data exists for the email
func (s *AuthService) HasTempRegistrationData(email string) bool {
	var count int64
	s.db.Model(&models.RegistrationRequest{}).Where("email = ?", strings.ToLower(email)).Count(&count)
	return count > 0
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
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
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
	email := strings.ToLower(req.Email)

	// Check if user already exists
	var existingUser models.User
	if result := s.db.Where("email = ?", email).First(&existingUser); result.Error == nil {
		return utils.NewBusinessLogicError("User with this email already exists.")
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}

	// Check if active registration request already exists
	var existingRequest models.RegistrationRequest
	if result := s.db.Where("email = ?", email).First(&existingRequest); result.Error == nil {
		// If already verified, tell them to set password
		if existingRequest.IsVerified {
			return errors.New("Registration already verified, please set your password")
		}
		// If not verified, resend OTP
		_, err := s.otpService.SendCentralOTP(email, "registration", s.emailQueueService)
		if err != nil {
			return fmt.Errorf("%w", err)
		}
		return nil
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return result.Error
	}

	// Use centralized OTP sending logic
	_, err := s.otpService.SendCentralOTP(email, "registration", s.emailQueueService)
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	// Store temporary registration data in database
	registrationRequest := models.RegistrationRequest{
		Email:       email,
		FirstName:   req.FirstName,
		LastName:    req.LastName,
		Phone:       req.Phone,
		CountryCode: req.CountryCode,
		Password:    "", // Will be set later in SetOrganizerPassword
		UserType:    "organizer",
		IsVerified:  false,
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	if err := s.db.Create(&registrationRequest).Error; err != nil {
		return fmt.Errorf("failed to create registration request: %w", err)
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
	if err := s.db.Preload("Roles").Where("id = ?", userID).First(&user).Error; err != nil {
		return fmt.Errorf("user not found")
	}

	// Validate status transition - once approved, cannot be rejected
	if user.OrganizerStatus == "approved" && req.Status == "rejected" {
		return fmt.Errorf("approved organizers cannot be rejected")
	}

	// Update organizer status and remark
	user.OrganizerStatus = req.Status
	user.AdminRemark = req.AdminRemark
	now := time.Now()
	if req.Status == "approved" {
		user.ApprovedAt = &now
		user.RejectedAt = nil
	} else if req.Status == "rejected" {
		user.RejectedAt = &now
		user.ApprovedAt = nil
	}

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
		Preload("Roles")

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
func (s *AuthService) GetAllOrganizers(page, limit int, sortParam, search, status, accountStatus string) ([]models.OrganizerListItemResponse, int64, error) {
	var users []models.User
	var total int64
	offset := (page - 1) * limit

	// Get users with organizer role (any status)
	db := s.db.Model(&models.User{}).
		Joins("JOIN user_roles ON users.id = user_roles.user_id").
		Joins("JOIN roles ON user_roles.role_id = roles.id").
		Where("roles.name = ?", "organizer").
		Preload("OrganizerOnboarding")

	// Apply search filter
	if search != "" {
		searchTerm := "%" + search + "%"
		db = db.Where("users.first_name ILIKE ? OR users.last_name ILIKE ? OR users.email ILIKE ?", searchTerm, searchTerm, searchTerm)
	}

	// Apply status filters
	if status != "" {
		db = db.Where("users.organizer_status = ?", status)
	}
	if accountStatus != "" {
		db = db.Where("users.account_status = ?", accountStatus)
	}

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Parse and apply sorting
	validSortFields := map[string]bool{
		"first_name": true, "last_name": true, "email": true, "created_at": true, "organizer_status": true, "account_status": true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, validSortFields, "created_at", "desc")
	orderClause := fmt.Sprintf("%s %s", sortBy, sortOrder)

	if err := db.Order(orderClause).Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, err
	}

	// Convert to simplified response format
	responses := make([]models.OrganizerListItemResponse, len(users))
	for i, user := range users {
		responses[i] = user.ToOrganizerListItemResponse()
	}

	return responses, total, nil
}

// GetAllApprovedOrganizers gets all approved organizers without pagination
func (s *AuthService) GetAllApprovedOrganizers() ([]models.OrganizerListItemResponse, error) {
	var users []models.User

	// Get all approved organizers with their onboarding data
	err := s.db.Model(&models.User{}).
		Joins("JOIN user_roles ON users.id = user_roles.user_id").
		Joins("JOIN roles ON user_roles.role_id = roles.id").
		Where("roles.name = ? AND users.organizer_status = ?", "organizer", "approved").
		Preload("OrganizerOnboarding").
		Order("users.created_at DESC").
		Find(&users).Error

	if err != nil {
		return nil, err
	}

	// Convert to simplified response format
	responses := make([]models.OrganizerListItemResponse, len(users))
	for i, user := range users {
		responses[i] = user.ToOrganizerListItemResponse()
	}

	return responses, nil
}

// GetOrganizerByID gets a specific organizer by ID with simplified details
func (s *AuthService) GetOrganizerByID(organizerID uuid.UUID) (*models.OrganizerSimpleResponse, error) {
	var user models.User

	// Get user with organizer role and preload onboarding data
	err := s.db.Model(&models.User{}).
		Joins("JOIN user_roles ON users.id = user_roles.user_id").
		Joins("JOIN roles ON user_roles.role_id = roles.id").
		Where("roles.name = ? AND users.id = ?", "organizer", organizerID).
		Preload("OrganizerOnboarding").
		First(&user).Error

	if err != nil {
		return nil, err
	}

	// Convert to simplified response format
	response := user.ToOrganizerSimpleResponse()
	return &response, nil
}

// GetOTPStatus returns the status of an OTP for debugging purposes
func (s *AuthService) GetOTPStatus(identifier, otpType string) (map[string]interface{}, error) {
	return s.otpService.GetOTPStatus(identifier, otpType, "auth")
}

// generateCrossLoginErrorMessage creates user-friendly error messages for cross-login attempts
func (s *AuthService) generateCrossLoginErrorMessage(userRoles, requiredRoles []string) error {
	// Use a common message for all cross-login attempts
	return errors.New("You cannot login with these credentials in this panel.")
}

// containsRole checks if a slice contains a specific role
func (s *AuthService) containsRole(roles []string, targetRole string) bool {
	for _, role := range roles {
		if role == targetRole {
			return true
		}
	}
	return false
}

// SetUserPassword completes user registration by setting password after OTP verification
func (s *AuthService) SetUserPassword(email, password string) error {
	// Find the user (should exist after OTP verification)
	var user models.User
	if err := s.db.Where("email = ?", strings.ToLower(email)).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("User not found. Please complete registration first.")
		}
		return fmt.Errorf("failed to find user: %w", err)
	}

	// Hash and set new password
	if err := user.HashPassword(password); err != nil {
		return err
	}

	// Save user
	if err := s.db.Save(&user).Error; err != nil {
		return err
	}

	// Clean up temp registration request if it exists
	if err := s.db.Where("email = ? AND user_type = ?", strings.ToLower(email), "user").Delete(&models.RegistrationRequest{}).Error; err != nil {
		// Log error but don't fail
		log.Printf("Failed to cleanup registration request: %v", err)
	}

	return nil
}

// SetOrganizerPassword completes organizer registration by setting password after OTP verification
func (s *AuthService) SetOrganizerPassword(email, password string) error {
	// Find the user (should exist after OTP verification)
	var user models.User
	if err := s.db.Where("email = ?", strings.ToLower(email)).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("User not found. Please complete registration first.")
		}
		return fmt.Errorf("failed to find user: %w", err)
	}

	// Hash and set new password
	if err := user.HashPassword(password); err != nil {
		return err
	}

	// Save user
	if err := s.db.Save(&user).Error; err != nil {
		return err
	}

	// Clean up temp registration request if it exists
	if err := s.db.Where("email = ? AND user_type = ?", strings.ToLower(email), "organizer").Delete(&models.RegistrationRequest{}).Error; err != nil {
		// Log error but don't fail
		log.Printf("Failed to cleanup registration request: %v", err)
	}

	return nil
}

// CheckUserRole checks if the user with the given email has one of the required roles
func (s *AuthService) CheckUserRole(email string, requiredRoles ...string) error {
	user, err := s.GetUserByEmail(email)
	if err != nil {
		return err
	}

	userRoleNames := make([]string, 0, len(user.Roles))
	for _, userRole := range user.Roles {
		userRoleNames = append(userRoleNames, userRole.Name)
		for _, req := range requiredRoles {
			if userRole.Name == req {
				return nil
			}
		}
	}

	// Provide specific error message for password reset scenarios
	return s.generatePasswordResetErrorMessage(userRoleNames, requiredRoles)
}

// CheckUserRoleForPasswordReset checks if the user with the given email has one of the required roles for password reset
// Returns nil if user exists and has required role, otherwise returns an error indicating email doesn't exist
func (s *AuthService) CheckUserRoleForPasswordReset(email string, requiredRoles ...string) error {
	user, err := s.GetUserByEmail(email)
	if err != nil {
		// If user doesn't exist, return "email doesn't exist" error
		return errors.New("Email doesn't exist")
	}

	userRoleNames := make([]string, 0, len(user.Roles))
	for _, userRole := range user.Roles {
		userRoleNames = append(userRoleNames, userRole.Name)
		for _, req := range requiredRoles {
			if userRole.Name == req {
				return nil
			}
		}
	}

	// If user exists but doesn't have required role, return "Invalid Email" error
	return errors.New("Invalid Email.")
}

// generatePasswordResetErrorMessage creates user-friendly error messages for password reset attempts with wrong user type
func (s *AuthService) generatePasswordResetErrorMessage(userRoles, requiredRoles []string) error {
	// Use a common message for all password reset attempts with wrong user type
	return errors.New("You cannot reset password with these credentials in this panel")
}

// AdminCreateOrganizer allows admin to directly create and approve an organizer account
func (s *AuthService) AdminCreateOrganizer(adminID uuid.UUID, req *models.AdminCreateOrganizerRequest) (*models.User, error) {
	// Check if admin has permission
	var admin models.User
	if err := s.db.Preload("Roles").Where("id = ?", adminID).First(&admin).Error; err != nil {
		return nil, fmt.Errorf("admin not found")
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
		return nil, fmt.Errorf("insufficient permissions: only admin or subadmin can create organizers")
	}

	email := strings.ToLower(req.Email)

	// Check if user already exists
	var existingUser models.User
	if result := s.db.Where("email = ?", email).First(&existingUser); result.Error == nil {
		return nil, errors.New("user with this email already exists")
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, result.Error
	}

	// Get organizer role
	var organizerRole models.Role
	if err := s.db.Where("name = ?", "organizer").First(&organizerRole).Error; err != nil {
		return nil, fmt.Errorf("organizer role not found")
	}

	// Create the user with approved status
	user := models.User{
		Email:           email,
		FirstName:       req.FirstName,
		LastName:        req.LastName,
		Phone:           req.Phone,
		CountryCode:     req.CountryCode,
		OrganizerStatus: "approved", // Pre-approved by admin
		AccountStatus:   "active",
		IsEmailVerified: true, // Admin-created accounts are pre-verified
		Roles:           []*models.Role{&organizerRole},
	}

	// Hash the password
	if err := user.HashPassword(req.Password); err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Start transaction
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()

	// Create user
	if err := tx.Create(&user).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Create organizer onboarding record
	onboarding := models.OrganizerOnboarding{
		OrganizerID: user.ID,
		IsComplete:  false,
	}
	if err := tx.Create(&onboarding).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create onboarding record: %w", err)
	}

	// Send welcome email (optional - can be enhanced later)
	// For now, we'll skip the email to avoid complexity
	// TODO: Add welcome email functionality

	// Send organizer credentials email
	if err := s.emailQueueService.QueueOrganizerCredentialsEmail(&user, req.Password); err != nil {
		// Log the error but don't fail the creation
		log.Printf("Failed to queue organizer credentials email: %v", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &user, nil
}

// CleanupExpiredRegistrationRequests is no longer needed since we don't use expiration
func (s *AuthService) CleanupExpiredRegistrationRequests() error {
	// No longer needed - registration requests don't expire
	return nil
}

// GetOrganizerUsers retrieves all users belonging to an organizer's organization
func (s *AuthService) GetOrganizerUsers(organizerID uuid.UUID, page, limit int, search, role, sortParam string) ([]models.UserResponse, int64, error) {
	var users []models.User
	var total int64

	offset := (page - 1) * limit
	query := s.db.Model(&models.User{}).Where("organizer_id = ? AND deleted_at IS NULL", organizerID)

	// Add search functionality
	if search != "" {
		searchTerm := "%" + search + "%"
		query = query.Where("email ILIKE ? OR first_name ILIKE ? OR last_name ILIKE ? OR CONCAT(first_name, ' ', last_name) ILIKE ?",
			searchTerm, searchTerm, searchTerm, searchTerm)
	}

	// Add role filter
	if role != "" {
		query = query.Joins("JOIN user_roles ON user_roles.user_id = users.id").
			Joins("JOIN roles ON roles.id = user_roles.role_id").
			Where("roles.name = ?", role)
	}

	// Count total records
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Apply sorting
	validSortFields := map[string]bool{
		"first_name": true, "last_name": true, "email": true, "created_at": true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, validSortFields, "created_at", "desc")
	orderClause := sortBy + " " + sortOrder

	// Get paginated results with roles
	if err := query.Preload("Roles").Order(orderClause).Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, err
	}

	// Convert to response format
	userResponses := make([]models.UserResponse, len(users))
	for i, user := range users {
		userResponses[i] = user.ToResponse()
	}

	return userResponses, total, nil
}

// CreateOrganizerUser creates a new user within an organizer's organization
func (s *AuthService) CreateOrganizerUser(organizerID uuid.UUID, req *models.CreateOrgUserRequest) (*models.User, bool, error) {
	// Check if user already exists
	var existingUser models.User
	if err := s.db.Where("email = ?", strings.ToLower(req.Email)).First(&existingUser).Error; err == nil {
		// User exists - check if they're already attached to ANY organizer
		if existingUser.OrganizerID != nil {
			return nil, false, fmt.Errorf("user_already_belongs_to_organizer")
		}

		// User exists but not attached to any organizer - attach them to this one
		existingUser.OrganizerID = &organizerID
		existingUser.UpdatedAt = time.Now()

		if err := s.db.Save(&existingUser).Error; err != nil {
			return nil, false, fmt.Errorf("failed to attach existing user to organizer: %w", err)
		}

		// Load roles for response
		s.db.Preload("Roles").First(&existingUser, existingUser.ID)

		return &existingUser, false, nil // false = existing user attached
	}

	// Create user
	user := models.User{
		Email:           strings.ToLower(req.Email),
		PasswordHash:    "", // Will be set by HashPassword
		FirstName:       req.FirstName,
		LastName:        req.LastName,
		Phone:           req.Phone,
		OrganizerID:     &organizerID,
		CreatedBy:       &organizerID,
		IsEmailVerified: true, // Organization users are pre-verified
		AccountStatus:   "active",
	}

	// Hash password
	if err := user.HashPassword(req.Password); err != nil {
		return nil, false, fmt.Errorf("failed to hash password: %w", err)
	}

	// Get the role
	var role models.Role
	if err := s.db.Where("name = ?", req.RoleName).First(&role).Error; err != nil {
		return nil, false, fmt.Errorf("invalid role: %s", req.RoleName)
	}

	// Ensure the role has permissions assigned (in case initialization missed it)
	permissionService := NewPermissionService()
	if err := permissionService.EnsureRoleHasPermissions(role.Name); err != nil {
		// Log error but don't fail - permissions can be assigned later
		log.Printf("Warning: Failed to ensure permissions for role %s: %v", role.Name, err)
	}

	// Start transaction
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			panic(r)
		}
	}()

	if err := tx.Create(&user).Error; err != nil {
		tx.Rollback()
		return nil, false, fmt.Errorf("failed to create user: %w", err)
	}

	// Assign role
	if err := tx.Model(&user).Association("Roles").Append(&role); err != nil {
		tx.Rollback()
		return nil, false, fmt.Errorf("failed to assign role: %w", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		return nil, false, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Load roles for response
	s.db.Preload("Roles").First(&user, user.ID)

	// Send credentials email (optional - can be enhanced later)
	// For now, we'll skip the email to avoid complexity
	// TODO: Add organization user credentials email functionality
	if err := s.emailQueueService.QueueOrganizationUserCredentialsEmail(&user, req.Password, req.RoleName); err != nil {
		// Log the error but don't fail the creation
		log.Printf("Failed to queue organization user credentials email: %v", err)
	}

	return &user, true, nil // true = newly created user
}

// UpdateOrganizerUser updates a user within an organizer's organization
func (s *AuthService) UpdateOrganizerUser(organizerID, userID uuid.UUID, req *models.UpdateOrgUserRequest) (*models.User, error) {
	// Find the user in the same organization
	var user models.User
	if err := s.db.Where("id = ? AND organizer_id = ? AND deleted_at IS NULL", userID, organizerID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("user not found in organization")
		}
		return nil, err
	}

	// Update role if provided
	if req.RoleType != "" {
		var role models.Role
		if err := s.db.Where("name = ?", req.RoleType).First(&role).Error; err != nil {
			return nil, fmt.Errorf("invalid role: %s", req.RoleType)
		}

		// Clear existing roles and assign new one
		if err := s.db.Model(&user).Association("Roles").Replace(&role); err != nil {
			return nil, fmt.Errorf("failed to update role: %w", err)
		}
	}

	// Update active status if provided
	if req.Active != nil {
		if *req.Active {
			user.AccountStatus = "active"
		} else {
			user.AccountStatus = "inactive"
		}
	}

	if err := s.db.Save(&user).Error; err != nil {
		return nil, err
	}

	// Load roles for response
	s.db.Preload("Roles").First(&user, user.ID)

	return &user, nil
}

// DeleteOrganizerUser soft deletes a user from an organizer's organization
func (s *AuthService) DeleteOrganizerUser(organizerID, userID uuid.UUID) error {
	// Find and soft delete the user in the same organization
	result := s.db.Where("id = ? AND organizer_id = ? AND deleted_at IS NULL", userID, organizerID).Delete(&models.User{})
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("user not found in organization")
	}

	return nil
}
