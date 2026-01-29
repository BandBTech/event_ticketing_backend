package services

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
	"event-ticketing-backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
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
	db                *gorm.DB
	financialService  *FinancialService
	emailQueueService *EmailQueueService
	authService       *AuthService
	jwtConfig         *config.JWTConfig
	secureQRService   *SecureQRService
}

func NewTicketService(db *gorm.DB, financialService *FinancialService, jwtConfig *config.JWTConfig) *TicketService {
	return &TicketService{
		db:               db,
		financialService: financialService,
		jwtConfig:        jwtConfig,
	}
}

// SetSecureQRService sets the secure QR service dependency
func (s *TicketService) SetSecureQRService(secureQR *SecureQRService) {
	s.secureQRService = secureQR
}

// SetEmailQueueService sets the email queue service for sending notifications
func (s *TicketService) SetEmailQueueService(emailQueueService *EmailQueueService) {
	s.emailQueueService = emailQueueService
}

// SetAuthService sets the auth service for user validation
func (s *TicketService) SetAuthService(authService *AuthService) {
	s.authService = authService
}

// GetEmailQueueService returns the email queue service
func (s *TicketService) GetEmailQueueService() *EmailQueueService {
	return s.emailQueueService
}

// PurchaseTicket creates multiple individual ticket purchases for a logged-in user
func (s *TicketService) PurchaseTicket(userID uuid.UUID, req *models.TicketPurchaseRequest) ([]*models.Ticket, error) {
	// Start transaction
	tx := s.db.Begin()

	// Get event details with lock for update
	var event models.Event
	err := tx.Set("gorm:query_option", "FOR UPDATE").First(&event, req.EventID).Error
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// Load the selected tier for price/name/availability (lock row for update)
	var tier models.EventTier
	if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ? AND event_id = ?", req.TierID, req.EventID).First(&tier).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// Check availability at tier level
	if tier.Available < req.Quantity {
		tx.Rollback()
		return nil, errors.New("Insufficient tickets available for selected tier.")
	}

	var tickets []*models.Ticket

	// Create individual tickets for each quantity
	// Starting sold count to generate sequential numbers within this transaction
	startingSold := tier.Sold

	for i := 0; i < req.Quantity; i++ {
		// Create ticket (one per person) using tier data
		ticket := &models.Ticket{
			UserID:         &userID,
			EventID:        req.EventID,
			TierID:         tier.ID,
			TotalAmount:    tier.Price,
			PaymentGateway: req.PaymentGateway,
			Status:         "active",
			PurchaseDate:   time.Now(),
		}

		// Generate sequential ticket number using tier name and event year
		// e.g., VIP-2025-0001 (zero padded based on event capacity)
		// Sanitize tier name to alphanumeric uppercase (keep letters and digits)
		sanitize := func(s string) string {
			s = strings.ToUpper(strings.ReplaceAll(s, " ", ""))
			// keep only alnum
			out := make([]rune, 0, len(s))
			for _, r := range s {
				if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
					out = append(out, r)
				}
			}
			if len(out) == 0 {
				return "T"
			}
			return string(out)
		}

		abbr := sanitize(tier.TierName)
		year := event.StartDate.Year()
		width := len(strconv.Itoa(event.Capacity))
		seq := startingSold + i + 1
		padded := fmt.Sprintf("%0*d", width, seq)
		ticketNum := fmt.Sprintf("%s-%d-%s", abbr, year, padded)

		ticket.TicketNumber = ticketNum

		if err := tx.Create(ticket).Error; err != nil {
			tx.Rollback()
			return nil, err
		}

		tickets = append(tickets, ticket)
	}

	// Update event availability
	event.Available -= req.Quantity
	if err := tx.Save(&event).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// Update tier availability/sold
	tier.Available -= req.Quantity
	tier.Sold += req.Quantity
	if err := tx.Save(&tier).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// Update financial tracking
	if s.financialService != nil {
		err := s.financialService.UpdateEventSales(
			req.EventID,
			tier.Price,
			req.Quantity,
			event.CommissionRate,
		)
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to update financial tracking: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	// Load associations for response
	for _, ticket := range tickets {
		if err := s.db.Preload("User").Preload("Event").Preload("Tier").First(ticket, ticket.ID).Error; err != nil {
			return nil, err
		}
	}

	// Send ticket confirmation emails with PDFs asynchronously for each ticket
	go s.sendUserTicketConfirmationEmails(tickets)

	return tickets, nil
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
		query = query.Where("purchase_date >= ?", *startDate)
	}
	if endDate != nil {
		query = query.Where("purchase_date <= ?", *endDate)
	}

	// Get total count
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Validate and set sorting
	validSortFields := map[string]bool{
		"purchase_date": true,
		"created_at":    true,
		"ticket_number": true,
		"price":         true,
	}

	if !validSortFields[sortBy] {
		sortBy = "purchase_date"
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

// GenerateQRCodeForTicket returns a base64-encoded QR payload (PNG) for a ticket
func (s *TicketService) GenerateQRCodeForTicket(ticketID uuid.UUID) (string, error) {
	if s.secureQRService == nil {
		return "", errors.New("secure QR service not configured")
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
		Preload("User").
		Preload("GuestUser").
		First(&ticket).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("ticket not found for this event")
		}
		return err
	}

	// Check if ticket is active
	if ticket.Status != "active" {
		tx.Rollback()
		return fmt.Errorf("ticket is %s and cannot be checked in", ticket.Status)
	}

	// Check if event is happening today or in the future
	now := time.Now()
	if ticket.Event.StartDate.After(now.Add(24 * time.Hour)) {
		tx.Rollback()
		return errors.New("check-in not available yet for this event")
	}

	// Check if event has already ended
	if ticket.Event.EndDate.Before(now) {
		tx.Rollback()
		return errors.New("cannot check in ticket: event has already ended")
	}

	// Check if already checked in
	if ticket.CheckInTime != nil {
		tx.Rollback()
		return errors.New("ticket already checked in")
	}

	// Set check-in time and staff
	checkInTime := time.Now()
	ticket.CheckInTime = &checkInTime
	ticket.CheckedInBy = &staffID

	if err := tx.Save(&ticket).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Commit transaction
	return tx.Commit().Error
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
			return errors.New("Ticket not found for this event")
		}
		return err
	}

	// Check if ticket is checked in
	if ticket.CheckInTime == nil {
		tx.Rollback()
		return errors.New("Ticket must be checked in before check-out")
	}

	// Check if already checked out
	if ticket.CheckOutTime != nil {
		tx.Rollback()
		return errors.New("Ticket already checked out")
	}

	// Check if event has already ended
	if ticket.Event.EndDate.Before(time.Now()) {
		tx.Rollback()
		return errors.New("Cannot check out ticket: event has already ended")
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

// GetEventTickets returns all tickets for a specific event (for organizers)
func (s *TicketService) GetEventTickets(eventID uuid.UUID, organizerID uuid.UUID, page, limit int) ([]models.Ticket, int64, error) {
	var tickets []models.Ticket
	var total int64

	offset := (page - 1) * limit

	// First verify the organizer owns this event
	var event models.Event
	if err := s.db.Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, errors.New("Event not found or access denied")
		}
		return nil, 0, err
	}

	query := s.db.Model(&models.Ticket{}).
		Where("event_id = ?", eventID).
		Preload("User").
		Preload("GuestUser")

	// Get total count
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Get paginated results
	if err := query.Order("purchase_date DESC").
		Offset(offset).
		Limit(limit).
		Find(&tickets).Error; err != nil {
		return nil, 0, err
	}

	return tickets, total, nil
}

