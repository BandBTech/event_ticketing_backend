package redis

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"event-ticketing-backend/pkg/config"

	"github.com/redis/go-redis/v9"
)

var Client *redis.Client

// Connect establishes a connection to Redis using the provided configuration
func Connect(cfg *config.Config) error {
	db, err := strconv.Atoi(cfg.Redis.DB)
	if err != nil {
		return fmt.Errorf("invalid Redis DB number: %w", err)
	}

	Client = redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password:     cfg.Redis.Password,
		DB:           db,
		MaxRetries:   3,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		PoolTimeout:  10 * time.Second,
	})

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := Client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// Test write permissions for asynq
	testKey := "test:write:permission"
	if err := Client.Set(ctx, testKey, "test", time.Second).Err(); err != nil {
		log.Printf("WARNING: Redis write test failed: %v", err)
		// Check if Redis is in readonly mode
		info := Client.Info(ctx, "replication")
		if infoStr, infoErr := info.Result(); infoErr == nil {
			log.Printf("Redis replication info: %s", infoStr)
		}
		return fmt.Errorf("Redis is in read-only mode or has write permission issues: %w", err)
	}

	// Clean up test key
	Client.Del(ctx, testKey)

	log.Println("Redis connected successfully with write permissions")
	return nil
}

// Close closes the Redis connection
func Close() error {
	if Client == nil {
		return nil
	}
	return Client.Close()
}

// IsHealthy checks if Redis is healthy by sending a PING command
func IsHealthy() bool {
	if Client == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := Client.Ping(ctx).Err()
	return err == nil
}

// GetClient returns the Redis client instance
func GetClient() *redis.Client {
	return Client
}
