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
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v74"
	"github.com/stripe/stripe-go/v74/checkout/session"
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

// GetEmailQueueService returns the email queue service
func (s *TicketService) GetEmailQueueService() *EmailQueueService {
	return s.emailQueueService
}

// GetDB returns the database connection
func (s *TicketService) GetDB() *gorm.DB {
	return s.db
}

// PurchaseTicket creates multiple individual ticket purchases for a logged-in user
func (s *TicketService) PurchaseTicket(userID uuid.UUID, req *models.TicketPurchaseRequest) ([]*models.Ticket, error) {
	// Validate total quantity across all tiers doesn't exceed limits
	totalQuantity := 0
	for _, tierSelection := range req.Tiers {
		totalQuantity += tierSelection.Quantity
	}
	if totalQuantity > 10 {
		return nil, utils.NewBusinessLogicError("Total tickets cannot exceed 10 per purchase")
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
	return utils.WithRetryFunc(func() ([]*models.Ticket, error) {
		// Start transaction with timeout
		tx := s.db.Begin()
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
				panic(r)
			}
		}()

		// Get event details with lock for update
		var event models.Event
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&event, req.EventID).Error
		if err != nil {
			tx.Rollback()
			return nil, err
		}

		var allTickets []*models.Ticket
		totalAmount := 0.0

		// Process each tier selection
		for _, tierSelection := range req.Tiers {
			// Load the selected tier for price/name/availability (lock row for update)
			var tier models.EventTier
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ? AND event_id = ?", tierSelection.TierID, req.EventID).
				First(&tier).Error; err != nil {
				tx.Rollback()
				return nil, err
			}

			// Check availability at tier level
			if tier.Available < tierSelection.Quantity {
				tx.Rollback()
				return nil, fmt.Errorf("Insufficient tickets available for tier %s", tier.TierName)
			}

			// Create individual tickets for each quantity in this tier
			for i := 0; i < tierSelection.Quantity; i++ {
				// Generate sequential ticket number using utility function
				ticketNum, err := utils.GenerateEventTicketNumber(tx, tier.TierName, event.StartDate.Year())
				if err != nil {
					tx.Rollback()
					return nil, fmt.Errorf("failed to generate ticket number: %w", err)
				}

				// Create ticket (one per person) using tier data
				ticket := &models.Ticket{
					TicketNumber:   ticketNum,
					UserID:         &userID,
					EventID:        req.EventID,
					TierID:         tier.ID,
					TotalAmount:    tier.Price,
					PaymentGateway: req.PaymentGateway,
					Status:         "active",
				}

				if err := tx.Create(ticket).Error; err != nil {
					tx.Rollback()
					return nil, err
				}

				allTickets = append(allTickets, ticket)
				totalAmount += tier.Price
			}

			// Update tier availability/sold atomically
			if err := tx.Model(&tier).
				Where("id = ? AND available >= ?", tier.ID, tierSelection.Quantity).
				Updates(map[string]interface{}{
					"available": gorm.Expr("available - ?", tierSelection.Quantity),
					"sold":      gorm.Expr("sold + ?", tierSelection.Quantity),
				}).Error; err != nil {
				tx.Rollback()
				return nil, err
			}
		}

		// Update event availability atomically
		if err := tx.Model(&event).
			Where("id = ? AND available >= ?", event.ID, totalQuantity).
			Update("available", gorm.Expr("available - ?", totalQuantity)).Error; err != nil {
			tx.Rollback()
			return nil, err
		}

		// Record transaction for successful user purchase (inside transaction for ACID guarantees)
		if err := s.recordTransactionInTx(tx, allTickets, req.PaymentGateway, "", nil, "completed", nil); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("Failed to record transaction: %w", err)
		}

		// Load associations for response BEFORE committing (within transaction)
		for _, ticket := range allTickets {
			if err := tx.Preload("User").Preload("Event").Preload("Tier").First(ticket, ticket.ID).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("Failed to load ticket associations: %w", err)
			}
		}

		// Commit transaction
		if err := tx.Commit().Error; err != nil {
			return nil, err
		}

		// Send ticket confirmation emails with PDFs asynchronously for each ticket
		go s.sendUserTicketConfirmationEmails(allTickets)

		return allTickets, nil
	})
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
	if err := s.db.Where("id = ? AND user_id = ? AND status = 'completed'", transactionID, userID).
		Preload("Event").
		First(&transaction).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, utils.NewNotFoundError("transaction")
		}
		return nil, err
	}

	// Get all tickets for this transaction
	var tickets []models.Ticket
	if err := s.db.Where("transaction_id = ?", transactionID).
		Preload("Event").
		Preload("Tier").
		Find(&tickets).Error; err != nil {
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

	// Add tickets with QR data
	for _, ticket := range tickets {
		qrData, err := s.GenerateQRCodeForTicket(ticket.ID)
		if err != nil {
			// Fallback to ticket number if QR generation fails
			qrData = ticket.TicketNumber
		}

		isCheckedIn := ticket.CheckInTime != nil
		ticketResp := models.UserTransactionTicketResponse{
			ID:           ticket.ID,
			TicketNumber: ticket.TicketNumber,
			Status:       ticket.Status,
			Tier: models.UserTicketListingTierResponse{
				ID:   ticket.Tier.ID,
				Name: ticket.Tier.TierName,
			},
			QRData:       qrData,
			CheckInTime:  ticket.CheckInTime,
			CheckOutTime: ticket.CheckOutTime,
			CheckedInBy:  ticket.CheckedInBy,
			CheckedOutBy: ticket.CheckedOutBy,
			IsCheckedIn:  isCheckedIn,
		}
		response.Tickets = append(response.Tickets, ticketResp)
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
		Select("COUNT(*) as total_tickets, SUM(CASE WHEN check_in_time IS NOT NULL THEN 1 ELSE 0 END) as checked_in, SUM(CASE WHEN check_out_time IS NOT NULL THEN 1 ELSE 0 END) as checked_out, SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END) as active_tickets, SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END) as cancelled_tickets").
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
			SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END) as cancelled_tickets,
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
		if err := s.recordTransactionInTx(tx, allTickets, req.PaymentGateway, "", nil, "completed", nil); err != nil {
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
		guestUser.Phone = req.Phone
		guestUser.CountryCode = req.CountryCode
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
		Email:       req.Email,
		FirstName:   req.FirstName,
		LastName:    req.LastName,
		Phone:       req.Phone,
		CountryCode: req.CountryCode,
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

// UnifiedGuestPurchase handles both immediate (cash) and deferred (gateway) guest ticket purchases
// with identical post-processing for emails, transactions, and JWT tokens
func (s *TicketService) UnifiedGuestPurchase(req *models.GuestPurchaseRequest) ([]*models.Ticket, *models.GuestUser, *models.CheckoutSession, error) {
	// For cash payments, create tickets immediately and mark as active
	if req.PaymentGateway == models.PaymentGatewayCash {
		return s.handleCashGuestPurchase(req)
	}

	// For gateway payments, use the existing InitiatePaymentGatewayPurchase
	checkoutSession, tickets, guestUser, err := s.InitiatePaymentGatewayPurchase(req)
	return tickets, guestUser, checkoutSession, err
}

// handleCashGuestPurchase handles immediate cash payments for guest purchases
func (s *TicketService) handleCashGuestPurchase(req *models.GuestPurchaseRequest) ([]*models.Ticket, *models.GuestUser, *models.CheckoutSession, error) {
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
	return utils.WithRetryFunc3(func() ([]*models.Ticket, *models.GuestUser, *models.CheckoutSession, error) {
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
			PaymentGateway:   models.PaymentGatewayCash,
			Amount:           totalAmount,
			Currency:         currency,
			Quantity:         totalQuantity,
			Status:           "completed",
			GatewayTxnID:     "",
			GatewayData:      map[string]interface{}{"payment_method": "cash"},
			CommissionRate:   0, // TODO: Get from config
			CommissionAmount: 0,
			OrganizerShare:   totalAmount,
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
			"payment_gateway": "cash",
			"total_amount":    totalAmount,
			"total_tickets":   totalQuantity,
			"payment_method":  "cash",
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

// ConvertGuestToUser converts a guest purchase to a registered user account
func (s *TicketService) ConvertGuestToUser(guestEmail string, userID uuid.UUID) error {
	// Start transaction
	tx := s.db.Begin()

	// Find guest user
	var guestUser models.GuestUser
	if err := tx.Where("email = ?", guestEmail).First(&guestUser).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewBusinessLogicError("Guest user not found.")
		}
		return err
	}

	// Check if already converted
	if guestUser.ConvertedToUser {
		tx.Rollback()
		return utils.NewBusinessLogicError("Guest user already converted to registered user.")
	}

	// Update all tickets to point to the registered user
	if err := tx.Model(&models.Ticket{}).
		Where("guest_user_id = ?", guestUser.ID).
		Updates(map[string]interface{}{
			"user_id":           userID,
			"guest_user_id":     nil,
			"is_guest_purchase": false,
		}).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Mark guest user as converted
	guestUser.ConvertedToUser = true
	guestUser.ConvertedUserID = &userID

	if err := tx.Save(&guestUser).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Commit transaction
	return tx.Commit().Error
}

// getRecipientEmail returns the appropriate email address for sending ticket confirmation
func (s *TicketService) getRecipientEmail(ticket *models.Ticket) string {
	if ticket.User != nil && ticket.User.Email != "" {
		return ticket.User.Email
	}
	if ticket.GuestUser != nil && ticket.GuestUser.Email != "" {
		return ticket.GuestUser.Email
	}
	return "" // This shouldn't happen, but fallback
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

// InitiateUserPaymentGatewayPurchase creates multiple ticket purchases with payment gateway integration for logged-in users
func (s *TicketService) InitiateUserPaymentGatewayPurchase(userID uuid.UUID, req *models.TicketPurchaseRequest) (*models.CheckoutSession, []*models.Ticket, error) {
	// Get user details for unified request
	var user models.User
	if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, nil, err
	}

	// Convert to unified purchase request
	unifiedReq := &UnifiedPurchaseRequest{
		UserID:         &userID,
		Email:          user.Email,
		FirstName:      user.FirstName,
		LastName:       user.LastName,
		Phone:          user.Phone,
		CountryCode:    user.CountryCode,
		EventID:        req.EventID,
		Tiers:          req.Tiers,
		PaymentGateway: req.PaymentGateway,
	}

	// Use unified purchase orchestrator
	ctx := context.Background()
	response, err := s.unifiedPurchaseOrchestrator.ProcessUnifiedPurchase(ctx, unifiedReq)
	if err != nil {
		return nil, nil, err
	}

	// Load checkout session for return
	var checkoutSession models.CheckoutSession
	if err := s.db.Where("checkout_token = ?", response.CheckoutToken).First(&checkoutSession).Error; err != nil {
		return nil, nil, err
	}

	// Return empty tickets array - actual tickets will be created on webhook
	return &checkoutSession, []*models.Ticket{}, nil
}

// InitiatePaymentGatewayPurchase creates multiple ticket purchases with payment gateway integration
// UNIFIED METHOD: Uses the unified purchase orchestrator for seamless guest purchases
func (s *TicketService) InitiatePaymentGatewayPurchase(req *models.GuestPurchaseRequest) (*models.CheckoutSession, []*models.Ticket, *models.GuestUser, error) {
	// Convert to unified purchase request
	unifiedReq := &UnifiedPurchaseRequest{
		Email:          req.Email,
		FirstName:      req.FirstName,
		LastName:       req.LastName,
		Phone:          req.Phone,
		CountryCode:    req.CountryCode,
		EventID:        req.EventID,
		Tiers:          req.Tiers,
		PaymentGateway: req.PaymentGateway,
	}

	// Use unified purchase orchestrator
	ctx := context.Background()
	response, err := s.unifiedPurchaseOrchestrator.ProcessUnifiedPurchase(ctx, unifiedReq)
	if err != nil {
		return nil, nil, nil, err
	}

	// Load checkout session for return
	var checkoutSession models.CheckoutSession
	if err := s.db.Where("checkout_token = ?", response.CheckoutToken).First(&checkoutSession).Error; err != nil {
		return nil, nil, nil, err
	}

	// Load guest user for return
	var guestUser *models.GuestUser
	if response.GuestUserID != nil {
		if err := s.db.Where("id = ?", response.GuestUserID).First(&guestUser).Error; err != nil {
			return nil, nil, nil, err
		}
	}

	// Return empty tickets array - actual tickets will be created on webhook
	return &checkoutSession, []*models.Ticket{}, guestUser, nil
}

// generateSecureToken generates a cryptographically secure token for checkout sessions
func (s *TicketService) generateSecureToken() string {
	// Generate a UUID and add some randomness
	token := uuid.New().String()
	// Add timestamp for additional uniqueness
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("%s_%d", token, timestamp)
}

// initializeGatewayData initializes payment gateway specific data for both user types
// UNIFIED METHOD: Handles both logged-in users and guests with single implementation
func (s *TicketService) initializeGatewayData(checkoutSession *models.CheckoutSession, req *models.GuestPurchaseRequest, ticket *models.Ticket, guestUser *models.GuestUser, userID *uuid.UUID) error {
	// Calculate total quantity across all tiers
	totalQuantity := 0
	for _, tierSelection := range req.Tiers {
		totalQuantity += tierSelection.Quantity
	}

	// Determine customer email and metadata based on user type
	var customerEmail string
	var metadata map[string]string

	if userID != nil {
		// Logged-in user: get email from user record
		var user models.User
		if err := s.db.Where("id = ?", userID).First(&user).Error; err != nil {
			return fmt.Errorf("failed to get user details: %w", err)
		}
		customerEmail = user.Email
		metadata = map[string]string{
			"checkout_token": checkoutSession.CheckoutToken,
			"user_id":        userID.String(),
			"event_id":       ticket.EventID.String(),
		}
	} else {
		// Guest user: use email from guest user record
		customerEmail = guestUser.Email
		metadata = map[string]string{
			"checkout_token": checkoutSession.CheckoutToken,
			"guest_user_id":  guestUser.ID.String(),
			"event_id":       ticket.EventID.String(),
		}
	}

	switch checkoutSession.PaymentGateway {
	case models.PaymentGatewayStripe:
		// Set Stripe API key from config
		stripe.Key = s.cfg.Payment.Gateways.StripeAPIKey

		// Create line items for Stripe checkout
		lineItems := []*stripe.CheckoutSessionLineItemParams{}
		for _, tierSelection := range req.Tiers {
			// Get tier details
			var tier models.EventTier
			if err := s.db.Where("id = ?", tierSelection.TierID).First(&tier).Error; err != nil {
				return fmt.Errorf("failed to get tier details: %w", err)
			}

			lineItem := &stripe.CheckoutSessionLineItemParams{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String(string(tier.Currency)),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name:        stripe.String(fmt.Sprintf("Tickets for %s - %s", ticket.Event.Title, tier.TierName)),
						Description: stripe.String(fmt.Sprintf("%d x %s tickets", tierSelection.Quantity, tier.TierName)),
					},
					UnitAmount: stripe.Int64(int64(tier.Price * 100)), // Convert to cents
				},
				Quantity: stripe.Int64(int64(tierSelection.Quantity)),
			}
			lineItems = append(lineItems, lineItem)
		}

		// Create Stripe checkout session
		params := &stripe.CheckoutSessionParams{
			LineItems:     lineItems,
			Mode:          stripe.String(string(stripe.CheckoutSessionModePayment)),
			SuccessURL:    stripe.String(fmt.Sprintf("%s?checkout_token=%s", s.getPaymentSuccessURL(), checkoutSession.CheckoutToken)),
			CancelURL:     stripe.String(fmt.Sprintf("%s?checkout_token=%s", s.getPaymentCancelURL(), checkoutSession.CheckoutToken)),
			Currency:      stripe.String(string(checkoutSession.Currency)),
			CustomerEmail: stripe.String(customerEmail),
		}

		// Add metadata to the checkout session
		params.AddMetadata("checkout_token", checkoutSession.CheckoutToken)

		params.PaymentIntentData = &stripe.CheckoutSessionPaymentIntentDataParams{
			Metadata: metadata,
		}

		stripeSession, err := session.New(params)
		if err != nil {
			return fmt.Errorf("failed to create Stripe checkout session: %w", err)
		}

		// Safely extract payment_intent_id (nil until first payment attempt)
		paymentIntentID := ""
		if stripeSession.PaymentIntent != nil {
			paymentIntentID = stripeSession.PaymentIntent.ID
		}

		// Update checkout session with Stripe data
		// Merge with existing gateway data to preserve ticket_ids
		updates := map[string]interface{}{
			"session_id":        stripeSession.ID,
			"payment_intent_id": paymentIntentID,
			"url":               stripeSession.URL,
			"success_url":       fmt.Sprintf("%s?checkout_token=%s", s.getPaymentSuccessURL(), checkoutSession.CheckoutToken),
			"cancel_url":        fmt.Sprintf("%s?checkout_token=%s", s.getPaymentCancelURL(), checkoutSession.CheckoutToken),
		}
		checkoutSession.GatewayData = mergeGatewayData(checkoutSession.GatewayData, updates)

		// Set the Stripe session ID for webhook lookup
		checkoutSession.StripeSessionID = stripeSession.ID

	default:
		return fmt.Errorf("unsupported payment gateway: %s", checkoutSession.PaymentGateway)
	}

	return nil
}

// ProcessPaymentSuccess processes a successful payment callback
func (s *TicketService) ProcessPaymentSuccess(req *models.PaymentCallbackRequest) error {
	// ⚠️  IMPORTANT: This is browser callback ONLY - No tickets or transactions created here
	// All creation happens in webhook processor (payment_worker)
	// This function just acknowledges browser callback for better UX

	tx := s.db.Begin()

	// Find checkout session with LOCK to prevent concurrent processing
	var checkoutSession models.CheckoutSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("checkout_token = ?", req.CheckoutToken).
		First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return utils.NewBusinessLogicError("Checkout session not found.")
	}

	// If already marked as "processing" or "completed", this is a retry or webhook already processed
	if checkoutSession.Status == "processing" {
		tx.Rollback()
		log.Printf("[BROWSER_CALLBACK_IDEMPOTENT] Checkout already processing: %s", checkoutSession.CheckoutToken)
		return nil // Browser callback is informational, webhook will finalize
	}

	if checkoutSession.Status == "completed" {
		tx.Rollback()
		log.Printf("[BROWSER_CALLBACK_ALREADY_FINALIZED] Payment already finalized: %s", checkoutSession.CheckoutToken)
		return nil // Already finalized by webhook
	}

	// Check if expired
	if checkoutSession.ExpiresAt.Before(time.Now()) {
		tx.Rollback()
		return utils.NewBusinessLogicError("Checkout session expired.")
	}

	// ========================================
	// Mark checkout session as "processing"
	// Do NOT create tickets or transactions yet
	// ========================================
	checkoutSession.Status = "processing"
	if req.GatewayData != nil {
		checkoutSession.GatewayData = req.GatewayData
	}

	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit().Error; err != nil {
		return err
	}

	log.Printf("[BROWSER_CALLBACK_SUCCESS] Payment callback acknowledged: checkout=%s, status=processing", checkoutSession.CheckoutToken)
	log.Printf("[BROWSER_CALLBACK_INFORMATIONAL] Ticket creation will occur in webhook processor (payment_worker)")

	// Return success to browser - webhook will finalize everything
	return nil
}

