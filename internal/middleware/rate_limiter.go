package middleware

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// RateLimiterConfig holds configuration for a specific rate limiter
type RateLimiterConfig struct {
	Name           string        // Name identifier for logging
	RequestsPerMin float64       // Requests allowed per minute
	Burst          int           // Burst capacity
	ExpiryDuration time.Duration // How long to keep IP records
	Enabled        bool          // Whether this limiter is enabled
}

// IPRateLimiter is a rate limiter that tracks rate limits by IP address
type IPRateLimiter struct {
	ips      map[string]*rate.Limiter
	mu       *sync.RWMutex
	rate     rate.Limit
	burst    int
	expiry   time.Duration
	lastSeen map[string]time.Time
	config   RateLimiterConfig
}

// NewIPRateLimiter creates a new rate limiter that limits by IP address
func NewIPRateLimiter(config RateLimiterConfig) *IPRateLimiter {
	rateLimit := rate.Limit(config.RequestsPerMin / 60.0) // Convert per minute to per second

	i := &IPRateLimiter{
		ips:      make(map[string]*rate.Limiter),
		mu:       &sync.RWMutex{},
		rate:     rateLimit,
		burst:    config.Burst,
		expiry:   config.ExpiryDuration,
		lastSeen: make(map[string]time.Time),
		config:   config,
	}

	// Start a cleanup goroutine
	go i.cleanupExpired()

	return i
}

// AddIP creates a new rate limiter and adds it to the ips map,
// using the IP address as the key
func (i *IPRateLimiter) AddIP(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()

	limiter := rate.NewLimiter(i.rate, i.burst)
	i.ips[ip] = limiter
	i.lastSeen[ip] = time.Now()

	return limiter
}

// GetLimiter returns the rate limiter for the provided IP address
// if it exists, otherwise calls AddIP to add a new limiter
func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	i.mu.RLock()
	limiter, exists := i.ips[ip]
	i.mu.RUnlock()

	if !exists {
		return i.AddIP(ip)
	}

	// Update last seen
	i.mu.Lock()
	i.lastSeen[ip] = time.Now()
	i.mu.Unlock()

	return limiter
}

// cleanupExpired periodically removes IP entries that haven't been seen recently
func (i *IPRateLimiter) cleanupExpired() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		i.mu.Lock()
		for ip, lastSeen := range i.lastSeen {
			if time.Since(lastSeen) > i.expiry {
				delete(i.ips, ip)
				delete(i.lastSeen, ip)
			}
		}
		i.mu.Unlock()
	}
}

// Global rate limiter variables for different API types
var (
	// Standard API rate limiter
	standardLimiter *IPRateLimiter

	// Authentication endpoints rate limiter
	authRateLimiter *IPRateLimiter

	// Password reset and sensitive operations rate limiter
	passwordRateLimiter *IPRateLimiter

	// Admin operations rate limiter
	adminRateLimiter *IPRateLimiter

	// Public API rate limiter (most permissive)
	publicRateLimiter *IPRateLimiter

	// OTP operations rate limiter (very restrictive)
	otpRateLimiter *IPRateLimiter

	// Event operations rate limiter
	eventRateLimiter *IPRateLimiter
)

// getEnvFloat gets a float value from environment variable with default fallback
func getEnvFloat(key string, defaultValue float64) float64 {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultValue
}

// getEnvInt gets an integer value from environment variable with default fallback
func getEnvInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultValue
}

// getEnvBool gets a boolean value from environment variable with default fallback
func getEnvBool(key string, defaultValue bool) bool {
	if val := os.Getenv(key); val != "" {
		return val == "true" || val == "1"
	}
	return defaultValue
}

