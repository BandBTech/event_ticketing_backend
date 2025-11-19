package services

import (
	"errors"
	"fmt"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EventManagementService handles advanced event management operations
type EventManagementService struct {
	db *gorm.DB
}

// NewEventManagementService creates a new event management service
func NewEventManagementService() *EventManagementService {
	return &EventManagementService{
		db: database.DB,
	}
}

// ControlEventSales allows organizers to pause, resume, or stop sales
func (s *EventManagementService) ControlEventSales(eventID, organizerID uuid.UUID, req *models.EventSalesControlRequest) (*models.Event, error) {
	var event models.Event

	// Find the event and verify ownership
	if err := s.db.Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("event not found or you don't have permission")
		}
		return nil, err
	}

	// Check if event is cancelled
	if event.IsCancelled {
		return nil, fmt.Errorf("cannot control sales for cancelled event")
	}

	// Update sales status based on action
	switch req.Action {
	case "pause":
		if event.SalesStatus == "paused" {
			return nil, fmt.Errorf("event sales are already paused")
		}
		event.SalesStatus = "paused"
	case "resume":
		if event.SalesStatus == "active" {
			return nil, fmt.Errorf("event sales are already active")
		}
		event.SalesStatus = "active"
	case "stop":
		if event.SalesStatus == "stopped" {
			return nil, fmt.Errorf("event sales are already stopped")
		}
		event.SalesStatus = "stopped"
	default:
		return nil, fmt.Errorf("invalid action: must be pause, resume, or stop")
	}

	if err := s.db.Save(&event).Error; err != nil {
		return nil, fmt.Errorf("failed to update event sales status: %w", err)
	}

	return &event, nil
}

// CancelEvent allows organizers to cancel their events
func (s *EventManagementService) CancelEvent(eventID, organizerID uuid.UUID, req *models.EventCancellationRequest) (*models.Event, error) {
	var event models.Event

	// Find the event and verify ownership
	if err := s.db.Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("event not found or you don't have permission")
		}
		return nil, err
	}

	// Check if event is already cancelled
	if event.IsCancelled {
		return nil, fmt.Errorf("event is already cancelled")
	}

	// Check if event has already started
	if time.Now().After(event.StartDate) {
		return nil, fmt.Errorf("cannot cancel event that has already started")
	}

	// Cancel the event
	now := time.Now()
	event.IsCancelled = true
	event.CancelledAt = &now
	event.CancelReason = req.Reason
	event.Status = "cancelled"
	event.SalesStatus = "stopped"

	if err := s.db.Save(&event).Error; err != nil {
		return nil, fmt.Errorf("failed to cancel event: %w", err)
	}

	// TODO: Send cancellation notifications to attendees
	// TODO: Process refunds if needed

	return &event, nil
}

// GetEventAnalytics returns comprehensive analytics for an event
func (s *EventManagementService) GetEventAnalytics(eventID, organizerID uuid.UUID) (*models.EventAnalyticsResponse, error) {
	var event models.Event

	// Find the event with tiers
	if err := s.db.Preload("Tiers").Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("event not found or you don't have permission")
		}
		return nil, err
	}

	// Calculate totals
	totalSeats := 0
	soldSeats := 0
	totalRevenue := 0.0
	tierAnalytics := make([]models.EventTierAnalytics, len(event.Tiers))

	for i, tier := range event.Tiers {
		totalSeats += tier.Quantity
		soldSeats += tier.Sold
		totalRevenue += float64(tier.Sold) * tier.Price
		tierAnalytics[i] = tier.ToAnalytics()
	}

	analytics := &models.EventAnalyticsResponse{
		EventID:      event.ID,
		EventTitle:   event.Title,
		EventStatus:  event.Status,
		SalesStatus:  event.SalesStatus,
		TotalSeats:   totalSeats,
		SoldSeats:    soldSeats,
		AvailSeats:   totalSeats - soldSeats,
		TotalRevenue: totalRevenue,
		TierCount:    len(event.Tiers),
		Tiers:        tierAnalytics,
		CreatedAt:    event.CreatedAt,
	}

	return analytics, nil
}

