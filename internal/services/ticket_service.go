package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/types"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// getOrganizerDisplayName returns the business name if available, otherwise falls back to first name + last name
func getOrganizerDisplayName(organizer *models.User) string {
	if organizer == nil {
		return "Unknown Organizer"
	}

	if organizer.OrganizerOnboarding != nil && organizer.OrganizerOnboarding.BusinessName != "" {
		return organizer.OrganizerOnboarding.BusinessName
	}

	return organizer.FirstName + " " + organizer.LastName
}

type TicketService struct {
	db                          *gorm.DB
	financialService            *FinancialService
	emailQueueService           *EmailQueueService
	emailOutboxService          *EmailOutboxService
	authService                 *AuthService
	jwtConfig                   *config.JWTConfig
	cfg                         *config.Config
	secureQRService             *SecureQRService
	emailService                *EmailService
	reservationService          *ReservationService
	unifiedPurchaseOrchestrator *UnifiedPurchaseOrchestrator
	transactionService          *TransactionService
}

func NewTicketService(db *gorm.DB, financialService *FinancialService, jwtConfig *config.JWTConfig, cfg *config.Config) *TicketService {
	return &TicketService{
		db:               db,
		financialService: financialService,
		jwtConfig:        jwtConfig,
		cfg:              cfg,
		emailService:     NewEmailService(cfg), // Initialize email service
	}
}

// GetEmailService returns the email service
func (s *TicketService) GetEmailService() *EmailService {
	return s.emailService
}
func (s *TicketService) SetSecureQRService(secureQR *SecureQRService) {
	s.secureQRService = secureQR
}

// SetEmailQueueService sets the email queue service for sending notifications
func (s *TicketService) SetEmailQueueService(emailQueueService *EmailQueueService) {
	s.emailQueueService = emailQueueService
}

// SetEmailOutboxService sets the email outbox service for sending notifications
func (s *TicketService) SetEmailOutboxService(emailOutboxService *EmailOutboxService) {
	s.emailOutboxService = emailOutboxService
}

// SetAuthService sets the auth service for user validation
func (s *TicketService) SetAuthService(authService *AuthService) {
	s.authService = authService
}

// SetReservationService sets the reservation service for managing ticket holds
func (s *TicketService) SetReservationService(reservationService *ReservationService) {
	s.reservationService = reservationService
}

// SetUnifiedPurchaseOrchestrator sets the unified purchase orchestrator
func (s *TicketService) SetUnifiedPurchaseOrchestrator(unifiedPurchaseOrchestrator *UnifiedPurchaseOrchestrator) {
	s.unifiedPurchaseOrchestrator = unifiedPurchaseOrchestrator
}

// SetTransactionService sets the transaction service dependency
func (s *TicketService) SetTransactionService(transactionService *TransactionService) {
	s.transactionService = transactionService
}

// GetEmailQueueService returns the email queue service
func (s *TicketService) GetEmailQueueService() *EmailQueueService {
	return s.emailQueueService
}

// GetDB returns the database connection
func (s *TicketService) GetDB() *gorm.DB {
	return s.db
}

// GetUserTickets returns all tickets purchased by a user with advanced filtering and pagination
func (s *TicketService) GetUserTickets(userID uuid.UUID, page, limit int, status, eventID, sortBy, sortOrder string, startDate, endDate *time.Time) ([]models.Ticket, int64, error) {
	var tickets []models.Ticket
	var total int64

	offset := (page - 1) * limit

	query := s.db.Model(&models.Ticket{}).
		Where("user_id = ?", userID).
		Preload("Event").
		Preload("User")

	// Apply status filter
	if status != "" {
		query = query.Where("status = ?", status)
	}

	// Apply event filter
	if eventID != "" {
		query = query.Where("event_id = ?", eventID)
	}

	// Apply date range filters
	if startDate != nil {
		query = query.Where("transactions.created_at >= ?", *startDate)
	}
	if endDate != nil {
		query = query.Where("transactions.created_at <= ?", *endDate)
	}

	// Get total count
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Validate and set sorting
	validSortFields := map[string]bool{
		"created_at":    true,
		"ticket_number": true,
		"price":         true,
	}

	if !validSortFields[sortBy] {
		sortBy = "created_at"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	orderClause := sortBy + " " + sortOrder

	// Get paginated results
	if err := query.Order(orderClause).
		Offset(offset).
		Limit(limit).
		Find(&tickets).Error; err != nil {
		return nil, 0, err
	}

	return tickets, total, nil
}

// GetUserTicketSummaries returns flattened ticket summaries for a user with filtering
func (s *TicketService) GetUserTicketSummaries(userID uuid.UUID, page, limit int, filter, search, eventID, sortBy, sortOrder string, startDate, endDate *time.Time) ([]models.UserTicketSummaryResponse, int64, error) {
	var summaries []models.UserTicketSummaryResponse
	var total int64

	offset := (page - 1) * limit
	now := time.Now()

	// Base query for transactions
	query := s.db.Table("transactions").
		Select(`
			transactions.id as transaction_id,
			events.id as event_id,
			events.title,
			events.banner_image,
			events.venue_name,
			events.address,
			events.start_date,
			events.end_date,
			events.status as event_status,
			transactions.status as transaction_status,
			transactions.quantity as ticket_count,
			transactions.created_at,
			transactions.updated_at
		`).
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Where("transactions.user_id = ?", userID)

	// Apply event filter
	if eventID != "" {
		query = query.Where("transactions.event_id = ?", eventID)
	}

	// Apply search filter
	if search != "" {
		searchTerm := "%" + search + "%"
		query = query.Where(`
			(transactions.id IN (
				SELECT DISTINCT t.transaction_id 
				FROM tickets t 
				WHERE t.transaction_id IS NOT NULL 
				AND t.ticket_number ILIKE ?
			)) OR events.title ILIKE ? OR events.venue_name ILIKE ? OR events.address ILIKE ? OR events.location ILIKE ?`,
			searchTerm, searchTerm, searchTerm, searchTerm, searchTerm)
	}

	// Apply date range filters
	if startDate != nil {
		query = query.Where("transactions.created_at >= ?", *startDate)
	}
	if endDate != nil {
		query = query.Where("transactions.created_at <= ?", *endDate)
	}

	// Apply upcoming/past filter
	if filter == "upcoming" {
		query = query.Where("events.start_date > ?", now)
	} else if filter == "past" {
		query = query.Where("events.end_date < ?", now)
	}

	// Get total count
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Validate and set sorting
	validSortFields := map[string]bool{
		"created_at":   true,
		"ticket_count": true,
	}

	if !validSortFields[sortBy] {
		sortBy = "created_at"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	// Map sortBy to actual column names
	orderClause := "transactions." + sortBy + " " + sortOrder

	// Get paginated transaction summaries
	var txRows []struct {
		TransactionID     uuid.UUID  `json:"transaction_id"`
		EventID           uuid.UUID  `json:"event_id"`
		Title             string     `json:"title"`
		BannerImage       string     `json:"banner_image"`
		VenueName         string     `json:"venue_name"`
		Address           string     `json:"address"`
		StartDate         time.Time  `json:"start_date"`
		EndDate           *time.Time `json:"end_date"`
		EventStatus       string     `json:"event_status"`
		TransactionStatus string     `json:"transaction_status"`
		TicketCount       int        `json:"ticket_count"`
		CreatedAt         time.Time  `json:"created_at"`
		UpdatedAt         time.Time  `json:"updated_at"`
	}

	if err := query.Order(orderClause).
		Offset(offset).
		Limit(limit).
		Scan(&txRows).Error; err != nil {
		return nil, 0, err
	}

	// Convert to response format
	for _, txRow := range txRows {
		summary := models.UserTicketSummaryResponse{
			ID: txRow.TransactionID,
			Event: models.UserTicketListingEventResponse{
				ID:          txRow.EventID,
				Title:       txRow.Title,
				BannerImage: txRow.BannerImage,
				VenueName:   txRow.VenueName,
				Address:     txRow.Address,
				StartDate:   txRow.StartDate,
				EndDate:     txRow.EndDate,
				Status:      txRow.EventStatus,
			},
			TicketCount:       txRow.TicketCount,
			TransactionStatus: txRow.TransactionStatus,
			CreatedAt:         txRow.CreatedAt,
			UpdatedAt:         txRow.UpdatedAt,
		}
		summaries = append(summaries, summary)
	}

	return summaries, total, nil
}

// GetUserTransactionDetails returns detailed information about a specific transaction
func (s *TicketService) GetUserTransactionDetails(userID uuid.UUID, transactionID uuid.UUID) (*models.UserTransactionWithTicketsResponse, error) {
	// First verify the transaction belongs to the user
	var transaction models.Transaction
	if err := s.db.Where("id = ? AND actor_id = ? AND status = 'completed'", transactionID, userID).
		Preload("Event").
		First(&transaction).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, utils.NewNotFoundError("transaction")
		}
		return nil, err
	}

	// Build response
	response := &models.UserTransactionWithTicketsResponse{
		ID: transactionID,
		Event: models.UserTicketListingEventResponse{
			ID:          transaction.Event.ID,
			Title:       transaction.Event.Title,
			BannerImage: transaction.Event.BannerImage,
			VenueName:   transaction.Event.VenueName,
			Address:     transaction.Event.Address,
			StartDate:   transaction.Event.StartDate,
			EndDate:     &transaction.Event.EndDate,
			Status:      transaction.Event.Status,
		},
		TransactionStatus: transaction.Status,
		CreatedAt:         transaction.CreatedAt,
		UpdatedAt:         transaction.UpdatedAt,
	}

	return response, nil
}

// GenerateQRCodeForTicket returns a base64-encoded QR payload (PNG) for a ticket
func (s *TicketService) GenerateQRCodeForTicket(ticketID uuid.UUID) (string, error) {
	if s.secureQRService == nil {
		return "", utils.NewBusinessLogicError("Secure QR service not configured.")
	}

	// Get the ticket
	var ticket models.Ticket
	if err := s.db.Where("id = ?", ticketID).Preload("Event").First(&ticket).Error; err != nil {
		return "", err
	}

	// Return base64-encoded JSON payload so frontend can render QR image itself
	return s.secureQRService.GenerateSecureQRPayload(&ticket, ticket.Event)
}

