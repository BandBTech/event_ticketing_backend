package handlers

import (
	"net/http"
	"strconv"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AuthHandler struct {
	authService       *services.AuthService
	permissionService *services.PermissionService
	db                *gorm.DB
}

func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		authService:       services.NewAuthService(cfg),
		permissionService: services.NewPermissionService(),
		db:                database.GetDB(),
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
		utils.HandleError(c, err)
		return
	}

	if err := h.authService.Register(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "User registered and otp has been sent in your email.", nil)
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
		utils.HandleError(c, err)
		return
	}

	tokens, err := h.authService.RefreshToken(&req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Token refreshed successfully.", tokens)
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
		utils.HandleError(c, utils.NewUnauthorizedError("Unauthorized."))
		return
	}

	// Parse the "all" query parameter
	all := c.DefaultQuery("all", "false") == "true"

	// Logout
	err := h.authService.Logout(userID.(uuid.UUID), all)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Logout successful.", nil)
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
		utils.HandleError(c, utils.NewUnauthorizedError("Unauthorized."))
		return
	}

	user, err := h.authService.GetUserByID(userID.(uuid.UUID))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get user permissions for UI adjustments
	permissions, err := h.permissionService.GetUserPermissions(userID.(uuid.UUID))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Convert permissions to string array
	permissionNames := make([]string, len(permissions))
	for i, perm := range permissions {
		permissionNames[i] = perm.Name
	}

	// Check roles
	isStaffOrManager := false
	isOrganizer := false
	for _, role := range user.Roles {
		if role.Name == "staff" || role.Name == "manager" {
			isStaffOrManager = true
		}
		if role.Name == "organizer" {
			isOrganizer = true
		}
	}

	// Populate OrganizationInfo if applicable
	var orgInfo *models.OrganizerInfoResponse
	if isStaffOrManager && user.OrganizerID != nil {
		// For staff/manager, always use their associated organizer's info
		var organizer models.User
		if err := h.db.Where("id = ?", *user.OrganizerID).First(&organizer).Error; err == nil {
			var onboarding models.OrganizerOnboarding
			h.db.Where("organizer_id = ?", organizer.ID).First(&onboarding)
			orgInfo = &models.OrganizerInfoResponse{
				ID:              organizer.ID,
				BusinessName:    onboarding.BusinessName,
				BusinessLogoURL: onboarding.BusinessLogoURL,
				Status:          organizer.OrganizerStatus,
				Remark:          organizer.AdminRemark,
				ApprovedAt:      organizer.ApprovedAt,
				RejectedAt:      organizer.RejectedAt,
				CreatedAt:       onboarding.CreatedAt,
				UpdatedAt:       onboarding.UpdatedAt,
			}
		}
	} else if isOrganizer {
		// For organizer (who are not staff/manager), use their own info
		var onboarding models.OrganizerOnboarding
		h.db.Where("organizer_id = ?", user.ID).First(&onboarding)
		orgInfo = &models.OrganizerInfoResponse{
			ID:              user.ID,
			BusinessName:    onboarding.BusinessName,
			BusinessLogoURL: onboarding.BusinessLogoURL,
			Status:          user.OrganizerStatus,
			Remark:          user.AdminRemark,
			ApprovedAt:      user.ApprovedAt,
			RejectedAt:      user.RejectedAt,
			CreatedAt:       onboarding.CreatedAt,
			UpdatedAt:       onboarding.UpdatedAt,
		}
	}

	utils.SuccessResponse(c, http.StatusOK, "User profile retrieved successfully.", user.ToProfileResponse(permissionNames, orgInfo))
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
		utils.HandleError(c, utils.NewUnauthorizedError("Unauthorized."))
		return
	}

	var req models.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	if err := h.authService.UpdateProfile(userID.(uuid.UUID), &req); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Profile updated successfully.", nil)
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
		utils.HandleError(c, utils.NewUnauthorizedError("Unauthorized."))
		return
	}

	var req models.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	err := h.authService.ChangePassword(userID.(uuid.UUID), &req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Password changed successfully.", nil)
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
		utils.HandleError(c, err)
		return
	}

	if err := h.authService.RegisterOrganizer(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Organizer registration submitted for approval.", nil)
}

