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
	"event-ticketing-backend/pkg/currency"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FinancialHandler struct {
	financialService   *services.FinancialService
	transactionService *services.TransactionService
	billService        *services.BillService
	ticketService      *services.TicketService
	fileStorageService *services.FileStorageService
}

func NewFinancialHandler(financialService *services.FinancialService, transactionService *services.TransactionService, ticketService *services.TicketService, fileStorageService *services.FileStorageService, billService *services.BillService) *FinancialHandler {
	return &FinancialHandler{
		financialService:   financialService,
		transactionService: transactionService,
		billService:        billService,
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
			Select("COALESCE(SUM(paid_amount), 0) as paid_amount, MAX(updated_at) as last_payment_date").
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
	paymentMethodStr := c.PostForm("method")
	if paymentMethodStr == "" {
		paymentMethodStr = c.PostForm("payment_method")
	}

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
		Type:        models.BillTypePayout,
	}
	if paymentMethodStr != "" {
		req.Notes = "Preferred payout method: " + paymentMethodStr
	}

	bill, err := fh.billService.CreatePaymentBill(adminID, req)
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
		if serr := fh.billService.SetBillScreenshot(bill.ID, screenshotURL); serr != nil {
			utils.HandleError(c, serr)
			return
		}
		_ = screenshotURL
	}

	utils.SuccessResponse(c, http.StatusCreated, "Payment bill created successfully", bill)
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
// @Param status query string false "Filter by multiple statuses (comma-separated: pending, paid, overdue, cancelled)"
// @Param organizer_ids query string false "Filter by multiple organizer IDs (comma-separated UUIDs)"
// @Param start_date query string false "Filter bills from this date (YYYY-MM-DD)"
// @Param end_date query string false "Filter bills to this date (YYYY-MM-DD)"
// @Param search query string false "Search by bill ID, organizer name, event title, or payment reference"
// @Param sort_by query string false "Sort by field (created_at, event_title, organizer_name, billed_amount, status)" default(created_at)
// @Param sort_order query string false "Sort order (asc, desc)" default(desc)
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/payments/bills [get]
func (fh *FinancialHandler) GetAllPaymentBills(c *gin.Context) {
	pagination := utils.GetPaginationParams(c, 20)
	status := c.Query("status")
	search := c.Query("search")
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	// Validate sort parameters using centralized utility
	sortBy, sortOrder = utils.ValidateSortForPaymentBills(sortBy, sortOrder)

	var organizerIDs []uuid.UUID
	if organizerIDsStr := c.Query("organizer_ids"); organizerIDsStr != "" {
		// Parse comma-separated organizer IDs
		idStrings := strings.Split(organizerIDsStr, ",")
		for _, idStr := range idStrings {
			idStr = strings.TrimSpace(idStr)
			if idStr != "" {
				if id, err := uuid.Parse(idStr); err == nil {
					organizerIDs = append(organizerIDs, id)
				}
			}
		}
	}

	var statuses []string
	if status != "" {
		// Parse comma-separated statuses
		statusStrings := strings.Split(status, ",")
		for _, statusStr := range statusStrings {
			statusStr = strings.TrimSpace(statusStr)
			if statusStr != "" {
				statuses = append(statuses, statusStr)
			}
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

	bills, total, err := fh.billService.GetPaymentBillSummariesWithSearch(pagination.Page, pagination.Limit, organizerIDs, statuses, search, startDate, endDate, sortBy, sortOrder)
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

	bill, err := fh.billService.GetPaymentBillByID(billID)
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
	err = fh.billService.DeletePaymentBill(billID)
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
		Method:        paymentMethodStr,
		Reference:     c.PostForm("reference"),
		PaidAt:        paymentDate,
		ProcessedByID: adminUUID,
		Notes:         c.PostForm("notes"),
	}
	if payment.Reference == "" {
		payment.Reference = c.PostForm("payment_ref")
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

	bill, err := fh.billService.AddPaymentToBill(billID, payment)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Payment added to bill successfully", bill)
}

// GetBillPaymentHistory returns payment history for a specific bill
// @Summary Get payment history for a bill
// @Description Get all payment records made against a specific bill with filtering, sorting and search options
// @Tags Financial
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param bill_id path string true "UUID of the bill"
// @Param search query string false "Search by payment reference, notes, or payment method"
// @Param payment_method query string false "Filter by payment method (cash, bank_transfer, check, other)"
// @Param start_date query string false "Filter payments from this date (YYYY-MM-DD)"
// @Param end_date query string false "Filter payments to this date (YYYY-MM-DD)"
// @Param sort_by query string false "Sort by field (payment_date, amount, payment_method, payment_ref, processed_by, notes, created_at)" default(payment_date)
// @Param sort_order query string false "Sort order (asc, desc)" default(desc)
// @Param limit query int false "Limit number of results (0 for all)" default(0)
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

	// Parse query parameters
	search := c.Query("search")
	paymentMethod := c.Query("payment_method")
	sortBy := c.DefaultQuery("sort_by", "payment_date")
	sortOrder := c.DefaultQuery("sort_order", "desc")
	limitStr := c.DefaultQuery("limit", "0")

	// Parse date filters
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

	// Validate sort parameters using centralized utility
	sortBy, sortOrder = utils.ValidateSortForPaymentHistory(sortBy, sortOrder)

	// Parse limit
	limit := 0
	if limitStr != "0" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	history, err := fh.billService.GetBillPaymentHistory(billID, search, paymentMethod, startDate, endDate, sortBy, sortOrder, limit)
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
		TotalRefunds      float64
	}

	err = database.GetDB().Model(&models.Transaction{}).
		Select(`
			COUNT(DISTINCT event_id) as total_events,
			COALESCE(SUM(quantity), 0) as total_tickets_sold,
			COALESCE(SUM(amount), 0) as total_gross_revenue,
			COALESCE(SUM(organizer_share), 0) as gross_earnings,
			COALESCE(SUM(r.organizer_refund), 0) as total_refunds
		`).
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Joins("LEFT JOIN refunds r ON r.transaction_id = transactions.id AND r.status = 'completed'").
		Where("events.organizer_id = ? AND transactions.status = ?", organizerID, "completed").
		Scan(&result).Error

	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Calculate net earnings (gross earnings minus refunds)
	result.TotalEarnings = result.TotalEarnings - result.TotalRefunds

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
			Select("COALESCE(SUM(paid_amount), 0) as paid_amount, MAX(updated_at) as last_payment_date").
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

	bills, total, err := fh.billService.GetPaymentBills(pagination.Page, pagination.Limit, &organizerID, status)
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
			Select("COALESCE(SUM(paid_amount), 0) as paid_amount, MAX(updated_at) as last_payment_date").
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
// @Param sort_by query string false "Sort by field (created_at, amount, commission_amount, organizer_share, quantity, event_title, user_name, payment_gateway, status)" default(created_at)
// @Param sort_order query string false "Sort order (asc, desc)" default(desc)
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/transactions [get]
func (fh *FinancialHandler) GetAllTransactions(c *gin.Context) {
	// Pagination
	pagination := utils.GetPaginationParams(c, 20)
	page := pagination.Page
	limit := pagination.Limit
	offset := (page - 1) * limit

	// Filters
	status := c.Query("status")
	paymentGateway := c.Query("payment_gateway")
	eventID := c.Query("event_id")
	actorID := c.Query("actor_id")
	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")
	search := c.Query("search")
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	// Validate sort
	sortBy, sortOrder = utils.ValidateSortForAdminTransactions(sortBy, sortOrder)

	db := database.GetDB()

	// Base query
	query := db.Model(&models.Transaction{}).
		Select(`
			transactions.id,
			transactions.event_id,
			COALESCE(events.title, '') as event_title,

			transactions.actor_id,
			transactions.actor_type,

			CASE
				WHEN transactions.actor_type = 'user'
				THEN NULLIF(TRIM(CONCAT(users.first_name, ' ', users.last_name)), '')
				ELSE NULL
			END as user_name,

			CASE
				WHEN transactions.actor_type = 'user'
				THEN users.email
				ELSE NULL
			END as user_email,

			CASE
				WHEN transactions.actor_type = 'guest'
				THEN NULLIF(TRIM(CONCAT(guest_users.first_name, ' ', guest_users.last_name)), '')
				ELSE NULL
			END as guest_user_name,

			CASE
				WHEN transactions.actor_type = 'guest'
				THEN guest_users.email
				ELSE NULL
			END as guest_user_email,

			transactions.quantity as quantity,
			transactions.payment_gateway,

			transactions.amount_total,
			transactions.currency,
			transactions.platform_fee,
			transactions.gateway_fee,
			transactions.organizer_earning,

			events.commission_rate,

			transactions.provider_charge_id,

			transactions.status,
			transactions.is_paid_out,

			transactions.created_at,
			transactions.updated_at,

			CASE
				WHEN transactions.provider_charge_id IS NOT NULL
				     AND transactions.provider_charge_id != ''
				THEN true
				ELSE false
			END as has_payment_details
		`).
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Joins(`
			LEFT JOIN users
			ON transactions.actor_id = users.id
			AND transactions.actor_type = 'user'
		`).
		Joins(`
			LEFT JOIN guest_users
			ON transactions.actor_id = guest_users.id
			AND transactions.actor_type = 'guest'
		`)

	// Search
	if search != "" {
		searchTerm := "%" + search + "%"

		query = query.Where(`
			transactions.id::text ILIKE ? OR
			transactions.provider_charge_id ILIKE ? OR
			events.title ILIKE ? OR
			CONCAT(COALESCE(users.first_name, ''), ' ', COALESCE(users.last_name, '')) ILIKE ? OR
			CONCAT(COALESCE(guest_users.first_name, ''), ' ', COALESCE(guest_users.last_name, '')) ILIKE ? OR
			COALESCE(users.email, guest_users.email) ILIKE ?
		`,
			searchTerm,
			searchTerm,
			searchTerm,
			searchTerm,
			searchTerm,
			searchTerm,
		)
	}

	// Filters
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

	if actorID != "" {
		if _, err := uuid.Parse(actorID); err == nil {
			query = query.Where("transactions.actor_id = ?", actorID)
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
			endDate = endDate.Add(24 * time.Hour)
			query = query.Where("transactions.created_at < ?", endDate)
		}
	}

	// Count query
	countQuery := db.Model(&models.Transaction{}).
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Joins(`
			LEFT JOIN users
			ON transactions.actor_id = users.id
			AND transactions.actor_type = 'user'
		`).
		Joins(`
			LEFT JOIN guest_users
			ON transactions.actor_id = guest_users.id
			AND transactions.actor_type = 'guest'
		`)

	if search != "" {
		searchTerm := "%" + search + "%"

		countQuery = countQuery.Where(`
			transactions.id::text ILIKE ? OR
			transactions.provider_charge_id ILIKE ? OR
			events.title ILIKE ? OR
			CONCAT(COALESCE(users.first_name, ''), ' ', COALESCE(users.last_name, '')) ILIKE ? OR
			CONCAT(COALESCE(guest_users.first_name, ''), ' ', COALESCE(guest_users.last_name, '')) ILIKE ? OR
			COALESCE(users.email, guest_users.email) ILIKE ?
		`,
			searchTerm,
			searchTerm,
			searchTerm,
			searchTerm,
			searchTerm,
			searchTerm,
		)
	}

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

	if actorID != "" {
		if _, err := uuid.Parse(actorID); err == nil {
			countQuery = countQuery.Where("transactions.actor_id = ?", actorID)
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

	// Total count
	var totalCount int64

	if err := countQuery.Count(&totalCount).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Sorting
	var orderClause string

	switch sortBy {
	case "event_title":
		orderClause = fmt.Sprintf("LOWER(events.title) %s NULLS LAST", sortOrder)

	case "user_name":
		orderClause = fmt.Sprintf(`
			LOWER(
				COALESCE(
					NULLIF(TRIM(CONCAT(users.first_name, ' ', users.last_name)), ''),
					NULLIF(TRIM(CONCAT(guest_users.first_name, ' ', guest_users.last_name)), ''),
					''
				)
			) %s
		`, sortOrder)

	default:
		orderClause = utils.GenerateOrderByClause(sortBy, sortOrder)
	}

	// Pagination
	query = query.
		Order(orderClause).
		Limit(limit).
		Offset(offset)

	// Execute
	var rows []models.TransactionScanRow

	if err := query.Scan(&rows).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	// Response mapping
	summaries := make([]models.TransactionSummaryResponse, 0, len(rows))

	for _, r := range rows {
		summaries = append(summaries, r.ToSummaryResponse())
	}

	response := map[string]interface{}{
		"transactions": summaries,
		"pagination":   utils.BuildPaginationInfo(totalCount, page, limit),
	}

	utils.SuccessResponse(
		c,
		http.StatusOK,
		"Transactions retrieved successfully",
		response,
	)
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
// @Param search query string false "Search by event title (partial match, case-insensitive)"
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
	search := c.Query("search")
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
		Search:        search,
		DateFrom:      dateFromParsed,
		DateTo:        dateToParsed,
	}

	// Get user transactions with filters
	transactions, total, err := fh.transactionService.GetUserTransactions(userID, pagination.Page, pagination.Limit, filters)
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

// GetTransactionByID returns details of a specific transaction for admin
// @Summary Get transaction by ID
// @Description Get detailed information about a specific transaction (including soft-deleted ones for admin)
// @Tags Financial
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param transaction_id path string true "Transaction ID"
// @Success 200 {object} utils.Response{data=models.UserTransactionDetailResponse}
// @Failure 400 {object} utils.Response
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

	var row models.TransactionScanRow

	err := database.GetDB().
		Unscoped().
		Model(&models.Transaction{}).
		Select(`
			transactions.id,
			transactions.event_id,
			COALESCE(events.title, '') as event_title,

			transactions.actor_id,
			transactions.actor_type,

			CASE
				WHEN transactions.actor_type = 'user'
				THEN NULLIF(TRIM(CONCAT(users.first_name, ' ', users.last_name)), '')
				ELSE NULL
			END as user_name,

			CASE
				WHEN transactions.actor_type = 'user'
				THEN users.email
				ELSE NULL
			END as user_email,

			CASE
				WHEN transactions.actor_type = 'guest'
				THEN NULLIF(TRIM(CONCAT(guest_users.first_name, ' ', guest_users.last_name)), '')
				ELSE NULL
			END as guest_user_name,

			CASE
				WHEN transactions.actor_type = 'guest'
				THEN guest_users.email
				ELSE NULL
			END as guest_user_email,

			transactions.quantity as quantity,

			transactions.payment_gateway,
			transactions.amount_total,
			transactions.currency,

			events.commission_rate,


			transactions.status,
			transactions.provider_charge_id,

			transactions.platform_fee,
			transactions.gateway_fee,
			transactions.organizer_earning,

			transactions.created_at,
			transactions.updated_at,

			CASE
				WHEN transactions.provider_charge_id IS NOT NULL
				AND transactions.provider_charge_id != ''
				THEN true ELSE false
			END as has_payment_details
		`).
		Joins("LEFT JOIN events ON transactions.event_id = events.id").
		Joins("LEFT JOIN users ON transactions.actor_id = users.id AND transactions.actor_type = 'user'").
		Joins("LEFT JOIN guest_users ON transactions.actor_id = guest_users.id AND transactions.actor_type = 'guest'").
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

	utils.SuccessResponse(
		c,
		http.StatusOK,
		"Transaction retrieved successfully",
		row.ToSummaryResponse(),
	)
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
	parsedTransactionID, err := uuid.Parse(transactionID)
	if err != nil {
		utils.BadRequestErrorResponse(c, "Invalid transaction ID format", nil)
		return
	}

	// Fetch transaction with related data - use actor_id and actor_type instead of user_id/guest_user_id
	var transaction models.Transaction
	err = database.GetDB().Preload("Event").Preload("User").Preload("GuestUser").
		Where("id = ? AND actor_id = ? AND actor_type = ?", parsedTransactionID, userUUID, models.ActorUser).
		First(&transaction).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.NotFoundErrorResponse(c, "Transaction not found or access denied", nil)
			return
		}
		utils.HandleError(c, err)
		return
	}

	// Build event info
	eventInfo := models.UserTransactionEventInfo{
		ID:          transaction.EventID,
		Title:       transaction.Event.Title,
		BannerImage: transaction.Event.BannerImage,
	}

	// Build user info
	var userInfo models.UserTransactionUserDetailInfo
	if transaction.ActorType == models.ActorUser {
		userInfo.ID = &transaction.ActorID
		userInfo.Name = transaction.User.FirstName + " " + transaction.User.LastName
		userInfo.Email = transaction.User.Email
	} else if transaction.ActorType == models.ActorGuest {
		userInfo.ID = &transaction.ActorID
		userInfo.Name = transaction.GuestUser.FirstName + " " + transaction.GuestUser.LastName
		userInfo.Email = transaction.GuestUser.Email
	}

	// Fetch organizer info
	var organizerInfo models.UserTransactionOrganizerDetailInfo
	if transaction.Event.OrganizerID != uuid.Nil {
		var organizer models.User
		err = database.GetDB().Preload("OrganizerOnboarding").First(&organizer, transaction.Event.OrganizerID).Error
		if err == nil {
			organizerInfo.ID = organizer.ID
			if organizer.OrganizerOnboarding != nil && organizer.OrganizerOnboarding.BusinessName != "" {
				organizerInfo.Name = organizer.OrganizerOnboarding.BusinessName
				organizerInfo.Logo = organizer.OrganizerOnboarding.BusinessLogoURL
			} else {
				organizerInfo.Name = organizer.FirstName + " " + organizer.LastName
			}
		}
	}

	// Fetch company info from database
	var companyDetailInfo models.UserTransactionCompanyDetailInfo
	var company models.CompanyInfo
	err = database.GetDB().First(&company).Error
	if err != nil {
		// Fallback to hardcoded values if database query fails
		companyDetailInfo = models.UserTransactionCompanyDetailInfo{
			ID:        uuid.New(),
			Name:      "Event Ticketing Platform",
			Email:     "support@timroticket.com",
			Phone:     "+977-1234567890",
			Address:   "Kathmandu, Nepal",
			TaxNumber: "123456789",
			Logo:      "",
		}
	} else {
		companyDetailInfo = models.UserTransactionCompanyDetailInfo{
			ID:        company.ID,
			Name:      company.Name,
			Email:     company.Email,
			Phone:     company.Phone,
			Address:   company.Address,
			TaxNumber: "", // TaxNumber not in CompanyInfo model, can be added later if needed
			Logo:      company.LogoURL,
		}
	}

	// Fetch ticket breakdown for invoice items
	var ticketItems []struct {
		TierID     uuid.UUID `json:"tier_id"`
		TierName   string    `json:"tier_name"`
		Quantity   int       `json:"quantity"`
		UnitPrice  float64   `json:"unit_price"`
		TotalPrice float64   `json:"total_price"`
	}

	err = database.GetDB().Table("tickets").
		Select(`
			event_tiers.id as tier_id,
			event_tiers.tier_name,
			COUNT(*) as quantity,
			event_tiers.price as unit_price,
			COUNT(*) * event_tiers.price as total_price
		`).
		Joins("INNER JOIN event_tiers ON tickets.tier_id = event_tiers.id").
		Where("tickets.transaction_id = ? AND tickets.status != 'cancelled'", parsedTransactionID).
		Group("event_tiers.id, event_tiers.tier_name, event_tiers.price").
		Scan(&ticketItems).Error

	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Create invoice items
	var invoiceItems []models.UserTransactionInvoiceItemDetail
	for _, item := range ticketItems {
		invoiceItems = append(invoiceItems, models.UserTransactionInvoiceItemDetail{
			ID:         item.TierID,
			Name:       item.TierName,
			Quantity:   item.Quantity,
			UnitPrice:  item.UnitPrice,
			TotalPrice: item.TotalPrice,
		})
	}
	amount, _ := currency.FromSmallestUnit(transaction.AmountTotal, transaction.Currency)

	// Create invoice info
	invoiceInfo := models.UserTransactionInvoiceDetailInfo{
		Organizer:     organizerInfo,
		Company:       companyDetailInfo,
		InvoiceNumber: "INV-" + transaction.ID.String()[:8],
		Total:         amount,
		Subtotal:      amount, // For now, no commission calculation in user view
		Tax:           0,      // No tax calculation for now
		Discount:      0,      // No discount calculation for now
		Items:         invoiceItems,
	}

	// Create response
	response := models.UserTransactionDetailResponse{
		ID:             transaction.ID,
		Event:          eventInfo,
		User:           userInfo,
		TicketCount:    transaction.Quantity,
		PaymentGateway: string(transaction.PaymentGateway),
		Amount:         amount,
		Currency:       transaction.Currency,
		Status:         string(transaction.Status),
		CreatedAt:      transaction.CreatedAt,
		UpdatedAt:      transaction.UpdatedAt,
		InvoiceInfo:    &invoiceInfo,
	}

	utils.SuccessResponse(c, http.StatusOK, "Transaction retrieved successfully", response)
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
		Where("idempotency_key = ?", paymentIntent.IdempotencyKey).
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
// @Param sort_by query string false "Sort by field (created_at, action, entity_type, actor_type)" default(created_at)
// @Param sort_order query string false "Sort order (asc, desc)" default(desc)
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

	// Set default sorting if not provided
	if req.SortBy == "" {
		req.SortBy = "created_at"
	}
	if req.SortOrder == "" {
		req.SortOrder = "desc"
	}

	// Validate sort parameters using centralized utility
	req.SortBy, req.SortOrder = utils.ValidateSortForAuditLogs(req.SortBy, req.SortOrder)

	// Get audit logs
	response, err := fh.financialService.GetAuditLogs(req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, "Audit logs retrieved successfully", response)
}

