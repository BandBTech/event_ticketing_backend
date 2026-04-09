package handlers

import (
	"log"
	"net/http"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type DashboardHandler struct{}

func NewDashboardHandler() *DashboardHandler {
	return &DashboardHandler{}
}

// GetAdminDashboard godoc
// @Summary Get admin dashboard data
// @Description Get comprehensive dashboard statistics for admin users
// @Tags Dashboard
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/dashboard [get]
func (h *DashboardHandler) GetAdminDashboard(c *gin.Context) {
	now := time.Now()

	// Single comprehensive query for all counts and aggregations
	var systemStats struct {
		// Users
		TotalUsers    int64 `json:"total_users"`
		ActiveUsers   int64 `json:"active_users"`
		InactiveUsers int64 `json:"inactive_users"`

		// Organizers
		TotalOrganizers    int64 `json:"total_organizers"`
		ApprovedOrganizers int64 `json:"approved_organizers"`
		PendingOrganizers  int64 `json:"pending_organizers"`
		RejectedOrganizers int64 `json:"rejected_organizers"`

		// Events
		TotalEvents     int64 `json:"total_events"`
		DraftEvents     int64 `json:"draft_events"`
		PendingEvents   int64 `json:"pending_events"`
		ApprovedEvents  int64 `json:"approved_events"`
		RejectedEvents  int64 `json:"rejected_events"`
		OnSaleEvents    int64 `json:"on_sale_events"`
		LiveEvents      int64 `json:"live_events"`
		CompletedEvents int64 `json:"completed_events"`
		CancelledEvents int64 `json:"cancelled_events"`
		ScheduledEvents int64 `json:"scheduled_events"`
		UpcomingEvents  int64 `json:"upcoming_events"`

		// Transactions & Revenue
		TotalTransactions      int64   `json:"total_transactions"`
		CompletedTrans         int64   `json:"completed_transactions"`
		PendingTrans           int64   `json:"pending_transactions"`
		FailedTrans            int64   `json:"failed_transactions"`
		TotalRevenue           float64 `json:"total_revenue"`
		TotalUSDRevenue        float64 `json:"total_usd_revenue"`
		TotalRefunds           float64 `json:"total_refunds"`
		NetRevenue             float64 `json:"net_revenue"`
		TotalCommission        float64 `json:"total_commission"`
		TotalCommissionRefunds float64 `json:"total_commission_refunds"`
		NetCommission          float64 `json:"net_commission"`
		TotalOrganizerShare    float64 `json:"total_organizer_share"`
		TotalOrganizerRefunds  float64 `json:"total_organizer_refunds"`
		NetOrganizerShare      float64 `json:"net_organizer_share"`

		// Refunds
		CompletedRefunds int64 `json:"completed_refunds"`
		PendingRefunds   int64 `json:"pending_refunds"`

		// Tickets
		TotalTicketsSold int64 `json:"total_tickets_sold"`
		ActiveTickets    int64 `json:"active_tickets"`
		UsedTickets      int64 `json:"used_tickets"`
		CancelledTickets int64 `json:"cancelled_tickets"`

		// Payments
		TotalPaymentBills int64   `json:"total_payment_bills"`
		PaidBills         int64   `json:"paid_bills"`
		PendingBills      int64   `json:"pending_bills"`
		TotalPaidOut      float64 `json:"total_paid_out"`
		TotalAmountDue    float64 `json:"total_amount_due"`

		// Payout Requests
		TotalPayoutRequests int64   `json:"total_payout_requests"`
		PendingPayouts      int64   `json:"pending_payouts"`
		ApprovedPayouts     int64   `json:"approved_payouts"`
		PaidPayouts         int64   `json:"paid_payouts"`
		CancelledPayouts    int64   `json:"cancelled_payouts"`
		RejectedPayouts     int64   `json:"rejected_payouts"`
		TotalPayoutAmount   float64 `json:"total_payout_amount"`
	}

	// Get all statistics efficiently using CTEs for better query optimization
	threeMonthsFromNow := now.AddDate(0, 3, 0)
	database.GetDB().Raw(`
		WITH user_stats AS (
			SELECT
				COUNT(*) FILTER (WHERE deleted_at IS NULL) as total_users,
				COUNT(*) FILTER (WHERE account_status = 'active' AND deleted_at IS NULL) as active_users,
				COUNT(*) FILTER (WHERE account_status = 'inactive' AND deleted_at IS NULL) as inactive_users,
				COUNT(*) FILTER (WHERE organizer_status != 'inactive' AND deleted_at IS NULL) as total_organizers,
				COUNT(*) FILTER (WHERE organizer_status = 'approved' AND deleted_at IS NULL) as approved_organizers,
				COUNT(*) FILTER (WHERE organizer_status = 'pending' AND deleted_at IS NULL) as pending_organizers,
				COUNT(*) FILTER (WHERE organizer_status = 'rejected' AND deleted_at IS NULL) as rejected_organizers
			FROM users
		),
		event_stats AS (
			SELECT
				COUNT(*) FILTER (WHERE deleted_at IS NULL) as total_events,
				COUNT(*) FILTER (WHERE status = 'draft' AND deleted_at IS NULL) as draft_events,
				COUNT(*) FILTER (WHERE status = 'pending' AND deleted_at IS NULL) as pending_events,
				COUNT(*) FILTER (WHERE status = 'approved' AND deleted_at IS NULL) as approved_events,
				COUNT(*) FILTER (WHERE status = 'rejected' AND deleted_at IS NULL) as rejected_events,
				COUNT(*) FILTER (WHERE status = 'on_sale' AND deleted_at IS NULL) as on_sale_events,
				COUNT(*) FILTER (WHERE status = 'live' AND deleted_at IS NULL) as live_events,
				COUNT(*) FILTER (WHERE status = 'completed' AND deleted_at IS NULL) as completed_events,
				COUNT(*) FILTER (WHERE is_cancelled = true AND deleted_at IS NULL) as cancelled_events,
				COUNT(*) FILTER (WHERE status = 'scheduled' AND deleted_at IS NULL) as scheduled_events,
				COUNT(*) FILTER (WHERE start_date > $1 AND start_date <= $2 AND status IN ('on_sale', 'approved') AND deleted_at IS NULL) as upcoming_events
			FROM events
		),
		transaction_stats AS (
			SELECT
				COUNT(*) as total_transactions,
				COUNT(*) FILTER (WHERE status = 'completed') as completed_trans,
				COUNT(*) FILTER (WHERE status = 'pending') as pending_trans,
				COUNT(*) FILTER (WHERE status = 'failed') as failed_trans,
				COALESCE(SUM(amount) FILTER (WHERE status = 'completed'), 0) as total_revenue,
				COALESCE(SUM(commission_amount) FILTER (WHERE status = 'completed'), 0) as total_commission,
				COALESCE(SUM(organizer_share) FILTER (WHERE status = 'completed'), 0) as total_organizer_share,
				COALESCE(SUM(quantity) FILTER (WHERE status = 'completed'), 0) as total_tickets_sold
			FROM transactions
		),
		refund_stats AS (
			SELECT
				COALESCE(SUM(amount) FILTER (WHERE status = 'completed'), 0) as total_refunds,
				COALESCE(SUM(commission_refund) FILTER (WHERE status = 'completed'), 0) as total_commission_refunds,
				COALESCE(SUM(organizer_refund) FILTER (WHERE status = 'completed'), 0) as total_organizer_refunds,
				COUNT(*) FILTER (WHERE status = 'completed') as completed_refunds,
				COUNT(*) FILTER (WHERE status = 'pending') as pending_refunds
			FROM refunds
		),
		currency_revenue_stats AS (
			SELECT
				currency,
				COALESCE(SUM(amount) FILTER (WHERE status = 'completed'), 0) as local_revenue,
				COALESCE(SUM(amount_base) FILTER (WHERE status = 'completed'), 0) as usd_revenue,
				COUNT(*) FILTER (WHERE status = 'completed') as transaction_count
			FROM transactions
			WHERE status = 'completed'
			GROUP BY currency
			ORDER BY usd_revenue DESC
		),
		total_usd_revenue AS (
			SELECT COALESCE(SUM(amount_base) FILTER (WHERE status = 'completed'), 0) as total_usd_revenue
			FROM transactions
		),
		payment_stats AS (
			SELECT
				COUNT(*) as total_payment_bills,
				COUNT(*) FILTER (WHERE status = 'paid') as paid_bills,
				COUNT(*) FILTER (WHERE status = 'pending') as pending_bills,
				COALESCE(SUM(billed_amount) FILTER (WHERE status = 'paid'), 0) as total_paid_out,
				COALESCE(SUM(billed_amount) FILTER (WHERE status = 'pending'), 0) as total_amount_due
			FROM payment_bills
		),
		payout_stats AS (
			SELECT
				COUNT(*) as total_payout_requests,
				COUNT(*) FILTER (WHERE status = 'pending') as pending_payouts,
				COUNT(*) FILTER (WHERE status = 'approved') as approved_payouts,
				COUNT(*) FILTER (WHERE status = 'paid') as paid_payouts,
				COUNT(*) FILTER (WHERE status = 'cancelled') as cancelled_payouts,
				COUNT(*) FILTER (WHERE status = 'rejected') as rejected_payouts,
				COALESCE(SUM(amount), 0) as total_payout_amount
			FROM payout_requests
		)
		SELECT 
			user_stats.*,
			event_stats.*,
			transaction_stats.*,
			refund_stats.*,
			ticket_stats.*,
			payment_stats.*,
			payout_stats.*,
			total_usd_revenue.total_usd_revenue
		FROM user_stats, event_stats, transaction_stats, refund_stats, ticket_stats, payment_stats, payout_stats, total_usd_revenue
	`, now, threeMonthsFromNow).Scan(&systemStats)

	// Get currency breakdown
	var currencyBreakdown []map[string]interface{}
	database.GetDB().Raw(`
		SELECT
			currency,
			SUM(amount) as local_revenue,
			SUM(amount_base) as usd_revenue,
			COUNT(*) as transaction_count
		FROM transactions
		WHERE status = 'completed'
		GROUP BY currency
		ORDER BY usd_revenue DESC
	`).Scan(&currencyBreakdown)

	// Calculate net values (gross minus refunds)
	systemStats.NetRevenue = systemStats.TotalRevenue - systemStats.TotalRefunds
	systemStats.NetCommission = systemStats.TotalCommission - systemStats.TotalCommissionRefunds
	systemStats.NetOrganizerShare = systemStats.TotalOrganizerShare - systemStats.TotalOrganizerRefunds

	// Get upcoming events list (only if needed for display) - limit to 3 months
	upcomingEventsResponse := []models.EventPublicSummaryResponse{}
	threeMonthsFromNow = now.AddDate(0, 3, 0)
	database.GetDB().Model(&models.Event{}).
		Select("id, title, banner_image, category, start_date, end_date, status, sales_status, is_featured, venue_name, created_at, updated_at").
		Where("start_date > ? AND start_date <= ? AND status IN (?)", now, threeMonthsFromNow, []string{"on_sale", "approved"}).
		Order("is_featured DESC, start_date ASC").
		Limit(12).
		Scan(&upcomingEventsResponse)

	dashboardData := map[string]interface{}{
		// Users Summary
		"users": map[string]interface{}{
			"total":    systemStats.TotalUsers,
			"active":   systemStats.ActiveUsers,
			"inactive": systemStats.InactiveUsers,
		},

		// Organizers Summary
		"organizers": map[string]interface{}{
			"total":    systemStats.TotalOrganizers,
			"approved": systemStats.ApprovedOrganizers,
			"pending":  systemStats.PendingOrganizers,
			"rejected": systemStats.RejectedOrganizers,
		},

		// Events Summary
		"events": map[string]interface{}{
			"total":     systemStats.TotalEvents,
			"draft":     systemStats.DraftEvents,
			"pending":   systemStats.PendingEvents,
			"approved":  systemStats.ApprovedEvents,
			"rejected":  systemStats.RejectedEvents,
			"on_sale":   systemStats.OnSaleEvents,
			"live":      systemStats.LiveEvents,
			"completed": systemStats.CompletedEvents,
			"cancelled": systemStats.CancelledEvents,
			"scheduled": systemStats.ScheduledEvents,
			"upcoming":  systemStats.UpcomingEvents,
		},

		// Transactions Summary
		"transactions": map[string]interface{}{
			"total":     systemStats.TotalTransactions,
			"completed": systemStats.CompletedTrans,
			"pending":   systemStats.PendingTrans,
			"failed":    systemStats.FailedTrans,
		},

		// Revenue Summary - PRIMARY VIEW: USD totals, SECONDARY: Currency breakdown
		"revenue": map[string]interface{}{
			// PRIMARY VIEW: Normalized to USD
			"total_revenue_usd":  systemStats.TotalUSDRevenue,
			"currency_breakdown": currencyBreakdown,

			// LEGACY: Local currency totals (for backward compatibility)
			"gross_revenue_local": systemStats.TotalRevenue,
			"total_refunds_local": systemStats.TotalRefunds,
			"net_revenue_local":   systemStats.NetRevenue,
		},

		// Refunds Summary
		"refunds": map[string]interface{}{
			"completed": systemStats.CompletedRefunds,
			"pending":   systemStats.PendingRefunds,
		},

		// Tickets Summary
		"tickets": map[string]interface{}{
			"total_sold": systemStats.TotalTicketsSold,
			"active":     systemStats.ActiveTickets,
			"used":       systemStats.UsedTickets,
			"cancelled":  systemStats.CancelledTickets,
		},

		// Payment Bills Summary
		"payment_bills": map[string]interface{}{
			"total":          systemStats.TotalPaymentBills,
			"paid":           systemStats.PaidBills,
			"pending":        systemStats.PendingBills,
			"total_paid_out": systemStats.TotalPaidOut,
			"total_due":      systemStats.TotalAmountDue,
		},

		// Payout Requests Summary
		"payout_requests": map[string]interface{}{
			"total":        systemStats.TotalPayoutRequests,
			"pending":      systemStats.PendingPayouts,
			"approved":     systemStats.ApprovedPayouts,
			"paid":         systemStats.PaidPayouts,
			"cancelled":    systemStats.CancelledPayouts,
			"rejected":     systemStats.RejectedPayouts,
			"total_amount": systemStats.TotalPayoutAmount,
		},

		// Upcoming Events List
		"upcoming_events_list": upcomingEventsResponse,
	}

	utils.SuccessResponse(c, http.StatusOK, "Admin dashboard data retrieved successfully", dashboardData)
}

// GetOrganizerDashboard godoc
// @Summary Get organizer dashboard data
// @Description Get dashboard statistics for organizer users including their events
// @Tags Dashboard
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/dashboard [get]
func (h *DashboardHandler) GetOrganizerDashboard(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	// Get organizer ID from user
	organizerID, err := utils.GetOrganizerIDForUser(database.GetDB(), userID.(uuid.UUID))
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	// Single optimized query for all statistics
	var stats struct {
		TotalEvents           int64   `json:"total_events_organized"`
		DraftEvents           int64   `json:"draft_events"`
		PendingEvents         int64   `json:"pending_events"`
		ApprovedEvents        int64   `json:"approved_events"`
		RejectedEvents        int64   `json:"rejected_events"`
		OnSaleEvents          int64   `json:"on_sale_events"`
		LiveEvents            int64   `json:"live_events"`
		CompletedEvents       int64   `json:"completed_events"`
		CancelledEvents       int64   `json:"cancelled_events"`
		TotalRevenue          float64 `json:"total_revenue"`      // Gross revenue (before commission)
		OrganizerEarnings     float64 `json:"organizer_earnings"` // Net earnings (after commission)
		TotalTicketsSold      int64   `json:"total_tickets_sold"`
		TotalCommissionAmount float64 `json:"total_commission_amount"`
	}

	// Get stats in one query using CTEs for better performance
	now := time.Now()

	database.GetDB().Raw(`
		WITH event_stats AS (
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
		),
		sales_stats AS (
			SELECT
				COALESCE(SUM(t.amount), 0) as total_revenue,
				COALESCE(SUM(t.commission_amount), 0) as total_commission_amount,
				COALESCE(SUM(t.organizer_share), 0) as gross_organizer_earnings,
				COALESCE(SUM(t.quantity), 0) as total_tickets_sold,
				COALESCE(SUM(r.organizer_refund), 0) as total_organizer_refunds
			FROM transactions t
			INNER JOIN events e ON t.event_id = e.id
			LEFT JOIN refunds r ON r.transaction_id = t.id AND r.status = 'completed'
			WHERE e.organizer_id = ? AND t.status = 'completed' AND t.deleted_at IS NULL
		)
		SELECT 
			es.total_events, es.draft_events, es.pending_events, es.approved_events, es.rejected_events,
			es.on_sale_events, es.live_events, es.completed_events, es.cancelled_events,
			ss.total_revenue, (ss.gross_organizer_earnings - ss.total_organizer_refunds) as organizer_earnings, ss.total_tickets_sold, ss.total_commission_amount
		FROM event_stats es
		CROSS JOIN sales_stats ss
	`, organizerID, organizerID).Scan(&stats)

	// Get upcoming events in single query with only needed fields - limit to 3 months
	threeMonthsFromNow := now.AddDate(0, 3, 0)
	upcomingEventsResponse := []models.EventPublicSummaryResponse{}

	database.GetDB().Model(&models.Event{}).
		Select("id, title, banner_image, category, start_date, end_date, status, sales_status, is_featured, venue_name, created_at, updated_at").
		Where("organizer_id = ? AND start_date > ? AND start_date <= ? AND status IN (?)", organizerID, now, threeMonthsFromNow, []string{"on_sale", "approved"}).
		Order("is_featured DESC, start_date ASC").
		Limit(12).
		Scan(&upcomingEventsResponse)

	// Get financial metrics: total received and pending amount
	var financialMetrics struct {
		TotalReceived float64
	}
	database.GetDB().Model(&models.EventSales{}).
		Select("COALESCE(SUM(paid_amount), 0) as total_received").
		Joins("INNER JOIN events ON event_sales.event_id = events.id").
		Where("events.organizer_id = ?", organizerID).
		Scan(&financialMetrics)

	// Get currency breakdown for this organizer
	var currencyBreakdown []map[string]interface{}
	database.GetDB().Raw(`
		SELECT
			t.currency,
			SUM(t.amount) as local_revenue,
			SUM(t.amount_base) as usd_revenue,
			COUNT(*) as transaction_count
		FROM transactions t
		INNER JOIN events e ON t.event_id = e.id
		WHERE e.organizer_id = ? AND t.status = 'completed'
		GROUP BY t.currency
		ORDER BY usd_revenue DESC
	`, organizerID).Scan(&currencyBreakdown)

	// Get total USD revenue for this organizer
	var totalUSDRevenue struct {
		TotalUSDRevenue float64 `json:"total_usd_revenue"`
	}
	database.GetDB().Raw(`
		SELECT COALESCE(SUM(t.amount_base), 0) as total_usd_revenue
		FROM transactions t
		INNER JOIN events e ON t.event_id = e.id
		WHERE e.organizer_id = ? AND t.status = 'completed'
	`, organizerID).Scan(&totalUSDRevenue)

	// Calculate total pending amount from pending payout requests
	var totalPendingAmount float64
	database.GetDB().Model(&models.PayoutRequest{}).
		Select("COALESCE(SUM(amount), 0) as total_pending").
		Where("organizer_id = ? AND status = ?", organizerID, "pending").
		Scan(&totalPendingAmount)

	dashboardData := map[string]interface{}{
		// Event Statistics
		"events": map[string]interface{}{
			"total":     stats.TotalEvents,
			"draft":     stats.DraftEvents,
			"pending":   stats.PendingEvents,
			"approved":  stats.ApprovedEvents,
			"rejected":  stats.RejectedEvents,
			"on_sale":   stats.OnSaleEvents,
			"live":      stats.LiveEvents,
			"completed": stats.CompletedEvents,
			"cancelled": stats.CancelledEvents,
		},
		// Sales Statistics (only for events older than 1 month)
		"total_revenue_usd":       totalUSDRevenue.TotalUSDRevenue,
		"currency_breakdown":      currencyBreakdown,
		"total_revenue_local":     stats.TotalRevenue, // Legacy field for backward compatibility
		"total_tickets_sold":      stats.TotalTicketsSold,
		"total_commission_amount": stats.TotalCommissionAmount,
		"organizer_earnings":      stats.OrganizerEarnings,
		// Financial Payment Metrics
		"total_amount_received": financialMetrics.TotalReceived, // Amount already paid to organizer
		"total_pending_amount":  totalPendingAmount,             // Amount still due to organizer
		"upcoming_events":       len(upcomingEventsResponse),
		"upcoming_list":         upcomingEventsResponse,
	}

	utils.SuccessResponse(c, http.StatusOK, "Organizer dashboard data retrieved successfully", dashboardData)
}

// GetUserDashboard godoc
// @Summary Get user dashboard data
// @Description Get dashboard statistics and upcoming events for regular users
// @Tags Dashboard
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 500 {object} utils.Response
// @Router /api/v1/user/dashboard [get]
func (h *DashboardHandler) GetUserDashboard(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewInternalServerError("An error occurred.", nil))
		return
	}

	userUUID := userID.(uuid.UUID)

	// Single optimized query for user statistics
	var stats struct {
		TotalTicketsPurchased int64   `json:"total_tickets_purchased"`
		TotalAmountSpent      float64 `json:"total_amount_spent"`
		ActiveTickets         int64   `json:"active_tickets"`
		UsedTickets           int64   `json:"used_tickets"`
	}

	// Get user stats in one query
	database.GetDB().Raw(`
		SELECT
			COALESCE(SUM(quantity), 0) as total_tickets_purchased,
			COALESCE(SUM(total_amount), 0) as total_amount_spent,
			COUNT(*) FILTER (WHERE status = 'active') as active_tickets,
			COUNT(*) FILTER (WHERE status = 'used') as used_tickets
		FROM tickets
		WHERE user_id = ? AND deleted_at IS NULL
	`, userUUID).Scan(&stats)

	// Get upcoming events (limit to 3 months from now)
	now := time.Now()
	threeMonthsFromNow := now.AddDate(0, 3, 0)
	upcomingEventsResponse := []models.EventPublicSummaryResponse{}

	database.GetDB().Model(&models.Event{}).
		Select("id, title, banner_image, category, start_date, end_date, status, sales_status, is_featured, venue_name, created_at, updated_at").
		Where("start_date > ? AND start_date <= ? AND status IN (?)", now, threeMonthsFromNow, []string{"scheduled", "on_sale", "approved"}).
		Order("is_featured DESC, start_date ASC").
		Limit(12).
		Scan(&upcomingEventsResponse)

	dashboardData := map[string]interface{}{
		"total_tickets_purchased": stats.TotalTicketsPurchased,
		"total_amount_spent":      stats.TotalAmountSpent,
		"active_tickets":          stats.ActiveTickets,
		"used_tickets":            stats.UsedTickets,
		"upcoming_events_count":   len(upcomingEventsResponse),
		"upcoming_events":         upcomingEventsResponse,
	}

	utils.SuccessResponse(c, http.StatusOK, "User dashboard data retrieved successfully", dashboardData)
}