// GetTicketStats returns ticket statistics for an event
func (s *TicketService) GetTicketStats(eventID uuid.UUID, organizerID uuid.UUID) (map[string]interface{}, error) {
	// First verify the organizer owns this event
	var event models.Event
	if err := s.db.Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("Event not found or access denied")
		}
		return nil, err
	}

	var stats struct {
		TotalTickets     int64   `json:"total_tickets"`
		CheckedIn        int64   `json:"checked_in"`
		CheckedOut       int64   `json:"checked_out"`
		ActiveTickets    int64   `json:"active_tickets"`
		CancelledTickets int64   `json:"cancelled_tickets"`
		TotalRevenue     float64 `json:"total_revenue"`
	}

	// Get ticket counts by status
	s.db.Model(&models.Ticket{}).
		Where("event_id = ?", eventID).
		Select("COUNT(*) as total_tickets, SUM(CASE WHEN check_in_time IS NOT NULL THEN 1 ELSE 0 END) as checked_in, SUM(CASE WHEN check_out_time IS NOT NULL THEN 1 ELSE 0 END) as checked_out, SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END) as active_tickets, SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END) as cancelled_tickets, COALESCE(SUM(total_amount), 0) as total_revenue").
		Scan(&stats)

	// Calculate estimated earnings after commission deduction
	commissionAmount := stats.TotalRevenue * (event.CommissionRate / 100)
	estimatedEarning := stats.TotalRevenue - commissionAmount

	return map[string]interface{}{
		"total_tickets":     stats.TotalTickets,
		"checked_in":        stats.CheckedIn,
		"checked_out":       stats.CheckedOut,
		"active_tickets":    stats.ActiveTickets,
		"cancelled_tickets": stats.CancelledTickets,
		"total_revenue":     stats.TotalRevenue,
		"commission_rate":   event.CommissionRate,
		"commission_amount": commissionAmount,
		"estimated_earning": estimatedEarning,
		"event_capacity":    event.Capacity,
		"available_tickets": event.Available,
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
	// Start transaction
	tx := s.db.Begin()

	// Create or find guest user
	guestUser, err := s.createOrFindGuestUser(tx, req)
	if err != nil {
		tx.Rollback()
		return nil, nil, err
	}

	// Get event tier details with lock for update
	var eventTier models.EventTier
	err = tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ? AND event_id = ?", req.TierID, req.EventID).First(&eventTier).Error
	if err != nil {
		tx.Rollback()
		return nil, nil, err
	}

	// Check if tier is active
	if !eventTier.IsActive {
		tx.Rollback()
		return nil, nil, errors.New("event tier is not active")
	}

	// Check availability
	if eventTier.Available < req.Quantity {
		tx.Rollback()
		return nil, nil, errors.New("insufficient tickets available for this tier")
	}

	var tickets []*models.Ticket

	// Create individual tickets for each quantity
	for i := 0; i < req.Quantity; i++ {
		// Create ticket (one per person)
		ticket := &models.Ticket{
			GuestUserID:     &guestUser.ID,
			EventID:         req.EventID,
			TierID:          req.TierID,
			TotalAmount:     eventTier.Price,
			PaymentGateway:  req.PaymentGateway,
			Status:          "active",
			IsGuestPurchase: true,
			PurchaseDate:    time.Now(),
		}

		if err := tx.Create(ticket).Error; err != nil {
			tx.Rollback()
			return nil, nil, err
		}

		tickets = append(tickets, ticket)
	}

	// Update tier availability and sold count
	eventTier.Available -= req.Quantity
	eventTier.Sold += req.Quantity
	if err := tx.Save(&eventTier).Error; err != nil {
		tx.Rollback()
		return nil, nil, err
	}

	// Update financial tracking
	if s.financialService != nil {
		err := s.financialService.UpdateEventSales(
			req.EventID,
			eventTier.Price,
			req.Quantity,
			0, // TODO: Add commission rate from event or organizer settings
		)
		if err != nil {
			tx.Rollback()
			return nil, nil, fmt.Errorf("failed to update financial tracking: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, nil, err
	}

	// Load associations for response
	for _, ticket := range tickets {
		if err := s.db.Preload("GuestUser").Preload("Event").First(ticket, ticket.ID).Error; err != nil {
			return nil, nil, err
		}
	}

	return tickets, guestUser, nil
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
			return nil, errors.New("invalid verification token")
		}
		return nil, err
	}

	// Check if token is expired
	if guestUser.TokenExpiresAt != nil && time.Now().After(*guestUser.TokenExpiresAt) {
		return nil, errors.New("verification token has expired")
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
		query = query.Where("tickets.purchase_date >= ?", *startDate)
	}
	if endDate != nil {
		query = query.Where("tickets.purchase_date <= ?", *endDate)
	}

	// Get total count
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Validate and set sorting
	validSortFields := map[string]bool{
		"purchase_date": true,
		"created_at":    true,
		"ticket_number": true,
		"price":         true,
	}

	if !validSortFields[sortBy] {
		sortBy = "purchase_date"
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
			return errors.New("guest user not found")
		}
		return err
	}

	// Check if already converted
	if guestUser.ConvertedToUser {
		tx.Rollback()
		return errors.New("guest user already converted to registered user")
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
		return errors.New("auth service not configured")
	}

	// Get the staff member details
	staff, err := s.authService.GetUserByID(staffID)
	if err != nil {
		return fmt.Errorf("failed to get staff details: %w", err)
	}

	// Get the event details
	var event models.Event
	if err := s.db.Where("id = ?", eventID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("event not found")
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

	return errors.New("access denied: you can only scan tickets for events organized by your organization")
}

