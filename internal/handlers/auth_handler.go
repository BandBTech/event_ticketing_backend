package handlers

import (
	"net/http"
	"strconv"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type AuthHandler struct {
	authService *services.AuthService
}

func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		authService: services.NewAuthService(cfg),
	}
}

// Register godoc
// @Summary Register a new user
// @Description Create a new user account
// @Tags User Auth
// @Accept json
// @Produce json
// @Param request body models.CreateUserRequest true "User registration data"
// @Success 201 {object} utils.Response{data=models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/user/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req models.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	user, err := h.authService.Register(&req)
	if err != nil {
		// You can now use specific error types
		utils.BadRequestErrorResponse(c, "Registration failed", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "User registered successfully", user)
}

// RefreshToken godoc
// @Summary Refresh access token
// @Description Get new access and refresh tokens using a valid refresh token
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body models.RefreshTokenRequest true "Refresh token"
// @Success 200 {object} utils.Response{data=models.TokenResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/refresh [post]
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	var req models.RefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	tokens, err := h.authService.RefreshToken(&req)
	if err != nil {
		utils.UnauthorizedErrorResponse(c, "Token refresh failed", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Token refreshed successfully", tokens)
}

// Logout godoc
// @Summary Logout user
// @Description Logout authenticated user by invalidating tokens
// @Tags Auth
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "Unauthorized", nil)
		return
	}

	// Parse the "all" query parameter
	all := c.DefaultQuery("all", "false") == "true"

	// Logout
	err := h.authService.Logout(userID.(uuid.UUID), all)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Logout failed", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Logout successful", nil)
}

// GetProfile godoc
// @Summary Get user profile
// @Description Get authenticated user profile
// @Tags Auth
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.UserProfileResponse}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/profile [get]
func (h *AuthHandler) GetProfile(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "Unauthorized", nil)
		return
	}

	user, err := h.authService.GetUserByID(userID.(uuid.UUID))
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to get user profile", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "User profile retrieved successfully", user.ToProfileResponse())
}

// UpdateProfile godoc
// @Summary Update user profile
// @Description Update authenticated user's profile information (first name, last name, and phone number only)
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body models.UpdateProfileRequest true "Profile update data"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.UserProfileResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/profile [put]
func (h *AuthHandler) UpdateProfile(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "Unauthorized", nil)
		return
	}

	var req models.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	updatedProfile, err := h.authService.UpdateProfile(userID.(uuid.UUID), &req)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Failed to update profile", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Profile updated successfully", updatedProfile)
}

// ChangePassword godoc
// @Summary Change user password
// @Description Change authenticated user's password
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body models.ChangePasswordRequest true "Password change data"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/change-password [post]
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "Unauthorized", nil)
		return
	}

	var req models.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	err := h.authService.ChangePassword(userID.(uuid.UUID), &req)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Failed to change password", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Password changed successfully", nil)
}

// RegisterOrganizer godoc
// @Summary Register a new organizer
// @Description Register as an organizer (requires approval)
// @Tags Organizer Auth
// @Accept json
// @Produce json
// @Param request body models.OrganizerRegistrationRequest true "Organizer registration data"
// @Success 201 {object} utils.Response{data=models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/organizer/register [post]
func (h *AuthHandler) RegisterOrganizer(c *gin.Context) {
	var req models.OrganizerRegistrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	user, err := h.authService.RegisterOrganizer(&req)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Organizer registration failed", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Organizer registration submitted for approval", user)
}

// ApproveOrganizer godoc
// @Summary Approve or reject organizer
// @Description Admin/subadmin can approve or reject pending organizers
// @Tags Admin
// @Accept json
// @Produce json
// @Param id path string true "User ID"
// @Param request body models.OrganizerApprovalRequest true "Approval data"
// @Success 200 {object} utils.Response{data=models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/organizers/{id}/approval [put]
func (h *AuthHandler) ApproveOrganizer(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid user ID", err)
		return
	}

	var req models.OrganizerApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Get admin user from context
	adminID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	user, err := h.authService.ApproveOrganizer(userID, adminID.(uuid.UUID), &req)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to process organizer approval", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organizer approval processed successfully", user)
}

