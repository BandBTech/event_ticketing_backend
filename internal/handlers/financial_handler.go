package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type FinancialHandler struct {
	financialService *services.FinancialService
}

func NewFinancialHandler(financialService *services.FinancialService) *FinancialHandler {
	return &FinancialHandler{
		financialService: financialService,
	}
}

// getOrganizerIDForUser returns the organizer ID for the given user
// For organizers: returns their user ID
// For staff/managers: returns their organization_id
func (fh *FinancialHandler) getOrganizerIDForUser(userID uuid.UUID) (uuid.UUID, error) {
	// Import database and models
	// Since it's internal, assume we can import
	var user models.User
	if err := database.GetDB().Preload("Roles").Where("id = ?", userID).First(&user).Error; err != nil {
		return uuid.Nil, fmt.Errorf("user not found")
	}

	// Check if user is organizer
	isOrganizer := false
	for _, role := range user.Roles {
		if role.Name == "organizer" {
			isOrganizer = true
			break
		}
	}

	if isOrganizer {
		return userID, nil
	}

	// For staff/managers, check if they have organization_id
	if user.OrganizerID == nil {
		return uuid.Nil, fmt.Errorf("staff/manager does not belong to an organization")
	}

	return *user.OrganizerID, nil
}

// Admin APIs

// GetAdminFinancialSummary returns overall financial summary for admin
func (fh *FinancialHandler) GetAdminFinancialSummary(c *gin.Context) {
	summary, err := fh.financialService.GetAdminFinancialSummary()
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to get financial summary", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Financial summary retrieved successfully", summary)
}

// GetAllEventSales returns paginated list of all event sales for admin
func (fh *FinancialHandler) GetAllEventSales(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	var organizerID *uuid.UUID
	if organizerIDStr := c.Query("organizer_id"); organizerIDStr != "" {
		if id, err := uuid.Parse(organizerIDStr); err == nil {
			organizerID = &id
		}
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	sales, total, err := fh.financialService.GetEventSalesList(page, limit, organizerID)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to get event sales", err)
		return
	}

	response := map[string]interface{}{
		"sales": sales,
		"pagination": map[string]interface{}{
			"page":        page,
			"limit":       limit,
			"total":       total,
			"total_pages": (total + int64(limit) - 1) / int64(limit),
		},
	}

	utils.SuccessResponse(c, http.StatusOK, "Event sales retrieved successfully", response)
}

// CreatePaymentBill creates a new payment bill for an organizer
func (fh *FinancialHandler) CreatePaymentBill(c *gin.Context) {
	userIDStr, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "Unauthorized", nil)
		return
	}

	adminID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", err)
		return
	}

	var req models.CreatePaymentBillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request", err)
		return
	}

	bill, err := fh.financialService.CreatePaymentBill(adminID, req)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Failed to create payment bill", err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Payment bill created successfully", bill)
}

// UpdatePaymentBill updates the status of a payment bill
func (fh *FinancialHandler) UpdatePaymentBill(c *gin.Context) {
	billIDStr := c.Param("bill_id")
	billID, err := strconv.ParseUint(billIDStr, 10, 32)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid bill ID", err)
		return
	}

	var req models.UpdatePaymentBillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationErrorResponse(c, "Invalid request", err)
		return
	}

	bill, err := fh.financialService.UpdatePaymentBill(uint(billID), req)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Failed to update payment bill", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment bill updated successfully", bill)
}