// sendPaymentSuccessEmails sends a single ticket confirmation email for guest purchases with secure JWT links
func (s *TicketService) sendPaymentSuccessEmails(checkoutSession models.CheckoutSession, tickets []models.Ticket) {
	if len(tickets) == 0 {
		return
	}

	// Get guest user email
	var guestUser models.GuestUser
	if err := s.db.First(&guestUser, checkoutSession.GuestUserID).Error; err != nil {
		log.Printf("Failed to get guest user for payment success email: %v", err)
		return
	}

	// Get event details for the email
	var event models.Event
	if err := s.db.Preload("Organizer").Preload("Organizer.OrganizerOnboarding").First(&event, tickets[0].EventID).Error; err != nil {
		log.Printf("Failed to get event for ticket confirmation email: %v", err)
		return
	}

	// Generate JWT tokens for each ticket
	var ticketData []map[string]interface{}

	for _, ticket := range tickets {
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
		"guest_name":          guestUser.FirstName + " " + guestUser.LastName,
		"guest_email":         guestUser.Email,
		"event_name":          event.Title,
		"event_date":          event.StartDate.Format("January 2, 2006"),
		"event_time":          event.StartDate.Format("3:04 PM"),
		"venue":               event.VenueName,
		"organizer_name":      getOrganizerDisplayName(event.Organizer),
		"tickets":             ticketData,
		"total_tickets":       len(tickets),
		"total_amount":        checkoutSession.Amount,
		"payment_gateway":     string(checkoutSession.PaymentGateway),
		"base_url":            s.getBaseURL(),
		"calendar_ics_url":    icsDataURL,
		"google_calendar_url": googleCalURL,
		"calendar_filename":   calendarFilename,
		"year":                time.Now().Year(),
	}

	// NOTE: Email is already queued by payment_worker async processing
	// Do NOT queue again here to avoid duplicate emails
	// The payment_worker calls emailOutboxService.QueueEmail() with the new data structure
	// ProcessPaymentSuccess is called by the synchronous success callback handler only as fallback
	_ = emailData // Make the variable used to pass linting
}

