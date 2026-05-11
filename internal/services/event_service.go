package services

import (
	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/state"
	"event-ticketing-backend/pkg/utils"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EventService struct{}

func NewEventService() *EventService {
	return &EventService{}
}

func (s *EventService) CreateEventWithTx(req *models.EventCreateRequest, organizerID string, tx *gorm.DB) (*models.Event, error) {
	// Parse the organizer ID to UUID
	organizerUUID, err := uuid.Parse(organizerID)
	if err != nil {
		return nil, utils.NewBusinessLogicError("Invalid organizer ID format.")
	}

	// Get user details to check if they are admin
	var user models.User
	if err := tx.Preload("Roles").Where("id = ?", organizerUUID).First(&user).Error; err != nil {
		return nil, utils.NewNotFoundError("user")
	}

	// Check if user has admin role - admins cannot create events, only review them
	for _, role := range user.Roles {
		if role.Name == "admin" {
			return nil, utils.NewBusinessLogicError("Admins cannot create events. Only organizers can create events, and admins review them.")
		}
	}

	// Set status to pending for organizer-created events
	status := models.EventStatusPending.String()

	// Trim category
	categoryStr := strings.TrimSpace(req.Category)

	event := &models.Event{
		Title:          req.Title,
		Description:    req.Description,
		BannerImage:    req.BannerImage,
		Category:       categoryStr,
		EventType:      req.EventType,
		VenueName:      req.VenueName,
		Address:        req.Address,
		Country:        req.Country,
		StartDate:      req.StartDate,
		EndDate:        req.EndDate,
		Timezone:       req.Timezone,
		Price:          req.Price,
		Currency:       req.Currency,
		Capacity:       req.Capacity,
		CommissionRate: req.CommissionRate,
		OrganizerID:    organizerUUID,
		Status:         status,
	}

	// Set default commission rate if not provided
	if event.CommissionRate == 0 {
		event.CommissionRate = 10 // Default 10%
	}

	if err := tx.Create(event).Error; err != nil {
		return nil, err
	}

	if err := s.createEventDaysWithTx(event, tx); err != nil {
		return nil, err
	}

	// Log initial event status history in the same transaction
	if err := s.LogStatusChangeTx(
		tx,
		event.ID,
		models.EventStatusDraft.String(),
		models.EventStatusPending.String(),
		models.EventStatusTypeApproval.String(),
		organizerID,
		"Event created and submitted for approval",
	); err != nil {
		return nil, err
	}

	return event, nil
}

func (s *EventService) createEventDaysWithTx(event *models.Event, tx *gorm.DB) error {
	if event == nil {
		return nil
	}

	loc := time.UTC
	if event.Timezone != "" {
		if parsedLoc, err := time.LoadLocation(event.Timezone); err == nil {
			loc = parsedLoc
		}
	}

	startLocal := event.StartDate.In(loc)
	endLocal := event.EndDate.In(loc)

	startDay := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, loc)
	endDay := time.Date(endLocal.Year(), endLocal.Month(), endLocal.Day(), 0, 0, 0, 0, loc)

	var eventDays []models.EventDay
	dayIndex := 1

	for day := startDay; !day.After(endDay); day = day.AddDate(0, 0, 1) {
		dayStart := day
		nextDay := day.AddDate(0, 0, 1)
		dayEnd := nextDay

		if day.Equal(startDay) {
			dayStart = startLocal
		}
		if day.Equal(endDay) {
			dayEnd = endLocal
		}

		if !dayEnd.After(dayStart) {
			continue
		}

		eventDays = append(eventDays, models.EventDay{
			ID:        uuid.New(),
			EventID:   event.ID,
			Name:      fmt.Sprintf("Day %d", dayIndex),
			StartTime: dayStart.UTC(),
			EndTime:   dayEnd.UTC(),
		})
		dayIndex++
	}

	if len(eventDays) == 0 {
		eventDays = append(eventDays, models.EventDay{
			ID:        uuid.New(),
			EventID:   event.ID,
			Name:      "Day 1",
			StartTime: event.StartDate,
			EndTime:   event.EndDate,
		})
	}

	return tx.Create(&eventDays).Error
}

