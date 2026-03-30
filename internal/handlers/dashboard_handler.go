package handlers

import (
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
		TotalUsers     int64 `json:"total_users"`
		ActiveUsers    int64 `json:"active_users"`
		InactiveUsers  int64 `json:"inactive_users"`
		SuspendedUsers int64 `json:"suspended_users"`

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
		UpcomingEvents  int64 `json:"upcoming_events"`

		// Transactions & Revenue
		TotalTransactions   int64   `json:"total_transactions"`
		CompletedTrans      int64   `json:"completed_transactions"`
		PendingTrans        int64   `json:"pending_transactions"`
		FailedTrans         int64   `json:"failed_transactions"`
		TotalRevenue        float64 `json:"total_revenue"`
		TotalCommission     float64 `json:"total_commission"`
		TotalOrganizerShare float64 `json:"total_organizer_share"`

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
				COUNT(*) FILTER (WHERE account_status = 'suspended' AND deleted_at IS NULL) as suspended_users,
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
		ticket_stats AS (
			SELECT
				COUNT(*) FILTER (WHERE status = 'active') as active_tickets,
				COUNT(*) FILTER (WHERE status = 'used') as used_tickets,
				COUNT(*) FILTER (WHERE status = 'cancelled') as cancelled_tickets
			FROM tickets
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
		SELECT * FROM user_stats, event_stats, transaction_stats, ticket_stats, payment_stats, payout_stats
	`, now, threeMonthsFromNow).Scan(&systemStats)

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
			"total":     systemStats.TotalUsers,
			"active":    systemStats.ActiveUsers,
			"inactive":  systemStats.InactiveUsers,
			"suspended": systemStats.SuspendedUsers,
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
			"upcoming":  systemStats.UpcomingEvents,
		},

		// Transactions Summary
		"transactions": map[string]interface{}{
			"total":     systemStats.TotalTransactions,
			"completed": systemStats.CompletedTrans,
			"pending":   systemStats.PendingTrans,
			"failed":    systemStats.FailedTrans,
		},

		// Revenue Summary
		"revenue": map[string]interface{}{
			"total_revenue":      systemStats.TotalRevenue,
			"total_commission":   systemStats.TotalCommission,
			"organizer_earnings": systemStats.TotalOrganizerShare,
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
				COALESCE(SUM(t.organizer_share), 0) as organizer_earnings,
				COALESCE(SUM(t.quantity), 0) as total_tickets_sold
			FROM transactions t
			INNER JOIN events e ON t.event_id = e.id
			WHERE e.organizer_id = ? AND t.status = 'completed' AND t.deleted_at IS NULL
		)
		SELECT 
			es.total_events, es.draft_events, es.pending_events, es.approved_events, es.rejected_events,
			es.on_sale_events, es.live_events, es.completed_events, es.cancelled_events,
			ss.total_revenue, ss.organizer_earnings, ss.total_tickets_sold, ss.total_commission_amount
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

	// Calculate total pending amount (earnings not yet received)
	totalPendingAmount := stats.OrganizerEarnings - financialMetrics.TotalReceived

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
		"total_revenue":           stats.TotalRevenue,
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
