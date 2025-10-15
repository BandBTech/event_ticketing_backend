package models

import (
	"time"

	"github.com/google/uuid"
)

// EmailJobType represents the type of email to be sent
type EmailJobType string

const (
	// Authentication & Account Management
	EmailTypeRegistration      EmailJobType = "registration"
	EmailTypeOTP               EmailJobType = "otp"
	EmailTypeVerification      EmailJobType = "verification"
	EmailTypePasswordReset     EmailJobType = "password_reset"
	EmailTypeWelcome           EmailJobType = "welcome"
	EmailTypeAccountActivation EmailJobType = "account_activation"

	// Organization Management
	EmailTypeOrganizationInvitation EmailJobType = "organization_invitation"
	EmailTypeOrganizationWelcome    EmailJobType = "organization_welcome"
	EmailTypeRoleChange             EmailJobType = "role_change"
	EmailTypeAccessRevoked          EmailJobType = "access_revoked"

	// Event Management
	EmailTypeEventNotification EmailJobType = "event_notification"
	EmailTypeEventReminder     EmailJobType = "event_reminder"
	EmailTypeEventCancellation EmailJobType = "event_cancellation"
	EmailTypeEventUpdate       EmailJobType = "event_update"

	// Ticketing
	EmailTypeTicketConfirmation EmailJobType = "ticket_confirmation"
	EmailTypeTicketRefund       EmailJobType = "ticket_refund"
	EmailTypeTicketTransfer     EmailJobType = "ticket_transfer"
	EmailTypeTicketReminder     EmailJobType = "ticket_reminder"

	// Payment & Billing
	EmailTypePaymentConfirmation EmailJobType = "payment_confirmation"
	EmailTypePaymentFailed       EmailJobType = "payment_failed"
	EmailTypeRefundProcessed     EmailJobType = "refund_processed"
	EmailTypeInvoice             EmailJobType = "invoice"
	EmailTypePaymentReminder     EmailJobType = "payment_reminder"

	// General
	EmailTypeNotification EmailJobType = "notification"
	EmailTypeReminder     EmailJobType = "reminder"
	EmailTypeMarketing    EmailJobType = "marketing"
	EmailTypeNewsletter   EmailJobType = "newsletter"
)

// EmailJob represents an email task to be processed by the worker
type EmailJob struct {
	ID              string                 `json:"id"`
	Type            EmailJobType           `json:"type"`
	To              string                 `json:"to"`
	CC              []string               `json:"cc,omitempty"`
	BCC             []string               `json:"bcc,omitempty"`
	Subject         string                 `json:"subject"`
	TemplateFile    string                 `json:"template_file"`
	TemplateData    map[string]interface{} `json:"template_data"`
	Priority        int                    `json:"priority"` // 0 = highest priority, 1 = high, 2 = normal, 3 = low
	CreatedAt       time.Time              `json:"created_at"`
	ProcessAfter    time.Time              `json:"process_after,omitempty"` // Optional delayed processing
	RetryCount      int                    `json:"retry_count"`
	MaxRetries      int                    `json:"max_retries"`
	LastError       string                 `json:"last_error,omitempty"`
	LastAttemptedAt time.Time              `json:"last_attempted_at,omitempty"`

	// Additional metadata
	UserID         string                 `json:"user_id,omitempty"`         // Associated user ID
	OrganizationID string                 `json:"organization_id,omitempty"` // Associated organization ID
	EventID        string                 `json:"event_id,omitempty"`        // Associated event ID
	TicketID       string                 `json:"ticket_id,omitempty"`       // Associated ticket ID
	PaymentID      string                 `json:"payment_id,omitempty"`      // Associated payment ID
	Tags           []string               `json:"tags,omitempty"`            // Tags for categorization
	Metadata       map[string]interface{} `json:"metadata,omitempty"`        // Additional metadata
}

// Priority levels
const (
	PriorityUrgent = 0 // Urgent (OTP, password reset, etc.)
	PriorityHigh   = 1 // High (verification, welcome emails)
	PriorityNormal = 2 // Normal (notifications, reminders)
	PriorityLow    = 3 // Low (marketing, newsletters)
)

// Default retry settings
const (
	DefaultMaxRetries = 3
	DefaultRetryDelay = 5 * time.Minute
)

// EmailJobResult represents the result of processing an email job
type EmailJobResult struct {
	JobID       string    `json:"job_id"`
	Successful  bool      `json:"successful"`
	Error       string    `json:"error,omitempty"`
	SentAt      time.Time `json:"sent_at,omitempty"`
	ProcessedBy string    `json:"processed_by,omitempty"` // Worker ID
	Attempts    int       `json:"attempts"`
}

// GenerateID generates a unique ID for the email job
func (ej *EmailJob) GenerateID() string {
	if ej.ID == "" {
		ej.ID = uuid.New().String()
	}
	return ej.ID
}

// SetDefaults sets default values for the email job
func (ej *EmailJob) SetDefaults() {
	if ej.CreatedAt.IsZero() {
		ej.CreatedAt = time.Now()
	}
	if ej.MaxRetries == 0 {
		ej.MaxRetries = DefaultMaxRetries
	}
	if ej.ProcessAfter.IsZero() {
		ej.ProcessAfter = time.Now()
	}
	if ej.ID == "" {
		ej.GenerateID()
	}
}

// ShouldRetry determines if the job should be retried
func (ej *EmailJob) ShouldRetry() bool {
	return ej.RetryCount < ej.MaxRetries
}

// IncrementRetry increments the retry count and sets the last error
func (ej *EmailJob) IncrementRetry(err error) {
	ej.RetryCount++
	ej.LastAttemptedAt = time.Now()
	if err != nil {
		ej.LastError = err.Error()
	}
}

// GetPriorityQueue returns the queue name based on priority
func (ej *EmailJob) GetPriorityQueue() string {
	switch ej.Priority {
	case PriorityUrgent:
		return "queue:email:urgent"
	case PriorityHigh:
		return "queue:email:high"
	case PriorityNormal:
		return "queue:email:normal"
	case PriorityLow:
		return "queue:email:low"
	default:
		return "queue:email:normal"
	}
}

// IsExpired checks if the job has expired (too old to process)
func (ej *EmailJob) IsExpired(maxAge time.Duration) bool {
	return time.Since(ej.CreatedAt) > maxAge
}
