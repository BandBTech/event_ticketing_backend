package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"event-ticketing-backend/internal/models"
	redisClient "event-ticketing-backend/internal/redis"
)

const (
	// Queue names
	EmailHighPriorityQueue   = "queue:email:high"
	EmailNormalPriorityQueue = "queue:email:normal"
	EmailLowPriorityQueue    = "queue:email:low"

	// Processing sets
	EmailProcessingSet = "processing:email"

	// Result sets
	EmailResultSet = "results:email"

	// TTLs
	ResultTTL     = 24 * time.Hour
	ProcessingTTL = 5 * time.Minute
)

// EmailQueueService manages queuing and processing of email jobs
type EmailQueueService struct {
	redisClient  *redis.Client
	emailService *EmailService
}

// NewEmailQueueService creates a new email queue service
func NewEmailQueueService(emailService *EmailService) *EmailQueueService {
	return &EmailQueueService{
		redisClient:  redisClient.Client,
		emailService: emailService,
	}
}

// QueueEmail adds an email job to the appropriate queue based on priority
func (s *EmailQueueService) QueueEmail(job *models.EmailJob) error {
	ctx := context.Background()

	// Generate a unique ID if not provided
	if job.ID == "" {
		job.ID = uuid.New().String()
	}

	// Set created time
	job.CreatedAt = time.Now()

	// Convert job to JSON
	jobBytes, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to serialize email job: %w", err)
	}

	// Select queue based on priority
	var queueName string
	switch {
	case job.Priority <= 0:
		queueName = EmailHighPriorityQueue
	case job.Priority <= 5:
		queueName = EmailNormalPriorityQueue
	default:
		queueName = EmailLowPriorityQueue
	}

	// Push to Redis list (queue)
	err = s.redisClient.LPush(ctx, queueName, jobBytes).Err()
	if err != nil {
		return fmt.Errorf("failed to add job to queue: %w", err)
	}

	return nil
}

// QueueOTPEmail is a helper to quickly queue an OTP email
func (s *EmailQueueService) QueueOTPEmail(to, otp, otpType string) error {
	// Determine subject and template data based on OTP type
	var subject, title, message string

	switch otpType {
	case "registration":
		subject = "Verify Your Email Address"
		title = "Email Verification"
		message = "Thank you for registering. Please use the following code to verify your email address."
	case "password_reset":
		subject = "Reset Your Password"
		title = "Password Reset Request"
		message = "We received a request to reset your password. Please use the following code to continue with your password reset."
	case "2fa":
		subject = "Two-Factor Authentication Code"
		title = "Login Authentication Code"
		message = "To complete your login, please enter the following verification code."
	default:
		subject = "Verification Code"
		title = "Your Verification Code"
		message = "Please use the following code to verify your identity."
	}

	// Create email job
	job := &models.EmailJob{
		ID:           uuid.New().String(),
		Type:         models.EmailTypeOTP,
		To:           to,
		Subject:      subject,
		TemplateFile: "otp_email.html",
		TemplateData: map[string]interface{}{
			"Title":       title,
			"Message":     message,
			"OTP":         otp,
			"CurrentYear": time.Now().Year(),
		},
		Priority:   0, // High priority for OTPs
		MaxRetries: 3,
		RetryCount: 0,
	}

	return s.QueueEmail(job)
}

// QueueWelcomeEmail queues a welcome email
func (s *EmailQueueService) QueueWelcomeEmail(user *models.User, password, orgName string) error {
	job := &models.EmailJob{
		ID:           uuid.New().String(),
		Type:         models.EmailTypeWelcome,
		To:           user.Email,
		Subject:      "Welcome to " + orgName + " - Your Account Information",
		TemplateFile: "welcome_email.html",
		TemplateData: map[string]interface{}{
			"Name":        user.FirstName + " " + user.LastName,
			"OrgName":     orgName,
			"Email":       user.Email,
			"Password":    password,
			"LoginURL":    "https://yourdomain.com/login",
			"CurrentYear": time.Now().Year(),
		},
		Priority:   3, // Normal priority
		MaxRetries: 3,
		RetryCount: 0,
	}

	return s.QueueEmail(job)
}

// ProcessEmailQueue processes a single job from the email queue
// This should be called repeatedly by a worker routine
func (s *EmailQueueService) ProcessEmailQueue(ctx context.Context) error {
	// Try to get a job from the high priority queue first, then normal, then low
	var jobBytes []byte
	var queueName string

	// Use BRPOP to block until a job is available from any queue, with priority
	result, err := s.redisClient.BRPop(ctx, 5*time.Second, EmailHighPriorityQueue, EmailNormalPriorityQueue, EmailLowPriorityQueue).Result()
	if err != nil {
		if err == redis.Nil {
			// No job available, that's fine
			return nil
		}
		return fmt.Errorf("failed to pop job from queue: %w", err)
	}

	// Extract queue name and job data
	queueName = result[0]
	jobBytes = []byte(result[1])

	// Parse the job
	var job models.EmailJob
	if err := json.Unmarshal(jobBytes, &job); err != nil {
		return fmt.Errorf("failed to deserialize email job: %w", err)
	}

	// Check if this job should be delayed
	if !job.ProcessAfter.IsZero() && job.ProcessAfter.After(time.Now()) {
		// Put it back in the queue for later processing
		jobBytes, _ := json.Marshal(job)
		err = s.redisClient.RPush(ctx, queueName, jobBytes).Err()
		if err != nil {
			return fmt.Errorf("failed to requeue delayed job: %w", err)
		}
		return nil
	}

	// Mark as being processed
	jobBytes, _ = json.Marshal(job)
	err = s.redisClient.Set(ctx,
		fmt.Sprintf("%s:%s", EmailProcessingSet, job.ID),
		jobBytes,
		ProcessingTTL).Err()
	if err != nil {
		return fmt.Errorf("failed to mark job as processing: %w", err)
	}

	// Process the job
	jobResult := s.processEmailJob(ctx, &job)

	// Save the result
	resultBytes, _ := json.Marshal(jobResult)
	err = s.redisClient.Set(ctx,
		fmt.Sprintf("%s:%s", EmailResultSet, job.ID),
		resultBytes,
		ResultTTL).Err()
	if err != nil {
		return fmt.Errorf("failed to save job result: %w", err)
	}

	// Clean up processing marker
	s.redisClient.Del(ctx, fmt.Sprintf("%s:%s", EmailProcessingSet, job.ID))

	// If failed and retries available, requeue with backoff
	if !jobResult.Successful && job.RetryCount < job.MaxRetries {
		job.RetryCount++
		job.LastAttemptedAt = time.Now()
		job.LastError = jobResult.Error

		// Exponential backoff
		backoff := time.Duration(1<<uint(job.RetryCount)) * time.Second
		job.ProcessAfter = time.Now().Add(backoff)

		// Requeue
		return s.QueueEmail(&job)
	}

	return nil
}

