package middleware

import (
	"log"
	"net/http"

	"event-ticketing-backend/internal/validators"

	"github.com/gin-gonic/gin"
)

// ValidateJSON middleware validates JSON request body against struct validation rules
func ValidateJSON(model interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Create a new instance of the model
		m := model

		// Bind request data to the model
		if err := c.ShouldBindJSON(&m); err != nil {
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

		// Log validation success
		log.Printf("Request validation successful for: %T", m)

		// Set model in context for controllers to use
		c.Set("validatedData", m)
		c.Next()
	}
}

// ValidateQuery middleware validates query parameters
func ValidateQuery(model interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Create a new instance of the model
		m := model

		// Bind query parameters to the model
		if err := c.ShouldBindQuery(&m); err != nil {
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
		c.Set("validatedQuery", m)
		c.Next()
	}
}

// ValidateURI middleware validates URI parameters
func ValidateURI(model interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Create a new instance of the model
		m := model

		// Bind URI parameters to the model
		if err := c.ShouldBindUri(&m); err != nil {
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
		c.Set("validatedURI", m)
		c.Next()
	}
}

// ValidateForm middleware validates form data
func ValidateForm(model interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Create a new instance of the model
		m := model

		// Bind form data to the model
		if err := c.ShouldBind(&m); err != nil {
			// Format validation errors
			validationErrors := validators.FormatErrors(err)

			// Return validation errors
			c.JSON(http.StatusBadRequest, gin.H{
				"status":  "error",
				"message": "Form validation failed",
				"errors":  validationErrors.Errors,
			})
			c.Abort()
			return
		}

		// Set model in context for controllers to use
		c.Set("validatedForm", m)
		c.Next()
	}
}

// ValidationErrorMiddleware is a middleware to catch validation errors
func ValidationErrorMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// Check if there are any validation errors
		if len(c.Errors) > 0 {
			var validationErrors []validators.ValidationError

			for _, err := range c.Errors {
				if err.Type == gin.ErrorTypePrivate {
					validationErrors = append(validationErrors, validators.ValidationError{
						Field:   "request",
						Message: err.Error(),
					})
				}
			}

			if len(validationErrors) > 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"status":  "error",
					"message": "Validation failed",
					"errors":  validationErrors,
				})
				c.Abort()
				return
			}
		}
	}
}
