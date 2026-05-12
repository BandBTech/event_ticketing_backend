package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/utils"

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
			return utils.NewNotFoundError("event")
		}
		return utils.NewDatabaseError("Failed to retrieve event.", err)
	}

	// Check if event is cancelled
	if event.IsCancelled {
		return utils.NewBusinessLogicError("Cannot control sales for cancelled event.")
	}

	// Update sales status based on action.
	var targetStatus string
	var targetSalesStatus string
	switch req.Action {
	case "pause":
		if event.SalesStatus == models.EventSalesStatusPaused.String() {
			return utils.NewBusinessLogicError("Event sales are already paused.")
		}
		targetSalesStatus = models.EventSalesStatusPaused.String()
		targetStatus = models.EventStatusHold.String()
	case "resume":
		if event.SalesStatus == models.EventSalesStatusActive.String() {
			return utils.NewBusinessLogicError("Event sales are already active.")
		}
		targetSalesStatus = models.EventSalesStatusActive.String()
		targetStatus = models.EventStatusOnSale.String()
	case "stop":
		if event.SalesStatus == models.EventSalesStatusStopped.String() {
			return utils.NewBusinessLogicError("Event sales are already stopped.")
		}
		targetSalesStatus = models.EventSalesStatusStopped.String()
		targetStatus = models.EventStatusSalesEnd.String()
	default:
		return utils.NewValidationError("Invalid action: must be pause, resume, or stop.", nil)
	}

	// Use central function to update both status and sales status with logging
	err := s.eventService.UpdateEventStatusAndSalesStatusWithLogging(
		eventID,
		targetStatus,
		targetSalesStatus,
		models.EventStatusTypeManual.String(),
		organizerID.String(),
		req.Reason,
	)
	if err != nil {
		return utils.NewDatabaseError("Failed to update event sales status.", err)
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
				return utils.NewNotFoundError("event")
			}
			return utils.NewForbiddenError("Event not found or you don't have permission.")
		}
		return utils.NewDatabaseError("Failed to retrieve event.", err)
	}

	// Check if event is already cancelled
	if event.IsCancelled {
		return utils.NewBusinessLogicError("Event is already cancelled.")
	}

	// Check if event has already started
	if time.Now().After(event.StartDate) {
		return utils.NewBusinessLogicError("Cannot cancel event that has already started.")
	}

	// Check cancellation criteria: allow if status is pending/approved OR if no tickets sold
	canCancel := false
	reason := ""

	if event.Status == models.EventStatusPending.String() || event.Status == models.EventStatusApproved.String() {
		canCancel = true
		reason = "Event is in early approval stage"
	} else {
		// Check if any tickets have been sold
		var soldTickets int64
		if err := s.db.Model(&models.Ticket{}).
			Where("event_id = ? AND status IN ('active', 'used') AND deleted_at IS NULL", eventID).
			Count(&soldTickets).Error; err != nil {
			return utils.NewDatabaseError("Failed to check ticket sales.", err)
		}

		if soldTickets == 0 {
			canCancel = true
			reason = "No tickets have been sold yet"
		} else {
			canCancel = false
			reason = fmt.Sprintf("Event has %d tickets sold and is in %s status", soldTickets, event.Status)
		}
	}

	if !canCancel {
		return utils.NewBusinessLogicError(fmt.Sprintf("Cannot cancel event: %s. Events can only be cancelled when status is 'pending' or 'approved', or when no tickets have been sold.", reason))
	}

	// Use central function to cancel event with logging
	err := s.eventService.CancelEventWithLogging(
		eventID,
		req.Reason,
		models.EventStatusTypeApproval.String(),
		userID.String(),
		req.Reason,
	)
	if err != nil {
		return utils.NewDatabaseError("Failed to cancel event.", err)
	}

	// TODO: Send cancellation notifications to attendees
	// TODO: Process refunds if needed

	return nil
}