// GetPendingOrganizers godoc
// @Summary Get pending organizers
// @Description Get list of organizers pending approval
// @Tags Admin
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param sort query string false "Sort by field with optional '-' prefix for desc (e.g., '-created_at', 'first_name')" default("-created_at")
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/organizers/pending [get]
func (h *AuthHandler) GetPendingOrganizers(c *gin.Context) {
	page := 1
	if pageParam := c.Query("page"); pageParam != "" {
		if p, err := strconv.Atoi(pageParam); err == nil {
			page = p
		}
	}

	limit := 10
	if limitParam := c.Query("limit"); limitParam != "" {
		if l, err := strconv.Atoi(limitParam); err == nil {
			limit = l
		}
	}

	sortParam := c.DefaultQuery("sort", "-created_at")

	organizers, total, err := h.authService.GetPendingOrganizers(page, limit, sortParam)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to fetch pending organizers", err)
		return
	}

	response := map[string]interface{}{
		"organizers": organizers,
		"total":      total,
		"page":       page,
		"limit":      limit,
	}
	utils.SuccessResponse(c, http.StatusOK, "Pending organizers fetched successfully", response)
}

// GetOTPStatus godoc
// @Summary Get OTP status for debugging
// @Description Get the status of OTP for a specific identifier (admin only)
// @Tags Admin
// @Produce json
// @Param identifier query string true "Email or identifier"
// @Param otp_type query string true "OTP type (registration, password_reset, etc.)"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/otp/status [get]
func (h *AuthHandler) GetOTPStatus(c *gin.Context) {
	identifier := c.Query("identifier")
	otpType := c.Query("otp_type")

	if identifier == "" || otpType == "" {
		utils.BadRequestErrorResponse(c, "Both identifier and otp_type are required", nil)
		return
	}

	status, err := h.authService.GetOTPStatus(identifier, otpType)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to get OTP status", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "OTP status retrieved successfully", status)
}

// === SEPARATE AUTH ENDPOINTS FOR DIFFERENT USER TYPES ===

// UserLogin godoc
// @Summary User login
// @Description Login for regular users only
// @Tags User Auth
// @Accept json
// @Produce json
// @Param request body models.LoginRequest true "Login credentials"
// @Success 200 {object} utils.Response{data=models.TokenResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/user/login [post]
func (h *AuthHandler) UserLogin(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Login and validate user has "user" role
	tokens, err := h.authService.LoginWithRoleCheck(&req, "user")
	if err != nil {
		utils.UnauthorizedErrorResponse(c, err.Error(), nil)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "User login successful", tokens)
}