// ApproveOrganizer godoc
// @Summary Approve or reject organizer
// @Description Admin/subadmin can approve, reject, or change status of organizers. Once approved, organizers cannot be rejected but can have their status changed to other values.
// @Tags Admin
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param id path string true "User ID"
// @Param request body models.OrganizerApprovalRequest true "Approval data"
// @Success 200 {object} utils.Response{data=models.UserResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/organizers/{id}/approval [put]
func (h *AuthHandler) ApproveOrganizer(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	var req models.OrganizerApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get admin user from context
	adminID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewUnauthorizedError("User not authenticated."))
		return
	}

	if err = h.authService.ApproveOrganizer(userID, adminID.(uuid.UUID), &req); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organizer approval processed successfully.", nil)
}

// GetPendingOrganizers godoc
// @Summary Get pending organizers
// @Description Get list of organizers pending approval
// @Tags Admin
// @Security ApiKeyAuth
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param sort query string false "Sort by field with optional '-' prefix for desc (e.g., '-created_at', 'first_name')" default("-created_at")
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 401 {object} utils.Response "Unauthorized"
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
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"organizers": organizers,
		"total":      total,
		"page":       page,
		"limit":      limit,
	}
	utils.SuccessResponse(c, http.StatusOK, "Pending organizers fetched successfully.", response)
}

// GetAllOrganizers godoc
// @Summary Get all organizers with their approval status
// @Description Get list of all organizers with their current approval status (pending, approved, rejected, inactive) with search and filter capabilities. Use all_approved=true to get all approved organizers without pagination.
// @Tags Admin
// @Security ApiKeyAuth
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Param sort query string false "Sort by field with optional '-' prefix for desc (e.g., '-created_at', 'first_name', '-organizer_status')" default("-created_at")
// @Param search query string false "Search term for first_name, last_name, or email"
// @Param status query string false "Filter by organizer status (pending, approved, rejected, inactive)"
// @Param account_status query string false "Filter by account status (active, inactive, suspended)"
// @Param all_approved query bool false "If true, returns all approved organizers without pagination" default(false)
// @Success 200 {object} utils.Response{data=models.OrganizerListResponse}
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/organizers [get]
func (h *AuthHandler) GetAllOrganizers(c *gin.Context) {
	// Check if all_approved parameter is set
	allApproved := c.Query("all_approved") == "true"

	if allApproved {
		// Return all approved organizers without pagination
		organizers, err := h.authService.GetAllApprovedOrganizers()
		if err != nil {
			utils.HandleError(c, err)
			return
		}

		response := map[string]interface{}{
			"organizers": organizers,
			"total":      len(organizers),
		}
		utils.SuccessResponse(c, http.StatusOK, "All approved organizers fetched successfully", response)
		return
	}

	// Normal paginated response
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
	search := c.Query("search")
	status := c.Query("status")
	accountStatus := c.Query("account_status")

	organizers, total, err := h.authService.GetAllOrganizers(page, limit, sortParam, search, status, accountStatus)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"organizers": organizers,
		"total":      total,
		"page":       page,
		"limit":      limit,
	}
	utils.SuccessResponse(c, http.StatusOK, "Organizers fetched successfully", response)
}

