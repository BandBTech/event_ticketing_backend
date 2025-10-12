package models

import "time"

// EmailJobType represents the type of email to be sent
type EmailJobType string

const (
	EmailTypeOTP                EmailJobType = "otp"
	EmailTypeVerification       EmailJobType = "verification"
	EmailTypePasswordReset      EmailJobType = "password_reset"
	EmailTypeWelcome            EmailJobType = "welcome"
	EmailTypeNotification       EmailJobType = "notification"
	EmailTypeInvoice            EmailJobType = "invoice"
	EmailTypeReminder           EmailJobType = "reminder"
	EmailTypeTicketConfirmation EmailJobType = "ticket_confirmation"
	EmailTypeInvitation         EmailJobType = "invitation"
)

// EmailJob represents an email task to be processed by the worker
type EmailJob struct {
	ID              string                 `json:"id"`
	Type            EmailJobType           `json:"type"`
	To              string                 `json:"to"`
	Subject         string                 `json:"subject"`
	TemplateFile    string                 `json:"template_file"`
	TemplateData    map[string]interface{} `json:"template_data"`
	Priority        int                    `json:"priority"` // 0 = highest priority
	CreatedAt       time.Time              `json:"created_at"`
	ProcessAfter    time.Time              `json:"process_after,omitempty"` // Optional delayed processing
	RetryCount      int                    `json:"retry_count"`
	MaxRetries      int                    `json:"max_retries"`
	LastError       string                 `json:"last_error,omitempty"`
	LastAttemptedAt time.Time              `json:"last_attempted_at,omitempty"`
}

// EmailJobResult represents the result of processing an email job
type EmailJobResult struct {
	JobID      string    `json:"job_id"`
	Successful bool      `json:"successful"`
	Error      string    `json:"error,omitempty"`
	SentAt     time.Time `json:"sent_at,omitempty"`
}
