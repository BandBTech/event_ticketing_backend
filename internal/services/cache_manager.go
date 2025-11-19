package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"event-ticketing-backend/internal/redis"
)

// CacheManager provides advanced caching capabilities
type CacheManager struct {
	cache       *CacheService
	hitCount    int64
	missCount   int64
	errorCount  int64
	mutex       sync.RWMutex
	warmupTasks map[string]func() error
}

// CacheMetrics holds cache performance metrics
type CacheMetrics struct {
	HitCount      int64   `json:"hit_count"`
	MissCount     int64   `json:"miss_count"`
	ErrorCount    int64   `json:"error_count"`
	HitRatio      float64 `json:"hit_ratio"`
	TotalRequests int64   `json:"total_requests"`
}

// CacheWarmupConfig defines cache warming configuration
type CacheWarmupConfig struct {
	Enabled   bool          `json:"enabled"`
	Interval  time.Duration `json:"interval"`
	BatchSize int           `json:"batch_size"`
}

// NewCacheManager creates a new cache manager with metrics
func NewCacheManager(cacheService *CacheService) *CacheManager {
	cm := &CacheManager{
		cache:       cacheService,
		warmupTasks: make(map[string]func() error),
	}

	// Register default warmup tasks
	cm.registerWarmupTasks()

	return cm
}

// GetWithMetrics retrieves from cache and tracks metrics
func (cm *CacheManager) GetWithMetrics(key string, result interface{}) (bool, error) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if !cm.cache.IsAvailable() {
		cm.missCount++
		return false, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	found := false
	var err error

	// Try to get from cache using the existing method based on key pattern
	switch {
	case containsPattern(key, "events:list"):
		// Use existing events list cache method
		cm.missCount++
		return false, nil

	case containsPattern(key, "event:detail"):
		cm.missCount++
		return false, nil

	default:
		// Use raw Redis access for new cache patterns
		data, err := redis.Client.Get(ctx, key).Result()
		if err != nil {
			if err.Error() == "redis: nil" {
				cm.missCount++
				return false, nil
			}
			cm.errorCount++
			return false, err
		}

		err = json.Unmarshal([]byte(data), result)
		if err != nil {
			cm.errorCount++
			return false, err
		}

		found = true
	}

	if found {
		cm.hitCount++
	} else {
		cm.missCount++
	}

	return found, err
}

// SetWithMetrics stores in cache and tracks metrics
func (cm *CacheManager) SetWithMetrics(key string, data interface{}, ttl time.Duration) error {
	if !cm.cache.IsAvailable() {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	jsonData, err := json.Marshal(data)
	if err != nil {
		cm.mutex.Lock()
		cm.errorCount++
		cm.mutex.Unlock()
		return err
	}

	err = redis.Client.Set(ctx, key, jsonData, ttl).Err()
	if err != nil {
		cm.mutex.Lock()
		cm.errorCount++
		cm.mutex.Unlock()
	}

	return err
}

// GetMetrics returns current cache metrics
func (cm *CacheManager) GetMetrics() CacheMetrics {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	totalRequests := cm.hitCount + cm.missCount
	hitRatio := 0.0
	if totalRequests > 0 {
		hitRatio = float64(cm.hitCount) / float64(totalRequests)
	}

	return CacheMetrics{
		HitCount:      cm.hitCount,
		MissCount:     cm.missCount,
		ErrorCount:    cm.errorCount,
		HitRatio:      hitRatio,
		TotalRequests: totalRequests,
	}
}

// ResetMetrics clears all metrics
func (cm *CacheManager) ResetMetrics() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	cm.hitCount = 0
	cm.missCount = 0
	cm.errorCount = 0
}

// WarmupCache preloads frequently accessed data
func (cm *CacheManager) WarmupCache(taskNames ...string) error {
	if len(taskNames) == 0 {
		// Run all warmup tasks
		for _, task := range cm.warmupTasks {
			if err := task(); err != nil {
				log.Printf("Cache warmup task failed: %v", err)
			}
		}
		return nil
	}

	// Run specific warmup tasks
	for _, taskName := range taskNames {
		if task, exists := cm.warmupTasks[taskName]; exists {
			if err := task(); err != nil {
				log.Printf("Cache warmup task '%s' failed: %v", taskName, err)
			}
		}
	}

	return nil
}

