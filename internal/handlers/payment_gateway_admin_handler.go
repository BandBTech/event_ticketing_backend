package handlers

import (
	"net/http"

	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AdminManageGatewayConfigs godoc
// @Summary Manage payment gateway configurations (Admin)
// @Description Get all gateways (paginated) or supported gateway types using ?type=supported
// @Tags Admin - Payment Gateways
// @Security ApiKeyAuth
// @Produce json
// @Param type query string false "Query type: 'supported' for gateway types"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(10)
// @Success 200 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payment-gateways [get]
func (h *PaymentHandler) AdminManageGatewayConfigs(c *gin.Context) {
	// Check if requesting supported gateway types
	if c.Query("type") == "supported" {
		supported, err := h.paymentService.GetSupportedGatewayTypes(c.Request.Context())
		if err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve supported gateways", err)
			return
		}
		utils.SuccessResponse(c, http.StatusOK, "Supported gateway types retrieved", supported)
		return
	}

	// Otherwise return paginated list of configured gateways
	pagination := utils.GetPaginationParams(c, 10)

	configs, total, err := h.paymentService.GetAllGatewayConfigs(c.Request.Context(), pagination.Page, pagination.Limit)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to retrieve gateways", err)
		return
	}

	response := map[string]interface{}{
		"gateways":   configs,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Gateways retrieved successfully", response)
}

// AdminCreateGatewayConfig godoc
// @Summary Create payment gateway configuration (Admin)
// @Description Create a new payment gateway configuration with API keys, secrets, and settings. All credentials will be encrypted before storage.
// @Tags Admin - Payment Gateways
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body models.CreatePaymentGatewayConfigRequest true "Gateway configuration with required API keys and settings"
// @Success 201 {object} utils.Response{data=models.PaymentGatewayConfig} "Gateway configuration created successfully"
// @Failure 400 {object} utils.Response "Invalid request payload or missing required fields"
// @Failure 401 {object} utils.Response "Unauthorized - Admin access required"
// @Failure 409 {object} utils.Response "Gateway with this name already exists"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/payment-gateways [post]
func (h *PaymentHandler) AdminCreateGatewayConfig(c *gin.Context) {
	var req models.CreatePaymentGatewayConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid request payload", err)
		return
	}

	// Get admin ID from context
	adminIDInterface, _ := c.Get("userID")
	adminID := adminIDInterface.(uuid.UUID)

	config, err := h.paymentService.CreateGatewayConfig(c.Request.Context(), &req, adminID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to create gateway configuration", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Gateway configuration created successfully", config)
}

// AdminGetGatewayByID godoc
// @Summary Get payment gateway by ID (Admin)
// @Description Retrieve a specific payment gateway configuration
// @Tags Admin - Payment Gateways
// @Security ApiKeyAuth
// @Produce json
// @Param gateway_id path string true "Gateway ID"
// @Success 200 {object} utils.Response{data=models.PaymentGatewayConfig}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Router /api/v1/admin/payment-gateways/{gateway_id} [get]
func (h *PaymentHandler) AdminGetGatewayByID(c *gin.Context) {
	gatewayID, err := uuid.Parse(c.Param("gateway_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid gateway ID", err)
		return
	}

	config, err := h.paymentService.GetGatewayConfigByID(c.Request.Context(), gatewayID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusNotFound, "Gateway not found", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Gateway retrieved successfully", config)
}

// AdminUpdateGateway godoc
// @Summary Update payment gateway configuration (Admin)
// @Description Update gateway API keys, secrets, settings, or configuration. Only provided fields will be updated.
// @Tags Admin - Payment Gateways
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param gateway_id path string true "Gateway configuration ID" example(550e8400-e29b-41d4-a716-446655440000)
// @Param request body models.PaymentGatewayConfig true "Gateway configuration updates (partial update supported)"
// @Success 200 {object} utils.Response{data=models.PaymentGatewayConfig} "Gateway configuration updated successfully"
// @Failure 400 {object} utils.Response "Invalid gateway ID or request payload"
// @Failure 401 {object} utils.Response "Unauthorized - Admin access required"
// @Failure 404 {object} utils.Response "Gateway configuration not found"
// @Failure 409 {object} utils.Response "Gateway name already exists"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/payment-gateways/{gateway_id} [put]
func (h *PaymentHandler) AdminUpdateGateway(c *gin.Context) {
	gatewayID, err := uuid.Parse(c.Param("gateway_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid gateway ID", err)
		return
	}

	var req models.PaymentGatewayConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid payload", err)
		return
	}

	adminID := c.GetString("userID")
	config, err := h.paymentService.UpdateGatewayConfig(c.Request.Context(), gatewayID, &req, uuid.MustParse(adminID))
	if err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to update gateway", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Gateway updated successfully", config)
}

