package services

import (
	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
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
	status := "pending"

	// Trim category
	categoryStr := strings.TrimSpace(req.Category)

	event := &models.Event{
		Title:          req.Title,
		Description:    req.Description,
		BannerImage:    req.BannerImage,
		Category:       categoryStr,
		VenueName:      req.VenueName,
		Address:        req.Address,
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

	// Set default currency if not provided
	if event.Currency == "" {
		event.Currency = "USD" // Default USD
	}

	if err := tx.Create(event).Error; err != nil {
		return nil, err
	}

	return event, nil
}

func (s *EventService) GetEventByID(id uuid.UUID) (*models.Event, error) {
	var event models.Event
	if err := database.DB.First(&event, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

// GetPublicEventByID gets an event by ID with tiers preloaded (for public APIs) - returns on_sale, hold, and live events
func (s *EventService) GetPublicEventByID(id uuid.UUID) (*models.Event, error) {
	var event models.Event

	// Include on_sale, hold, and live events
	if err := database.DB.Preload("Tiers").
		Where("status IN (?) AND id = ?", []string{"on_sale", "hold", "live"}, id).First(&event).Error; err != nil {
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
	if req.VenueName != "" {
		event.VenueName = req.VenueName
	}
	if req.Address != "" {
		event.Address = req.Address
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
		event.Currency = req.Currency
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

	// Get the event
	var event models.Event
	if err := database.DB.First(&event, "id = ?", eventID).Error; err != nil {
		return nil, utils.NewNotFoundError("event")
	}

	// Validate status transition - admin can change most statuses except for completed events
	if event.Status == "cancelled" && status != "cancelled" {
		return nil, utils.NewBusinessLogicError("cancelled events cannot be changed to other statuses")
	}

	// Update event status, commission rate, and remark
	oldStatus := event.Status

	// When admin approves an event (status = "approved"), change it to "scheduled" instead
	finalStatus := status
	if status == "approved" {
		finalStatus = "scheduled"
	}

	event.Status = finalStatus
	event.AdminRemark = adminRemark

	// Update commission rate if provided (allow override of existing rate)
	if commissionRate != nil {
		event.CommissionRate = *commissionRate
	}

	if err := database.DB.Save(&event).Error; err != nil {
		return nil, err
	}

	// Log the status change to history
	if err := s.LogStatusChange(eventID, oldStatus, finalStatus, "approval", userID, adminRemark); err != nil {
		// Log the error but don't fail the operation
		fmt.Printf("[ERROR] Failed to log status change: %v\n", err)
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

// GetPublicEvents returns public events including scheduled, on_sale, and sales_end events with future tiers (Sales Upcoming)
func (s *EventService) GetPublicEvents(page, limit int, search, location, startDate, endDate string, minPrice, maxPrice *float64, sortBy, sortOrder string) ([]models.Event, int64, error) {
	var events []models.Event
	var total int64
	offset := (page - 1) * limit

	db := database.DB.Model(&models.Event{})

	// Include scheduled, on_sale events, and sales_end events with future tiers (multi-tier only)
	// Sales Upcoming: events with no active tier but have future tier sales windows
	db = db.Where("status IN (?) OR (status = ? AND (SELECT COUNT(*) FROM event_tiers WHERE event_id = events.id AND deleted_at IS NULL) > 1 AND id IN (SELECT DISTINCT event_id FROM event_tiers WHERE sales_start > ? AND deleted_at IS NULL))",
		[]string{"scheduled", "on_sale"},
		"sales_end",
		time.Now())

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

	// Apply sorting - always prioritize featured events first, then sort by newest first
	orderClause := "is_featured DESC, created_at DESC"
	query := db.Offset(offset).Limit(limit).Order(orderClause)

	// Preload tiers for public events (scheduled, on_sale, live events)
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
	if oldStatus == newStatus {
		return nil // No change, don't log
	}

	changedByUUID, err := uuid.Parse(changedByUserID)
	if err != nil {
		return utils.NewBusinessLogicError("invalid changed_by user ID")
	}

	statusHistory := &models.EventStatusHistory{
		EventID:    eventID,
		OldStatus:  oldStatus,
		NewStatus:  newStatus,
		StatusType: statusType,
		ChangedBy:  changedByUUID,
		Remark:     remark,
	}

	if err := database.DB.Create(statusHistory).Error; err != nil {
		return utils.NewDatabaseError("failed to log status change", err)
	}

	return nil
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
		}

		responses = append(responses, response)
	}

	return responses, nil
}

// calculateEventTicketSales calculates real-time ticket sales for an event based on its tiers
// This ensures the available count reflects actual sold tickets, not just cached values
// Works for ALL event types regardless of status:
// - Events WITH tiers: Calculates from tier quantities and sold tickets per tier
// - Events WITHOUT tiers: Falls back to direct ticket table queries (legacy events)
// - All statuses: draft, pending, approved, on_sale, live, completed, cancelled, etc.
func (s *EventService) calculateEventTicketSales(event *models.Event) {
	totalCapacity := event.Capacity // Start with the stored capacity
	totalSold := 0
	totalRevenue := 0.0

	if len(event.Tiers) > 0 {
		// Event has tiers - calculate from tier data
		totalCapacity = 0 // Recalculate capacity from tiers

		for _, tier := range event.Tiers {
			totalCapacity += tier.Quantity

			// Count actual sold tickets and calculate revenue for this tier
			var tierSummary struct {
				SoldCount int     `json:"sold_count"`
				Revenue   float64 `json:"revenue"`
			}

			database.DB.Model(&models.Ticket{}).
				Joins("JOIN event_tiers ON tickets.tier_id = event_tiers.id").
				Select("COUNT(*) as sold_count, COALESCE(SUM(event_tiers.price), 0) as revenue").
				Where("tickets.event_id = ? AND tickets.tier_id = ? AND (tickets.payment_status = 'completed' OR tickets.status = 'active') AND tickets.deleted_at IS NULL",
					event.ID, tier.ID).
				Scan(&tierSummary)

			totalSold += tierSummary.SoldCount
			totalRevenue += tierSummary.Revenue
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
			Where("event_id = ? AND (payment_status = 'completed' OR status = 'active') AND deleted_at IS NULL",
				event.ID).
			Scan(&eventSummary)

		totalSold = eventSummary.SoldCount
		totalRevenue = eventSummary.Revenue
		// Keep the stored capacity for events without tiers
	}

	// Update the event's computed fields based on real-time calculations
	event.Capacity = totalCapacity
	event.Available = totalCapacity - totalSold
	event.TotalSoldTickets = totalSold
	event.TotalRevenue = totalRevenue
}