// InitiatePaymentGatewayPurchase creates multiple ticket purchases with payment gateway integration
func (s *TicketService) InitiatePaymentGatewayPurchase(req *models.GuestPurchaseRequest) (*models.CheckoutSession, []*models.Ticket, *models.GuestUser, error) {
	// Start transaction
	tx := s.db.Begin()

	// Create or find guest user
	guestUser, err := s.createOrFindGuestUser(tx, req)
	if err != nil {
		tx.Rollback()
		return nil, nil, nil, err
	}

	// Get event tier details with lock for update
	var eventTier models.EventTier
	err = tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ? AND event_id = ?", req.TierID, req.EventID).First(&eventTier).Error
	if err != nil {
		tx.Rollback()
		return nil, nil, nil, err
	}

	// Check if tier is active
	if !eventTier.IsActive {
		tx.Rollback()
		return nil, nil, nil, errors.New("event tier is not active")
	}

	// Check availability
	if eventTier.Available < req.Quantity {
		tx.Rollback()
		return nil, nil, nil, errors.New("insufficient tickets available for this tier")
	}

	// Calculate total amount
	totalAmount := eventTier.Price * float64(req.Quantity)

	var tickets []*models.Ticket

	// Create individual tickets for each quantity
	for i := 0; i < req.Quantity; i++ {
		// Create ticket (one per person)
		ticket := &models.Ticket{
			GuestUserID:     &guestUser.ID,
			EventID:         req.EventID,
			TierID:          req.TierID,
			TotalAmount:     eventTier.Price,
			PaymentGateway:  req.PaymentGateway,
			Status:          "pending_payment",
			IsGuestPurchase: true,
			PurchaseDate:    time.Now(),
		}

		if err := tx.Create(ticket).Error; err != nil {
			tx.Rollback()
			return nil, nil, nil, err
		}

		tickets = append(tickets, ticket)
	}

	// Update tier availability and sold count
	eventTier.Available -= req.Quantity
	eventTier.Sold += req.Quantity
	if err := tx.Save(&eventTier).Error; err != nil {
		tx.Rollback()
		return nil, nil, nil, err
	}

	// Generate unique checkout token
	checkoutToken := s.generateSecureToken()

	// Create checkout session (references the first ticket for simplicity, but we track all tickets)
	checkoutSession := &models.CheckoutSession{
		TicketID:       tickets[0].ID, // Reference first ticket
		GuestUserID:    guestUser.ID,
		CheckoutToken:  checkoutToken,
		PaymentGateway: req.PaymentGateway,
		Amount:         totalAmount,
		Currency:       eventTier.Currency, // Use tier currency
		Status:         "pending",
		GatewayData:    make(map[string]interface{}),
		ExpiresAt:      time.Now().Add(30 * time.Minute), // 30 minutes expiry
	}

	// Initialize gateway-specific data
	err = s.initializeGatewayData(checkoutSession, req, tickets[0], guestUser)
	if err != nil {
		tx.Rollback()
		return nil, nil, nil, err
	}

	if err := tx.Create(checkoutSession).Error; err != nil {
		tx.Rollback()
		return nil, nil, nil, err
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, nil, nil, err
	}

	// Load associations for response
	for _, ticket := range tickets {
		if err := s.db.Preload("GuestUser").Preload("Event").First(ticket, ticket.ID).Error; err != nil {
			return nil, nil, nil, err
		}
	}

	return checkoutSession, tickets, guestUser, nil
}