// InitRateLimiters initializes all rate limiters based on environment variables
func InitRateLimiters() {
	globalEnabled := getEnvBool("RATE_LIMIT_ENABLED", true)

	// Standard API rate limiter configuration
	standardConfig := RateLimiterConfig{
		Name:           "standard",
		RequestsPerMin: getEnvFloat("STANDARD_RATE_LIMIT", 120.0),
		Burst:          getEnvInt("STANDARD_BURST_LIMIT", 30),
		ExpiryDuration: 1 * time.Hour,
		Enabled:        globalEnabled && getEnvBool("STANDARD_RATE_LIMIT_ENABLED", true),
	}

	// Authentication endpoints rate limiter (more restrictive)
	authConfig := RateLimiterConfig{
		Name:           "auth",
		RequestsPerMin: getEnvFloat("AUTH_RATE_LIMIT", 30.0),
		Burst:          getEnvInt("AUTH_BURST_LIMIT", 10),
		ExpiryDuration: 2 * time.Hour,
		Enabled:        globalEnabled && getEnvBool("AUTH_RATE_LIMIT_ENABLED", true),
	}

	// Password reset and sensitive operations (very restrictive)
	passwordConfig := RateLimiterConfig{
		Name:           "password",
		RequestsPerMin: getEnvFloat("PASSWORD_RATE_LIMIT", 5.0),
		Burst:          getEnvInt("PASSWORD_BURST_LIMIT", 3),
		ExpiryDuration: 4 * time.Hour,
		Enabled:        globalEnabled && getEnvBool("PASSWORD_RATE_LIMIT_ENABLED", true),
	}

	// Admin operations rate limiter
	adminConfig := RateLimiterConfig{
		Name:           "admin",
		RequestsPerMin: getEnvFloat("ADMIN_RATE_LIMIT", 60.0),
		Burst:          getEnvInt("ADMIN_BURST_LIMIT", 20),
		ExpiryDuration: 2 * time.Hour,
		Enabled:        globalEnabled && getEnvBool("ADMIN_RATE_LIMIT_ENABLED", true),
	}

	// Public API rate limiter (most permissive)
	publicConfig := RateLimiterConfig{
		Name:           "public",
		RequestsPerMin: getEnvFloat("PUBLIC_RATE_LIMIT", 200.0),
		Burst:          getEnvInt("PUBLIC_BURST_LIMIT", 50),
		ExpiryDuration: 30 * time.Minute,
		Enabled:        globalEnabled && getEnvBool("PUBLIC_RATE_LIMIT_ENABLED", true),
	}

	// OTP operations (extremely restrictive)
	otpConfig := RateLimiterConfig{
		Name:           "otp",
		RequestsPerMin: getEnvFloat("OTP_RATE_LIMIT", 3.0),
		Burst:          getEnvInt("OTP_BURST_LIMIT", 2),
		ExpiryDuration: 6 * time.Hour,
		Enabled:        globalEnabled && getEnvBool("OTP_RATE_LIMIT_ENABLED", true),
	}

	// Event operations rate limiter
	eventConfig := RateLimiterConfig{
		Name:           "event",
		RequestsPerMin: getEnvFloat("EVENT_RATE_LIMIT", 90.0),
		Burst:          getEnvInt("EVENT_BURST_LIMIT", 25),
		ExpiryDuration: 1 * time.Hour,
		Enabled:        globalEnabled && getEnvBool("EVENT_RATE_LIMIT_ENABLED", true),
	}

	// Initialize all rate limiters
	standardLimiter = NewIPRateLimiter(standardConfig)
	authRateLimiter = NewIPRateLimiter(authConfig)
	passwordRateLimiter = NewIPRateLimiter(passwordConfig)
	adminRateLimiter = NewIPRateLimiter(adminConfig)
	publicRateLimiter = NewIPRateLimiter(publicConfig)
	otpRateLimiter = NewIPRateLimiter(otpConfig)
	eventRateLimiter = NewIPRateLimiter(eventConfig)
}

// extractClientIP extracts the real client IP from request headers
func extractClientIP(c *gin.Context) string {
	ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		ip = c.Request.RemoteAddr
	}

	// Use X-Forwarded-For or X-Real-IP if behind proxy
	forwardedIP := c.Request.Header.Get("X-Forwarded-For")
	if forwardedIP != "" {
		// Use the first IP if multiple are provided
		ips := strings.Split(forwardedIP, ",")
		ip = strings.TrimSpace(ips[0])
	} else if realIP := c.Request.Header.Get("X-Real-IP"); realIP != "" {
		ip = realIP
	}

	return ip
}