// GetEventAnalytics returns comprehensive analytics for an event
func (s *EventManagementService) GetEventAnalytics(eventID, organizerID uuid.UUID) (*models.EventAnalyticsResponse, error) {
	var event models.Event

	// Find the event with tiers
	if err := s.db.Scopes(models.WithTiers).Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("event")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve event.", err)
	}

	return s.buildEventAnalytics(&event)
}

// AdminGetEventAnalytics returns comprehensive analytics for an event (Admin access - no organizer scoping)
func (s *EventManagementService) AdminGetEventAnalytics(eventID uuid.UUID) (*models.EventAnalyticsResponse, error) {
	var event models.Event

	// Find the event with tiers (no organizer scoping for admin)
	if err := s.db.Scopes(models.WithTiers).Where("id = ?", eventID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("event")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve event.", err)
	}

	return s.buildEventAnalytics(&event)
}

// buildEventAnalytics is a helper method to build analytics for an event (eliminates code duplication)
// Uses transaction isolation to prevent race conditions between aggregate and per-tier queries
func (s *EventManagementService) buildEventAnalytics(event *models.Event) (*models.EventAnalyticsResponse, error) {
	// Calculate total seats from tiers (fixed capacity)
	totalSeats := 0
	for _, tier := range event.Tiers {
		totalSeats += tier.Quantity
	}

	// Get tier analytics from transactions (with atomic consistency)
	// Calculate totals by summing individual tier data instead of separate aggregate query
	// This prevents race conditions where data changes between queries
	tierAnalytics := make([]models.EventTierAnalytics, len(event.Tiers))
	totalSoldSeats := 0
	totalRevenue := 0.0

	for i, tier := range event.Tiers {
		var tierSummary struct {
			SoldSeats int     `json:"sold_seats"`
			Revenue   float64 `json:"revenue"`
		}

		// Count TICKETS by tier instead of transactions
		// This works with the new single-transaction-per-purchase model
		// Tickets are linked to transactions via transaction_id, and each ticket has a tier_id
		if err := s.db.Model(&models.Ticket{}).
			Joins("JOIN event_tiers ON tickets.tier_id = event_tiers.id").
			Select("COALESCE(COUNT(*), 0) as sold_seats, COALESCE(SUM(event_tiers.price), 0) as revenue").
			Where("tickets.event_id = ? AND tickets.tier_id = ? AND tickets.status IN ('active', 'used') AND tickets.deleted_at IS NULL",
				event.ID, tier.ID).
			Scan(&tierSummary).Error; err != nil {
			return nil, utils.NewDatabaseError("Failed to calculate tier analytics from tickets.", err)
		}

		availSeats := tier.Quantity - tierSummary.SoldSeats
		commissionEarning := tierSummary.Revenue * event.CommissionRate / 100
		tierAnalytics[i] = models.EventTierAnalytics{
			TierID:            tier.ID,
			TierName:          tier.TierName,
			Price:             tier.Price,
			Currency:          tier.Currency,
			TotalSeats:        tier.Quantity,
			SoldSeats:         tierSummary.SoldSeats,
			AvailSeats:        availSeats,
			Revenue:           tierSummary.Revenue,
			SalesStart:        tier.SalesStart,
			SalesEnd:          tier.SalesEnd,
			IsActive:          tier.IsActive,
			CommissionEarning: commissionEarning,
		}

		// Accumulate totals from tier data for consistency
		totalSoldSeats += tierSummary.SoldSeats
		totalRevenue += tierSummary.Revenue
	}

	commissionEarning := totalRevenue * event.CommissionRate / 100
	return &models.EventAnalyticsResponse{
		EventID:           event.ID,
		EventTitle:        event.Title,
		EventStatus:       event.Status,
		SalesStatus:       event.SalesStatus,
		TotalSeats:        totalSeats,
		SoldSeats:         totalSoldSeats, // Sum of tier data, not separate query
		AvailSeats:        totalSeats - totalSoldSeats,
		TotalRevenue:      totalRevenue, // Sum of tier data, not separate query
		CommissionRate:    event.CommissionRate,
		CommissionEarning: commissionEarning,
		OrganizerShare:    totalRevenue - commissionEarning,
		TierCount:         len(event.Tiers),
		Tiers:             tierAnalytics,
		CreatedAt:         event.CreatedAt,
	}, nil
}

