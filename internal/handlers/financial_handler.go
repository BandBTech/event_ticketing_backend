package handlers

import (
	"net/http"
	"strconv"
	"time"

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

// getOrganizerIDForUser uses centralized utility
func (fh *FinancialHandler) getOrganizerIDForUser(userID uuid.UUID) (uuid.UUID, error) {
	return utils.GetOrganizerIDForUser(database.GetDB(), userID)
}

// Admin APIs

// GetAdminFinancialSummary returns overall financial summary for admin
func (fh *FinancialHandler) GetAdminFinancialSummary(c *gin.Context) {
	// Calculate summary from transactions table
	var result struct {
		TotalGrossRevenue     float64
		TotalCommissions      float64
		TotalOrganizerShare   float64
		TotalTicketsSold      int64
		ActiveEvents          int64
		AverageCommissionRate float64
	}

	err := database.GetDB().Model(&models.Transaction{}).
		Select(`
			COALESCE(SUM(amount), 0) as total_gross_revenue,
			COALESCE(SUM(commission_amount), 0) as total_commissions,
			COALESCE(SUM(organizer_share), 0) as total_organizer_share,
			COALESCE(SUM(quantity), 0) as total_tickets_sold,
			COUNT(DISTINCT event_id) as active_events,
			COALESCE(AVG(commission_rate), 0) as average_commission_rate
		`).
		Where("status = ?", "completed").
		Scan(&result).Error

	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Calculate total paid out from payment bills
	var totalPaidOut struct {
		Total float64
	}
	database.GetDB().Model(&models.PaymentBill{}).
		Select("COALESCE(SUM(bill_amount), 0) as total").
		Where("status = ?", "paid").
		Scan(&totalPaidOut)

	// Calculate total due (organizer share - paid out)
	totalDue := result.TotalOrganizerShare - totalPaidOut.Total

	summary := models.AdminFinancialSummary{
		TotalGrossRevenue:     result.TotalGrossRevenue,
		TotalCommissions:      result.TotalCommissions,
		TotalOrganizerShare:   result.TotalOrganizerShare,
		TotalPaidOut:          totalPaidOut.Total,
		TotalDue:              totalDue,
		ActiveEvents:          result.ActiveEvents,
		TotalTicketsSold:      result.TotalTicketsSold,
		AverageCommissionRate: result.AverageCommissionRate,
	}

	utils.SuccessResponse(c, http.StatusOK, "Financial summary retrieved successfully", summary)
}

// GetAllEventSales returns paginated list of all event sales for admin
func (fh *FinancialHandler) GetAllEventSales(c *gin.Context) {
	pagination := utils.GetPaginationParams(c, 20)

	var organizerID *uuid.UUID
	if organizerIDStr := c.Query("organizer_id"); organizerIDStr != "" {
		if id, err := uuid.Parse(organizerIDStr); err == nil {
			organizerID = &id
		}
	}

	// Calculate event sales from transactions
	query := database.GetDB().Model(&models.Transaction{}).
		Select(`
			event_id,
			events.title as event_title,
			events.organizer_id as organizer_id,
			COALESCE(users.first_name || ' ' || users.last_name, organizer_onboardings.business_name) as organizer_name,
			SUM(quantity) as total_tickets_sold,
			SUM(amount) as gross_revenue,
			AVG(commission_rate) as commission_rate,
			SUM(commission_amount) as commission_amount,
			SUM(organizer_share) as organizer_share,
			MIN(transactions.created_at) as created_at,
			MAX(transactions.updated_at) as updated_at
		`).
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Joins("LEFT JOIN users ON events.organizer_id = users.id").
		Joins("LEFT JOIN organizer_onboardings ON users.id = organizer_onboardings.user_id").
		Where("transactions.status = ?", "completed").
		Group("event_id, events.title, events.organizer_id, COALESCE(users.first_name || ' ' || users.last_name, organizer_onboardings.business_name)")

	if organizerID != nil {
		query = query.Where("events.organizer_id = ?", *organizerID)
	}

	// Get total count for pagination
	var total int64
	countQuery := database.GetDB().Model(&models.Transaction{}).
		Select("COUNT(DISTINCT event_id)").
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Where("transactions.status = ?", "completed")

	if organizerID != nil {
		countQuery = countQuery.Where("events.organizer_id = ?", *organizerID)
	}

	if err := countQuery.Scan(&total).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get paginated results
	offset := (pagination.Page - 1) * pagination.Limit
	var results []models.EventSalesResponse
	if err := query.Order("updated_at DESC").Offset(offset).Limit(pagination.Limit).Scan(&results).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Calculate paid and due amounts from payment bills
	for i := range results {
		var paymentData struct {
			PaidAmount      float64
			LastPaymentDate *time.Time
		}
		database.GetDB().Model(&models.PaymentBill{}).
			Select("COALESCE(SUM(bill_amount), 0) as paid_amount, MAX(paid_date) as last_payment_date").
			Where("event_id = ? AND status = ?", results[i].EventID, "paid").
			Scan(&paymentData)

		results[i].PaidAmount = paymentData.PaidAmount
		results[i].DueAmount = results[i].OrganizerShare - results[i].PaidAmount
		results[i].LastPaymentDate = paymentData.LastPaymentDate
		results[i].ID = results[i].EventID // Use event_id as ID for compatibility
	}

	response := map[string]interface{}{
		"sales":      results,
		"pagination": utils.BuildPaginatedResponse(nil, total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Event sales retrieved successfully", response)
}

// CreatePaymentBill creates a new payment bill for an organizer
func (fh *FinancialHandler) CreatePaymentBill(c *gin.Context) {
	userIDStr, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	adminID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	var req models.CreatePaymentBillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	bill, err := fh.financialService.CreatePaymentBill(adminID, req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusCreated, "Payment bill created successfully", bill)
}

// UpdatePaymentBill updates the status of a payment bill
func (fh *FinancialHandler) UpdatePaymentBill(c *gin.Context) {
	billIDStr := c.Param("bill_id")
	billID, err := strconv.ParseUint(billIDStr, 10, 32)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	var req models.UpdatePaymentBillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	bill, err := fh.financialService.UpdatePaymentBill(uint(billID), req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment bill updated successfully", bill)
}

// GetAllPaymentBills returns paginated list of all payment bills for admin
func (fh *FinancialHandler) GetAllPaymentBills(c *gin.Context) {
	pagination := utils.GetPaginationParams(c, 20)
	status := c.Query("status")

	var organizerID *uuid.UUID
	if organizerIDStr := c.Query("organizer_id"); organizerIDStr != "" {
		if id, err := uuid.Parse(organizerIDStr); err == nil {
			organizerID = &id
		}
	}

	bills, total, err := fh.financialService.GetPaymentBills(pagination.Page, pagination.Limit, organizerID, status)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"bills":      bills,
		"pagination": utils.BuildPaginatedResponse(nil, total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment bills retrieved successfully", response)
}

// GetPaymentBillByID returns a specific payment bill
func (fh *FinancialHandler) GetPaymentBillByID(c *gin.Context) {
	billIDStr := c.Param("bill_id")
	billID, err := strconv.ParseUint(billIDStr, 10, 32)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	bill, err := fh.financialService.GetPaymentBillByID(uint(billID))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment bill retrieved successfully", bill)
}

// Organizer APIs

// GetOrganizerFinancialSummary returns financial summary for the authenticated organizer
func (fh *FinancialHandler) GetOrganizerFinancialSummary(c *gin.Context) {
	userIDStr, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	userID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get the organizer ID (handles scoping for staff/managers)
	organizerID, err := fh.getOrganizerIDForUser(userID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Calculate summary from transactions
	var result struct {
		TotalEvents       int64
		TotalTicketsSold  int64
		TotalGrossRevenue float64
		TotalEarnings     float64
	}

	err = database.GetDB().Model(&models.Transaction{}).
		Select(`
			COUNT(DISTINCT event_id) as total_events,
			COALESCE(SUM(quantity), 0) as total_tickets_sold,
			COALESCE(SUM(amount), 0) as total_gross_revenue,
			COALESCE(SUM(organizer_share), 0) as total_earnings
		`).
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Where("events.organizer_id = ? AND transactions.status = ?", organizerID, "completed").
		Scan(&result).Error

	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get organizer name
	var organizer models.User
	if err := database.GetDB().Preload("OrganizerOnboarding").First(&organizer, organizerID).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	organizerName := organizer.FirstName + " " + organizer.LastName
	if organizer.OrganizerOnboarding != nil && organizer.OrganizerOnboarding.BusinessName != "" {
		organizerName = organizer.OrganizerOnboarding.BusinessName
	}

	// Calculate total received from payment bills
	var totalReceived struct {
		Total float64
	}
	database.GetDB().Model(&models.PaymentBill{}).
		Select("COALESCE(SUM(bill_amount), 0) as total").
		Where("organizer_id = ? AND status = ?", organizerID, "paid").
		Scan(&totalReceived)

	summary := models.OrganizerFinancialSummary{
		OrganizerID:       organizerID,
		OrganizerName:     organizerName,
		TotalEvents:       result.TotalEvents,
		TotalTicketsSold:  result.TotalTicketsSold,
		TotalGrossRevenue: result.TotalGrossRevenue,
		TotalEarnings:     result.TotalEarnings,
		TotalReceived:     totalReceived.Total,
		AmountDue:         result.TotalEarnings - totalReceived.Total,
	}

	utils.SuccessResponse(c, http.StatusOK, "Financial summary retrieved successfully", summary)
}

// GetOrganizerSales returns sales data for the authenticated organizer
func (fh *FinancialHandler) GetOrganizerSales(c *gin.Context) {
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get the organizer ID
	organizerID, err := fh.getOrganizerIDForUser(userID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Calculate sales data from transactions
	var results []models.EventSalesResponse
	err = database.GetDB().Model(&models.Transaction{}).
		Select(`
			event_id,
			events.title as event_title,
			events.organizer_id as organizer_id,
			COALESCE(users.first_name || ' ' || users.last_name, organizer_onboardings.business_name) as organizer_name,
			SUM(quantity) as total_tickets_sold,
			SUM(amount) as gross_revenue,
			AVG(commission_rate) as commission_rate,
			SUM(commission_amount) as commission_amount,
			SUM(organizer_share) as organizer_share,
			MIN(transactions.created_at) as created_at,
			MAX(transactions.updated_at) as updated_at
		`).
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Joins("LEFT JOIN users ON events.organizer_id = users.id").
		Joins("LEFT JOIN organizer_onboardings ON users.id = organizer_onboardings.user_id").
		Where("events.organizer_id = ? AND transactions.status = ?", organizerID, "completed").
		Group("event_id, events.title, events.organizer_id, COALESCE(users.first_name || ' ' || users.last_name, organizer_onboardings.business_name)").
		Order("updated_at DESC").
		Scan(&results).Error

	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Calculate paid and due amounts from payment bills
	for i := range results {
		var paymentData struct {
			PaidAmount      float64
			LastPaymentDate *time.Time
		}
		database.GetDB().Model(&models.PaymentBill{}).
			Select("COALESCE(SUM(bill_amount), 0) as paid_amount, MAX(paid_date) as last_payment_date").
			Where("event_id = ? AND status = ?", results[i].EventID, "paid").
			Scan(&paymentData)

		results[i].PaidAmount = paymentData.PaidAmount
		results[i].DueAmount = results[i].OrganizerShare - results[i].PaidAmount
		results[i].LastPaymentDate = paymentData.LastPaymentDate
		results[i].ID = results[i].EventID // Use event_id as ID for compatibility
	}

	utils.SuccessResponse(c, http.StatusOK, "Sales data retrieved successfully", results)
}

// GetOrganizerPaymentBills returns payment bills for the authenticated organizer
func (fh *FinancialHandler) GetOrganizerPaymentBills(c *gin.Context) {
	userIDStr, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	userID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get the organizer ID (handles scoping for staff/managers)
	organizerID, err := fh.getOrganizerIDForUser(userID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	pagination := utils.GetPaginationParams(c, 20)
	status := c.Query("status")

	bills, total, err := fh.financialService.GetPaymentBills(pagination.Page, pagination.Limit, &organizerID, status)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"bills":      bills,
		"pagination": utils.BuildPaginatedResponse(nil, total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment bills retrieved successfully", response)
}

// GetSpecificOrganizerFinancialSummary returns financial summary for a specific organizer (admin only)
func (fh *FinancialHandler) GetSpecificOrganizerFinancialSummary(c *gin.Context) {
	organizerIDStr := c.Param("organizer_id")
	organizerID, err := uuid.Parse(organizerIDStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Calculate summary from transactions
	var result struct {
		TotalEvents       int64
		TotalTicketsSold  int64
		TotalGrossRevenue float64
		TotalEarnings     float64
	}

	err = database.GetDB().Model(&models.Transaction{}).
		Select(`
			COUNT(DISTINCT event_id) as total_events,
			COALESCE(SUM(quantity), 0) as total_tickets_sold,
			COALESCE(SUM(amount), 0) as total_gross_revenue,
			COALESCE(SUM(organizer_share), 0) as total_earnings
		`).
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Where("events.organizer_id = ? AND transactions.status = ?", organizerID, "completed").
		Scan(&result).Error

	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Get organizer name
	var organizer models.User
	if err := database.GetDB().Preload("OrganizerOnboarding").First(&organizer, organizerID).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	organizerName := organizer.FirstName + " " + organizer.LastName
	if organizer.OrganizerOnboarding != nil && organizer.OrganizerOnboarding.BusinessName != "" {
		organizerName = organizer.OrganizerOnboarding.BusinessName
	}

	// Calculate total received from payment bills
	var totalReceived struct {
		Total float64
	}
	database.GetDB().Model(&models.PaymentBill{}).
		Select("COALESCE(SUM(bill_amount), 0) as total").
		Where("organizer_id = ? AND status = ?", organizerID, "paid").
		Scan(&totalReceived)

	summary := models.OrganizerFinancialSummary{
		OrganizerID:       organizerID,
		OrganizerName:     organizerName,
		TotalEvents:       result.TotalEvents,
		TotalTicketsSold:  result.TotalTicketsSold,
		TotalGrossRevenue: result.TotalGrossRevenue,
		TotalEarnings:     result.TotalEarnings,
		TotalReceived:     totalReceived.Total,
		AmountDue:         result.TotalEarnings - totalReceived.Total,
	}

	utils.SuccessResponse(c, http.StatusOK, "Financial summary retrieved successfully", summary)
}

// GetSpecificOrganizerSales returns sales data for a specific organizer (admin only)
func (fh *FinancialHandler) GetSpecificOrganizerSales(c *gin.Context) {
	organizerIDStr := c.Param("organizer_id")
	organizerID, err := uuid.Parse(organizerIDStr)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Calculate sales data from transactions
	var results []models.EventSalesResponse
	err = database.GetDB().Model(&models.Transaction{}).
		Select(`
			event_id,
			events.title as event_title,
			events.organizer_id as organizer_id,
			COALESCE(users.first_name || ' ' || users.last_name, organizer_onboardings.business_name) as organizer_name,
			SUM(quantity) as total_tickets_sold,
			SUM(amount) as gross_revenue,
			AVG(commission_rate) as commission_rate,
			SUM(commission_amount) as commission_amount,
			SUM(organizer_share) as organizer_share,
			MIN(transactions.created_at) as created_at,
			MAX(transactions.updated_at) as updated_at
		`).
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Joins("LEFT JOIN users ON events.organizer_id = users.id").
		Joins("LEFT JOIN organizer_onboardings ON users.id = organizer_onboardings.user_id").
		Where("events.organizer_id = ? AND transactions.status = ?", organizerID, "completed").
		Group("event_id, events.title, events.organizer_id, COALESCE(users.first_name || ' ' || users.last_name, organizer_onboardings.business_name)").
		Order("updated_at DESC").
		Scan(&results).Error

	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Calculate paid and due amounts from payment bills
	for i := range results {
		var paymentData struct {
			PaidAmount      float64
			LastPaymentDate *time.Time
		}
		database.GetDB().Model(&models.PaymentBill{}).
			Select("COALESCE(SUM(bill_amount), 0) as paid_amount, MAX(paid_date) as last_payment_date").
			Where("event_id = ? AND status = ?", results[i].EventID, "paid").
			Scan(&paymentData)

		results[i].PaidAmount = paymentData.PaidAmount
		results[i].DueAmount = results[i].OrganizerShare - results[i].PaidAmount
		results[i].LastPaymentDate = paymentData.LastPaymentDate
		results[i].ID = results[i].EventID // Use event_id as ID for compatibility
	}

	utils.SuccessResponse(c, http.StatusOK, "Sales data retrieved successfully", results)
}