// createRateLimitMiddleware creates a middleware function for a specific rate limiter
func createRateLimitMiddleware(limiter *IPRateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip if rate limiter is disabled
		if !limiter.config.Enabled {
			c.Next()
			return
		}

		ip := extractClientIP(c)
		rateLimiter := limiter.GetLimiter(ip)

		if !rateLimiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "Rate limit exceeded",
				"message": "Too many requests for " + limiter.config.Name + " operations. Please try again later.",
				"type":    limiter.config.Name + "_rate_limit",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// Global rate limiter middleware - routes to appropriate limiter based on path
func RateLimiterMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// Skip rate limiting for health checks
		if path == "/health" || path == "/api/v1/health" {
			c.Next()
			return
		}

		ip := extractClientIP(c)
		var limiter *rate.Limiter
		var limiterType string

		// Route to appropriate rate limiter based on path patterns
		switch {
		case strings.Contains(path, "/auth/send-otp") || strings.Contains(path, "/auth/verify-otp"):
			limiter = otpRateLimiter.GetLimiter(ip)
			limiterType = "otp"
		case strings.Contains(path, "/auth/reset-password") || strings.Contains(path, "/auth/change-password"):
			limiter = passwordRateLimiter.GetLimiter(ip)
			limiterType = "password"
		case strings.HasPrefix(path, "/api/v1/auth"):
			limiter = authRateLimiter.GetLimiter(ip)
			limiterType = "auth"
		case strings.HasPrefix(path, "/api/v1/admin"):
			limiter = adminRateLimiter.GetLimiter(ip)
			limiterType = "admin"
		case strings.Contains(path, "/events"):
			limiter = eventRateLimiter.GetLimiter(ip)
			limiterType = "event"
		case strings.HasPrefix(path, "/api/v1/public"):
			limiter = publicRateLimiter.GetLimiter(ip)
			limiterType = "public"
		default:
			limiter = standardLimiter.GetLimiter(ip)
			limiterType = "standard"
		}

		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "Rate limit exceeded",
				"message": "Too many " + limiterType + " requests. Please try again later.",
				"type":    limiterType + "_rate_limit",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// Specific rate limiter middleware functions

// AuthRateLimiter middleware for authentication endpoints
func AuthRateLimiter() gin.HandlerFunc {
	return createRateLimitMiddleware(authRateLimiter)
}

// PasswordRateLimiter middleware for password-related operations
func PasswordRateLimiter() gin.HandlerFunc {
	return createRateLimitMiddleware(passwordRateLimiter)
}

// AdminRateLimiter middleware for admin operations
func AdminRateLimiter() gin.HandlerFunc {
	return createRateLimitMiddleware(adminRateLimiter)
}

// PublicRateLimiter middleware for public API endpoints
func PublicRateLimiter() gin.HandlerFunc {
	return createRateLimitMiddleware(publicRateLimiter)
}

// OTPRateLimiter middleware for OTP operations
func OTPRateLimiter() gin.HandlerFunc {
	return createRateLimitMiddleware(otpRateLimiter)
}

// EventRateLimiter middleware for event operations
func EventRateLimiter() gin.HandlerFunc {
	return createRateLimitMiddleware(eventRateLimiter)
}

// StrictRateLimiter is a more restrictive rate limiter for highly sensitive operations
func StrictRateLimiter() gin.HandlerFunc {
	// Create an extremely restrictive limiter
	strictConfig := RateLimiterConfig{
		Name:           "strict",
		RequestsPerMin: 2.0,
		Burst:          1,
		ExpiryDuration: 8 * time.Hour,
		Enabled:        getEnvBool("STRICT_RATE_LIMIT_ENABLED", true),
	}
	strictLimiter := NewIPRateLimiter(strictConfig)

	return createRateLimitMiddleware(strictLimiter)
}
