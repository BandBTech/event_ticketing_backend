package middleware

import (
	"net/http"
	"strings"

	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
)

// InputSanitizationMiddleware sanitizes all user inputs to prevent injection attacks
func InputSanitizationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check JSON body for malicious patterns
		if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut || c.Request.Method == http.MethodPatch {
			contentType := c.GetHeader("Content-Type")
			if strings.Contains(contentType, "application/json") {
				// Body will be sanitized during binding, but we can add logging here
			}
		}

		// Sanitize query parameters
		for key, values := range c.Request.URL.Query() {
			for i, value := range values {
				if utils.IsSQLInjectionAttempt(value) || utils.IsXSSAttempt(value) {
					utils.ErrorResponse(c, http.StatusBadRequest, "Invalid input detected", nil)
					c.Abort()
					return
				}
				values[i] = utils.SanitizeInput(value)
			}
			c.Request.URL.Query()[key] = values
		}

		// Sanitize path parameters
		for _, param := range c.Params {
			if utils.IsSQLInjectionAttempt(param.Value) || utils.IsXSSAttempt(param.Value) {
				utils.ErrorResponse(c, http.StatusBadRequest, "Invalid input detected in path", nil)
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// SecurityHeadersMiddleware adds security headers to responses
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Prevent MIME type sniffing
		c.Header("X-Content-Type-Options", "nosniff")

		// Enable XSS protection
		c.Header("X-XSS-Protection", "1; mode=block")

		// Prevent clickjacking
		c.Header("X-Frame-Options", "DENY")

		// Strict Transport Security (HSTS)
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		// Content Security Policy
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; object-src 'none'")

		// Referrer Policy
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		// Permissions Policy
		c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		c.Next()
	}
}

// RequestSizeLimitMiddleware limits request body size
func RequestSizeLimitMiddleware(maxSize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)
		c.Next()
	}
}