// processEmailJob sends an actual email
func (s *EmailQueueService) processEmailJob(ctx context.Context, job *models.EmailJob) *models.EmailJobResult {
	result := &models.EmailJobResult{
		JobID:      job.ID,
		Successful: false,
	}

	// Ensure we have a template file
	if job.TemplateFile == "" {
		result.Error = "template file not specified"
		return result
	}

	// Parse template and send email regardless of type
	templateData := job.TemplateData

	// Ensure CurrentYear is in the template data
	if _, exists := templateData["CurrentYear"]; !exists {
		templateData["CurrentYear"] = time.Now().Year()
	}

	body, err := s.emailService.parseTemplate(job.TemplateFile, templateData)
	if err != nil {
		result.Error = fmt.Sprintf("template error: %v", err)
		return result
	}

	err = s.emailService.sendEmail(job.To, job.Subject, body)

	// Handle result
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Successful = true
	result.SentAt = time.Now()
	return result
}

// QueueTicketConfirmationEmail queues an email with ticket confirmation details
func (s *EmailQueueService) QueueTicketConfirmationEmail(to, name, eventName, ticketID, eventDate, eventTime, eventVenue, ticketType, barcodeImage, downloadURL string) error {
	job := &models.EmailJob{
		ID:           uuid.New().String(),
		Type:         models.EmailTypeTicketConfirmation,
		To:           to,
		Subject:      "Your Ticket Confirmation for " + eventName,
		TemplateFile: "ticket_confirmation.html",
		TemplateData: map[string]interface{}{
			"Name":         name,
			"EventName":    eventName,
			"TicketID":     ticketID,
			"EventDate":    eventDate,
			"EventTime":    eventTime,
			"EventVenue":   eventVenue,
			"TicketType":   ticketType,
			"BarcodeImage": barcodeImage,
			"DownloadURL":  downloadURL,
			"CurrentYear":  time.Now().Year(),
		},
		Priority:   1, // High priority
		MaxRetries: 3,
		RetryCount: 0,
	}

	return s.QueueEmail(job)
}

// QueueEventNotificationEmail queues an email with event notification details
func (s *EmailQueueService) QueueEventNotificationEmail(to, name, notificationType, message, eventName, description, date, eventTime, location, organizer, eventURL, unsubscribeURL string) error {
	job := &models.EmailJob{
		ID:           uuid.New().String(),
		Type:         models.EmailTypeNotification,
		To:           to,
		Subject:      notificationType + ": " + eventName,
		TemplateFile: "event_notification.html",
		TemplateData: map[string]interface{}{
			"Name":                name,
			"NotificationType":    notificationType,
			"NotificationMessage": message,
			"EventName":           eventName,
			"EventDescription":    description,
			"EventDate":           date,
			"EventTime":           eventTime,
			"EventLocation":       location,
			"EventOrganizer":      organizer,
			"EventURL":            eventURL,
			"UnsubscribeURL":      unsubscribeURL,
			"CurrentYear":         time.Now().Year(),
		},
		Priority:   2, // Normal priority
		MaxRetries: 3,
		RetryCount: 0,
	}

	return s.QueueEmail(job)
}

// QueueOrganizationInvitationEmail queues an email inviting a user to an organization
func (s *EmailQueueService) QueueOrganizationInvitationEmail(to, name, orgName, orgDesc, inviterName, roleName, rolePerms, acceptURL, declineURL, expirationDate string) error {
	job := &models.EmailJob{
		ID:           uuid.New().String(),
		Type:         models.EmailTypeInvitation,
		To:           to,
		Subject:      "Invitation to Join " + orgName,
		TemplateFile: "organization_invitation.html",
		TemplateData: map[string]interface{}{
			"Name":                    name,
			"OrganizationName":        orgName,
			"OrganizationDescription": orgDesc,
			"InviterName":             inviterName,
			"RoleName":                roleName,
			"RoleSpecificPerms":       rolePerms,
			"AcceptURL":               acceptURL,
			"DeclineURL":              declineURL,
			"ExpirationDate":          expirationDate,
			"CurrentYear":             time.Now().Year(),
		},
		Priority:   2, // Normal priority
		MaxRetries: 3,
		RetryCount: 0,
	}

	return s.QueueEmail(job)
}