// GetAllPaymentBills returns paginated list of all payment bills for admin
func (fh *FinancialHandler) GetAllPaymentBills(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	status := c.Query("status")

	var organizerID *uuid.UUID
	if organizerIDStr := c.Query("organizer_id"); organizerIDStr != "" {
		if id, err := uuid.Parse(organizerIDStr); err == nil {
			organizerID = &id
		}
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	bills, total, err := fh.financialService.GetPaymentBills(page, limit, organizerID, status)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to get payment bills", err)
		return
	}

	response := map[string]interface{}{
		"bills": bills,
		"pagination": map[string]interface{}{
			"page":        page,
			"limit":       limit,
			"total":       total,
			"total_pages": (total + int64(limit) - 1) / int64(limit),
		},
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment bills retrieved successfully", response)
}

// GetPaymentBillByID returns a specific payment bill
func (fh *FinancialHandler) GetPaymentBillByID(c *gin.Context) {
	billIDStr := c.Param("bill_id")
	billID, err := strconv.ParseUint(billIDStr, 10, 32)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid bill ID", err)
		return
	}

	bill, err := fh.financialService.GetPaymentBillByID(uint(billID))
	if err != nil {
		utils.NotFoundErrorResponse(c, "Payment bill not found", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment bill retrieved successfully", bill)
}

// Organizer APIs

// GetOrganizerFinancialSummary returns financial summary for the authenticated organizer
func (fh *FinancialHandler) GetOrganizerFinancialSummary(c *gin.Context) {
	userIDStr, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "Unauthorized", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", err)
		return
	}

	// Get the organizer ID (handles scoping for staff/managers)
	organizerID, err := fh.getOrganizerIDForUser(userID)
	if err != nil {
		utils.ForbiddenErrorResponse(c, err.Error(), nil)
		return
	}

	summary, err := fh.financialService.GetOrganizerFinancialSummary(organizerID)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to get financial summary", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Financial summary retrieved successfully", summary)
}

// GetOrganizerSales returns sales data for the authenticated organizer
func (fh *FinancialHandler) GetOrganizerSales(c *gin.Context) {
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}

	// Get the organizer ID
	organizerID, err := fh.getOrganizerIDForUser(userID)
	if err != nil {
		utils.ForbiddenErrorResponse(c, err.Error(), nil)
		return
	}

	sales, err := fh.financialService.GetOrganizerSales(organizerID)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to get organizer sales", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Sales data retrieved successfully", sales)
}

// GetOrganizerPaymentBills returns payment bills for the authenticated organizer
func (fh *FinancialHandler) GetOrganizerPaymentBills(c *gin.Context) {
	userIDStr, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "Unauthorized", nil)
		return
	}

	userID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", err)
		return
	}

	// Get the organizer ID (handles scoping for staff/managers)
	organizerID, err := fh.getOrganizerIDForUser(userID)
	if err != nil {
		utils.ForbiddenErrorResponse(c, err.Error(), nil)
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	bills, total, err := fh.financialService.GetPaymentBills(page, limit, &organizerID, status)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to get payment bills", err)
		return
	}

	response := map[string]interface{}{
		"bills": bills,
		"pagination": map[string]interface{}{
			"page":        page,
			"limit":       limit,
			"total":       total,
			"total_pages": (total + int64(limit) - 1) / int64(limit),
		},
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment bills retrieved successfully", response)
}

// GetSpecificOrganizerFinancialSummary returns financial summary for a specific organizer (admin only)
func (fh *FinancialHandler) GetSpecificOrganizerFinancialSummary(c *gin.Context) {
	organizerIDStr := c.Param("organizer_id")
	organizerID, err := uuid.Parse(organizerIDStr)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid organizer ID", err)
		return
	}

	summary, err := fh.financialService.GetOrganizerFinancialSummary(organizerID)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to get financial summary", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Financial summary retrieved successfully", summary)
}

// GetSpecificOrganizerSales returns sales data for a specific organizer (admin only)
func (fh *FinancialHandler) GetSpecificOrganizerSales(c *gin.Context) {
	organizerIDStr := c.Param("organizer_id")
	organizerID, err := uuid.Parse(organizerIDStr)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid organizer ID", err)
		return
	}

	sales, err := fh.financialService.GetOrganizerSales(organizerID)
	if err != nil {
		utils.InternalServerErrorResponse(c, "Failed to get organizer sales", err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Sales data retrieved successfully", sales)
}
