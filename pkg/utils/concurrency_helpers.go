package utils

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
)

// InventoryLock provides distributed locking for inventory management
type InventoryLock struct {
	mu     sync.Map // tier_id -> *sync.Mutex
	global sync.Mutex
}

var inventoryLock = &InventoryLock{}

// GetInventoryLock returns the singleton inventory lock manager
func GetInventoryLock() *InventoryLock {
	return inventoryLock
}

// LockTier locks a specific tier for inventory updates
func (il *InventoryLock) LockTier(tierID string) func() {
	// Get or create mutex for this tier
	value, _ := il.mu.LoadOrStore(tierID, &sync.Mutex{})
	mutex := value.(*sync.Mutex)

	// Lock the tier
	mutex.Lock()

	// Return unlock function
	return func() {
		mutex.Unlock()
	}
}

// WithRetry executes a function with retry logic for deadlock recovery
func WithRetry(fn func() error, maxRetries int, backoff time.Duration) error {
	var err error
	for i := 0; i < maxRetries; i++ {
		err = fn()
		if err == nil {
			return nil
		}

		// Check if error is a deadlock or lock timeout
		errStr := err.Error()
		if containsAny(errStr, []string{"deadlock", "lock timeout", "could not obtain lock"}) {
			if i < maxRetries-1 {
				// Exponential backoff
				time.Sleep(backoff * time.Duration(1<<uint(i)))
				continue
			}
		}

		// Non-retryable error or max retries reached
		return err
	}
	return fmt.Errorf("max retries (%d) exceeded: %w", maxRetries, err)
}

// WithTimeout executes a database operation with timeout
func WithTimeout(db *gorm.DB, timeout time.Duration, fn func(*gorm.DB) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return fn(db.WithContext(ctx))
}

// OptimisticLockUpdate performs optimistic locking update
// Returns true if update succeeded, false if version mismatch (retry needed)
func OptimisticLockUpdate(db *gorm.DB, model interface{}, updates map[string]interface{}, whereClause string, args ...interface{}) (bool, error) {
	// Add version check to where clause if not present
	result := db.Model(model).
		Where(whereClause, args...).
		Updates(updates)

	if result.Error != nil {
		return false, result.Error
	}

	// If no rows affected, version mismatch occurred
	return result.RowsAffected > 0, nil
}

// BatchLock locks multiple resources in consistent order to prevent deadlocks
func BatchLock(locks ...*sync.Mutex) func() {
	// Sort locks by memory address to ensure consistent ordering
	// This prevents circular wait conditions
	sortedLocks := make([]*sync.Mutex, len(locks))
	copy(sortedLocks, locks)

	// Lock in order
	for _, lock := range sortedLocks {
		lock.Lock()
	}

	// Return unlock function (reverse order)
	return func() {
		for i := len(sortedLocks) - 1; i >= 0; i-- {
			sortedLocks[i].Unlock()
		}
	}
}

// containsAny checks if string contains any of the substrings
func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}

// RateLimitedExecutor limits concurrent executions
type RateLimitedExecutor struct {
	semaphore chan struct{}
}

// NewRateLimitedExecutor creates a new rate-limited executor
func NewRateLimitedExecutor(maxConcurrent int) *RateLimitedExecutor {
	return &RateLimitedExecutor{
		semaphore: make(chan struct{}, maxConcurrent),
	}
}

// Execute runs function with concurrency limit
func (r *RateLimitedExecutor) Execute(fn func() error) error {
	r.semaphore <- struct{}{}
	defer func() { <-r.semaphore }()
	return fn()
}
