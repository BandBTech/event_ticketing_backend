package middleware

import (
	"fmt"
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

// IPRateLimiter is a rate limiter that tracks rate limits by IP address
type IPRateLimiter struct {
	ips    map[string]*rate.Limiter
	mu     *sync.RWMutex
	rate   rate.Limit
	burst  int
	expiry time.Duration
	// Track last seen to cleanup old entries
	lastSeen map[string]time.Time
}

// NewIPRateLimiter creates a new rate limiter that limits by IP address
func NewIPRateLimiter(r rate.Limit, burst int, expiry time.Duration) *IPRateLimiter {
	i := &IPRateLimiter{
		ips:      make(map[string]*rate.Limiter),
		mu:       &sync.RWMutex{},
		rate:     r,
		burst:    burst,
		expiry:   expiry,
		lastSeen: make(map[string]time.Time),
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

// Global rate limiter variables with different tiers
var (
	// Standard API rate limiter (100 requests per minute with burst of 20)
	standardLimiter *IPRateLimiter

	// Auth endpoints rate limiter (more restrictive: 20 requests per minute with burst of 5)
	authLimiter *IPRateLimiter

	// Sensitive operations rate limiter (very restrictive: 3 requests per 10 minutes with burst of 1)
	sensitiveLimiter *IPRateLimiter

	// OTP rate limiter (strict: 1 request per minute with no burst)
	otpLimiter *IPRateLimiter
)

// InitRateLimiters initializes the rate limiters based on environment variables
func InitRateLimiters() {
	// Read rate limiting settings from environment
	enabled := os.Getenv("RATE_LIMIT_ENABLED")

	// If rate limiting is explicitly disabled, use very permissive limits
	if enabled == "false" {
		standardLimiter = NewIPRateLimiter(rate.Limit(1000.0/60.0), 200, 1*time.Hour)
		authLimiter = NewIPRateLimiter(rate.Limit(500.0/60.0), 100, 1*time.Hour)
		sensitiveLimiter = NewIPRateLimiter(rate.Limit(50.0/60.0), 10, 1*time.Hour)
		otpLimiter = NewIPRateLimiter(rate.Limit(100.0/60.0), 20, 1*time.Hour) // Permissive when disabled
		return
	}

	// Try to parse configured rate limits
	requestsStr := os.Getenv("RATE_LIMIT_REQUESTS")
	windowStr := os.Getenv("RATE_LIMIT_WINDOW")

	var requests float64 = 100.0 // Default to 100 requests
	if requestsStr != "" {
		if r, err := strconv.ParseFloat(requestsStr, 64); err == nil && r > 0 {
			requests = r
		}
	}

	var window time.Duration = time.Minute // Default to 1 minute
	if windowStr != "" {
		if w, err := time.ParseDuration(windowStr); err == nil && w > 0 {
			window = w
		}
	}

	// Calculate rate as requests/window in seconds
	rateLimit := rate.Limit(requests / window.Seconds())

	// Initialize the limiters with configurable settings
	standardLimiter = NewIPRateLimiter(rateLimit, int(requests/5), 1*time.Hour)

	// Auth limiter is always more restrictive
	authLimiter = NewIPRateLimiter(rateLimit/5, int(requests/25), 1*time.Hour)

	// Sensitive operations limiter is very restrictive (3 requests per 10 minutes)
	sensitiveLimiter = NewIPRateLimiter(rate.Limit(3.0/600.0), 1, 2*time.Hour)

	// OTP limiter is strict: 1 request per minute with no burst
	otpLimiter = NewIPRateLimiter(rate.Limit(1.0/60.0), 1, 2*time.Hour)
}

// RateLimiterMiddleware returns a middleware that limits request rate based on client IP
func RateLimiterMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
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

		// Choose limiter based on the path
		var limiter *rate.Limiter

		// More restrictive rate limiting for authentication endpoints
		if strings.HasPrefix(c.Request.URL.Path, "/api/v1/auth") {
			limiter = authLimiter.GetLimiter(ip)
		} else {
			limiter = standardLimiter.GetLimiter(ip)
		}

		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "Too many requests",
				"message": "Rate limit exceeded. Please try again later.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// SensitiveRateLimiter is for highly sensitive operations like registration and password reset
func SensitiveRateLimiter() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err != nil {
			ip = c.Request.RemoteAddr
		}

		// Use X-Forwarded-For or X-Real-IP if behind proxy
		if forwardedIP := c.Request.Header.Get("X-Forwarded-For"); forwardedIP != "" {
			ips := strings.Split(forwardedIP, ",")
			ip = strings.TrimSpace(ips[0])
		} else if realIP := c.Request.Header.Get("X-Real-IP"); realIP != "" {
			ip = realIP
		}

		limiter := sensitiveLimiter.GetLimiter(ip)
		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "Too many requests",
				"message":     "Rate limit exceeded for sensitive operation. Please wait 1 minute before trying again.",
				"retry_after": "60", // 1 minute in seconds
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// PasswordResetRateLimiter is specifically for password reset endpoints (1 request per minute per email)
func PasswordResetRateLimiter() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract email from request body
		var req struct {
			Email string `json:"email" binding:"required,email"`
		}

		// Try to bind JSON to get email
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid request",
				"message": "Email is required for password reset",
			})
			c.Abort()
			return
		}

		// Use email as the rate limiting key instead of IP
		emailKey := fmt.Sprintf("rate_limit:password_reset:%s", strings.ToLower(req.Email))

		// Get Redis client (assuming it's available in context or global)
		// For now, we'll use a simple in-memory approach since Redis integration would require more setup
		// In production, this should use Redis with TTL

		// Check if this email has made a request recently
		limiter := otpLimiter.GetLimiter(emailKey)
		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "Too many password reset requests",
				"message":     "Only one password reset request per minute per email. Please wait before requesting another reset.",
				"retry_after": "60", // 1 minute in seconds
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// OTPRateLimiter is specifically for OTP sending endpoints (1 request per minute per email)
func OTPRateLimiter() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract email from request body
		var req struct {
			Email string `json:"email" binding:"required,email"`
		}

		// Try to bind JSON to get email
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Invalid request",
				"message": "Email is required for OTP request",
			})
			c.Abort()
			return
		}

		// Use email as the rate limiting key instead of IP
		emailKey := fmt.Sprintf("rate_limit:otp:%s", strings.ToLower(req.Email))

		// Check if this email has made a request recently
		limiter := otpLimiter.GetLimiter(emailKey)
		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "Too many OTP requests",
				"message":     "Only one OTP request per minute per email. Please wait before requesting another OTP.",
				"retry_after": "60", // 1 minute in seconds
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
