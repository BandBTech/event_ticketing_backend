package services

import (
	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
)

type EventService struct{}

func NewEventService() *EventService {
	return &EventService{}
}

func (s *EventService) CreateEvent(req *models.EventCreateRequest) (*models.Event, error) {
	event := &models.Event{
		Title:       req.Title,
		Description: req.Description,
		Location:    req.Location,
		StartDate:   req.StartDate,
		EndDate:     req.EndDate,
		Price:       req.Price,
		Capacity:    req.Capacity,
	}

	if err := database.DB.Create(event).Error; err != nil {
		return nil, err
	}

	return event, nil
}

func (s *EventService) GetAllEvents() ([]models.Event, error) {
	var events []models.Event
	if err := database.DB.Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

func (s *EventService) GetEventByID(id uint) (*models.Event, error) {
	var event models.Event
	if err := database.DB.First(&event, id).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

func (s *EventService) UpdateEvent(id uint, req *models.EventUpdateRequest) (*models.Event, error) {
	var event models.Event
	if err := database.DB.First(&event, id).Error; err != nil {
		return nil, err
	}

	if req.Title != "" {
		event.Title = req.Title
	}
	if req.Description != "" {
		event.Description = req.Description
	}
	if req.Location != "" {
		event.Location = req.Location
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

	if err := database.DB.Save(&event).Error; err != nil {
		return nil, err
	}

	return &event, nil
}

func (s *EventService) DeleteEvent(id uint) error {
	return database.DB.Delete(&models.Event{}, id).Error
}
