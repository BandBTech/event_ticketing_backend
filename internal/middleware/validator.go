package middleware

import (
	"net/http"

	"event-ticketing-backend/internal/validators"

	"github.com/gin-gonic/gin"
)

// Validate middleware validates request data against struct validation rules
func Validate(model interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Bind request data to the model
		if err := c.ShouldBindJSON(model); err != nil {
			// Format validation errors
			validationErrors := validators.FormatErrors(err)

			// Return validation errors
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  "error",
				"message": "Validation failed",
				"errors":  validationErrors.Errors,
			})
			c.Abort()
			return
		}

		// Set model in context for controllers to use
		c.Set("validatedData", model)
		c.Next()
	}
}

// ValidateQuery middleware validates query parameters
func ValidateQuery(model interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Bind query parameters to the model
		if err := c.ShouldBindQuery(model); err != nil {
			// Format validation errors
			validationErrors := validators.FormatErrors(err)

			// Return validation errors
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  "error",
				"message": "Query validation failed",
				"errors":  validationErrors.Errors,
			})
			c.Abort()
			return
		}

		// Set model in context for controllers to use
		c.Set("validatedQuery", model)
		c.Next()
	}
}

// ValidateURI middleware validates URI parameters
func ValidateURI(model interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Bind URI parameters to the model
		if err := c.ShouldBindUri(model); err != nil {
			// Format validation errors
			validationErrors := validators.FormatErrors(err)

			// Return validation errors
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  "error",
				"message": "URI validation failed",
				"errors":  validationErrors.Errors,
			})
			c.Abort()
			return
		}

		// Set model in context for controllers to use
		c.Set("validatedURI", model)
		c.Next()
	}
}
