# Email Queue System Usage

This document explains how to use the new Asynq-based email queue system.

## Overview

The email queue system consists of:

- **EmailService**: Handles actual email sending via SMTP
- **EmailQueueService**: Manages job queuing using Asynq
- **EmailWorker**: Processes queued email jobs
- **Email Templates**: HTML templates for different email types

## Configuration

Add these environment variables to your `.env` file:

```bash
# Redis Configuration (required for Asynq)
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0

# SMTP Configuration
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your-email@gmail.com
SMTP_PASSWORD=your-app-password
SMTP_FROM=noreply@yourdomain.com
```

## Usage Examples

### 1. Registration OTP Email

```go
// In your auth service
func (s *AuthService) sendRegistrationOTP(email, otp string) error {
    return s.emailQueueService.QueueRegistrationOTP(email, otp)
}
```

### 2. Password Reset OTP Email

```go
// In your auth service
func (s *AuthService) sendPasswordResetOTP(email, otp string) error {
    return s.emailQueueService.QueuePasswordResetOTP(email, otp)
}
```

### 3. Welcome Email

```go
// In your auth service
func (s *AuthService) sendWelcomeEmail(email, firstName string) error {
    return s.emailQueueService.QueueWelcomeEmail(email, firstName)
}
```

### 4. Custom Email Job

```go
// Create a custom email job
emailJob := &models.EmailJob{
    Type:         models.EmailTypeNotification,
    To:           "user@example.com",
    Subject:      "Custom Notification",
    TemplateFile: "notification.html",
    TemplateData: map[string]interface{}{
        "UserName": "John Doe",
        "Message":  "Your custom message here",
    },
    Priority:   models.PriorityNormal,
    MaxRetries: 3,
}
emailJob.SetDefaults()

// Queue the job
err := emailQueueService.queueEmailJob(emailJob)
```

## Email Job Priorities

The system supports 4 priority levels:

- **PriorityUrgent (0)**: OTP emails, password resets (processed first)
- **PriorityHigh (1)**: Welcome emails, verification emails
- **PriorityNormal (2)**: General notifications, reminders
- **PriorityLow (3)**: Marketing emails, newsletters

## Queue Management

### Starting the Email Worker

The email worker is automatically started in `main.go`:

```go
// Initialize background workers
emailService := services.NewEmailService(cfg)
emailWorker := workers.NewEmailWorker(cfg, emailService)
workerManager := workers.NewWorkerManager(emailWorker)

// Start background workers
workerManager.StartAll()
```

### Monitoring Queues

You can monitor the queue status using Asynq tools or Redis CLI:

```bash
# Check queue length
redis-cli llen queue:email:urgent
redis-cli llen queue:email:high
redis-cli llen queue:email:normal
redis-cli llen queue:email:low
```

## Error Handling

- Failed emails are automatically retried up to 3 times (configurable)
- Retry delays increase exponentially: 1min, 2min, 3min
- Failed jobs are logged with error details
- You can implement custom error handling in the worker

## Available Email Templates

Current templates in `internal/templates/email/`:

- `otp_email.html` - OTP verification emails
- `reset_password_email.html` - Password reset emails
- `welcome_email.html` - Welcome emails for new users
- `verification_email.html` - Email verification
- `notification.html` - General notifications

## Adding New Email Types

1. Add the email type to `models/email_job.go`:

```go
const (
    EmailTypeNewFeature EmailJobType = "new_feature"
)
```

2. Create the HTML template in `internal/templates/email/`

3. Add a queue method to `EmailQueueService`:

```go
func (s *EmailQueueService) QueueNewFeatureEmail(to, feature string) error {
    emailJob := &models.EmailJob{
        Type:         models.EmailTypeNewFeature,
        To:           to,
        Subject:      "New Feature Available!",
        TemplateFile: "new_feature.html",
        TemplateData: map[string]interface{}{
            "FeatureName": feature,
        },
        Priority:   models.PriorityNormal,
        MaxRetries: 3,
    }
    emailJob.SetDefaults()
    return s.queueEmailJob(emailJob)
}
```

## Benefits

- **Asynchronous**: Email sending doesn't block API requests
- **Reliable**: Failed emails are automatically retried
- **Scalable**: Multiple workers can process emails concurrently
- **Prioritized**: Important emails (OTPs) are processed first
- **Monitored**: Built-in logging and error tracking
- **Flexible**: Easy to add new email types and templates