// GetAllEventsAnalytics returns analytics for all events of an organizer with accurate transaction-based calculations
func (s *EventManagementService) GetAllEventsAnalytics(organizerID uuid.UUID, page, limit int) ([]models.EventAnalyticsResponse, int64, error) {
	var events []models.Event
	var total int64

	// Get total count
	s.db.Model(&models.Event{}).Where("organizer_id = ? AND is_cancelled = ?", organizerID, false).Count(&total)

	// Get paginated events with tiers
	offset := (page - 1) * limit
	if err := s.db.Scopes(models.WithTiers).Where("organizer_id = ? AND is_cancelled = ?", organizerID, false).
		Offset(offset).Limit(limit).Find(&events).Error; err != nil {
		return nil, 0, err
	}

	analytics := make([]models.EventAnalyticsResponse, len(events))
	for i, event := range events {
		// Use the same calculation logic as buildEventAnalytics to ensure consistency
		// Calculate total seats from tiers (fixed capacity)
		totalSeats := 0
		for _, tier := range event.Tiers {
			totalSeats += tier.Quantity
		}

		// Get tier analytics from transactions (consistent with buildEventAnalytics)
		tierAnalytics := make([]models.EventTierAnalytics, len(event.Tiers))
		totalSoldSeats := 0
		totalRevenue := 0.0

		for j, tier := range event.Tiers {
			var tierSummary struct {
				SoldSeats int     `json:"sold_seats"`
				Revenue   float64 `json:"revenue"`
			}

			if err := s.db.Model(&models.Ticket{}).
				Joins("JOIN event_tiers ON tickets.tier_id = event_tiers.id").
				Select("COALESCE(COUNT(*), 0) as sold_seats, COALESCE(SUM(event_tiers.price), 0) as revenue").
				Where("tickets.event_id = ? AND tickets.tier_id = ? AND tickets.status IN ('active', 'used') AND tickets.deleted_at IS NULL",
					event.ID, tier.ID).
				Scan(&tierSummary).Error; err != nil {
				return nil, 0, utils.NewDatabaseError("Failed to calculate tier analytics from tickets.", err)
			}

			availSeats := tier.Quantity - tierSummary.SoldSeats
			commissionEarning := tierSummary.Revenue * event.CommissionRate / 100
			tierAnalytics[j] = models.EventTierAnalytics{
				TierID:            tier.ID,
				TierName:          tier.TierName,
				Price:             tier.Price,
				Currency:          tier.Currency,
				TotalSeats:        tier.Quantity,
				SoldSeats:         tierSummary.SoldSeats,
				AvailSeats:        availSeats,
				Revenue:           tierSummary.Revenue,
				SalesStart:        tier.SalesStart,
				SalesEnd:          tier.SalesEnd,
				IsActive:          tier.IsActive,
				CommissionEarning: commissionEarning,
			}

			// Accumulate totals from tier data for consistency
			totalSoldSeats += tierSummary.SoldSeats
			totalRevenue += tierSummary.Revenue
		}

		commissionEarning := totalRevenue * event.CommissionRate / 100
		analytics[i] = models.EventAnalyticsResponse{
			EventID:           event.ID,
			EventTitle:        event.Title,
			EventStatus:       event.Status,
			SalesStatus:       event.SalesStatus,
			TotalSeats:        totalSeats,
			SoldSeats:         totalSoldSeats,
			AvailSeats:        totalSeats - totalSoldSeats,
			TotalRevenue:      totalRevenue,
			CommissionRate:    event.CommissionRate,
			CommissionEarning: commissionEarning,
			OrganizerShare:    totalRevenue - commissionEarning,
			TierCount:         len(event.Tiers),
			Tiers:             tierAnalytics,
			CreatedAt:         event.CreatedAt,
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
		return utils.NewConflictError(fmt.Sprintf("Tier template with name '%s' already exists.", req.TemplateName))
	} else if err != gorm.ErrRecordNotFound {
		return utils.NewDatabaseError("Failed to check existing tier template.", err)
	}

	template := &models.OrganizerTierTemplate{
		OrganizerID:  organizerID,
		TemplateName: req.TemplateName,
		Description:  req.Description,
		IsActive:     true,
	}

	if err := s.db.Create(template).Error; err != nil {
		return utils.NewDatabaseError("Failed to create tier template.", err)
	}

	return nil
}

// UpdateOrganizerTierTemplate updates an existing tier template
func (s *EventManagementService) UpdateOrganizerTierTemplate(templateID, organizerID uuid.UUID, req *models.UpdateOrganizerTierTemplateRequest) error {
	var template models.OrganizerTierTemplate
	if err := s.db.Where("id = ? AND organizer_id = ?", templateID, organizerID).First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewNotFoundError("tier template")
		}
		return utils.NewDatabaseError("Failed to retrieve tier template.", err)
	}

	// Check name uniqueness if changing name
	if req.TemplateName != "" && req.TemplateName != template.TemplateName {
		var existing models.OrganizerTierTemplate
		if err := s.db.Where("organizer_id = ? AND template_name = ? AND id != ?", organizerID, req.TemplateName, templateID).First(&existing).Error; err == nil {
			return utils.NewConflictError(fmt.Sprintf("Tier template with name '%s' already exists.", req.TemplateName))
		} else if err != gorm.ErrRecordNotFound {
			return utils.NewDatabaseError("Failed to check existing tier template.", err)
		}
		template.TemplateName = req.TemplateName
	}

	if req.Description != nil {
		template.Description = *req.Description
	}
	if req.IsActive != nil {
		template.IsActive = *req.IsActive
	}

	if err := s.db.Save(&template).Error; err != nil {
		return utils.NewDatabaseError("Failed to update tier template.", err)
	}

	return nil
}