// CheckInTicket handles ticket check-in (simplified: one ticket = one person)
func (s *TicketService) CheckInTicket(ticketID uuid.UUID, eventID uuid.UUID, staffID uuid.UUID) error {
	// Use retry logic to handle concurrent check-ins
	return utils.WithRetry(func() error {
		// Start transaction with timeout
		tx := s.db.Begin()
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
			}
		}()

		// Get the ticket with row-level lock and NOWAIT to fail fast on conflicts
		var ticket models.Ticket
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "NOWAIT"}).
			Where("id = ? AND event_id = ?", ticketID, eventID).
			Preload("Event").
			Preload("User").
			Preload("GuestUser").
			Preload("Transaction").
			First(&ticket).Error; err != nil {
			tx.Rollback()
			if strings.Contains(err.Error(), "could not obtain lock") {
				return utils.NewBusinessLogicError("Ticket is being processed, please try again.")
			}
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return utils.NewBusinessLogicError("Ticket not found for this event.")
			}
			return err
		}

		// CRITICAL: Verify transaction exists and payment is completed
		if ticket.TransactionID == nil {
			tx.Rollback()
			return utils.NewBusinessLogicError("No transaction associated with this ticket.")
		}

		if ticket.Transaction == nil {
			tx.Rollback()
			return utils.NewBusinessLogicError("Transaction data not found.")
		}

		// Verify payment was successful
		if ticket.Transaction.Status != "completed" {
			tx.Rollback()
			return fmt.Errorf("Payment not completed - status: %s", ticket.Transaction.Status)
		}

		// Check if ticket is active
		if ticket.Status != "active" {
			tx.Rollback()
			return fmt.Errorf("Ticket is %s and cannot be checked in", ticket.Status)
		}

		// Check if event is happening today or in the future
		now := time.Now()
		if ticket.Event.StartDate.After(now.Add(24 * time.Hour)) {
			tx.Rollback()
			return utils.NewBusinessLogicError("Check-in not available yet for this event.")
		}

		// Check if event has already ended
		if ticket.Event.EndDate.Before(now) {
			tx.Rollback()
			return utils.NewBusinessLogicError("Cannot check in ticket: event has already ended.")
		}

		// Check if already checked in - CRITICAL: prevent duplicate check-ins for single-day events
		// For multi-day events, allow check-in on different days
		isMultiDayEvent := ticket.Event.EndDate.After(ticket.Event.StartDate.Add(24 * time.Hour))
		if ticket.CheckInTime != nil && !isMultiDayEvent {
			tx.Rollback()
			return utils.NewBusinessLogicError("Ticket already checked in.")
		}

		// For multi-day events, allow re-check-in on different days
		if ticket.CheckInTime != nil && isMultiDayEvent {
			// Check if already checked in today
			checkInDate := ticket.CheckInTime.Truncate(24 * time.Hour)
			today := time.Now().Truncate(24 * time.Hour)
			if checkInDate.Equal(today) {
				tx.Rollback()
				return utils.NewBusinessLogicError("Ticket already checked in today.")
			}
		}

		// Set check-in time and staff atomically
		checkInTime := time.Now()
		ticket.CheckInTime = &checkInTime
		ticket.CheckedInBy = &staffID

		if err := tx.Save(&ticket).Error; err != nil {
			tx.Rollback()
			return err
		}

		// Commit transaction
		return tx.Commit().Error
	}, 3, 50*time.Millisecond)
}

// CheckOutTicket handles ticket check-out by staff
func (s *TicketService) CheckOutTicket(ticketID uuid.UUID, eventID uuid.UUID, staffID uuid.UUID) error {
	// Start transaction
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Get the ticket
	var ticket models.Ticket
	if err := tx.Where("id = ? AND event_id = ?", ticketID, eventID).
		Preload("Event").
		First(&ticket).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewBusinessLogicError("Ticket not found for this event.")
		}
		return err
	}

	// Check if ticket is checked in
	if ticket.CheckInTime == nil {
		tx.Rollback()
		return utils.NewBusinessLogicError("Ticket must be checked in before check-out.")
	}

	// Check if already checked out - allow re-check-out for multi-day events on different days
	isMultiDayEvent := ticket.Event.EndDate.After(ticket.Event.StartDate.Add(24 * time.Hour))
	if ticket.CheckOutTime != nil && !isMultiDayEvent {
		tx.Rollback()
		return utils.NewBusinessLogicError("Ticket already checked out.")
	}

	// For multi-day events, allow re-check-out on different days
	if ticket.CheckOutTime != nil && isMultiDayEvent {
		// Check if already checked out today
		checkOutDate := ticket.CheckOutTime.Truncate(24 * time.Hour)
		today := time.Now().Truncate(24 * time.Hour)
		if checkOutDate.Equal(today) {
			tx.Rollback()
			return utils.NewBusinessLogicError("Ticket already checked out today.")
		}
	}

	// Check if event has already ended
	if ticket.Event.EndDate.Before(time.Now()) {
		tx.Rollback()
		return utils.NewBusinessLogicError("Cannot check out ticket: event has already ended.")
	}

	// Update ticket
	checkOutTime := time.Now()
	ticket.CheckOutTime = &checkOutTime
	ticket.CheckedOutBy = &staffID

	if err := tx.Save(&ticket).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return err
	}

	// Reload with associations
	if err := s.db.Preload("User").Preload("Event").First(&ticket, ticket.ID).Error; err != nil {
		return err
	}

	return nil
}

// BulkCheckInTickets handles bulk check-in of multiple tickets
func (s *TicketService) BulkCheckInTickets(qrCodes []string, eventID uuid.UUID, staffID uuid.UUID) ([]map[string]interface{}, error) {
	results := make([]map[string]interface{}, len(qrCodes))

	for i, qrCode := range qrCodes {
		result := map[string]interface{}{
			"qr_code": qrCode,
			"success": false,
			"message": "",
		}

		// Validate QR code
		qrData, err := s.secureQRService.ValidateSecureQR(qrCode, eventID, staffID)
		if err != nil {
			result["message"] = err.Error()
			results[i] = result
			continue
		}

		// Parse ticket ID
		ticketID, err := uuid.Parse(qrData.TicketID)
		if err != nil {
			result["message"] = "Invalid ticket ID in QR code"
			results[i] = result
			continue
		}

		// Check in the ticket
		err = s.CheckInTicket(ticketID, eventID, staffID)
		if err != nil {
			result["message"] = err.Error()
		} else {
			result["success"] = true
			result["message"] = "Ticket checked in successfully"
		}

		results[i] = result
	}

	return results, nil
}

// BulkCheckOutTickets handles bulk check-out of multiple tickets
func (s *TicketService) BulkCheckOutTickets(qrCodes []string, eventID uuid.UUID, staffID uuid.UUID) ([]map[string]interface{}, error) {
	results := make([]map[string]interface{}, len(qrCodes))

	for i, qrCode := range qrCodes {
		result := map[string]interface{}{
			"qr_code": qrCode,
			"success": false,
			"message": "",
		}

		// Validate QR code
		qrData, err := s.secureQRService.ValidateSecureQR(qrCode, eventID, staffID)
		if err != nil {
			result["message"] = err.Error()
			results[i] = result
			continue
		}

		// Parse ticket ID
		ticketID, err := uuid.Parse(qrData.TicketID)
		if err != nil {
			result["message"] = "Invalid ticket ID in QR code"
			results[i] = result
			continue
		}

		// Check out the ticket
		err = s.CheckOutTicket(ticketID, eventID, staffID)
		if err != nil {
			result["message"] = err.Error()
		} else {
			result["success"] = true
			result["message"] = "Ticket checked out successfully"
		}

		results[i] = result
	}

	return results, nil
}

// ValidateTicketForCheckIn validates a single ticket for check-in without actually checking it in
func (s *TicketService) ValidateTicketForCheckIn(qrCode string, eventID uuid.UUID, staffID uuid.UUID) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"qr_code":     qrCode,
		"valid":       false,
		"can_checkin": false,
		"message":     "",
		"ticket_info": map[string]interface{}{},
	}

	// Validate QR code
	qrData, err := s.secureQRService.ValidateSecureQR(qrCode, eventID, staffID)
	if err != nil {
		result["message"] = fmt.Sprintf("QR validation failed: %s", err.Error())
		return result, nil
	}

	// Parse ticket ID
	ticketID, err := uuid.Parse(qrData.TicketID)
	if err != nil {
		result["message"] = "Invalid ticket ID in QR code"
		return result, nil
	}

	// Get ticket details
	var ticket models.Ticket
	if err := s.db.Preload("Event").Preload("Tier").Preload("User").Preload("GuestUser").First(&ticket, ticketID).Error; err != nil {
		result["message"] = "Ticket not found"
		return result, nil
	}

	// Validate ticket belongs to the event
	if ticket.EventID != eventID {
		result["message"] = "Ticket does not belong to this event"
		return result, nil
	}

	// Check ticket status
	if ticket.Status != "active" {
		result["message"] = fmt.Sprintf("Ticket status is %s, cannot check-in", ticket.Status)
		return result, nil
	}

	// Check if already checked in
	isMultiDayEvent := ticket.Event.EndDate.After(ticket.Event.StartDate.Add(24 * time.Hour))
	if ticket.CheckInTime != nil && !isMultiDayEvent {
		result["message"] = "Ticket already checked in"
		result["can_checkin"] = false
	} else if ticket.CheckInTime != nil && isMultiDayEvent {
		// For multi-day events, check if already checked in today
		checkInDate := ticket.CheckInTime.Truncate(24 * time.Hour)
		today := time.Now().Truncate(24 * time.Hour)
		if checkInDate.Equal(today) {
			result["message"] = "Ticket already checked in today"
			result["can_checkin"] = false
		} else {
			result["valid"] = true
			result["can_checkin"] = true
			result["message"] = "Ticket is valid and ready for check-in (multi-day event)"
		}
	} else {
		result["valid"] = true
		result["can_checkin"] = true
		result["message"] = "Ticket is valid and ready for check-in"
	}

	// Add ticket information
	ticketInfo := map[string]interface{}{
		"ticket_id":     ticket.ID.String(),
		"ticket_number": ticket.TicketNumber,
		"status":        ticket.Status,
		"checked_in":    ticket.CheckInTime != nil,
		"checked_out":   ticket.CheckOutTime != nil,
	}

	// Add attendee info
	if ticket.User != nil {
		ticketInfo["attendee"] = map[string]interface{}{
			"name":  ticket.User.FirstName + " " + ticket.User.LastName,
			"email": ticket.User.Email,
			"type":  "user",
		}
	} else if ticket.GuestUser != nil {
		ticketInfo["attendee"] = map[string]interface{}{
			"name":  ticket.GuestUser.FirstName + " " + ticket.GuestUser.LastName,
			"email": ticket.GuestUser.Email,
			"type":  "guest",
		}
	}

	result["ticket_info"] = ticketInfo
	return result, nil
}

