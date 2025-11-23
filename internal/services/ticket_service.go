package services

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"event-ticketing-backend/internal/models"

	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"
	"gorm.io/gorm"
)

type TicketService struct {
	db                             *gorm.DB
	financialService               *FinancialService
	emailQueueService              *EmailQueueService
	universalTicketTemplateService *UniversalTicketTemplateService
}

func NewTicketService(db *gorm.DB, financialService *FinancialService) *TicketService {
	return &TicketService{
		db:               db,
		financialService: financialService,
	}
}

// SetEmailQueueService sets the email queue service for sending notifications
func (s *TicketService) SetEmailQueueService(emailQueueService *EmailQueueService) {
	s.emailQueueService = emailQueueService
}

// SetUniversalTicketTemplateService sets the universal ticket template service for generating ticket PDFs
func (s *TicketService) SetUniversalTicketTemplateService(universalTicketTemplateService *UniversalTicketTemplateService) {
	s.universalTicketTemplateService = universalTicketTemplateService
}

// PurchaseTicket creates a new ticket purchase
func (s *TicketService) PurchaseTicket(userID uuid.UUID, req *models.TicketPurchaseRequest) (*models.Ticket, error) {
	// Start transaction
	tx := s.db.Begin()

	// Get event details with lock for update
	var event models.Event
	err := tx.Set("gorm:query_option", "FOR UPDATE").First(&event, req.EventID).Error
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// Check availability
	if event.Available < req.Quantity {
		tx.Rollback()
		return nil, errors.New("insufficient tickets available")
	}

	// Create ticket
	ticket := &models.Ticket{
		UserID:       &userID,
		EventID:      req.EventID,
		Quantity:     req.Quantity,
		TotalAmount:  event.Price * float64(req.Quantity),
		Status:       "active",
		PurchaseDate: time.Now(),
	}

	// Generate unique ticket number
	ticket.TicketNumber = fmt.Sprintf("TKT-%d-%s-%d", req.EventID, userID.String()[:8], time.Now().Unix())

	if err := tx.Create(ticket).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// Update event availability
	event.Available -= req.Quantity
	if err := tx.Save(&event).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// Update financial tracking
	if s.financialService != nil {
		err := s.financialService.UpdateEventSales(
			req.EventID,
			event.Price,
			req.Quantity,
			event.CommissionRate, // Use the commission rate set for this event
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
	if err := s.db.Preload("User").Preload("Event").Preload("EventTier").First(ticket, ticket.ID).Error; err != nil {
		return nil, err
	}

	// Send ticket confirmation email with attachment (async)
	go s.sendTicketConfirmationEmail(ticket)

	return ticket, nil
}

// GetUserTickets returns all tickets purchased by a user
func (s *TicketService) GetUserTickets(userID uuid.UUID, page, limit int) ([]models.Ticket, int64, error) {
	var tickets []models.Ticket
	var total int64

	offset := (page - 1) * limit

	query := s.db.Model(&models.Ticket{}).
		Where("user_id = ?", userID).
		Preload("Event").
		Preload("User")

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

// GetTicketByNumber returns a ticket by its ticket number
func (s *TicketService) GetTicketByNumber(ticketNumber string) (*models.Ticket, error) {
	var ticket models.Ticket
	if err := s.db.Where("ticket_number = ?", ticketNumber).
		Preload("User").
		Preload("Event").
		First(&ticket).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("ticket not found")
		}
		return nil, err
	}
	return &ticket, nil
}

// CheckInTicket handles ticket check-in by staff
func (s *TicketService) CheckInTicket(ticketNumber string, eventID uuid.UUID, staffID uuid.UUID) (*models.Ticket, error) {
	// Start transaction
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Get the ticket
	var ticket models.Ticket
	if err := tx.Where("ticket_number = ? AND event_id = ?", ticketNumber, eventID).
		Preload("Event").
		First(&ticket).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("ticket not found for this event")
		}
		return nil, err
	}

	// Check if ticket is active
	if ticket.Status != "active" {
		tx.Rollback()
		return nil, fmt.Errorf("ticket is %s and cannot be checked in", ticket.Status)
	}

	// Check if event is happening today or in the future
	now := time.Now()
	if ticket.Event.StartDate.After(now.Add(24 * time.Hour)) {
		tx.Rollback()
		return nil, errors.New("check-in not available yet for this event")
	}

	// Check if already checked in
	if ticket.CheckInTime != nil {
		tx.Rollback()
		return nil, errors.New("ticket already checked in")
	}

	// Update ticket
	checkInTime := time.Now()
	ticket.CheckInTime = &checkInTime
	ticket.CheckedInBy = &staffID

	if err := tx.Save(&ticket).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	// Reload with associations
	if err := s.db.Preload("User").Preload("Event").First(&ticket, ticket.ID).Error; err != nil {
		return nil, err
	}

	return &ticket, nil
}