func (s *EventService) GetEventByID(id uuid.UUID) (*models.Event, error) {
	var event models.Event
	if err := database.DB.First(&event, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

// GetPublicEventByID gets an event by ID with tiers preloaded (for public APIs) - returns scheduled, on_sale, sales_upcoming, hold events
// Scheduled events are viewable but NOT purchasable (validation in purchase flow prevents this)
func (s *EventService) GetPublicEventByID(id uuid.UUID) (*models.Event, error) {
	var event models.Event

	// Include scheduled (viewable only), on_sale, sales_upcoming, hold events
	// Purchase validation prevents buying from scheduled/sales_upcoming events
	if err := database.DB.Preload("Tiers").
		Where("status IN (?) AND id = ?", []string{"scheduled", "on_sale", "sales_upcoming", "hold"}, id).First(&event).Error; err != nil {
		return nil, err
	}

	// Manually load organizer and onboarding
	if event.OrganizerID != uuid.Nil {
		var organizer models.User
		if err := database.DB.Preload("OrganizerOnboarding").Where("id = ? AND deleted_at IS NULL", event.OrganizerID).First(&organizer).Error; err == nil {
			event.Organizer = &organizer
		}
	}

	return &event, nil
}

func (s *EventService) UpdateEvent(id uuid.UUID, req *models.EventUpdateRequest) (*models.Event, error) {
	var event models.Event
	if err := database.DB.First(&event, "id = ?", id).Error; err != nil {
		return nil, err
	}

	if req.Title != "" {
		event.Title = req.Title
	}
	if req.Description != "" {
		event.Description = req.Description
	}
	if req.BannerImage != "" {
		event.BannerImage = req.BannerImage
	}
	if strings.TrimSpace(req.Category) != "" {
		event.Category = strings.TrimSpace(req.Category)
	}
	if strings.TrimSpace(req.EventType) != "" {
		event.EventType = strings.TrimSpace(req.EventType)
	}
	if req.VenueName != "" {
		event.VenueName = req.VenueName
	}
	if req.Address != "" {
		event.Address = req.Address
	}
	if req.Country != "" {
		event.Country = req.Country
	}
	if req.Timezone != "" {
		event.Timezone = req.Timezone
	}
	if !req.StartDate.IsZero() {
		event.StartDate = req.StartDate
	}
	if !req.EndDate.IsZero() {
		event.EndDate = req.EndDate
	}
	if req.Price > 0 {
		event.Price = req.Price
	}
	if req.Currency != "" {
		event.Currency = strings.ToUpper(req.Currency)
	}
	if req.Capacity > 0 {
		event.Capacity = req.Capacity
	}
	if req.Status != "" {
		event.Status = req.Status
	}
	// Commission rate is immutable once set
	if req.CommissionRate > 0 && event.CommissionRate == 0 {
		event.CommissionRate = req.CommissionRate
	}

	if err := database.DB.Save(&event).Error; err != nil {
		return nil, err
	}

	return &event, nil
}

func (s *EventService) DeleteEvent(id uuid.UUID) error {
	return database.DB.Delete(&models.Event{}, "id = ?", id).Error
}

// UpdateEventStatus allows admin/subadmin to update event status, commission rate, and admin remarks
func (s *EventService) UpdateEventStatus(eventID uuid.UUID, userID string, status string, commissionRate *float64, adminRemark string) (*models.Event, error) {
	// Check if user has admin or subadmin role
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, utils.NewBusinessLogicError("Invalid user ID format.")
	}

	var user models.User
	if err := database.DB.Preload("Roles").Where("id = ?", userUUID).First(&user).Error; err != nil {
		return nil, utils.NewNotFoundError("user")
	}

	// Check if user has admin or subadmin role
	hasPermission := false
	for _, role := range user.Roles {
		if role.Name == "admin" || role.Name == "subadmin" {
			hasPermission = true
			break
		}
	}

	if !hasPermission {
		return nil, utils.NewBusinessLogicError("Insufficient permissions: only admin or subadmin can approve events.")
	}

	// Get the event with tiers
	var event models.Event
	if err := database.DB.Preload("Tiers").First(&event, "id = ?", eventID).Error; err != nil {
		return nil, utils.NewNotFoundError("event")
	}

	// Validate status transition - admin can change most statuses except for completed events
	if event.Status == models.EventStatusCancelled.String() && status != models.EventStatusCancelled.String() {
		return nil, utils.NewBusinessLogicError("cancelled events cannot be changed to other statuses")
	}

	// Update event status, commission rate, and remark
	oldStatus := event.Status

	// When admin approves an event (status = "approved"), change it to "scheduled" or "on_sale" depending on tier sales windows
	finalStatus := status
	if status == models.EventStatusApproved.String() {
		finalStatus = models.EventStatusScheduled.String()
		now := time.Now().UTC()
		for _, tier := range event.Tiers {
			if tier.SalesStart != nil && tier.SalesEnd != nil && !now.Before(*tier.SalesStart) && now.Before(*tier.SalesEnd) {
				finalStatus = models.EventStatusOnSale.String()
				break
			}
		}
	}

	if !models.IsValidEventStatus(finalStatus) {
		return nil, utils.NewBusinessLogicError("invalid event status transition target")
	}

	// Validate status transitions through event state machine
	sm := state.NewStateMachine(state.EventTransitions)
	if err := sm.Transition(models.EventStatus(oldStatus), models.EventStatus(finalStatus)); err != nil {
		return nil, utils.NewBusinessLogicError(fmt.Sprintf("Invalid event status transition: %s -> %s", oldStatus, finalStatus))
	}

	// Determine sales status based on final status
	finalSalesStatus := event.SalesStatus
	if finalStatus == models.EventStatusOnSale.String() {
		// Ensure sales status is active for manual on_sale updates
		finalSalesStatus = models.EventSalesStatusActive.String()
	}

	event.Status = finalStatus
	event.SalesStatus = finalSalesStatus
	event.AdminRemark = adminRemark

	// Update commission rate if provided (allow override of existing rate)
	if commissionRate != nil {
		event.CommissionRate = *commissionRate
	}

	if err := database.DB.Save(&event).Error; err != nil {
		return nil, err
	}

	// Use central function to log the status change
	if oldStatus != finalStatus {
		err := s.UpdateEventStatusWithLogging(eventID, finalStatus, models.EventStatusTypeApproval.String(), userID, adminRemark)
		if err != nil {
			// Log the error but don't fail the operation
			fmt.Printf("[ERROR] Failed to log status change: %v\n", err)
		}
	}

	return &event, nil
} // GetEventsByStatus gets events by status with pagination and sorting
func (s *EventService) GetEventsByStatus(status string, page, limit int, sortParam string) ([]models.Event, int64, error) {
	var events []models.Event
	var total int64
	offset := (page - 1) * limit

	db := database.DB.Model(&models.Event{}).Where("LOWER(status) = LOWER(?)", status)
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Parse and apply sorting - always prioritize featured events first
	validSortFields := map[string]bool{
		"title": true, "start_date": true, "price": true, "created_at": true, "is_featured": true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, validSortFields, "created_at", "desc")

	// Always prioritize featured events first, then apply the requested sort
	orderClause := "is_featured DESC, " + sortBy + " " + sortOrder

	if err := db.Order(orderClause).Offset(offset).Limit(limit).Find(&events).Error; err != nil {
		return nil, 0, err
	}

	return events, total, nil
}

// GetFilteredEvents returns filtered and paginated events
func (s *EventService) GetFilteredEvents(status string, page, limit int, search, location, startDate, endDate string, minPrice, maxPrice *float64, sortBy, sortOrder string, organizerID string) ([]models.Event, int64, error) {
	var events []models.Event
	var total int64
	offset := (page - 1) * limit

	db := database.DB.Model(&models.Event{}).Where("deleted_at IS NULL")

	// Apply status filter only if status is provided
	if status != "" {
		db = db.Where("LOWER(status) = LOWER(?)", status)
	}

	// Apply search filter
	if search != "" {
		db = db.Where("title ILIKE ? OR description ILIKE ?", "%"+search+"%", "%"+search+"%")
	}

	// Apply location filter
	if location != "" {
		db = db.Where("location ILIKE ?", "%"+location+"%")
	}

	// Apply date filters
	if startDate != "" {
		db = db.Where("start_date >= ?", startDate)
	}
	if endDate != "" {
		db = db.Where("end_date <= ?", endDate)
	}

	// Apply price filters
	if minPrice != nil {
		db = db.Where("price >= ?", *minPrice)
	}
	if maxPrice != nil {
		db = db.Where("price <= ?", *maxPrice)
	}

	// Apply organizer filter
	if organizerID != "" {
		db = db.Where("organizer_id = ?", organizerID)
	}

	// Count total records
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Apply sorting
	orderClause := sortBy + " " + sortOrder
	query := db.Offset(offset).Limit(limit).Order(orderClause)

	// Always preload tiers for accurate ticket sales calculation
	query = query.Preload("Tiers")

	if err := query.Find(&events).Error; err != nil {
		return nil, 0, err
	}

	// Calculate real-time ticket sales for each event
	for i := range events {
		s.calculateEventTicketSales(&events[i])
	}

	return events, total, nil
}

// GetPublicEvents returns public events including scheduled, on_sale, sales_upcoming, hold events (or filtered by status if provided)
func (s *EventService) GetPublicEvents(page, limit int, search, location, statusFilter, startDate, endDate string, minPrice, maxPrice *float64, sortBy, sortOrder string) ([]models.Event, int64, error) {
	var events []models.Event
	var total int64
	offset := (page - 1) * limit

	db := database.DB.Model(&models.Event{})

	// Include events that are publicly viewable: scheduled, on_sale, sales_upcoming, hold
	// If statusFilter is provided, use it; otherwise use default public statuses
	var statusList []string
	if statusFilter != "" {
		// Validate that the requested status is allowed for public viewing
		validPublicStatuses := map[string]bool{
			"scheduled": true, "on_sale": true, "sales_upcoming": true, "hold": true,
		}
		if validPublicStatuses[statusFilter] {
			statusList = []string{statusFilter}
		} else {
			// If invalid status requested, return empty result
			return []models.Event{}, 0, nil
		}
	} else {
		// Default public statuses
		statusList = []string{"scheduled", "on_sale", "sales_upcoming", "hold"}
	}
	db = db.Where("status IN (?)", statusList)

	// Apply search filter
	if search != "" {
		db = db.Where("title ILIKE ? OR description ILIKE ?", "%"+search+"%", "%"+search+"%")
	}

	// Apply location filter
	if location != "" {
		db = db.Where("location ILIKE ?", "%"+location+"%")
	}

	// Apply date filters
	if startDate != "" {
		db = db.Where("start_date >= ?", startDate)
	}
	if endDate != "" {
		db = db.Where("end_date <= ?", endDate)
	}

	// Apply price filters
	if minPrice != nil {
		db = db.Where("price >= ?", *minPrice)
	}
	if maxPrice != nil {
		db = db.Where("price <= ?", *maxPrice)
	}

	// Count total records
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Apply sorting - featured events first in alphabetical order, then non-featured events in alphabetical order
	orderClause := "is_featured DESC, LOWER(title) ASC"
	query := db.Offset(offset).Limit(limit).Order(orderClause)

	// Preload tiers for public events (scheduled, on_sale, sales_upcoming, hold events)
	query = query.Preload("Tiers").Preload("Organizer").Preload("Organizer.OrganizerOnboarding")

	if err := query.Find(&events).Error; err != nil {
		return nil, 0, err
	}

	return events, total, nil
}

// GetEventsByOrganizer gets events by organizer ID with pagination and sorting
func (s *EventService) GetEventsByOrganizer(organizerID string, page, limit int, sortParam string) ([]models.Event, int64, error) {
	// Parse the organizer ID to UUID
	organizerUUID, err := uuid.Parse(organizerID)
	if err != nil {
		return nil, 0, utils.NewBusinessLogicError("invalid organizer ID format")
	}

	var events []models.Event
	var total int64
	offset := (page - 1) * limit

	db := database.DB.Model(&models.Event{}).Where("organizer_id = ?", organizerUUID)
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Parse and apply sorting - always prioritize featured events first
	validSortFields := map[string]bool{
		"title": true, "start_date": true, "price": true, "created_at": true, "status": true, "is_featured": true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, validSortFields, "created_at", "desc")

	// Always prioritize featured events first, then apply the requested sort
	orderClause := "is_featured DESC, " + sortBy + " " + sortOrder

	if err := db.Order(orderClause).Offset(offset).Limit(limit).Find(&events).Error; err != nil {
		return nil, 0, err
	}

	return events, total, nil
}

// LogStatusChange logs a status change to the history table
func (s *EventService) LogStatusChange(eventID uuid.UUID, oldStatus, newStatus, statusType, changedByUserID, remark string) error {
	return s.logStatusChangeWithDB(database.DB, eventID, oldStatus, newStatus, statusType, changedByUserID, remark)
}

// LogStatusChangeTx logs a status change using the provided transaction.
func (s *EventService) LogStatusChangeTx(tx *gorm.DB, eventID uuid.UUID, oldStatus, newStatus, statusType, changedByUserID, remark string) error {
	if tx == nil {
		return fmt.Errorf("nil transaction passed to LogStatusChangeTx")
	}
	return s.logStatusChangeWithDB(tx, eventID, oldStatus, newStatus, statusType, changedByUserID, remark)
}

func (s *EventService) logStatusChangeWithDB(db *gorm.DB, eventID uuid.UUID, oldStatus, newStatus, statusType, changedByUserID, remark string) error {
	if oldStatus == newStatus {
		return nil // No change, don't log
	}

	if !models.IsValidEventStatus(oldStatus) && !models.IsValidEventSalesStatus(oldStatus) {
		return fmt.Errorf("invalid old status '%s' for history logging", oldStatus)
	}
	if !models.IsValidEventStatus(newStatus) && !models.IsValidEventSalesStatus(newStatus) {
		return fmt.Errorf("invalid new status '%s' for history logging", newStatus)
	}

	switch models.EventStatusType(statusType) {
	case models.EventStatusTypeApproval, models.EventStatusTypeSales, models.EventStatusTypeAutomatic, models.EventStatusTypeManual:
	default:
		return fmt.Errorf("invalid event status history type '%s'", statusType)
	}

	// For system changes, we use nil to represent automatic/system-triggered changes
	var changedByUUID *uuid.UUID
	if changedByUserID == "system" {
		// Use nil for system changes - no user associated
		changedByUUID = nil
	} else {
		parsed, err := uuid.Parse(changedByUserID)
		if err != nil {
			return fmt.Errorf("invalid changed_by UUID: %w", err)
		}
		changedByUUID = &parsed
	}

	statusHistory := models.EventStatusHistory{
		EventID:    eventID,
		OldStatus:  oldStatus,
		NewStatus:  newStatus,
		StatusType: statusType, // 'automatic' for system changes
		ChangedBy:  changedByUUID,
		Remark:     remark,
		CreatedAt:  time.Now(),
	}

	return db.Create(&statusHistory).Error
}

// GetEventStatusHistory retrieves all status change history for an event
func (s *EventService) GetEventStatusHistory(eventID uuid.UUID) ([]models.EventStatusHistoryResponse, error) {
	var history []models.EventStatusHistory
	if err := database.DB.Preload("Event").Preload("ChangedByUser").
		Where("event_id = ?", eventID).
		Order("created_at DESC").
		Find(&history).Error; err != nil {
		return nil, err
	}

	// Convert to response format
	var responses []models.EventStatusHistoryResponse
	for _, h := range history {
		response := models.EventStatusHistoryResponse{
			ID:         h.ID,
			EventID:    h.EventID,
			OldStatus:  h.OldStatus,
			NewStatus:  h.NewStatus,
			StatusType: h.StatusType,
			ChangedBy:  h.ChangedBy,
			Remark:     h.Remark,
			CreatedAt:  h.CreatedAt,
		}

		// Add event title and changed by name
		if h.Event != nil {
			response.EventTitle = h.Event.Title
		}
		if h.ChangedByUser != nil {
			response.ChangedByName = h.ChangedByUser.FirstName + " " + h.ChangedByUser.LastName
		} else if h.ChangedBy == nil && h.StatusType == "automatic" {
			// For automatic/system changes with no user
			response.ChangedByName = "System (Automatic)"
		}

		responses = append(responses, response)
	}

	return responses, nil
}

// PauseEvent allows organizers to pause their event sales (set status to hold)
func (s *EventService) PauseEvent(eventID uuid.UUID, organizerID string) (*models.Event, error) {
	// Parse the organizer ID to UUID
	organizerUUID, err := uuid.Parse(organizerID)
	if err != nil {
		return nil, utils.NewBusinessLogicError("Invalid organizer ID format.")
	}

	// Get the event
	var event models.Event
	if err := database.DB.First(&event, "id = ? AND organizer_id = ?", eventID, organizerUUID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, utils.NewNotFoundError("event")
		}
		return nil, err
	}

	// Check if event can be paused (only on_sale, scheduled, sales_upcoming, sales_end events can be paused)
	if event.Status != models.EventStatusOnSale.String() && event.Status != models.EventStatusScheduled.String() &&
		event.Status != models.EventStatusSalesUpcoming.String() && event.Status != models.EventStatusSalesEnd.String() {
		return nil, utils.NewBusinessLogicError("Event can only be paused when in on_sale, scheduled, sales_upcoming, or sales_end status")
	}

	// Update event status to hold
	err = s.UpdateEventStatusWithLogging(eventID, models.EventStatusHold.String(), models.EventStatusTypeManual.String(), organizerID, "Event sales paused by organizer")
	if err != nil {
		return nil, err
	}

	// Get the updated event
	var updatedEvent models.Event
	if err := database.DB.First(&updatedEvent, "id = ?", eventID).Error; err != nil {
		return nil, err
	}

	return &updatedEvent, nil
}