// ValidateTicketForCheckOut validates a single ticket for check-out without actually checking it out
func (s *TicketService) ValidateTicketForCheckOut(qrCode string, eventID uuid.UUID, staffID uuid.UUID) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"qr_code":      qrCode,
		"valid":        false,
		"can_checkout": false,
		"message":      "",
		"ticket_info":  map[string]interface{}{},
	}

	// Validate QR code
	qrData, err := s.secureQRService.ValidateSecureQR(qrCode, eventID, staffID)
	if err != nil {
		result["message"] = fmt.Sprintf("QR validation failed: %s", err.Error())
		return result, nil
	}

	// Parse ticket ID
	ticketID, err := uuid.Parse(qrData.TicketID)
	if err != nil {
		result["message"] = "Invalid ticket ID in QR code"
		return result, nil
	}

	// Get ticket details
	var ticket models.Ticket
	if err := s.db.Preload("Event").Preload("Tier").Preload("User").Preload("GuestUser").First(&ticket, ticketID).Error; err != nil {
		result["message"] = "Ticket not found"
		return result, nil
	}

	// Validate ticket belongs to the event
	if ticket.EventID != eventID {
		result["message"] = "Ticket does not belong to this event"
		return result, nil
	}

	// Check ticket status
	if ticket.Status != "active" {
		result["message"] = fmt.Sprintf("Ticket status is %s, cannot check-out", ticket.Status)
		return result, nil
	}

	// Check if checked in but not checked out
	isMultiDayEvent := ticket.Event.EndDate.After(ticket.Event.StartDate.Add(24 * time.Hour))
	if ticket.CheckInTime == nil {
		result["message"] = "Ticket not checked in yet"
		result["can_checkout"] = false
	} else if ticket.CheckOutTime != nil && !isMultiDayEvent {
		result["message"] = "Ticket already checked out"
		result["can_checkout"] = false
	} else if ticket.CheckOutTime != nil && isMultiDayEvent {
		// For multi-day events, check if already checked out today
		checkOutDate := ticket.CheckOutTime.Truncate(24 * time.Hour)
		today := time.Now().Truncate(24 * time.Hour)
		if checkOutDate.Equal(today) {
			result["message"] = "Ticket already checked out today"
			result["can_checkout"] = false
		} else {
			result["valid"] = true
			result["can_checkout"] = true
			result["message"] = "Ticket is valid and ready for check-out (multi-day event)"
		}
	} else {
		result["valid"] = true
		result["can_checkout"] = true
		result["message"] = "Ticket is valid and ready for check-out"
	}

	// Add ticket information
	ticketInfo := map[string]interface{}{
		"ticket_id":     ticket.ID.String(),
		"ticket_number": ticket.TicketNumber,
		"status":        ticket.Status,
		"checked_in":    ticket.CheckInTime != nil,
		"checked_out":   ticket.CheckOutTime != nil,
	}

	// Add attendee info
	if ticket.User != nil {
		ticketInfo["attendee"] = map[string]interface{}{
			"name":  ticket.User.FirstName + " " + ticket.User.LastName,
			"email": ticket.User.Email,
			"type":  "user",
		}
	} else if ticket.GuestUser != nil {
		ticketInfo["attendee"] = map[string]interface{}{
			"name":  ticket.GuestUser.FirstName + " " + ticket.GuestUser.LastName,
			"email": ticket.GuestUser.Email,
			"type":  "guest",
		}
	}

	result["ticket_info"] = ticketInfo
	return result, nil
}

// ValidateTicketForCheckInByNumber validates a single ticket for check-in using ticket number
func (s *TicketService) ValidateTicketForCheckInByNumber(ticketNumber string, eventID uuid.UUID, staffID uuid.UUID) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"ticket_number": ticketNumber,
		"valid":         false,
		"can_checkin":   false,
		"message":       "",
		"ticket_info":   map[string]interface{}{},
	}

	// Get ticket by ticket number and event
	var ticket models.Ticket
	if err := s.db.Preload("Event").Preload("Tier").Preload("User").Preload("GuestUser").
		Where("ticket_number = ? AND event_id = ?", ticketNumber, eventID).First(&ticket).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			result["message"] = "Ticket not found for this event"
		} else {
			result["message"] = "Database error occurred"
		}
		return result, nil
	}

	// Check ticket status
	if ticket.Status != "active" {
		result["message"] = fmt.Sprintf("Ticket status is %s, cannot check-in", ticket.Status)
		return result, nil
	}

	// Check if already checked in
	isMultiDayEvent := ticket.Event.EndDate.After(ticket.Event.StartDate.Add(24 * time.Hour))
	if ticket.CheckInTime != nil && !isMultiDayEvent {
		result["message"] = "Ticket already checked in"
		result["can_checkin"] = false
	} else if ticket.CheckInTime != nil && isMultiDayEvent {
		// For multi-day events, check if already checked in today
		checkInDate := ticket.CheckInTime.Truncate(24 * time.Hour)
		today := time.Now().Truncate(24 * time.Hour)
		if checkInDate.Equal(today) {
			result["message"] = "Ticket already checked in today"
			result["can_checkin"] = false
		} else {
			result["valid"] = true
			result["can_checkin"] = true
			result["message"] = "Ticket is valid and ready for check-in (multi-day event)"
		}
	} else {
		result["valid"] = true
		result["can_checkin"] = true
		result["message"] = "Ticket is valid and ready for check-in"
	}

	// Add ticket information
	ticketInfo := map[string]interface{}{
		"ticket_id":     ticket.ID.String(),
		"ticket_number": ticket.TicketNumber,
		"status":        ticket.Status,
		"checked_in":    ticket.CheckInTime != nil,
		"checked_out":   ticket.CheckOutTime != nil,
		"tier_name":     ticket.Tier.TierName,
		"purchase_date": ticket.CreatedAt.Format("2006-01-02 15:04:05"),
	}

	// Add user/guest information
	if ticket.User != nil {
		ticketInfo["attendee"] = map[string]interface{}{
			"name":  ticket.User.FirstName + " " + ticket.User.LastName,
			"email": ticket.User.Email,
			"type":  "registered",
		}
	} else if ticket.GuestUser != nil {
		ticketInfo["attendee"] = map[string]interface{}{
			"name":  ticket.GuestUser.FirstName + " " + ticket.GuestUser.LastName,
			"email": ticket.GuestUser.Email,
			"type":  "guest",
		}
	}

	result["ticket_info"] = ticketInfo
	return result, nil
}

// ValidateTicketForCheckOutByNumber validates a single ticket for check-out using ticket number
func (s *TicketService) ValidateTicketForCheckOutByNumber(ticketNumber string, eventID uuid.UUID, staffID uuid.UUID) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"ticket_number": ticketNumber,
		"valid":         false,
		"can_checkout":  false,
		"message":       "",
		"ticket_info":   map[string]interface{}{},
	}

	// Get ticket by ticket number and event
	var ticket models.Ticket
	if err := s.db.Preload("Event").Preload("Tier").Preload("User").Preload("GuestUser").
		Where("ticket_number = ? AND event_id = ?", ticketNumber, eventID).First(&ticket).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			result["message"] = "Ticket not found for this event"
		} else {
			result["message"] = "Database error occurred"
		}
		return result, nil
	}

	// Check ticket status
	if ticket.Status != "active" {
		result["message"] = fmt.Sprintf("Ticket status is %s, cannot check-out", ticket.Status)
		return result, nil
	}

	// Check if checked in
	if ticket.CheckInTime == nil {
		result["message"] = "Ticket has not been checked in yet"
		return result, nil
	}

	// Check if already checked out
	if ticket.CheckOutTime != nil {
		result["message"] = "Ticket already checked out"
		result["can_checkout"] = false
	} else {
		result["valid"] = true
		result["can_checkout"] = true
		result["message"] = "Ticket is valid and ready for check-out"
	}

	// Add ticket information
	ticketInfo := map[string]interface{}{
		"ticket_id":     ticket.ID.String(),
		"ticket_number": ticket.TicketNumber,
		"status":        ticket.Status,
		"checked_in":    ticket.CheckInTime != nil,
		"checked_out":   ticket.CheckOutTime != nil,
		"tier_name":     ticket.Tier.TierName,
		"purchase_date": ticket.CreatedAt.Format("2006-01-02 15:04:05"),
	}

	// Add user/guest information
	if ticket.User != nil {
		ticketInfo["attendee"] = map[string]interface{}{
			"name":  ticket.User.FirstName + " " + ticket.User.LastName,
			"email": ticket.User.Email,
			"type":  "registered",
		}
	} else if ticket.GuestUser != nil {
		ticketInfo["attendee"] = map[string]interface{}{
			"name":  ticket.GuestUser.FirstName + " " + ticket.GuestUser.LastName,
			"email": ticket.GuestUser.Email,
			"type":  "guest",
		}
	}

	result["ticket_info"] = ticketInfo
	return result, nil
}

// SearchTicketsByNumber performs real-time search for tickets by partial ticket number
func (s *TicketService) SearchTicketsByNumber(eventID uuid.UUID, searchTerm string, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 {
		limit = 10 // Default limit
	}
	if limit > 50 {
		limit = 50 // Max limit
	}

	var tickets []models.Ticket
	query := s.db.Preload("User").Preload("GuestUser").Preload("Tier").
		Where("event_id = ? AND ticket_number ILIKE ?", eventID, "%"+searchTerm+"%").
		Order("ticket_number ASC").
		Limit(limit)

	if err := query.Find(&tickets).Error; err != nil {
		return nil, fmt.Errorf("failed to search tickets: %w", err)
	}

	results := make([]map[string]interface{}, 0, len(tickets))
	for _, ticket := range tickets {
		result := map[string]interface{}{
			"ticket_id":     ticket.ID.String(),
			"ticket_number": ticket.TicketNumber,
			"status":        ticket.Status,
			"tier_name":     ticket.Tier.TierName,
			"checked_in":    ticket.CheckInTime != nil,
			"checked_out":   ticket.CheckOutTime != nil,
		}

		// Add attendee info
		if ticket.User != nil {
			result["attendee"] = map[string]interface{}{
				"name":  ticket.User.FirstName + " " + ticket.User.LastName,
				"email": ticket.User.Email,
				"type":  "registered",
			}
		} else if ticket.GuestUser != nil {
			result["attendee"] = map[string]interface{}{
				"name":  ticket.GuestUser.FirstName + " " + ticket.GuestUser.LastName,
				"email": ticket.GuestUser.Email,
				"type":  "guest",
			}
		}

		results = append(results, result)
	}

	return results, nil
}

// GetEventTickets returns all tickets for a specific event (for organizers)
func (s *TicketService) GetEventTickets(eventID uuid.UUID, organizerID uuid.UUID, page, limit int, sortBy, sortOrder string) ([]models.OrganizerTicketResponse, int64, error) {
	return s.GetEventTicketsWithFilters(eventID, organizerID, "", "", nil, "", page, limit, sortBy, sortOrder)
}

