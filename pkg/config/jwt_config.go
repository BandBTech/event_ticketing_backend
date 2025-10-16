package config

import (
	"time"
)

// JWTConfig defines the configuration for JWT authentication
type JWTConfig struct {
	Secret          string        // Secret key for signing JWTs
	AccessTokenTTL  time.Duration // Time-to-live for access tokens
	RefreshTokenTTL time.Duration // Time-to-live for refresh tokens
	Issuer          string        // JWT issuer claim
	Audience        string        // JWT audience claim
}

// Add JWT config to Config struct
func init() {
	// JWT configuration will be added to the main config struct
}

// UpdateConfig adds JWT configuration to the main Config struct
func (c *Config) AddJWTConfig() {
	// Default values for JWT config
	c.JWT = JWTConfig{
		Secret:          getEnv("JWT_SECRET", "your-super-secret-key-change-in-production"),
		AccessTokenTTL:  time.Duration(getEnvAsInt("JWT_ACCESS_TOKEN_TTL", 24)) * time.Hour,    // 24 hours (1 day)
		RefreshTokenTTL: time.Duration(getEnvAsInt("JWT_REFRESH_TOKEN_TTL", 7*24)) * time.Hour, // 7 days
		Issuer:          getEnv("JWT_ISSUER", "event-ticketing-api"),
		Audience:        getEnv("JWT_AUDIENCE", "event-ticketing-clients"),
	}
}
