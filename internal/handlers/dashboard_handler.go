package handlers

import (
	"net/http"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/currency"
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
		TotalRefunds           float64 `json:"total_refunds"`
		NetRevenue             float64 `json:"net_revenue"`
		TotalCommission        float64 `json:"total_commission"`
		TotalCommissionRefunds float64 `json:"total_commission_refunds"`
		NetCommission          float64 `json:"net_commission"`
		TotalOrganizerShare    float64 `json:"total_organizer_share"`
		TotalOrganizerRefunds  float64 `json:"total_organizer_refunds"`
		NetOrganizerShare      float64 `json:"net_organizer_share"`

		// Refunds
		CompletedRefunds  int64 `json:"completed_refunds"`
		PendingRefunds    int64 `json:"pending_refunds"`
		FailedRefunds     int64 `json:"failed_refunds"`
		ProcessingRefunds int64 `json:"processing_refunds"`

		// Tickets
		TotalTicketsSold int64 `json:"total_tickets_sold"`
		ActiveTickets    int64 `json:"active_tickets"`
		UsedTickets      int64 `json:"used_tickets"`
		CancelledTickets int64 `json:"cancelled_tickets"`

		// Payments
		TotalPaymentBills  int64   `json:"total_payment_bills"`
		PaidBills          int64   `json:"paid_bills"`
		PendingBills       int64   `json:"pending_bills"`
		PartiallyPaidBills int64   `json:"partially_paid_bills"`
		CancelledBills     int64   `json:"cancelled_bills"`
		OverdueBills       int64   `json:"overdue_bills"`
		TotalPaidOut       float64 `json:"total_paid_out"`
		TotalAmountDue     float64 `json:"total_amount_due"`

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
	if err := database.GetDB().Raw(`
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
				COUNT(*) FILTER (WHERE status = 'succeeded') as completed_trans,
				COUNT(*) FILTER (WHERE status = 'pending') as pending_trans,
				COUNT(*) FILTER (WHERE status = 'failed') as failed_trans,
				COALESCE(SUM(amount_total) FILTER (WHERE status = 'succeeded'), 0) as total_revenue,
				COALESCE(SUM(platform_fee) FILTER (WHERE status = 'succeeded'), 0) as total_commission,
				COALESCE(SUM(organizer_earning) FILTER (WHERE status = 'succeeded'), 0) as total_organizer_share,
				COALESCE(SUM(quantity) FILTER (WHERE status = 'succeeded'), 0) as total_tickets_sold
			FROM transactions
		),
		refund_stats AS (
			SELECT
				COALESCE(SUM(amount) FILTER (WHERE status IN ('succeeded', 'processing')), 0) as total_refunds,
				COALESCE(SUM(commission_refund) FILTER (WHERE status IN ('succeeded', 'processing')), 0) as total_commission_refunds,
				COALESCE(SUM(organizer_refund) FILTER (WHERE status IN ('succeeded', 'processing')), 0) as total_organizer_refunds,
				COUNT(*) FILTER (WHERE status = 'succeeded') as completed_refunds,
				COUNT(*) FILTER (WHERE status = 'pending') as pending_refunds,
				COUNT(*) FILTER (WHERE status = 'failed') as failed_refunds,
				COUNT(*) FILTER (WHERE status = 'processing') as processing_refunds
			FROM refunds
		),
		ticket_stats AS (
			SELECT
				COUNT(*) FILTER (WHERE status = 'active') as active_tickets,
				COUNT(*) FILTER (WHERE status = 'used') as used_tickets,
				COUNT(*) FILTER (WHERE status IN ('cancelled', 'expired')) as cancelled_tickets
			FROM tickets
		),
		payment_stats AS (
			SELECT
				COUNT(*) as total_payment_bills,
				COUNT(*) FILTER (WHERE status = 'paid') as paid_bills,
				COUNT(*) FILTER (WHERE status = 'pending') as pending_bills,
				COUNT(*) FILTER (WHERE status = 'partially_paid') as partially_paid_bills,
				COUNT(*) FILTER (WHERE status = 'cancelled') as cancelled_bills,
				COUNT(*) FILTER (WHERE status = 'overdue') as overdue_bills,
				COALESCE(SUM(paid_amount), 0) as total_paid_out,
				COALESCE(SUM(amount - paid_amount) FILTER (WHERE status IN ('pending', 'partially_paid')), 0) as total_amount_due
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
		SELECT * FROM user_stats, event_stats, transaction_stats, refund_stats, ticket_stats, payment_stats, payout_stats
	`, now, threeMonthsFromNow).Scan(&systemStats).Error; err != nil {
		utils.HandleError(c, utils.NewDatabaseError("Failed to load admin dashboard summary.", err))
		return
	}

	// Calculate net values: NetRevenue = GrossRevenue - (Refunds + Discounts + TransactionFees)
	// Currently: Discounts = 0 (not implemented), TransactionFees = TotalCommission
	systemStats.NetRevenue = systemStats.TotalRevenue - (systemStats.TotalRefunds + 0 + systemStats.TotalCommission)
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

	type adminEarningRow struct {
		Currency           string  `json:"currency"`
		Country            string  `json:"country"`
		GrossRevenue       float64 `json:"gross_revenue"`
		NetRevenue         float64 `json:"net_revenue"`
		PlatformCommission float64 `json:"platform_commission"`
		GatewayFee         float64 `json:"gateway_fee"`
		RefundAmount       float64 `json:"refund_amount"`
		PendingPayout      float64 `json:"pending_payout"`
		PaidOut            float64 `json:"paid_out"`
	}
	var adminEarningRows []adminEarningRow
	if err := database.GetDB().Raw(`
		SELECT
			e.currency as currency,
			e.country as country,
			COALESCE(SUM(t.amount_total) FILTER (WHERE t.status = ?), 0) as gross_revenue,
			COALESCE(SUM(t.organizer_earning) FILTER (WHERE t.status = ?), 0) - COALESCE(SUM(r.amount) FILTER (WHERE r.status IN (?, ?)), 0) as net_revenue,
			COALESCE(SUM(t.platform_fee) FILTER (WHERE t.status = ?), 0) as platform_commission,
			COALESCE(SUM(t.gateway_fee) FILTER (WHERE t.status = ?), 0) as gateway_fee,
			COALESCE(SUM(r.amount) FILTER (WHERE r.status IN (?, ?)), 0) as refund_amount,
			COALESCE(SUM(pr.amount) FILTER (WHERE pr.status IN ('pending', 'approved')), 0) as pending_payout,
			COALESCE(SUM(ph.amount), 0) as paid_out
		FROM events e
		LEFT JOIN transactions t ON t.event_id = e.id
		LEFT JOIN refunds r ON r.transaction_id = t.id
		LEFT JOIN payout_requests pr ON pr.event_id = e.id
		LEFT JOIN payment_bills pb ON pb.event_id = e.id
		LEFT JOIN payment_histories ph ON ph.payment_bill_id = pb.id
		WHERE e.deleted_at IS NULL
		GROUP BY e.currency, e.country
		ORDER BY e.currency, e.country
	`, models.TransactionSucceeded, models.TransactionSucceeded, models.RefundSucceeded, models.RefundProcessing, models.TransactionSucceeded, models.TransactionSucceeded, models.RefundSucceeded, models.RefundProcessing).Scan(&adminEarningRows).Error; err != nil {
		utils.HandleError(c, utils.NewDatabaseError("Failed to load admin earnings breakdown.", err))
		return
	}

	earningsByMarket := make([]map[string]interface{}, 0, len(adminEarningRows))
	for _, row := range adminEarningRows {
		metadata := utils.ResolveMoneyMetadata(row.Currency, row.Country)
		grossRevenue := row.GrossRevenue
		netRevenue := row.NetRevenue
		platformCommission := row.PlatformCommission
		gatewayFee := row.GatewayFee
		refundAmount := row.RefundAmount
		pendingPayout := row.PendingPayout
		paidOut := row.PaidOut
		if v, err := currency.FromSmallestUnit(int64(row.GrossRevenue), row.Currency); err == nil {
			grossRevenue = v
		}
		if v, err := currency.FromSmallestUnit(int64(row.NetRevenue), row.Currency); err == nil {
			netRevenue = v
		}
		if v, err := currency.FromSmallestUnit(int64(row.PlatformCommission), row.Currency); err == nil {
			platformCommission = v
		}
		if v, err := currency.FromSmallestUnit(int64(row.GatewayFee), row.Currency); err == nil {
			gatewayFee = v
		}
		if v, err := currency.FromSmallestUnit(int64(row.RefundAmount), row.Currency); err == nil {
			refundAmount = v
		}
		if v, err := currency.FromSmallestUnit(int64(row.PendingPayout), row.Currency); err == nil {
			pendingPayout = v
		}
		if v, err := currency.FromSmallestUnit(int64(row.PaidOut), row.Currency); err == nil {
			paidOut = v
		}
		earningsByMarket = append(earningsByMarket, map[string]interface{}{
			"currency":            metadata.Currency,
			"country":             metadata.Country,
			"currency_symbol":     metadata.CurrencySymbol,
			"symbol":              metadata.CurrencySymbol,
			"gross_revenue":       grossRevenue,
			"net_revenue":         netRevenue,
			"platform_commission": platformCommission,
			"gateway_fee":         gatewayFee,
			"refund_amount":       refundAmount,
			"pending_payout":      pendingPayout,
			"paid_out":            paidOut,
		})
	}

	type adminBillingRow struct {
		Currency       string  `json:"currency"`
		Country        string  `json:"country"`
		TotalBilled    float64 `json:"total_billed"`
		TotalPaid      float64 `json:"total_paid"`
		RemainingTotal float64 `json:"remaining_total"`
	}
	var adminBillingRows []adminBillingRow
	if err := database.GetDB().Raw(`
		SELECT
			e.currency as currency,
			e.country as country,
			COALESCE(SUM(pb.amount), 0) as total_billed,
			COALESCE(SUM(pb.paid_amount), 0) as total_paid,
			COALESCE(SUM(pb.amount - pb.paid_amount), 0) as remaining_total
		FROM payment_bills pb
		INNER JOIN events e ON pb.event_id = e.id
		WHERE e.deleted_at IS NULL
		GROUP BY e.currency, e.country
		ORDER BY e.currency, e.country
	`).Scan(&adminBillingRows).Error; err != nil {
		utils.HandleError(c, utils.NewDatabaseError("Failed to load admin billing breakdown.", err))
		return
	}

	billingsByMarket := make([]map[string]interface{}, 0, len(adminBillingRows))
	for _, row := range adminBillingRows {
		metadata := utils.ResolveMoneyMetadata(row.Currency, row.Country)
		totalBilled := row.TotalBilled
		totalPaid := row.TotalPaid
		remainingTotal := row.RemainingTotal
		if v, err := currency.FromSmallestUnit(int64(row.TotalBilled), row.Currency); err == nil {
			totalBilled = v
		}
		if v, err := currency.FromSmallestUnit(int64(row.TotalPaid), row.Currency); err == nil {
			totalPaid = v
		}
		if v, err := currency.FromSmallestUnit(int64(row.RemainingTotal), row.Currency); err == nil {
			remainingTotal = v
		}
		billingsByMarket = append(billingsByMarket, map[string]interface{}{
			"currency":        metadata.Currency,
			"country":         metadata.Country,
			"currency_symbol": metadata.CurrencySymbol,
			"symbol":          metadata.CurrencySymbol,
			"total_billed":    totalBilled,
			"total_paid":      totalPaid,
			"remaining_total": remainingTotal,
		})
	}

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

		"earnings": earningsByMarket,

		// Refunds Summary
		"refunds": map[string]interface{}{
			"completed":  systemStats.CompletedRefunds,
			"pending":    systemStats.PendingRefunds,
			"failed":     systemStats.FailedRefunds,
			"processing": systemStats.ProcessingRefunds,
		},

		// Tickets Summary
		"tickets": map[string]interface{}{
			"total_sold": systemStats.TotalTicketsSold,
			"active":     systemStats.ActiveTickets,
			"used":       systemStats.UsedTickets,
			"cancelled":  systemStats.CancelledTickets,
		},

		"payment_bills": map[string]interface{}{
			"total":          systemStats.TotalPaymentBills,
			"paid":           systemStats.PaidBills,
			"pending":        systemStats.PendingBills,
			"partially_paid": systemStats.PartiallyPaidBills,
			"cancelled":      systemStats.CancelledBills,
			"overdue":        systemStats.OverdueBills,
		},
		"billings": billingsByMarket,

		// Payout Requests Summary
		"payout_requests": map[string]interface{}{
			"total":     systemStats.TotalPayoutRequests,
			"pending":   systemStats.PendingPayouts,
			"approved":  systemStats.ApprovedPayouts,
			"paid":      systemStats.PaidPayouts,
			"cancelled": systemStats.CancelledPayouts,
			"rejected":  systemStats.RejectedPayouts,
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

	now := time.Now()
	var eventStats struct {
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
	}

	threeMonthsFromNow := now.AddDate(0, 3, 0)
	if err := database.GetDB().Raw(`
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
			COUNT(*) FILTER (WHERE start_date > ? AND start_date <= ? AND status IN ('on_sale', 'approved') AND deleted_at IS NULL) as upcoming_events
		FROM events
		WHERE organizer_id = ?
	`, now, threeMonthsFromNow, organizerID).Scan(&eventStats).Error; err != nil {
		utils.HandleError(c, utils.NewDatabaseError("Failed to load organizer event stats.", err))
		return
	}

	var ticketStats struct {
		TotalSold int64 `json:"total_sold"`
		Active    int64 `json:"active"`
		Used      int64 `json:"used"`
		Cancelled int64 `json:"cancelled"`
		Refunded  int64 `json:"refunded"`
	}
	if err := database.GetDB().Raw(`
		SELECT
			COUNT(*) as total_sold,
			COUNT(*) FILTER (WHERE t.status = ?) as active,
			COUNT(*) FILTER (WHERE t.status = ?) as used,
			COUNT(*) FILTER (WHERE t.status = ?) as cancelled,
			COUNT(*) FILTER (WHERE t.status = ?) as refunded
		FROM tickets t
		INNER JOIN events e ON t.event_id = e.id
		WHERE e.organizer_id = ? AND t.deleted_at IS NULL
	`, models.TicketActive, models.TicketUsed, models.TicketCanceled, models.TicketRefunded, organizerID).Scan(&ticketStats).Error; err != nil {
		utils.HandleError(c, utils.NewDatabaseError("Failed to load organizer ticket stats.", err))
		return
	}

	var transactionStats struct {
		Total      int64 `json:"total"`
		Pending    int64 `json:"pending"`
		Processing int64 `json:"processing"`
		Completed  int64 `json:"completed"`
		Failed     int64 `json:"failed"`
		Refunded   int64 `json:"refunded"`
	}
	if err := database.GetDB().Raw(`
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE t.status = ?) as pending,
			COUNT(*) FILTER (WHERE t.status = ?) as processing,
			COUNT(*) FILTER (WHERE t.status = ?) as completed,
			COUNT(*) FILTER (WHERE t.status = ?) as failed,
			COUNT(*) FILTER (WHERE t.status = ?) as refunded
		FROM transactions t
		INNER JOIN events e ON t.event_id = e.id
		WHERE e.organizer_id = ? AND t.deleted_at IS NULL
	`, models.TransactionPending, models.TransactionProcessing, models.TransactionSucceeded, models.TransactionFailed, models.TransactionRefunded, organizerID).Scan(&transactionStats).Error; err != nil {
		utils.HandleError(c, utils.NewDatabaseError("Failed to load organizer transaction stats.", err))
		return
	}

	var refundStats struct {
		Pending    int64 `json:"pending"`
		Processing int64 `json:"processing"`
		Completed  int64 `json:"completed"`
		Failed     int64 `json:"failed"`
	}
	if err := database.GetDB().Raw(`
		SELECT
			COUNT(*) FILTER (WHERE r.status = ?) as pending,
			COUNT(*) FILTER (WHERE r.status = ?) as processing,
			COUNT(*) FILTER (WHERE r.status = ?) as completed,
			COUNT(*) FILTER (WHERE r.status = ?) as failed
		FROM refunds r
		INNER JOIN events e ON r.event_id = e.id
		WHERE e.organizer_id = ?
	`, models.RefundPending, models.RefundProcessing, models.RefundSucceeded, models.RefundFailed, organizerID).Scan(&refundStats).Error; err != nil {
		utils.HandleError(c, utils.NewDatabaseError("Failed to load organizer refund stats.", err))
		return
	}

	type earningRow struct {
		Currency           string  `json:"currency"`
		Country            string  `json:"country"`
		GrossRevenue       float64 `json:"gross_revenue"`
		NetRevenue         float64 `json:"net_revenue"`
		PlatformCommission float64 `json:"platform_commission"`
		GatewayFee         float64 `json:"gateway_fee"`
		RefundAmount       float64 `json:"refund_amount"`
		PendingPayout      float64 `json:"pending_payout"`
		PaidOut            float64 `json:"paid_out"`
	}
	var earningRows []earningRow
	selectedEventID := c.Query("event_id")
	earningsQuery := `
		SELECT
			e.currency as currency,
			e.country as country,
			COALESCE(SUM(t.amount_total) FILTER (WHERE t.status = ?), 0) as gross_revenue,
			COALESCE(SUM(t.organizer_earning) FILTER (WHERE t.status = ?), 0) - COALESCE(SUM(r.amount) FILTER (WHERE r.status IN (?, ?)), 0) as net_revenue,
			COALESCE(SUM(t.platform_fee) FILTER (WHERE t.status = ?), 0) as platform_commission,
			COALESCE(SUM(t.gateway_fee) FILTER (WHERE t.status = ?), 0) as gateway_fee,
			COALESCE(SUM(r.amount) FILTER (WHERE r.status IN (?, ?)), 0) as refund_amount,
			COALESCE(SUM(pr.amount) FILTER (WHERE pr.status IN ('pending', 'approved')), 0) as pending_payout,
			COALESCE(SUM(ph.amount), 0) as paid_out
		FROM events e
		LEFT JOIN transactions t ON t.event_id = e.id
		LEFT JOIN refunds r ON r.transaction_id = t.id
		LEFT JOIN payout_requests pr ON pr.event_id = e.id
		LEFT JOIN payment_bills pb ON pb.event_id = e.id
		LEFT JOIN payment_histories ph ON ph.payment_bill_id = pb.id
		WHERE e.organizer_id = ?
	`
	args := []interface{}{
		models.TransactionSucceeded,
		models.TransactionSucceeded,
		models.RefundSucceeded,
		models.RefundProcessing,
		models.TransactionSucceeded,
		models.TransactionSucceeded,
		models.RefundSucceeded,
		models.RefundProcessing,
		organizerID,
	}
	if selectedEventID != "" {
		earningsQuery += " AND e.id = ?"
		args = append(args, selectedEventID)
	}
	earningsQuery += `
		GROUP BY e.currency, e.country
		ORDER BY e.currency, e.country
	`
	if err := database.GetDB().Raw(earningsQuery, args...).Scan(&earningRows).Error; err != nil {
		utils.HandleError(c, utils.NewDatabaseError("Failed to load organizer earnings.", err))
		return
	}

	earnings := make([]map[string]interface{}, 0, len(earningRows))
	for _, row := range earningRows {
		metadata := utils.ResolveMoneyMetadata(row.Currency, row.Country)
		grossRevenue := row.GrossRevenue
		netRevenue := row.NetRevenue
		platformCommission := row.PlatformCommission
		gatewayFee := row.GatewayFee
		refundAmount := row.RefundAmount
		pendingPayout := row.PendingPayout
		paidOut := row.PaidOut
		if v, err := currency.FromSmallestUnit(int64(row.GrossRevenue), row.Currency); err == nil {
			grossRevenue = v
		}
		if v, err := currency.FromSmallestUnit(int64(row.NetRevenue), row.Currency); err == nil {
			netRevenue = v
		}
		if v, err := currency.FromSmallestUnit(int64(row.PlatformCommission), row.Currency); err == nil {
			platformCommission = v
		}
		if v, err := currency.FromSmallestUnit(int64(row.GatewayFee), row.Currency); err == nil {
			gatewayFee = v
		}
		if v, err := currency.FromSmallestUnit(int64(row.RefundAmount), row.Currency); err == nil {
			refundAmount = v
		}
		if v, err := currency.FromSmallestUnit(int64(row.PendingPayout), row.Currency); err == nil {
			pendingPayout = v
		}
		if v, err := currency.FromSmallestUnit(int64(row.PaidOut), row.Currency); err == nil {
			paidOut = v
		}
		earnings = append(earnings, map[string]interface{}{
			"currency":            metadata.Currency,
			"country":             metadata.Country,
			"currency_symbol":     metadata.CurrencySymbol,
			"symbol":              metadata.CurrencySymbol,
			"gross_revenue":       grossRevenue,
			"net_revenue":         netRevenue,
			"platform_commission": platformCommission,
			"gateway_fee":         gatewayFee,
			"refund_amount":       refundAmount,
			"pending_payout":      pendingPayout,
			"paid_out":            paidOut,
		})
	}

	// Get upcoming events in single query with only needed fields - limit to 3 months
	upcomingEventsResponse := []models.EventPublicSummaryResponse{}

	database.GetDB().Model(&models.Event{}).
		Select("id, title, banner_image, category, start_date, end_date, status, sales_status, is_featured, venue_name, created_at, updated_at").
		Where("organizer_id = ? AND start_date > ? AND start_date <= ? AND status IN (?)", organizerID, now, threeMonthsFromNow, []string{"on_sale", "approved"}).
		Order("is_featured DESC, start_date ASC").
		Limit(12).
		Scan(&upcomingEventsResponse)

	var selectedEvent struct {
		ID       uuid.UUID `json:"id"`
		Title    string    `json:"title"`
		Currency string    `json:"currency"`
		Country  string    `json:"country"`
	}
	var selectedEventData interface{}
	if selectedEventID != "" {
		if err := database.GetDB().Model(&models.Event{}).
			Select("id, title, currency, country").
			Where("id = ? AND organizer_id = ?", selectedEventID, organizerID).
			Take(&selectedEvent).Error; err != nil {
			utils.HandleError(c, utils.NewDatabaseError("Failed to load selected event.", err))
			return
		}
		selectedMoney := utils.ResolveMoneyMetadata(selectedEvent.Currency, selectedEvent.Country)
		selectedEventData = map[string]interface{}{
			"id":       selectedEvent.ID,
			"title":    selectedEvent.Title,
			"currency": selectedMoney.Currency,
			"country":  selectedMoney.Country,
			"symbol":   selectedMoney.CurrencySymbol,
		}
	}

	dashboardData := map[string]interface{}{
		"selected_event": selectedEventData,
		"events": map[string]interface{}{
			"total":     eventStats.TotalEvents,
			"draft":     eventStats.DraftEvents,
			"pending":   eventStats.PendingEvents,
			"approved":  eventStats.ApprovedEvents,
			"rejected":  eventStats.RejectedEvents,
			"on_sale":   eventStats.OnSaleEvents,
			"live":      eventStats.LiveEvents,
			"completed": eventStats.CompletedEvents,
			"cancelled": eventStats.CancelledEvents,
			"upcoming":  eventStats.UpcomingEvents,
		},
		"tickets": map[string]interface{}{
			"total_sold": ticketStats.TotalSold,
			"active":     ticketStats.Active,
			"used":       ticketStats.Used,
			"cancelled":  ticketStats.Cancelled,
			"refunded":   ticketStats.Refunded,
		},
		"earnings":        earnings,
		"upcoming_events": upcomingEventsResponse,
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