// ProcessPaymentFailure processes a failed payment callback
func (s *TicketService) ProcessPaymentFailure(req *models.PaymentCallbackRequest) error {
	// Start transaction
	tx := s.db.Begin()

	// Find checkout session
	var checkoutSession models.CheckoutSession
	if err := tx.Where("checkout_token = ?", req.CheckoutToken).First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return utils.NewBusinessLogicError("Checkout session not found.")
	}

	// Update checkout session
	checkoutSession.Status = "failed"
	checkoutSession.GatewayData = req.GatewayData
	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Find all tickets associated with this checkout session
	var tickets []models.Ticket
	query := tx.Where("status = ?", "pending_payment")

	if checkoutSession.UserID != nil {
		query = query.Where("user_id = ?", *checkoutSession.UserID)
	} else if checkoutSession.GuestUserID != nil {
		query = query.Where("guest_user_id = ?", *checkoutSession.GuestUserID)
	}

	// Get tickets for this event
	if err := query.Where("event_id = (SELECT event_id FROM tickets WHERE id = ?)", checkoutSession.TicketID).Find(&tickets).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Update all tickets status to cancelled
	totalQuantity := 0
	for _, ticket := range tickets {
		ticket.Status = "cancelled"
		if err := tx.Save(&ticket).Error; err != nil {
			tx.Rollback()
			return err
		}

		totalQuantity += 1 // Each ticket is for 1 person
	}

	// Record failed transaction
	// Convert []models.Ticket to []*models.Ticket for RecordTransaction
	ticketPtrs := make([]*models.Ticket, len(tickets))
	for i := range tickets {
		ticketPtrs[i] = &tickets[i]
	}

	// Record failed transaction (only for logged-in users who can retry)
	// Removed: No transaction recording for failed/cancelled payments

	// Restore event availability
	if len(tickets) > 0 {
		var event models.Event
		if err := tx.Where("id = ?", tickets[0].EventID).First(&event).Error; err != nil {
			tx.Rollback()
			return err
		}

		event.Available += totalQuantity
		if err := tx.Save(&event).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return err
	}

	return nil
}