// GetAllEventsAnalytics returns analytics for all events (admin only)
func (s *EventManagementService) GetAllEventsAnalytics(page, limit int) ([]models.EventAnalyticsResponse, int64, error) {
	var events []models.Event
	var total int64

	// Get total count
	s.db.Model(&models.Event{}).Where("is_cancelled = ?", false).Count(&total)

	// Get paginated events with tiers
	offset := (page - 1) * limit
	if err := s.db.Preload("Tiers").Where("is_cancelled = ?", false).
		Offset(offset).Limit(limit).Find(&events).Error; err != nil {
		return nil, 0, err
	}

	analytics := make([]models.EventAnalyticsResponse, len(events))
	for i, event := range events {
		// Calculate totals for each event
		totalSeats := 0
		soldSeats := 0
		totalRevenue := 0.0
		tierAnalytics := make([]models.EventTierAnalytics, len(event.Tiers))

		for j, tier := range event.Tiers {
			totalSeats += tier.Quantity
			soldSeats += tier.Sold
			totalRevenue += float64(tier.Sold) * tier.Price
			tierAnalytics[j] = tier.ToAnalytics()
		}

		analytics[i] = models.EventAnalyticsResponse{
			EventID:      event.ID,
			EventTitle:   event.Title,
			EventStatus:  event.Status,
			SalesStatus:  event.SalesStatus,
			TotalSeats:   totalSeats,
			SoldSeats:    soldSeats,
			AvailSeats:   totalSeats - soldSeats,
			TotalRevenue: totalRevenue,
			TierCount:    len(event.Tiers),
			Tiers:        tierAnalytics,
			CreatedAt:    event.CreatedAt,
		}
	}

	return analytics, total, nil
}

// CreateEventTier creates a new tier for an event
func (s *EventManagementService) CreateEventTier(eventID, organizerID uuid.UUID, req *models.CreateEventTierRequest) (*models.EventTier, error) {
	// Verify event ownership
	var event models.Event
	if err := s.db.Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("event not found or you don't have permission")
		}
		return nil, err
	}

	// Check if event is cancelled
	if event.IsCancelled {
		return nil, fmt.Errorf("cannot add tiers to cancelled event")
	}

	tier := &models.EventTier{
		EventID:    eventID,
		TierName:   req.TierName,
		Price:      req.Price,
		Currency:   req.Currency,
		Quantity:   req.Quantity,
		Available:  req.Quantity,
		GST:        req.GST,
		SalesStart: req.SalesStart,
		SalesEnd:   req.SalesEnd,
		SortOrder:  req.SortOrder,
	}

	if tier.Currency == "" {
		tier.Currency = "USD"
	}

	if err := s.db.Create(tier).Error; err != nil {
		return nil, fmt.Errorf("failed to create event tier: %w", err)
	}

	return tier, nil
}

// UpdateEventTier updates an existing event tier
func (s *EventManagementService) UpdateEventTier(tierID, organizerID uuid.UUID, req *models.UpdateEventTierRequest) (*models.EventTier, error) {
	var tier models.EventTier

	// Find tier and verify ownership through event
	if err := s.db.Joins("JOIN events ON events.id = event_tiers.event_id").
		Where("event_tiers.id = ? AND events.organizer_id = ?", tierID, organizerID).
		First(&tier).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("tier not found or you don't have permission")
		}
		return nil, err
	}

	// Update fields if provided
	if req.TierName != "" {
		tier.TierName = req.TierName
	}
	if req.Price > 0 {
		tier.Price = req.Price
	}
	if req.Currency != "" {
		tier.Currency = req.Currency
	}
	if req.Quantity > 0 {
		// Adjust available based on quantity change
		sold := tier.Quantity - tier.Available
		tier.Quantity = req.Quantity
		tier.Available = tier.Quantity - sold
		if tier.Available < 0 {
			tier.Available = 0
		}
	}
	if req.GST >= 0 {
		tier.GST = req.GST
	}
	if req.SalesStart != nil {
		tier.SalesStart = req.SalesStart
	}
	if req.SalesEnd != nil {
		tier.SalesEnd = req.SalesEnd
	}
	if req.IsActive != nil {
		tier.IsActive = *req.IsActive
	}
	if req.SortOrder >= 0 {
		tier.SortOrder = req.SortOrder
	}

	if err := s.db.Save(&tier).Error; err != nil {
		return nil, fmt.Errorf("failed to update tier: %w", err)
	}

	return &tier, nil
}

// DeleteEventTier deletes an event tier
func (s *EventManagementService) DeleteEventTier(tierID, organizerID uuid.UUID) error {
	var tier models.EventTier

	// Find tier and verify ownership through event
	if err := s.db.Joins("JOIN events ON events.id = event_tiers.event_id").
		Where("event_tiers.id = ? AND events.organizer_id = ?", tierID, organizerID).
		First(&tier).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("tier not found or you don't have permission")
		}
		return err
	}

	// Check if any tickets have been sold for this tier
	if tier.Sold > 0 {
		return fmt.Errorf("cannot delete tier with sold tickets")
	}

	if err := s.db.Delete(&tier).Error; err != nil {
		return fmt.Errorf("failed to delete tier: %w", err)
	}

	return nil
}
