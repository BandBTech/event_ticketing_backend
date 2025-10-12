package models

// OTPVerifyRequest is the request structure for verifying an OTP
type OTPVerifyRequest struct {
	Identifier string `json:"identifier" binding:"required" example:"user@example.com"` // Email, phone, or user ID
	OTPCode    string `json:"otp_code" binding:"required" example:"123456"`             // The OTP code
	OTPType    string `json:"otp_type" binding:"required" example:"registration"`       // The purpose of OTP
}

// OTPSendRequest is the request structure for sending an OTP
type OTPSendRequest struct {
	Identifier string `json:"identifier" binding:"required" example:"user@example.com"` // Email, phone, or user ID
	OTPType    string `json:"otp_type" binding:"required" example:"registration"`       // The purpose of OTP
}

// OTPResponse is the response structure after OTP operations
type OTPResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	ExpiresIn int    `json:"expires_in,omitempty"` // Time in seconds until OTP expires
}