// AdminLogin godoc
// @Summary Admin login
// @Description Login for admin and subadmin users only
// @Tags Admin Auth
// @Accept json
// @Produce json
// @Param request body models.LoginRequest true "Login credentials"
// @Success 200 {object} utils.Response{data=models.TokenResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/admin/login [post]
func (h *AuthHandler) AdminLogin(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Login and validate user has admin or subadmin role
	tokens, err := h.authService.LoginWithRoleCheck(&req, "admin", "subadmin")
	if err != nil {
		utils.UnauthorizedErrorResponse(c, err.Error(), nil)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Admin login successful", tokens)
}

// OrganizerLogin godoc
// @Summary Organizer login
// @Description Login for organizer, staff, and manager users only
// @Tags Organizer Auth
// @Accept json
// @Produce json
// @Param request body models.LoginRequest true "Login credentials"
// @Success 200 {object} utils.Response{data=models.TokenResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/organizer/login [post]
func (h *AuthHandler) OrganizerLogin(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Login and validate user has organizer, staff, or manager role
	tokens, err := h.authService.LoginWithRoleCheck(&req, "organizer", "staff", "manager")
	if err != nil {
		utils.UnauthorizedErrorResponse(c, err.Error(), nil)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organizer login successful", tokens)
}

// === PASSWORD RESET ENDPOINTS FOR DIFFERENT USER TYPES ===

// UserResetPasswordRequest godoc
// @Summary Request password reset for user
// @Description Send password reset OTP to user email
// @Tags User Auth
// @Accept json
// @Produce json
// @Param request body models.ResetPasswordRequest true "Password reset request data"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/user/reset-password-request [post]
func (h *AuthHandler) UserResetPasswordRequest(c *gin.Context) {
	var req models.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Check if user has "user" role
	if err := h.authService.CheckUserRole(req.Email, "user"); err != nil {
		utils.SuccessResponse(c, http.StatusOK, "If your email is registered as a user, you will receive a password reset OTP", nil)
		return
	}

	// Send password reset OTP
	if err := h.authService.SendPasswordResetEmail(&req); err != nil {
		utils.BadRequestErrorResponse(c, "Failed to send password reset OTP", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Password reset OTP sent successfully", nil)
}

// AdminResetPasswordRequest godoc
// @Summary Request password reset for admin
// @Description Send password reset OTP to admin email
// @Tags Admin Auth
// @Accept json
// @Produce json
// @Param request body models.ResetPasswordRequest true "Password reset request data"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/admin/reset-password-request [post]
func (h *AuthHandler) AdminResetPasswordRequest(c *gin.Context) {
	var req models.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Check if user has admin or subadmin role
	if err := h.authService.CheckUserRole(req.Email, "admin", "subadmin"); err != nil {
		utils.SuccessResponse(c, http.StatusOK, "If your email is registered as an admin, you will receive a password reset OTP", nil)
		return
	}

	// Send password reset OTP
	if err := h.authService.SendPasswordResetEmail(&req); err != nil {
		utils.BadRequestErrorResponse(c, "Failed to send password reset OTP", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Password reset OTP sent successfully", nil)
}

// OrganizerResetPasswordRequest godoc
// @Summary Request password reset for organizer
// @Description Send password reset OTP to organizer email
// @Tags Organizer Auth
// @Accept json
// @Produce json
// @Param request body models.ResetPasswordRequest true "Password reset request data"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/organizer/reset-password-request [post]
func (h *AuthHandler) OrganizerResetPasswordRequest(c *gin.Context) {
	var req models.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Check if user has organizer, staff, or manager role
	if err := h.authService.CheckUserRole(req.Email, "organizer", "staff", "manager"); err != nil {
		utils.SuccessResponse(c, http.StatusOK, "If your email is registered as an organizer, you will receive a password reset OTP", nil)
		return
	}

	// Send password reset OTP
	if err := h.authService.SendPasswordResetEmail(&req); err != nil {
		utils.BadRequestErrorResponse(c, "Failed to send password reset OTP", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Password reset OTP sent successfully", nil)
}

// UserResetPassword godoc
// @Summary Reset user password with OTP verification
// @Description Reset user password using OTP verification
// @Tags User Auth
// @Accept json
// @Produce json
// @Param request body models.UpdatePasswordRequest true "Password reset request with OTP verification"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/user/reset-password [post]
func (h *AuthHandler) UserResetPassword(c *gin.Context) {
	var req models.UpdatePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Check if user has "user" role
	if err := h.authService.CheckUserRole(req.EmailToken, "user"); err != nil {
		utils.BadRequestErrorResponse(c, "Invalid email or insufficient permissions", err)
		return
	}

	if err := h.authService.ResetPassword(&req); err != nil {
		utils.BadRequestErrorResponse(c, "Password reset failed", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Password reset successful", nil)
}

// SetUserPassword godoc
// @Summary Set password for user registration
// @Description Complete user registration by setting password after OTP verification
// @Tags User Auth
// @Accept json
// @Produce json
// @Param request body models.SetPasswordRequest true "Set password request"
// @Success 201 {object} utils.Response{data=models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/user/set-password [post]
func (h *AuthHandler) SetUserPassword(c *gin.Context) {
	var req models.SetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	user, err := h.authService.SetUserPassword(req.Email, req.Password)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Failed to set password", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "User registration completed successfully", user)
}

// AdminResetPassword godoc
// @Summary Reset admin password with OTP verification
// @Description Reset admin password using OTP verification
// @Tags Admin Auth
// @Accept json
// @Produce json
// @Param request body models.UpdatePasswordRequest true "Password reset request with OTP verification"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/admin/reset-password [post]
func (h *AuthHandler) AdminResetPassword(c *gin.Context) {
	var req models.UpdatePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Check if user has admin or subadmin role
	if err := h.authService.CheckUserRole(req.EmailToken, "admin", "subadmin"); err != nil {
		utils.BadRequestErrorResponse(c, "Invalid email or insufficient permissions", err)
		return
	}

	if err := h.authService.ResetPassword(&req); err != nil {
		utils.BadRequestErrorResponse(c, "Password reset failed", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Password reset successful", nil)
}

// OrganizerResetPassword godoc
// @Summary Reset organizer password with OTP verification
// @Description Reset organizer password using OTP verification
// @Tags Organizer Auth
// @Accept json
// @Produce json
// @Param request body models.UpdatePasswordRequest true "Password reset request with OTP verification"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/organizer/reset-password [post]
func (h *AuthHandler) OrganizerResetPassword(c *gin.Context) {
	var req models.UpdatePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Check if user has organizer, staff, or manager role
	if err := h.authService.CheckUserRole(req.EmailToken, "organizer", "staff", "manager"); err != nil {
		utils.BadRequestErrorResponse(c, "Invalid email or insufficient permissions", err)
		return
	}

	if err := h.authService.ResetPassword(&req); err != nil {
		utils.BadRequestErrorResponse(c, "Password reset failed", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Password reset successful", nil)
}

// SetOrganizerPassword godoc
// @Summary Set password for organizer registration
// @Description Complete organizer registration by setting password after OTP verification
// @Tags Organizer Auth
// @Accept json
// @Produce json
// @Param request body models.SetPasswordRequest true "Set password request"
// @Success 201 {object} utils.Response{data=models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/organizer/set-password [post]
func (h *AuthHandler) SetOrganizerPassword(c *gin.Context) {
	var req models.SetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	user, err := h.authService.SetOrganizerPassword(req.Email, req.Password)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Failed to set password", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Organizer registration completed successfully", user)
}

// UserVerifyOTP godoc
// @Summary Verify OTP for user password reset
// @Description Verify OTP code for user password reset
// @Tags User Auth
// @Accept json
// @Produce json
// @Param request body models.OTPVerifyRequest true "OTP verification data"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/user/verify-otp [post]
func (h *AuthHandler) UserVerifyOTP(c *gin.Context) {
	var req models.OTPVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Ensure role is set to "user"
	req.Role = "user"

	// Check if user has "user" role
	if err := h.authService.CheckUserRole(req.Identifier, "user"); err != nil {
		utils.BadRequestErrorResponse(c, "Invalid email or insufficient permissions", err)
		return
	}

	if err := h.authService.VerifyOTP(&req); err != nil {
		utils.BadRequestErrorResponse(c, "OTP verification failed", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "OTP verified successfully", nil)
}

// UserSendOTP godoc
// @Summary Send OTP for user password reset
// @Description Send a new OTP for user password reset
// @Tags User Auth
// @Accept json
// @Produce json
// @Param request body models.OTPSendRequest true "OTP send request data"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/user/send-otp [post]
func (h *AuthHandler) UserSendOTP(c *gin.Context) {
	var req models.OTPSendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Ensure role is set to "user"
	req.Role = "user"

	// Check if user has "user" role
	if err := h.authService.CheckUserRole(req.Identifier, "user"); err != nil {
		utils.SuccessResponse(c, http.StatusOK, "If your email is registered as a user, you will receive an OTP", nil)
		return
	}

	// Send OTP
	resetReq := models.ResetPasswordRequest{Email: req.Identifier}
	if err := h.authService.SendPasswordResetEmail(&resetReq); err != nil {
		utils.BadRequestErrorResponse(c, "Failed to send OTP", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "OTP sent successfully", nil)
}

// AdminVerifyOTP godoc
// @Summary Verify OTP for admin password reset
// @Description Verify OTP code for admin password reset
// @Tags Admin Auth
// @Accept json
// @Produce json
// @Param request body models.OTPVerifyRequest true "OTP verification data"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/admin/verify-otp [post]
func (h *AuthHandler) AdminVerifyOTP(c *gin.Context) {
	var req models.OTPVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Ensure role is set to "admin"
	req.Role = "admin"

	// Check if user has admin or subadmin role
	if err := h.authService.CheckUserRole(req.Identifier, "admin", "subadmin"); err != nil {
		utils.BadRequestErrorResponse(c, "Invalid email or insufficient permissions", err)
		return
	}

	if err := h.authService.VerifyOTP(&req); err != nil {
		utils.BadRequestErrorResponse(c, "OTP verification failed", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "OTP verified successfully", nil)
}

// AdminSendOTP godoc
// @Summary Send OTP for admin password reset
// @Description Send a new OTP for admin password reset
// @Tags Admin Auth
// @Accept json
// @Produce json
// @Param request body models.OTPSendRequest true "OTP send request data"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/admin/send-otp [post]
func (h *AuthHandler) AdminSendOTP(c *gin.Context) {
	var req models.OTPSendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Ensure role is set to "admin"
	req.Role = "admin"

	// Check if user has admin or subadmin role
	if err := h.authService.CheckUserRole(req.Identifier, "admin", "subadmin"); err != nil {
		utils.SuccessResponse(c, http.StatusOK, "If your email is registered as an admin, you will receive an OTP", nil)
		return
	}

	// Send OTP
	resetReq := models.ResetPasswordRequest{Email: req.Identifier}
	if err := h.authService.SendPasswordResetEmail(&resetReq); err != nil {
		utils.BadRequestErrorResponse(c, "Failed to send OTP", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "OTP sent successfully", nil)
}

// OrganizerVerifyOTP godoc
// @Summary Verify OTP for organizer password reset
// @Description Verify OTP code for organizer password reset
// @Tags Organizer Auth
// @Accept json
// @Produce json
// @Param request body models.OTPVerifyRequest true "OTP verification data"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/organizer/verify-otp [post]
func (h *AuthHandler) OrganizerVerifyOTP(c *gin.Context) {
	var req models.OTPVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Ensure role is set to "organizer"
	req.Role = "organizer"

	// Check if user has organizer, staff, or manager role
	if err := h.authService.CheckUserRole(req.Identifier, "organizer", "staff", "manager"); err != nil {
		utils.BadRequestErrorResponse(c, "Invalid email or insufficient permissions", err)
		return
	}

	if err := h.authService.VerifyOTP(&req); err != nil {
		utils.BadRequestErrorResponse(c, "OTP verification failed", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "OTP verified successfully", nil)
}

// OrganizerSendOTP godoc
// @Summary Send OTP for organizer password reset
// @Description Send a new OTP for organizer password reset
// @Tags Organizer Auth
// @Accept json
// @Produce json
// @Param request body models.OTPSendRequest true "OTP send request data"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/auth/organizer/send-otp [post]
func (h *AuthHandler) OrganizerSendOTP(c *gin.Context) {
	var req models.OTPSendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Ensure role is set to "organizer"
	req.Role = "organizer"

	// Check if user has organizer, staff, or manager role
	if err := h.authService.CheckUserRole(req.Identifier, "organizer", "staff", "manager"); err != nil {
		utils.SuccessResponse(c, http.StatusOK, "If your email is registered as an organizer, you will receive an OTP", nil)
		return
	}

	// Send OTP
	resetReq := models.ResetPasswordRequest{Email: req.Identifier}
	if err := h.authService.SendPasswordResetEmail(&resetReq); err != nil {
		utils.BadRequestErrorResponse(c, "Failed to send OTP", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "OTP sent successfully", nil)
}
