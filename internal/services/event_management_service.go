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
	db           *gorm.DB
	eventService *EventService
}

// NewEventManagementService creates a new event management service
func NewEventManagementService() *EventManagementService {
	return &EventManagementService{
		db:           database.DB,
		eventService: NewEventService(),
	}
}

// GetDB returns the database instance
func (s *EventManagementService) GetDB() *gorm.DB {
	return s.db
}

// ControlEventSales allows organizers to pause, resume, or stop sales
func (s *EventManagementService) ControlEventSales(eventID, organizerID uuid.UUID, req *models.EventSalesControlRequest) error {
	var event models.Event

	// Find the event and verify ownership
	if err := s.db.Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("event not found or you don't have permission")
		}
		return err
	}

	// Check if event is cancelled
	if event.IsCancelled {
		return fmt.Errorf("cannot control sales for cancelled event")
	}

	// Update sales status based on action
	oldSalesStatus := event.SalesStatus
	switch req.Action {
	case "pause":
		if event.SalesStatus == "paused" {
			return fmt.Errorf("event sales are already paused")
		}
		event.SalesStatus = "paused"
	case "resume":
		if event.SalesStatus == "active" {
			return fmt.Errorf("event sales are already active")
		}
		event.SalesStatus = "active"
	case "stop":
		if event.SalesStatus == "stopped" {
			return fmt.Errorf("event sales are already stopped")
		}
		event.SalesStatus = "stopped"
	default:
		return fmt.Errorf("invalid action: must be pause, resume, or stop")
	}

	if err := s.db.Save(&event).Error; err != nil {
		return fmt.Errorf("failed to update event sales status: %w", err)
	}

	// Log the sales status change to history
	organizerIDStr := organizerID.String()
	if err := s.eventService.LogStatusChange(eventID, oldSalesStatus, event.SalesStatus, "sales", organizerIDStr, req.Reason); err != nil {
		// Log the error but don't fail the operation
		fmt.Printf("[ERROR] Failed to log sales status change: %v\n", err)
	}

	return nil
}

// CancelEvent allows organizers to cancel their events
func (s *EventManagementService) CancelEvent(eventID, userID uuid.UUID, req *models.EventCancellationRequest, isAdmin bool) error {
	var event models.Event

	// Find the event - for admin, no ownership check needed
	query := s.db
	if !isAdmin {
		query = query.Where("organizer_id = ?", userID)
	}

	if err := query.Where("id = ?", eventID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if isAdmin {
				return fmt.Errorf("event not found")
			}
			return fmt.Errorf("Event not found or you don't have permission")
		}
		return err
	}

	// Check if event is already cancelled
	if event.IsCancelled {
		return fmt.Errorf("Event is already cancelled.")
	}

	// Check if event has already started
	if time.Now().After(event.StartDate) {
		return fmt.Errorf("Cannot cancel event that has already started.")
	}

	// Store old statuses for logging
	oldApprovalStatus := event.Status
	oldSalesStatus := event.SalesStatus

	// Cancel the event
	now := time.Now()
	event.IsCancelled = true
	event.CancelledAt = &now
	event.CancelReason = req.Reason
	event.Status = "cancelled"
	event.SalesStatus = "stopped"

	if err := s.db.Save(&event).Error; err != nil {
		return fmt.Errorf("failed to cancel event: %w", err)
	}

	// Log the approval status change to history
	userIDStr := userID.String()
	if err := s.eventService.LogStatusChange(eventID, oldApprovalStatus, event.Status, "approval", userIDStr, req.Reason); err != nil {
		// Log the error but don't fail the operation
		fmt.Printf("[ERROR] Failed to log approval status change for cancellation: %v\n", err)
	}

	// Log the sales status change to history
	if err := s.eventService.LogStatusChange(eventID, oldSalesStatus, event.SalesStatus, "sales", userIDStr, req.Reason); err != nil {
		// Log the error but don't fail the operation
		fmt.Printf("[ERROR] Failed to log sales status change for cancellation: %v\n", err)
	}

	// TODO: Send cancellation notifications to attendees
	// TODO: Process refunds if needed

	return nil
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