// GetCheckoutSessions retrieves checkout sessions with filters (admin only)
func (s *TicketService) GetCheckoutSessions(status, paymentGateway string, eventID *uuid.UUID, page, limit int, sortBy, sortOrder string) ([]*models.CheckoutSession, int64, error) {
	var sessions []*models.CheckoutSession
	var total int64

	query := s.db.Model(&models.CheckoutSession{}).Preload("Ticket").Preload("GuestUser").Preload("User")

	if status != "" {
		query = query.Where("status = ?", status)
	}
	if paymentGateway != "" {
		query = query.Where("payment_gateway = ?", paymentGateway)
	}
	if eventID != nil {
		query = query.Joins("JOIN tickets ON checkout_sessions.ticket_id = tickets.id").
			Where("tickets.event_id = ?", *eventID)
	}

	query.Count(&total)

	offset := (page - 1) * limit
	orderClause := sortBy + " " + sortOrder
	if err := query.Order(orderClause).
		Offset(offset).
		Limit(limit).
		Find(&sessions).Error; err != nil {
		return nil, 0, err
	}

	return sessions, total, nil
}

// GetCheckoutSessionByToken retrieves a checkout session by token
func (s *TicketService) GetCheckoutSessionByToken(token string) (*models.CheckoutSession, error) {
	var checkoutSession models.CheckoutSession
	if err := s.db.Where("checkout_token = ?", token).Preload("Ticket").Preload("GuestUser").Preload("User").First(&checkoutSession).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewBusinessLogicError("Checkout session not found.")
		}
		return nil, err
	}

	// Check if expired
	if checkoutSession.ExpiresAt.Before(time.Now()) {
		return nil, utils.NewBusinessLogicError("Checkout session expired.")
	}

	return &checkoutSession, nil
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
func (s *TicketService) RecordTransaction(tickets []*models.Ticket, paymentGateway models.PaymentGateway, gatewayTxnID string, gatewayData map[string]interface{}, paymentIntentID *uuid.UUID) error {
	return s.recordTransactionInTx(s.db, tickets, paymentGateway, gatewayTxnID, gatewayData, "completed", paymentIntentID)
}

// extractGatewayIDs extracts gateway transaction ID and payment intent ID from gateway data
// This consolidates the duplicate extraction logic used in multiple payment paths
func (s *TicketService) extractGatewayIDs(gatewayData map[string]interface{}) (string, *uuid.UUID) {
	gatewayTxnID := ""
	var paymentIntentID *uuid.UUID

	if gatewayData != nil {
		// Extract transaction ID
		if txnID, ok := gatewayData["payment_intent_id"].(string); ok {
			gatewayTxnID = txnID
		} else if txnID, ok := gatewayData["txn_id"].(string); ok {
			gatewayTxnID = txnID
		}

		// Extract payment intent ID
		if piID, ok := gatewayData["payment_intent_id"].(string); ok && piID != "" {
			if parsedID, err := uuid.Parse(piID); err == nil {
				paymentIntentID = &parsedID
			}
		}
	}

	return gatewayTxnID, paymentIntentID
}

