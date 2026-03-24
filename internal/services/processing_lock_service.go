package services

import (
	"context"
	"fmt"
	"time"

	"event-ticketing-backend/internal/models"

	"gorm.io/gorm"
)

// ProcessingLockService handles distributed locking to prevent double processing
type ProcessingLockService struct {
	db *gorm.DB
}

// NewProcessingLockService creates a new processing lock service
func NewProcessingLockService(db *gorm.DB) *ProcessingLockService {
	return &ProcessingLockService{db: db}
}

// AcquireLock attempts to acquire a processing lock
func (s *ProcessingLockService) AcquireLock(ctx context.Context, lockKey, lockType, ownerID string, ttl time.Duration) (bool, error) {
	expiresAt := time.Now().Add(ttl)

	lock := &models.ProcessingLock{
		LockKey:   lockKey,
		LockType:  lockType,
		OwnerID:   ownerID,
		ExpiresAt: expiresAt,
	}

	// Try to insert the lock (will fail if key already exists)
	err := s.db.Create(lock).Error
	if err != nil {
		// Check if it's a duplicate key error (lock already exists)
		if s.isDuplicateKeyError(err) {
			return false, nil // Lock already held
		}
		return false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	return true, nil // Lock acquired
}

// ReleaseLock releases a processing lock
func (s *ProcessingLockService) ReleaseLock(ctx context.Context, lockKey, ownerID string) error {
	result := s.db.Where("lock_key = ? AND owner_id = ?", lockKey, ownerID).
		Delete(&models.ProcessingLock{})

	if result.Error != nil {
		return fmt.Errorf("failed to release lock: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("lock not found or not owned by this instance")
	}

	return nil
}

// RenewLock extends the expiry of an existing lock
func (s *ProcessingLockService) RenewLock(ctx context.Context, lockKey, ownerID string, ttl time.Duration) error {
	expiresAt := time.Now().Add(ttl)

	result := s.db.Model(&models.ProcessingLock{}).
		Where("lock_key = ? AND owner_id = ?", lockKey, ownerID).
		Update("expires_at", expiresAt)

	if result.Error != nil {
		return fmt.Errorf("failed to renew lock: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("lock not found or not owned by this instance")
	}

	return nil
}

// IsLocked checks if a lock key is currently held
func (s *ProcessingLockService) IsLocked(ctx context.Context, lockKey string) (bool, error) {
	var count int64
	err := s.db.Model(&models.ProcessingLock{}).
		Where("lock_key = ? AND expires_at > ?", lockKey, time.Now()).
		Count(&count).Error

	if err != nil {
		return false, fmt.Errorf("failed to check lock status: %w", err)
	}

	return count > 0, nil
}

// CleanupExpiredLocks removes expired locks (should be called periodically)
func (s *ProcessingLockService) CleanupExpiredLocks(ctx context.Context) error {
	result := s.db.Where("expires_at <= ?", time.Now()).
		Delete(&models.ProcessingLock{})

	if result.Error != nil {
		return fmt.Errorf("failed to cleanup expired locks: %w", result.Error)
	}

	return nil
}

// isDuplicateKeyError checks if the error is a duplicate key constraint violation
func (s *ProcessingLockService) isDuplicateKeyError(err error) bool {
	// This is database-specific, but for PostgreSQL it's "duplicate key value"
	// For other databases, you might need different checks
	return err != nil && (err.Error() == "duplicate key value violates unique constraint" ||
		err.Error() == "UNIQUE constraint failed" ||
		err.Error() == "Duplicate entry")
}

// WithLock executes a function while holding a lock
func (s *ProcessingLockService) WithLock(ctx context.Context, lockKey, lockType, ownerID string, ttl time.Duration, fn func() error) error {
	// Try to acquire lock
	acquired, err := s.AcquireLock(ctx, lockKey, lockType, ownerID, ttl)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	if !acquired {
		return fmt.Errorf("lock already held for key: %s", lockKey)
	}

	// Ensure lock is released even if function panics
	defer func() {
		if err := s.ReleaseLock(ctx, lockKey, ownerID); err != nil {
			// Log but don't fail - the lock will expire anyway
			fmt.Printf("Warning: failed to release lock %s: %v\n", lockKey, err)
		}
	}()

	// Execute the function
	return fn()
}