// GetOrganizerFinancialDashboard godoc
// @Summary Get organizer financial dashboard
// @Description Get financial analytics for organizer showing sales, refunds, and net revenue in event currency
// @Tags Dashboard
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 500 {object} utils.Response
// @Router /api/v1/organizer/financial-dashboard [get]
func (h *DashboardHandler) GetOrganizerFinancialDashboard(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		utils.HandleError(c, utils.NewUnauthorizedError("User not authenticated"))
		return
	}

	userUUID := userID.(uuid.UUID)

	// Get organizer's events and their financial data
	var financialData struct {
		TotalEvents   int64   `json:"total_events"`
		TotalSales    float64 `json:"total_sales"`
		TotalRefunds  float64 `json:"total_refunds"`
		NetRevenue    float64 `json:"net_revenue"`
		EventCurrency string  `json:"event_currency"`
	}

	// Get financial summary for organizer's events (in event currency)
	database.GetDB().Raw(`
		WITH organizer_events AS (
			SELECT id, currency
			FROM events
			WHERE organizer_id = ? AND deleted_at IS NULL
		),
		event_financials AS (
			SELECT
				oe.currency as event_currency,
				COALESCE(SUM(t.amount) FILTER (WHERE t.status = 'completed'), 0) as sales,
				COALESCE(SUM(r.amount) FILTER (WHERE r.status = 'completed'), 0) as refunds
			FROM organizer_events oe
			LEFT JOIN transactions t ON t.event_id = oe.id AND t.status = 'completed'
			LEFT JOIN refunds r ON r.transaction_id = t.id AND r.status = 'completed'
			GROUP BY oe.currency
		)
		SELECT
			COUNT(DISTINCT oe.id) as total_events,
			COALESCE(SUM(ef.sales), 0) as total_sales,
			COALESCE(SUM(ef.refunds), 0) as total_refunds,
			COALESCE(SUM(ef.sales - ef.refunds), 0) as net_revenue,
			STRING_AGG(DISTINCT ef.event_currency, ', ') as event_currency
		FROM organizer_events oe
		LEFT JOIN event_financials ef ON ef.event_currency = oe.currency
	`, userUUID).Scan(&financialData)

	// Get per-event breakdown
	type EventFinancials struct {
		EventID       uuid.UUID `json:"event_id"`
		EventTitle    string    `json:"event_title"`
		EventCurrency string    `json:"event_currency"`
		Sales         float64   `json:"sales"`
		Refunds       float64   `json:"refunds"`
		NetRevenue    float64   `json:"net_revenue"`
		TicketsSold   int64     `json:"tickets_sold"`
	}

	var eventBreakdown []EventFinancials
	database.GetDB().Raw(`
		SELECT
			e.id as event_id,
			e.title as event_title,
			e.currency as event_currency,
			COALESCE(SUM(t.amount) FILTER (WHERE t.status = 'completed'), 0) as sales,
			COALESCE(SUM(r.amount) FILTER (WHERE r.status = 'completed'), 0) as refunds,
			COALESCE(SUM(t.amount) FILTER (WHERE t.status = 'completed'), 0) -
			COALESCE(SUM(r.amount) FILTER (WHERE r.status = 'completed'), 0) as net_revenue,
			COALESCE(SUM(t.quantity) FILTER (WHERE t.status = 'completed'), 0) as tickets_sold
		FROM events e
		LEFT JOIN transactions t ON t.event_id = e.id AND t.status = 'completed'
		LEFT JOIN refunds r ON r.transaction_id = t.id AND r.status = 'completed'
		WHERE e.organizer_id = ? AND e.deleted_at IS NULL
		GROUP BY e.id, e.title, e.currency
		ORDER BY e.created_at DESC
	`, userUUID).Scan(&eventBreakdown)

	dashboardData := map[string]interface{}{
		"summary": map[string]interface{}{
			"total_events":   financialData.TotalEvents,
			"total_sales":    financialData.TotalSales,
			"total_refunds":  financialData.TotalRefunds,
			"net_revenue":    financialData.NetRevenue,
			"event_currency": financialData.EventCurrency,
		},
		"events": eventBreakdown,
	}

	utils.SuccessResponse(c, http.StatusOK, "Organizer financial dashboard retrieved successfully", dashboardData)
}

