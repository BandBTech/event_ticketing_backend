package handlers

import (
	"fmt"
	"net/http"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ReportHandler struct {
}

func NewReportHandler() *ReportHandler {
	return &ReportHandler{}
}

// GetAdminReport godoc
// @Summary Get admin report by type
// @Description Get various report types (overview, sales, customer-analytics, financial, event-performance)
// @Tags Reports
// @Security ApiKeyAuth
// @Param type query string true "Report type: overview, sales, customer-analytics, financial, event-performance"
// @Param start_date query string false "Start date (YYYY-MM-DD)"
// @Param end_date query string false "End date (YYYY-MM-DD)"
// @Param organizer_id query string false "Filter by organizer ID"
// @Param event_id query string false "Event ID (required for event-performance)"
// @Param limit query int false "Number of items to return (default 5-10)"
// @Produce json
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/reports [get]
func (h *ReportHandler) GetAdminReport(c *gin.Context) {
	reportType := c.DefaultQuery("type", "overview")
	startDate, endDate := parseDateRange(c)

	var organizerID *uuid.UUID
	if orgIDStr := c.Query("organizer_id"); orgIDStr != "" {
		id, err := uuid.Parse(orgIDStr)
		if err != nil {
			utils.ErrorResponse(c, http.StatusBadRequest, "Invalid organizer_id", nil)
			return
		}
		organizerID = &id
	}

	var result interface{}
	var msg string

	switch reportType {
	case "overview":
		result = &models.AdminOverviewReport{
			SummaryMetrics:      h.getSummaryMetrics(startDate, endDate),
			TopPerformingEvents: h.getTopPerformingEvents(startDate, endDate, 5, organizerID),
			RecentTransactions:  h.getRecentTransactions(startDate, endDate, 5, organizerID),
			RevenueTrend:        h.getRevenueTrend(startDate, endDate, organizerID),
			TicketSalesTrend:    h.getTicketSalesTrend(startDate, endDate, organizerID),
			EventsStatistics:    h.getEventStatistics(startDate, endDate, organizerID),
		}
		msg = "Admin overview report retrieved successfully"

	case "sales":
		result = &models.SalesReportData{
			SummaryMetrics:        h.getSummaryMetrics(startDate, endDate),
			DailySales:            h.getDailySales(startDate, endDate, organizerID),
			TopProductEvents:      h.getTopPerformingEvents(startDate, endDate, 5, organizerID),
			SalesByPaymentGateway: h.getSalesByPaymentGateway(startDate, endDate, organizerID),
		}
		msg = "Sales report retrieved successfully"

	case "customer-analytics":
		result = &models.CustomerAnalyticsReport{
			TotalCustomers:    h.getTotalCustomers(startDate, endDate),
			RegisteredUsers:   h.getRegisteredUsers(startDate, endDate),
			GuestPurchases:    h.getGuestPurchases(startDate, endDate),
			RepeatCustomers:   h.getRepeatCustomers(startDate, endDate),
			AverageOrderValue: h.getAverageOrderValue(startDate, endDate),
			CustomerSegments:  h.getCustomerSegments(startDate, endDate),
			TopCustomers:      h.getTopCustomers(startDate, endDate, 10),
			CustomerRetention: h.getCustomerRetention(startDate, endDate),
		}
		msg = "Customer analytics report retrieved successfully"

	case "financial":
		result = &models.FinancialReport{
			SummaryMetrics:    h.getFinancialSummary(startDate, endDate, organizerID),
			RevenueBreakdown:  h.getRevenueBreakdown(startDate, endDate, organizerID),
			CommissionHistory: h.getCommissionHistory(startDate, endDate, organizerID),
			PayoutHistory:     h.getPayoutHistory(startDate, endDate, organizerID),
			CurrencyBreakdown: h.getCurrencyBreakdown(startDate, endDate, organizerID),
		}
		msg = "Financial report retrieved successfully"

	case "event-performance":
		eventIDStr := c.Query("event_id")
		if eventIDStr == "" {
			utils.ErrorResponse(c, http.StatusBadRequest, "event_id is required for event-performance report", nil)
			return
		}

		eventID, err := uuid.Parse(eventIDStr)
		if err != nil {
			utils.ErrorResponse(c, http.StatusBadRequest, "Invalid event_id", nil)
			return
		}

		report := h.getEventPerformanceReportData(eventID)
		if report == nil {
			utils.ErrorResponse(c, http.StatusNotFound, "Event not found", nil)
			return
		}

		result = report
		msg = "Event performance report retrieved successfully"

	default:
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid report type. Allowed: overview, sales, customer-analytics, financial, event-performance", nil)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, msg, result)
}

// GetOrganizerReport godoc
// @Summary Get organizer report by type
// @Description Get various report types for organizer (overview, sales, customer-analytics, financial, event-performance)
// @Tags Reports
// @Security ApiKeyAuth
// @Param type query string true "Report type: overview, sales, customer-analytics, financial, event-performance"
// @Param start_date query string false "Start date (YYYY-MM-DD)"
// @Param end_date query string false "End date (YYYY-MM-DD)"
// @Param event_id query string false "Event ID (required for event-performance)"
// @Param limit query int false "Number of items to return (default 5-10)"
// @Produce json
// @Success 200 {object} utils.Response
// @Failure 400 {object} utils.Response
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/reports [get]
func (h *ReportHandler) GetOrganizerReport(c *gin.Context) {
	userIDInterface, exists := c.Get("userID")
	if !exists {
		utils.ErrorResponse(c, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	userUUID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid user ID format", nil)
		return
	}

	organizerID, err := utils.GetOrganizerIDForUser(database.GetDB(), userUUID)
	if err != nil {
		utils.ErrorResponse(c, http.StatusForbidden, "User is not an organizer", nil)
		return
	}

	reportType := c.DefaultQuery("type", "overview")
	startDate, endDate := parseDateRange(c)

	var result interface{}
	var msg string

	switch reportType {
	case "overview":
		result = &models.OrganizerOverviewReport{
			SummaryMetrics:      h.getOrganizerSummaryMetrics(startDate, endDate, organizerID),
			TopPerformingEvents: h.getTopPerformingEvents(startDate, endDate, 5, &organizerID),
			RecentTransactions:  h.getRecentTransactions(startDate, endDate, 5, &organizerID),
			RevenueTrend:        h.getRevenueTrend(startDate, endDate, &organizerID),
			TicketSalesTrend:    h.getTicketSalesTrend(startDate, endDate, &organizerID),
			EventsStatistics:    h.getOrganizerEventStatistics(startDate, endDate, organizerID),
		}
		msg = "Organizer overview report retrieved successfully"

	case "sales":
		result = &models.SalesReportData{
			SummaryMetrics:        h.getSummaryMetrics(startDate, endDate),
			DailySales:            h.getDailySales(startDate, endDate, &organizerID),
			TopProductEvents:      h.getTopPerformingEvents(startDate, endDate, 5, &organizerID),
			SalesByPaymentGateway: h.getSalesByPaymentGateway(startDate, endDate, &organizerID),
		}
		msg = "Sales report retrieved successfully"

	case "customer-analytics":
		result = &models.CustomerAnalyticsReport{
			TotalCustomers:    h.getTotalCustomers(startDate, endDate),
			RegisteredUsers:   h.getRegisteredUsers(startDate, endDate),
			GuestPurchases:    h.getGuestPurchases(startDate, endDate),
			RepeatCustomers:   h.getRepeatCustomers(startDate, endDate),
			AverageOrderValue: h.getAverageOrderValue(startDate, endDate),
			CustomerSegments:  h.getCustomerSegments(startDate, endDate),
			TopCustomers:      h.getTopCustomers(startDate, endDate, 10),
			CustomerRetention: h.getCustomerRetention(startDate, endDate),
		}
		msg = "Customer analytics report retrieved successfully"

	case "financial":
		result = &models.FinancialReport{
			SummaryMetrics:    h.getFinancialSummary(startDate, endDate, &organizerID),
			RevenueBreakdown:  h.getRevenueBreakdown(startDate, endDate, &organizerID),
			CommissionHistory: h.getCommissionHistory(startDate, endDate, &organizerID),
			PayoutHistory:     h.getPayoutHistory(startDate, endDate, &organizerID),
			CurrencyBreakdown: h.getCurrencyBreakdown(startDate, endDate, &organizerID),
		}
		msg = "Financial report retrieved successfully"

	case "event-performance":
		eventIDStr := c.Query("event_id")
		if eventIDStr == "" {
			utils.ErrorResponse(c, http.StatusBadRequest, "event_id is required for event-performance report", nil)
			return
		}

		eventID, err := uuid.Parse(eventIDStr)
		if err != nil {
			utils.ErrorResponse(c, http.StatusBadRequest, "Invalid event_id", nil)
			return
		}

		report := h.getEventPerformanceReportData(eventID)
		if report == nil {
			utils.ErrorResponse(c, http.StatusNotFound, "Event not found", nil)
			return
		}

		result = report
		msg = "Event performance report retrieved successfully"

	case "dashboard":
		result = &models.OrganizerDashboardReport{
			SummaryMetrics:     h.getOrganizerDashboardMetrics(startDate, endDate, organizerID),
			TicketsByEvent:     h.getTicketsByEvent(startDate, endDate, organizerID),
			RevenueOverTime:    h.getRevenueOverTime(startDate, endDate, organizerID),
			EventPerformance:   h.getEventPerformanceSummary(startDate, endDate, organizerID),
			AttendanceMetrics:  h.getAttendanceMetrics(startDate, endDate, organizerID),
			PaymentMethodStats: h.getPaymentMethodStats(startDate, endDate, organizerID),
			TopEventTiers:      h.getTopEventTiers(startDate, endDate, organizerID),
			UpcomingEvents:     h.getUpcomingEvents(organizerID),
		}
		msg = "Organizer dashboard report retrieved successfully"

	default:
		utils.ErrorResponse(c, http.StatusBadRequest, "Invalid report type. Allowed: overview, sales, customer-analytics, financial, event-performance, dashboard", nil)
		return
	}

	utils.SuccessResponse(c, http.StatusOK, msg, result)
}

// ====================== Helper Functions ======================

func parseDateRange(c *gin.Context) (time.Time, time.Time) {
	now := time.Now()
	startDate := now.AddDate(0, -1, 0) // Default: 1 month ago
	endDate := now

	if startStr := c.Query("start_date"); startStr != "" {
		if parsed, err := time.Parse("2006-01-02", startStr); err == nil {
			startDate = parsed
		}
	}

	if endStr := c.Query("end_date"); endStr != "" {
		if parsed, err := time.Parse("2006-01-02", endStr); err == nil {
			endDate = parsed
		}
	}

	return startDate, endDate
}

func (h *ReportHandler) getSummaryMetrics(startDate, endDate time.Time) models.SummaryMetrics {
	var metrics struct {
		TotalRevenue          float64
		TotalCommission       float64
		OrganizerShare        float64
		TotalTicketsSold      int64
		ActiveEvents          int64
		TotalEvents           int64
		TotalTransactions     int64
		CompletedTransactions int64
		PendingTransactions   int64
		FailedTransactions    int64
	}

	database.GetDB().Raw(`
		SELECT
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as total_revenue,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.commission_amount ELSE 0 END), 0) as total_commission,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.organizer_share ELSE 0 END), 0) as organizer_share,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.quantity ELSE 0 END), 0) as total_tickets_sold,
			COUNT(DISTINCT CASE WHEN e.status IN ('on_sale', 'live') AND e.deleted_at IS NULL THEN e.id END) as active_events,
			COUNT(DISTINCT CASE WHEN e.deleted_at IS NULL THEN e.id END) as total_events,
			COUNT(DISTINCT t.id) as total_transactions,
			COUNT(DISTINCT CASE WHEN t.status = 'completed' THEN t.id END) as completed_transactions,
			COUNT(DISTINCT CASE WHEN t.status = 'pending' THEN t.id END) as pending_transactions,
			COUNT(DISTINCT CASE WHEN t.status = 'failed' THEN t.id END) as failed_transactions
		FROM transactions t
		LEFT JOIN events e ON t.event_id = e.id
		WHERE t.created_at BETWEEN ? AND ?
	`, startDate, endDate.AddDate(0, 0, 1)).Scan(&metrics)

	avgOrderValue := 0.0
	conversionRate := 0.0

	if metrics.TotalTransactions > 0 {
		avgOrderValue = metrics.TotalRevenue / float64(metrics.TotalTransactions)
	}

	if metrics.ActiveEvents > 0 && metrics.TotalEvents > 0 {
		conversionRate = (float64(metrics.TotalTicketsSold) / float64(metrics.TotalEvents)) * 100
	}

	return models.SummaryMetrics{
		TotalRevenue:          metrics.TotalRevenue,
		TotalCommission:       metrics.TotalCommission,
		OrganizerShare:        metrics.OrganizerShare,
		TotalTicketsSold:      metrics.TotalTicketsSold,
		ActiveEvents:          metrics.ActiveEvents,
		TotalEvents:           metrics.TotalEvents,
		TotalTransactions:     metrics.TotalTransactions,
		CompletedTransactions: metrics.CompletedTransactions,
		PendingTransactions:   metrics.PendingTransactions,
		FailedTransactions:    metrics.FailedTransactions,
		AverageOrderValue:     avgOrderValue,
		ConversionRate:        conversionRate,
	}
}

func (h *ReportHandler) getOrganizerSummaryMetrics(startDate, endDate time.Time, organizerID uuid.UUID) models.OrganizerSummaryMetrics {
	var metrics struct {
		TotalRevenue           float64
		TotalEarnings          float64
		TotalRefunds           float64
		NetRevenue             float64
		NetEarnings            float64
		TotalTicketsSold       int64
		ActiveEvents           int64
		TotalEvents            int64
		TotalTransactions      int64
		CompletedTransactions  int64
		PendingPayouts         float64
		AverageTicketsPerEvent float64
	}

	database.GetDB().Raw(`
		WITH transaction_metrics AS (
			SELECT
				COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as total_revenue,
				COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.organizer_share ELSE 0 END), 0) as total_earnings,
				COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.quantity ELSE 0 END), 0) as total_tickets_sold,
				COUNT(DISTINCT t.id) as total_transactions,
				COUNT(DISTINCT CASE WHEN t.status = 'completed' THEN t.id END) as completed_transactions
			FROM transactions t
			LEFT JOIN events e ON t.event_id = e.id
			WHERE e.organizer_id = ? AND t.created_at BETWEEN ? AND ?
		),
		refund_metrics AS (
			SELECT
				COALESCE(SUM(CASE WHEN r.status = 'completed' THEN r.amount ELSE 0 END), 0) as total_refunds,
				COALESCE(SUM(CASE WHEN r.status = 'completed' THEN r.organizer_refund ELSE 0 END), 0) as total_earnings_refunds
			FROM refunds r
			LEFT JOIN transactions t ON r.transaction_id = t.id
			LEFT JOIN events e ON t.event_id = e.id
			WHERE e.organizer_id = ? AND r.created_at BETWEEN ? AND ?
		),
		event_metrics AS (
			SELECT
				COUNT(DISTINCT CASE WHEN e.status IN ('on_sale', 'live') AND e.deleted_at IS NULL THEN e.id END) as active_events,
				COUNT(DISTINCT CASE WHEN e.deleted_at IS NULL THEN e.id END) as total_events
			FROM events e
			WHERE e.organizer_id = ?
		)
		SELECT
			tm.total_revenue,
			tm.total_earnings,
			rm.total_refunds,
			(tm.total_revenue - rm.total_refunds) as net_revenue,
			(tm.total_earnings - rm.total_earnings_refunds) as net_earnings,
			tm.total_tickets_sold,
			em.active_events,
			em.total_events,
			tm.total_transactions,
			tm.completed_transactions
		FROM transaction_metrics tm
		CROSS JOIN refund_metrics rm
		CROSS JOIN event_metrics em
	`, organizerID, startDate, endDate.AddDate(0, 0, 1), organizerID, startDate, endDate.AddDate(0, 0, 1), organizerID).Scan(&metrics)

	// Get pending payouts
	var pendingPayouts float64
	database.GetDB().Raw(`
		SELECT COALESCE(SUM(billed_amount), 0)
		FROM payment_bills
		WHERE status = 'pending' AND organizer_id = ?
	`, organizerID).Scan(&pendingPayouts)

	conversionRate := 0.0
	avgTicketsPerEvent := 0.0

	if metrics.TotalEvents > 0 {
		avgTicketsPerEvent = float64(metrics.TotalTicketsSold) / float64(metrics.TotalEvents)
		conversionRate = (float64(metrics.TotalTicketsSold) / float64(metrics.TotalEvents)) * 100
	}

	return models.OrganizerSummaryMetrics{
		TotalRevenue:           metrics.TotalRevenue,
		TotalEarnings:          metrics.TotalEarnings,
		TotalRefunds:           metrics.TotalRefunds,
		NetRevenue:             metrics.NetRevenue,
		NetEarnings:            metrics.NetEarnings,
		TotalTicketsSold:       metrics.TotalTicketsSold,
		ActiveEvents:           metrics.ActiveEvents,
		TotalEvents:            metrics.TotalEvents,
		TotalTransactions:      metrics.TotalTransactions,
		CompletedTransactions:  metrics.CompletedTransactions,
		PendingPayouts:         pendingPayouts,
		AverageTicketsPerEvent: avgTicketsPerEvent,
		ConversionRate:         conversionRate,
	}
}

func (h *ReportHandler) getTopPerformingEvents(startDate, endDate time.Time, limit int, organizerID *uuid.UUID) []models.TopPerformingEvent {
	var events []models.TopPerformingEvent

	query := `
		SELECT
			e.id as event_id,
			e.title as event_title,
			e.banner_image,
			e.organizer_id,
			u.full_name as organizer_name,
			COALESCE(co.business_name, u.full_name) as organizer_business_name,
			COALESCE(co.business_logo_url, u.profile_picture_url, '') as organizer_logo,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.quantity ELSE 0 END), 0) as tickets_sold,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as revenue,
			CASE 
				WHEN e.capacity > 0 THEN (COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.quantity ELSE 0 END), 0)::float / e.capacity * 100)
				ELSE 0
			END as conversion,
			e.status,
			e.start_date
		FROM events e
		LEFT JOIN users u ON e.organizer_id = u.id
		LEFT JOIN organizer_onboardings co ON e.organizer_id = co.organizer_id
		LEFT JOIN transactions t ON e.id = t.event_id AND t.created_at BETWEEN ? AND ?
		WHERE e.deleted_at IS NULL
	`

	params := []interface{}{startDate, endDate.AddDate(0, 0, 1)}

	if organizerID != nil {
		query += ` AND e.organizer_id = ?`
		params = append(params, organizerID)
	}

	query += `
		GROUP BY e.id, u.id, co.id
		ORDER BY tickets_sold DESC
		LIMIT ?
	`
	params = append(params, limit)

	database.GetDB().Raw(query, params...).Scan(&events)

	// Add rank
	for i := range events {
		events[i].Rank = i + 1
	}

	return events
}

func (h *ReportHandler) getRecentTransactions(startDate, endDate time.Time, limit int, organizerID *uuid.UUID) []models.RecentTransactionRecord {
	var transactions []models.RecentTransactionRecord

	query := `
		SELECT
			t.id as transaction_id,
			t.event_id,
			e.title as event_title,
			COALESCE(u.full_name, gu.name, 'Guest') as customer_name,
			COALESCE(u.email, gu.email, '') as customer_email,
			t.amount,
			t.quantity as ticket_quantity,
			t.payment_gateway,
			t.status,
			t.created_at
		FROM transactions t
		LEFT JOIN events e ON t.event_id = e.id
		LEFT JOIN users u ON t.user_id = u.id
		LEFT JOIN guest_users gu ON t.guest_user_id = gu.id
		WHERE t.created_at BETWEEN ? AND ? AND t.deleted_at IS NULL
	`

	params := []interface{}{startDate, endDate.AddDate(0, 0, 1)}

	if organizerID != nil {
		query += ` AND e.organizer_id = ?`
		params = append(params, organizerID)
	}

	query += `
		ORDER BY t.created_at DESC
		LIMIT ?
	`
	params = append(params, limit)

	database.GetDB().Raw(query, params...).Scan(&transactions)
	return transactions
}

func (h *ReportHandler) getRevenueTrend(startDate, endDate time.Time, organizerID *uuid.UUID) []models.MonthlyTrendData {
	var trends []models.MonthlyTrendData

	query := `
		SELECT
			TO_CHAR(DATE_TRUNC('month', t.created_at), 'Mon') as month,
			EXTRACT(YEAR FROM t.created_at)::int as year,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as revenue
		FROM transactions t
		LEFT JOIN events e ON t.event_id = e.id
		WHERE t.created_at BETWEEN ? AND ?
	`

	params := []interface{}{startDate, endDate.AddDate(0, 0, 1)}

	if organizerID != nil {
		query += ` AND e.organizer_id = ?`
		params = append(params, organizerID)
	}

	query += `
		GROUP BY DATE_TRUNC('month', t.created_at)
		ORDER BY DATE_TRUNC('month', t.created_at) ASC
	`

	database.GetDB().Raw(query, params...).Scan(&trends)
	return trends
}

func (h *ReportHandler) getTicketSalesTrend(startDate, endDate time.Time, organizerID *uuid.UUID) []models.MonthlyTicketSaleData {
	var trends []models.MonthlyTicketSaleData

	query := `
		SELECT
			TO_CHAR(DATE_TRUNC('month', t.created_at), 'Mon') as month,
			EXTRACT(YEAR FROM t.created_at)::int as year,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.quantity ELSE 0 END), 0) as tickets_sold
		FROM transactions t
		LEFT JOIN events e ON t.event_id = e.id
		WHERE t.created_at BETWEEN ? AND ?
	`

	params := []interface{}{startDate, endDate.AddDate(0, 0, 1)}

	if organizerID != nil {
		query += ` AND e.organizer_id = ?`
		params = append(params, organizerID)
	}

	query += `
		GROUP BY DATE_TRUNC('month', t.created_at)
		ORDER BY DATE_TRUNC('month', t.created_at) ASC
	`

	database.GetDB().Raw(query, params...).Scan(&trends)

	for i := range trends {
		trends[i].DisplayLabel = fmt.Sprintf("%d %s", trends[i].TicketsSold, "tickets")
	}

	return trends
}

func (h *ReportHandler) getEventStatistics(startDate, endDate time.Time, organizerID *uuid.UUID) models.EventsStatistics {
	var stats struct {
		TotalEvents     int64
		DraftEvents     int64
		PendingEvents   int64
		ApprovedEvents  int64
		RejectedEvents  int64
		OnSaleEvents    int64
		LiveEvents      int64
		CompletedEvents int64
		CancelledEvents int64
	}

	query := `
		SELECT
			COUNT(*) FILTER (WHERE deleted_at IS NULL) as total_events,
			COUNT(*) FILTER (WHERE status = 'draft' AND deleted_at IS NULL) as draft_events,
			COUNT(*) FILTER (WHERE status = 'pending' AND deleted_at IS NULL) as pending_events,
			COUNT(*) FILTER (WHERE status = 'approved' AND deleted_at IS NULL) as approved_events,
			COUNT(*) FILTER (WHERE status = 'rejected' AND deleted_at IS NULL) as rejected_events,
			COUNT(*) FILTER (WHERE status = 'on_sale' AND deleted_at IS NULL) as on_sale_events,
			COUNT(*) FILTER (WHERE status = 'live' AND deleted_at IS NULL) as live_events,
			COUNT(*) FILTER (WHERE status = 'completed' AND deleted_at IS NULL) as completed_events,
			COUNT(*) FILTER (WHERE is_cancelled = true AND deleted_at IS NULL) as cancelled_events
		FROM events
	`

	params := []interface{}{}

	if organizerID != nil {
		query += ` WHERE organizer_id = ?`
		params = append(params, organizerID)
	}

	database.GetDB().Raw(query, params...).Scan(&stats)

	return models.EventsStatistics{
		TotalEvents:     stats.TotalEvents,
		DraftEvents:     stats.DraftEvents,
		PendingEvents:   stats.PendingEvents,
		ApprovedEvents:  stats.ApprovedEvents,
		RejectedEvents:  stats.RejectedEvents,
		OnSaleEvents:    stats.OnSaleEvents,
		LiveEvents:      stats.LiveEvents,
		CompletedEvents: stats.CompletedEvents,
		CancelledEvents: stats.CancelledEvents,
	}
}

func (h *ReportHandler) getOrganizerEventStatistics(startDate, endDate time.Time, organizerID uuid.UUID) models.OrganizerEventStatistics {
	var stats struct {
		TotalEvents     int64
		DraftEvents     int64
		PendingEvents   int64
		ApprovedEvents  int64
		RejectedEvents  int64
		OnSaleEvents    int64
		LiveEvents      int64
		CompletedEvents int64
		CancelledEvents int64
	}

	database.GetDB().Raw(`
		SELECT
			COUNT(*) FILTER (WHERE deleted_at IS NULL) as total_events,
			COUNT(*) FILTER (WHERE status = 'draft' AND deleted_at IS NULL) as draft_events,
			COUNT(*) FILTER (WHERE status = 'pending' AND deleted_at IS NULL) as pending_events,
			COUNT(*) FILTER (WHERE status = 'approved' AND deleted_at IS NULL) as approved_events,
			COUNT(*) FILTER (WHERE status = 'rejected' AND deleted_at IS NULL) as rejected_events,
			COUNT(*) FILTER (WHERE status = 'on_sale' AND deleted_at IS NULL) as on_sale_events,
			COUNT(*) FILTER (WHERE status = 'live' AND deleted_at IS NULL) as live_events,
			COUNT(*) FILTER (WHERE status = 'completed' AND deleted_at IS NULL) as completed_events,
			COUNT(*) FILTER (WHERE is_cancelled = true AND deleted_at IS NULL) as cancelled_events
		FROM events
		WHERE organizer_id = ?
	`, organizerID).Scan(&stats)

	return models.OrganizerEventStatistics{
		TotalEvents:     stats.TotalEvents,
		DraftEvents:     stats.DraftEvents,
		PendingEvents:   stats.PendingEvents,
		ApprovedEvents:  stats.ApprovedEvents,
		RejectedEvents:  stats.RejectedEvents,
		OnSaleEvents:    stats.OnSaleEvents,
		LiveEvents:      stats.LiveEvents,
		CompletedEvents: stats.CompletedEvents,
		CancelledEvents: stats.CancelledEvents,
	}
}

func (h *ReportHandler) getDailySales(startDate, endDate time.Time, organizerID *uuid.UUID) []models.DailySaleRecord {
	var records []models.DailySaleRecord

	query := `
		SELECT
			DATE(t.created_at) as date,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as revenue,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.quantity ELSE 0 END), 0) as tickets_sold,
			COUNT(DISTINCT t.id) as transactions,
			COALESCE(AVG(CASE WHEN t.status = 'completed' THEN t.amount ELSE NULL END), 0) as average_order_value
		FROM transactions t
		LEFT JOIN events e ON t.event_id = e.id
		WHERE t.created_at BETWEEN ? AND ?
	`

	params := []interface{}{startDate, endDate.AddDate(0, 0, 1)}

	if organizerID != nil {
		query += ` AND e.organizer_id = ?`
		params = append(params, organizerID)
	}

	query += `
		GROUP BY DATE(t.created_at)
		ORDER BY DATE(t.created_at) ASC
	`

	database.GetDB().Raw(query, params...).Scan(&records)
	return records
}

func (h *ReportHandler) getSalesByPaymentGateway(startDate, endDate time.Time, organizerID *uuid.UUID) []models.PaymentGatewayStats {
	var stats []struct {
		PaymentGateway    string
		TotalTransactions int64
		TotalRevenue      float64
	}

	query := `
		SELECT
			t.payment_gateway,
			COUNT(*) as total_transactions,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as total_revenue
		FROM transactions t
		LEFT JOIN events e ON t.event_id = e.id
		WHERE t.created_at BETWEEN ? AND ?
	`

	params := []interface{}{startDate, endDate.AddDate(0, 0, 1)}

	if organizerID != nil {
		query += ` AND e.organizer_id = ?`
		params = append(params, organizerID)
	}

	query += `
		GROUP BY t.payment_gateway
		ORDER BY total_revenue DESC
	`

	database.GetDB().Raw(query, params...).Scan(&stats)

	// Calculate total revenue
	var totalRevenue float64
	for _, s := range stats {
		totalRevenue += s.TotalRevenue
	}

	// Convert to response model
	results := make([]models.PaymentGatewayStats, len(stats))
	for i, s := range stats {
		percentage := 0.0
		if totalRevenue > 0 {
			percentage = (s.TotalRevenue / totalRevenue) * 100
		}

		results[i] = models.PaymentGatewayStats{
			GatewayName:       s.PaymentGateway,
			TotalTransactions: s.TotalTransactions,
			TotalRevenue:      s.TotalRevenue,
			PercentageOfTotal: percentage,
			Status:            "active",
		}
	}

	return results
}

func (h *ReportHandler) getEventPerformanceReportData(eventID uuid.UUID) *models.EventPerformanceReport {
	var event models.Event
	if err := database.GetDB().First(&event, "id = ?", eventID).Error; err != nil {
		return nil
	}

	var perfData struct {
		TicketsSold       int64
		Revenue           float64
		Commission        float64
		OrganizerEarnings float64
		Transactions      int64
	}

	database.GetDB().Raw(`
		SELECT
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.quantity ELSE 0 END), 0) as tickets_sold,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as revenue,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.commission_amount ELSE 0 END), 0) as commission,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.organizer_share ELSE 0 END), 0) -
			COALESCE(SUM(CASE WHEN r.status = 'completed' THEN r.organizer_refund ELSE 0 END), 0) as organizer_earnings,
			COUNT(*) as transactions
		FROM transactions t
		LEFT JOIN refunds r ON r.transaction_id = t.id
		WHERE t.event_id = ?
	`, eventID).Scan(&perfData)

	soldPercentage := 0.0
	if event.Capacity > 0 {
		soldPercentage = (float64(perfData.TicketsSold) / float64(event.Capacity)) * 100
	}

	avgPrice := 0.0
	if perfData.TicketsSold > 0 {
		avgPrice = perfData.Revenue / float64(perfData.TicketsSold)
	}

	// Get tier performance
	var tiers []models.EventTier
	database.GetDB().Find(&tiers, "event_id = ?", eventID)

	tierPerformance := make([]models.TierPerformance, len(tiers))
	topTier := models.TierPerformance{}

	for i, tier := range tiers {
		var tierData struct {
			TicketsSold int64
			Revenue     float64
		}

		database.GetDB().Raw(`
			SELECT
				COALESCE(SUM(CASE WHEN status = 'completed' THEN quantity ELSE 0 END), 0) as tickets_sold,
				COALESCE(SUM(CASE WHEN status = 'completed' THEN amount ELSE 0 END), 0) as revenue
			FROM transactions
			WHERE event_id = ? AND tier_id = ? AND status = 'completed'
		`, eventID, tier.ID).Scan(&tierData)

		tierSoldPercentage := 0.0
		if tier.Quantity > 0 {
			tierSoldPercentage = (float64(tierData.TicketsSold) / float64(tier.Quantity)) * 100
		}

		tp := models.TierPerformance{
			TierID:         tier.ID,
			TierName:       tier.TierName,
			TicketPrice:    tier.Price,
			TicketCapacity: tier.Quantity,
			TicketsSold:    tierData.TicketsSold,
			SoldPercentage: tierSoldPercentage,
			Revenue:        tierData.Revenue,
		}

		tierPerformance[i] = tp

		if i == 0 || tierData.TicketsSold > topTier.TicketsSold {
			topTier = tp
		}
	}

	return &models.EventPerformanceReport{
		EventID:            event.ID,
		EventTitle:         event.Title,
		BannerImage:        event.BannerImage,
		Status:             event.Status,
		StartDate:          event.StartDate,
		EndDate:            event.EndDate,
		Capacity:           event.Capacity,
		TicketsSold:        perfData.TicketsSold,
		SoldPercentage:     soldPercentage,
		Revenue:            perfData.Revenue,
		Commission:         perfData.Commission,
		OrganizerEarnings:  perfData.OrganizerEarnings,
		AverageTicketPrice: avgPrice,
		TotalTransactions:  perfData.Transactions,
		TopTier:            topTier,
		TierPerformance:    tierPerformance,
	}
}

func (h *ReportHandler) getTotalCustomers(startDate, endDate time.Time) int64 {
	var count int64
	database.GetDB().Raw(`
		SELECT COUNT(DISTINCT COALESCE(user_id, guest_user_id))
		FROM transactions
		WHERE created_at BETWEEN ? AND ? AND status = 'completed'
	`, startDate, endDate.AddDate(0, 0, 1)).Scan(&count)
	return count
}

func (h *ReportHandler) getRegisteredUsers(startDate, endDate time.Time) int64 {
	var count int64
	database.GetDB().Raw(`
		SELECT COUNT(DISTINCT user_id)
		FROM transactions
		WHERE created_at BETWEEN ? AND ? AND user_id IS NOT NULL AND status = 'completed'
	`, startDate, endDate.AddDate(0, 0, 1)).Scan(&count)
	return count
}

func (h *ReportHandler) getGuestPurchases(startDate, endDate time.Time) int64 {
	var count int64
	database.GetDB().Raw(`
		SELECT COUNT(DISTINCT guest_user_id)
		FROM transactions
		WHERE created_at BETWEEN ? AND ? AND guest_user_id IS NOT NULL AND status = 'completed'
	`, startDate, endDate.AddDate(0, 0, 1)).Scan(&count)
	return count
}

func (h *ReportHandler) getRepeatCustomers(startDate, endDate time.Time) int64 {
	var count int64
	database.GetDB().Raw(`
		SELECT COUNT(*)
		FROM (
			SELECT COALESCE(user_id, guest_user_id) as customer_id
			FROM transactions
			WHERE created_at BETWEEN ? AND ? AND status = 'completed'
			GROUP BY COALESCE(user_id, guest_user_id)
			HAVING COUNT(*) > 1
		) repeated_customers
	`, startDate, endDate.AddDate(0, 0, 1)).Scan(&count)
	return count
}

func (h *ReportHandler) getAverageOrderValue(startDate, endDate time.Time) float64 {
	var avg float64
	database.GetDB().Raw(`
		SELECT COALESCE(AVG(amount), 0)
		FROM transactions
		WHERE created_at BETWEEN ? AND ? AND status = 'completed'
	`, startDate, endDate.AddDate(0, 0, 1)).Scan(&avg)
	return avg
}

func (h *ReportHandler) getCustomerSegments(startDate, endDate time.Time) []models.CustomerSegment {
	var segments []models.CustomerSegment

	database.GetDB().Raw(`
		WITH customer_stats AS (
			SELECT
				COALESCE(user_id, guest_user_id) as customer_id,
				COUNT(*) as purchases,
				SUM(amount) as total_spent
			FROM transactions
			WHERE created_at BETWEEN ? AND ? AND status = 'completed'
			GROUP BY COALESCE(user_id, guest_user_id)
		),
		segment_data AS (
			SELECT
				CASE
					WHEN total_spent > 100000 THEN 'High Value'
					WHEN total_spent BETWEEN 50000 AND 100000 THEN 'Premium'
					WHEN total_spent BETWEEN 10000 AND 50000 THEN 'Regular'
					ELSE 'Occasional'
				END as segment_name,
				COUNT(*) as customer_count,
				SUM(total_spent) as total_spent_in_segment
			FROM customer_stats
			GROUP BY segment_name
		),
		total_customers AS (
			SELECT SUM(customer_count) as total FROM segment_data
		)
		SELECT
			segment_name,
			customer_count,
			total_spent_in_segment as total_spent,
			CASE WHEN customer_count > 0 THEN total_spent_in_segment / customer_count ELSE 0 END as average_spent,
			(customer_count::float / (SELECT total FROM total_customers)) * 100 as percentage_of_total
		FROM segment_data
	`, startDate, endDate.AddDate(0, 0, 1)).Scan(&segments)

	return segments
}

func (h *ReportHandler) getTopCustomers(startDate, endDate time.Time, limit int) []models.TopCustomer {
	var customers []models.TopCustomer

	database.GetDB().Raw(`
		SELECT
			user_id as customer_id,
			COALESCE(u.full_name, gu.name, 'Guest') as customer_name,
			COALESCE(u.email, gu.email, '') as customer_email,
			SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END) as total_spent,
			SUM(CASE WHEN t.status = 'completed' THEN t.quantity ELSE 0 END) as tickets_purchased,
			COUNT(DISTINCT CASE WHEN t.status = 'completed' THEN t.event_id END) as events_attended,
			MAX(t.created_at) as last_purchase_date
		FROM transactions t
		LEFT JOIN users u ON t.user_id = u.id
		LEFT JOIN guest_users gu ON t.guest_user_id = gu.id
		WHERE t.created_at BETWEEN ? AND ?
		GROUP BY t.user_id, u.id, gu.id
		ORDER BY total_spent DESC
		LIMIT ?
	`, startDate, endDate.AddDate(0, 0, 1), limit).Scan(&customers)

	return customers
}

func (h *ReportHandler) getCustomerRetention(startDate, endDate time.Time) models.CustomerRetention {
	var newCustomers, returningCustomers, active int64

	database.GetDB().Raw(`
		SELECT
			COUNT(*) FILTER (WHERE first_purchase_date BETWEEN ? AND ?) as new_customers,
			COUNT(*) FILTER (WHERE first_purchase_date < ? AND last_purchase_date BETWEEN ? AND ?) as returning_customers,
			COUNT(*) as active_customers
		FROM (
			SELECT
				COALESCE(user_id, guest_user_id) as customer_id,
				MIN(created_at) as first_purchase_date,
				MAX(created_at) as last_purchase_date
			FROM transactions
			WHERE status = 'completed'
			GROUP BY COALESCE(user_id, guest_user_id)
		) customer_history
	`, startDate, endDate.AddDate(0, 0, 1), startDate, startDate, endDate.AddDate(0, 0, 1)).
		Row().
		Scan(&newCustomers, &returningCustomers, &active)

	retention := 0.0
	churn := 0.0

	if active > 0 {
		retention = (float64(returningCustomers) / float64(active)) * 100
		churn = 100 - retention
	}

	return models.CustomerRetention{
		NewCustomers:       newCustomers,
		ReturningCustomers: returningCustomers,
		RetentionRate:      retention,
		ChurnRate:          churn,
	}
}

func (h *ReportHandler) getFinancialSummary(startDate, endDate time.Time, organizerID *uuid.UUID) models.FinancialSummary {
	var summary struct {
		GrossRevenue      float64
		Commission        float64
		OrganizerShare    float64
		Refunds           float64
		PendingPayouts    float64
		TotalTransactions int64
		TicketCount       int64
	}

	query := `
		SELECT
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as gross_revenue,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.commission_amount ELSE 0 END), 0) as commission,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.organizer_share ELSE 0 END), 0) as organizer_share,
			COALESCE(SUM(CASE WHEN t.status = 'refunded' THEN t.amount ELSE 0 END), 0) as refunds,
			COUNT(*) as total_transactions,
			COALESCE(SUM(t.quantity), 0) as ticket_count
		FROM transactions t
		LEFT JOIN events e ON t.event_id = e.id
		WHERE t.created_at BETWEEN ? AND ?
	`

	params := []interface{}{startDate, endDate.AddDate(0, 0, 1)}

	if organizerID != nil {
		query += ` AND e.organizer_id = ?`
		params = append(params, organizerID)
	}

	database.GetDB().Raw(query, params...).Scan(&summary)

	// Get pending payouts
	var pendingPayouts float64
	if organizerID != nil {
		database.GetDB().Raw(`
			SELECT COALESCE(SUM(billed_amount), 0)
			FROM payment_bills
			WHERE status = 'pending' AND organizer_id = ?
		`, organizerID).Scan(&pendingPayouts)
	}

	// Get completed payouts
	var completedPayouts float64
	query = `
		SELECT COALESCE(SUM(billed_amount), 0)
		FROM payment_bills
		WHERE status = 'paid'
	`
	params = []interface{}{}
	if organizerID != nil {
		query += ` AND organizer_id = ?`
		params = append(params, organizerID)
	}
	database.GetDB().Raw(query, params...).Scan(&completedPayouts)

	avgTicketPrice := 0.0
	if summary.TicketCount > 0 {
		avgTicketPrice = summary.GrossRevenue / float64(summary.TicketCount)
	}

	netRevenue := summary.GrossRevenue - summary.Refunds

	return models.FinancialSummary{
		TotalGrossRevenue:   summary.GrossRevenue,
		TotalCommission:     summary.Commission,
		TotalOrganizerShare: summary.OrganizerShare,
		TotalRefunds:        summary.Refunds,
		NetRevenue:          netRevenue,
		PendingPayouts:      pendingPayouts,
		CompletedPayouts:    completedPayouts,
		AverageTicketPrice:  avgTicketPrice,
		TotalTransactions:   summary.TotalTransactions,
	}
}

func (h *ReportHandler) getRevenueBreakdown(startDate, endDate time.Time, organizerID *uuid.UUID) []models.RevenueBreakdown {
	var breakdown []models.RevenueBreakdown

	query := `
		SELECT
			e.id as event_id,
			e.title as event_title,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as gross_revenue,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.commission_amount ELSE 0 END), 0) as commission,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.organizer_share ELSE 0 END), 0) as organizer_share,
			COALESCE(SUM(CASE WHEN t.status = 'refunded' THEN t.amount ELSE 0 END), 0) as refunds,
			COUNT(*) as transaction_count
		FROM events e
		LEFT JOIN transactions t ON e.id = t.event_id AND t.created_at BETWEEN ? AND ?
		WHERE e.deleted_at IS NULL
	`

	params := []interface{}{startDate, endDate.AddDate(0, 0, 1)}

	if organizerID != nil {
		query += ` AND e.organizer_id = ?`
		params = append(params, organizerID)
	}

	query += `
		GROUP BY e.id
		ORDER BY gross_revenue DESC
	`

	database.GetDB().Raw(query, params...).Scan(&breakdown)

	for i := range breakdown {
		breakdown[i].NetRevenue = breakdown[i].GrossRevenue - breakdown[i].Refunds
	}

	return breakdown
}

func (h *ReportHandler) getCommissionHistory(startDate, endDate time.Time, organizerID *uuid.UUID) []models.CommissionRecord {
	var records []models.CommissionRecord

	query := `
		SELECT
			t.id as transaction_id,
			t.event_id,
			e.title as event_title,
			t.amount as revenue,
			t.commission_rate,
			t.commission_amount,
			t.created_at
		FROM transactions t
		LEFT JOIN events e ON t.event_id = e.id
		WHERE t.created_at BETWEEN ? AND ? AND t.status = 'completed'
	`

	params := []interface{}{startDate, endDate.AddDate(0, 0, 1)}

	if organizerID != nil {
		query += ` AND e.organizer_id = ?`
		params = append(params, organizerID)
	}

	query += ` ORDER BY t.created_at DESC`

	database.GetDB().Raw(query, params...).Scan(&records)
	return records
}

func (h *ReportHandler) getPayoutHistory(startDate, endDate time.Time, organizerID *uuid.UUID) []models.PayoutRecord {
	var records []models.PayoutRecord

	query := `
		SELECT
			pb.id as payout_id,
			pb.organizer_id,
			COALESCE(u.full_name, 'Unknown') as organizer_name,
			pb.billed_amount as amount,
			pb.status,
			COALESCE(pb.payment_method::text, 'N/A') as payment_method,
			COALESCE(pb.payment_ref, '') as payment_reference,
			pb.updated_at as processed_at,
			pb.created_at
		FROM payment_bills pb
		LEFT JOIN users u ON pb.organizer_id = u.id
		WHERE pb.created_at BETWEEN ? AND ?
	`

	params := []interface{}{startDate, endDate.AddDate(0, 0, 1)}

	if organizerID != nil {
		query += ` AND pb.organizer_id = ?`
		params = append(params, organizerID)
	}

	query += ` ORDER BY pb.created_at DESC`

	database.GetDB().Raw(query, params...).Scan(&records)
	return records
}

func (h *ReportHandler) getCurrencyBreakdown(startDate, endDate time.Time, organizerID *uuid.UUID) []models.CurrencyFinancial {
	var breakdown []models.CurrencyFinancial

	query := `
		SELECT
			t.currency,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as gross_revenue,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.commission_amount ELSE 0 END), 0) as commission,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.organizer_share ELSE 0 END), 0) as organizer_share,
			COUNT(*) as transaction_count
		FROM transactions t
		LEFT JOIN events e ON t.event_id = e.id
		WHERE t.created_at BETWEEN ? AND ?
	`

	params := []interface{}{startDate, endDate.AddDate(0, 0, 1)}

	if organizerID != nil {
		query += ` AND e.organizer_id = ?`
		params = append(params, organizerID)
	}

	query += `
		GROUP BY t.currency
		ORDER BY gross_revenue DESC
	`

	database.GetDB().Raw(query, params...).Scan(&breakdown)

	// Calculate total
	var totalRevenue float64
	for _, b := range breakdown {
		totalRevenue += b.GrossRevenue
	}

	for i := range breakdown {
		if totalRevenue > 0 {
			breakdown[i].PercentageOfTotal = (breakdown[i].GrossRevenue / totalRevenue) * 100
		}
	}

	return breakdown
}

// ====================== Organizer Dashboard Helper Methods ======================

// getOrganizerDashboardMetrics retrieves comprehensive metrics for organizer dashboard
func (h *ReportHandler) getOrganizerDashboardMetrics(startDate, endDate time.Time, organizerID uuid.UUID) models.OrganizerDashboardMetrics {
	metrics := models.OrganizerDashboardMetrics{}

	query := `
		SELECT
			COALESCE(SUM(t.amount), 0) as total_revenue,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as organizer_earnings,
			COALESCE(COUNT(DISTINCT(CASE WHEN t.status = 'completed' THEN t.id END)), 0) as total_tickets_sold,
			COALESCE(COUNT(DISTINCT CASE WHEN t.status = 'completed' THEN t.customer_id END), 0) as total_attendees,
			(SELECT COUNT(*) FROM events WHERE organizer_id = ? AND deleted_at IS NULL) as total_events,
			(SELECT COUNT(*) FROM events WHERE organizer_id = ? AND status IN ('on_sale', 'live') AND start_date > now() AND deleted_at IS NULL) as active_events
		FROM transactions t
		JOIN events e ON t.event_id = e.id
		WHERE e.organizer_id = ? AND t.created_at BETWEEN ? AND ? AND t.deleted_at IS NULL
	`
	database.GetDB().Raw(query, organizerID, organizerID, organizerID, startDate, endDate).Scan(&metrics)

	// Calculate additional metrics
	if metrics.TotalTicketsSold > 0 {
		metrics.AverageTicketPrice = metrics.TotalRevenue / float64(metrics.TotalTicketsSold)
	}

	if metrics.TotalEvents > 0 {
		metrics.AverageEventRevenue = metrics.TotalRevenue / float64(metrics.TotalEvents)
		metrics.AverageTicketsPerEvent = float64(metrics.TotalTicketsSold) / float64(metrics.TotalEvents)
	}

	// Get capacity metrics
	capacityQuery := `
		SELECT COALESCE(SUM(et.quantity), 0) as total_capacity
		FROM event_tiers et
		JOIN events e ON et.event_id = e.id
		WHERE e.organizer_id = ? AND et.deleted_at IS NULL
	`
	database.GetDB().Raw(capacityQuery, organizerID).Scan(&metrics)

	if metrics.TotalTicketsCapacity > 0 {
		metrics.OccupancyRate = (float64(metrics.TotalTicketsSold) / float64(metrics.TotalTicketsCapacity)) * 100
	}

	// Calculate conversion rate
	totalCapacity := metrics.TotalTicketsCapacity
	if totalCapacity > 0 {
		metrics.ConversionRate = (float64(metrics.TotalTicketsSold) / float64(totalCapacity)) * 100
	}

	return metrics
}

// getTicketsByEvent retrieves ticket sales broken down by individual events
func (h *ReportHandler) getTicketsByEvent(startDate, endDate time.Time, organizerID uuid.UUID) []models.TicketsByEventData {
	var ticketsData []models.TicketsByEventData

	query := `
		SELECT
			e.id as event_id,
			e.title as event_title,
			COALESCE(COUNT(DISTINCT CASE WHEN t.status = 'completed' THEN t.id END), 0) as tickets_sold,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as event_revenue,
			COALESCE(COUNT(DISTINCT CASE WHEN t.status = 'completed' THEN t.customer_id END), 0) as attendees,
			e.status,
			CONCAT(COALESCE(COUNT(DISTINCT CASE WHEN t.status = 'completed' THEN t.id END), 0), ' tickets') as display_label
		FROM events e
		LEFT JOIN transactions t ON e.id = t.event_id AND t.created_at BETWEEN ? AND ? AND t.deleted_at IS NULL
		WHERE e.organizer_id = ? AND e.deleted_at IS NULL
		GROUP BY e.id, e.title, e.status
		ORDER BY tickets_sold DESC
	`
	database.GetDB().Raw(query, startDate, endDate, organizerID).Scan(&ticketsData)
	return ticketsData
}

// getRevenueOverTime retrieves revenue aggregated over time periods
func (h *ReportHandler) getRevenueOverTime(startDate, endDate time.Time, organizerID uuid.UUID) []models.RevenueOverTimeData {
	var revenueData []models.RevenueOverTimeData

	query := `
		SELECT
			TO_CHAR(t.created_at, 'YYYY-MM') as period,
			COALESCE(SUM(t.amount), 0) as revenue,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as organizer_earn,
			COALESCE(COUNT(DISTINCT t.id), 0) as tickets_sold,
			COALESCE(COUNT(DISTINCT t.id), 0) as transaction_count
		FROM transactions t
		JOIN events e ON t.event_id = e.id
		WHERE e.organizer_id = ? AND t.created_at BETWEEN ? AND ? AND t.deleted_at IS NULL
		GROUP BY TO_CHAR(t.created_at, 'YYYY-MM')
		ORDER BY period ASC
	`
	database.GetDB().Raw(query, organizerID, startDate, endDate).Scan(&revenueData)
	return revenueData
}

// getEventPerformanceSummary retrieves summary performance for each event
func (h *ReportHandler) getEventPerformanceSummary(startDate, endDate time.Time, organizerID uuid.UUID) []models.EventPerformanceSummary {
	var eventPerf []models.EventPerformanceSummary

	query := `
		SELECT
			e.id as event_id,
			e.title as event_title,
			e.banner_image,
			e.status,
			e.start_date,
			COALESCE(SUM(et.quantity), 0) as tickets_capacity,
			COALESCE(COUNT(DISTINCT CASE WHEN t.status = 'completed' THEN t.id END), 0) as tickets_sold,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as revenue,
			COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as organizer_earnings,
			COALESCE(COUNT(DISTINCT CASE WHEN t.status = 'completed' THEN t.customer_id END), 0) as attendees,
			COALESCE(COUNT(DISTINCT t.id), 0) as total_transactions,
			COALESCE(COUNT(DISTINCT CASE WHEN t.status = 'refunded' THEN t.id END), 0) as refunded_count
		FROM events e
		LEFT JOIN event_tiers et ON e.id = et.event_id AND et.deleted_at IS NULL
		LEFT JOIN transactions t ON e.id = t.event_id AND t.created_at BETWEEN ? AND ? AND t.deleted_at IS NULL
		WHERE e.organizer_id = ? AND e.deleted_at IS NULL
		GROUP BY e.id, e.title, e.banner_image, e.status, e.start_date
		ORDER BY revenue DESC
	`
	database.GetDB().Raw(query, startDate, endDate, organizerID).Scan(&eventPerf)

	// Calculate percentages
	for i := range eventPerf {
		if eventPerf[i].TicketsCapacity > 0 {
			eventPerf[i].SoldPercentage = (float64(eventPerf[i].TicketsSold) / float64(eventPerf[i].TicketsCapacity)) * 100
		}
		if eventPerf[i].TotalTransactions > 0 {
			eventPerf[i].ConversionRate = (float64(eventPerf[i].TicketsSold) / float64(eventPerf[i].TotalTransactions)) * 100
			eventPerf[i].AverageTicketPrice = eventPerf[i].Revenue / float64(eventPerf[i].TotalTransactions)
		}
	}

	return eventPerf
}

// getAttendanceMetrics retrieves attendee-related metrics
func (h *ReportHandler) getAttendanceMetrics(startDate, endDate time.Time, organizerID uuid.UUID) models.AttendanceMetrics {
	metrics := models.AttendanceMetrics{}

	query := `
		SELECT
			COALESCE(COUNT(DISTINCT CASE WHEN t.status = 'completed' THEN t.customer_id END), 0) as total_attendees,
			COALESCE(COUNT(DISTINCT CASE WHEN t.status = 'completed' AND u.id IS NOT NULL THEN t.customer_id END), 0) as registered_attendees,
			COALESCE(COUNT(DISTINCT CASE WHEN t.status = 'completed' AND t.customer_id IS NULL THEN t.id END), 0) as guest_attendees
		FROM transactions t
		JOIN events e ON t.event_id = e.id
		LEFT JOIN users u ON t.customer_id = u.id
		WHERE e.organizer_id = ? AND t.created_at BETWEEN ? AND ? AND t.deleted_at IS NULL
	`
	database.GetDB().Raw(query, organizerID, startDate, endDate).Scan(&metrics)

	// Get repeat attendee rate
	repeatQuery := `
		SELECT
			COUNT(DISTINCT customer_id) FILTER (WHERE purchase_count > 1) as returning_attendees,
			COUNT(DISTINCT customer_id) FILTER (WHERE purchase_count = 1) as new_attendees
		FROM (
			SELECT t.customer_id, COUNT(*) as purchase_count
			FROM transactions t
			JOIN events e ON t.event_id = e.id
			WHERE e.organizer_id = ? AND t.status = 'completed' AND t.deleted_at IS NULL
			GROUP BY t.customer_id
		) subq
	`
	database.GetDB().Raw(repeatQuery, organizerID).Scan(&metrics)

	// Calculate metrics
	if metrics.TotalAttendees > 0 {
		var totalEvents int64
		database.GetDB().Raw("SELECT COUNT(*) FROM events WHERE organizer_id = ? AND deleted_at IS NULL", organizerID).Scan(&totalEvents)
		if totalEvents > 0 {
			metrics.AveragePerEvent = float64(metrics.TotalAttendees) / float64(totalEvents)
		}

		if metrics.ReturningAttendees > 0 {
			metrics.RepeatAttendeeRate = (float64(metrics.ReturningAttendees) / float64(metrics.TotalAttendees)) * 100
		}
	}

	return metrics
}

// getPaymentMethodStats retrieves payment method distribution
func (h *ReportHandler) getPaymentMethodStats(startDate, endDate time.Time, organizerID uuid.UUID) []models.PaymentMethodData {
	var paymentStats []models.PaymentMethodData

	query := `
		SELECT
			t.payment_gateway as payment_method,
			COUNT(*) as transaction_count,
			COALESCE(SUM(t.amount), 0) as total_amount,
			COALESCE(COUNT(CASE WHEN t.status = 'completed' THEN t.id END), 0)::float / COUNT(*)::float * 100 as success_rate
		FROM transactions t
		JOIN events e ON t.event_id = e.id
		WHERE e.organizer_id = ? AND t.created_at BETWEEN ? AND ? AND t.deleted_at IS NULL
		GROUP BY t.payment_gateway
		ORDER BY total_amount DESC
	`
	database.GetDB().Raw(query, organizerID, startDate, endDate).Scan(&paymentStats)

	// Calculate percentages
	var totalAmount float64
	for _, stat := range paymentStats {
		totalAmount += stat.TotalAmount
	}

	for i := range paymentStats {
		if totalAmount > 0 {
			paymentStats[i].PercentageOfTotal = (paymentStats[i].TotalAmount / totalAmount) * 100
		}
	}

	return paymentStats
}

// getTopEventTiers retrieves top-performing event tiers
func (h *ReportHandler) getTopEventTiers(startDate, endDate time.Time, organizerID uuid.UUID) []models.TopEventTierData {
	var topTiers []models.TopEventTierData

	query := `
		WITH tier_stats AS (
			SELECT
				et.id as tier_id,
				et.event_id,
				et.name as tier_name,
				e.title as event_title,
				et.price as ticket_price,
				et.quantity as ticket_capacity,
				COALESCE(COUNT(DISTINCT CASE WHEN t.status = 'completed' THEN t.id END), 0) as tickets_sold,
				COALESCE(SUM(CASE WHEN t.status = 'completed' THEN t.amount ELSE 0 END), 0) as revenue,
				ROW_NUMBER() OVER (ORDER BY COUNT(DISTINCT t.id) DESC) as rank
			FROM event_tiers et
			JOIN events e ON et.event_id = e.id
			LEFT JOIN transactions t ON et.id = t.tier_id AND t.created_at BETWEEN ? AND ? AND t.deleted_at IS NULL
			WHERE e.organizer_id = ? AND et.deleted_at IS NULL
			GROUP BY et.id, et.event_id, et.name, et.price, et.quantity, e.title
		)
		SELECT * FROM tier_stats WHERE rank <= 5
		ORDER BY rank ASC
	`
	database.GetDB().Raw(query, startDate, endDate, organizerID).Scan(&topTiers)

	// Calculate percentages
	for i := range topTiers {
		if topTiers[i].TicketCapacity > 0 {
			topTiers[i].SoldPercentage = (float64(topTiers[i].TicketsSold) / float64(topTiers[i].TicketCapacity)) * 100
		}
	}

	return topTiers
}

// getUpcomingEvents retrieves upcoming organizer events
func (h *ReportHandler) getUpcomingEvents(organizerID uuid.UUID) []models.UpcomingEventData {
	var upcomingEvents []models.UpcomingEventData

	query := `
		SELECT
			e.id as event_id,
			e.title as event_title,
			e.banner_image,
			e.status,
			e.start_date,
			COALESCE(SUM(et.quantity), 0) as ticket_capacity,
			COALESCE(COUNT(DISTINCT CASE WHEN t.status = 'completed' THEN t.id END), 0) as tickets_sold,
			EXTRACT(DAY FROM e.start_date - now()) as days_until_start
		FROM events e
		LEFT JOIN event_tiers et ON e.id = et.event_id AND et.deleted_at IS NULL
		LEFT JOIN transactions t ON e.id = t.event_id AND t.status = 'completed' AND t.deleted_at IS NULL
		WHERE e.organizer_id = ? AND e.start_date > now() AND e.deleted_at IS NULL
		GROUP BY e.id, e.title, e.banner_image, e.status, e.start_date
		ORDER BY e.start_date ASC
		LIMIT 10
	`
	database.GetDB().Raw(query, organizerID).Scan(&upcomingEvents)

	// Calculate percentages
	for i := range upcomingEvents {
		if upcomingEvents[i].TicketCapacity > 0 {
			upcomingEvents[i].SoldPercentage = (float64(upcomingEvents[i].TicketsSold) / float64(upcomingEvents[i].TicketCapacity)) * 100
		}
	}

	return upcomingEvents
}