// generateSecureToken generates a cryptographically secure token for checkout sessions
func (s *TicketService) generateSecureToken() string {
	// Generate a UUID and add some randomness
	token := uuid.New().String()
	// Add timestamp for additional uniqueness
	timestamp := time.Now().UnixNano()
	return fmt.Sprintf("%s_%d", token, timestamp)
}

// initializeGatewayData initializes payment gateway specific data
func (s *TicketService) initializeGatewayData(checkoutSession *models.CheckoutSession, req *models.GuestPurchaseRequest, ticket *models.Ticket, guestUser *models.GuestUser) error {
	baseURL := s.getBaseURL()

	switch checkoutSession.PaymentGateway {
	case models.PaymentGatewayStripe:
		// Initialize Stripe session data
		checkoutSession.GatewayData = map[string]interface{}{
			"session_id":  "", // Will be set by Stripe API call
			"success_url": fmt.Sprintf("%s/payment/success/%s", baseURL, checkoutSession.CheckoutToken),
			"cancel_url":  fmt.Sprintf("%s/payment/cancel/%s", baseURL, checkoutSession.CheckoutToken),
			"line_items": []map[string]interface{}{
				{
					"price_data": map[string]interface{}{
						"currency": "npr",
						"product_data": map[string]interface{}{
							"name":        fmt.Sprintf("Ticket for %s", ticket.Event.Title),
							"description": fmt.Sprintf("%d tickets", req.Quantity),
						},
						"unit_amount": int64(ticket.TotalAmount * 100), // Convert to paisa
					},
					"quantity": 1,
				},
			},
			"metadata": map[string]interface{}{
				"ticket_id":      ticket.ID.String(),
				"guest_user_id":  guestUser.ID.String(),
				"checkout_token": checkoutSession.CheckoutToken,
			},
		}

	case models.PaymentGatewayPayPal:
		// Initialize PayPal order data
		checkoutSession.GatewayData = map[string]interface{}{
			"order_id": "", // Will be set by PayPal API call
			"intent":   "CAPTURE",
			"purchase_units": []map[string]interface{}{
				{
					"amount": map[string]interface{}{
						"currency_code": "NPR",
						"value":         fmt.Sprintf("%.2f", ticket.TotalAmount),
					},
					"description": fmt.Sprintf("Ticket purchase for %s", ticket.Event.Title),
				},
			},
			"application_context": map[string]interface{}{
				"return_url": fmt.Sprintf("%s/payment/success/%s", baseURL, checkoutSession.CheckoutToken),
				"cancel_url": fmt.Sprintf("%s/payment/cancel/%s", baseURL, checkoutSession.CheckoutToken),
			},
		}

	case models.PaymentGatewayEsewa:
		// Initialize eSewa payment data
		checkoutSession.GatewayData = map[string]interface{}{
			"amt":   fmt.Sprintf("%.2f", ticket.TotalAmount),
			"txAmt": "0",
			"psc":   "0",
			"pdc":   "0",
			"tAmt":  fmt.Sprintf("%.2f", ticket.TotalAmount),
			"pid":   checkoutSession.CheckoutToken, // Use checkout token as product ID
			"scd":   "your_esewa_merchant_code",    // This should come from config
			"su":    fmt.Sprintf("%s/payment/success/%s", baseURL, checkoutSession.CheckoutToken),
			"fu":    fmt.Sprintf("%s/payment/failure/%s", baseURL, checkoutSession.CheckoutToken),
		}

	default:
		return fmt.Errorf("unsupported payment gateway: %s", checkoutSession.PaymentGateway)
	}

	return nil
}

