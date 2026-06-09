package handlers

import (
	"net/http"
	"strings"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/currency"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ReportHandler struct{}

func NewReportHandler() *ReportHandler { return &ReportHandler{} }

// ── Currency symbol helper ──

func sym(code string) string {
	if cfg, err := currency.Get(code); err == nil {
		return cfg.Symbol
	}
	return code
}

// ── Route handlers ──

func (h *ReportHandler) GetAdminReport(c *gin.Context) {
	h.dispatch(c, parseFilters(c, nil))
}

func (h *ReportHandler) GetOrganizerReport(c *gin.Context) {
	uid, _ := c.Get("userID")
	orgID, err := utils.GetOrganizerIDForUser(database.GetDB(), uid.(uuid.UUID))
	if err != nil {
		utils.ErrorResponse(c, http.StatusForbidden, "Not an organizer", nil)
		return
	}
	h.dispatch(c, parseFilters(c, &orgID))
}

// ── Filters ──

type filters struct {
	start time.Time
	end   time.Time
	eod   time.Time
	orgID *uuid.UUID
}

func parseFilters(c *gin.Context, o *uuid.UUID) filters {
	now := time.Now()
	s, e := now.AddDate(0, -1, 0), now

	if v := c.Query("start_date"); v != "" {
		if p, err := time.Parse(time.RFC3339, v); err == nil {
			s = p
		} else if p, err := time.Parse("2006-01-02", v); err == nil {
			s = p
		}
	}
	if v := c.Query("end_date"); v != "" {
		if p, err := time.Parse(time.RFC3339, v); err == nil {
			e = p
		} else if p, err := time.Parse("2006-01-02", v); err == nil {
			e = p
		}
	}

	return filters{start: s, end: e, eod: e.AddDate(0, 0, 1), orgID: o}
}

func (h *ReportHandler) dispatch(c *gin.Context, f filters) {
	switch c.DefaultQuery("type", "overview") {
	case "overview":
		if f.orgID != nil {
			utils.SuccessResponse(c, http.StatusOK, "Overview", h.organizerOverview(f))
		} else {
			utils.SuccessResponse(c, http.StatusOK, "Overview", h.adminOverview(f))
		}
	case "sales":
		utils.SuccessResponse(c, http.StatusOK, "Sales", h.salesReport(f))
	case "customer-analytics":
		utils.SuccessResponse(c, http.StatusOK, "Customer analytics", h.customerAnalytics(f))
	case "financial":
		utils.SuccessResponse(c, http.StatusOK, "Financial", h.financialReport(f))
	case "event-performance":
		eid := c.Query("event_id")
		if eid == "" {
			utils.ErrorResponse(c, http.StatusBadRequest, "event_id required", nil)
			return
		}
		eventID, err := uuid.Parse(eid)
		if err != nil {
			utils.ErrorResponse(c, http.StatusBadRequest, "Invalid event_id", nil)
			return
		}
		r := h.eventPerformance(eventID, f)
		if r == nil {
			utils.ErrorResponse(c, http.StatusNotFound, "Event not found", nil)
			return
		}
		utils.SuccessResponse(c, http.StatusOK, "Event performance", r)
	default:
		utils.ErrorResponse(c, http.StatusBadRequest,
			"Allowed: overview, sales, customer-analytics, financial, event-performance", nil)
	}
}

// ── Scope (for date-filtered queries) ──

func orgScope(f filters) (join, where string, args []interface{}) {
	args = []interface{}{}
	if f.orgID != nil {
		join = "INNER JOIN events e ON t.event_id = e.id"
		where = "e.organizer_id = ? AND "
		args = append(args, *f.orgID)
	}
	where += "t.created_at BETWEEN ? AND ?"
	args = append(args, f.start, f.eod)
	return
}

// ── saleRows returns per-currency aggregated sales with date filter ──

func (h *ReportHandler) saleRows(f filters) []models.SaleRow {
	db := database.GetDB()
	j, w, a := orgScope(f)
	rr := make([]models.SaleRow, 0)
	db.Raw(`
		SELECT t.currency,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.amount_total ELSE 0 END),0) as gross_revenue,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.amount_total ELSE 0 END),0)
			 - COALESCE(SUM(CASE WHEN ref.status=? THEN ref.amount ELSE 0 END),0) as net_revenue,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.platform_fee ELSE 0 END),0) as platform_fee,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.gateway_fee ELSE 0 END),0) as gateway_fee,
			COALESCE(SUM(CASE WHEN ref.status=? THEN ref.amount ELSE 0 END),0) as refund
		FROM transactions t
		`+j+`
		LEFT JOIN refunds ref ON ref.transaction_id = t.id
		WHERE `+w+`
		GROUP BY t.currency ORDER BY t.currency`,
		append([]interface{}{
			models.TransactionSucceeded, models.TransactionSucceeded,
			models.RefundSucceeded,
			models.TransactionSucceeded, models.TransactionSucceeded,
			models.RefundSucceeded,
		}, a...)...).Scan(&rr)
	for i := range rr {
		rr[i].CurrencySymbol = sym(rr[i].Currency)
	}
	return rr
}

// ── todaySales returns per-currency sales for today only ──

func (h *ReportHandler) todaySales(f filters) []models.DailySaleRow {
	db := database.GetDB()
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayEnd := todayStart.AddDate(0, 0, 1)

	join, where := "", "t.created_at BETWEEN ? AND ?"
	args := []interface{}{todayStart, todayEnd}
	if f.orgID != nil {
		join = "INNER JOIN events e ON t.event_id = e.id"
		where = "e.organizer_id = ? AND " + where
		args = append([]interface{}{*f.orgID}, args...)
	}

	rr := make([]models.DailySaleRow, 0)
	db.Raw(`
		SELECT t.currency,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.amount_total ELSE 0 END),0) as gross_revenue
		FROM transactions t `+join+` WHERE `+where+`
		GROUP BY t.currency ORDER BY t.currency`,
		append([]interface{}{models.TransactionSucceeded}, args...)...).Scan(&rr)
	for i := range rr {
		rr[i].CurrencySymbol = sym(rr[i].Currency)
	}
	return rr
}

// ── financeRows returns per-currency detailed finance with date filter ──

func (h *ReportHandler) financeRows(f filters) []models.FinanceRow {
	db := database.GetDB()
	j, w, a := orgScope(f)
	rr := make([]models.FinanceRow, 0)
	db.Raw(`
		SELECT t.currency,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.amount_total ELSE 0 END),0) as gross_revenue,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.amount_total ELSE 0 END),0)
			 - COALESCE(SUM(CASE WHEN ref.status=? THEN ref.amount ELSE 0 END),0) as net_revenue,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.organizer_share ELSE 0 END),0) as organizer_share,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.platform_fee ELSE 0 END),0) as platform_fee,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.gateway_fee ELSE 0 END),0) as gateway_fee,
			COALESCE(SUM(CASE WHEN ref.status=? THEN ref.amount ELSE 0 END),0) as refund
		FROM transactions t
		`+j+`
		LEFT JOIN refunds ref ON ref.transaction_id = t.id
		WHERE `+w+`
		GROUP BY t.currency ORDER BY t.currency`,
		append([]interface{}{
			models.TransactionSucceeded, models.TransactionSucceeded,
			models.RefundSucceeded,
			models.TransactionSucceeded,
			models.TransactionSucceeded, models.TransactionSucceeded,
			models.RefundSucceeded,
		}, a...)...).Scan(&rr)
	for i := range rr {
		rr[i].CurrencySymbol = sym(rr[i].Currency)
	}
	return rr
}

// ── paymentMethodRows returns per-gateway per-currency earnings ──

func (h *ReportHandler) paymentMethodRows(f filters) []models.PaymentMethodSummary {
	db := database.GetDB()
	j, w, a := orgScope(f)

	type row struct {
		Gateway        string
		Currency       string
		GrossRevenue   float64
		NetRevenue     float64
		OrganizerShare float64
		PlatformFee    float64
		GatewayFee     float64
		Refund         float64
	}
	rows := make([]row, 0)
	db.Raw(`
		SELECT t.payment_gateway as gateway, t.currency,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.amount_total ELSE 0 END),0) as gross_revenue,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.amount_total ELSE 0 END),0)
			 - COALESCE(SUM(CASE WHEN ref.status=? THEN ref.amount ELSE 0 END),0) as net_revenue,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.organizer_share ELSE 0 END),0) as organizer_share,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.platform_fee ELSE 0 END),0) as platform_fee,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.gateway_fee ELSE 0 END),0) as gateway_fee,
			COALESCE(SUM(CASE WHEN ref.status=? THEN ref.amount ELSE 0 END),0) as refund
		FROM transactions t
		`+j+`
		LEFT JOIN refunds ref ON ref.transaction_id = t.id
		WHERE `+w+`
		GROUP BY t.payment_gateway, t.currency ORDER BY t.payment_gateway, t.currency`,
		append([]interface{}{
			models.TransactionSucceeded, models.TransactionSucceeded,
			models.RefundSucceeded,
			models.TransactionSucceeded,
			models.TransactionSucceeded, models.TransactionSucceeded,
			models.RefundSucceeded,
		}, a...)...).Scan(&rows)

	pm := make(map[string][]models.PaymentEarning)
	for _, r := range rows {
		pm[r.Gateway] = append(pm[r.Gateway], models.PaymentEarning{
			Currency:       r.Currency,
			CurrencySymbol: sym(r.Currency),
			GrossRevenue:   r.GrossRevenue,
			NetRevenue:     r.NetRevenue,
			OrganizerShare: r.OrganizerShare,
			PlatformFee:    r.PlatformFee,
			GatewayFee:     r.GatewayFee,
			Refund:         r.Refund,
		})
	}
	out := make([]models.PaymentMethodSummary, 0, len(pm))
	for name, earnings := range pm {
		out = append(out, models.PaymentMethodSummary{Name: name, Earnings: earnings})
	}
	return out
}

// ── 1. Overview ──

func (h *ReportHandler) adminOverview(f filters) *models.AdminOverviewReport {
	db := database.GetDB()

	// Events — all statuses from enum
	events := map[string]int64{}
	for _, s := range []string{
		"draft", "pending", "approved", "scheduled", "sales_upcoming",
		"on_sale", "sales_end", "live", "hold", "held",
		"rejected", "cancel_pending", "cancelled", "completed",
	} {
		var n int64
		db.Raw(`SELECT COUNT(*) FROM events WHERE status=? AND deleted_at IS NULL`, s).Scan(&n)
		events[s] = n
	}
	var totalEv int64
	db.Raw(`SELECT COUNT(*) FROM events WHERE deleted_at IS NULL`).Scan(&totalEv)
	events["total"] = totalEv

	// Users
	var totalUsr, activeUsr, inactiveUsr int64
	db.Raw(`SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&totalUsr)
	db.Raw(`SELECT COUNT(*) FROM users WHERE account_status='active' AND deleted_at IS NULL`).Scan(&activeUsr)
	db.Raw(`SELECT COUNT(*) FROM users WHERE account_status='inactive' AND deleted_at IS NULL`).Scan(&inactiveUsr)

	// Guests
	var totalGuest int64
	db.Raw(`SELECT COUNT(*) FROM guest_users`).Scan(&totalGuest)

	// Organizers
	var orgTotal, orgApproved, orgPending, orgRejected int64
	db.Raw(`SELECT COUNT(*) FROM users WHERE organizer_status IS NOT NULL AND organizer_status != 'inactive' AND deleted_at IS NULL`).Scan(&orgTotal)
	db.Raw(`SELECT COUNT(*) FROM users WHERE organizer_status='approved' AND deleted_at IS NULL`).Scan(&orgApproved)
	db.Raw(`SELECT COUNT(*) FROM users WHERE organizer_status='pending' AND deleted_at IS NULL`).Scan(&orgPending)
	db.Raw(`SELECT COUNT(*) FROM users WHERE organizer_status='rejected' AND deleted_at IS NULL`).Scan(&orgRejected)

	// Transactions
	txns := map[string]int64{}
	for _, s := range []string{"pending", "processing", "succeeded", "failed", "canceled", "expired"} {
		var n int64
		db.Raw(`SELECT COUNT(*) FROM transactions WHERE status=?`, s).Scan(&n)
		txns[s] = n
	}
	var totalTxn int64
	db.Raw(`SELECT COUNT(*) FROM transactions`).Scan(&totalTxn)
	txns["total"] = totalTxn

	// Refunds
	refs := map[string]int64{}
	for _, s := range []string{"pending", "processing", "succeeded", "failed", "cancelled", "rejected"} {
		var n int64
		db.Raw(`SELECT COUNT(*) FROM refunds WHERE status=?`, s).Scan(&n)
		refs[s] = n
	}
	var totalRef int64
	db.Raw(`SELECT COUNT(*) FROM refunds`).Scan(&totalRef)
	refs["total"] = totalRef

	// Billing (all bill types)
	bills := map[string]int64{}
	for _, s := range []string{"pending", "partially_paid", "paid", "cancelled"} {
		var n int64
		db.Raw(`SELECT COUNT(*) FROM payment_bills WHERE status=?`, s).Scan(&n)
		bills[s] = n
	}
	var totalBill int64
	db.Raw(`SELECT COUNT(*) FROM payment_bills`).Scan(&totalBill)
	bills["total"] = totalBill

	// Payouts (bill_type='payout')
	pouts := map[string]int64{}
	for _, s := range []string{"pending", "partially_paid", "paid", "cancelled"} {
		var n int64
		db.Raw(`SELECT COUNT(*) FROM payment_bills WHERE status=? AND bill_type='payout'`, s).Scan(&n)
		pouts[s] = n
	}
	var totalPout int64
	db.Raw(`SELECT COUNT(*) FROM payment_bills WHERE bill_type='payout'`).Scan(&totalPout)
	pouts["total"] = totalPout

	return &models.AdminOverviewReport{
		Events:       events,
		Users:        map[string]int64{"total": totalUsr, "active": activeUsr, "inactive": inactiveUsr},
		Guests:       map[string]int64{"total": totalGuest},
		Organizers:   map[string]int64{"total": orgTotal, "approved": orgApproved, "pending": orgPending, "rejected": orgRejected},
		Transactions: txns,
		Refunds:      refs,
		Billing:      bills,
		Payouts:      pouts,
		Currencies:   h.saleRows(f),
		SalesTrend:   h.todaySales(f),
	}
}

func (h *ReportHandler) organizerOverview(f filters) *models.OrganizerOverviewReport {
	db := database.GetDB()
	oid := *f.orgID

	// Events
	events := map[string]int64{}
	for _, s := range []string{
		"draft", "pending", "approved", "scheduled", "sales_upcoming",
		"on_sale", "sales_end", "live", "hold", "held",
		"rejected", "cancel_pending", "cancelled", "completed",
	} {
		var n int64
		db.Raw(`SELECT COUNT(*) FROM events WHERE status=? AND organizer_id=? AND deleted_at IS NULL`, s, oid).Scan(&n)
		events[s] = n
	}
	var totalEv int64
	db.Raw(`SELECT COUNT(*) FROM events WHERE organizer_id=? AND deleted_at IS NULL`, oid).Scan(&totalEv)
	events["total"] = totalEv

	// Transactions
	txns := map[string]int64{}
	for _, s := range []string{"pending", "processing", "succeeded", "failed", "canceled", "expired"} {
		var n int64
		db.Raw(`SELECT COUNT(*) FROM transactions t INNER JOIN events e ON t.event_id=e.id WHERE t.status=? AND e.organizer_id=?`, s, oid).Scan(&n)
		txns[s] = n
	}
	var totalTxn int64
	db.Raw(`SELECT COUNT(*) FROM transactions t INNER JOIN events e ON t.event_id=e.id WHERE e.organizer_id=?`, oid).Scan(&totalTxn)
	txns["total"] = totalTxn

	// Refunds
	refs := map[string]int64{}
	for _, s := range []string{"pending", "processing", "succeeded", "failed", "cancelled", "rejected"} {
		var n int64
		db.Raw(`SELECT COUNT(*) FROM refunds r INNER JOIN transactions t ON r.transaction_id=t.id INNER JOIN events e ON t.event_id=e.id WHERE r.status=? AND e.organizer_id=?`, s, oid).Scan(&n)
		refs[s] = n
	}
	var totalRef int64
	db.Raw(`SELECT COUNT(*) FROM refunds r INNER JOIN transactions t ON r.transaction_id=t.id INNER JOIN events e ON t.event_id=e.id WHERE e.organizer_id=?`, oid).Scan(&totalRef)
	refs["total"] = totalRef

	// Billing
	bills := map[string]int64{}
	for _, s := range []string{"pending", "partially_paid", "paid", "cancelled"} {
		var n int64
		db.Raw(`SELECT COUNT(*) FROM payment_bills WHERE status=? AND organizer_id=?`, s, oid).Scan(&n)
		bills[s] = n
	}
	var totalBill int64
	db.Raw(`SELECT COUNT(*) FROM payment_bills WHERE organizer_id=?`, oid).Scan(&totalBill)
	bills["total"] = totalBill

	// Payouts
	pouts := map[string]int64{}
	for _, s := range []string{"pending", "partially_paid", "paid", "cancelled"} {
		var n int64
		db.Raw(`SELECT COUNT(*) FROM payment_bills WHERE status=? AND bill_type='payout' AND organizer_id=?`, s, oid).Scan(&n)
		pouts[s] = n
	}
	var totalPout int64
	db.Raw(`SELECT COUNT(*) FROM payment_bills WHERE bill_type='payout' AND organizer_id=?`, oid).Scan(&totalPout)
	pouts["total"] = totalPout

	return &models.OrganizerOverviewReport{
		Events:       events,
		Transactions: txns,
		Refunds:      refs,
		Billing:      bills,
		Payouts:      pouts,
		Currencies:   h.saleRows(f),
		SalesTrend:   h.todaySales(f),
	}
}

// ── 2. Sales ──

func (h *ReportHandler) salesReport(f filters) *models.SalesReport {
	return &models.SalesReport{
		Sales:      h.saleRows(f),
		DailySales: h.todaySales(f),
	}
}

// ── 3. Customer Analytics ──

func (h *ReportHandler) customerAnalytics(f filters) *models.CustomerAnalyticsReport {
	db := database.GetDB()
	j, w, a := orgScope(f)

	var totalUsers, activeUsers, inactiveUsers int64
	db.Raw(`SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&totalUsers)
	db.Raw(`SELECT COUNT(*) FROM users WHERE account_status='active' AND deleted_at IS NULL`).Scan(&activeUsers)
	db.Raw(`SELECT COUNT(*) FROM users WHERE account_status='inactive' AND deleted_at IS NULL`).Scan(&inactiveUsers)

	var totalGuests int64
	db.Raw(`SELECT COUNT(*) FROM guest_users`).Scan(&totalGuests)

	var totalCust int64
	db.Raw(`SELECT COUNT(DISTINCT t.actor_id) FROM transactions t `+j+` WHERE t.status=? AND `+w,
		append([]interface{}{models.TransactionSucceeded}, a...)...).Scan(&totalCust)

	var newCust, repeatCust int64
	var cs struct {
		New    int64
		Repeat int64
	}
	db.Raw(`
		SELECT COUNT(*) FILTER (WHERE first_purchase BETWEEN ? AND ?) as new,
			COUNT(*) FILTER (WHERE first_purchase<? AND last_purchase BETWEEN ? AND ?) as repeat
		FROM (SELECT t.actor_id as cid, MIN(t.created_at) first_purchase, MAX(t.created_at) last_purchase
			FROM transactions t `+j+` WHERE t.status=? AND `+w+` GROUP BY t.actor_id) sub`,
		append([]interface{}{f.start, f.eod, f.start, f.start, f.eod, models.TransactionSucceeded}, a...)...).
		Scan(&cs)
	newCust, repeatCust = cs.New, cs.Repeat

	rr := 0.0
	if totalCust > 0 {
		rr = float64(repeatCust) / float64(totalCust) * 100
	}

	// Top 5 actors by total tickets purchased (grouped by currency + event)
	type actorCurrencyRow struct {
		ActorID          string
		ActorName        string
		ActorEmail       string
		ActorType        string
		Currency         string
		EventTitle       string
		TotalSpent       float64
		TicketsPurchased int64
	}
	acRows := make([]actorCurrencyRow, 0)
	db.Raw(`
		SELECT
			t.actor_id::text as actor_id,
			COALESCE(CASE
				WHEN t.actor_type = 'user' THEN CONCAT(u.first_name, ' ', u.last_name)
				WHEN t.actor_type = 'guest' THEN gu.name
			END, 'Guest') as actor_name,
			COALESCE(CASE
				WHEN t.actor_type = 'user' THEN u.email
				WHEN t.actor_type = 'guest' THEN gu.email
			END, '') as actor_email,
			t.actor_type as actor_type,
			t.currency,
			COALESCE(e.title, 'Unknown') as event_title,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.amount_total ELSE 0 END), 0) as total_spent,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.quantity ELSE 0 END), 0) as tickets_purchased
		FROM transactions t
		LEFT JOIN users u ON t.actor_type = 'user' AND t.actor_id = u.id
		LEFT JOIN guest_users gu ON t.actor_type = 'guest' AND t.actor_id = gu.id
		LEFT JOIN events e ON t.event_id = e.id
		`+j+` WHERE t.status = ? AND `+w+`
		GROUP BY t.actor_id, t.actor_type, t.currency, e.title, u.id, gu.id
		ORDER BY tickets_purchased DESC
		LIMIT 20`,
		append([]interface{}{models.TransactionSucceeded, models.TransactionSucceeded, models.TransactionSucceeded}, a...)...).
		Scan(&acRows)

	// Group into TopActor with per-currency spending and event names
	type actorKey struct {
		id   string
		iso  string // currency
	}
	actorMap := make(map[string]*models.TopActor)
	actorCurrEvents := make(map[actorKey]map[string]struct{}) // actor_key -> currency -> event_names set

	for _, r := range acRows {
		if _, exists := actorMap[r.ActorID]; !exists {
			actorMap[r.ActorID] = &models.TopActor{
				ID:    r.ActorID,
				Name:  r.ActorName,
				Email: r.ActorEmail,
				Type:  r.ActorType,
			}
		}

		// Find or create spending entry for this currency
		ak := actorKey{id: r.ActorID, iso: r.Currency}
		if actorCurrEvents[ak] == nil {
			actorCurrEvents[ak] = make(map[string]struct{})
			actorMap[r.ActorID].Spending = append(actorMap[r.ActorID].Spending, models.ActorSpend{
				Currency:       r.Currency,
				CurrencySymbol: sym(r.Currency),
				TotalSpent:     r.TotalSpent,
				Tickets:        r.TicketsPurchased,
			})
		}
		actorCurrEvents[ak][r.EventTitle] = struct{}{}
		actorMap[r.ActorID].TotalTickets += r.TicketsPurchased
	}

	// Populate event_names for each spending entry
	for _, actor := range actorMap {
		spendIdx := make(map[string]int) // currency -> index in Spending
		for idx, s := range actor.Spending {
			spendIdx[s.Currency] = idx
		}
		for ak, events := range actorCurrEvents {
			if ak.id != actor.ID {
				continue
			}
			names := make([]string, 0, len(events))
			for name := range events {
				names = append(names, name)
			}
			if idx, ok := spendIdx[ak.iso]; ok {
				actor.Spending[idx].EventNames = strings.Join(names, ", ")
			}
		}
	}

	// Sort by total tickets descending and take top 5
	type sorted struct {
		actor   *models.TopActor
		tickets int64
	}
	sortedActors := make([]sorted, 0, len(actorMap))
	for _, a := range actorMap {
		sortedActors = append(sortedActors, sorted{actor: a, tickets: a.TotalTickets})
	}
	for i := 0; i < len(sortedActors); i++ {
		for j := i + 1; j < len(sortedActors); j++ {
			if sortedActors[j].tickets > sortedActors[i].tickets {
				sortedActors[i], sortedActors[j] = sortedActors[j], sortedActors[i]
			}
		}
	}

	topActors := make([]models.TopActor, 0)
	limit := 5
	if len(sortedActors) < limit {
		limit = len(sortedActors)
	}
	for i := 0; i < limit; i++ {
		topActors = append(topActors, *sortedActors[i].actor)
	}

	return &models.CustomerAnalyticsReport{
		Users:          map[string]int64{"total": totalUsers, "active": activeUsers, "inactive": inactiveUsers},
		Guests:         map[string]int64{"total": totalGuests},
		TotalCustomers: totalCust,
		NewCustomers:   newCust,
		RepeatRate:     rr,
		TopActors:      topActors,
	}
}

// ── 4. Financial ──

func (h *ReportHandler) financialReport(f filters) *models.FinancialReport {
	return &models.FinancialReport{
		Finances:       h.financeRows(f),
		PaymentMethods: h.paymentMethodRows(f),
	}
}

// ── 5. Event Performance ──

func (h *ReportHandler) eventPerformance(eventID uuid.UUID, f filters) *models.EventPerformanceReport {
	db := database.GetDB()
	var ev models.Event
	if err := db.First(&ev, "id=?", eventID).Error; err != nil {
		return nil
	}
	if f.orgID != nil && ev.OrganizerID != *f.orgID {
		return nil
	}

	var r struct {
		Sold  int64
		Rev   float64
		Trans int64
	}
	db.Raw(`SELECT COALESCE(SUM(CASE WHEN status=? THEN quantity END),0) sold,
		COALESCE(SUM(CASE WHEN status=? THEN amount_total END),0) rev,
		COUNT(DISTINCT id) trans FROM transactions WHERE event_id=?`,
		models.TransactionSucceeded, models.TransactionSucceeded, eventID).Scan(&r)

	soldPct := 0.0
	if ev.Capacity > 0 {
		soldPct = float64(r.Sold) / float64(ev.Capacity) * 100
	}

	var checkedIn int64
	db.Raw(`SELECT COUNT(DISTINCT tci.ticket_id) FROM ticket_check_ins tci
		INNER JOIN tickets tk ON tk.id=tci.ticket_id WHERE tk.event_id=?`, eventID).Scan(&checkedIn)

	tiers := make([]models.TierPerformRow, 0)
	db.Raw(`
		SELECT et.tier_name, et.quantity as capacity,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.quantity END),0) as sold,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.amount_total END),0) as revenue,
			CASE WHEN et.quantity>0 THEN ROUND((COALESCE(SUM(CASE WHEN t.status=? THEN t.quantity END),0)::numeric/et.quantity)*100,2) ELSE 0 END as sold_pct
		FROM event_tiers et
		LEFT JOIN transactions t ON t.tier_id=et.id AND t.event_id=et.event_id
		WHERE et.event_id=? AND et.deleted_at IS NULL
		GROUP BY et.id,et.tier_name,et.quantity ORDER BY et.sort_order,et.price`,
		models.TransactionSucceeded, models.TransactionSucceeded, models.TransactionSucceeded, eventID).Scan(&tiers)

	return &models.EventPerformanceReport{
		EventID:        eventID.String(),
		EventTitle:     ev.Title,
		Status:         ev.Status,
		Currency:       ev.Currency,
		CurrencySymbol: sym(ev.Currency),
		TicketsSold:    r.Sold,
		Revenue:        r.Rev,
		Transactions:   r.Trans,
		SoldPercentage: soldPct,
		CheckedIn:      checkedIn,
		TierBreakdown:  tiers,
	}
}