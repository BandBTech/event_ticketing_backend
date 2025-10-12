package config

import (
	"fmt"
)

// SMTPConfig defines the configuration for email delivery
type SMTPConfig struct {
	Host     string // SMTP server host
	Port     string // SMTP server port
	Username string // SMTP username
	Password string // SMTP password
	From     string // Email sender address
}

// Add SMTP config to main config
func (c *Config) AddSMTPConfig() {
	// Get SMTP values from environment variables
	host := getEnv("SMTP_HOST", "")
	port := getEnv("SMTP_PORT", "587")
	user := getEnv("SMTP_USER", "")
	password := getEnv("SMTP_PASSWORD", "")
	from := getEnv("SMTP_FROM", "noreply@eventticketingapp.com")

	// Log SMTP configuration for debugging
	fmt.Printf("Loading SMTP Config: Host=%s, Port=%s, User=%s, From=%s\n",
		host, port, user, from)

	// Default values for SMTP config
	c.SMTP = SMTPConfig{
		Host:     host,
		Port:     port,
		Username: user, // Maps from SMTP_USER env var
		Password: password,
		From:     from,
	}
}
