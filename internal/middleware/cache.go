package middleware

import (
	"crypto/md5"
	"fmt"
	"net/http"
	"strings"
	"time"

	"event-ticketing-backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// CacheableEndpoint defines which endpoints should be cached
type CacheableEndpoint struct {
	Method      string
	Path        string
	TTL         time.Duration
	KeyBuilder  func(*gin.Context) string
	ShouldCache func(*gin.Context) bool
}

// CachingMiddleware provides intelligent caching for high-traffic endpoints
type CachingMiddleware struct {
	cacheService *services.CacheService
	endpoints    map[string]CacheableEndpoint
}

// NewCachingMiddleware creates a new caching middleware
func NewCachingMiddleware(cacheService *services.CacheService) *CachingMiddleware {
	cm := &CachingMiddleware{
		cacheService: cacheService,
		endpoints:    make(map[string]CacheableEndpoint),
	}

	cm.registerCacheableEndpoints()
	return cm
}

// registerCacheableEndpoints defines which endpoints should be cached
func (cm *CachingMiddleware) registerCacheableEndpoints() {
	// 🎯 PUBLIC EVENTS - Highest traffic, most cacheable
	cm.endpoints["GET:/api/v1/public/events"] = CacheableEndpoint{
		Method: "GET",
		Path:   "/api/v1/public/events",
		TTL:    5 * time.Minute,
		KeyBuilder: func(c *gin.Context) string {
			return fmt.Sprintf("public_events:page:%s:limit:%s:category:%s:location:%s:search:%s",
				c.DefaultQuery("page", "1"),
				c.DefaultQuery("limit", "20"),
				c.Query("category"),
				c.Query("location"),
				c.Query("search"))
		},
		ShouldCache: func(c *gin.Context) bool { return true },
	}

	cm.endpoints["GET:/api/v1/public/events/:id"] = CacheableEndpoint{
		Method: "GET",
		Path:   "/api/v1/public/events/:id",
		TTL:    10 * time.Minute,
		KeyBuilder: func(c *gin.Context) string {
			return fmt.Sprintf("public_event_detail:%s", c.Param("id"))
		},
		ShouldCache: func(c *gin.Context) bool { return true },
	}

	// ️ ADMIN EVENTS - Dashboard cache
	cm.endpoints["GET:/api/v1/admin/events"] = CacheableEndpoint{
		Method: "GET",
		Path:   "/api/v1/admin/events",
		TTL:    2 * time.Minute,
		KeyBuilder: func(c *gin.Context) string {
			return fmt.Sprintf("admin_events:page:%s:limit:%s:status:%s",
				c.DefaultQuery("page", "1"),
				c.DefaultQuery("limit", "20"),
				c.Query("status"))
		},
		ShouldCache: func(c *gin.Context) bool {
			role := c.GetString("role")
			return role == "admin" || role == "super_admin"
		},
	}

	// 💰 FINANCIAL SUMMARIES - Dashboard data
	cm.endpoints["GET:/api/v1/admin/financial/summary"] = CacheableEndpoint{
		Method: "GET",
		Path:   "/api/v1/admin/financial/summary",
		TTL:    1 * time.Minute, // Short TTL for financial data
		KeyBuilder: func(c *gin.Context) string {
			return fmt.Sprintf("admin_financial_summary:%s:%s",
				c.Query("start_date"),
				c.Query("end_date"))
		},
		ShouldCache: func(c *gin.Context) bool {
			role := c.GetString("role")
			return role == "admin" || role == "super_admin"
		},
	}

	cm.endpoints["GET:/api/v1/organizer/financial/summary"] = CacheableEndpoint{
		Method: "GET",
		Path:   "/api/v1/organizer/financial/summary",
		TTL:    2 * time.Minute,
		KeyBuilder: func(c *gin.Context) string {
			orgID := c.GetString("organizer_id")
			return fmt.Sprintf("organizer_financial:%s:%s:%s",
				orgID,
				c.Query("start_date"),
				c.Query("end_date"))
		},
		ShouldCache: func(c *gin.Context) bool {
			role := c.GetString("role")
			return role == "organizer" && c.GetString("organizer_id") != ""
		},
	}

	// 📋 PENDING ORGANIZERS - Admin dashboard
	cm.endpoints["GET:/api/v1/admin/organizers/pending"] = CacheableEndpoint{
		Method: "GET",
		Path:   "/api/v1/admin/organizers/pending",
		TTL:    5 * time.Minute,
		KeyBuilder: func(c *gin.Context) string {
			return fmt.Sprintf("pending_organizers:page:%s:limit:%s",
				c.DefaultQuery("page", "1"),
				c.DefaultQuery("limit", "20"))
		},
		ShouldCache: func(c *gin.Context) bool {
			role := c.GetString("role")
			return role == "admin" || role == "super_admin"
		},
	}
}

// CacheMiddleware returns the caching middleware function
func (cm *CachingMiddleware) CacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip caching if Redis is not available
		if !cm.cacheService.IsAvailable() {
			c.Next()
			return
		}

		// Check if this endpoint should be cached
		endpointKey := fmt.Sprintf("%s:%s", c.Request.Method, c.FullPath())
		cacheableEndpoint, exists := cm.endpoints[endpointKey]

		if !exists || !cacheableEndpoint.ShouldCache(c) {
			c.Next()
			return
		}

		// Generate cache key
		cacheKey := cacheableEndpoint.KeyBuilder(c)

		// Try to get from cache
		cachedResponse, found := cm.cacheService.GetAPIResponse(generateHash(cacheKey))
		if found {
			// Cache hit! Return cached response
			c.Header("X-Cache-Status", "HIT")
			c.Header("Content-Type", "application/json")
			c.String(http.StatusOK, cachedResponse)
			c.Abort()
			return
		}

		// Cache miss - continue with request processing
		c.Header("X-Cache-Status", "MISS")

		// Capture the response
		writer := &responseWriter{
			ResponseWriter: c.Writer,
			body:           strings.Builder{},
			status:         http.StatusOK,
		}
		c.Writer = writer

		c.Next()

		// Cache the response if it was successful
		if writer.status >= 200 && writer.status < 300 && writer.body.Len() > 0 {
			responseData := writer.body.String()

			// Cache the response
			cm.cacheService.SetAPIResponse(
				generateHash(cacheKey),
				responseData,
				cacheableEndpoint.TTL,
			)
		}
	}
}

