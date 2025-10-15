package middleware

import (
	"fmt"
	"log"
	"runtime/debug"

	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
)

// ErrorHandler middleware handles panics and errors
func ErrorHandler() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		// Log the panic with stack trace
		log.Printf("Panic recovered: %v\n%s", recovered, debug.Stack())

		// Check if it's an abort error (already handled)
		if c.IsAborted() {
			return
		}

		// Return internal server error response
		utils.InternalServerErrorResponse(c, "An unexpected error occurred", fmt.Errorf("%v", recovered))
	})
}

// GlobalErrorHandler handles any remaining errors
func GlobalErrorHandler() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		c.Next()

		// Check for errors after processing the request
		if len(c.Errors) > 0 {
			// Get the last error (most recent)
			err := c.Errors.Last()

			// Log the error
			log.Printf("Request error: %v", err.Err)

			// If response hasn't been written yet
			if !c.Writer.Written() {
				// Check if it's an AppError first
				if appErr, ok := err.Err.(*utils.AppError); ok {
					utils.HandleAppError(c, appErr)
					return
				}

				// Handle other error types
				switch err.Type {
				case gin.ErrorTypeBind:
					utils.ValidationErrorResponse(c, "Invalid request data", err.Err)
				case gin.ErrorTypePublic:
					utils.BadRequestErrorResponse(c, err.Error(), err.Err)
				default:
					utils.InternalServerErrorResponse(c, "An unexpected error occurred", err.Err)
				}
			}
		}
	})
}
