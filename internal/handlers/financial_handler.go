package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/internal/services"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FinancialHandler struct {
	financialService   *services.FinancialService
	ticketService      *services.TicketService
	fileStorageService *services.FileStorageService
}

func NewFinancialHandler(financialService *services.FinancialService, ticketService *services.TicketService, fileStorageService *services.FileStorageService) *FinancialHandler {
	return &FinancialHandler{
		financialService:   financialService,
		ticketService:      ticketService,
		fileStorageService: fileStorageService,
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
		Select("COALESCE(SUM(paid_amount), 0) as total").
		Where("status IN ?", []string{"paid", "partially_paid"}).
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
	results := []models.EventSalesResponse{}
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
			Select("COALESCE(SUM(paid_amount), 0) as paid_amount, MAX(paid_date) as last_payment_date").
			Where("event_id = ? AND status IN ?", results[i].EventID, []string{"paid", "partially_paid"}).
			Scan(&paymentData)

		results[i].PaidAmount = paymentData.PaidAmount
		results[i].DueAmount = results[i].OrganizerShare - results[i].PaidAmount
		results[i].LastPaymentDate = paymentData.LastPaymentDate
		results[i].ID = results[i].EventID // Use event_id as ID for compatibility
	}

	response := map[string]interface{}{
		"sales":      results,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Event sales retrieved successfully", response)
}

// CreatePaymentBill creates a new payment bill for an organizer
// @Summary Create payment bill
// @Description Create a payment bill to track organizer payout for a single event.\n\nThe system automatically calculates the outstanding organizer earnings from completed transactions (minus any previously paid bills). The bill is created with default priority 'normal' and can be updated later for additional details.
// @Tags Financial
// @Security ApiKeyAuth
// @Accept multipart/form-data
// @Produce json
// @Param event_id formData string true "UUID of the event" example(fa50c770-6a8c-4f50-a9fc-84c9dce21fe9)
// @Param organizer_id formData string true "UUID of the organizer" example(dcf2dda4-a490-4898-a402-d301567c2cf6)
// @Param payment_method formData string false "Payment method: bank_transfer | check | cash | mobile_payment | other (optional)"
// @Param screenshot formData file false "Optional payment proof screenshot (jpg/png/pdf, max 10 MB)"
// @Success 201 {object} utils.Response{data=models.PaymentBillResponse} "Bill created"
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/bills [post]
func (fh *FinancialHandler) CreatePaymentBill(c *gin.Context) {
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}
	adminID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Invalid user ID format", nil)
		return
	}

	// Parse multipart form (max 10 MB)
	if err := c.Request.ParseMultipartForm(10 << 20); err != nil {
		utils.HandleError(c, utils.NewValidationError("Failed to parse form: "+err.Error(), nil))
		return
	}

	// --- Required fields ---
	eventIDStr := c.PostForm("event_id")
	organizerIDStr := c.PostForm("organizer_id")
	paymentMethodStr := c.PostForm("payment_method")

	if eventIDStr == "" || organizerIDStr == "" {
		utils.HandleError(c, utils.NewValidationError("event_id and organizer_id are required", nil))
		return
	}

	eventID, err := uuid.Parse(eventIDStr)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid event_id UUID", nil))
		return
	}
	organizerID, err := uuid.Parse(organizerIDStr)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid organizer_id UUID", nil))
		return
	}

	req := models.CreatePaymentBillRequest{
		EventID:     eventID,
		OrganizerID: organizerID,
	}
	if paymentMethodStr != "" {
		pm := models.PaymentMethod(paymentMethodStr)
		req.PaymentMethod = &pm
	}

	bill, err := fh.financialService.CreatePaymentBill(adminID, req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// --- Optional screenshot upload ---
	if screenshotFile, header, ferr := c.Request.FormFile("screenshot"); ferr == nil {
		defer screenshotFile.Close()
		if header.Size > 10*1024*1024 {
			utils.HandleError(c, utils.NewValidationError("Screenshot must be less than 10 MB", nil))
			return
		}
		screenshotURL, uerr := fh.fileStorageService.UploadFile(
			screenshotFile, header,
			models.FileCategoryPaymentProof,
			adminID,
			&services.FileUploadOptions{
				Description: "Payment proof for bill " + bill.BillNumber,
			},
		)
		if uerr != nil {
			utils.HandleError(c, uerr)
			return
		}
		// Persist URL on the bill
		if serr := fh.financialService.SetBillScreenshot(bill.ID, screenshotURL); serr != nil {
			utils.HandleError(c, serr)
			return
		}
		bill.PaymentScreenshotURL = screenshotURL
	}

	utils.SuccessResponse(c, http.StatusCreated, "Payment bill created successfully", bill)
}

// UpdatePaymentBill updates the status of a payment bill
// @Summary Update payment bill
// @Description Record a (partial) payment or change the bill status.\n\nWhen **payment_amount** is provided the service increments **paid_amount** and decrements **remaining_amount** then sets status automatically:\n- remaining_amount == 0 → **paid**\n- remaining_amount > 0  → **partially_paid**\n\nOmit payment_amount for a status-only update (e.g. mark as cancelled or overdue).
// @Tags Financial
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param bill_id path string true "UUID of the bill to update"
// @Param bill body models.UpdatePaymentBillRequest true "Update payload. status is required; payment_amount triggers partial/full payment logic."
// @Success 200 {object} utils.Response{data=models.PaymentBillResponse} "Updated bill with current paid_amount and remaining_amount"
// @Failure 400 {object} utils.Response "payment_amount exceeds remaining_amount, or invalid status value"
// @Failure 404 {object} utils.Response "Bill not found"
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/bills/{bill_id} [put]
func (fh *FinancialHandler) UpdatePaymentBill(c *gin.Context) {
	billIDStr := c.Param("bill_id")
	billID, err := uuid.Parse(billIDStr)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid bill_id UUID", nil))
		return
	}

	var req models.UpdatePaymentBillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleError(c, err)
		return
	}

	bill, err := fh.financialService.UpdatePaymentBill(billID, req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment bill updated successfully", bill)
}

