package utils

import (
	"fmt"
	"net/http"
)

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
		Message:    message,
		Details:    "One or more fields failed validation",
		StatusCode: http.StatusBadRequest,
		Fields:     fields,
	}
}

// NewNotFoundError creates a not found error
func NewNotFoundError(resource string) *AppError {
	return &AppError{
		Code:       "NOT_FOUND",
		Message:    fmt.Sprintf("%s not found", resource),
		Details:    "The requested resource was not found",
		StatusCode: http.StatusNotFound,
	}
}

// NewUnauthorizedError creates an unauthorized error
func NewUnauthorizedError(message string) *AppError {
	return &AppError{
		Code:       "UNAUTHORIZED",
		Message:    message,
		Details:    "Authentication required or invalid credentials",
		StatusCode: http.StatusUnauthorized,
	}
}

// NewForbiddenError creates a forbidden error
func NewForbiddenError(message string) *AppError {
	return &AppError{
		Code:       "FORBIDDEN",
		Message:    message,
		Details:    "Insufficient permissions to access this resource",
		StatusCode: http.StatusForbidden,
	}
}

// NewConflictError creates a conflict error
func NewConflictError(message string) *AppError {
	return &AppError{
		Code:       "CONFLICT",
		Message:    message,
		Details:    "The request conflicts with the current state of the resource",
		StatusCode: http.StatusConflict,
	}
}

// NewDatabaseError creates a database error
func NewDatabaseError(message string, cause error) *AppError {
	return &AppError{
		Code:       "DATABASE_ERROR",
		Message:    message,
		Details:    "Database operation failed",
		StatusCode: http.StatusInternalServerError,
		Cause:      cause,
	}
}

// NewInternalServerError creates an internal server error
func NewInternalServerError(message string, cause error) *AppError {
	return &AppError{
		Code:       "INTERNAL_SERVER_ERROR",
		Message:    message,
		Details:    "An unexpected error occurred on the server",
		StatusCode: http.StatusInternalServerError,
		Cause:      cause,
	}
}

// NewBusinessLogicError creates a business logic error
func NewBusinessLogicError(message string) *AppError {
	return &AppError{
		Code:       "BUSINESS_LOGIC_ERROR",
		Message:    message,
		Details:    "The operation violates business rules",
		StatusCode: http.StatusBadRequest,
	}
}

// NewExternalServiceError creates an external service error
func NewExternalServiceError(service, message string, cause error) *AppError {
	return &AppError{
		Code:       "EXTERNAL_SERVICE_ERROR",
		Message:    fmt.Sprintf("%s service error: %s", service, message),
		Details:    "External service is currently unavailable",
		StatusCode: http.StatusServiceUnavailable,
		Cause:      cause,
	}
}

// NewRateLimitError creates a rate limit error
func NewRateLimitError(message string) *AppError {
	return &AppError{
		Code:       "RATE_LIMIT_EXCEEDED",
		Message:    message,
		Details:    "Too many requests, please try again later",
		StatusCode: http.StatusTooManyRequests,
	}
}

// NewTimeoutError creates a timeout error
func NewTimeoutError(operation string) *AppError {
	return &AppError{
		Code:       "TIMEOUT_ERROR",
		Message:    fmt.Sprintf("%s operation timed out", operation),
		Details:    "The operation took too long to complete",
		StatusCode: http.StatusRequestTimeout,
	}
}
