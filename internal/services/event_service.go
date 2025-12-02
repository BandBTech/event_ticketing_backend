package services

import (
	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/utils"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EventService struct{}

func NewEventService() *EventService {
	return &EventService{}
}

func (s *EventService) CreateEvent(req *models.EventCreateRequest, organizerID string) (*models.Event, error) {
	// Parse the organizer ID to UUID
	organizerUUID, err := uuid.Parse(organizerID)
	if err != nil {
		return nil, fmt.Errorf("invalid organizer ID format: %w", err)
	}

	// Get user details to check if they are admin
	var user models.User
	if err := database.DB.Preload("Roles").Where("id = ?", organizerUUID).First(&user).Error; err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	// Check if user has admin role
	isAdmin := false
	for _, role := range user.Roles {
		if role.Name == "admin" {
			isAdmin = true
			break
		}
	}

	// Set status based on user role
	status := "pending" // Default for organizers
	if isAdmin {
		status = "approved" // Admin-created events are auto-approved
	}

	// Parse comma-separated category string
	categoryArray := models.StringArray{}
	if req.Category != "" {
		categories := strings.Split(req.Category, ",")
		for _, cat := range categories {
			trimmed := strings.TrimSpace(cat)
			if trimmed != "" {
				categoryArray = append(categoryArray, trimmed)
			}
		}
	}

	event := &models.Event{
		Title:          req.Title,
		Description:    req.Description,
		BannerImage:    req.BannerImage,
		Category:       categoryArray,
		VenueName:      req.VenueName,
		Address:        req.Address,
		StartDate:      req.StartDate,
		EndDate:        req.EndDate,
		Timezone:       req.Timezone,
		Price:          req.Price,
		Capacity:       req.Capacity,
		CommissionRate: req.CommissionRate,
		OrganizerID:    organizerUUID,
		Status:         status,
	}

	// Set default commission rate if not provided
	if event.CommissionRate == 0 {
		event.CommissionRate = 10 // Default 10%
	}

	if err := database.DB.Create(event).Error; err != nil {
		return nil, err
	}

	return event, nil
}

func (s *EventService) CreateEventWithTx(req *models.EventCreateRequest, organizerID string, tx *gorm.DB) (*models.Event, error) {
	// Parse the organizer ID to UUID
	organizerUUID, err := uuid.Parse(organizerID)
	if err != nil {
		return nil, fmt.Errorf("invalid organizer ID format: %w", err)
	}

	// Get user details to check if they are admin
	var user models.User
	if err := tx.Preload("Roles").Where("id = ?", organizerUUID).First(&user).Error; err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	// Check if user has admin role
	isAdmin := false
	for _, role := range user.Roles {
		if role.Name == "admin" {
			isAdmin = true
			break
		}
	}

	// Set status based on user role
	status := "pending" // Default for organizers
	if isAdmin {
		status = "approved" // Admin-created events are auto-approved
	}

	// Parse comma-separated category string
	categoryArray := models.StringArray{}
	if req.Category != "" {
		categories := strings.Split(req.Category, ",")
		for _, cat := range categories {
			trimmed := strings.TrimSpace(cat)
			if trimmed != "" {
				categoryArray = append(categoryArray, trimmed)
			}
		}
	}

	event := &models.Event{
		Title:          req.Title,
		Description:    req.Description,
		BannerImage:    req.BannerImage,
		Category:       categoryArray,
		VenueName:      req.VenueName,
		Address:        req.Address,
		StartDate:      req.StartDate,
		EndDate:        req.EndDate,
		Timezone:       req.Timezone,
		Price:          req.Price,
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

	return event, nil
}

// GetPaginatedEvents returns paginated events and total count
func (s *EventService) GetPaginatedEvents(page, limit int) ([]models.Event, int64, error) {
	var events []models.Event
	var total int64
	offset := (page - 1) * limit
	db := database.DB.Model(&models.Event{})
	db.Count(&total)
	if err := db.Offset(offset).Limit(limit).Find(&events).Error; err != nil {
		return nil, 0, err
	}
	return events, total, nil
}

func (s *EventService) GetAllEvents() ([]models.Event, error) {
	var events []models.Event
	if err := database.DB.Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

func (s *EventService) GetEventByID(id uuid.UUID) (*models.Event, error) {
	var event models.Event
	if err := database.DB.First(&event, "id = ?", id).Error; err != nil {
		return nil, err
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
	if req.Category != "" {
		categoryArray := models.StringArray{}
		categories := strings.Split(req.Category, ",")
		for _, cat := range categories {
			trimmed := strings.TrimSpace(cat)
			if trimmed != "" {
				categoryArray = append(categoryArray, trimmed)
			}
		}
		event.Category = categoryArray
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
	if req.Capacity > 0 {
		event.Capacity = req.Capacity
	}
	if req.Status != "" {
		event.Status = req.Status
	}
	if req.CommissionRate > 0 {
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

// ApproveEvent allows admin/subadmin to approve, hold, or reject events
func (s *EventService) ApproveEvent(eventID uuid.UUID, userID string, req *models.EventApprovalRequest) (*models.Event, error) {
	// Check if user has admin or subadmin role
	userUUID, err := uuid.Parse(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID format")
	}

	var user models.User
	if err := database.DB.Preload("Roles").Where("id = ?", userUUID).First(&user).Error; err != nil {
		return nil, fmt.Errorf("user not found")
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
		return nil, fmt.Errorf("insufficient permissions: only admin or subadmin can approve events")
	}

	// Get the event
	var event models.Event
	if err := database.DB.First(&event, "id = ?", eventID).Error; err != nil {
		return nil, fmt.Errorf("event not found")
	}

	// Validate status transition - admin can change most statuses except for completed events
	if event.Status == "cancelled" && req.Status != "cancelled" {
		return nil, fmt.Errorf("cancelled events cannot be changed to other statuses")
	}

	// Update event status, commission rate, and remark
	event.Status = req.Status
	event.AdminRemark = req.AdminRemark

	// Update commission rate if provided
	if req.CommissionRate != nil {
		event.CommissionRate = *req.CommissionRate
	}

	if err := database.DB.Save(&event).Error; err != nil {
		return nil, err
	}

	return &event, nil
}

// GetEventsByStatus gets events by status with pagination and sorting
func (s *EventService) GetEventsByStatus(status string, page, limit int, sortParam string) ([]models.Event, int64, error) {
	var events []models.Event
	var total int64
	offset := (page - 1) * limit

	db := database.DB.Model(&models.Event{}).Where("status = ?", status)
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Parse and apply sorting
	validSortFields := map[string]bool{
		"title": true, "start_date": true, "price": true, "created_at": true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, validSortFields, "created_at", "desc")
	orderClause := sortBy + " " + sortOrder

	if err := db.Order(orderClause).Offset(offset).Limit(limit).Find(&events).Error; err != nil {
		return nil, 0, err
	}

	return events, total, nil
}

// GetFilteredEvents returns filtered and paginated events
func (s *EventService) GetFilteredEvents(status string, page, limit int, search, location, startDate, endDate string, minPrice, maxPrice *float64, sortBy, sortOrder string) ([]models.Event, int64, error) {
	var events []models.Event
	var total int64
	offset := (page - 1) * limit

	db := database.DB.Model(&models.Event{})

	// Apply status filter only if status is provided
	if status != "" {
		db = db.Where("status = ?", status)
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

	// Count total records
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Apply sorting
	orderClause := sortBy + " " + sortOrder
	if err := db.Offset(offset).Limit(limit).Order(orderClause).Find(&events).Error; err != nil {
		return nil, 0, err
	}

	return events, total, nil
}

// GetEventsByOrganizer gets events by organizer ID with pagination and sorting
func (s *EventService) GetEventsByOrganizer(organizerID string, page, limit int, sortParam string) ([]models.Event, int64, error) {
	// Parse the organizer ID to UUID
	organizerUUID, err := uuid.Parse(organizerID)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid organizer ID format: %w", err)
	}

	var events []models.Event
	var total int64
	offset := (page - 1) * limit

	db := database.DB.Model(&models.Event{}).Where("organizer_id = ?", organizerUUID)
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Parse and apply sorting
	validSortFields := map[string]bool{
		"title": true, "start_date": true, "price": true, "created_at": true, "status": true,
	}
	sortBy, sortOrder := utils.ValidateAndParseSortParam(sortParam, validSortFields, "created_at", "desc")
	orderClause := sortBy + " " + sortOrder

	if err := db.Order(orderClause).Offset(offset).Limit(limit).Find(&events).Error; err != nil {
		return nil, 0, err
	}

	return events, total, nil
}
