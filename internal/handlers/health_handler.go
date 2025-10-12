package handlers

import (
	"net/http"

	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
)

type HealthHandler struct {
	healthService *services.HealthService
}

func NewHealthHandler(healthService *services.HealthService) *HealthHandler {
	return &HealthHandler{
		healthService: healthService,
	}
}

// Health check for all components (removed from Swagger docs)
func (h *HealthHandler) Health(c *gin.Context) {
	// Get detailed health status
	healthStatus := h.healthService.CheckSimpleHealth()

	// Log the healthStatus
	c.Set("DEBUG_HEALTH", "Using updated health handler")

	if healthStatus.Status == "healthy" {
		c.JSON(http.StatusOK, healthStatus)
	} else {
		c.JSON(http.StatusServiceUnavailable, healthStatus)
	}
}

// Database health check (removed from Swagger docs)
// @Router /health/db [get]
func (h *HealthHandler) HealthDB(c *gin.Context) {
	status := h.healthService.CheckDBHealth()

	if status.Status == "healthy" {
		utils.SuccessResponse(c, http.StatusOK, status.Message, gin.H{
			"status": "up",
		})
		return
	}

	utils.ErrorResponse(c, http.StatusServiceUnavailable, status.Message, nil)
}

// Redis health check (removed from Swagger docs)
func (h *HealthHandler) HealthRedis(c *gin.Context) {
	status := h.healthService.CheckRedisHealth()

	if status.Status == "healthy" {
		utils.SuccessResponse(c, http.StatusOK, status.Message, gin.H{
			"status": "up",
		})
		return
	}

	utils.ErrorResponse(c, http.StatusServiceUnavailable, status.Message, nil)
}

// Complete health check (removed from Swagger docs)
func (h *HealthHandler) HealthAll(c *gin.Context) {
	healthStatus := h.healthService.CheckHealth()

	if healthStatus.Status == "healthy" {
		c.JSON(http.StatusOK, healthStatus)
	} else {
		c.JSON(http.StatusServiceUnavailable, healthStatus)
	}
}
