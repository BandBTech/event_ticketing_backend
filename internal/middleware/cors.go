package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Hardcoded allowed origins, methods and headers
		allowedOrigins := []string{
			"http://localhost:3000",
			"http://localhost:5173",
			"http://localhost:8082",
			"https://timroticket.com",
			"https://www.timroticket.com",
			"https://sandbox-admin.timroticket.com",
			"https://sandbox-organizer.timroticket.com",
			"https://user.timroticket.com",
			"https://api.timroticket.com",
			"https://secureadmin.timroticket.com",
		}

		allowedMethods := "GET,POST,PUT,DELETE,OPTIONS,PATCH"
		allowedHeaders := "Content-Type,Content-Length,Accept-Encoding,X-CSRF-Token,Authorization,accept,origin,Cache-Control,X-Requested-With"

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