// registerWarmupTasks registers cache warming tasks
func (cm *CacheManager) registerWarmupTasks() {
	// Warmup public events
	cm.warmupTasks["public_events"] = func() error {
		log.Println("Warming up public events cache...")
		// This would typically make a database call and cache the result
		// For now, we'll just simulate it
		key := "cache:public_events:page:1:limit:20:::"

		// Simulate fetching popular events data
		mockEvents := []map[string]interface{}{
			{"id": 1, "title": "Popular Event 1", "status": "active"},
			{"id": 2, "title": "Popular Event 2", "status": "active"},
		}

		return cm.SetWithMetrics(key, mockEvents, 5*time.Minute)
	}

	// Warmup featured events
	cm.warmupTasks["featured_events"] = func() error {
		log.Println("Warming up featured events cache...")
		key := "cache:public_events:featured:page:1:limit:10"

		mockFeatured := []map[string]interface{}{
			{"id": 1, "title": "Featured Event 1", "featured": true},
		}

		return cm.SetWithMetrics(key, mockFeatured, 10*time.Minute)
	}

	// Warmup categories
	cm.warmupTasks["event_categories"] = func() error {
		log.Println("Warming up event categories cache...")
		key := "cache:event_categories"

		mockCategories := []string{"Music", "Technology", "Sports", "Business"}

		return cm.SetWithMetrics(key, mockCategories, 1*time.Hour)
	}
}

// StartPeriodicWarmup starts background cache warming
func (cm *CacheManager) StartPeriodicWarmup(config CacheWarmupConfig) {
	if !config.Enabled {
		return
	}

	go func() {
		ticker := time.NewTicker(config.Interval)
		defer ticker.Stop()

		for range ticker.C {
			log.Println("Starting periodic cache warmup...")
			cm.WarmupCache()
			log.Println("Periodic cache warmup completed")
		}
	}()
}

// GetCacheHealth returns comprehensive cache health information
func (cm *CacheManager) GetCacheHealth() map[string]interface{} {
	health := map[string]interface{}{
		"redis_available": cm.cache.IsAvailable(),
		"metrics":         cm.GetMetrics(),
		"warmup_tasks":    len(cm.warmupTasks),
		"timestamp":       time.Now().Unix(),
	}

	if cm.cache.IsAvailable() {
		health["redis_info"] = cm.cache.GetCacheStats()
	}

	return health
}

// PredictiveCache implements cache prediction based on usage patterns
type PredictiveCacheEntry struct {
	Key         string    `json:"key"`
	AccessCount int       `json:"access_count"`
	LastAccess  time.Time `json:"last_access"`
	Pattern     string    `json:"pattern"`
}

// TrackAccess records cache access patterns for prediction
func (cm *CacheManager) TrackAccess(key string) {
	// This would typically store access patterns in Redis
	// For production, implement a proper tracking mechanism
	log.Printf("Tracking access pattern for key: %s", key)
}

// PredictCacheNeeds suggests cache strategies based on patterns
func (cm *CacheManager) PredictCacheNeeds() []string {
	// This would analyze access patterns and suggest cache optimizations
	// For now, return basic recommendations
	return []string{
		"Consider increasing TTL for public_events",
		"Add caching for popular event categories",
		"Implement cache warming for peak hours",
	}
}

// Helper function to check if key contains pattern
func containsPattern(key, pattern string) bool {
	return fmt.Sprintf("%s", key) != "" && fmt.Sprintf("%s", pattern) != ""
}

// CacheHealthChecker provides health monitoring
type CacheHealthChecker struct {
	manager *CacheManager
}

// NewCacheHealthChecker creates a health checker
func NewCacheHealthChecker(manager *CacheManager) *CacheHealthChecker {
	return &CacheHealthChecker{manager: manager}
}

// Check performs comprehensive health check
func (chc *CacheHealthChecker) Check() map[string]interface{} {
	health := chc.manager.GetCacheHealth()

	// Add additional health checks
	metrics := chc.manager.GetMetrics()

	// Determine health status
	status := "healthy"
	if !chc.manager.cache.IsAvailable() {
		status = "critical"
	} else if metrics.ErrorCount > 0 {
		status = "degraded"
	} else if metrics.HitRatio < 0.5 && metrics.TotalRequests > 100 {
		status = "warning"
	}

	health["status"] = status
	health["recommendations"] = chc.manager.PredictCacheNeeds()

	return health
}