// GetEventTicketsWithFilters gets event tickets with search, filter, and sorting capabilities
func (s *TicketService) GetEventTicketsWithFilters(eventID uuid.UUID, organizerID uuid.UUID, search, status string, tierID *uuid.UUID, checkinStatus string, page, limit int, sortBy, sortOrder string) ([]models.OrganizerTicketResponse, int64, error) {
	var total int64
	offset := (page - 1) * limit

	// First verify the organizer owns this event
	var event models.Event
	if err := s.db.Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, utils.NewBusinessLogicError("Event not found or access denied.")
		}
		return nil, 0, err
	}

	// Build the base query with all necessary JOINs to avoid conditional JOIN issues
	baseQuery := s.db.Model(&models.Ticket{}).
		Joins("LEFT JOIN event_tiers et ON tickets.tier_id = et.id").
		Joins("LEFT JOIN users u ON tickets.user_id = u.id").
		Joins("LEFT JOIN guest_users gu ON tickets.guest_user_id = gu.id").
		Joins("LEFT JOIN users staff ON tickets.checked_in_by = staff.id").
		Where("tickets.event_id = ?", eventID)

	// Apply filters
	if status != "" {
		baseQuery = baseQuery.Where("tickets.status = ?", status)
	}
	if tierID != nil {
		baseQuery = baseQuery.Where("tickets.tier_id = ?", *tierID)
	}

	// Apply check-in status filter
	switch checkinStatus {
	case "checked_in":
		baseQuery = baseQuery.Where("tickets.check_in_time IS NOT NULL AND tickets.check_out_time IS NULL")
	case "not_checked_in":
		baseQuery = baseQuery.Where("tickets.check_in_time IS NULL")
	case "checked_out":
		baseQuery = baseQuery.Where("tickets.check_out_time IS NOT NULL")
	}

	// Apply search filter
	if search != "" {
		searchTerm := "%" + strings.ToLower(search) + "%"
		baseQuery = baseQuery.Where("LOWER(tickets.ticket_number) LIKE ? OR LOWER(COALESCE(u.first_name || ' ' || u.last_name, gu.first_name || ' ' || gu.last_name)) LIKE ? OR LOWER(COALESCE(u.email, gu.email)) LIKE ?",
			searchTerm, searchTerm, searchTerm)
	}

	// Get total count
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Apply sorting
	var orderClause string
	switch sortBy {
	case "tier":
		orderClause = "LOWER(et.tier_name) " + sortOrder
	case "check_in_time":
		// NULLS FIRST for check_in_time
		if sortOrder == "asc" {
			orderClause = "tickets.check_in_time IS NULL DESC, tickets.check_in_time ASC"
		} else {
			orderClause = "tickets.check_in_time IS NULL DESC, tickets.check_in_time DESC"
		}
	case "checked_in_by":
		orderClause = "LOWER(staff.first_name || ' ' || staff.last_name) " + sortOrder
	case "purchase_date":
		orderClause = "tickets.created_at " + sortOrder
	case "purchased_by":
		orderClause = "LOWER(COALESCE(u.first_name || ' ' || u.last_name, gu.first_name || ' ' || gu.last_name)) " + sortOrder
	case "ticket_number":
		orderClause = "LOWER(tickets.ticket_number) " + sortOrder
	case "status":
		orderClause = "LOWER(tickets.status) " + sortOrder
	default:
		orderClause = "tickets." + sortBy + " " + sortOrder
	}

	// Get paginated results with preloaded relations
	var tickets []models.Ticket
	if err := baseQuery.Preload("Tier").
		Order(orderClause).
		Offset(offset).
		Limit(limit).
		Find(&tickets).Error; err != nil {
		return nil, 0, err
	}

	// Get all unique staff IDs for checked_in_by and checked_out_by
	staffIDs := make(map[uuid.UUID]bool)
	for _, ticket := range tickets {
		if ticket.CheckedInBy != nil {
			staffIDs[*ticket.CheckedInBy] = true
		}
		if ticket.CheckedOutBy != nil {
			staffIDs[*ticket.CheckedOutBy] = true
		}
	}

	// Fetch staff users
	staffMap := make(map[uuid.UUID]string)
	if len(staffIDs) > 0 {
		var staffUsers []models.User
		var ids []uuid.UUID
		for id := range staffIDs {
			ids = append(ids, id)
		}
		if err := s.db.Where("id IN ?", ids).Find(&staffUsers).Error; err == nil {
			for _, staff := range staffUsers {
				fullName := staff.FirstName
				if staff.LastName != "" {
					fullName += " " + staff.LastName
				}
				staffMap[staff.ID] = fullName
			}
		}
	}

	// Map to response
	responses := make([]models.OrganizerTicketResponse, len(tickets))

	// Collect attendee ids to query minimal info (users and guest_users)
	userIDs := make(map[uuid.UUID]bool)
	guestIDs := make(map[uuid.UUID]bool)
	for _, ticket := range tickets {
		if ticket.UserID != nil {
			userIDs[*ticket.UserID] = true
		} else if ticket.GuestUserID != nil {
			guestIDs[*ticket.GuestUserID] = true
		}
	}

	// Query users
	userMap := make(map[uuid.UUID]models.User)
	if len(userIDs) > 0 {
		var ids []uuid.UUID
		for id := range userIDs {
			ids = append(ids, id)
		}
		var users []models.User
		if err := s.db.Where("id IN ?", ids).Find(&users).Error; err == nil {
			for _, u := range users {
				userMap[u.ID] = u
			}
		}
	}

	// Query guest users
	guestMap := make(map[uuid.UUID]models.GuestUser)
	if len(guestIDs) > 0 {
		var ids []uuid.UUID
		for id := range guestIDs {
			ids = append(ids, id)
		}
		var guests []models.GuestUser
		if err := s.db.Where("id IN ?", ids).Find(&guests).Error; err == nil {
			for _, g := range guests {
				guestMap[g.ID] = g
			}
		}
	}
	for i, ticket := range tickets {
		// Create simplified tier response
		var simpleTier *models.SimpleTierResponse
		if ticket.Tier != nil {
			simpleTier = &models.SimpleTierResponse{
				ID:   ticket.Tier.ID,
				Name: ticket.Tier.TierName,
			}
		}

		response := models.OrganizerTicketResponse{
			ID:              ticket.ID,
			TicketNumber:    ticket.TicketNumber,
			EventID:         ticket.EventID,
			Tier:            simpleTier,
			TotalAmount:     ticket.TotalAmount,
			PaymentGateway:  ticket.PaymentGateway,
			Status:          ticket.Status,
			IsGuestPurchase: ticket.IsGuestPurchase,
			CheckInTime:     ticket.CheckInTime,
			CheckOutTime:    ticket.CheckOutTime,
			CreatedAt:       ticket.CreatedAt,
			UpdatedAt:       ticket.UpdatedAt,
		}

		// Populate attendee minimal info (id, name, email)
		if ticket.UserID != nil {
			if u, ok := userMap[*ticket.UserID]; ok {
				response.Attendee = &models.AttendeeResponse{
					ID:    &u.ID,
					Name:  u.FirstName + " " + u.LastName,
					Email: u.Email,
					Type:  "user",
				}
			}
		} else if ticket.GuestUserID != nil {
			if g, ok := guestMap[*ticket.GuestUserID]; ok {
				response.Attendee = &models.AttendeeResponse{
					ID:    &g.ID,
					Name:  g.FirstName + " " + g.LastName,
					Email: g.Email,
					Type:  "guest",
				}
			}
		}

		// Add checked in by name
		if ticket.CheckedInBy != nil {
			if name, ok := staffMap[*ticket.CheckedInBy]; ok {
				response.CheckedInByName = name
			}
		}

		// Add checked out by name
		if ticket.CheckedOutBy != nil {
			if name, ok := staffMap[*ticket.CheckedOutBy]; ok {
				response.CheckedOutByName = name
			}
		}

		responses[i] = response
	}

	return responses, total, nil
}

// GetTicketStats returns ticket statistics for an event (counts from tickets table, revenue from transactions table)
func (s *TicketService) GetTicketStats(eventID uuid.UUID, organizerID uuid.UUID) (map[string]interface{}, error) {
	// First verify the organizer owns this event
	var event models.Event
	if err := s.db.Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewBusinessLogicError("Event not found or access denied.")
		}
		return nil, err
	}

	var ticketStats struct {
		TotalTickets     int64 `json:"total_tickets"`
		CheckedIn        int64 `json:"checked_in"`
		CheckedOut       int64 `json:"checked_out"`
		ActiveTickets    int64 `json:"active_tickets"`
		CancelledTickets int64 `json:"cancelled_tickets"`
	}

	// Get ticket counts by status from the Ticket table
	s.db.Model(&models.Ticket{}).
		Where("event_id = ?", eventID).
		Select("COUNT(*) as total_tickets, SUM(CASE WHEN check_in_time IS NOT NULL THEN 1 ELSE 0 END) as checked_in, SUM(CASE WHEN check_out_time IS NOT NULL THEN 1 ELSE 0 END) as checked_out, SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END) as active_tickets, SUM(CASE WHEN status IN ('cancelled', 'pending_refund', 'refunded', 'expired') THEN 1 ELSE 0 END) as cancelled_tickets").
		Scan(&ticketStats)

	// Get revenue from transactions table (authoritative financial source)
	var revenueStats struct {
		TotalRevenue     float64 `json:"total_revenue"`
		CommissionAmount float64 `json:"commission_amount"`
		OrganizerShare   float64 `json:"organizer_share"`
	}

	if err := s.db.Model(&models.Transaction{}).
		Where("event_id = ? AND status = ?", eventID, "completed").
		Select("COALESCE(SUM(amount), 0) as total_revenue, COALESCE(SUM(commission_amount), 0) as commission_amount, COALESCE(SUM(organizer_share), 0) as organizer_share").
		Scan(&revenueStats).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to calculate revenue statistics from transactions.", err)
	}

	// Use transaction-based commission if available, otherwise calculate from rate
	commissionRate := event.CommissionRate / 100
	var finalCommissionAmount float64
	var finalOrganizerShare float64

	if revenueStats.CommissionAmount > 0 || revenueStats.OrganizerShare > 0 {
		// Use the actual stored commission and organizer share from transactions
		finalCommissionAmount = revenueStats.CommissionAmount
		finalOrganizerShare = revenueStats.OrganizerShare
	} else {
		// Fallback: calculate from rate (for events with no transactions or if fields not populated)
		finalCommissionAmount = revenueStats.TotalRevenue * commissionRate
		finalOrganizerShare = revenueStats.TotalRevenue - finalCommissionAmount
	}

	return map[string]interface{}{
		"total_tickets":     ticketStats.TotalTickets,
		"checked_in":        ticketStats.CheckedIn,
		"checked_out":       ticketStats.CheckedOut,
		"active_tickets":    ticketStats.ActiveTickets,
		"cancelled_tickets": ticketStats.CancelledTickets,
		"total_revenue":     revenueStats.TotalRevenue,
		"commission_rate":   event.CommissionRate,
		"commission_amount": finalCommissionAmount,
		"organizer_share":   finalOrganizerShare,
	}, nil
}