// GetAllPaymentBills returns paginated list of all payment bills for admin
// @Summary Get all payment bills
// @Description Get paginated list of all payment bills with filtering and search options
// @Tags Financial
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param page query int false "Page number (default: 1)"
// @Param limit query int false "Items per page (default: 20, max: 100)"
// @Param status query string false "Filter by status (pending, paid, overdue, cancelled)"
// @Param organizer_id query string false "Filter by organizer ID"
// @Param start_date query string false "Filter bills from this date (YYYY-MM-DD)"
// @Param end_date query string false "Filter bills to this date (YYYY-MM-DD)"
// @Param search query string false "Search by bill ID, organizer name, event title, or payment reference"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/bills [get]
func (fh *FinancialHandler) GetAllPaymentBills(c *gin.Context) {
	pagination := utils.GetPaginationParams(c, 20)
	status := c.Query("status")
	search := c.Query("search")

	var organizerID *uuid.UUID
	if organizerIDStr := c.Query("organizer_id"); organizerIDStr != "" {
		if id, err := uuid.Parse(organizerIDStr); err == nil {
			organizerID = &id
		}
	}

	var startDate, endDate *time.Time
	if startDateStr := c.Query("start_date"); startDateStr != "" {
		if parsedDate, err := time.Parse("2006-01-02", startDateStr); err == nil {
			startDate = &parsedDate
		}
	}
	if endDateStr := c.Query("end_date"); endDateStr != "" {
		if parsedDate, err := time.Parse("2006-01-02", endDateStr); err == nil {
			// Set end date to end of day
			endOfDay := parsedDate.Add(24*time.Hour - time.Second)
			endDate = &endOfDay
		}
	}

	bills, total, err := fh.financialService.GetPaymentBillSummariesWithSearch(pagination.Page, pagination.Limit, organizerID, status, search, startDate, endDate)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	response := map[string]interface{}{
		"bills":      bills,
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment bills retrieved successfully", response)
}

// GetPaymentBillByID returns a specific payment bill
// @Summary Get payment bill by ID
// @Description Get full details of a payment bill including organizer earnings breakdown,\ncurrent paid_amount, remaining_amount, and payment history.
// @Tags Financial
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param bill_id path string true "UUID of the bill"
// @Success 200 {object} utils.Response{data=models.PaymentBillResponse} "Bill detail with billed_amount, paid_amount, remaining_amount and status"
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/bills/{bill_id} [get]
func (fh *FinancialHandler) GetPaymentBillByID(c *gin.Context) {
	billIDStr := c.Param("bill_id")
	billID, err := uuid.Parse(billIDStr)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid bill_id UUID", nil))
		return
	}

	bill, err := fh.financialService.GetPaymentBillByID(billID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment bill retrieved successfully", bill)
}

// DeletePaymentBill deletes a payment bill if no payments have been made
// @Summary Delete payment bill
// @Description Delete a payment bill only if no payments have been recorded against it.\nThis prevents accidental deletion of bills that have financial transactions.
// @Tags Financial
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param bill_id path string true "UUID of the bill"
// @Success 200 {object} utils.Response "Bill deleted successfully"
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 409 {object} utils.Response "Cannot delete bill with existing payments"
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/bills/{bill_id} [delete]
func (fh *FinancialHandler) DeletePaymentBill(c *gin.Context) {
	billIDStr := c.Param("bill_id")
	billID, err := uuid.Parse(billIDStr)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid bill_id UUID", nil))
		return
	}

	// Delete the bill (service method will check for existing payments)
	err = fh.financialService.DeletePaymentBill(billID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment bill deleted successfully", nil)
}

// AddPaymentToBill adds a payment to an existing bill
// @Summary Add payment to bill
// @Description Record an individual payment against a bill. The bill's **paid_amount**
// is incremented and **remaining_amount** decremented. Status transitions automatically
// to **partially_paid** or **paid** depending on whether the balance is cleared.
// @Tags Financial
// @Security ApiKeyAuth
// @Accept multipart/form-data
// @Produce json
// @Param bill_id path string true "UUID of the bill"
// @Param amount formData number true "Amount being paid (must be > 0 and ≤ remaining_amount)"
// @Param payment_method formData string true "bank_transfer | check | cash | mobile_payment | other"
// @Param payment_ref formData string false "External reference e.g. SWIFT ID"
// @Param payment_date formData string false "ISO8601 payment date (defaults to now)"
// @Param notes formData string false "Optional remarks"
// @Param screenshot formData file false "Payment proof screenshot (jpg/png/pdf, max 10 MB)"
// @Success 200 {object} utils.Response{data=models.PaymentBillResponse} "Updated bill with new paid_amount and remaining_amount"
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/bills/{bill_id}/payments [post]
func (fh *FinancialHandler) AddPaymentToBill(c *gin.Context) {
	billIDStr := c.Param("bill_id")
	billID, err := uuid.Parse(billIDStr)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid bill_id UUID", nil))
		return
	}

	// Get admin ID
	adminIDInterface, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("Admin ID not found in context", nil))
		return
	}
	adminUUID, ok := adminIDInterface.(uuid.UUID)
	if !ok {
		utils.HandleError(c, utils.NewInternalServerError("Invalid admin ID format", nil))
		return
	}

	// Parse multipart form (max 10 MB)
	if err := c.Request.ParseMultipartForm(10 << 20); err != nil {
		utils.HandleError(c, utils.NewValidationError("Failed to parse form: "+err.Error(), nil))
		return
	}

	// Required: amount
	amountStr := c.PostForm("amount")
	if amountStr == "" {
		utils.HandleError(c, utils.NewValidationError("amount is required", nil))
		return
	}
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		utils.HandleError(c, utils.NewValidationError("amount must be a number greater than 0", nil))
		return
	}

	// Required: payment_method
	paymentMethodStr := c.PostForm("payment_method")
	if paymentMethodStr == "" {
		utils.HandleError(c, utils.NewValidationError("payment_method is required", nil))
		return
	}

	paymentDate := time.Now()
	if v := c.PostForm("payment_date"); v != "" {
		if t, perr := time.Parse(time.RFC3339, v); perr == nil {
			paymentDate = t
		}
	}

	payment := &models.PaymentHistory{
		PaymentBillID: billID,
		Amount:        amount,
		PaymentMethod: models.PaymentMethod(paymentMethodStr),
		PaymentRef:    c.PostForm("payment_ref"),
		PaymentDate:   paymentDate,
		ProcessedByID: adminUUID,
		Notes:         c.PostForm("notes"),
	}

	// Optional screenshot upload - upload before creating payment
	if screenshotFile, header, ferr := c.Request.FormFile("screenshot"); ferr == nil {
		defer screenshotFile.Close()
		if header.Size > 10*1024*1024 {
			utils.HandleError(c, utils.NewValidationError("Screenshot must be less than 10 MB", nil))
			return
		}
		screenshotURL, uerr := fh.fileStorageService.UploadFile(
			screenshotFile, header,
			models.FileCategoryPaymentProof,
			adminUUID,
			&services.FileUploadOptions{
				Description: "Payment proof for bill " + billID.String(),
			},
		)
		if uerr != nil {
			utils.HandleError(c, uerr)
			return
		}
		payment.ScreenshotURL = screenshotURL
		// Log successful screenshot upload
		fmt.Printf("[DEBUG] Screenshot uploaded successfully: %s\n", screenshotURL)
	} else {
		// Log if no screenshot was provided
		fmt.Printf("[DEBUG] No screenshot provided in form: %v\n", ferr)
	}

	bill, err := fh.financialService.AddPaymentToBill(billID, payment)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment added to bill successfully", bill)
}