// ResumeEvent allows organizers to resume their paused event sales (set status back to on_sale)
func (s *EventService) ResumeEvent(eventID uuid.UUID, organizerID string) (*models.Event, error) {
	// Parse the organizer ID to UUID
	organizerUUID, err := uuid.Parse(organizerID)
	if err != nil {
		return nil, utils.NewBusinessLogicError("Invalid organizer ID format.")
	}

	// Get the event
	var event models.Event
	if err := database.DB.First(&event, "id = ? AND organizer_id = ?", eventID, organizerUUID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, utils.NewNotFoundError("event")
		}
		return nil, err
	}

	// Check if event is actually paused
	if event.Status != models.EventStatusHold.String() {
		return nil, utils.NewBusinessLogicError("Event is not paused - only hold status events can be resumed")
	}

	// Update event status back to on_sale
	err = s.UpdateEventStatusAndSalesStatusWithLogging(
		eventID,
		models.EventStatusOnSale.String(),
		models.EventSalesStatusActive.String(),
		models.EventStatusTypeManual.String(),
		organizerID,
		"Event sales resumed by organizer",
	)
	if err != nil {
		return nil, err
	}

	// Get the updated event
	var updatedEvent models.Event
	if err := database.DB.First(&updatedEvent, "id = ?", eventID).Error; err != nil {
		return nil, err
	}

	return &updatedEvent, nil
}