// GetTicketByID retrieves a ticket by its ID
func (s *TicketService) GetTicketByID(ticketID uuid.UUID) (*models.Ticket, error) {
	var ticket models.Ticket
	if err := s.db.Preload("Event").Preload("User").Preload("Tier").First(&ticket, ticketID).Error; err != nil {
		return nil, err
	}
	return &ticket, nil
}

// GetUserEventTickets retrieves all tickets purchased by a user for a specific event
func (s *TicketService) GetUserEventTickets(userID, eventID uuid.UUID, page, limit int) ([]models.Ticket, int64, error) {
	var tickets []models.Ticket
	var total int64

	offset := (page - 1) * limit

	query := s.db.Model(&models.Ticket{}).
		Where("user_id = ? AND event_id = ?", userID, eventID).
		Preload("Event").
		Preload("Tier")

	// Get total count
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Get paginated results
	if err := query.Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&tickets).Error; err != nil {
		return nil, 0, err
	}

	return tickets, total, nil
}

// GetUserTicketStats retrieves statistics about a user's ticket purchases and usage
func (s *TicketService) GetUserTicketStats(userID uuid.UUID) (map[string]interface{}, error) {
	var stats struct {
		TotalTickets     int64   `json:"total_tickets"`
		ActiveTickets    int64   `json:"active_tickets"`
		UsedTickets      int64   `json:"used_tickets"`
		CancelledTickets int64   `json:"cancelled_tickets"`
		TotalSpent       float64 `json:"total_spent"`
		UpcomingEvents   int64   `json:"upcoming_events"`
		PastEvents       int64   `json:"past_events"`
	}

	// Get ticket statistics
	err := s.db.Model(&models.Ticket{}).
		Select(`
			COUNT(*) as total_tickets,
			SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END) as active_tickets,
			SUM(CASE WHEN status = 'used' THEN 1 ELSE 0 END) as used_tickets,
			SUM(CASE WHEN status IN ('cancelled', 'pending_refund', 'refunded', 'expired') THEN 1 ELSE 0 END) as cancelled_tickets,
			COALESCE(SUM(total_amount), 0) as total_spent
		`).
		Where("user_id = ?", userID).
		Scan(&stats).Error

	if err != nil {
		return nil, err
	}

	// Get event statistics (upcoming vs past events the user has tickets for)
	now := "NOW()" // Assuming PostgreSQL, adjust for other databases
	if s.db.Dialector.Name() == "sqlite" {
		now = "datetime('now')"
	}

	err = s.db.Table("tickets t").
		Joins("JOIN events e ON t.event_id = e.id").
		Select(`
			SUM(CASE WHEN e.start_date > `+now+` THEN 1 ELSE 0 END) as upcoming_events,
			SUM(CASE WHEN e.end_date < `+now+` THEN 1 ELSE 0 END) as past_events
		`).
		Where("t.user_id = ?", userID).
		Scan(&stats).Error

	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total_tickets":     stats.TotalTickets,
		"active_tickets":    stats.ActiveTickets,
		"used_tickets":      stats.UsedTickets,
		"cancelled_tickets": stats.CancelledTickets,
		"total_spent":       stats.TotalSpent,
		"upcoming_events":   stats.UpcomingEvents,
		"past_events":       stats.PastEvents,
	}, nil
}

// PurchaseTicketAsGuest creates multiple individual ticket purchases for a guest user
func (s *TicketService) PurchaseTicketAsGuest(req *models.GuestPurchaseRequest) ([]*models.Ticket, *models.GuestUser, error) {
	// Validate total quantity across all tiers doesn't exceed limits
	totalQuantity := 0
	for _, tierSelection := range req.Tiers {
		totalQuantity += tierSelection.Quantity
	}
	if totalQuantity > 6 {
		return nil, nil, utils.NewBusinessLogicError("Total tickets cannot exceed 6 per purchase for guest users")
	}

	// Use tier-level locking to prevent race conditions - lock all tiers
	var unlocks []func()
	for _, tierSelection := range req.Tiers {
		unlock := utils.GetInventoryLock().LockTier(tierSelection.TierID.String())
		unlocks = append(unlocks, unlock)
	}
	defer func() {
		for _, unlock := range unlocks {
			unlock()
		}
	}()

	// Retry logic for deadlock recovery
	return utils.WithRetryFunc2(func() ([]*models.Ticket, *models.GuestUser, error) {
		// Start transaction
		tx := s.db.Begin()

		// Create or find guest user
		guestUser, err := s.createOrFindGuestUser(tx, req)
		if err != nil {
			tx.Rollback()
			return nil, nil, err
		}

		// Get event details for ticket number generation
		var event models.Event
		if err := tx.Where("id = ?", req.EventID).First(&event).Error; err != nil {
			tx.Rollback()
			return nil, nil, err
		}

		var allTickets []*models.Ticket
		totalAmount := 0.0

		// Process each tier selection
		for _, tierSelection := range req.Tiers {
			// Get event tier details with lock for update
			var eventTier models.EventTier
			err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND event_id = ?", tierSelection.TierID, req.EventID).
				First(&eventTier).Error
			if err != nil {
				tx.Rollback()
				return nil, nil, err
			}

			// Check if tier is active
			if !eventTier.IsActive {
				tx.Rollback()
				return nil, nil, fmt.Errorf("Event tier %s is not active", eventTier.TierName)
			}

			// Check availability
			if eventTier.Available < tierSelection.Quantity {
				tx.Rollback()
				return nil, nil, fmt.Errorf("Insufficient tickets available for tier %s", eventTier.TierName)
			}

			// Create individual tickets for each quantity in this tier
			for i := 0; i < tierSelection.Quantity; i++ {
				// Generate sequential ticket number using atomic sequence
				ticketNum, err := utils.GenerateEventTicketNumber(tx, eventTier.TierName, event.StartDate.Year())
				if err != nil {
					tx.Rollback()
					return nil, nil, fmt.Errorf("failed to generate ticket number: %w", err)
				}

				ticket := &models.Ticket{
					TicketNumber:    ticketNum,
					GuestUserID:     &guestUser.ID,
					EventID:         req.EventID,
					TierID:          eventTier.ID,
					TotalAmount:     eventTier.Price,
					PaymentGateway:  req.PaymentGateway,
					Status:          "active",
					IsGuestPurchase: true,
				}

				if err := tx.Create(ticket).Error; err != nil {
					tx.Rollback()
					return nil, nil, err
				}

				allTickets = append(allTickets, ticket)
				totalAmount += eventTier.Price
			}

			// Update tier availability and sold count atomically
			if err := tx.Model(&eventTier).
				Where("id = ? AND available >= ?", eventTier.ID, tierSelection.Quantity).
				Updates(map[string]interface{}{
					"available": gorm.Expr("available - ?", tierSelection.Quantity),
					"sold":      gorm.Expr("sold + ?", tierSelection.Quantity),
				}).Error; err != nil {
				tx.Rollback()
				return nil, nil, err
			}
		}

		// Update event availability atomically
		if err := tx.Model(&event).
			Where("id = ? AND available >= ?", event.ID, totalQuantity).
			Update("available", gorm.Expr("available - ?", totalQuantity)).Error; err != nil {
			tx.Rollback()
			return nil, nil, err
		}

		// Record transaction for successful guest purchase (inside transaction)
		if err := s.transactionService.recordTransactionInTx(tx, allTickets, string(req.PaymentGateway), "", nil, "completed", nil); err != nil {
			tx.Rollback()
			return nil, nil, fmt.Errorf("Failed to record transaction: %w", err)
		}

		// Commit transaction
		if err := tx.Commit().Error; err != nil {
			return nil, nil, err
		}

		// Load associations for response
		for _, ticket := range allTickets {
			if err := s.db.Preload("GuestUser").Preload("Event").Preload("Tier").First(ticket, ticket.ID).Error; err != nil {
				return nil, nil, err
			}
		}

		return allTickets, guestUser, nil
	})
}

// createOrFindGuestUser creates a new guest user or finds existing one
func (s *TicketService) createOrFindGuestUser(tx *gorm.DB, req *models.GuestPurchaseRequest) (*models.GuestUser, error) {
	var guestUser models.GuestUser

	// Try to find existing guest user
	err := tx.Where("email = ?", req.Email).First(&guestUser).Error
	if err == nil {
		// Update existing guest user info
		guestUser.FirstName = req.FirstName
		guestUser.LastName = req.LastName
		guestUser.UpdatedAt = time.Now()

		if err := tx.Save(&guestUser).Error; err != nil {
			return nil, err
		}
		return &guestUser, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Create new guest user
	guestUser = models.GuestUser{
		Email:     req.Email,
		FirstName: req.FirstName,
		LastName:  req.LastName,
	}

	if err := tx.Create(&guestUser).Error; err != nil {
		return nil, err
	}

	return &guestUser, nil
}

// VerifyGuestEmail verifies guest email and activates tickets
func (s *TicketService) VerifyGuestEmail(token string) (*models.Ticket, error) {
	// Find guest user by verification token
	var guestUser models.GuestUser
	if err := s.db.Where("verification_token = ?", token).First(&guestUser).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewBusinessLogicError("Invalid verification token.")
		}
		return nil, err
	}

	// Check if token is expired
	if guestUser.TokenExpiresAt != nil && time.Now().After(*guestUser.TokenExpiresAt) {
		return nil, utils.NewBusinessLogicError("Verification token has expired.")
	}

	// Mark email as verified
	guestUser.EmailVerified = true
	guestUser.VerificationToken = ""
	guestUser.TokenExpiresAt = nil

	if err := s.db.Save(&guestUser).Error; err != nil {
		return nil, err
	}

	// Find and activate the associated ticket
	var ticket models.Ticket
	if err := s.db.Where("guest_user_id = ? AND status = ?", guestUser.ID, "pending_verification").
		Preload("Event").Preload("GuestUser").First(&ticket).Error; err != nil {
		return nil, err
	}

	// Activate ticket
	ticket.Status = "active"
	if err := s.db.Save(&ticket).Error; err != nil {
		return nil, err
	}

	return &ticket, nil
}