// recordTransactionInTx is an internal helper that allows recording transactions within an existing transaction
func (s *TicketService) recordTransactionInTx(db *gorm.DB, tickets []*models.Ticket, paymentGateway models.PaymentGateway, gatewayTxnID string, gatewayData map[string]interface{}, status string, paymentIntentID *uuid.UUID) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}
	if len(tickets) == 0 {
		return utils.NewBusinessLogicError("No tickets provided for transaction recording.")
	}
	if tickets[0] == nil {
		return fmt.Errorf("first ticket is nil")
	}

	// Get event details for commission calculation
	var event models.Event
	if err := db.First(&event, tickets[0].EventID).Error; err != nil {
		return fmt.Errorf("failed to get event details: %w", err)
	}

	// Get tier details for currency
	var tier models.EventTier
	if err := db.First(&tier, tickets[0].TierID).Error; err != nil {
		return fmt.Errorf("failed to get tier details: %w", err)
	}

	// Calculate total amount
	totalAmount := 0.0
	for _, ticket := range tickets {
		totalAmount += ticket.TotalAmount
	}

	// Calculate commission
	commissionAmount := totalAmount * (event.CommissionRate / 100)
	organizerShare := totalAmount - commissionAmount

	// Create transaction record (without specific tier_id for multi-tier purchases)
	transaction := &models.Transaction{
		EventID:          tickets[0].EventID,
		TierID:           nil, // Don't set specific tier for multi-ticket purchases
		UserID:           tickets[0].UserID,
		GuestUserID:      tickets[0].GuestUserID,
		PaymentIntentID:  paymentIntentID,
		PaymentGateway:   paymentGateway,
		Amount:           totalAmount,
		Currency:         tier.Currency,
		Quantity:         len(tickets),
		Status:           status,
		GatewayTxnID:     gatewayTxnID,
		GatewayData:      gatewayData,
		CommissionRate:   event.CommissionRate,
		CommissionAmount: commissionAmount,
		OrganizerShare:   organizerShare,
	}

	// Create transaction record
	if err := db.Create(transaction).Error; err != nil {
		return fmt.Errorf("failed to create transaction record: %w", err)
	}

	// Log audit for transaction creation
	s.logAudit(context.Background(), "transaction_created", "transaction", transaction.ID, nil, "system", &transaction.EventID, map[string]interface{}{
		"amount":            transaction.Amount,
		"currency":          transaction.Currency,
		"quantity":          transaction.Quantity,
		"payment_gateway":   transaction.PaymentGateway,
		"gateway_txn_id":    transaction.GatewayTxnID,
		"commission_rate":   transaction.CommissionRate,
		"commission_amount": transaction.CommissionAmount,
		"organizer_share":   transaction.OrganizerShare,
		"status":            transaction.Status,
	})

	// Update all tickets with the transaction ID (establishes the relationship)
	// Use individual updates to ensure transaction context is maintained
	for _, ticket := range tickets {
		ticket.TransactionID = &transaction.ID
		result := db.Model(&models.Ticket{}).Where("id = ?", ticket.ID).Update("transaction_id", transaction.ID)
		if result.Error != nil {
			return fmt.Errorf("failed to update ticket %s with transaction_id: %w", ticket.ID.String(), result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("no rows updated for ticket %s (ticket may not exist)", ticket.ID.String())
		}
		log.Printf("[TRANSACTION_LINKING_DETAIL] Updated ticket %s with transaction_id %s (rows affected: %d)", ticket.ID.String(), transaction.ID.String(), result.RowsAffected)
	}
	log.Printf("[TRANSACTION_LINKING] Linked %d tickets to transaction %s", len(tickets), transaction.ID.String())

	log.Printf("Transaction recorded: ID=%s, Amount=%.2f, Gateway=%s, Tickets=%d",
		transaction.ID.String(), totalAmount, paymentGateway, len(tickets))

	return nil
}

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

// RequestRefund creates a refund request for tickets
func (ts *TicketService) RequestRefund(userID *uuid.UUID, guestUserID *uuid.UUID, req models.RefundRequest) (*models.RefundResponse, error) {
	// Validate that user owns the tickets
	var tickets []models.Ticket
	query := ts.db.Preload("Event").Preload("Tier").Preload("Transaction")

	if userID != nil {
		query = query.Where("user_id = ? AND id IN ?", *userID, req.TicketIDs)
	} else if guestUserID != nil {
		query = query.Where("guest_user_id = ? AND id IN ?", *guestUserID, req.TicketIDs)
	} else {
		return nil, utils.NewValidationError("Either user ID or guest user ID must be provided", nil)
	}

	if err := query.Find(&tickets).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to find tickets", err)
	}

	if len(tickets) == 0 {
		return nil, utils.NewNotFoundError("No valid tickets found for refund")
	}

	// Check that all tickets belong to the same transaction
	transactionID := tickets[0].TransactionID
	for _, ticket := range tickets {
		if ticket.TransactionID != transactionID {
			return nil, utils.NewValidationError("All tickets must belong to the same transaction", nil)
		}
	}

	// Check refund eligibility
	eligible, reason, err := ts.CheckRefundEligibility(req.TicketIDs)
	if err != nil {
		return nil, err
	}
	if !eligible {
		return nil, utils.NewValidationError("Refund not eligible: "+reason, nil)
	}

	// Calculate refund amount (sum of ticket amounts)
	var refundAmount float64
	for _, ticket := range tickets {
		refundAmount += ticket.TotalAmount
	}

	// Create refund request record
	refundRequest := &models.RefundRequest{
		TransactionID: *transactionID,
		UserID:        userID,
		GuestUserID:   guestUserID,
		TicketIDs:     req.TicketIDs,
		RefundAmount:  refundAmount,
		Currency:      tickets[0].Transaction.Currency,
		Status:        "pending",
		Reason:        req.Reason,
	}

	if err := ts.db.Create(refundRequest).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to create refund request", err)
	}

	// Get event title for response
	eventTitle := ""
	if len(tickets) > 0 && tickets[0].Event != nil {
		eventTitle = tickets[0].Event.Title
	}

	response := &models.RefundResponse{
		ID:            refundRequest.ID,
		TransactionID: refundRequest.TransactionID,
		EventTitle:    eventTitle,
		RefundAmount:  refundRequest.RefundAmount,
		Currency:      refundRequest.Currency,
		Status:        refundRequest.Status,
		Reason:        refundRequest.Reason,
		CreatedAt:     refundRequest.CreatedAt,
		UpdatedAt:     refundRequest.UpdatedAt,
	}

	return response, nil
}