// ProcessPaymentSuccess processes a successful payment callback
func (s *TicketService) ProcessPaymentSuccess(req *models.PaymentCallbackRequest) error {
	// Start transaction
	tx := s.db.Begin()

	// Find checkout session
	var checkoutSession models.CheckoutSession
	if err := tx.Where("checkout_token = ?", req.CheckoutToken).First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return errors.New("checkout session not found")
	}

	// Check if already processed
	if checkoutSession.Status == "completed" {
		tx.Rollback()
		return errors.New("payment already processed")
	}

	// Check if expired
	if checkoutSession.ExpiresAt.Before(time.Now()) {
		tx.Rollback()
		return errors.New("checkout session expired")
	}

	// Update checkout session
	checkoutSession.Status = "completed"
	checkoutSession.GatewayData = req.GatewayData
	if err := tx.Save(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Find all tickets associated with this checkout session
	// Since we create multiple tickets, we need to find all pending_payment tickets
	// for this guest user and event that were created recently
	var tickets []models.Ticket
	if err := tx.Preload("Event").Where("guest_user_id = ? AND event_id = (SELECT event_id FROM tickets WHERE id = ?) AND status = ?",
		checkoutSession.GuestUserID,
		checkoutSession.TicketID,
		"pending_payment").Find(&tickets).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Update all tickets status to active
	for _, ticket := range tickets {
		ticket.Status = "active"
		if err := tx.Save(&ticket).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	// Update financial tracking (use the total from checkout session)
	if s.financialService != nil && len(tickets) > 0 {
		err := s.financialService.UpdateEventSales(
			tickets[0].EventID,
			checkoutSession.Amount/float64(len(tickets)), // Price per ticket
			len(tickets),
			tickets[0].Event.CommissionRate,
		)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update financial tracking: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return err
	}

	// Send ticket confirmation emails with PDFs asynchronously for each ticket
	go s.sendPaymentSuccessEmails(checkoutSession, tickets)

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
	var ticketTokens []string
	var ticketData []map[string]interface{}

	for _, ticket := range tickets {
		// Generate JWT access token for this ticket
		jwtService := utils.NewJWTService(s.jwtConfig)
		token, err := jwtService.GenerateTicketAccessToken(&ticket)
		if err != nil {
			log.Printf("Failed to generate JWT token for ticket %s: %v", ticket.ID, err)
			continue
		}

		ticketTokens = append(ticketTokens, token)

		// Prepare ticket data for email template
		ticketData = append(ticketData, map[string]interface{}{
			"ticket_number": ticket.TicketNumber,
			"access_token":  token,
			"view_url":      fmt.Sprintf("%s/tickets/view?token=%s", s.getBaseURL(), token),
		})
	}

	// Prepare email data
	emailData := map[string]interface{}{
		"guest_name":     guestUser.FirstName + " " + guestUser.LastName,
		"guest_email":    guestUser.Email,
		"event_name":     event.Title,
		"event_date":     event.StartDate.Format("January 2, 2006"),
		"event_time":     event.StartDate.Format("3:04 PM"),
		"venue":          event.VenueName,
		"organizer_name": getOrganizerDisplayName(event.Organizer),
		"tickets":        ticketData,
		"total_tickets":  len(tickets),
		"total_amount":   checkoutSession.Amount,
		"base_url":       s.getBaseURL(),
	}

	// Send single email with all tickets
	if s.emailQueueService != nil {
		if err := s.emailQueueService.QueueGuestOrderConfirmationEmail(guestUser.Email, emailData); err != nil {
			log.Printf("Failed to queue guest order confirmation email: %v", err)
		}
	}
}

// ProcessPaymentFailure processes a failed payment callback
func (s *TicketService) ProcessPaymentFailure(req *models.PaymentCallbackRequest) error {
	// Start transaction
	tx := s.db.Begin()

	// Find checkout session
	var checkoutSession models.CheckoutSession
	if err := tx.Where("checkout_token = ?", req.CheckoutToken).First(&checkoutSession).Error; err != nil {
		tx.Rollback()
		return errors.New("checkout session not found")
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
	if err := tx.Where("guest_user_id = ? AND event_id = (SELECT event_id FROM tickets WHERE id = ?) AND status = ?",
		checkoutSession.GuestUserID,
		checkoutSession.TicketID,
		"pending_payment").Find(&tickets).Error; err != nil {
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

// GetCheckoutSessionByToken retrieves a checkout session by token
func (s *TicketService) GetCheckoutSessionByToken(token string) (*models.CheckoutSession, error) {
	var checkoutSession models.CheckoutSession
	if err := s.db.Where("checkout_token = ?", token).Preload("Ticket").Preload("GuestUser").First(&checkoutSession).Error; err != nil {
		return nil, err
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

	// Generate JWT tokens for each ticket
	var ticketTokens []string
	var ticketData []map[string]interface{}

	for _, ticketPtr := range tickets {
		ticket := *ticketPtr // Dereference the pointer
		// Generate JWT access token for this ticket
		jwtService := utils.NewJWTService(s.jwtConfig)
		token, err := jwtService.GenerateTicketAccessToken(&ticket)
		if err != nil {
			log.Printf("Failed to generate JWT token for ticket %s: %v", ticket.ID, err)
			continue
		}

		ticketTokens = append(ticketTokens, token)

		// Prepare ticket data for email template
		ticketData = append(ticketData, map[string]interface{}{
			"ticket_number": ticket.TicketNumber,
			"access_token":  token,
			"view_url":      fmt.Sprintf("%s/tickets/view?token=%s", s.getBaseURL(), token),
		})
	}

	// Prepare email data
	emailData := map[string]interface{}{
		"user_name":      user.FirstName + " " + user.LastName,
		"user_email":     user.Email,
		"event_name":     event.Title,
		"event_date":     event.StartDate.Format("January 2, 2006"),
		"event_time":     event.StartDate.Format("3:04 PM"),
		"venue":          event.VenueName,
		"organizer_name": getOrganizerDisplayName(event.Organizer),
		"tickets":        ticketData,
		"total_tickets":  len(tickets),
		"total_amount":   tickets[0].TotalAmount * float64(len(tickets)), // Calculate total
		"base_url":       s.getBaseURL(),
	}

	// Send single email with all tickets
	if s.emailQueueService != nil {
		if err := s.emailQueueService.QueueOrderConfirmationEmail(user.Email, emailData); err != nil {
			log.Printf("Failed to queue user order confirmation email: %v", err)
		}
	}
}

// getBaseURL returns the base URL for the application
func (s *TicketService) getBaseURL() string {
	// This would ideally come from config, but for now use a default
	// In a real implementation, this should be injected from config
	return "https://user.timroticket.com"
}
