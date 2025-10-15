package config

import (
	"fmt"
)

// SMTPConfig defines the configuration for email delivery
type SMTPConfig struct {
	Host      string // SMTP server host
	Port      int    // SMTP server port
	Username  string // SMTP username
	Password  string // SMTP password
	FromEmail string // Email sender address
}

// Add SMTP config to main config
func (c *Config) AddSMTPConfig() {
	// Get SMTP values from environment variables
	host := getEnv("SMTP_HOST", "")
	user := getEnv("SMTP_USER", "")
	password := getEnv("SMTP_PASSWORD", "")
	from := getEnv("SMTP_FROM", "noreply@eventticketingapp.com")

	// Convert port to int
	port := getEnvAsInt("SMTP_PORT", 587)

	// Log SMTP configuration for debugging
	fmt.Printf("Loading SMTP Config: Host=%s, Port=%d, User=%s, From=%s\n",
		host, port, user, from)

	// Default values for SMTP config
	c.SMTP = SMTPConfig{
		Host:      host,
		Port:      port,
		Username:  user,
		Password:  password,
		FromEmail: from,
	}
}