// GetUserRefunds returns paginated list of user's refund requests
func (ts *TicketService) GetUserRefunds(userID *uuid.UUID, guestUserID *uuid.UUID, page, limit int) ([]models.RefundResponse, int64, error) {
	var refundRequests []models.RefundRequest
	var total int64

	query := ts.db.Model(&models.RefundRequest{}).
		Preload("Transaction.Event")

	if userID != nil {
		query = query.Where("user_id = ?", *userID)
	} else if guestUserID != nil {
		query = query.Where("guest_user_id = ?", *guestUserID)
	} else {
		return nil, 0, utils.NewValidationError("Either user ID or guest user ID must be provided", nil)
	}

	// Count total records
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to count refund requests", err)
	}

	// Get paginated results
	offset := (page - 1) * limit
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&refundRequests).Error; err != nil {
		return nil, 0, utils.NewDatabaseError("Failed to get refund requests", err)
	}

	// Convert to response format
	responses := make([]models.RefundResponse, 0, len(refundRequests))
	for _, refundRequest := range refundRequests {
		eventTitle := ""
		if refundRequest.Transaction != nil && refundRequest.Transaction.Event != nil {
			eventTitle = refundRequest.Transaction.Event.Title
		}

		response := models.RefundResponse{
			ID:            refundRequest.ID,
			TransactionID: refundRequest.TransactionID,
			EventTitle:    eventTitle,
			RefundAmount:  refundRequest.RefundAmount,
			Currency:      refundRequest.Currency,
			Status:        refundRequest.Status,
			Reason:        refundRequest.Reason,
			CreatedAt:     refundRequest.CreatedAt,
			UpdatedAt:     refundRequest.UpdatedAt,
		}
		responses = append(responses, response)
	}

	return responses, total, nil
}

// ProcessRefund processes a refund request (admin only)
func (ts *TicketService) ProcessRefund(refundRequestID uuid.UUID, adminID uuid.UUID, approve bool, adminNotes string) error {
	var refundRequest models.RefundRequest
	if err := ts.db.Preload("Transaction").Preload("Transaction.Tickets").First(&refundRequest, refundRequestID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return utils.NewNotFoundError("Refund request not found")
		}
		return utils.NewDatabaseError("Failed to find refund request", err)
	}

	if refundRequest.Status != "pending" {
		return utils.NewValidationError("Refund request has already been processed", nil)
	}

	now := time.Now()
	refundRequest.UpdatedAt = now

	if approve {
		// Get payment intent ID from transaction
		var transaction models.Transaction
		if err := ts.db.Select("payment_intent_id, payment_gateway").First(&transaction, refundRequest.TransactionID).Error; err != nil {
			return utils.NewDatabaseError("Failed to find transaction", err)
		}

		// Verify payment intent exists (transaction must have it)
		if transaction.PaymentIntentID == nil {
			return utils.NewBusinessLogicError("Transaction has no payment intent linked. Payment may not have completed.")
		}

		// Approve refund request - create actual refund record for gateway processing
		refund := &models.Refund{
			TransactionID:   refundRequest.TransactionID,
			PaymentIntentID: *transaction.PaymentIntentID,       // Use directly from transaction
			PaymentGateway:  string(transaction.PaymentGateway), // Use from transaction
			GatewayRefundID: "",                                 // Will be set after gateway processing
			Amount:          refundRequest.RefundAmount,
			Currency:        refundRequest.Currency,
			Reason:          refundRequest.Reason,
			RefundType:      "customer_request",
			Status:          "processing",
			AffectedTicketIDs: func() []string {
				ids := make([]string, len(refundRequest.TicketIDs))
				for i, id := range refundRequest.TicketIDs {
					ids[i] = id.String()
				}
				return ids
			}(),
			TicketCount: len(refundRequest.TicketIDs),
			InitiatedBy: refundRequest.UserID, // User who requested refund
			ApprovedBy:  &adminID,
			Notes:       adminNotes,
			RequestedAt: &refundRequest.CreatedAt,
			ApprovedAt:  &now,
		}

		if err := ts.db.Create(refund).Error; err != nil {
			return utils.NewDatabaseError("Failed to create refund record", err)
		}

		// Update refund request status
		refundRequest.Status = "approved"

		// TODO: Trigger actual gateway refund processing asynchronously
		// For now, we'll simulate completion
		refund.Status = "completed"
		refund.GatewayRefundID = string(refundRequest.Transaction.PaymentGateway) // Use gateway name
		refund.ProcessedAt = &now

		// Update ticket statuses to refunded
		if err := ts.db.Model(&models.Ticket{}).Where("id IN ?", refundRequest.TicketIDs).
			Updates(map[string]interface{}{
				"status":     "refunded",
				"updated_at": now,
			}).Error; err != nil {
			return utils.NewDatabaseError("Failed to update ticket statuses", err)
		}

		// Update transaction status if all tickets are refunded
		var totalTickets int64
		var refundedTickets int64
		ts.db.Model(&models.Ticket{}).Where("transaction_id = ?", refundRequest.TransactionID).Count(&totalTickets)
		ts.db.Model(&models.Ticket{}).Where("transaction_id = ? AND status = 'refunded'", refundRequest.TransactionID).Count(&refundedTickets)

		if totalTickets == refundedTickets {
			if err := ts.db.Model(&models.Transaction{}).Where("id = ?", refundRequest.TransactionID).
				Update("status", "refunded").Error; err != nil {
				return utils.NewDatabaseError("Failed to update transaction status", err)
			}

			// Log audit for transaction status update
			ts.logAudit(context.Background(), "transaction_refunded", "transaction", refundRequest.TransactionID, nil, "system", nil, map[string]interface{}{
				"status": "refunded",
			})
		}

		// Save the updated refund record
		if err := ts.db.Save(refund).Error; err != nil {
			return utils.NewDatabaseError("Failed to update refund status", err)
		}

	} else {
		// Reject refund request
		refundRequest.Status = "rejected"
	}

	return ts.db.Save(&refundRequest).Error
}

