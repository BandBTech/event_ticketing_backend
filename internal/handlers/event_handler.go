package handlers

import (
	"net/http"
	"strconv"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
)

type EventHandler struct {
	service *services.EventService
}

func NewEventHandler(service *services.EventService) *EventHandler {
	return &EventHandler{service: service}
}

// CreateEvent godoc
// @Summary Create a new event
// @Description Create a new event with the provided details
// @Tags events
// @Accept json
// @Produce json
// @Param event body models.EventCreateRequest true "Event details"
// @Success 201 {object} utils.Response{data=models.Event}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/events [post]
func (h *EventHandler) CreateEvent(c *gin.Context) {
	var req models.EventCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	event, err := h.service.CreateEvent(&req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to create event", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Event created successfully", event)
}

// GetAllEvents godoc
// @Summary Get all events
// @Description Get a list of all events
// @Tags events
// @Produce json
// @Success 200 {object} utils.Response{data=[]models.Event}
// @Failure 500 {object} utils.Response
// @Router /api/v1/events [get]
func (h *EventHandler) GetAllEvents(c *gin.Context) {
	events, err := h.service.GetAllEvents()
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to fetch events", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Events fetched successfully", events)
}

// GetEventByID godoc
// @Summary Get event by ID
// @Description Get details of a specific event by ID
// @Tags events
// @Produce json
// @Param id path int true "Event ID"
// @Success 200 {object} utils.Response{data=models.Event}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Router /api/v1/events/{id} [get]
func (h *EventHandler) GetEventByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid event ID", err)
		return
	}

	event, err := h.service.GetEventByID(uint(id))
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "Event not found", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event fetched successfully", event)
}

// UpdateEvent godoc
// @Summary Update an event
// @Description Update event details by ID
// @Tags events
// @Accept json
// @Produce json
// @Param id path int true "Event ID"
// @Param event body models.EventUpdateRequest true "Updated event details"
// @Success 200 {object} utils.Response{data=models.Event}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/events/{id} [put]
func (h *EventHandler) UpdateEvent(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid event ID", err)
		return
	}

	var req models.EventUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	event, err := h.service.UpdateEvent(uint(id), &req)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to update event", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event updated successfully", event)
}

// DeleteEvent godoc
// @Summary Delete an event
// @Description Delete an event by ID
// @Tags events
// @Produce json
// @Param id path int true "Event ID"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/events/{id} [delete]
func (h *EventHandler) DeleteEvent(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid event ID", err)
		return
	}

	if err := h.service.DeleteEvent(uint(id)); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to delete event", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Event deleted successfully", nil)
}