// calculateEventTicketSales calculates real-time ticket sales for an event based on its tiers
// This ensures the available count reflects actual sold tickets, not just cached values
// Works for ALL event types regardless of status:
// - Events WITH tiers: Calculates from tier quantities and sold tickets per tier
// - Events WITHOUT tiers: Falls back to direct ticket table queries (legacy events)
// - All statuses: draft, pending, approved, on_sale, live, completed, cancelled, etc.
func (s *EventService) calculateEventTicketSales(event *models.Event) {
	originalCapacity := event.Capacity // Preserve the original capacity set by organizer
	totalSold := 0
	totalRevenue := 0.0
	totalTierCapacity := 0

	if len(event.Tiers) > 0 {
		// Event has tiers - calculate from tier data
		for _, tier := range event.Tiers {
			totalTierCapacity += tier.Quantity

			// Count actual sold tickets and calculate revenue for this tier
			var tierSummary struct {
				SoldCount int     `json:"sold_count"`
				Revenue   float64 `json:"revenue"`
			}

			database.DB.Model(&models.Ticket{}).
				Joins("JOIN event_tiers ON tickets.tier_id = event_tiers.id").
				Select("COUNT(*) as sold_count, COALESCE(SUM(event_tiers.price), 0) as revenue").
				Where("tickets.event_id = ? AND tickets.tier_id = ? AND tickets.status IN ('active', 'used') AND tickets.deleted_at IS NULL",
					event.ID, tier.ID).
				Scan(&tierSummary)

			totalSold += tierSummary.SoldCount
			totalRevenue += tierSummary.Revenue
		}

		// Available tickets = min(original capacity, total tier capacity - sold tickets)
		availableFromTiers := totalTierCapacity - totalSold
		if originalCapacity < availableFromTiers {
			event.Available = originalCapacity
		} else {
			event.Available = availableFromTiers
		}
	} else {
		// Event doesn't have tiers - calculate from tickets table directly
		// This handles legacy events or events that don't use the tier system
		var eventSummary struct {
			SoldCount int     `json:"sold_count"`
			Revenue   float64 `json:"revenue"`
		}

		database.DB.Model(&models.Ticket{}).
			Select("COUNT(*) as sold_count, COALESCE(SUM(price), 0) as revenue").
			Where("event_id = ? AND status IN ('active', 'used') AND deleted_at IS NULL",
				event.ID).
			Scan(&eventSummary)

		totalSold = eventSummary.SoldCount
		totalRevenue = eventSummary.Revenue
		// For events without tiers, available = original capacity - sold
		event.Available = originalCapacity - totalSold
	}

	// Update the event's computed fields based on real-time calculations
	// Keep the original capacity set by the organizer
	event.TotalSoldTickets = totalSold
	event.TotalRevenue = totalRevenue
}

