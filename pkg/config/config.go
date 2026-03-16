package config

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	App      AppConfig
	Database DatabaseConfig
	Redis    RedisConfig
	Server   ServerConfig
	JWT      JWTConfig
	SMTP     SMTPConfig
	S3       S3Config
	URLs     URLsConfig
	CORS     CORSConfig
	Payment  PaymentConfig
}

type AppConfig struct {
	Env       string
	Name      string
	Version   string
	Port      string
	Host      string
	SecretKey string
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
}

type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       string
}

type ServerConfig struct {
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

type S3Config struct {
	BucketName      string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	PublicReadACL   bool
}

type URLsConfig struct {
	UserBaseURL      string
	OrganizerBaseURL string
	AdminBaseURL     string
	FrontendBaseURL  string
}

type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods string
	AllowedHeaders string
}

type PaymentConfig struct {
	CashAllowedEmails []string
	SuccessURL        string
	FailedURL         string
	CancelURL         string

	// Payment Gateway Configurations
	Gateways PaymentGatewaysConfig
}

type PaymentGatewaysConfig struct {
	// Stripe
	StripeAPIKey        string
	StripeWebhookSecret string
	StripeTestMode      bool
}

func Load() (*Config, error) {
	// Load .env file
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "local"
	}

	envFile := fmt.Sprintf(".env.%s", env)
	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		envFile = ".env"
	}

	if err := godotenv.Load(envFile); err != nil {
		log.Printf("Warning: .env file not found, using environment variables")
	} else {
		log.Printf("DEBUG: Successfully loaded .env file: %s", envFile)
	}

	stripeWebhookSecret := normalizeSecretList(getFirstNonEmptyEnv(
		"STRIPE_WEBHOOK_SECRETS",
		"STRIPE_WEBHOOK_SECRET",
		"STRIPE_WEBHOOK_SIGNING_SECRET",
		"STRIPE_WEBHOOK_SECRET_KEY",
		"STRIPE_SIGNING_SECRET",
	))

	config := &Config{
		App: AppConfig{
			Env:       getEnv("APP_ENV", "local"),
			Name:      getEnv("APP_NAME", "Event Ticketing API"),
			Version:   getEnv("APP_VERSION", "1.0.0"),
			Port:      getEnv("PORT", "8080"),
			Host:      getEnv("HOST", "0.0.0.0"),
			SecretKey: getEnv("APP_SECRET_KEY", "your-super-secret-key-change-this-in-production"),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", "postgres"),
			DBName:   getEnv("DB_NAME", "event_ticketing"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnvAsInt("REDIS_PORT", 6379),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnv("REDIS_DB", "0"),
		},
		Server: ServerConfig{
			ReadTimeout:  parseDuration(getEnv("SERVER_READ_TIMEOUT", "30s")),
			WriteTimeout: parseDuration(getEnv("SERVER_WRITE_TIMEOUT", "30s")),
			IdleTimeout:  parseDuration(getEnv("SERVER_IDLE_TIMEOUT", "60s")),
		},
		S3: S3Config{
			BucketName:      getEnv("S3_BUCKET_NAME", ""),
			Region:          getEnv("S3_REGION", "us-east-1"),
			AccessKeyID:     getEnv("S3_ACCESS_KEY_ID", ""),
			SecretAccessKey: getEnv("S3_SECRET_ACCESS_KEY", ""),
			PublicReadACL:   getEnvAsBool("S3_PUBLIC_READ_ACL", true),
		},
		URLs: URLsConfig{
			UserBaseURL:      getEnv("USER_BASE_URL", "https://user.timroticket.com"),
			OrganizerBaseURL: getEnv("ORGANIZER_BASE_URL", "https://sandbox-organizer.timroticket.com"),
			AdminBaseURL:     getEnv("ADMIN_BASE_URL", "http://sandbox-admin.timroticket.com"),
			FrontendBaseURL:  getEnv("FRONTEND_BASE_URL", "https://user.timroticket.com"),
		},
		CORS: CORSConfig{
			AllowedOrigins: getEnvAsSlice("CORS_ALLOWED_ORIGINS", []string{
				"http://localhost:3000",
				"http://localhost:5173",
				"http://localhost:8082",
				"https://timroticket.com",
				"https://www.timroticket.com",
				"https://sandbox-admin.timroticket.com",
				"https://sandbox-organizer.timroticket.com",
				"https://user.timroticket.com",
				"https://api.timroticket.com",
				"https://secureadmin.timroticket.com",
			}),
			AllowedMethods: getEnv("CORS_ALLOWED_METHODS", "GET,POST,PUT,DELETE,OPTIONS,PATCH"),
			AllowedHeaders: getEnv("CORS_ALLOWED_HEADERS", "Content-Type,Content-Length,Accept-Encoding,X-CSRF-Token,Authorization,accept,origin,Cache-Control,X-Requested-With"),
		},
		Payment: PaymentConfig{
			CashAllowedEmails: getEnvAsSlice("CASH_ALLOWED_EMAILS", []string{}),
			SuccessURL:        getEnv("PAYMENT_SUCCESS_URL", getEnv("FRONTEND_BASE_URL", "https://user.timroticket.com")+"/payment/success"),
			FailedURL:         getEnv("PAYMENT_FAILED_URL", getEnv("FRONTEND_BASE_URL", "https://user.timroticket.com")+"/payment/failed"),
			CancelURL:         getEnv("PAYMENT_CANCEL_URL", getEnv("FRONTEND_BASE_URL", "https://user.timroticket.com")+"/payment/cancel"),
			Gateways: PaymentGatewaysConfig{
				// Stripe
				StripeAPIKey:        getEnv("STRIPE_API_KEY", ""),
				StripeWebhookSecret: stripeWebhookSecret,
				StripeTestMode:      getEnvAsBool("STRIPE_TEST_MODE", true),
			},
		},
	}

	// Debug logging for Stripe config
	log.Printf("DEBUG: STRIPE_API_KEY configured: %t (length: %d)", config.Payment.Gateways.StripeAPIKey != "", len(config.Payment.Gateways.StripeAPIKey))
	webhookSecrets := splitAndCleanCSV(config.Payment.Gateways.StripeWebhookSecret)
	log.Printf("DEBUG: STRIPE_WEBHOOK_SECRET configured: %t (count: %d)", len(webhookSecrets) > 0, len(webhookSecrets))

	// Add JWT and SMTP configurations
	config.AddJWTConfig()
	config.AddSMTPConfig()

	return config, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}

	value := 0
	_, err := fmt.Sscanf(valueStr, "%d", &value)
	if err != nil {
		log.Printf("Warning: Environment variable %s is not an integer, using default value %d", key, defaultValue)
		return defaultValue
	}
	return value
}

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}

	// Simple boolean parsing
	switch valueStr {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		log.Printf("Warning: Environment variable %s has invalid boolean value '%s', using default value %t", key, valueStr, defaultValue)
		return defaultValue
	}
}

func getEnvAsSlice(key string, defaultValue []string) []string {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}

	// Split by comma and trim spaces
	var result []string
	for _, item := range strings.Split(valueStr, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}

	if len(result) == 0 {
		return defaultValue
	}

	return result
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 30 * time.Second
	}
	return d
}

func getFirstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func splitAndCleanCSV(value string) []string {
	if value == "" {
		return []string{}
	}

	parts := strings.Split(value, ",")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.Trim(strings.TrimSpace(part), "\"'")
		if trimmed != "" {
			clean = append(clean, trimmed)
		}
	}

	return clean
}

func normalizeSecretList(value string) string {
	return strings.Join(splitAndCleanCSV(value), ",")
}

func (c *Config) GetDSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.Database.Host,
		c.Database.Port,
		c.Database.User,
		c.Database.Password,
		c.Database.DBName,
		c.Database.SSLMode,
	)
}