// GetTransactionPaymentDetails returns payment details for a specific transaction
// @Summary Get payment details for transaction (Admin)
// @Description Get detailed payment information for a specific transaction by transaction ID
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

	var transaction models.Transaction
	if err := database.GetDB().
		Preload("Event").
		Preload("User").
		Preload("GuestUser").
		First(&transaction, transactionID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.HandleError(c, utils.NewNotFoundError("Transaction not found"))
			return
		}
		utils.HandleError(c, err)
		return
	}

	var paymentIntent models.PaymentIntent
	if err := database.GetDB().First(&paymentIntent, transaction.PaymentIntentID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.HandleError(c, utils.NewNotFoundError("Payment intent not found for this transaction"))
			return
		}
		utils.HandleError(c, err)
		return
	}

	var tickets []models.Ticket
	if err := database.GetDB().
		Preload("Tier").
		Preload("User").
		Preload("GuestUser").
		Where("transaction_id = ?", transaction.ID).
		Find(&tickets).Error; err != nil {
		utils.HandleError(c, err)
		return
	}

	userSummary := models.TransactionPaymentDetailsUserSummary{}
	if transaction.ActorType == models.ActorUser && transaction.User != nil {
		userSummary = models.TransactionPaymentDetailsUserSummary{
			ID:    transaction.User.ID,
			Name:  strings.TrimSpace(transaction.User.FirstName + " " + transaction.User.LastName),
			Email: transaction.User.Email,
			Phone: transaction.User.Phone,
		}
	} else if transaction.GuestUser != nil {
		userSummary = models.TransactionPaymentDetailsUserSummary{
			ID:    transaction.GuestUser.ID,
			Name:  strings.TrimSpace(transaction.GuestUser.FirstName + " " + transaction.GuestUser.LastName),
			Email: transaction.GuestUser.Email,
			Phone: transaction.GuestUser.Phone,
		}
	}

	eventSummary := models.TransactionPaymentDetailsEventSummary{}
	if transaction.Event != nil {
		eventSummary = models.TransactionPaymentDetailsEventSummary{
			ID:     transaction.Event.ID,
			Name:   transaction.Event.Title,
			Banner: transaction.Event.BannerImage,
		}
	}

	ticketSummaries := make([]models.TransactionPaymentDetailsTicketSummary, 0, len(tickets))
	for _, ticket := range tickets {
		ticketUser := models.TransactionPaymentDetailsTicketUserSummary{}
		if ticket.ActorType == models.ActorUser && ticket.User != nil {
			ticketUser = models.TransactionPaymentDetailsTicketUserSummary{
				ID:    ticket.User.ID,
				Name:  strings.TrimSpace(ticket.User.FirstName + " " + ticket.User.LastName),
				Email: ticket.User.Email,
			}
		} else if ticket.GuestUser != nil {
			ticketUser = models.TransactionPaymentDetailsTicketUserSummary{
				ID:    ticket.GuestUser.ID,
				Name:  strings.TrimSpace(ticket.GuestUser.FirstName + " " + ticket.GuestUser.LastName),
				Email: ticket.GuestUser.Email,
			}
		}

		ticketTier := models.TransactionPaymentDetailsTicketTierSummary{}
		if ticket.Tier != nil {
			ticketTier = models.TransactionPaymentDetailsTicketTierSummary{
				ID:       ticket.Tier.ID,
				TierName: ticket.Tier.TierName,
			}
		}
		ticketSummaries = append(ticketSummaries, models.TransactionPaymentDetailsTicketSummary{
			ID:              ticket.ID,
			TicketNumber:    ticket.TicketNumber,
			User:            ticketUser,
			Tier:            ticketTier,
			IsGuestPurchase: ticket.ActorType == models.ActorGuest,
			TotalAmount:     ticket.Tier.Price,
			Status:          string(ticket.Status),
			CreatedAt:       ticket.CreatedAt,
			UpdatedAt:       ticket.UpdatedAt,
		})
	}

	paymentMethod := ""
	var paymentAttempt models.PaymentAttempt
	if err := database.GetDB().
		Where("payment_intent_id = ?", paymentIntent.ID).
		Order("created_at DESC").
		First(&paymentAttempt).Error; err == nil {
		if v, ok := paymentAttempt.ProviderData["payment_method"].(string); ok {
			paymentMethod = v
		} else if v, ok := paymentAttempt.ProviderData["payment_method_type"].(string); ok {
			paymentMethod = v
		}
	}
	amount, _ := currency.FromSmallestUnit(transaction.AmountTotal, transaction.Currency)
	response := models.TransactionPaymentDetailsResponse{
		Transaction: models.TransactionPaymentDetailsTransactionSummary{
			ID:             transaction.ID,
			Event:          eventSummary,
			User:           userSummary,
			PaymentGateway: string(transaction.PaymentGateway),
			Amount:         amount,
			Currency:       transaction.Currency,
			Quantity:       transaction.Quantity,
			Status:         string(transaction.Status),
			CreatedAt:      transaction.CreatedAt,
			UpdatedAt:      transaction.UpdatedAt,
		},
		PaymentIntent: &models.TransactionPaymentIntentSummary{
			ID:             paymentIntent.ID,
			Status:         string(paymentIntent.Status),
			PaymentGateway: string(paymentIntent.PaymentGateway),
			PaymentMethod:  paymentMethod,
			CustomerEmail:  paymentIntent.CustomerEmail,
			CreatedAt:      paymentIntent.CreatedAt,
		},
		Tickets: ticketSummaries,
	}

	utils.SuccessResponse(c, http.StatusOK, "Transaction payment details retrieved successfully", response)
}
