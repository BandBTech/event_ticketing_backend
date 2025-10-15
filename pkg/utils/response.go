package utils

import (
	"net/http"
	"time"

	"event-ticketing-backend/internal/validators"

	"github.com/gin-gonic/gin"
)

// Response represents the standard API response structure
type Response struct {
	Success   bool        `json:"success"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	Error     *ErrorInfo  `json:"error,omitempty"`
	Timestamp string      `json:"timestamp"`
	RequestID string      `json:"request_id,omitempty"`
}

// ErrorInfo provides detailed error information
type ErrorInfo struct {
	Code    string      `json:"code"`
	Details string      `json:"details"`
	Fields  interface{} `json:"fields,omitempty"` // For validation errors
}

// SuccessResponse sends a successful response
func SuccessResponse(c *gin.Context, statusCode int, message string, data interface{}) {
	c.JSON(statusCode, Response{
		Success:   true,
		Message:   message,
		Data:      data,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		RequestID: getRequestID(c),
	})
}

// ErrorResponse sends a generic error response
func ErrorResponse(c *gin.Context, statusCode int, message string, err error) {
	errorInfo := &ErrorInfo{
		Code:    "GENERIC_ERROR",
		Details: message,
	}

	if err != nil {
		errorInfo.Details = err.Error()
	}

	c.JSON(statusCode, Response{
		Success:   false,
		Message:   message,
		Error:     errorInfo,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		RequestID: getRequestID(c),
	})
}

// ValidationErrorResponse sends a validation error response with user-friendly messages
func ValidationErrorResponse(c *gin.Context, message string, err error) {
	errorInfo := &ErrorInfo{
		Code:    "VALIDATION_ERROR",
		Details: "Request validation failed",
	}

	// Format validation errors into user-friendly messages
	if err != nil {
		validationErrors := validators.FormatErrors(err)
		if len(validationErrors.Errors) > 0 {
			// Use the first validation error as the main details
			errorInfo.Details = validationErrors.Errors[0].Message

			// Include all validation errors in the fields for detailed response
			fields := make(map[string]interface{})
			for _, valErr := range validationErrors.Errors {
				fields[valErr.Field] = valErr.Message
			}
			errorInfo.Fields = fields
		} else {
			errorInfo.Details = err.Error()
		}
	}

	c.JSON(http.StatusBadRequest, Response{
		Success:   false,
		Message:   message,
		Error:     errorInfo,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		RequestID: getRequestID(c),
	})
}

// BadRequestErrorResponse sends a bad request error response
func BadRequestErrorResponse(c *gin.Context, message string, err error) {
	errorInfo := &ErrorInfo{
		Code:    "BAD_REQUEST",
		Details: message,
	}

	if err != nil {
		errorInfo.Details = err.Error()
	}

	c.JSON(http.StatusBadRequest, Response{
		Success:   false,
		Message:   message,
		Error:     errorInfo,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		RequestID: getRequestID(c),
	})
}

// UnauthorizedErrorResponse sends an unauthorized error response
func UnauthorizedErrorResponse(c *gin.Context, message string, err error) {
	errorInfo := &ErrorInfo{
		Code:    "UNAUTHORIZED",
		Details: "Authentication required or invalid credentials",
	}

	if err != nil {
		errorInfo.Details = err.Error()
	}

	c.JSON(http.StatusUnauthorized, Response{
		Success:   false,
		Message:   message,
		Error:     errorInfo,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		RequestID: getRequestID(c),
	})
}

// ForbiddenErrorResponse sends a forbidden error response
func ForbiddenErrorResponse(c *gin.Context, message string, err error) {
	errorInfo := &ErrorInfo{
		Code:    "FORBIDDEN",
		Details: "Insufficient permissions to access this resource",
	}

	if err != nil {
		errorInfo.Details = err.Error()
	}

	c.JSON(http.StatusForbidden, Response{
		Success:   false,
		Message:   message,
		Error:     errorInfo,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		RequestID: getRequestID(c),
	})
}

// NotFoundErrorResponse sends a not found error response
func NotFoundErrorResponse(c *gin.Context, message string, err error) {
	errorInfo := &ErrorInfo{
		Code:    "NOT_FOUND",
		Details: "The requested resource was not found",
	}

	if err != nil {
		errorInfo.Details = err.Error()
	}

	c.JSON(http.StatusNotFound, Response{
		Success:   false,
		Message:   message,
		Error:     errorInfo,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		RequestID: getRequestID(c),
	})
}

// ConflictErrorResponse sends a conflict error response
func ConflictErrorResponse(c *gin.Context, message string, err error) {
	errorInfo := &ErrorInfo{
		Code:    "CONFLICT",
		Details: "The request conflicts with the current state of the resource",
	}

	if err != nil {
		errorInfo.Details = err.Error()
	}

	c.JSON(http.StatusConflict, Response{
		Success:   false,
		Message:   message,
		Error:     errorInfo,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		RequestID: getRequestID(c),
	})
}

// InternalServerErrorResponse sends an internal server error response
func InternalServerErrorResponse(c *gin.Context, message string, err error) {
	errorInfo := &ErrorInfo{
		Code:    "INTERNAL_SERVER_ERROR",
		Details: "An unexpected error occurred on the server",
	}

	// Don't expose internal error details in production
	if gin.Mode() != gin.ReleaseMode && err != nil {
		errorInfo.Details = err.Error()
	}

	c.JSON(http.StatusInternalServerError, Response{
		Success:   false,
		Message:   message,
		Error:     errorInfo,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		RequestID: getRequestID(c),
	})
}

// ValidationErrorWithFieldsResponse sends a validation error with field details
func ValidationErrorWithFieldsResponse(c *gin.Context, message string, fields interface{}) {
	errorInfo := &ErrorInfo{
		Code:    "VALIDATION_ERROR",
		Details: "One or more fields failed validation",
		Fields:  fields,
	}

	c.JSON(http.StatusBadRequest, Response{
		Success:   false,
		Message:   message,
		Error:     errorInfo,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		RequestID: getRequestID(c),
	})
}

// DatabaseErrorResponse sends a database error response
func DatabaseErrorResponse(c *gin.Context, message string, err error) {
	errorInfo := &ErrorInfo{
		Code:    "DATABASE_ERROR",
		Details: "Database operation failed",
	}

	// Don't expose database details in production
	if gin.Mode() != gin.ReleaseMode && err != nil {
		errorInfo.Details = err.Error()
	}

	c.JSON(http.StatusInternalServerError, Response{
		Success:   false,
		Message:   message,
		Error:     errorInfo,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		RequestID: getRequestID(c),
	})
}

// ServiceUnavailableErrorResponse sends a service unavailable error response
func ServiceUnavailableErrorResponse(c *gin.Context, message string, err error) {
	errorInfo := &ErrorInfo{
		Code:    "SERVICE_UNAVAILABLE",
		Details: "The service is temporarily unavailable",
	}

	if err != nil {
		errorInfo.Details = err.Error()
	}

	c.JSON(http.StatusServiceUnavailable, Response{
		Success:   false,
		Message:   message,
		Error:     errorInfo,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		RequestID: getRequestID(c),
	})
}

// getRequestID extracts request ID from context or generates one
func getRequestID(c *gin.Context) string {
	if requestID := c.GetString("request_id"); requestID != "" {
		return requestID
	}
	// Could generate a UUID here if needed
	return ""
}

// HandleAppError handles AppError and sends appropriate response
func HandleAppError(c *gin.Context, err error) {
	if appErr, ok := err.(*AppError); ok {
		errorInfo := &ErrorInfo{
			Code:    appErr.Code,
			Details: appErr.Details,
			Fields:  appErr.Fields,
		}

		c.JSON(appErr.StatusCode, Response{
			Success:   false,
			Message:   appErr.Message,
			Error:     errorInfo,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			RequestID: getRequestID(c),
		})
	} else {
		// Fallback to internal server error for unknown errors
		InternalServerErrorResponse(c, "An unexpected error occurred", err)
	}
}