// GetBillPaymentHistory returns payment history for a specific bill
// @Summary Get payment history for a bill
// @Description Get all payment records made against a specific bill, ordered by payment date (newest first)
// @Tags Financial
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param bill_id path string true "UUID of the bill"
// @Success 200 {object} utils.Response{data=[]models.PaymentHistoryResponse} "Payment history for the bill"
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/bills/{bill_id}/history [get]
func (fh *FinancialHandler) GetBillPaymentHistory(c *gin.Context) {
	billIDStr := c.Param("bill_id")
	billID, err := uuid.Parse(billIDStr)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid bill_id UUID", nil))
		return
	}

	history, err := fh.financialService.GetBillPaymentHistory(billID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment history retrieved successfully", history)
}

// Organizer APIs

// GetOrganizerFinancialSummary returns financial summary for the authenticated organizer
func (fh *FinancialHandler) GetOrganizerFinancialSummary(c *gin.Context) {
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Invalid user ID format", nil)
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
		Select("COALESCE(SUM(paid_amount), 0) as total").
		Where("organizer_id = ? AND status IN ?", organizerID, []string{"paid", "partially_paid"}).
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
	results := []models.EventSalesResponse{}
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
			Select("COALESCE(SUM(paid_amount), 0) as paid_amount, MAX(paid_date) as last_payment_date").
			Where("event_id = ? AND status IN ?", results[i].EventID, []string{"paid", "partially_paid"}).
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
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.ErrorResponse(c, http.StatusInternalServerError, "Invalid user ID format", nil)
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
		"pagination": utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
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
		Select("COALESCE(SUM(paid_amount), 0) as total").
		Where("organizer_id = ? AND status IN ?", organizerID, []string{"paid", "partially_paid"}).
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
	results := []models.EventSalesResponse{}
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
			Select("COALESCE(SUM(paid_amount), 0) as paid_amount, MAX(paid_date) as last_payment_date").
			Where("event_id = ? AND status IN ?", results[i].EventID, []string{"paid", "partially_paid"}).
			Scan(&paymentData)

		results[i].PaidAmount = paymentData.PaidAmount
		results[i].DueAmount = results[i].OrganizerShare - results[i].PaidAmount
		results[i].LastPaymentDate = paymentData.LastPaymentDate
		results[i].ID = results[i].EventID // Use event_id as ID for compatibility
	}

	utils.SuccessResponse(c, http.StatusOK, "Sales data retrieved successfully", results)
}

