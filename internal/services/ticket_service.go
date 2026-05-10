package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/currency"
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
	db                 *gorm.DB
	financialService   *FinancialService
	emailQueueService  *EmailQueueService
	emailOutboxService *EmailOutboxService
	authService        *AuthService
	jwtConfig          *config.JWTConfig
	cfg                *config.Config
	secureQRService    *SecureQRService
	emailService       *EmailService
	reservationService *ReservationService
	transactionService *TransactionService
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
		TransactionStatus: string(transaction.Status),
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

func ticketCheckInStatusMessage(status models.TicketStatus) string {
	switch string(status) {
	case "refunded", "partially_refunded":
		return "This ticket is refunded."
	case "cancelled", "canceled":
		return "This ticket is cancelled."
	case "expired":
		return "This ticket is expired."
	case "pending_refund":
		return "This ticket is pending refund."
	case string(models.TicketUsed):
		return "This ticket is already used."
	default:
		return "This ticket is not valid for check-in."
	}
}

func isSameEventLocalDay(first, second time.Time, timezone string) bool {
	loc := time.UTC
	if timezone != "" {
		if parsedLoc, err := time.LoadLocation(timezone); err == nil {
			loc = parsedLoc
		}
	}

	firstInLoc := first.In(loc)
	secondInLoc := second.In(loc)
	return firstInLoc.Year() == secondInLoc.Year() &&
		firstInLoc.Month() == secondInLoc.Month() &&
		firstInLoc.Day() == secondInLoc.Day()
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

		// Verify payment was successful
		if ticket.Transaction.Status != models.TransactionSucceeded {
			tx.Rollback()
			return utils.NewBusinessLogicError("Payment is not completed for this ticket.")
		}

		// Check if ticket is active
		if ticket.Status != models.TicketActive {
			tx.Rollback()
			return utils.NewBusinessLogicError(ticketCheckInStatusMessage(ticket.Status))
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
		if ticket.CheckedInAt != nil && !isMultiDayEvent {
			tx.Rollback()
			return utils.NewBusinessLogicError("Ticket already checked in.")
		}

		// For multi-day events, allow re-check-in on different days
		if ticket.CheckedInAt != nil && isMultiDayEvent {
			// Check if already checked in today
			if isSameEventLocalDay(*ticket.CheckedInAt, time.Now(), ticket.Event.Timezone) {
				tx.Rollback()
				return utils.NewBusinessLogicError("Ticket already checked in today.")
			}
		}

		// Set check-in time and staff atomically
		checkInTime := time.Now()
		ticket.CheckedInAt = &checkInTime
		ticket.CheckedInBy = &staffID

		if err := tx.Save(&ticket).Error; err != nil {
			tx.Rollback()
			return err
		}

		// Commit transaction
		return tx.Commit().Error
	}, 3, 50*time.Millisecond)
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
		result["message"] = err.Error()
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
	if ticket.Status != models.TicketActive {
		result["message"] = ticketCheckInStatusMessage(ticket.Status)
		return result, nil
	}

	// Check if already checked in
	isMultiDayEvent := ticket.Event.EndDate.After(ticket.Event.StartDate.Add(24 * time.Hour))
	if ticket.CheckedInAt != nil && !isMultiDayEvent {
		result["message"] = "Ticket already checked in"
		result["can_checkin"] = false
	} else if ticket.CheckedInAt != nil && isMultiDayEvent {
		// For multi-day events, check if already checked in today
		if isSameEventLocalDay(*ticket.CheckedInAt, time.Now(), ticket.Event.Timezone) {
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
		"checked_in":    ticket.CheckedInAt != nil,
	}

	//load user details with actorid and type
	if ticket.ActorType == "user" {
		var user models.User
		if err := s.db.First(&user, ticket.ActorID).Error; err != nil {
			result["message"] = "User not found"
			return result, nil
		}
		ticketInfo["attendee"] = map[string]interface{}{
			"name":  user.FirstName + " " + user.LastName,
			"email": user.Email,
			"type":  "user",
		}
	} else if ticket.ActorType == "guest" {
		var guest models.GuestUser
		if err := s.db.First(&guest, ticket.ActorID).Error; err != nil {
			result["message"] = "Guest user not found"
			return result, nil
		}
		ticketInfo["attendee"] = map[string]interface{}{
			"name":  guest.FirstName + " " + guest.LastName,
			"email": guest.Email,
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
	if ticket.Status != models.TicketActive {
		result["message"] = ticketCheckInStatusMessage(ticket.Status)
		return result, nil
	}

	// Check if already checked in
	isMultiDayEvent := ticket.Event.EndDate.After(ticket.Event.StartDate.Add(24 * time.Hour))
	if ticket.CheckedInAt != nil && !isMultiDayEvent {
		result["message"] = "Ticket already checked in"
		result["can_checkin"] = false
	} else if ticket.CheckedInAt != nil && isMultiDayEvent {
		// For multi-day events, check if already checked in today
		if isSameEventLocalDay(*ticket.CheckedInAt, time.Now(), ticket.Event.Timezone) {
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
		"checked_in":    ticket.CheckedInAt != nil,
		"tier_name":     ticket.Tier.TierName,
		"purchase_date": ticket.CreatedAt,
	}

	//load user details with actorid and type
	if ticket.ActorType == "user" {
		var user models.User
		if err := s.db.First(&user, ticket.ActorID).Error; err != nil {
			result["message"] = "User not found"
			return result, nil
		}
		ticketInfo["attendee"] = map[string]interface{}{
			"name":  user.FirstName + " " + user.LastName,
			"email": user.Email,
			"type":  "user",
		}
	} else if ticket.ActorType == "guest" {
		var guest models.GuestUser
		if err := s.db.First(&guest, ticket.ActorID).Error; err != nil {
			result["message"] = "Guest user not found"
			return result, nil
		}
		ticketInfo["attendee"] = map[string]interface{}{
			"name":  guest.FirstName + " " + guest.LastName,
			"email": guest.Email,
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
			"checked_in":    ticket.CheckedInAt != nil,
		}

		// Add attendee info
		if ticket.ActorType == models.ActorUser {
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

// GetEventTicketsWithFilters gets event tickets with search, filter, and sorting capabilities
func (s *TicketService) GetEventTicketsWithFilters(
	eventID uuid.UUID,
	organizerID uuid.UUID,
	search,
	status string,
	tierID *uuid.UUID,
	checkinStatus string,
	page,
	limit int,
	sortBy,
	sortOrder string,
) ([]models.OrganizerTicketResponse, int64, error) {

	var total int64
	offset := (page - 1) * limit

	// Verify organizer owns event
	var event models.Event

	if err := s.db.
		Where("id = ? AND organizer_id = ?", eventID, organizerID).
		First(&event).Error; err != nil {

		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0,
				utils.NewBusinessLogicError(
					"Event not found or access denied.",
				)
		}

		return nil, 0, err
	}

	// Base query
	baseQuery := s.db.Model(&models.Ticket{}).
		Joins("LEFT JOIN event_tiers et ON tickets.tier_id = et.id").
		Joins("LEFT JOIN users u ON tickets.actor_id = u.id AND tickets.actor_type = ?", models.ActorUser).
		Joins("LEFT JOIN guest_users gu ON tickets.actor_id = gu.id AND tickets.actor_type = ?", models.ActorGuest).
		Joins("LEFT JOIN users staff ON tickets.checked_in_by = staff.id").
		Where("tickets.event_id = ?", eventID)

	// Status filter
	if status != "" {
		baseQuery = baseQuery.Where("tickets.status = ?", status)
	}

	// Tier filter
	if tierID != nil {
		baseQuery = baseQuery.Where("tickets.tier_id = ?", *tierID)
	}

	// Check-in filter
	switch checkinStatus {
	case "checked_in":
		baseQuery = baseQuery.Where("tickets.check_in_time IS NOT NULL")

	case "not_checked_in":
		baseQuery = baseQuery.Where("tickets.check_in_time IS NULL")
	}

	// Search filter
	if search != "" {

		searchTerm := "%" + strings.ToLower(search) + "%"

		baseQuery = baseQuery.Where(`
			LOWER(tickets.ticket_number) LIKE ?
			OR LOWER(COALESCE(
				u.first_name || ' ' || u.last_name,
				gu.first_name || ' ' || gu.last_name
			)) LIKE ?
			OR LOWER(COALESCE(u.email, gu.email)) LIKE ?
		`,
			searchTerm,
			searchTerm,
			searchTerm,
		)
	}

	// Count
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Sorting
	var orderClause string

	switch sortBy {

	case "tier":
		orderClause = "LOWER(et.tier_name) " + sortOrder

	case "check_in_time":

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
		orderClause = `
			LOWER(
				COALESCE(
					u.first_name || ' ' || u.last_name,
					gu.first_name || ' ' || gu.last_name
				)
			) ` + sortOrder

	case "ticket_number":
		orderClause = "LOWER(tickets.ticket_number) " + sortOrder

	case "status":
		orderClause = "LOWER(tickets.status) " + sortOrder

	default:
		orderClause = "tickets.created_at DESC"
	}

	// Fetch tickets
	var tickets []models.Ticket

	if err := baseQuery.
		Preload("Tier").
		Preload("PaymentIntent").
		Preload("Transaction").
		Preload("CheckInByUser").
		Order(orderClause).
		Offset(offset).
		Limit(limit).
		Find(&tickets).Error; err != nil {

		return nil, 0, err
	}

	// Collect checked-in staff IDs
	staffIDs := make(map[uuid.UUID]bool)

	for _, ticket := range tickets {
		if ticket.CheckedInBy != nil {
			staffIDs[*ticket.CheckedInBy] = true
		}

	}

	// Fetch staff users
	staffMap := make(map[uuid.UUID]string)

	if len(staffIDs) > 0 {

		var ids []uuid.UUID

		for id := range staffIDs {
			ids = append(ids, id)
		}

		var staffUsers []models.User

		if err := s.db.
			Where("id IN ?", ids).
			Find(&staffUsers).Error; err == nil {

			for _, staff := range staffUsers {

				fullName := staff.FirstName

				if staff.LastName != "" {
					fullName += " " + staff.LastName
				}

				staffMap[staff.ID] = fullName
			}
		}
	}

	// Collect attendee IDs
	userIDs := make(map[uuid.UUID]bool)
	guestIDs := make(map[uuid.UUID]bool)

	for _, ticket := range tickets {

		if ticket.ActorType == models.ActorUser {
			userIDs[ticket.ActorID] = true
		}

		if ticket.ActorType == models.ActorGuest {
			guestIDs[ticket.ActorID] = true
		}
	}

	// Fetch users
	userMap := make(map[uuid.UUID]models.User)

	if len(userIDs) > 0 {

		var ids []uuid.UUID

		for id := range userIDs {
			ids = append(ids, id)
		}

		var users []models.User

		if err := s.db.
			Where("id IN ?", ids).
			Find(&users).Error; err == nil {

			for _, u := range users {
				userMap[u.ID] = u
			}
		}
	}

	// Fetch guests
	guestMap := make(map[uuid.UUID]models.GuestUser)

	if len(guestIDs) > 0 {

		var ids []uuid.UUID

		for id := range guestIDs {
			ids = append(ids, id)
		}

		var guests []models.GuestUser

		if err := s.db.
			Where("id IN ?", ids).
			Find(&guests).Error; err == nil {

			for _, g := range guests {
				guestMap[g.ID] = g
			}
		}
	}

	// Build response
	responses := make([]models.OrganizerTicketResponse, len(tickets))

	for i, ticket := range tickets {

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
			TotalAmount:     float64(ticket.UnitPrice),
			PaymentGateway:  "",
			Status:          string(ticket.Status),
			IsGuestPurchase: ticket.ActorType == models.ActorGuest,
			CheckInTime:     ticket.CheckedInAt,
			CheckedInByName: "",
			CreatedAt:       ticket.CreatedAt,
			UpdatedAt:       ticket.UpdatedAt,
		}

		if ticket.PaymentIntent != nil {
			response.PaymentGateway = ticket.PaymentIntent.PaymentGateway
		} else if ticket.Transaction != nil {
			response.PaymentGateway = ticket.Transaction.PaymentGateway
		}

		if ticket.CheckInByUser != nil {
			response.CheckedInByName = strings.TrimSpace(ticket.CheckInByUser.FirstName + " " + ticket.CheckInByUser.LastName)
		}

		// Attendee
		if ticket.ActorType == models.ActorUser {

			if u, ok := userMap[ticket.ActorID]; ok {

				response.Attendee = &models.AttendeeResponse{
					ID:    &u.ID,
					Name:  u.FirstName + " " + u.LastName,
					Email: u.Email,
					Type:  "user",
				}
			}
		}

		if ticket.ActorType == models.ActorGuest {

			if g, ok := guestMap[ticket.ActorID]; ok {

				response.Attendee = &models.AttendeeResponse{
					ID:    &g.ID,
					Name:  g.FirstName + " " + g.LastName,
					Email: g.Email,
					Type:  "guest",
				}
			}
		}

		// Checked in by
		if ticket.CheckedInBy != nil {

			if name, ok := staffMap[*ticket.CheckedInBy]; ok {
				response.CheckedInByName = name
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
		ActiveTickets    int64 `json:"active_tickets"`
		CancelledTickets int64 `json:"cancelled_tickets"`
	}

	// Get ticket counts by status from the Ticket table
	s.db.Model(&models.Ticket{}).
		Where("event_id = ?", eventID).
		Select("COUNT(*) as total_tickets, SUM(CASE WHEN check_in_time IS NOT NULL THEN 1 ELSE 0 END) as checked_in, SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END) as active_tickets, SUM(CASE WHEN status IN ('cancelled', 'pending_refund', 'refunded', 'expired') THEN 1 ELSE 0 END) as cancelled_tickets").
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

// ProcessRefundedPayment processes a refunded payment from Stripe webhook
// This cancels tickets and marks the transaction as refunded
// logAudit creates audit log entries for ticket operations
func (s *TicketService) logAudit(ctx context.Context, action, entityType string, entityID uuid.UUID, actorID *uuid.UUID, actorType string, eventID *uuid.UUID, changes map[string]interface{}) {
	LogPaymentAuditAsync(s.db, action, entityType, entityID, actorID, actorType, eventID, changes)
}

// CreateTicketsAfterPayment is called ONLY from webhook
func (s *TicketService) CreateTicketsAfterPayment(
	tx *gorm.DB,
	intent *models.PaymentIntent,
	event models.Event,
	transactionID uuid.UUID,
) error {

	var reservations []models.TicketReservation

	if err := tx.
		Where("payment_intent_id = ? AND status = ?",
			intent.ID,
			models.ReservationReserved,
		).
		Find(&reservations).Error; err != nil {
		return err
	}

	if len(reservations) == 0 {
		return fmt.Errorf("no reservations found")
	}

	for _, r := range reservations {

		var tier models.EventTier

		if err := tx.
			Where("id = ?", r.TierID).
			First(&tier).Error; err != nil {
			return err
		}

		unitPrice, err := currency.ToSmallestUnit(
			tier.Price,
			intent.Currency,
		)
		if err != nil {
			return err
		}

		for i := 0; i < r.Quantity; i++ {

			ticketNumber, err := utils.GenerateEventTicketNumber(
				tx,
				tier.TierName,
				event.StartDate.Year(),
			)
			if err != nil {
				return err
			}

			now := time.Now()

			ticket := models.Ticket{
				ID:              uuid.New(),
				TicketNumber:    ticketNumber,
				ActorID:         intent.ActorID,
				ActorType:       intent.ActorType,
				EventID:         intent.EventID,
				TierID:          r.TierID,
				CheckoutToken:   intent.CheckoutToken,
				PaymentIntentID: &intent.ID,
				TransactionID:   transactionID,
				UnitPrice:       unitPrice,
				Currency:        intent.Currency,
				Status:          models.TicketActive,
				PaidAt:          &now,
				CreatedAt:       now,
				UpdatedAt:       now,
			}

			if err := tx.Create(&ticket).Error; err != nil {
				return err
			}

			if err := LogPaymentAuditTx(
				tx,
				"ticket_created",
				"ticket",
				ticket.ID,
				&intent.ActorID,
				string(intent.ActorType),
				&intent.EventID,
				map[string]interface{}{
					"ticket_number":    ticket.TicketNumber,
					"transaction_id":   transactionID,
					"payment_intent":   intent.ID,
					"tier_id":          ticket.TierID,
					"status":           ticket.Status,
					"checkout_token":   ticket.CheckoutToken,
					"currency":         ticket.Currency,
					"unit_price_cents": ticket.UnitPrice,
				},
			); err != nil {
				return err
			}
		}

		// reservation consumed
		if err := tx.Model(&r).
			Updates(map[string]interface{}{
				"status":       models.ReservationConfirmed,
				"confirmed_at": time.Now(),
			}).Error; err != nil {
			return err
		}

		// move reserved -> sold
		if err := tx.Model(&models.EventTier{}).
			Where("id = ?", r.TierID).
			Updates(map[string]interface{}{
				"reserved": gorm.Expr("reserved - ?", r.Quantity),
				"sold":     gorm.Expr("sold + ?", r.Quantity),
			}).Error; err != nil {
			return err
		}
	}

	return nil
}