// GetAdminFinancialDashboard godoc
// @Summary Get admin financial dashboard
// @Description Get global financial analytics aggregated in USD (base currency)
// @Tags Dashboard
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} utils.Response{data=map[string]interface{}}
// @Failure 500 {object} utils.Response
// @Router /api/v1/admin/financial-dashboard [get]
func (h *DashboardHandler) GetAdminFinancialDashboard(c *gin.Context) {
	var globalFinancials struct {
		TotalSalesUSD      float64 `json:"total_sales_usd"`
		TotalRefundsUSD    float64 `json:"total_refunds_usd"`
		NetRevenueUSD      float64 `json:"net_revenue_usd"`
		TotalCommissionUSD float64 `json:"total_commission_usd"`
		TotalPayoutsUSD    float64 `json:"total_payouts_usd"`
		TotalTicketsSold   int64   `json:"total_tickets_sold"`
		TotalEvents        int64   `json:"total_events"`
		TotalOrganizers    int64   `json:"total_organizers"`
	}

	// Get global financial data aggregated in USD
	database.GetDB().Raw(`
		WITH transaction_financials AS (
			SELECT
				COALESCE(SUM(t.amount_base) FILTER (WHERE t.status = 'completed'), 0) as sales_usd,
				COALESCE(SUM(r.amount_base) FILTER (WHERE r.status = 'completed'), 0) as refunds_usd,
				COALESCE(SUM(t.commission_amount), 0) as commission_usd,
				COALESCE(SUM(t.quantity) FILTER (WHERE t.status = 'completed'), 0) as tickets_sold
			FROM transactions t
			LEFT JOIN refunds r ON r.transaction_id = t.id AND r.status = 'completed'
		),
		payout_financials AS (
			SELECT
				COALESCE(SUM(pb.amount) FILTER (WHERE pb.status = 'paid'), 0) as payouts_usd
			FROM payment_bills pb
		),
		event_organizer_counts AS (
			SELECT
				COUNT(DISTINCT e.id) as total_events,
				COUNT(DISTINCT e.organizer_id) as total_organizers
			FROM events e
			WHERE e.deleted_at IS NULL
		)
		SELECT
			tf.sales_usd,
			tf.refunds_usd,
			(tf.sales_usd - tf.refunds_usd) as net_revenue_usd,
			tf.commission_usd,
			pf.payouts_usd,
			tf.tickets_sold,
			eoc.total_events,
			eoc.total_organizers
		FROM transaction_financials tf
		CROSS JOIN payout_financials pf
		CROSS JOIN event_organizer_counts eoc
	`).Scan(&globalFinancials)

	// Get currency breakdown
	type CurrencyBreakdown struct {
		Currency     string  `json:"currency"`
		SalesLocal   float64 `json:"sales_local"`
		RefundsLocal float64 `json:"refunds_local"`
		NetLocal     float64 `json:"net_local"`
		SalesUSD     float64 `json:"sales_usd"`
		RefundsUSD   float64 `json:"refunds_usd"`
		NetUSD       float64 `json:"net_usd"`
		EventCount   int64   `json:"event_count"`
	}

	var currencyStats []CurrencyBreakdown
	database.GetDB().Raw(`
		SELECT
			e.currency,
			COALESCE(SUM(t.amount) FILTER (WHERE t.status = 'completed'), 0) as sales_local,
			COALESCE(SUM(r.amount) FILTER (WHERE r.status = 'completed'), 0) as refunds_local,
			COALESCE(SUM(t.amount) FILTER (WHERE t.status = 'completed'), 0) -
			COALESCE(SUM(r.amount) FILTER (WHERE r.status = 'completed'), 0) as net_local,
			COALESCE(SUM(t.amount_base) FILTER (WHERE t.status = 'completed'), 0) as sales_usd,
			COALESCE(SUM(r.amount_base) FILTER (WHERE r.status = 'completed'), 0) as refunds_usd,
			COALESCE(SUM(t.amount_base) FILTER (WHERE t.status = 'completed'), 0) -
			COALESCE(SUM(r.amount_base) FILTER (WHERE r.status = 'completed'), 0) as net_usd,
			COUNT(DISTINCT e.id) as event_count
		FROM events e
		LEFT JOIN transactions t ON t.event_id = e.id AND t.status = 'completed'
		LEFT JOIN refunds r ON r.transaction_id = t.id AND r.status = 'completed'
		WHERE e.deleted_at IS NULL
		GROUP BY e.currency
		ORDER BY sales_usd DESC
	`).Scan(&currencyStats)

	// Debug logging
	log.Printf("[DASHBOARD] Currency stats length: %d", len(currencyStats))
	for i, stat := range currencyStats {
		log.Printf("[DASHBOARD] Currency stat %d: %+v", i, stat)
	}

	// Get top performing events (by revenue)
	type TopEvent struct {
		EventID       uuid.UUID `json:"event_id"`
		EventTitle    string    `json:"event_title"`
		OrganizerName string    `json:"organizer_name"`
		Currency      string    `json:"currency"`
		SalesLocal    float64   `json:"sales_local"`
		SalesUSD      float64   `json:"sales_usd"`
		TicketsSold   int64     `json:"tickets_sold"`
	}

	var topEvents []TopEvent
	database.GetDB().Raw(`
		SELECT
			e.id as event_id,
			e.title as event_title,
			u.first_name || ' ' || u.last_name as organizer_name,
			e.currency,
			COALESCE(SUM(t.amount) FILTER (WHERE t.status = 'completed'), 0) as sales_local,
			COALESCE(SUM(t.amount_base) FILTER (WHERE t.status = 'completed'), 0) as sales_usd,
			COALESCE(SUM(t.quantity) FILTER (WHERE t.status = 'completed'), 0) as tickets_sold
		FROM events e
		JOIN users u ON u.id = e.organizer_id
		LEFT JOIN transactions t ON t.event_id = e.id AND t.status = 'completed'
		WHERE e.deleted_at IS NULL
		GROUP BY e.id, e.title, u.first_name, u.last_name, e.currency
		ORDER BY sales_usd DESC
		LIMIT 10
	`).Scan(&topEvents)

	dashboardData := map[string]interface{}{
		"global_summary": map[string]interface{}{
			"total_sales_usd":      globalFinancials.TotalSalesUSD,
			"total_refunds_usd":    globalFinancials.TotalRefundsUSD,
			"net_revenue_usd":      globalFinancials.NetRevenueUSD,
			"total_commission_usd": globalFinancials.TotalCommissionUSD,
			"total_payouts_usd":    globalFinancials.TotalPayoutsUSD,
			"total_tickets_sold":   globalFinancials.TotalTicketsSold,
			"total_events":         globalFinancials.TotalEvents,
			"total_organizers":     globalFinancials.TotalOrganizers,
		},
		"currency_breakdown": currencyStats,
		"top_events":         topEvents,
	}

	utils.SuccessResponse(c, http.StatusOK, "Admin financial dashboard retrieved successfully", dashboardData)
}