// GetAllTransactions returns paginated list of all transactions for admin
// @Summary Get all transactions
// @Description Get paginated list of all transactions with filtering and search options
// @Tags Financial
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param page query int false "Page number (default: 1)"
// @Param limit query int false "Items per page (default: 20, max: 100)"
// @Param status query string false "Filter by status (completed, pending, failed, refunded)"
// @Param payment_gateway query string false "Filter by payment gateway"
// @Param event_id query string false "Filter by event ID"
// @Param user_id query string false "Filter by user ID"
// @Param guest_user_id query string false "Filter by guest user ID"
// @Param start_date query string false "Filter transactions from this date (YYYY-MM-DD)"
// @Param end_date query string false "Filter transactions to this date (YYYY-MM-DD)"
// @Param search query string false "Search by transaction ID, gateway transaction ID, customer name, email, or event title"
// @Param sort_by query string false "Sort by field (created_at, amount, etc.)"
// @Param sort_order query string false "Sort order (asc, desc)"
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/transactions [get]
func (fh *FinancialHandler) GetAllTransactions(c *gin.Context) {
	// Parse pagination parameters
	pagination := utils.GetPaginationParams(c, 20)
	page := pagination.Page
	limit := pagination.Limit
	offset := (page - 1) * limit

	// Parse filter parameters
	status := c.Query("status")
	paymentGateway := c.Query("payment_gateway")
	eventID := c.Query("event_id")
	userID := c.Query("user_id")
	guestUserID := c.Query("guest_user_id")
	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")
	search := c.Query("search")
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	// Validate sort parameters
	validSortFields := map[string]bool{
		"created_at":        true,
		"amount":            true,
		"commission_amount": true,
		"organizer_share":   true,
		"quantity":          true,
	}
	if !validSortFields[sortBy] {
		sortBy = "created_at"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	// Build query
	query := database.GetDB().Model(&models.Transaction{}).
		Select(`
			transactions.id,
			transactions.event_id,
			COALESCE(events.title, '') as event_title,
			transactions.user_id,
			CASE WHEN transactions.user_id IS NOT NULL THEN NULLIF(TRIM(CONCAT(users.first_name, ' ', users.last_name)), '') ELSE NULL END as user_name,
			transactions.guest_user_id,
			CASE WHEN transactions.guest_user_id IS NOT NULL THEN NULLIF(TRIM(CONCAT(guest_users.first_name, ' ', guest_users.last_name)), '') ELSE NULL END as guest_user_name,
			transactions.quantity as ticket_count,
			transactions.payment_gateway,
			transactions.amount,
			transactions.currency,
			transactions.status,
			transactions.gateway_txn_id,
			transactions.commission_rate,
			transactions.commission_amount,
			transactions.organizer_share,
			transactions.created_at,
			transactions.updated_at,
			CASE WHEN transactions.gateway_txn_id IS NOT NULL AND transactions.gateway_txn_id != ''
			     THEN true ELSE false END as has_payment_details
		`).
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Joins("LEFT JOIN users ON transactions.user_id = users.id").
		Joins("LEFT JOIN guest_users ON transactions.guest_user_id = guest_users.id")

	// Apply search filter
	if search != "" {
		searchTerm := "%" + search + "%"
		query = query.Where(`
			transactions.id::text ILIKE ? OR
			transactions.gateway_txn_id ILIKE ? OR
			events.title ILIKE ? OR
			CASE WHEN transactions.user_id IS NOT NULL THEN CONCAT(users.first_name, ' ', users.last_name) ELSE '' END ILIKE ? OR
			CASE WHEN transactions.guest_user_id IS NOT NULL THEN CONCAT(guest_users.first_name, ' ', guest_users.last_name) ELSE '' END ILIKE ? OR
			CASE WHEN transactions.guest_user_id IS NOT NULL THEN guest_users.email ELSE users.email END ILIKE ?
		`, searchTerm, searchTerm, searchTerm, searchTerm, searchTerm, searchTerm)
	}

	// Apply filters
	if status != "" {
		query = query.Where("transactions.status = ?", status)
	}
	if paymentGateway != "" {
		query = query.Where("transactions.payment_gateway = ?", paymentGateway)
	}
	if eventID != "" {
		if _, err := uuid.Parse(eventID); err == nil {
			query = query.Where("transactions.event_id = ?", eventID)
		}
	}
	if userID != "" {
		if _, err := uuid.Parse(userID); err == nil {
			query = query.Where("transactions.user_id = ?", userID)
		}
	}
	if guestUserID != "" {
		if _, err := uuid.Parse(guestUserID); err == nil {
			query = query.Where("transactions.guest_user_id = ?", guestUserID)
		}
	}

	// Date filters
	if startDateStr != "" {
		if startDate, err := time.Parse("2006-01-02", startDateStr); err == nil {
			query = query.Where("transactions.created_at >= ?", startDate)
		}
	}
	if endDateStr != "" {
		if endDate, err := time.Parse("2006-01-02", endDateStr); err == nil {
			endDate = endDate.Add(24 * time.Hour) // Include the entire end date
			query = query.Where("transactions.created_at < ?", endDate)
		}
	}

	// Get total count
	var totalCount int64
	countQuery := database.GetDB().Model(&models.Transaction{}).
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Joins("LEFT JOIN users ON transactions.user_id = users.id").
		Joins("LEFT JOIN guest_users ON transactions.guest_user_id = guest_users.id")

	// Apply same search filter to count query
	if search != "" {
		searchTerm := "%" + search + "%"
		countQuery = countQuery.Where(`
			transactions.id::text ILIKE ? OR
			transactions.gateway_txn_id ILIKE ? OR
			events.title ILIKE ? OR
			CASE WHEN transactions.user_id IS NOT NULL THEN CONCAT(users.first_name, ' ', users.last_name) ELSE '' END ILIKE ? OR
			CASE WHEN transactions.guest_user_id IS NOT NULL THEN CONCAT(guest_users.first_name, ' ', guest_users.last_name) ELSE '' END ILIKE ? OR
			CASE WHEN transactions.guest_user_id IS NOT NULL THEN guest_users.email ELSE users.email END ILIKE ?
		`, searchTerm, searchTerm, searchTerm, searchTerm, searchTerm, searchTerm)
	}

	// Apply same filters to count query
	if status != "" {
		countQuery = countQuery.Where("transactions.status = ?", status)
	}
	if paymentGateway != "" {
		countQuery = countQuery.Where("transactions.payment_gateway = ?", paymentGateway)
	}
	if eventID != "" {
		if _, err := uuid.Parse(eventID); err == nil {
			countQuery = countQuery.Where("transactions.event_id = ?", eventID)
		}
	}
	if userID != "" {
		if _, err := uuid.Parse(userID); err == nil {
			countQuery = countQuery.Where("transactions.user_id = ?", userID)
		}
	}
	if guestUserID != "" {
		if _, err := uuid.Parse(guestUserID); err == nil {
			countQuery = countQuery.Where("transactions.guest_user_id = ?", guestUserID)
		}
	}
	if startDateStr != "" {
		if startDate, err := time.Parse("2006-01-02", startDateStr); err == nil {
			countQuery = countQuery.Where("transactions.created_at >= ?", startDate)
		}
	}
	if endDateStr != "" {
		if endDate, err := time.Parse("2006-01-02", endDateStr); err == nil {
			endDate = endDate.Add(24 * time.Hour)
			countQuery = countQuery.Where("transactions.created_at < ?", endDate)
		}
	}

	if err := countQuery.Count(&totalCount).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Apply sorting and pagination
	orderClause := sortBy + " " + sortOrder
	query = query.Order(orderClause).Limit(limit).Offset(offset)

	// Execute query
	var rows []models.TransactionScanRow
	if err := query.Scan(&rows).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Convert flat rows to nested summary responses
	summaries := make([]models.TransactionSummaryResponse, 0, len(rows))
	for _, r := range rows {
		summaries = append(summaries, r.ToSummaryResponse())
	}

	// Use centralized pagination builder
	response := map[string]interface{}{
		"transactions": summaries,
		"pagination":   utils.BuildPaginationInfo(totalCount, page, limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "Transactions retrieved successfully", response)
}

// GetUserTransactions returns paginated list of transactions for the current user
// @Summary Get user transactions
// @Description Get paginated list of transactions for the authenticated user with search and filter options
// @Tags User
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param page query int false "Page number (default: 1)" default(1)
// @Param limit query int false "Items per page (default: 20)" default(20)
// @Param payment_method query string false "Filter by payment method (stripe, paypal, etc.)"
// @Param date_from query string false "Filter transactions from date (YYYY-MM-DD format)"
// @Param date_to query string false "Filter transactions to date (YYYY-MM-DD format)"
// @Success 200 {object} utils.Response{data=object{transactions=[]models.UserTransactionListingResponse,pagination=object}}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/transactions [get]
func (fh *FinancialHandler) GetUserTransactions(c *gin.Context) {
	// Get user ID from context
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

	// Get pagination parameters
	pagination := utils.GetPaginationParams(c, 20)

	// Get filter parameters
	paymentMethod := c.Query("payment_method")
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")

	// Parse date filters
	var dateFromParsed, dateToParsed *time.Time
	if dateFrom != "" {
		if parsed, err := time.Parse("2006-01-02", dateFrom); err == nil {
			dateFromParsed = &parsed
		} else {
			utils.BadRequestErrorResponse(c, "Invalid date_from parameter. Use YYYY-MM-DD format", nil)
			return
		}
	}
	if dateTo != "" {
		if parsed, err := time.Parse("2006-01-02", dateTo); err == nil {
			// Set to end of day
			endOfDay := parsed.Add(24*time.Hour - time.Second)
			dateToParsed = &endOfDay
		} else {
			utils.BadRequestErrorResponse(c, "Invalid date_to parameter. Use YYYY-MM-DD format", nil)
			return
		}
	}

	// Create filter struct
	filters := models.UserTransactionFilters{
		PaymentMethod: paymentMethod,
		DateFrom:      dateFromParsed,
		DateTo:        dateToParsed,
	}

	// Get user transactions with filters
	transactions, total, err := fh.financialService.GetUserTransactions(userID, pagination.Page, pagination.Limit, filters)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Create response with pagination info
	response := map[string]interface{}{
		"transactions": transactions,
		"pagination":   utils.BuildPaginationInfo(total, pagination.Page, pagination.Limit),
	}

	utils.SuccessResponse(c, http.StatusOK, "User transactions retrieved successfully", response)
}

// RetryTransaction allows users to retry failed transactions
// @Summary Retry a failed transaction
// @Description Create a new payment session for a failed transaction so user can retry payment
// @Tags User, Financial
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body object{transaction_id=string} true "Transaction ID to retry"
// @Success 200 {object} utils.Response{data=object{checkout_url=string,checkout_token=string,transaction_id=string}}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/transactions/retry [post]
func (fh *FinancialHandler) RetryTransaction(c *gin.Context) {
	// Get user ID from context
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

	// Parse request
	var req struct {
		TransactionID string `json:"transaction_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		utils.BadRequestErrorResponse(c, "Invalid request format", err)
		return
	}

	// Parse transaction ID
	transactionID, err := uuid.Parse(req.TransactionID)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid transaction ID format", err)
		return
	}

	// Retry the transaction
	result, err := fh.financialService.RetryTransaction(userID, transactionID, fh.ticketService)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Transaction retry initiated successfully", result)
}

// GetTransactionByID returns details of a specific transaction for admin
// @Summary Get transaction by ID
// @Description Get detailed information about a specific transaction (including soft-deleted ones for admin)
// @Tags Admin, Financial
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param transaction_id path string true "Transaction ID"
// @Success 200 {object} utils.Response{data=models.UserTransactionDetailResponse}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/transactions/{transaction_id} [get]
// GetTransactionByID returns details of a specific transaction for admin
// @Summary Get transaction by ID (Admin)
// @Description Get detailed information about a specific transaction for admin
// @Tags Admin
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param transaction_id path string true "Transaction ID"
// @Success 200 {object} utils.Response{data=models.TransactionSummaryResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/transactions/{transaction_id} [get]
func (fh *FinancialHandler) GetTransactionByID(c *gin.Context) {
	transactionID := c.Param("transaction_id")

	// Validate UUID
	if _, err := uuid.Parse(transactionID); err != nil {
		utils.BadRequestErrorResponse(c, "Invalid transaction ID format", nil)
		return
	}

	// Fetch the transaction row (including soft-deleted)
	var row models.TransactionScanRow
	err := database.GetDB().Unscoped().Model(&models.Transaction{}).
		Select(`
			transactions.id,
			transactions.event_id,
			COALESCE(events.title, '') as event_title,
			transactions.user_id,
			CASE WHEN transactions.user_id IS NOT NULL THEN NULLIF(TRIM(CONCAT(users.first_name, ' ', users.last_name)), '') ELSE NULL END as user_name,
			transactions.guest_user_id,
			CASE WHEN transactions.guest_user_id IS NOT NULL THEN NULLIF(TRIM(CONCAT(guest_users.first_name, ' ', guest_users.last_name)), '') ELSE NULL END as guest_user_name,
			transactions.quantity as ticket_count,
			transactions.payment_gateway,
			transactions.amount,
			transactions.currency,
			transactions.status,
			transactions.gateway_txn_id,
			transactions.commission_rate,
			transactions.commission_amount,
			transactions.organizer_share,
			transactions.created_at,
			transactions.updated_at,
			CASE WHEN transactions.gateway_txn_id IS NOT NULL AND transactions.gateway_txn_id != ''
			     THEN true ELSE false END as has_payment_details
		`).
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Joins("LEFT JOIN users ON transactions.user_id = users.id").
		Joins("LEFT JOIN guest_users ON transactions.guest_user_id = guest_users.id").
		Where("transactions.id = ?", transactionID).
		Scan(&row).Error

	if err != nil {
		utils.HandleError(c, err)
		return
	}
	if row.ID == uuid.Nil {
		utils.NotFoundErrorResponse(c, "Transaction not found", nil)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Transaction retrieved successfully", row.ToSummaryResponse())
}

// GetUserTransactionByID returns details of a specific transaction for the authenticated user
// @Summary Get user transaction by ID
// @Description Get detailed information about a specific transaction for the current user
// @Tags User
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param transaction_id path string true "Transaction ID"
// @Success 200 {object} utils.Response{data=models.UserTransactionDetailResponse}
// @Failure 400 {object} utils.Response
// @Failure 401 {object} utils.Response
// @Failure 403 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/transactions/{transaction_id} [get]
func (fh *FinancialHandler) GetUserTransactionByID(c *gin.Context) {
	// Get user ID from context
	userID, exists := c.Get("userID")
	if !exists {
		utils.UnauthorizedErrorResponse(c, "User not authenticated", nil)
		return
	}

	userUUID, ok := userID.(uuid.UUID)
	if !ok {
		utils.UnauthorizedErrorResponse(c, "Invalid user ID", nil)
		return
	}

	transactionID := c.Param("transaction_id")

	// Validate UUID
	if _, err := uuid.Parse(transactionID); err != nil {
		utils.BadRequestErrorResponse(c, "Invalid transaction ID format", nil)
		return
	}

	// Query transaction with related data, ensuring it belongs to the user
	// Exclude sensitive financial data like commission_rate, commission_amount, organizer_share
	var transaction models.UserTransactionDetailResponse
	err := database.GetDB().Model(&models.Transaction{}).
		Select(`
			transactions.id,
			transactions.event_id,
			events.title as event_title,
			transactions.tier_id,
			event_tiers.tier_name,
			transactions.user_id,
			CASE WHEN transactions.user_id IS NOT NULL THEN CONCAT(users.first_name, ' ', users.last_name) ELSE NULL END as user_name,
			transactions.guest_user_id,
			CASE WHEN transactions.guest_user_id IS NOT NULL THEN CONCAT(guest_users.first_name, ' ', guest_users.last_name) ELSE NULL END as guest_user_name,
			CASE WHEN transactions.guest_user_id IS NOT NULL THEN guest_users.email ELSE users.email END as customer_email,
			transactions.quantity as ticket_count,
			transactions.payment_gateway,
			transactions.amount,
			transactions.currency,
			transactions.status,
			transactions.gateway_txn_id,
			transactions.processed_at,
			transactions.created_at,
			transactions.updated_at
		`).
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Joins("LEFT JOIN event_tiers ON transactions.tier_id = event_tiers.id").
		Joins("LEFT JOIN users ON transactions.user_id = users.id").
		Joins("LEFT JOIN guest_users ON transactions.guest_user_id = guest_users.id").
		Where("transactions.id = ? AND (transactions.user_id = ? OR transactions.guest_user_id IN (SELECT id FROM guest_users WHERE email = (SELECT email FROM users WHERE id = ?)))", transactionID, userUUID, userUUID).
		Scan(&transaction).Error

	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Check if transaction exists and belongs to user
	if transaction.ID == uuid.Nil {
		utils.NotFoundErrorResponse(c, "Transaction not found or access denied", nil)
		return
	}

	// Add invoice information
	companyInfo, err := fh.financialService.GetCompanyInfo()
	if err != nil {
		// Fallback to hardcoded values if API is unavailable
		companyInfo = models.UserTransactionInvoiceInfo{
			CompanyName:    "Event Ticketing Platform",
			CompanyAddress: "Kathmandu, Nepal",
			CompanyPhone:   "+977-1234567890",
			CompanyEmail:   "support@timro.com",
			TaxNumber:      "123456789",
		}
	}

	invoiceInfo := models.UserTransactionInvoiceInfo{
		CompanyName:    companyInfo.CompanyName,
		CompanyAddress: companyInfo.CompanyAddress,
		CompanyPhone:   companyInfo.CompanyPhone,
		CompanyEmail:   companyInfo.CompanyEmail,
		TaxNumber:      companyInfo.TaxNumber,
		InvoiceNumber:  "INV-" + transaction.ID.String()[:8],
		TransactionRef: transaction.GatewayTxnID,
		PaymentGateway: string(transaction.PaymentGateway),
		Currency:       transaction.Currency,
		Subtotal:       transaction.Amount, // For now, no commission calculation in user view
		TaxAmount:      0,                  // No tax calculation for now
		TotalAmount:    transaction.Amount,
		IssueDate:      transaction.CreatedAt,
	}

	transaction.InvoiceInfo = &invoiceInfo

	utils.SuccessResponse(c, http.StatusOK, "Transaction retrieved successfully", transaction)
}

// GetTransactionPaymentIntent returns payment intent details for a specific transaction
// @Summary Get payment intent for transaction (Admin)
// @Description Get payment gateway details for a specific transaction by transaction ID
// @Tags Financial
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param transaction_id path string true "Transaction ID"
// @Success 200 {object} utils.Response{data=models.PaymentIntent}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/transactions/{transaction_id}/payment [get]
func (fh *FinancialHandler) GetTransactionPaymentIntent(c *gin.Context) {
	transactionIDStr := c.Param("transaction_id")
	transactionID, err := uuid.Parse(transactionIDStr)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid transaction ID", nil))
		return
	}

	// Get transaction details
	var transaction models.Transaction
	if err := database.GetDB().First(&transaction, transactionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.HandleError(c, utils.NewNotFoundError("Transaction not found"))
			return
		}
		utils.HandleError(c, err)
		return
	}

	// Find payment intent by matching GatewayTxnID with IdempotencyKey
	var paymentIntent models.PaymentIntent
	if err := database.GetDB().Preload("Event").Preload("Tier").Preload("User").Preload("GuestUser").
		Where("idempotency_key = ?", transaction.GatewayTxnID).
		First(&paymentIntent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.HandleError(c, utils.NewNotFoundError("Payment intent not found for this transaction"))
			return
		}
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment intent retrieved successfully", paymentIntent)
}

// GetTransactionPaymentDetails returns comprehensive payment details for a specific transaction
// @Summary Get payment details for transaction (Admin)
// @Description Get comprehensive payment information including transaction, payment intent, and related details
// @Tags Financial
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param transaction_id path string true "Transaction ID"
// @Success 200 {object} utils.Response{data=models.TransactionPaymentDetailsResponse}
// @Failure 400 {object} utils.Response
// @Failure 404 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/transactions/{transaction_id}/payment-details [get]
func (fh *FinancialHandler) GetTransactionPaymentDetails(c *gin.Context) {
	transactionIDStr := c.Param("transaction_id")
	transactionID, err := uuid.Parse(transactionIDStr)
	if err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid transaction ID", nil))
		return
	}

	// Get transaction with related data
	var transaction models.Transaction
	if err := database.GetDB().Preload("Event").Preload("User").Preload("GuestUser").
		First(&transaction, transactionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.HandleError(c, utils.NewNotFoundError("Transaction not found"))
			return
		}
		utils.HandleError(c, err)
		return
	}

	// Get payment intent details
	var paymentIntent models.PaymentIntent
	paymentIntentFound := true
	if transaction.PaymentIntentID != nil {
		if err := database.GetDB().Preload("Event").Preload("Tier").Preload("User").Preload("GuestUser").
			First(&paymentIntent, *transaction.PaymentIntentID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				paymentIntentFound = false
			} else {
				utils.HandleError(c, err)
				return
			}
		}
	} else {
		paymentIntentFound = false
	}

	// Get associated tickets
	var tickets []models.Ticket
	if err := database.GetDB().Preload("User").Preload("GuestUser").Preload("Tier").Preload("Event").
		Where("transaction_id = ?", transactionID).
		Find(&tickets).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	buildName := func(firstName, lastName string) string {
		name := strings.TrimSpace(strings.TrimSpace(firstName) + " " + strings.TrimSpace(lastName))
		if name == "" {
			return "N/A"
		}
		return name
	}

	buildUserSummary := func(user *models.User, guestUser *models.GuestUser) models.TransactionPaymentDetailsUserSummary {
		if user != nil {
			return models.TransactionPaymentDetailsUserSummary{
				ID:    user.ID,
				Name:  buildName(user.FirstName, user.LastName),
				Email: user.Email,
				Phone: user.Phone,
			}
		}

		if guestUser != nil {
			return models.TransactionPaymentDetailsUserSummary{
				ID:    guestUser.ID,
				Name:  buildName(guestUser.FirstName, guestUser.LastName),
				Email: guestUser.Email,
				Phone: guestUser.Phone,
			}
		}

		return models.TransactionPaymentDetailsUserSummary{Name: "N/A"}
	}

	buildTicketUserSummary := func(user *models.User, guestUser *models.GuestUser) models.TransactionPaymentDetailsTicketUserSummary {
		summary := buildUserSummary(user, guestUser)
		return models.TransactionPaymentDetailsTicketUserSummary{
			ID:    summary.ID,
			Name:  summary.Name,
			Email: summary.Email,
		}
	}

	getMapString := func(data map[string]interface{}, keys ...string) string {
		for _, key := range keys {
			if value, ok := data[key]; ok {
				if text, ok := value.(string); ok {
					trimmed := strings.TrimSpace(text)
					if trimmed != "" {
						return trimmed
					}
				}
			}
		}
		return ""
	}

	getMapInt := func(data map[string]interface{}, key string) int {
		if value, ok := data[key]; ok {
			switch typed := value.(type) {
			case int:
				return typed
			case int32:
				return int(typed)
			case int64:
				return int(typed)
			case float32:
				return int(typed)
			case float64:
				return int(typed)
			case string:
				if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
					return parsed
				}
			}
		}
		return 0
	}

	// Build response
	response := models.TransactionPaymentDetailsResponse{
		Transaction: models.TransactionPaymentDetailsTransactionSummary{
			ID: transaction.ID,
			Event: models.TransactionPaymentDetailsEventSummary{
				ID: transaction.EventID,
				Name: func() string {
					if transaction.Event != nil {
						return transaction.Event.Title
					}
					return ""
				}(),
				Banner: func() string {
					if transaction.Event != nil {
						return transaction.Event.BannerImage
					}
					return ""
				}(),
			},
			User:           buildUserSummary(transaction.User, transaction.GuestUser),
			PaymentGateway: transaction.PaymentGateway,
			Amount:         transaction.Amount,
			Currency:       transaction.Currency,
			Quantity:       transaction.Quantity,
			Status:         transaction.Status,
			CreatedAt:      transaction.CreatedAt,
			UpdatedAt:      transaction.UpdatedAt,
		},
		Tickets: make([]models.TransactionPaymentDetailsTicketSummary, 0, len(tickets)),
	}

	for _, ticket := range tickets {
		tierSummary := models.TransactionPaymentDetailsTicketTierSummary{ID: ticket.TierID}
		if ticket.Tier != nil {
			tierSummary.ID = ticket.Tier.ID
			tierSummary.TierName = ticket.Tier.TierName
		}

		response.Tickets = append(response.Tickets, models.TransactionPaymentDetailsTicketSummary{
			ID:              ticket.ID,
			TicketNumber:    ticket.TicketNumber,
			User:            buildTicketUserSummary(ticket.User, ticket.GuestUser),
			Tier:            tierSummary,
			IsGuestPurchase: ticket.IsGuestPurchase,
			TotalAmount:     ticket.TotalAmount,
			Status:          ticket.Status,
			CreatedAt:       ticket.CreatedAt,
			UpdatedAt:       ticket.UpdatedAt,
		})
	}

	if paymentIntentFound {
		cardBrand := getMapString(paymentIntent.PaymentMethodDetails, "brand", "card_brand")
		cardLast4 := getMapString(paymentIntent.PaymentMethodDetails, "last4", "card_last4")
		expMonth := getMapInt(paymentIntent.PaymentMethodDetails, "exp_month")
		expYear := getMapInt(paymentIntent.PaymentMethodDetails, "exp_year")

		if cardBrand == "" {
			cardBrand = getMapString(paymentIntent.GatewayResponse, "card_brand", "brand")
		}
		if cardLast4 == "" {
			cardLast4 = getMapString(paymentIntent.GatewayResponse, "card_last4", "last4")
		}

		maskedCardNumber := ""
		if cardLast4 != "" {
			maskedCardNumber = fmt.Sprintf("**** **** **** %s", cardLast4)
		}

		response.PaymentIntent = &models.TransactionPaymentIntentSummary{
			ID:               paymentIntent.ID,
			Status:           paymentIntent.Status,
			PaymentGateway:   paymentIntent.PaymentGateway,
			PaymentMethod:    paymentIntent.PaymentMethodType,
			CardBrand:        cardBrand,
			CardLast4:        cardLast4,
			MaskedCardNumber: maskedCardNumber,
			ExpMonth:         expMonth,
			ExpYear:          expYear,
			CustomerEmail:    paymentIntent.CustomerEmail,
			CreatedAt:        paymentIntent.CreatedAt,
		}
	} else {
		// Always include payment intent information, even if no PaymentIntent record exists
		// Extract basic payment info from transaction gateway data
		response.PaymentIntent = &models.TransactionPaymentIntentSummary{
			ID:               uuid.Nil, // No PaymentIntent record
			Status:           "unknown",
			PaymentGateway:   string(transaction.PaymentGateway),
			PaymentMethod:    "unknown",
			CardBrand:        "",
			CardLast4:        "",
			MaskedCardNumber: "",
			ExpMonth:         0,
			ExpYear:          0,
			CustomerEmail:    "",
			CreatedAt:        transaction.CreatedAt,
		}
	}

	utils.SuccessResponse(c, http.StatusOK, "Transaction payment details retrieved successfully", response)
}

// GetAuditLogs godoc
// @Summary Get audit logs (Admin)
// @Description Retrieve audit logs for financial operations with filtering and pagination
// @Tags Admin
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(50)
// @Param action query string false "Filter by action (e.g., payment_created, refund_approved)"
// @Param entity_type query string false "Filter by entity type (e.g., payment, refund, gateway_config)"
// @Param entity_id query string false "Filter by entity ID (UUID)"
// @Param actor_id query string false "Filter by actor ID (UUID)"
// @Param event_id query string false "Filter by event ID (UUID)"
// @Param start_date query string false "Filter logs from this date (YYYY-MM-DD)"
// @Param end_date query string false "Filter logs until this date (YYYY-MM-DD)"
// @Success 200 {object} utils.Response{data=models.GetAuditLogsResponse} "Audit logs retrieved successfully"
// @Failure 400 {object} utils.Response "Invalid query parameters"
// @Failure 401 {object} utils.Response "Unauthorized"
// @Failure 403 {object} utils.Response "Forbidden"
// @Failure 500 {object} utils.Response "Internal server error"
// @Router /api/v1/admin/payments/audit-logs [get]
func (fh *FinancialHandler) GetAuditLogs(c *gin.Context) {
	var req models.GetAuditLogsRequest

	// Set defaults
	req.Page = 1
	req.Limit = 50

	// Bind query parameters
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.HandleError(c, utils.NewValidationError("Invalid query parameters: "+err.Error(), nil))
		return
	}

	// Get audit logs
	response, err := fh.financialService.GetAuditLogs(req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Audit logs retrieved successfully", response)
}
