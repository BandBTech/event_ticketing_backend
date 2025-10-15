package handlers

import (
	"net/http"

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
// @Tags auth
// @Accept json
// @Produce json
// @Param request body models.CreateUserRequest true "User registration data"
// @Success 201 {object} utils.Response{data=models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /auth/register [post]
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

// Login godoc
// @Summary Authenticate user
// @Description Login with email and password to get JWT tokens
// @Tags auth
// @Accept json
// @Produce json
// @Param request body models.LoginRequest true "Login credentials"
// @Success 200 {object} utils.Response{data=models.TokenResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	tokens, err := h.authService.Login(&req)
	if err != nil {
		utils.UnauthorizedErrorResponse(c, err.Error(), nil)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Login successful", tokens)
}

// RefreshToken godoc
// @Summary Refresh access token
// @Description Get new access and refresh tokens using a valid refresh token
// @Tags auth
// @Accept json
// @Produce json
// @Param request body models.RefreshTokenRequest true "Refresh token"
// @Success 200 {object} utils.Response{data=models.TokenResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /auth/refresh [post]
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
// @Description Revoke user's refresh tokens
// @Tags auth
// @Accept json
// @Produce json
// @Param all query boolean false "Revoke all refresh tokens for the user"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /auth/logout [post]
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

// ResetPasswordRequest godoc
// @Summary Request password reset OTP
// @Description Send a password reset OTP to the user's email
// @Tags auth
// @Accept json
// @Produce json
// @Param request body models.ResetPasswordRequest true "Email address"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /auth/reset-password-request [post]
func (h *AuthHandler) ResetPasswordRequest(c *gin.Context) {
	var req models.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	// Always return success for security reasons, even if email doesn't exist
	if err := h.authService.SendPasswordResetEmail(&req); err != nil {
		// Log the error but don't expose it to the client
		c.Error(err)
	}

	utils.SuccessResponse(c, http.StatusOK, "If your email is registered, you will receive a password reset OTP", nil)
}

// ResetPassword godoc
// @Summary Reset password with OTP verification
// @Description Reset user password using OTP verification. The OTP must be valid and not expired. This endpoint automatically verifies the OTP before resetting the password.
// @Tags auth
// @Accept json
// @Produce json
// @Param request body models.UpdatePasswordRequest true "Password reset request with OTP verification (reset_token=OTP, email_token=email)"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /auth/reset-password [post]
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req models.UpdatePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request data", err)
		return
	}

	if err := h.authService.ResetPassword(&req); err != nil {
		utils.BadRequestErrorResponse(c, "Password reset failed", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Password reset successful", nil)
}

// GetProfile godoc
// @Summary Get user profile
// @Description Get authenticated user profile
// @Tags auth
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.UserProfileResponse}
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /auth/profile [get]
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
// @Tags auth
// @Accept json
// @Produce json
// @Param request body models.UpdateProfileRequest true "Profile update data"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response{data=models.UserProfileResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /auth/profile [put]
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
// @Tags auth
// @Accept json
// @Produce json
// @Param request body models.ChangePasswordRequest true "Password change data"
// @Security ApiKeyAuth
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /auth/change-password [post]
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
