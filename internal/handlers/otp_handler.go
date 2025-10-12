package handlers

import (
	"net/http"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
)

// VerifyOTP godoc
// @Summary Verify OTP code
// @Description Verify a one-time password code for various purposes (registration, password reset, 2FA)
// @Tags auth
// @Accept json
// @Produce json
// @Param request body models.OTPVerifyRequest true "OTP verification request"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /auth/verify-otp [post]
func (h *AuthHandler) VerifyOTP(c *gin.Context) {
	var req models.OTPVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request data", err)
		return
	}

	if err := h.authService.VerifyOTP(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "OTP verification failed", err)
		return
	}

	// Respond with appropriate message based on OTP type
	var message string
	switch req.OTPType {
	case "registration":
		message = "Email verified successfully"
	case "password_reset":
		message = "OTP verified successfully, you can now reset your password"
	case "2fa":
		message = "Two-factor authentication successful"
	default:
		message = "OTP verified successfully"
	}

	utils.SuccessResponse(c, http.StatusOK, message, nil)
}

// SendOTP godoc
// @Summary Send OTP code
// @Description Send a one-time password code for various purposes (registration, password reset, 2FA)
// @Tags auth
// @Accept json
// @Produce json
// @Param request body models.OTPSendRequest true "OTP send request"
// @Success 200 {object} utils.Response{data=models.OTPResponse}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /auth/send-otp [post]
func (h *AuthHandler) SendOTP(c *gin.Context) {
	var req models.OTPSendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request data", err)
		return
	}

	response, err := h.authService.GenerateAndSendOTP(&req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Failed to send OTP", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "OTP sent successfully", response)
}
