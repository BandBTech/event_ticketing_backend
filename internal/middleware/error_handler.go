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

		// Check if response has already been written
		if c.Writer.Written() {
			return
		}

		// Use centralized error handler
		utils.HandleError(c, fmt.Errorf("%v", recovered))
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
				// Use centralized error handler
				utils.HandleError(c, err.Err)
			}
		}
	})
}