// handleCashGuestPurchase handles immediate cash payments for guest purchases
func (s *TicketService) handleCashGuestPurchase(req *models.GuestPurchaseRequest) ([]*models.Ticket, *models.GuestUser, *models.PaymentIntent, error) {
	// Validate total quantity across all tiers doesn't exceed limits
	totalQuantity := 0
	for _, tierSelection := range req.Tiers {
		totalQuantity += tierSelection.Quantity
	}
	if totalQuantity > 6 {
		return nil, nil, nil, utils.NewBusinessLogicError("Total tickets cannot exceed 6 per purchase for guest users.")
	}

	// Use tier-level locking to prevent race conditions - lock all tiers
	var unlocks []func()
	for _, tierSelection := range req.Tiers {
		unlock := utils.GetInventoryLock().LockTier(tierSelection.TierID.String())
		unlocks = append(unlocks, unlock)
	}
	defer func() {
		for _, unlock := range unlocks {
			unlock()
		}
	}()

	// Retry logic for deadlock recovery
	return utils.WithRetryFunc3(func() ([]*models.Ticket, *models.GuestUser, *models.PaymentIntent, error) {
		// Start transaction
		tx := s.db.Begin()

		// Create or find guest user
		guestUser, err := s.createOrFindGuestUser(tx, req)
		if err != nil {
			tx.Rollback()
			return nil, nil, nil, err
		}

		// Get event details for ticket number generation
		var event models.Event
		if err := tx.Where("id = ?", req.EventID).First(&event).Error; err != nil {
			tx.Rollback()
			return nil, nil, nil, err
		}

		var allTickets []*models.Ticket
		totalAmount := 0.0
		currency := "" // Will be set from first tier

		// Process each tier selection
		for _, tierSelection := range req.Tiers {
			// Get event tier details with lock for update
			var eventTier models.EventTier
			err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND event_id = ?", tierSelection.TierID, req.EventID).
				First(&eventTier).Error
			if err != nil {
				tx.Rollback()
				return nil, nil, nil, err
			}

			// Check if tier is active
			if !eventTier.IsActive {
				tx.Rollback()
				return nil, nil, nil, fmt.Errorf("Event tier %s is not active", eventTier.TierName)
			}

			// Check availability
			if eventTier.Available < tierSelection.Quantity {
				tx.Rollback()
				return nil, nil, nil, fmt.Errorf("Insufficient tickets available for tier %s", eventTier.TierName)
			}

			// Set currency from first tier
			if currency == "" {
				currency = eventTier.Currency
			}

			// Create individual tickets for each quantity in this tier
			for i := 0; i < tierSelection.Quantity; i++ {
				// Create ticket (one per person)
				ticket := &models.Ticket{
					GuestUserID:     &guestUser.ID,
					EventID:         req.EventID,
					TierID:          tierSelection.TierID,
					TotalAmount:     eventTier.Price,
					PaymentGateway:  req.PaymentGateway,
					Status:          "active", // Cash payments are immediately active
					IsGuestPurchase: true,
				}

				// Generate sequential ticket number using atomic counter
				ticketNumber, err := utils.GenerateEventTicketNumber(tx, eventTier.TierName, event.StartDate.Year())
				if err != nil {
					tx.Rollback()
					return nil, nil, nil, err
				}
				ticket.TicketNumber = ticketNumber

				// Create ticket in database
				if err := tx.Create(ticket).Error; err != nil {
					tx.Rollback()
					return nil, nil, nil, err
				}

				// Load event data on ticket for gateway initialization
				if err := tx.Preload("Event").First(ticket, ticket.ID).Error; err != nil {
					tx.Rollback()
					return nil, nil, nil, err
				}

				allTickets = append(allTickets, ticket)
				totalAmount += eventTier.Price
			}

			// Update tier availability and sold count atomically after creating all tickets
			if err := tx.Model(&eventTier).
				Where("id = ? AND available >= ?", eventTier.ID, tierSelection.Quantity).
				Updates(map[string]interface{}{
					"available": gorm.Expr("available - ?", tierSelection.Quantity),
					"sold":      gorm.Expr("sold + ?", tierSelection.Quantity),
				}).Error; err != nil {
				tx.Rollback()
				return nil, nil, nil, err
			}
		}

		// Create transaction record for cash payment
		transaction := &models.Transaction{
			EventID:          req.EventID,
			GuestUserID:      &guestUser.ID,
			PaymentGateway:   "cash",
			Amount:           int64(totalAmount * 100), // Convert to cents
			Currency:         currency,
			Quantity:         totalQuantity,
			Status:           "completed",
			GatewayTxnID:     "",
			GatewayData:      map[string]interface{}{"payment_method": "cash"},
			CommissionRate:   0, // TODO: Get from config
			CommissionAmount: 0,
			OrganizerShare:   int64(totalAmount * 100), // Convert to cents
		}

		// Associate tickets with transaction
		var ticketIDs []uuid.UUID
		for _, ticket := range allTickets {
			ticketIDs = append(ticketIDs, ticket.ID)
			ticket.TransactionID = &transaction.ID
			if err := tx.Save(ticket).Error; err != nil {
				tx.Rollback()
				return nil, nil, nil, err
			}
		}

		if err := tx.Create(transaction).Error; err != nil {
			tx.Rollback()
			return nil, nil, nil, err
		}

		// Commit transaction (release database locks)
		if err := tx.Commit().Error; err != nil {
			return nil, nil, nil, err
		}

		// Audit logging for cash payment success
		s.logAudit(context.Background(), "payment_succeeded", "transaction", transaction.ID, nil, "system", &req.EventID, map[string]interface{}{
			"payment_gateway":   "cash",
			"total_amount":      totalAmount,
			"total_tickets":     totalQuantity,
			"payment_method":    "cash",
			"buyer_type":        "guest",
			"currency":          transaction.Currency,
			"commission_amount": transaction.CommissionAmount,
		})

		// Send confirmation emails for cash payments
		if s.emailQueueService != nil {
			if err := s.emailQueueService.QueueGuestTicketConfirmationEmail(guestUser.Email, allTickets); err != nil {
				log.Printf("Failed to queue confirmation email for guest %s: %v", guestUser.Email, err)
			}
		}

		// Return tickets, guest user, and nil checkout session for cash payments
		return allTickets, guestUser, nil, nil
	})
}

// GetGuestTickets returns all tickets purchased by a guest user with advanced filtering and pagination
func (s *TicketService) GetGuestTickets(guestEmail string, page, limit int, status, eventID, sortBy, sortOrder string, startDate, endDate *time.Time) ([]models.Ticket, int64, error) {
	var tickets []models.Ticket
	var total int64

	offset := (page - 1) * limit

	query := s.db.Model(&models.Ticket{}).
		Joins("JOIN guest_users ON tickets.guest_user_id = guest_users.id").
		Where("guest_users.email = ? AND tickets.is_guest_purchase = ?", guestEmail, true).
		Preload("Event").
		Preload("GuestUser")

	// Apply status filter
	if status != "" {
		query = query.Where("tickets.status = ?", status)
	}

	// Apply event filter
	if eventID != "" {
		query = query.Where("tickets.event_id = ?", eventID)
	}

	// Apply date range filters
	if startDate != nil {
		query = query.Where("tickets.created_at >= ?", *startDate)
	}
	if endDate != nil {
		query = query.Where("tickets.created_at <= ?", *endDate)
	}

	// Get total count
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Validate and set sorting
	validSortFields := map[string]bool{
		"created_at":    true,
		"ticket_number": true,
		"price":         true,
	}

	if !validSortFields[sortBy] {
		sortBy = "created_at"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	orderClause := sortBy + " " + sortOrder

	// Get paginated results
	if err := query.Order(orderClause).
		Offset(offset).
		Limit(limit).
		Find(&tickets).Error; err != nil {
		return nil, 0, err
	}

	return tickets, total, nil
}

// ValidateStaffAccessToEvent checks if a staff member can access tickets for a specific event
func (s *TicketService) ValidateStaffAccessToEvent(staffID uuid.UUID, eventID uuid.UUID) error {
	if s.authService == nil {
		return utils.NewBusinessLogicError("Auth service not configured.")
	}

	// Get the staff member details
	staff, err := s.authService.GetUserByID(staffID)
	if err != nil {
		return fmt.Errorf("Failed to get staff details: %w", err)
	}

	// Get the event details
	var event models.Event
	if err := s.db.Where("id = ?", eventID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewBusinessLogicError("Event not found.")
		}
		return err
	}

	// Check if staff is the organizer of the event
	if event.OrganizerID == staffID {
		return nil // Staff is the organizer, access granted
	}

	// Check if staff belongs to the same organizer as the event organizer
	if staff.OrganizerID != nil && *staff.OrganizerID == event.OrganizerID {
		return nil // Staff belongs to same organizer, access granted
	}

	return utils.NewBusinessLogicError("Access denied: you can only scan tickets for events organized by your organization.")
}

func convertTiers(tiers []models.TicketTierSelection) []types.TierSelection {
	out := make([]types.TierSelection, 0, len(tiers))

	for _, t := range tiers {
		out = append(out, types.TierSelection{
			TierID:   t.TierID,
			Quantity: t.Quantity,
		})
	}

	return out
}

// =======================================================
// USER PURCHASE ENTRY (ADAPTER ONLY)
// =======================================================
func (s *TicketService) InitiateUserPaymentGatewayPurchase(
	userID uuid.UUID,
	req *models.TicketPurchaseRequest,
) (*models.PaymentIntent, []*models.Ticket, error) {

	var user models.User
	if err := s.db.First(&user, userID).Error; err != nil {
		return nil, nil, err
	}

	unifiedReq := &UnifiedPurchaseRequest{
		ActorID:        userID,
		ActorType:      "user",
		EventID:        req.EventID,
		Tiers:          convertTiers(req.Tiers),
		PaymentGateway: req.PaymentGateway,
		Currency:       req.Currency,
		Email:          user.Email,
		FullName:       user.FirstName + " " + user.LastName,
	}

	resp, err := s.unifiedPurchaseOrchestrator.Process(context.Background(), unifiedReq)
	if err != nil {
		return nil, nil, err
	}

	var intent models.PaymentIntent
	if err := s.db.Where("checkout_token = ?", resp.CheckoutToken).
		First(&intent).Error; err != nil {
		return nil, nil, err
	}

	return &intent, []*models.Ticket{}, nil
}