// CheckRefundEligibility checks if tickets are eligible for refund
// Returns eligibility status and reason if not eligible
func (s *TicketService) CheckRefundEligibility(ticketIDs []uuid.UUID) (bool, string, error) {
	var tickets []models.Ticket
	if err := s.db.Where("id IN ?", ticketIDs).
		Preload("Event").
		Preload("Transaction").
		Find(&tickets).Error; err != nil {
		return false, "", fmt.Errorf("failed to fetch tickets: %w", err)
	}

	if len(tickets) != len(ticketIDs) {
		return false, "Some tickets not found", nil
	}

	// Check each ticket for refund eligibility
	for _, ticket := range tickets {
		// 1. Check ticket status
		if ticket.Status == "refunded" {
			return false, fmt.Sprintf("Ticket %s has already been refunded", ticket.TicketNumber), nil
		}
		if ticket.Status == "cancelled" {
			return false, fmt.Sprintf("Ticket %s is already cancelled", ticket.TicketNumber), nil
		}
		if ticket.Status == "used" || ticket.CheckInTime != nil {
			return false, fmt.Sprintf("Ticket %s has been checked in and cannot be refunded", ticket.TicketNumber), nil
		}

		// 2. Check event status
		if ticket.Event == nil {
			return false, "Event information not available", nil
		}
		if ticket.Event.IsCancelled || ticket.Event.Status == "cancelled" {
			return false, fmt.Sprintf("Cannot refund tickets for cancelled event: %s", ticket.Event.Title), nil
		}
		if ticket.Event.Status == "completed" {
			return false, fmt.Sprintf("Cannot refund tickets for completed event: %s", ticket.Event.Title), nil
		}

		// 3. Check event timing - no refunds within 24 hours of event start
		now := time.Now()
		timeUntilEvent := ticket.Event.StartDate.Sub(now)
		if timeUntilEvent < 24*time.Hour {
			return false, fmt.Sprintf("Refunds not allowed within 24 hours of event start. Event starts at: %s",
				ticket.Event.StartDate.Format("2006-01-02 15:04:05")), nil
		}

		// 4. Check purchase timing - no refunds within 1 hour of purchase
		timeSincePurchase := now.Sub(ticket.CreatedAt)
		if timeSincePurchase < 1*time.Hour {
			return false, fmt.Sprintf("Refunds not allowed within 1 hour of purchase. Purchase time: %s",
				ticket.CreatedAt.Format("2006-01-02 15:04:05")), nil
		}

		// 5. Check event sales status
		if ticket.Event.SalesStatus == "stopped" {
			return false, fmt.Sprintf("Ticket sales have been stopped for event: %s", ticket.Event.Title), nil
		}
	}

	return true, "", nil
}

// CancelTicketWithRefund handles ticket cancellation and creates a refund request
// Returns a map with refund status information
func (s *TicketService) CancelTicketWithRefund(ticketID uuid.UUID, userID uuid.UUID, reason string) (map[string]interface{}, error) {
	// Get ticket with related data
	var ticket models.Ticket
	if err := s.db.Where("id = ?", ticketID).
		Preload("Event").
		Preload("Transaction").
		Preload("Tier").
		Find(&ticket).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch ticket: %w", err)
	}

	// Begin transaction
	tx := s.db.Begin()

	// 1. Mark ticket as cancelled
	now := time.Now()
	if err := tx.Model(&ticket).Updates(map[string]interface{}{
		"status":     "cancelled",
		"updated_at": now,
	}).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to cancel ticket: %w", err)
	}

	// 2. Restore tier inventory
	if err := tx.Model(&models.EventTier{}).
		Where("id = ?", ticket.TierID).
		Update("available", gorm.Expr("available + ?", 1)).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to restore tier inventory: %w", err)
	}

	// 3. Create refund request
	refundNumber := fmt.Sprintf("RF-%s-%d", ticket.TicketNumber, time.Now().Unix())
	affectedTicketIDs := []string{ticketID.String()}
	refund := models.Refund{
		RefundNumber:      refundNumber,
		TransactionID:     *ticket.TransactionID,
		PaymentIntentID:   *ticket.TransactionID, // Using TransactionID as PaymentIntentID
		PaymentGateway:    string(ticket.PaymentGateway),
		GatewayRefundID:   fmt.Sprintf("LOCAL-%d", time.Now().Unix()),
		Amount:            ticket.TotalAmount,
		Currency:          ticket.Event.Currency,
		Reason:            reason,
		RefundType:        "customer_request",
		Status:            "pending",
		AffectedTicketIDs: affectedTicketIDs,
		TicketCount:       1,
		InitiatedBy:       &userID,
		RequestedAt:       &now,
	}

	if err := tx.Create(&refund).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create refund: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Send refund created notification email
	go func() {
		if s.emailQueueService != nil {
			// Get user email for notification
			var userEmail string
			var userName string
			if userID != uuid.Nil {
				var user models.User
				if err := s.db.Where("id = ?", userID).First(&user).Error; err == nil {
					userEmail = user.Email
					userName = user.FirstName + " " + user.LastName
				}
			} else {
				// For guest users, we might need to get email from payment intent
				// This would require additional logic to fetch payment intent
				log.Printf("[REFUND] Guest user refund created, email notification skipped: %s", refundNumber)
				return
			}

			if userEmail != "" {
				// Queue refund created email using centralized system
				templateData := map[string]interface{}{
					"event_name":      ticket.Event.Title,
					"refund_amount":   ticket.TotalAmount,
					"currency":        ticket.Event.Currency,
					"ticket_count":    1, // Single ticket refund
					"refund_number":   refundNumber,
					"refund_reason":   reason,
					"refund_status":   "pending",
					"user_name":       userName,
					"recipient_email": userEmail,
				}

				subject := fmt.Sprintf("Refund Request Submitted - %s", refundNumber)
				if err := s.emailOutboxService.QueueEmail(context.Background(), models.EmailEventRefundProcessed, userEmail, subject, templateData, 2); err != nil {
					log.Printf("[REFUND] Warning: Failed to queue refund created email: %v", err)
				} else {
					log.Printf("[REFUND] Refund created email queued for %s", userEmail)
				}
			}
		}
	}()

	return map[string]interface{}{
		"refund_status": "pending",
		"refund_number": refundNumber,
		"ticket_id":     ticketID,
	}, nil
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