// UpdateEventStatusWithLogging is a central function to update event status and log the change in a transaction
// This ensures that status changes and history logging are atomic operations
func (s *EventService) UpdateEventStatusWithLogging(eventID uuid.UUID, newStatus string, changeType, changedBy, remark string) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// Get current event status
		var event models.Event
		if err := tx.Where("id = ?", eventID).First(&event).Error; err != nil {
			return fmt.Errorf("failed to find event: %w", err)
		}

		oldStatus := event.Status

		// Update the event status
		if err := tx.Model(&event).Update("status", newStatus).Error; err != nil {
			return fmt.Errorf("failed to update event status: %w", err)
		}

		// Log the status change
		if err := s.LogStatusChangeTx(tx, eventID, oldStatus, newStatus, changeType, changedBy, remark); err != nil {
			return fmt.Errorf("failed to log status change: %w", err)
		}

		return nil
	})
}

// UpdateEventStatusAndSalesStatusWithLogging updates both status and sales_status with logging
func (s *EventService) UpdateEventStatusAndSalesStatusWithLogging(eventID uuid.UUID, newStatus, newSalesStatus string, changeType, changedBy, remark string) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// Get current event status
		var event models.Event
		if err := tx.Where("id = ?", eventID).First(&event).Error; err != nil {
			return fmt.Errorf("failed to find event: %w", err)
		}

		oldStatus := event.Status
		oldSalesStatus := event.SalesStatus

		// Prepare updates
		updates := make(map[string]interface{})
		if newStatus != "" {
			updates["status"] = newStatus
		}
		if newSalesStatus != "" {
			updates["sales_status"] = newSalesStatus
		}

		// Update the event
		if err := tx.Model(&event).Updates(updates).Error; err != nil {
			return fmt.Errorf("failed to update event: %w", err)
		}

		// Log status change if status changed
		if newStatus != "" && oldStatus != newStatus {
			if err := s.LogStatusChangeTx(tx, eventID, oldStatus, newStatus, changeType, changedBy, remark); err != nil {
				return fmt.Errorf("failed to log status change: %w", err)
			}
		}

		// Log sales status change if it changed
		if newSalesStatus != "" && oldSalesStatus != newSalesStatus {
			if err := s.LogStatusChangeTx(tx, eventID, oldSalesStatus, newSalesStatus, models.EventStatusTypeSales.String(), changedBy, remark); err != nil {
				return fmt.Errorf("failed to log sales status change: %w", err)
			}
		}

		return nil
	})
}