// responseWriter captures the response for caching
type responseWriter struct {
	gin.ResponseWriter
	body   strings.Builder
	status int
}

func (rw *responseWriter) Write(data []byte) (int, error) {
	rw.body.Write(data)
	return rw.ResponseWriter.Write(data)
}

func (rw *responseWriter) WriteString(s string) (int, error) {
	rw.body.WriteString(s)
	return rw.ResponseWriter.WriteString(s)
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.status = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

// generateHash creates a hash for the cache key
func generateHash(key string) string {
	hash := md5.Sum([]byte(key))
	return fmt.Sprintf("%x", hash)
}

// CacheInvalidationMiddleware provides cache invalidation for write operations
func (cm *CachingMiddleware) CacheInvalidationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// Only invalidate cache on successful write operations
		if c.Writer.Status() < 200 || c.Writer.Status() >= 300 {
			return
		}

		// Determine what cache to invalidate based on the endpoint
		method := c.Request.Method
		path := c.FullPath()

		switch {
		// Event operations
		case strings.Contains(path, "/events") && (method == "POST" || method == "PUT" || method == "DELETE"):
			cm.invalidateEventCache(c)

		// Financial operations
		case strings.Contains(path, "/financial") && method == "POST":
			cm.invalidateFinancialCache(c)

		// Organizer approval operations
		case strings.Contains(path, "/organizers") && method == "PUT":
			cm.invalidateOrganizerCache(c)

		// Ticket purchase operations
		case strings.Contains(path, "/tickets") && method == "POST":
			cm.invalidateTicketRelatedCache(c)
		}
	}
}

// invalidateEventCache invalidates event-related cache
func (cm *CachingMiddleware) invalidateEventCache(c *gin.Context) {
	eventID := c.Param("id")
	if eventID != "" {
		if id, err := uuid.Parse(eventID); err == nil {
			cm.cacheService.InvalidateEventCache(id)

			// Also invalidate the API response cache for public event detail
			publicEventDetailKey := fmt.Sprintf("public_event_detail:%s", eventID)
			cm.cacheService.DeleteAPIResponse(generateHash(publicEventDetailKey))
		}
	}

	// Also invalidate list caches
	cm.invalidatePatternCache("public_events:*")
	cm.invalidatePatternCache("admin_events:*")
}

// invalidateFinancialCache invalidates financial cache
func (cm *CachingMiddleware) invalidateFinancialCache(c *gin.Context) {
	cm.cacheService.InvalidateFinancialCache()
	cm.invalidatePatternCache("admin_financial_summary:*")
	cm.invalidatePatternCache("organizer_financial:*")
}

// invalidateOrganizerCache invalidates organizer-related cache
func (cm *CachingMiddleware) invalidateOrganizerCache(c *gin.Context) {
	cm.invalidatePatternCache("pending_organizers:*")
}

// invalidateTicketRelatedCache invalidates ticket purchase related cache
func (cm *CachingMiddleware) invalidateTicketRelatedCache(c *gin.Context) {
	// When tickets are purchased, event availability changes
	cm.invalidatePatternCache("public_events:*")
	cm.invalidatePatternCache("admin_financial_summary:*")
	cm.invalidatePatternCache("organizer_financial:*")

	// Also invalidate specific event if we know the event ID
	eventID := c.Query("event_id")
	if eventID == "" {
		eventID = c.PostForm("event_id")
	}

	if eventID != "" {
		if id, err := uuid.Parse(eventID); err == nil {
			cm.cacheService.InvalidateEventCache(id)
		}
	}
}

// invalidatePatternCache invalidates cache entries matching a pattern
func (cm *CachingMiddleware) invalidatePatternCache(pattern string) {
	// This is a simplified version - you might want to implement pattern-based invalidation
	// For now, we'll let the existing cache service handle it
}

// GetCacheStats returns cache statistics
func (cm *CachingMiddleware) GetCacheStats() map[string]interface{} {
	stats := cm.cacheService.GetCacheStats()

	// Add middleware-specific stats
	stats["cacheable_endpoints"] = len(cm.endpoints)
	stats["endpoint_list"] = make([]string, 0, len(cm.endpoints))

	for endpoint := range cm.endpoints {
		stats["endpoint_list"] = append(stats["endpoint_list"].([]string), endpoint)
	}

	return stats
}

// AddCacheableEndpoint allows dynamic addition of cacheable endpoints
func (cm *CachingMiddleware) AddCacheableEndpoint(endpoint CacheableEndpoint) {
	key := fmt.Sprintf("%s:%s", endpoint.Method, endpoint.Path)
	cm.endpoints[key] = endpoint
}

// RemoveCacheableEndpoint removes an endpoint from caching
func (cm *CachingMiddleware) RemoveCacheableEndpoint(method, path string) {
	key := fmt.Sprintf("%s:%s", method, path)
	delete(cm.endpoints, key)
}
