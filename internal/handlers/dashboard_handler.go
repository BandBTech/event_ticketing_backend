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
	}

	// Get all statistics efficiently using CTEs for better query optimization
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
				COUNT(*) FILTER (WHERE status = 'on_sale' AND deleted_at IS NULL) as on_sale_events,
				COUNT(*) FILTER (WHERE status = 'live' AND deleted_at IS NULL) as live_events,
				COUNT(*) FILTER (WHERE status = 'completed' AND deleted_at IS NULL) as completed_events,
				COUNT(*) FILTER (WHERE is_cancelled = true AND deleted_at IS NULL) as cancelled_events,
				COUNT(*) FILTER (WHERE start_date > $1 AND status IN ('on_sale', 'approved') AND deleted_at IS NULL) as upcoming_events
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
				COALESCE(SUM(bill_amount) FILTER (WHERE status = 'paid'), 0) as total_paid_out,
				COALESCE(SUM(bill_amount) FILTER (WHERE status = 'pending'), 0) as total_amount_due
			FROM payment_bills
		)
		SELECT * FROM user_stats, event_stats, transaction_stats, ticket_stats, payment_stats
	`, now).Scan(&systemStats)

	// Get upcoming events list (only if needed for display)
	upcomingEventsResponse := []models.EventPublicSummaryResponse{}
	database.GetDB().Model(&models.Event{}).
		Select("id, title, banner_image, category, start_date, end_date, status, sales_status, is_featured, venue_name, created_at, updated_at").
		Where("start_date > ? AND status IN (?)", now, []string{"on_sale", "approved"}).
		Order("start_date ASC").
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
		TotalEvents      int64   `json:"total_events_organized"`
		TotalRevenue     float64 `json:"total_revenue"`
		TotalTicketsSold int64   `json:"total_tickets_sold"`
	}

	// Get stats in one query using subquery
	database.GetDB().Raw(`
		SELECT 
			(SELECT COUNT(*) FROM events WHERE organizer_id = ? AND deleted_at IS NULL) as total_events,
			COALESCE(SUM(organizer_share), 0) as total_revenue,
			COALESCE(SUM(quantity), 0) as total_tickets_sold
		FROM transactions 
		LEFT JOIN events ON transactions.event_id = events.id
		WHERE events.organizer_id = ? AND transactions.status = 'completed'
	`, organizerID, organizerID).Scan(&stats)

	// Get upcoming events in single query with only needed fields
	now := time.Now()
	upcomingEventsResponse := []models.EventPublicSummaryResponse{}

	database.GetDB().Model(&models.Event{}).
		Select("id, title, banner_image, category, start_date, end_date, status, sales_status, is_featured, venue_name, created_at, updated_at").
		Where("organizer_id = ? AND start_date > ? AND status IN (?)", organizerID, now, []string{"on_sale", "approved"}).
		Order("start_date ASC").
		Limit(12).
		Scan(&upcomingEventsResponse)

	dashboardData := map[string]interface{}{
		"total_events_organized": stats.TotalEvents,
		"total_revenue":          stats.TotalRevenue,
		"total_tickets_sold":     stats.TotalTicketsSold,
		"upcoming_events":        len(upcomingEventsResponse),
		"upcoming_list":          upcomingEventsResponse,
	}

	utils.SuccessResponse(c, http.StatusOK, "Organizer dashboard data retrieved successfully", dashboardData)
}
