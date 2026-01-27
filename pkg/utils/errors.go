package utils

import (
	"fmt"
	"net/http"
	"strings"
)

// formatMessage formats error messages to be clear, concise, and properly punctuated
func formatMessage(message string) string {
	if message == "" {
		return "An error occurred."
	}

	// Capitalize first letter
	message = strings.ToUpper(string(message[0])) + message[1:]

	// Ensure it ends with a period
	if !strings.HasSuffix(message, ".") {
		message += "."
	}

	return message
}

// AppError represents a custom application error
type AppError struct {
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	Details    string                 `json:"details,omitempty"`
	StatusCode int                    `json:"-"`
	Fields     map[string]interface{} `json:"fields,omitempty"`
	Cause      error                  `json:"-"`
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("%s: %s", e.Message, e.Details)
	}
	return e.Message
}

// Unwrap implements the unwrap interface for error wrapping
func (e *AppError) Unwrap() error {
	return e.Cause
}

// Error constructors for common scenarios

// NewValidationError creates a validation error
func NewValidationError(message string, fields map[string]interface{}) *AppError {
	return &AppError{
		Code:       "VALIDATION_ERROR",
		Message:    formatMessage(message),
		Details:    "One or more fields failed validation.",
		StatusCode: http.StatusBadRequest,
		Fields:     fields,
	}
}

// NewNotFoundError creates a not found error
func NewNotFoundError(resource string) *AppError {
	return &AppError{
		Code:       "NOT_FOUND",
		Message:    fmt.Sprintf("%s not found.", strings.Title(resource)),
		Details:    "The requested resource was not found.",
		StatusCode: http.StatusNotFound,
	}
}

// NewUnauthorizedError creates an unauthorized error
func NewUnauthorizedError(message string) *AppError {
	return &AppError{
		Code:       "UNAUTHORIZED",
		Message:    formatMessage(message),
		Details:    "Authentication required or invalid credentials.",
		StatusCode: http.StatusUnauthorized,
	}
}

// NewForbiddenError creates a forbidden error
func NewForbiddenError(message string) *AppError {
	return &AppError{
		Code:       "FORBIDDEN",
		Message:    formatMessage(message),
		Details:    "Insufficient permissions to access this resource.",
		StatusCode: http.StatusForbidden,
	}
}

// NewConflictError creates a conflict error
func NewConflictError(message string) *AppError {
	return &AppError{
		Code:       "CONFLICT",
		Message:    formatMessage(message),
		Details:    "The request conflicts with the current state of the resource.",
		StatusCode: http.StatusConflict,
	}
}

// NewDatabaseError creates a database error
func NewDatabaseError(message string, cause error) *AppError {
	return &AppError{
		Code:       "DATABASE_ERROR",
		Message:    formatMessage(message),
		Details:    "Database operation failed.",
		StatusCode: http.StatusInternalServerError,
		Cause:      cause,
	}
}

// NewInternalServerError creates an internal server error
func NewInternalServerError(message string, cause error) *AppError {
	return &AppError{
		Code:       "INTERNAL_SERVER_ERROR",
		Message:    formatMessage(message),
		Details:    "An unexpected error occurred on the server.",
		StatusCode: http.StatusInternalServerError,
		Cause:      cause,
	}
}

// NewBusinessLogicError creates a business logic error
func NewBusinessLogicError(message string) *AppError {
	return &AppError{
		Code:       "BUSINESS_LOGIC_ERROR",
		Message:    formatMessage(message),
		Details:    "The operation violates business rules.",
		StatusCode: http.StatusBadRequest,
	}
}

// NewExternalServiceError creates an external service error
func NewExternalServiceError(service, message string, cause error) *AppError {
	return &AppError{
		Code:       "EXTERNAL_SERVICE_ERROR",
		Message:    fmt.Sprintf("%s service error: %s.", strings.Title(service), formatMessage(message)),
		Details:    "External service is currently unavailable.",
		StatusCode: http.StatusServiceUnavailable,
		Cause:      cause,
	}
}

// NewRateLimitError creates a rate limit error
func NewRateLimitError(message string) *AppError {
	return &AppError{
		Code:       "RATE_LIMIT_EXCEEDED",
		Message:    formatMessage(message),
		Details:    "Too many requests, please try again later.",
		StatusCode: http.StatusTooManyRequests,
	}
}

// NewTimeoutError creates a timeout error
func NewTimeoutError(operation string) *AppError {
	return &AppError{
		Code:       "TIMEOUT_ERROR",
		Message:    fmt.Sprintf("%s operation timed out.", strings.Title(operation)),
		Details:    "The operation took too long to complete.",
		StatusCode: http.StatusRequestTimeout,
	}
}

// NewSessionExpiredError creates a session expired error
func NewSessionExpiredError() *AppError {
	return &AppError{
		Code:       "SESSION_EXPIRED",
		Message:    "Your session has expired.",
		Details:    "Please log in again to continue.",
		StatusCode: http.StatusUnauthorized,
	}
}

// NewAccountInactiveError creates an account inactive error
func NewAccountInactiveError() *AppError {
	return &AppError{
		Code:       "ACCOUNT_INACTIVE",
		Message:    "Your account is inactive.",
		Details:    "Please contact support to reactivate your account.",
		StatusCode: http.StatusForbidden,
	}
}

// NewAccountSuspendedError creates an account suspended error
func NewAccountSuspendedError() *AppError {
	return &AppError{
		Code:       "ACCOUNT_SUSPENDED",
		Message:    "Your account has been suspended.",
		Details:    "Please contact support for more information.",
		StatusCode: http.StatusForbidden,
	}
}

// NewInvalidCredentialsError creates an invalid credentials error
func NewInvalidCredentialsError() *AppError {
	return &AppError{
		Code:       "INVALID_CREDENTIALS",
		Message:    "Invalid email or password.",
		Details:    "Please check your credentials and try again.",
		StatusCode: http.StatusUnauthorized,
	}
}

// NewTokenExpiredError creates a token expired error
func NewTokenExpiredError() *AppError {
	return &AppError{
		Code:       "TOKEN_EXPIRED",
		Message:    "Your token has expired.",
		Details:    "Please refresh your token or log in again.",
		StatusCode: http.StatusUnauthorized,
	}
}

// NewInvalidTokenError creates an invalid token error
func NewInvalidTokenError() *AppError {
	return &AppError{
		Code:       "INVALID_TOKEN",
		Message:    "Invalid token provided.",
		Details:    "Please provide a valid token.",
		StatusCode: http.StatusUnauthorized,
	}
}

// NewOrganizerInactiveError creates an organizer inactive error
func NewOrganizerInactiveError() *AppError {
	return &AppError{
		Code:       "ORGANIZER_INACTIVE",
		Message:    "Your organizer account is inactive.",
		Details:    "Please complete your profile and submit for approval.",
		StatusCode: http.StatusForbidden,
	}
}

// NewOrganizerPendingError creates an organizer pending error
func NewOrganizerPendingError() *AppError {
	return &AppError{
		Code:       "ORGANIZER_PENDING",
		Message:    "Your organizer account is pending approval.",
		Details:    "Please wait for admin review.",
		StatusCode: http.StatusForbidden,
	}
}

// NewOrganizerRejectedError creates an organizer rejected error
func NewOrganizerRejectedError() *AppError {
	return &AppError{
		Code:       "ORGANIZER_REJECTED",
		Message:    "Your organizer account has been rejected.",
		Details:    "Please contact support for more information.",
		StatusCode: http.StatusForbidden,
	}
}
