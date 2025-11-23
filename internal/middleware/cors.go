package middleware

import (
	"strings"

	"event-ticketing-backend/pkg/config"

	"github.com/gin-gonic/gin"
)

func CORS(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Use configured allowed origins, methods and headers
		allowedOrigins := cfg.CORS.AllowedOrigins
		allowedMethods := cfg.CORS.AllowedMethods
		allowedHeaders := cfg.CORS.AllowedHeaders

		// Check if the request origin is in the allowed origins list
		origin := c.Request.Header.Get("Origin")
		allowOrigin := "*"

		// Only check specific origins if the origin header is set
		if origin != "" {
			allowed := false
			for _, allowedOrigin := range allowedOrigins {
				if origin == strings.TrimSpace(allowedOrigin) {
					allowed = true
					allowOrigin = origin
					break
				}
			}

			// If not allowed, use the first allowed origin (less permissive than *)
			if !allowed && len(allowedOrigins) > 0 {
				allowOrigin = allowedOrigins[0]
			}
		}

		c.Writer.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
		c.Writer.Header().Set("Access-Control-Allow-Methods", allowedMethods)

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// RateLimiter is deprecated - use RateLimiterMiddleware in rate_limiter.go instead
// Keeping this for backward compatibility
func RateLimiter() gin.HandlerFunc {
	return RateLimiterMiddleware()
}