// GetAllEventsAnalytics returns analytics for all events of an organizer
func (s *EventManagementService) GetAllEventsAnalytics(organizerID uuid.UUID, page, limit int) ([]models.EventAnalyticsResponse, int64, error) {
	var events []models.Event
	var total int64

	// Get total count
	s.db.Model(&models.Event{}).Where("organizer_id = ? AND is_cancelled = ?", organizerID, false).Count(&total)

	// Get paginated events with tiers
	offset := (page - 1) * limit
	if err := s.db.Preload("Tiers").Where("organizer_id = ? AND is_cancelled = ?", organizerID, false).
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

// GetOrganizerTierTemplates returns all tier templates for an organizer
func (s *EventManagementService) GetOrganizerTierTemplates(organizerID uuid.UUID) ([]models.OrganizerTierTemplateResponse, error) {
	var templates []models.OrganizerTierTemplate
	if err := s.db.Where("organizer_id = ? AND is_active = ?", organizerID, true).
		Order("created_at DESC").
		Find(&templates).Error; err != nil {
		return nil, err
	}

	responses := make([]models.OrganizerTierTemplateResponse, len(templates))
	for i, template := range templates {
		responses[i] = models.OrganizerTierTemplateResponse{
			ID:           template.ID,
			OrganizerID:  template.OrganizerID,
			TemplateName: template.TemplateName,
			Description:  template.Description,
			IsActive:     template.IsActive,
			CreatedAt:    template.CreatedAt,
			UpdatedAt:    template.UpdatedAt,
		}
	}

	return responses, nil
}

// CreateOrganizerTierTemplate creates a new tier template for an organizer
func (s *EventManagementService) CreateOrganizerTierTemplate(organizerID uuid.UUID, req *models.CreateOrganizerTierTemplateRequest) error {
	// Check if template name already exists for this organizer
	var existing models.OrganizerTierTemplate
	if err := s.db.Where("organizer_id = ? AND template_name = ?", organizerID, req.TemplateName).First(&existing).Error; err == nil {
		return fmt.Errorf("tier template with name '%s' already exists", req.TemplateName)
	} else if err != gorm.ErrRecordNotFound {
		return err
	}

	template := &models.OrganizerTierTemplate{
		OrganizerID:  organizerID,
		TemplateName: req.TemplateName,
		Description:  req.Description,
		IsActive:     true,
	}

	if err := s.db.Create(template).Error; err != nil {
		return fmt.Errorf("failed to create tier template: %w", err)
	}

	return nil
}

// UpdateOrganizerTierTemplate updates an existing tier template
func (s *EventManagementService) UpdateOrganizerTierTemplate(templateID, organizerID uuid.UUID, req *models.UpdateOrganizerTierTemplateRequest) error {
	var template models.OrganizerTierTemplate
	if err := s.db.Where("id = ? AND organizer_id = ?", templateID, organizerID).First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("tier template not found or you don't have permission")
		}
		return err
	}

	// Check name uniqueness if changing name
	if req.TemplateName != "" && req.TemplateName != template.TemplateName {
		var existing models.OrganizerTierTemplate
		if err := s.db.Where("organizer_id = ? AND template_name = ? AND id != ?", organizerID, req.TemplateName, templateID).First(&existing).Error; err == nil {
			return fmt.Errorf("tier template with name '%s' already exists", req.TemplateName)
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		template.TemplateName = req.TemplateName
	}

	if req.Description != "" {
		template.Description = req.Description
	}
	if req.IsActive != nil {
		template.IsActive = *req.IsActive
	}

	if err := s.db.Save(&template).Error; err != nil {
		return fmt.Errorf("failed to update tier template: %w", err)
	}

	return nil
}

// DeleteOrganizerTierTemplate deletes a tier template
func (s *EventManagementService) DeleteOrganizerTierTemplate(templateID, organizerID uuid.UUID) error {
	var template models.OrganizerTierTemplate
	if err := s.db.Where("id = ? AND organizer_id = ?", templateID, organizerID).First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("tier template not found or you don't have permission")
		}
		return err
	}

	// Check if template is being used in any event tiers (code-level protection)
	var count int64
	if err := s.db.Model(&models.EventTier{}).
		Where("tier_template_id = ?", templateID).
		Count(&count).Error; err != nil {
		return fmt.Errorf("failed to check template usage: %w", err)
	}

	if count > 0 {
		return fmt.Errorf("cannot delete tier template: it is currently being used in %d event tier(s). Please remove it from all events first", count)
	}

	if err := s.db.Unscoped().Delete(&template).Error; err != nil {
		return fmt.Errorf("failed to delete tier template: %w", err)
	}

	return nil
}