// =======================================================
// GUEST PURCHASE ENTRY (ADAPTER ONLY)
// =======================================================
func (s *TicketService) InitiateGuestPaymentGatewayPurchase(
	req *models.GuestPurchaseRequest,
) (*models.PaymentIntent, []*models.Ticket, error) {

	// 1. Find or create guest user (IMPORTANT FIX)
	guest, err := s.createOrFindGuestUser(s.db, req)
	if err != nil {
		return nil, nil, err
	}

	// ensure stable guest identity (DO NOT generate new UUID each time)
	// guest.ID is your ActorID

	unifiedReq := &UnifiedPurchaseRequest{
		ActorID:        guest.ID,
		ActorType:      "guest",
		EventID:        req.EventID,
		Tiers:          convertTiers(req.Tiers),
		PaymentGateway: req.PaymentGateway,
		Currency:       req.Currency,
		Email:          guest.Email,
		FullName:       fmt.Sprintf("%s %s", guest.FirstName, guest.LastName),
	}

	ctx := context.Background()

	resp, err := s.unifiedPurchaseOrchestrator.Process(ctx, unifiedReq)
	if err != nil {
		return nil, nil, err
	}

	var intent models.PaymentIntent
	if err := s.db.Where("checkout_token = ?", resp.CheckoutToken).
		First(&intent).Error; err != nil {
		return nil, nil, err
	}

	return &intent, []*models.Ticket{}, nil
}

// GetPaymentIntents retrieves payment intents with filters (admin only)
// UPDATED: Now returns PaymentIntents instead of CheckoutSessions per clean architecture
func (s *TicketService) GetPaymentIntents(status, paymentGateway string, eventID *uuid.UUID, page, limit int, sortBy, sortOrder string) ([]*models.PaymentIntent, int64, error) {
	var intents []*models.PaymentIntent
	var total int64

	query := s.db.Model(&models.PaymentIntent{}).Preload("Event").Preload("GuestUser").Preload("User")

	if status != "" {
		query = query.Where("status = ?", status)
	}
	if paymentGateway != "" {
		query = query.Where("payment_gateway = ?", paymentGateway)
	}
	if eventID != nil {
		query = query.Where("event_id = ?", *eventID)
	}

	query.Count(&total)

	offset := (page - 1) * limit
	orderClause := sortBy + " " + sortOrder
	if err := query.Order(orderClause).
		Offset(offset).
		Limit(limit).
		Find(&intents).Error; err != nil {
		return nil, 0, err
	}

	return intents, total, nil
}

// GetPaymentIntentByToken retrieves a payment intent by token
// UPDATED: Now returns PaymentIntent instead of CheckoutSession per clean architecture
func (s *TicketService) GetPaymentIntentByToken(token string) (*models.PaymentIntent, error) {
	var paymentIntent models.PaymentIntent
	if err := s.db.Where("checkout_token = ?", token).Preload("Event").Preload("GuestUser").Preload("User").First(&paymentIntent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewBusinessLogicError("Payment intent not found.")
		}
		return nil, err
	}

	// Check if expired
	if paymentIntent.ExpiresAt != nil && paymentIntent.ExpiresAt.Before(time.Now()) {
		return nil, utils.NewBusinessLogicError("Payment intent expired.")
	}

	return &paymentIntent, nil
}

// sendUserTicketConfirmationEmails sends a single ticket confirmation email for logged-in user purchases with secure JWT links
func (s *TicketService) sendUserTicketConfirmationEmails(tickets []*models.Ticket) {
	if len(tickets) == 0 {
		return
	}

	// Get user email from the first ticket (all tickets should have the same user)
	var user models.User
	if err := s.db.Where("id = ?", tickets[0].UserID).First(&user).Error; err != nil {
		log.Printf("Failed to get user for ticket confirmation email: %v", err)
		return
	}

	// Get event details for the email
	var event models.Event
	if err := s.db.Preload("Organizer").Preload("Organizer.OrganizerOnboarding").First(&event, tickets[0].EventID).Error; err != nil {
		log.Printf("Failed to get event for ticket confirmation email: %v", err)
		return
	}

	// Generate ticket view URLs for each ticket
	var ticketData []map[string]interface{}

	for _, ticketPtr := range tickets {
		ticket := *ticketPtr // Dereference the pointer
		// Generate secure view URL using centralized helper
		viewURL, err := s.generateTicketViewURL(&ticket)
		if err != nil {
			log.Printf("Failed to generate ticket view URL for ticket %s: %v", ticket.ID, err)
			continue
		}

		// Prepare ticket data for email template
		ticketData = append(ticketData, map[string]interface{}{
			"ticket_number": ticket.TicketNumber,
			"view_url":      viewURL,
		})
	}

	// Generate calendar data
	calendarEvent := utils.ICalendarEvent{
		UID:         event.ID.String(),
		Summary:     event.Title,
		Description: utils.FormatEventDescription(event.Title, tickets[0].TicketNumber, "", len(tickets)),
		Location:    fmt.Sprintf("%s, %s", event.VenueName, event.Address),
		StartTime:   event.StartDate,
		EndTime:     event.EndDate,
		Organizer:   getOrganizerDisplayName(event.Organizer),
		URL:         fmt.Sprintf("%s/events/%s", s.getBaseURL(), event.ID),
	}

	icsContent := utils.GenerateICS(calendarEvent)
	icsDataURL := utils.GenerateAddToCalendarURL(icsContent)
	googleCalURL := utils.GenerateGoogleCalendarURL(calendarEvent)
	calendarFilename := utils.GetCalendarFilename(event.Title)

	// Prepare email data
	emailData := map[string]interface{}{
		"user_name":           user.FirstName + " " + user.LastName,
		"user_email":          user.Email,
		"event_name":          event.Title,
		"event_date":          event.StartDate.Format("January 2, 2006"),
		"event_time":          event.StartDate.Format("3:04 PM"),
		"venue":               event.VenueName,
		"organizer_name":      getOrganizerDisplayName(event.Organizer),
		"tickets":             ticketData,
		"total_tickets":       len(tickets),
		"total_amount":        tickets[0].TotalAmount * float64(len(tickets)), // Calculate total
		"payment_gateway":     string(tickets[0].PaymentGateway),
		"base_url":            s.getBaseURL(),
		"calendar_ics_url":    icsDataURL,
		"google_calendar_url": googleCalURL,
		"calendar_filename":   calendarFilename,
		"year":                time.Now().Year(),
	}

	// NOTE: Email is already queued by payment_worker async processing
	// Do NOT queue again here to avoid duplicate emails
	// The payment_worker calls emailOutboxService.QueueEmail() with the new data structure
	_ = emailData // Make the variable used to pass linting
}

// RecordTransaction creates a transaction record for successful ticket purchases
// getBaseURL returns the base URL for the application from config
func (s *TicketService) getBaseURL() string {
	if s.cfg != nil {
		return s.cfg.URLs.FrontendBaseURL
	}
	return "http://localhost:3000"
}

// getPaymentSuccessURL returns the payment success URL from config
func (s *TicketService) getPaymentSuccessURL() string {
	if s.cfg != nil {
		return s.cfg.Payment.SuccessURL
	}
	return s.getBaseURL() + "/payment/success"
}

// getPaymentFailedURL returns the payment failed URL from config
func (s *TicketService) getPaymentFailedURL() string {
	if s.cfg != nil {
		return s.cfg.Payment.FailedURL
	}
	return s.getBaseURL() + "/payment/failed"
}

// getPaymentCancelURL returns the payment cancel URL from config
func (s *TicketService) getPaymentCancelURL() string {
	if s.cfg != nil {
		return s.cfg.Payment.CancelURL
	}
	return s.getBaseURL() + "/payment/cancel"
}

// generateTicketViewURL generates a secure JWT-based view URL for a ticket
// Format: {base_url}/tickets/view?token={jwt_token}
// This is a centralized function to ensure consistency across guest and user flows
func (s *TicketService) generateTicketViewURL(ticket *models.Ticket) (string, error) {
	jwtService := utils.NewJWTService(s.jwtConfig)
	token, err := jwtService.GenerateTicketAccessToken(ticket)
	if err != nil {
		return "", fmt.Errorf("failed to generate JWT token: %w", err)
	}
	return fmt.Sprintf("%s/tickets/view?token=%s", s.getBaseURL(), token), nil
}

// AdminProcessCheckoutSession manually processes a checkout session for admin (bypasses expiry check)
func (s *TicketService) AdminProcessCheckoutSession(checkoutToken string, adminID uuid.UUID) error {
	return s.ProcessSuccessfulPayment(checkoutToken)
}

func extractUUIDsFromValues(values []string) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		parsedID, err := uuid.Parse(strings.TrimSpace(value))
		if err == nil {
			ids = append(ids, parsedID)
		}
	}
	return ids
}

func parseTicketIDsFromGatewayData(raw interface{}) []uuid.UUID {
	switch value := raw.(type) {
	case []uuid.UUID:
		return value
	case []string:
		return extractUUIDsFromValues(value)
	case []interface{}:
		ids := make([]uuid.UUID, 0, len(value))
		for _, item := range value {
			switch typedItem := item.(type) {
			case uuid.UUID:
				ids = append(ids, typedItem)
			case string:
				parsedID, err := uuid.Parse(strings.TrimSpace(typedItem))
				if err == nil {
					ids = append(ids, parsedID)
				}
			}
		}
		return ids
	case string:
		trimmed := strings.TrimSpace(value)
		if parsedID, err := uuid.Parse(trimmed); err == nil {
			return []uuid.UUID{parsedID}
		}

		var asStrings []string
		if err := json.Unmarshal([]byte(trimmed), &asStrings); err == nil {
			return extractUUIDsFromValues(asStrings)
		}
	case []byte:
		var asStrings []string
		if err := json.Unmarshal(value, &asStrings); err == nil {
			return extractUUIDsFromValues(asStrings)
		}

		var asInterfaces []interface{}
		if err := json.Unmarshal(value, &asInterfaces); err == nil {
			return parseTicketIDsFromGatewayData(asInterfaces)
		}
	}

	return nil
}

// REMOVED: getCheckoutSessionTicketIDs - CheckoutSession removed per clean architecture
// Tickets are now linked to Transactions, not stored in gateway data

// mergeGatewayData safely merges new gateway data with existing data, preserving ticket_ids and other critical information
func mergeGatewayData(existing map[string]interface{}, updates map[string]interface{}) map[string]interface{} {
	if existing == nil {
		existing = make(map[string]interface{})
	}

	// Deep copy existing data to avoid modifying the original
	result := make(map[string]interface{})
	for k, v := range existing {
		result[k] = v
	}

	// Apply updates
	for k, v := range updates {
		result[k] = v
	}

	return result
}

// initializeGatewayData initializes payment gateway specific data for both user types
// UNIFIED METHOD: Handles both logged-in users and guests with single implementation
func (s *TicketService) initializeGatewayData(checkoutSession *models.PaymentIntent, req *models.GuestPurchaseRequest, ticket *models.Ticket, guestUser *models.GuestUser, userID *uuid.UUID) error {
}

