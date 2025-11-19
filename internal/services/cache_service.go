package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/redis"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

type CacheService struct {
	client        *goredis.Client
	defaultExpiry time.Duration
	eventExpiry   time.Duration
	userExpiry    time.Duration
	sessionExpiry time.Duration
}

type CacheConfig struct {
	DefaultExpiry time.Duration
	EventExpiry   time.Duration
	UserExpiry    time.Duration
	SessionExpiry time.Duration
}

func NewCacheService(config *CacheConfig) *CacheService {
	if config == nil {
		config = &CacheConfig{
			DefaultExpiry: 15 * time.Minute,
			EventExpiry:   30 * time.Minute,
			UserExpiry:    5 * time.Minute,
			SessionExpiry: 24 * time.Hour,
		}
	}

	return &CacheService{
		client:        redis.Client,
		defaultExpiry: config.DefaultExpiry,
		eventExpiry:   config.EventExpiry,
		userExpiry:    config.UserExpiry,
		sessionExpiry: config.SessionExpiry,
	}
}

// Cache Keys
const (
	// Event Cache Keys
	EventListKey       = "events:list"
	EventDetailKey     = "event:detail:%d"
	EventSearchKey     = "events:search:%s"
	OrganizerEventsKey = "organizer:events:%s"

	// User Cache Keys
	UserDetailKey  = "user:detail:%s"
	UserSessionKey = "user:session:%s"

	// Financial Cache Keys
	FinancialSummaryKey = "financial:summary:admin"
	OrganizerFinKey     = "financial:organizer:%s"
	EventSalesKey       = "financial:sales:event:%d"

	// Rate Limiting Keys
	RateLimitKey    = "rate_limit:%s:%s"
	LoginAttemptKey = "login_attempts:%s"

	// API Response Cache
	APIResponseKey = "api:response:%s"
)

// Event Caching Methods

func (c *CacheService) GetEventsList(page, limit int, category, location string) ([]models.Event, bool) {
	if c.client == nil {
		return nil, false
	}

	key := fmt.Sprintf("%s:page:%d:limit:%d:cat:%s:loc:%s", EventListKey, page, limit, category, location)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return nil, false
	}

	var events []models.Event
	if err := json.Unmarshal([]byte(val), &events); err != nil {
		log.Printf("Cache unmarshal error: %v", err)
		return nil, false
	}

	return events, true
}