// GetOrganizerByID godoc
// @Summary Get organizer by ID
// @Description Get detailed information about a specific organizer by their ID including business/onboarding data
// @Tags Admin
// @Security ApiKeyAuth
// @Produce json
// @Param id path string true "Organizer ID"
// @Success 200 {object} utils.Response{data=models.OrganizerDetailResponse}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/organizers/{id} [get]
func (h *AuthHandler) GetOrganizerByID(c *gin.Context) {
	idParam := c.Param("id")
	organizerID, err := uuid.Parse(idParam)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	organizer, err := h.authService.GetOrganizerByID(organizerID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Organizer fetched successfully.", organizer)
}

// GetOTPStatus godoc
// @Summary Get OTP status for debugging
// @Description Get the status of OTP for a specific identifier (admin only)
// @Tags Admin
// @Security ApiKeyAuth
// @Produce json
// @Param identifier query string true "Email or identifier"
// @Param otp_type query string true "OTP type (registration, password_reset, etc.)"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/otp/status [get]
func (h *AuthHandler) GetOTPStatus(c *gin.Context) {
	identifier := c.Query("identifier")
	otpType := c.Query("otp_type")

	if identifier == "" || otpType == "" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	status, err := h.authService.GetOTPStatus(identifier, otpType)
	if err != nil {
		utils.HandleError(c, err)
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
		utils.HandleError(c, err)
		return
	}

	// Login and validate user has "user" role
	tokens, err := h.authService.LoginWithRoleCheck(&req, "user")
	if err != nil {
		utils.HandleError(c, err)
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
		utils.HandleError(c, err)
		return
	}

	// Login and validate user has admin or subadmin role
	tokens, err := h.authService.LoginWithRoleCheck(&req, "admin", "subadmin")
	if err != nil {
		utils.HandleError(c, err)
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
		utils.HandleError(c, err)
		return
	}

	// Login and validate user has organizer, staff, or manager role
	tokens, err := h.authService.LoginWithRoleCheck(&req, "organizer", "staff", "manager")
	if err != nil {
		utils.HandleError(c, err)
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
		utils.HandleError(c, err)
		return
	}

	// Check if user exists and has "user" role
	if err := h.authService.CheckUserRoleForPasswordReset(req.Email, "user"); err != nil {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Send password reset OTP
	if err := h.authService.SendPasswordResetEmail(&req); err != nil {
		utils.HandleError(c, err)
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
		utils.HandleError(c, err)
		return
	}

	// Check if user exists and has admin or subadmin role
	if err := h.authService.CheckUserRoleForPasswordReset(req.Email, "admin", "subadmin"); err != nil {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Send password reset OTP
	if err := h.authService.SendPasswordResetEmail(&req); err != nil {
		utils.HandleError(c, err)
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
		utils.HandleError(c, err)
		return
	}

	// Check if user exists and has organizer, staff, or manager role
	if err := h.authService.CheckUserRoleForPasswordReset(req.Email, "organizer", "staff", "manager"); err != nil {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Send password reset OTP
	if err := h.authService.SendPasswordResetEmail(&req); err != nil {
		utils.HandleError(c, err)
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
		utils.HandleError(c, err)
		return
	}

	// Check if user has "user" role
	if err := h.authService.CheckUserRole(req.EmailToken, "user"); err != nil {
		utils.HandleError(c, err)
		return
	}

	if err := h.authService.ResetPassword(&req); err != nil {
		utils.HandleError(c, err)
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
		utils.HandleError(c, err)
		return
	}

	if err := h.authService.SetUserPassword(req.Email, req.Password); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "User registration completed successfully", nil)
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
		utils.HandleError(c, err)
		return
	}

	// Check if user has admin or subadmin role
	if err := h.authService.CheckUserRole(req.EmailToken, "admin", "subadmin"); err != nil {
		utils.HandleError(c, err)
		return
	}

	if err := h.authService.ResetPassword(&req); err != nil {
		utils.HandleError(c, err)
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
		utils.HandleError(c, err)
		return
	}

	// Check if user has organizer, staff, or manager role
	if err := h.authService.CheckUserRole(req.EmailToken, "organizer", "staff", "manager"); err != nil {
		utils.HandleError(c, err)
		return
	}

	if err := h.authService.ResetPassword(&req); err != nil {
		utils.HandleError(c, err)
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
		utils.HandleError(c, err)
		return
	}

	if err := h.authService.SetOrganizerPassword(req.Email, req.Password); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Organizer registration completed successfully", nil)
}

// UserVerifyOTP godoc
// @Summary Verify OTP for user registration or password reset
// @Description Verify OTP code for user registration or password reset
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
		utils.HandleError(c, err)
		return
	}

	// For password reset, check if user exists and has user role
	if req.OTPType == "password_reset" {
		if err := h.authService.CheckUserRole(req.Identifier, "user"); err != nil {
			utils.HandleError(c, err)
			return
		}
	}

	if err := h.authService.VerifyOTP(&req); err != nil {
		utils.HandleError(c, err)
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
		utils.HandleError(c, err)
		return
	}

	if req.OTPType == "registration" {
		if h.authService.HasTempRegistrationData(req.Identifier) {
			// Resend registration OTP
			if err := h.authService.ResendRegistrationOTP(req.Identifier); err != nil {
				utils.HandleError(c, err)
				return
			}
			utils.SuccessResponse(c, http.StatusOK, "Registration OTP resent successfully", nil)
			return
		} else {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
	} else if req.OTPType == "password_reset" {
		// Check if user exists and has "user" role
		if err := h.authService.CheckUserRoleForPasswordReset(req.Identifier, "user"); err != nil {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}

		// Send OTP
		resetReq := models.ResetPasswordRequest{Email: req.Identifier}
		if err := h.authService.SendPasswordResetEmail(&resetReq); err != nil {
			utils.HandleError(c, err)
			return
		}

		utils.SuccessResponse(c, http.StatusOK, "OTP sent successfully", nil)
	} else {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}
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
		utils.HandleError(c, err)
		return
	}

	// Admin only supports password reset
	if req.OTPType != "password_reset" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Check if user has admin or subadmin role
	if err := h.authService.CheckUserRole(req.Identifier, "admin", "subadmin"); err != nil {
		utils.HandleError(c, err)
		return
	}

	if err := h.authService.VerifyOTP(&req); err != nil {
		utils.HandleError(c, err)
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
		utils.HandleError(c, err)
		return
	}

	// Admin only supports password reset
	if req.OTPType != "password_reset" {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Check if user exists and has admin or subadmin role
	if err := h.authService.CheckUserRoleForPasswordReset(req.Identifier, "admin", "subadmin"); err != nil {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Send OTP
	resetReq := models.ResetPasswordRequest{Email: req.Identifier}
	if err := h.authService.SendPasswordResetEmail(&resetReq); err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "OTP sent successfully", nil)
}

// OrganizerVerifyOTP godoc
// @Summary Verify OTP for organizer registration or password reset
// @Description Verify OTP code for organizer registration or password reset
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
		utils.HandleError(c, err)
		return
	}

	// For password reset, check if user exists and has organizer role
	if req.OTPType == "password_reset" {
		if err := h.authService.CheckUserRole(req.Identifier, "organizer", "staff", "manager"); err != nil {
			utils.HandleError(c, err)
			return
		}
	}

	if err := h.authService.VerifyOTP(&req); err != nil {
		utils.HandleError(c, err)
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
		utils.HandleError(c, err)
		return
	}

	if req.OTPType == "registration" {
		if h.authService.HasTempRegistrationData(req.Identifier) {
			// Resend registration OTP
			if err := h.authService.ResendRegistrationOTP(req.Identifier); err != nil {
				utils.HandleError(c, err)
				return
			}
			utils.SuccessResponse(c, http.StatusOK, "Registration OTP resent successfully", nil)
			return
		} else {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}
	} else if req.OTPType == "password_reset" {
		// Check if user exists and has organizer, staff, or manager role
		if err := h.authService.CheckUserRoleForPasswordReset(req.Identifier, "organizer", "staff", "manager"); err != nil {
			utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
			return
		}

		// Send OTP
		resetReq := models.ResetPasswordRequest{Email: req.Identifier}
		if err := h.authService.SendPasswordResetEmail(&resetReq); err != nil {
			utils.HandleError(c, err)
			return
		}

		utils.SuccessResponse(c, http.StatusOK, "OTP sent successfully", nil)
	} else {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}
}
