package services

import (
	"errors"
	"fmt"
	"time"

	"event-ticketing-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TicketService struct {
	db               *gorm.DB
	financialService *FinancialService
}

func NewTicketService(db *gorm.DB, financialService *FinancialService) *TicketService {
	return &TicketService{
		db:               db,
		financialService: financialService,
	}
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
		UserID:       userID,
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
	if err := s.db.Preload("User").Preload("Event").First(ticket, ticket.ID).Error; err != nil {
		return nil, err
	}

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