func getCheckoutSessionTicketIDs(checkoutSession *models.CheckoutSession) []uuid.UUID {
	if checkoutSession == nil {
		log.Printf("[TICKET_SERVICE] ERROR: checkoutSession is nil in getCheckoutSessionTicketIDs")
		return []uuid.UUID{}
	}

	var ids []uuid.UUID

	if checkoutSession.GatewayData != nil {
		if ticketIDsRaw, ok := checkoutSession.GatewayData["ticket_ids"]; ok {
			ids = parseTicketIDsFromGatewayData(ticketIDsRaw)
		}
	}

	if len(ids) == 0 && checkoutSession.TicketID != uuid.Nil {
		ids = append(ids, checkoutSession.TicketID)
	}

	seen := make(map[uuid.UUID]bool)
	uniqueIDs := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		uniqueIDs = append(uniqueIDs, id)
	}

	return uniqueIDs
}

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

	// Find checkout session with this token
	var checkoutSession models.CheckoutSession
	if err := tx.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to find checkout session: %w", err)
	}

	// Check if already processed (idempotency)
	if checkoutSession.Status == "completed" {
		log.Printf("[TICKET_SERVICE] Checkout session %s already processed, skipping", checkoutToken)
		tx.Rollback() // Nothing to do
		return nil
	}

	// Validate checkout session data
	if checkoutSession.ID == uuid.Nil {
		tx.Rollback()
		return fmt.Errorf("checkout session ID is nil")
	}
	checkoutSession.UpdatedAt = time.Now()
	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// Collect all tickets - either from ticket_ids in gateway data or from the single ticket
	var allTickets []*models.Ticket
	ticketIDMap := make(map[uuid.UUID]bool)

	ticketIDs := getCheckoutSessionTicketIDs(&checkoutSession)
	if len(ticketIDs) == 0 {
		tx.Rollback()
		return utils.NewBusinessLogicError("No tickets found for checkout session")
	}

	for _, ticketID := range ticketIDs {
		var ticket models.Ticket
		if err := tx.Where("id = ?", ticketID).First(&ticket).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to find ticket: %w", err)
		}

		// Update ticket status
		if err := tx.Model(&models.Ticket{}).Where("id = ?", ticketID).Update("status", "active").Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update ticket status: %w", err)
		}

		// Collect unique tickets for transaction recording
		if !ticketIDMap[ticket.ID] {
			allTickets = append(allTickets, &ticket)
			ticketIDMap[ticket.ID] = true
		}
	}

	// Record transaction for successful payment gateway purchase (inside transaction for ACID guarantees)
	// Extract gateway transaction ID and payment intent ID - consolidated extraction logic
	gatewayTxnID, paymentIntentID := s.extractGatewayIDs(checkoutSession.GatewayData)

	if err := s.recordTransactionInTx(tx, allTickets, checkoutSession.PaymentGateway, gatewayTxnID, checkoutSession.GatewayData, "completed", paymentIntentID); err != nil {
		tx.Rollback()
		if _, ok := err.(*utils.AppError); ok {
			return err
		}
		return fmt.Errorf("failed to record transaction: %w", err)
	}

	// Mark checkout session as completed
	checkoutSession.Status = "completed"
	checkoutSession.UpdatedAt = time.Now()
	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to mark checkout session as completed: %w", err)
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

	// Find checkout session
	var checkoutSession models.CheckoutSession
	if err := tx.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to find checkout session: %w", err)
	}

	// Update checkout session status
	checkoutSession.Status = "failed"
	checkoutSession.UpdatedAt = time.Now()
	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// Check if there are tickets to cancel (for logged-in user purchases)
	ticketIDs := getCheckoutSessionTicketIDs(&checkoutSession)
	if len(ticketIDs) > 0 {
		// Load tickets to get tier information for inventory restoration
		var tickets []models.Ticket
		if err := tx.Where("id IN ? AND status = ?", ticketIDs, "pending_payment").Preload("Tier").Find(&tickets).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to load tickets for inventory restoration: %w", err)
		}

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
		for _, ticketID := range ticketIDs {
			if err := tx.Model(&models.Ticket{}).Where("id = ?", ticketID).Update("status", "cancelled").Error; err != nil {
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

// ProcessCanceledPayment processes a canceled payment from Stripe webhook
func (s *TicketService) ProcessCanceledPayment(checkoutToken string) error {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Find checkout session with this token
	var checkoutSession models.CheckoutSession
	if err := tx.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to find checkout session: %w", err)
	}

	// Update checkout session status
	checkoutSession.Status = "cancelled"
	checkoutSession.UpdatedAt = time.Now()
	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// Collect all tickets - either from ticket_ids in gateway data or from the single ticket
	var ticketIDs []uuid.UUID

	ticketIDs = getCheckoutSessionTicketIDs(&checkoutSession)
	if len(ticketIDs) == 0 {
		tx.Rollback()
		return utils.NewBusinessLogicError("No tickets found for checkout session")
	}

	// Load tickets to get tier information for inventory restoration
	var tickets []models.Ticket
	if err := tx.Where("id IN ?", ticketIDs).Preload("Tier").Find(&tickets).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to load tickets for inventory restoration: %w", err)
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
	for _, ticketID := range ticketIDs {
		if err := tx.Model(&models.Ticket{}).Where("id = ?", ticketID).Update("status", "cancelled").Error; err != nil {
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
func (s *TicketService) ProcessRefundedPayment(checkoutToken string) error {
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Find checkout session with this token
	var checkoutSession models.CheckoutSession
	if err := tx.Where("checkout_token = ?", checkoutToken).First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to find checkout session: %w", err)
	}

	// Update checkout session status to refunded
	checkoutSession.Status = "refunded"
	checkoutSession.UpdatedAt = time.Now()
	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update checkout session: %w", err)
	}

	// Collect all tickets - either from ticket_ids in gateway data or from the single ticket
	var ticketIDs []uuid.UUID

	ticketIDs = getCheckoutSessionTicketIDs(&checkoutSession)
	if len(ticketIDs) == 0 {
		tx.Rollback()
		return utils.NewBusinessLogicError("No tickets found for checkout session")
	}

	// Update tickets status to refunded
	for _, ticketID := range ticketIDs {
		if err := tx.Model(&models.Ticket{}).Where("id = ?", ticketID).Update("status", "refunded").Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update ticket status: %w", err)
		}
	}

	// Restore inventory: group tickets by tier and update sold count
	tierQuantities := make(map[uuid.UUID]int)
	for _, ticketID := range ticketIDs {
		var ticket models.Ticket
		if err := tx.Where("id = ?", ticketID).First(&ticket).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to find ticket for inventory restoration: %w", err)
		}
		tierQuantities[ticket.TierID]++
	}

	// Update tier sold counts
	for tierID, qty := range tierQuantities {
		result := tx.Model(&models.EventTier{}).
			Where("id = ?", tierID).
			Updates(map[string]interface{}{
				"sold": gorm.Expr("GREATEST(sold - ?, 0)", qty), // Prevent negative
			})

		if result.Error != nil {
			tx.Rollback()
			return fmt.Errorf("failed to restore inventory for tier %s: %w", tierID, result.Error)
		}
	}

	// Find and update transaction status to refunded
	var transaction models.Transaction
	if err := tx.Where("checkout_session_id = ?", checkoutSession.ID).First(&transaction).Error; err != nil {
		// Transaction may not exist yet if refund came before success webhook was processed
		log.Printf("Warning: Transaction not found for refunded checkout session %s\n", checkoutSession.CheckoutToken)
	} else {
		// NOTE: We don't update transaction status to "refunded" because:
		// 1. Financial reporting filters by status = 'completed' for revenue/ticket counts
		// 2. Refunds are tracked separately in the Refund model
		// 3. Changing status would break revenue analytics and commission calculations
		// Instead, refunds are handled separately in financial reports
		log.Printf("Transaction %s associated with refunded checkout session (keeping status as-is for financial reporting)\n", transaction.ID)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

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