// DeleteOrganizerTierTemplate deletes a tier template
func (s *EventManagementService) DeleteOrganizerTierTemplate(templateID, organizerID uuid.UUID) error {
	var template models.OrganizerTierTemplate
	if err := s.db.Where("id = ? AND organizer_id = ?", templateID, organizerID).First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.NewNotFoundError("tier template")
		}
		return utils.NewDatabaseError("Failed to retrieve tier template.", err)
	}

	// Check if template is being used in any event tiers (code-level protection)
	var count int64
	if err := s.db.Model(&models.EventTier{}).
		Where("tier_template_id = ?", templateID).
		Count(&count).Error; err != nil {
		return utils.NewDatabaseError("Failed to check template usage.", err)
	}

	if count > 0 {
		return utils.NewBusinessLogicError(fmt.Sprintf("Cannot delete tier template: it is currently being used in %d event tier(s). Please remove it from all events first.", count))
	}

	if err := s.db.Unscoped().Delete(&template).Error; err != nil {
		return utils.NewDatabaseError("Failed to delete tier template.", err)
	}

	return nil
}

// CreateEventTier creates a new tier for an event with minimal fields
func (s *EventManagementService) CreateEventTier(eventID, organizerID uuid.UUID, req *models.CreateEventTierRequest) (*models.EventTier, error) {
	// Verify event ownership
	var event models.Event
	if err := s.db.Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("event")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve event.", err)
	}

	// Check if event is cancelled
	if event.IsCancelled {
		return nil, utils.NewBusinessLogicError("Cannot add tiers to cancelled event.")
	}

	// Validate tier limits
	if req.Price > 10000 {
		return nil, utils.NewValidationError("Tier price cannot exceed $10,000", nil)
	}

	if req.Quantity > 100000 {
		return nil, utils.NewValidationError("Tier quantity cannot exceed 100,000", nil)
	}

	// Validate that the tier template exists and belongs to the organizer
	var template models.OrganizerTierTemplate
	if err := s.db.Where("id = ? AND organizer_id = ? AND is_active = ?", req.TierTemplateID, organizerID, true).First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("tier template")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve tier template.", err)
	}

	// Check for duplicate tier name within the event
	var existingTier models.EventTier
	if err := s.db.Where("event_id = ? AND tier_name = ?", eventID, template.TemplateName).First(&existingTier).Error; err == nil {
		return nil, utils.NewConflictError(fmt.Sprintf("Tier with name '%s' already exists for this event.", template.TemplateName))
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
		return nil, utils.NewDatabaseError("Failed to create event tier.", err)
	}

	return tier, nil
}

