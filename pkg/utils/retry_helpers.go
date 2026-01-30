package utils

import (
	"fmt"
	"strings"
	"time"
)

// WithRetryFunc executes a function returning a value with retry logic for deadlock recovery
func WithRetryFunc[T any](fn func() (T, error)) (T, error) {
	maxRetries := 3
	backoff := 50 * time.Millisecond

	var result T
	var err error

	for i := 0; i < maxRetries; i++ {
		result, err = fn()
		if err == nil {
			return result, nil
		}

		// Check if error is a deadlock or lock timeout
		errStr := err.Error()
		if containsRetryableError(errStr) {
			if i < maxRetries-1 {
				// Exponential backoff with jitter
				sleep := backoff * time.Duration(1<<uint(i))
				time.Sleep(sleep)
				continue
			}
		}

		// Non-retryable error or max retries reached
		return result, err
	}

	return result, fmt.Errorf("max retries (%d) exceeded: %w", maxRetries, err)
}

// WithRetryFunc2 executes a function returning 2 values with retry logic
func WithRetryFunc2[T1, T2 any](fn func() (T1, T2, error)) (T1, T2, error) {
	maxRetries := 3
	backoff := 50 * time.Millisecond

	var result1 T1
	var result2 T2
	var err error

	for i := 0; i < maxRetries; i++ {
		result1, result2, err = fn()
		if err == nil {
			return result1, result2, nil
		}

		if containsRetryableError(err.Error()) && i < maxRetries-1 {
			time.Sleep(backoff * time.Duration(1<<uint(i)))
			continue
		}

		return result1, result2, err
	}

	return result1, result2, fmt.Errorf("max retries (%d) exceeded: %w", maxRetries, err)
}

// WithRetryFunc3 executes a function returning 3 values with retry logic
func WithRetryFunc3[T1, T2, T3 any](fn func() (T1, T2, T3, error)) (T1, T2, T3, error) {
	maxRetries := 3
	backoff := 50 * time.Millisecond

	var result1 T1
	var result2 T2
	var result3 T3
	var err error

	for i := 0; i < maxRetries; i++ {
		result1, result2, result3, err = fn()
		if err == nil {
			return result1, result2, result3, nil
		}

		if containsRetryableError(err.Error()) && i < maxRetries-1 {
			time.Sleep(backoff * time.Duration(1<<uint(i)))
			continue
		}

		return result1, result2, result3, err
	}

	return result1, result2, result3, fmt.Errorf("max retries (%d) exceeded: %w", maxRetries, err)
}

// containsRetryableError checks if error should trigger a retry
func containsRetryableError(errStr string) bool {
	retryableErrors := []string{
		"deadlock",
		"lock timeout",
		"could not obtain lock",
		"lock wait timeout",
		"serialization failure",
	}

	lowerErr := strings.ToLower(errStr)
	for _, retryErr := range retryableErrors {
		if strings.Contains(lowerErr, retryErr) {
			return true
		}
	}
	return false
}