// CancelEventWithLogging is a central function to cancel an event with proper logging
// This handles setting all cancellation fields and logging the status change atomically
func (s *EventService) CancelEventWithLogging(eventID uuid.UUID, cancelReason string, changeType, changedBy, remark string) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		// Get current event status
		var event models.Event
		if err := tx.Where("id = ?", eventID).First(&event).Error; err != nil {
			return fmt.Errorf("failed to find event: %w", err)
		}

		oldStatus := event.Status
		now := time.Now().UTC()

		// Update event with cancellation fields
		updates := map[string]interface{}{
			"status":        models.EventStatusCancelled.String(),
			"sales_status":  models.EventSalesStatusStopped.String(),
			"is_cancelled":  true,
			"cancelled_at":  now,
			"cancel_reason": cancelReason,
		}

		if err := tx.Model(&event).Updates(updates).Error; err != nil {
			return fmt.Errorf("failed to cancel event: %w", err)
		}

		// Log the status change
		if err := s.LogStatusChangeTx(tx, eventID, oldStatus, models.EventStatusCancelled.String(), changeType, changedBy, remark); err != nil {
			return fmt.Errorf("failed to log status change: %w", err)
		}

		// Log sales status change if it changed
		if event.SalesStatus != models.EventSalesStatusStopped.String() {
			if err := s.LogStatusChangeTx(tx, eventID, event.SalesStatus, models.EventSalesStatusStopped.String(), models.EventStatusTypeSales.String(), changedBy, remark); err != nil {
				return fmt.Errorf("failed to log sales status change: %w", err)
			}
		}

		return nil
	})
}