// CheckOutTicket handles ticket check-out by staff
func (s *TicketService) CheckOutTicket(ticketNumber string, eventID uuid.UUID, staffID uuid.UUID) (*models.Ticket, error) {
	// Start transaction
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Get the ticket
	var ticket models.Ticket
	if err := tx.Where("ticket_number = ? AND event_id = ?", ticketNumber, eventID).
		Preload("Event").
		First(&ticket).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("ticket not found for this event")
		}
		return nil, err
	}

	// Check if ticket is checked in
	if ticket.CheckInTime == nil {
		tx.Rollback()
		return nil, errors.New("ticket must be checked in before check-out")
	}

	// Check if already checked out
	if ticket.CheckOutTime != nil {
		tx.Rollback()
		return nil, errors.New("ticket already checked out")
	}

	// Update ticket
	checkOutTime := time.Now()
	ticket.CheckOutTime = &checkOutTime
	ticket.CheckedOutBy = &staffID

	if err := tx.Save(&ticket).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	// Reload with associations
	if err := s.db.Preload("User").Preload("Event").First(&ticket, ticket.ID).Error; err != nil {
		return nil, err
	}

	return &ticket, nil
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
			return nil, 0, errors.New("event not found or access denied")
		}
		return nil, 0, err
	}

	query := s.db.Model(&models.Ticket{}).
		Where("event_id = ?", eventID).
		Preload("User")

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
			return nil, errors.New("event not found or access denied")
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

	return map[string]interface{}{
		"total_tickets":     stats.TotalTickets,
		"checked_in":        stats.CheckedIn,
		"checked_out":       stats.CheckedOut,
		"active_tickets":    stats.ActiveTickets,
		"cancelled_tickets": stats.CancelledTickets,
		"total_revenue":     stats.TotalRevenue,
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

// PurchaseTicketAsGuest creates a ticket purchase for a guest user
func (s *TicketService) PurchaseTicketAsGuest(req *models.GuestPurchaseRequest) (*models.Ticket, *models.GuestUser, error) {
	// Start transaction
	tx := s.db.Begin()

	// Create or find guest user
	guestUser, err := s.createOrFindGuestUser(tx, req)
	if err != nil {
		tx.Rollback()
		return nil, nil, err
	}

	// Get event details with lock for update
	var event models.Event
	err = tx.Set("gorm:query_option", "FOR UPDATE").First(&event, req.EventID).Error
	if err != nil {
		tx.Rollback()
		return nil, nil, err
	}

	// Check availability
	if event.Available < req.Quantity {
		tx.Rollback()
		return nil, nil, errors.New("insufficient tickets available")
	}

	// Create ticket
	ticket := &models.Ticket{
		GuestUserID:     &guestUser.ID,
		EventID:         req.EventID,
		Quantity:        req.Quantity,
		TotalAmount:     event.Price * float64(req.Quantity),
		Status:          "pending_verification",
		IsGuestPurchase: true,
		PurchaseDate:    time.Now(),
	}

	if err := tx.Create(ticket).Error; err != nil {
		tx.Rollback()
		return nil, nil, err
	}

	// Create individual tickets with QR codes
	for i := 0; i < req.Quantity; i++ {
		individualTicket := &models.IndividualTicket{
			TicketID: ticket.ID,
			Status:   "pending_verification",
		}

		// Generate QR code for this individual ticket
		qrData := map[string]interface{}{
			"ticket_id":     individualTicket.TicketNumber,
			"event_id":      req.EventID.String(),
			"guest_email":   req.Email,
			"purchase_time": time.Now().Unix(),
		}

		qrJSON, _ := json.Marshal(qrData)
		qrCode, err := qrcode.Encode(string(qrJSON), qrcode.Medium, 256)
		if err != nil {
			tx.Rollback()
			return nil, nil, fmt.Errorf("failed to generate QR code: %w", err)
		}

		individualTicket.QRCode = base64.StdEncoding.EncodeToString(qrCode)

		if err := tx.Create(individualTicket).Error; err != nil {
			tx.Rollback()
			return nil, nil, err
		}
	}

	// Update event availability
	event.Available -= req.Quantity
	if err := tx.Save(&event).Error; err != nil {
		tx.Rollback()
		return nil, nil, err
	}

	// Update financial tracking
	if s.financialService != nil {
		err := s.financialService.UpdateEventSales(
			req.EventID,
			event.Price,
			req.Quantity,
			event.CommissionRate,
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
	if err := s.db.Preload("GuestUser").Preload("Event").First(ticket, ticket.ID).Error; err != nil {
		return nil, nil, err
	}

	return ticket, guestUser, nil
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

	// Activate all individual tickets
	if err := s.db.Model(&models.IndividualTicket{}).
		Where("ticket_id = ?", ticket.ID).
		Update("status", "active").Error; err != nil {
		return nil, err
	}

	return &ticket, nil
}

// GetGuestTickets returns all tickets purchased by a guest user
func (s *TicketService) GetGuestTickets(guestEmail string, page, limit int) ([]models.Ticket, int64, error) {
	var tickets []models.Ticket
	var total int64

	offset := (page - 1) * limit

	query := s.db.Model(&models.Ticket{}).
		Joins("JOIN guest_users ON tickets.guest_user_id = guest_users.id").
		Where("guest_users.email = ? AND tickets.is_guest_purchase = ?", guestEmail, true).
		Preload("Event").
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

// GetIndividualTickets returns all individual tickets for a parent ticket
func (s *TicketService) GetIndividualTickets(ticketID uuid.UUID) ([]models.IndividualTicket, error) {
	var individualTickets []models.IndividualTicket
	if err := s.db.Where("ticket_id = ?", ticketID).
		Preload("Ticket").
		Preload("Ticket.Event").
		Find(&individualTickets).Error; err != nil {
		return nil, err
	}
	return individualTickets, nil
}

// CheckInIndividualTicket handles individual ticket check-in
func (s *TicketService) CheckInIndividualTicket(ticketNumber string, eventID uuid.UUID, staffID uuid.UUID) (*models.IndividualTicket, error) {
	// Start transaction
	tx := s.db.Begin()

	// Get the individual ticket
	var individualTicket models.IndividualTicket
	if err := tx.Joins("JOIN tickets ON individual_tickets.ticket_id = tickets.id").
		Where("individual_tickets.ticket_number = ? AND tickets.event_id = ?", ticketNumber, eventID).
		Preload("Ticket").
		Preload("Ticket.Event").
		First(&individualTicket).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("ticket not found for this event")
		}
		return nil, err
	}

	// Check if ticket is active
	if individualTicket.Status != "active" {
		tx.Rollback()
		return nil, fmt.Errorf("ticket is %s and cannot be checked in", individualTicket.Status)
	}

	// Check if event is happening today or in the future
	now := time.Now()
	if individualTicket.Ticket.Event.StartDate.After(now.Add(24 * time.Hour)) {
		tx.Rollback()
		return nil, errors.New("check-in not available yet for this event")
	}

	// Check if already checked in
	if individualTicket.CheckInTime != nil {
		tx.Rollback()
		return nil, errors.New("ticket already checked in")
	}

	// Update individual ticket
	checkInTime := time.Now()
	individualTicket.CheckInTime = &checkInTime
	individualTicket.CheckedInBy = &staffID

	if err := tx.Save(&individualTicket).Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	return &individualTicket, nil
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

// sendTicketConfirmationEmail sends ticket confirmation email with PDF attachment
func (s *TicketService) sendTicketConfirmationEmail(ticket *models.Ticket) {
	if s.emailQueueService == nil || s.universalTicketTemplateService == nil {
		log.Printf("Email or universal ticket template service not configured, skipping ticket confirmation email")
		return
	}

	// Get individual tickets for this purchase
	individualTickets, err := s.GetIndividualTickets(ticket.ID)
	if err != nil {
		log.Printf("Failed to get individual tickets for email: %v", err)
		return
	}

	// Send email for each individual ticket
	for _, individualTicket := range individualTickets {
		// Generate ticket PDF attachment
		attachment, err := s.universalTicketTemplateService.GenerateTicketAttachment(&individualTicket)
		if err != nil {
			log.Printf("Failed to generate ticket PDF for %s: %v", individualTicket.TicketNumber, err)
			continue
		}

		// Prepare email data
		attendeeName := "Valued Customer"
		if ticket.User != nil {
			attendeeName = ticket.User.FirstName + " " + ticket.User.LastName
		} else if ticket.GuestUser != nil {
			attendeeName = ticket.GuestUser.FirstName + " " + ticket.GuestUser.LastName
		}

		tierName := ""
		// Tier information would be available if individual tickets are associated with specific tiers

		emailData := map[string]interface{}{
			"Title":         "Your Event Ticket - Payment Confirmed",
			"Message":       "Thank you for your purchase! Your ticket is attached to this email as a PDF. Please save it to your device and present the QR code at event entry.",
			"EventTitle":    ticket.Event.Title,
			"EventDate":     ticket.Event.StartDate.Format("January 2, 2006 at 3:04 PM"),
			"EventLocation": ticket.Event.Location,
			"TicketNumber":  individualTicket.TicketNumber,
			"AttendeeName":  attendeeName,
			"TierName":      tierName,
			"TicketURL":     fmt.Sprintf("%s/ticket/%s", s.getBaseURL(), individualTicket.TicketNumber),
			"EventURL":      fmt.Sprintf("%s/events/%s", s.getBaseURL(), ticket.EventID.String()),
		}

		// Send email with attachment
		err = s.emailQueueService.QueueTicketWithAttachmentEmail(
			s.getRecipientEmail(ticket),
			emailData,
			attachment,
		)
		if err != nil {
			log.Printf("Failed to queue ticket confirmation email for %s: %v", individualTicket.TicketNumber, err)
		}
	}
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

// getBaseURL returns the base URL for the application
func (s *TicketService) getBaseURL() string {
	// This would ideally come from config, but for now use a default
	// In a real implementation, this should be injected from config
	return "https://user.timroticket.com"
}