// ProcessSuccessfulPayment processes a successful payment from Stripe webhook
func (s *TicketService) ProcessSuccessfulPayment(checkoutToken string) error {
	// Add nil checks at the beginning
	if s == nil {
		return fmt.Errorf("ticket service is nil")
	}
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}
	if checkoutToken == "" {
		return fmt.Errorf("checkout token is empty")
	}

	tx := s.db.Begin()
	if tx == nil {
		return fmt.Errorf("failed to begin database transaction")
	}
	if tx == nil {
		return fmt.Errorf("failed to begin transaction")
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[TICKET_SERVICE] PANIC in ProcessSuccessfulPayment: %v", r)
			if tx != nil {
				tx.Rollback()
			}
		}
	}()

	// Find payment intent with this token
	var paymentIntent models.PaymentIntent
	if err := tx.Where("checkout_token = ?", checkoutToken).First(&paymentIntent).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to find payment intent: %w", err)
	}

	// Check if already processed (idempotency)
	if paymentIntent.Status == "succeeded" {
		log.Printf("[TICKET_SERVICE] Payment intent %s already processed, skipping", checkoutToken)
		tx.Rollback() // Nothing to do
		return nil
	}

	// Validate payment intent data
	if paymentIntent.ID == uuid.Nil {
		tx.Rollback()
		return fmt.Errorf("payment intent ID is nil")
	}
	paymentIntent.UpdatedAt = time.Now()
	if err := tx.Save(&paymentIntent).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update payment intent: %w", err)
	}

	// Collect all tickets - either from ticket_ids in gateway data or from the single ticket
	var allTickets []*models.Ticket
	ticketIDMap := make(map[uuid.UUID]bool)

	// For PaymentIntent, we need to find tickets by checkout_token or user/event
	// Since tickets are created before payment, they should have the checkout_token
	var tickets []models.Ticket
	if err := tx.Where("checkout_token = ? AND status = ?", checkoutToken, "pending_payment").Find(&tickets).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to find tickets for checkout token: %w", err)
	}

	if len(tickets) == 0 {
		tx.Rollback()
		return utils.NewBusinessLogicError("No tickets found for payment intent")
	}

	for i := range tickets {
		allTickets = append(allTickets, &tickets[i])
		ticketIDMap[tickets[i].ID] = true
	}

	// Update all tickets to active status
	for _, ticket := range allTickets {
		if err := tx.Model(ticket).Update("status", "active").Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update ticket status: %w", err)
		}
	}

	// Record transaction for successful payment gateway purchase (inside transaction for ACID guarantees)
	// Extract gateway transaction ID - consolidated extraction logic
	gatewayTxnID, _ := s.transactionService.extractGatewayIDs(paymentIntent.GatewayResponse)

	if err := s.transactionService.recordTransactionInTx(tx, allTickets, paymentIntent.PaymentGateway, gatewayTxnID, paymentIntent.GatewayResponse, "completed", &paymentIntent.ID); err != nil {
		tx.Rollback()
		if _, ok := err.(*utils.AppError); ok {
			return err
		}
		return fmt.Errorf("failed to record transaction: %w", err)
	}

	// Mark payment intent as completed
	now := time.Now()
	paymentIntent.Status = "succeeded"
	paymentIntent.SucceededAt = &now
	paymentIntent.UpdatedAt = now
	if err := tx.Save(&paymentIntent).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to mark payment intent as completed: %w", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Send confirmation emails
	if s.emailQueueService != nil {
		// Group tickets by user type for email sending
		userTickets := make(map[*models.User][]*models.Ticket)
		guestEmails := make(map[string][]*models.Ticket)

		for _, ticket := range allTickets {
			// Reload ticket with associations
			if err := s.db.Preload("User").Preload("GuestUser").First(ticket, ticket.ID).Error; err != nil {
				continue // Skip if ticket not found
			}

			if ticket.User != nil {
				userTickets[ticket.User] = append(userTickets[ticket.User], ticket)
			} else if ticket.GuestUser != nil {
				guestEmails[ticket.GuestUser.Email] = append(guestEmails[ticket.GuestUser.Email], ticket)
			}
		}

		// Send emails
		for user, tickets := range userTickets {
			if err := s.emailQueueService.QueueUserTicketConfirmationEmail(user, tickets); err != nil {
				log.Printf("Failed to queue confirmation email for user %s: %v", user.Email, err)
			}
		}

		for email, tickets := range guestEmails {
			if err := s.emailQueueService.QueueGuestTicketConfirmationEmail(email, tickets); err != nil {
				log.Printf("Failed to queue confirmation email for guest %s: %v", email, err)
			}
		}
	}

	return nil
}

// ProcessFailedPayment processes a failed payment from Stripe webhook
func (s *TicketService) ProcessFailedPayment(checkoutToken string) error {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// First, try to release reservations (for guest purchases)
	if s.reservationService != nil {
		if err := s.reservationService.ReleaseReservation(context.Background(), checkoutToken); err != nil {
			// If no reservations found, that's OK - might be logged-in user purchase
			if !strings.Contains(err.Error(), "no reservations found") {
				tx.Rollback()
				return fmt.Errorf("failed to release reservation: %w", err)
			}
		}
	}

	// Find payment intent
	var paymentIntent models.PaymentIntent
	if err := tx.Where("checkout_token = ?", checkoutToken).First(&paymentIntent).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to find payment intent: %w", err)
	}

	// Update payment intent status
	now := time.Now()
	paymentIntent.Status = "failed"
	paymentIntent.FailedAt = &now
	paymentIntent.UpdatedAt = now
	if err := tx.Save(&paymentIntent).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update payment intent: %w", err)
	}

	// Check if there are tickets to cancel (for logged-in user purchases)
	// For PaymentIntent, find tickets by checkout_token
	var tickets []models.Ticket
	if err := tx.Where("checkout_token = ? AND status = ?", checkoutToken, "pending_payment").Preload("Tier").Find(&tickets).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to load tickets for inventory restoration: %w", err)
	}

	if len(tickets) > 0 {
		// Group tickets by tier for inventory restoration
		tierQuantities := make(map[uuid.UUID]int)
		for _, ticket := range tickets {
			tierQuantities[ticket.TierID] += 1
		}

		// Restore inventory for failed tickets
		for tierID, quantity := range tierQuantities {
			if err := tx.Model(&models.EventTier{}).
				Where("id = ?", tierID).
				Updates(map[string]interface{}{
					"available": gorm.Expr("available + ?", quantity),
					"sold":      gorm.Expr("sold - ?", quantity),
				}).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to restore inventory for tier %s: %w", tierID, err)
			}
		}

		// Update tickets status to cancelled
		for _, ticket := range tickets {
			if err := tx.Model(ticket).Update("status", "cancelled").Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to update ticket status: %w", err)
			}
		}
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ReleasePaymentIntentReservations releases reserved tickets when user abandons checkout
// Called when user navigates away from payment page (immediate release, not waiting for TTL)
func (s *TicketService) ReleasePaymentIntentReservations(checkoutToken string) error {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Find payment intent
	var paymentIntent models.PaymentIntent
	if err := tx.Where("checkout_token = ?", checkoutToken).First(&paymentIntent).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewNotFoundError("payment intent")
		}
		return fmt.Errorf("failed to find payment intent: %w", err)
	}

	// Check if already cancelled or completed
	if paymentIntent.Status == "succeeded" {
		tx.Rollback()
		return utils.NewBusinessLogicError("Cannot release reservations for completed payments")
	}

	if paymentIntent.Status == "canceled" {
		tx.Rollback()
		return nil // Already cancelled, idempotent
	}

	// Update payment intent status to cancelled
	now := time.Now()
	paymentIntent.Status = "canceled"
	paymentIntent.CanceledAt = &now
	paymentIntent.UpdatedAt = now
	if err := tx.Save(&paymentIntent).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update payment intent status: %w", err)
	}

	// Release reservations if this is a guest/reservation-based purchase
	if s.reservationService != nil {
		if err := s.reservationService.ReleaseReservation(context.Background(), checkoutToken); err != nil {
			// If no reservations found, that's OK - might be logged-in user purchase
			if !strings.Contains(err.Error(), "no reservations found") {
				tx.Rollback()
				return fmt.Errorf("failed to release reservation: %w", err)
			}
		}
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ProcessCanceledPayment processes a canceled payment from Stripe webhook
func (s *TicketService) ProcessCanceledPayment(checkoutToken string) error {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Find payment intent with this token
	var paymentIntent models.PaymentIntent
	if err := tx.Where("checkout_token = ?", checkoutToken).First(&paymentIntent).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to find payment intent: %w", err)
	}

	// Update payment intent status
	now := time.Now()
	paymentIntent.Status = "canceled"
	paymentIntent.CanceledAt = &now
	paymentIntent.UpdatedAt = now
	if err := tx.Save(&paymentIntent).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update payment intent: %w", err)
	}

	// Collect all tickets for this payment intent
	var tickets []models.Ticket
	if err := tx.Where("checkout_token = ? AND status = ?", checkoutToken, "pending_payment").Find(&tickets).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to find tickets: %w", err)
	}

	if len(tickets) == 0 {
		tx.Rollback()
		return utils.NewBusinessLogicError("No tickets found for payment intent")
	}

	// Group tickets by tier for inventory restoration
	tierQuantities := make(map[uuid.UUID]int)
	for _, ticket := range tickets {
		if ticket.Status == "pending_payment" { // Only restore inventory for tickets that were never paid
			tierQuantities[ticket.TierID] += 1
		}
	}

	// Restore inventory for cancelled tickets
	for tierID, quantity := range tierQuantities {
		if err := tx.Model(&models.EventTier{}).
			Where("id = ?", tierID).
			Updates(map[string]interface{}{
				"available": gorm.Expr("available + ?", quantity),
				"sold":      gorm.Expr("sold - ?", quantity),
			}).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to restore inventory for tier %s: %w", tierID, err)
		}
	}

	// Update tickets status to cancelled
	for _, ticket := range tickets {
		if err := tx.Model(ticket).Update("status", "cancelled").Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update ticket status: %w", err)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ProcessRefundedPayment processes a refunded payment from Stripe webhook
// This cancels tickets and marks the transaction as refunded
// logAudit creates audit log entries for ticket operations
func (s *TicketService) logAudit(ctx context.Context, action, entityType string, entityID uuid.UUID, actorID *uuid.UUID, actorType string, eventID *uuid.UUID, changes map[string]interface{}) {
	audit := &models.PaymentAuditLog{
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		ActorID:    actorID,
		ActorType:  actorType,
		EventID:    eventID,
		Timestamp:  time.Now(),
	}

	if changes != nil {
		audit.ChangesAfter = changes
	}

	// Log async to avoid blocking
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[TICKET_SERVICE] Panic in async audit logging: %v", r)
			}
		}()
		s.db.Create(audit)
	}()
}
