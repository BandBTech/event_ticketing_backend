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
		Addr:     fmt.Sprintf("%s:%s", cfg.Redis.Host, cfg.Redis.Port),
		Password: cfg.Redis.Password,
		DB:       db,
	})

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := Client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	log.Println("Redis connected successfully")
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