// CreateEventTier creates a new tier for an event with minimal fields
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

	// Validate that the tier template exists and belongs to the organizer
	var template models.OrganizerTierTemplate
	if err := s.db.Where("id = ? AND organizer_id = ? AND is_active = ?", req.TierTemplateID, organizerID, true).First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("Tier template not found or you don't have permission to use it.")
		}
		return nil, err
	}

	// Check for duplicate tier name within the event
	var existingTier models.EventTier
	if err := s.db.Where("event_id = ? AND tier_name = ?", eventID, template.TemplateName).First(&existingTier).Error; err == nil {
		return nil, fmt.Errorf("Tier with name '%s' already exists for this event.", template.TemplateName)
	}

	// Create tier with template name and provided values
	tier := &models.EventTier{
		EventID:        eventID,
		TierTemplateID: req.TierTemplateID,
		TierName:       template.TemplateName,
		Price:          req.Price,
		Currency:       req.Currency,
		Quantity:       req.Quantity,
		Available:      req.Quantity,
		GST:            req.GST,
		SalesStart:     req.SalesStart,
		SalesEnd:       req.SalesEnd,
		SortOrder:      req.SortOrder,
	}

	// Set default currency if not provided
	if tier.Currency == "" {
		tier.Currency = "USD"
	}

	if err := s.db.Create(tier).Error; err != nil {
		return nil, fmt.Errorf("failed to create event tier: %w", err)
	}

	return tier, nil
}

func (s *EventManagementService) CreateEventTierWithTx(eventID, organizerID uuid.UUID, req *models.CreateEventTierRequest, tx *gorm.DB) (*models.EventTier, error) {
	// Verify event ownership
	var event models.Event
	if err := tx.Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("event not found or you don't have permission")
		}
		return nil, err
	}

	// Check if event is cancelled
	if event.IsCancelled {
		return nil, fmt.Errorf("cannot add tiers to cancelled event")
	}

	// Validate that the tier template exists and belongs to the organizer
	var template models.OrganizerTierTemplate
	if err := tx.Where("id = ? AND organizer_id = ? AND is_active = ?", req.TierTemplateID, organizerID, true).First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("tier template not found or you don't have permission to use it")
		}
		return nil, err
	}

	// Check for duplicate tier name within the event
	var existingTier models.EventTier
	if err := tx.Where("event_id = ? AND tier_name = ?", eventID, template.TemplateName).First(&existingTier).Error; err == nil {
		return nil, fmt.Errorf("Tier with name '%s' already exists for this event.", template.TemplateName)
	}

	// Create tier with template name and provided values
	tier := &models.EventTier{
		EventID:        eventID,
		TierTemplateID: req.TierTemplateID,
		TierName:       template.TemplateName,
		Price:          req.Price,
		Currency:       req.Currency,
		Quantity:       req.Quantity,
		Available:      req.Quantity,
		GST:            req.GST,
		SalesStart:     req.SalesStart,
		SalesEnd:       req.SalesEnd,
		SortOrder:      req.SortOrder,
	}

	// Set default currency if not provided
	if tier.Currency == "" {
		tier.Currency = "USD"
	}

	if err := tx.Create(tier).Error; err != nil {
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

	// Validate tier template if being changed
	if req.TierTemplateID != nil {
		var template models.OrganizerTierTemplate
		if err := s.db.Where("id = ? AND organizer_id = ? AND is_active = ?", *req.TierTemplateID, organizerID, true).First(&template).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("Tier template not found or you don't have permission to use it.")
			}
			return nil, err
		}
		// Check for duplicate tier name within the event, excluding current tier
		var existingTier models.EventTier
		if err := s.db.Where("event_id = ? AND tier_name = ? AND id != ?", tier.EventID, template.TemplateName, tierID).First(&existingTier).Error; err == nil {
			return nil, fmt.Errorf("Tier with name '%s' already exists for this event.", template.TemplateName)
		}
		tier.TierTemplateID = *req.TierTemplateID
		tier.TierName = template.TemplateName
	}

	// Update fields if provided
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
			return fmt.Errorf("Tier not found or you don't have permission.")
		}
		return err
	}

	// Check if any tickets have been sold for this tier
	if tier.Sold > 0 {
		return fmt.Errorf("Cannot delete tier with sold tickets.")
	}

	if err := s.db.Delete(&tier).Error; err != nil {
		return fmt.Errorf("failed to delete tier: %w", err)
	}

	return nil
}