func (c *CacheService) SetEventsList(events []models.Event, page, limit int, category, location string) {
	if c.client == nil {
		return
	}

	key := fmt.Sprintf("%s:page:%d:limit:%d:cat:%s:loc:%s", EventListKey, page, limit, category, location)

	data, err := json.Marshal(events)
	if err != nil {
		log.Printf("Cache marshal error: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c.client.Set(ctx, key, data, c.eventExpiry)
}

func (c *CacheService) GetEventDetail(eventID uuid.UUID) (*models.Event, bool) {
	if c.client == nil {
		return nil, false
	}

	key := fmt.Sprintf(EventDetailKey, eventID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return nil, false
	}

	var event models.Event
	if err := json.Unmarshal([]byte(val), &event); err != nil {
		log.Printf("Cache unmarshal error: %v", err)
		return nil, false
	}

	return &event, true
}

func (c *CacheService) SetEventDetail(event *models.Event) {
	if c.client == nil || event == nil {
		return
	}

	key := fmt.Sprintf(EventDetailKey, event.ID)

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("Cache marshal error: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c.client.Set(ctx, key, data, c.eventExpiry)
}

// User Caching Methods

func (c *CacheService) GetUser(userID uuid.UUID) (*models.User, bool) {
	if c.client == nil {
		return nil, false
	}

	key := fmt.Sprintf(UserDetailKey, userID.String())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return nil, false
	}

	var user models.User
	if err := json.Unmarshal([]byte(val), &user); err != nil {
		log.Printf("Cache unmarshal error: %v", err)
		return nil, false
	}

	return &user, true
}

func (c *CacheService) SetUser(user *models.User) {
	if c.client == nil || user == nil {
		return
	}

	key := fmt.Sprintf(UserDetailKey, user.ID.String())

	data, err := json.Marshal(user)
	if err != nil {
		log.Printf("Cache marshal error: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c.client.Set(ctx, key, data, c.userExpiry)
}

// Session Caching

func (c *CacheService) GetUserSession(sessionID string) (uuid.UUID, bool) {
	if c.client == nil {
		return uuid.Nil, false
	}

	key := fmt.Sprintf(UserSessionKey, sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return uuid.Nil, false
	}

	userID, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, false
	}

	return userID, true
}

func (c *CacheService) SetUserSession(sessionID string, userID uuid.UUID) {
	if c.client == nil {
		return
	}

	key := fmt.Sprintf(UserSessionKey, sessionID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c.client.Set(ctx, key, userID.String(), c.sessionExpiry)
}

// Financial Data Caching

func (c *CacheService) GetFinancialSummary() (*models.AdminFinancialSummary, bool) {
	if c.client == nil {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	val, err := c.client.Get(ctx, FinancialSummaryKey).Result()
	if err != nil {
		return nil, false
	}

	var summary models.AdminFinancialSummary
	if err := json.Unmarshal([]byte(val), &summary); err != nil {
		log.Printf("Cache unmarshal error: %v", err)
		return nil, false
	}

	return &summary, true
}

func (c *CacheService) SetFinancialSummary(summary *models.AdminFinancialSummary) {
	if c.client == nil || summary == nil {
		return
	}

	data, err := json.Marshal(summary)
	if err != nil {
		log.Printf("Cache marshal error: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Financial data cache for 5 minutes only due to frequent updates
	c.client.Set(ctx, FinancialSummaryKey, data, 5*time.Minute)
}

// Rate Limiting Methods

func (c *CacheService) CheckRateLimit(userID, endpoint string, limit int, window time.Duration) (bool, error) {
	if c.client == nil {
		return true, nil // Allow if no cache
	}

	key := fmt.Sprintf(RateLimitKey, userID, endpoint)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	current, err := c.client.Get(ctx, key).Result()
	if err != nil {
		// Key doesn't exist, create it
		c.client.Set(ctx, key, "1", window)
		return true, nil
	}

	count, err := strconv.Atoi(current)
	if err != nil {
		return false, err
	}

	if count >= limit {
		return false, nil
	}

	// Increment counter
	c.client.Incr(ctx, key)
	return true, nil
}

func (c *CacheService) GetLoginAttempts(identifier string) int {
	if c.client == nil {
		return 0
	}

	key := fmt.Sprintf(LoginAttemptKey, identifier)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return 0
	}

	attempts, _ := strconv.Atoi(val)
	return attempts
}

func (c *CacheService) IncrementLoginAttempts(identifier string) {
	if c.client == nil {
		return
	}

	key := fmt.Sprintf(LoginAttemptKey, identifier)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c.client.Incr(ctx, key)
	c.client.Expire(ctx, key, 15*time.Minute)
}

func (c *CacheService) ClearLoginAttempts(identifier string) {
	if c.client == nil {
		return
	}

	key := fmt.Sprintf(LoginAttemptKey, identifier)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c.client.Del(ctx, key)
}

// Cache Invalidation Methods

func (c *CacheService) InvalidateEventCache(eventID uuid.UUID) {
	if c.client == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Invalidate specific event
	eventKey := fmt.Sprintf(EventDetailKey, eventID)
	c.client.Del(ctx, eventKey)

	// Invalidate event lists (use pattern matching)
	pattern := fmt.Sprintf("%s:*", EventListKey)
	keys, err := c.client.Keys(ctx, pattern).Result()
	if err == nil {
		if len(keys) > 0 {
			c.client.Del(ctx, keys...)
		}
	}

	// Invalidate financial data
	financialKey := fmt.Sprintf(EventSalesKey, eventID)
	c.client.Del(ctx, financialKey, FinancialSummaryKey)
}

func (c *CacheService) InvalidateUserCache(userID uuid.UUID) {
	if c.client == nil {
		return
	}

	key := fmt.Sprintf(UserDetailKey, userID.String())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c.client.Del(ctx, key)
}

func (c *CacheService) InvalidateFinancialCache() {
	if c.client == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Clear all financial cache
	pattern := "financial:*"
	keys, err := c.client.Keys(ctx, pattern).Result()
	if err == nil {
		if len(keys) > 0 {
			c.client.Del(ctx, keys...)
		}
	}
}

// API Response Caching

func (c *CacheService) GetAPIResponse(requestHash string) (string, bool) {
	if c.client == nil {
		return "", false
	}

	key := fmt.Sprintf(APIResponseKey, requestHash)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return "", false
	}

	return val, true
}

func (c *CacheService) SetAPIResponse(requestHash, response string, expiry time.Duration) {
	if c.client == nil {
		return
	}

	key := fmt.Sprintf(APIResponseKey, requestHash)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c.client.Set(ctx, key, response, expiry)
}

// Cache Statistics

func (c *CacheService) GetCacheStats() map[string]interface{} {
	if c.client == nil {
		return map[string]interface{}{"status": "disabled"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	info := c.client.Info(ctx, "memory", "stats").Val()

	stats := map[string]interface{}{
		"status": "connected",
		"info":   info,
	}

	return stats
}

// Utility Methods

func (c *CacheService) IsAvailable() bool {
	return c.client != nil && redis.IsHealthy()
}

func (c *CacheService) FlushAll() error {
	if c.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return c.client.FlushAll(ctx).Err()
}

// Generate cache key based on request parameters
func GenerateRequestHash(parts ...string) string {
	return strings.Join(parts, ":")
}