func (s *EventManagementService) CreateEventTierWithTx(eventID, organizerID uuid.UUID, req *models.CreateEventTierRequest, tx *gorm.DB) (*models.EventTier, error) {
	// Verify event ownership
	var event models.Event
	if err := tx.Where("id = ? AND organizer_id = ?", eventID, organizerID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("event")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve event.", err)
	}

	// Check if event is cancelled
	if event.IsCancelled {
		return nil, utils.NewBusinessLogicError("Cannot add tiers to cancelled event.")
	}

	// Validate that the tier template exists and belongs to the organizer
	var template models.OrganizerTierTemplate
	if err := tx.Where("id = ? AND organizer_id = ? AND is_active = ?", req.TierTemplateID, organizerID, true).First(&template).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, utils.NewNotFoundError("tier template")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve tier template.", err)
	}

	// Check for duplicate tier name within the event
	var existingTier models.EventTier
	if err := tx.Where("event_id = ? AND tier_name = ?", eventID, template.TemplateName).First(&existingTier).Error; err == nil {
		return nil, utils.NewConflictError(fmt.Sprintf("Tier with name '%s' already exists for this event.", template.TemplateName))
	}

	// Create tier with template name and provided values
	tier := &models.EventTier{
		EventID:        eventID,
		TierTemplateID: req.TierTemplateID,
		TierName:       template.TemplateName,
		Price:          req.Price,
		Currency:       strings.ToUpper(req.Currency),
		Quantity:       req.Quantity,
		Available:      req.Quantity,
		GST:            req.GST,
		SalesStart:     req.SalesStart,
		SalesEnd:       req.SalesEnd,
		SortOrder:      req.SortOrder,
	}

	// Inherit currency from event if not provided
	if tier.Currency == "" {
		tier.Currency = event.Currency
	}

	// Set default currency if not provided
	if tier.Currency == "" {
		tier.Currency = "USD"
	}

	if err := tx.Create(tier).Error; err != nil {
		return nil, utils.NewDatabaseError("Failed to create event tier.", err)
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
			return nil, utils.NewNotFoundError("tier")
		}
		return nil, utils.NewDatabaseError("Failed to retrieve tier.", err)
	}

	// Validate tier template if being changed
	if req.TierTemplateID != nil {
		var template models.OrganizerTierTemplate
		if err := s.db.Where("id = ? AND organizer_id = ? AND is_active = ?", *req.TierTemplateID, organizerID, true).First(&template).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, utils.NewNotFoundError("tier template")
			}
			return nil, utils.NewDatabaseError("Failed to retrieve tier template.", err)
		}
		// Check for duplicate tier name within the event, excluding current tier
		var existingTier models.EventTier
		if err := s.db.Where("event_id = ? AND tier_name = ? AND id != ?", tier.EventID, template.TemplateName, tierID).First(&existingTier).Error; err == nil {
			return nil, utils.NewConflictError(fmt.Sprintf("Tier with name '%s' already exists for this event.", template.TemplateName))
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
		return nil, utils.NewDatabaseError("Failed to update tier.", err)
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
			return utils.NewNotFoundError("tier")
		}
		return utils.NewDatabaseError("Failed to retrieve tier.", err)
	}

	// Check if any tickets have been sold for this tier
	if tier.Sold > 0 {
		return utils.NewBusinessLogicError("Cannot delete tier with sold tickets.")
	}

	if err := s.db.Delete(&tier).Error; err != nil {
		return utils.NewDatabaseError("Failed to delete tier.", err)
	}

	return nil
}