// AdminDeleteGateway godoc
// @Summary Delete payment gateway (Admin)
// @Description Delete a payment gateway configuration
// @Tags Admin - Payment Gateways
// @Security ApiKeyAuth
// @Param gateway_id path string true "Gateway ID"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Router /api/v1/admin/payment-gateways/{gateway_id} [delete]
func (h *PaymentHandler) AdminDeleteGateway(c *gin.Context) {
	gatewayID, err := uuid.Parse(c.Param("gateway_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid gateway ID", err)
		return
	}

	if err := h.paymentService.DeleteGatewayConfig(c.Request.Context(), gatewayID); err != nil {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to delete gateway", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Gateway deleted successfully", nil)
}

// AdminGatewayAction godoc
// @Summary Perform actions on payment gateway (Admin)
// @Description Execute actions: toggle (enable/disable), test (connection), reload, validate
// @Tags Admin - Payment Gateways
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param gateway_id path string true "Gateway ID (use 'all' for reload action)"
// @Param action query string true "Action: toggle, test, reload, validate"
// @Param request body map[string]interface{} false "Action parameters"
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Router /api/v1/admin/payment-gateways/{gateway_id} [patch]
func (h *PaymentHandler) AdminGatewayAction(c *gin.Context) {
	action := c.Query("action")
	if action == "" {
		utils.ErrorResponse(c, http.StatusBadRequest, "Action parameter required: ?action=toggle|test|reload|validate", nil)
		return
	}

	// Reload action doesn't need gateway_id
	if action == "reload" {
		if err := h.paymentService.ReloadGateways(c.Request.Context()); err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to reload gateways", err)
			return
		}
		utils.SuccessResponse(c, http.StatusOK, "Gateways reloaded successfully", nil)
		return
	}

	// All other actions need gateway_id
	gatewayID, err := uuid.Parse(c.Param("gateway_id"))
	if err != nil {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid gateway ID", err)
		return
	}

	// Parse request body
	var payload map[string]interface{}
	_ = c.ShouldBindJSON(&payload)

	switch action {
	case "toggle":
		// Toggle enable/disable or test/prod mode
		if isEnabled, ok := payload["is_enabled"].(bool); ok {
			config, err := h.paymentService.ToggleGatewayStatus(c.Request.Context(), gatewayID, isEnabled)
			if err != nil {
				utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to toggle status", err)
				return
			}
			status := map[bool]string{true: "enabled", false: "disabled"}[isEnabled]
			utils.SuccessResponse(c, http.StatusOK, "Gateway "+status, config)
			return
		}
		if isTestMode, ok := payload["is_test_mode"].(bool); ok {
			config, err := h.paymentService.ToggleGatewayMode(c.Request.Context(), gatewayID, isTestMode)
			if err != nil {
				utils.ErrorResponse(c, http.StatusInternalServerError, "Failed to toggle mode", err)
				return
			}
			mode := map[bool]string{true: "sandbox", false: "production"}[isTestMode]
			utils.SuccessResponse(c, http.StatusOK, "Gateway switched to "+mode+" mode", config)
			return
		}
		utils.ErrorResponse(c, http.StatusBadRequest, "Toggle requires is_enabled or is_test_mode parameter", nil)

	case "test":
		result, err := h.paymentService.TestGatewayConnection(c.Request.Context(), gatewayID)
		if err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "Connection test failed", err)
			return
		}
		utils.SuccessResponse(c, http.StatusOK, "Connection test completed", result)

	case "validate":
		validation, err := h.paymentService.ValidateGatewayConfig(c.Request.Context(), gatewayID)
		if err != nil {
			utils.ErrorResponse(c, http.StatusInternalServerError, "Validation failed", err)
			return
		}
		if !validation["is_valid"].(bool) {
			utils.ErrorResponse(c, http.StatusBadRequest, "Gateway configuration is invalid", nil)
			return
		}
		utils.SuccessResponse(c, http.StatusOK, "Gateway configuration is valid", validation)

	default:
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid action. Use: toggle, test, reload, or validate", nil)
	}
}
