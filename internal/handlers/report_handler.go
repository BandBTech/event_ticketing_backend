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

type ReportHandler struct{}

func NewReportHandler() *ReportHandler { return &ReportHandler{} }

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
		if p, err := time.Parse("2006-01-02", v); err == nil {
			s = p
		}
	}
	if v := c.Query("end_date"); v != "" {
		if p, err := time.Parse("2006-01-02", v); err == nil {
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
	case "customer_analytics":
		utils.SuccessResponse(c, http.StatusOK, "Customer analytics", h.customerAnalytics(f))
	case "financial":
		utils.SuccessResponse(c, http.StatusOK, "Financial", h.financialReport(f))
	case "event_performance":
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
			"Allowed: overview, sales, customer_analytics, financial, event_performance", nil)
	}
}

// ── Scope ──

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

// ── Helpers ──

func (h *ReportHandler) dailySales(f filters) []models.DailySaleRecord {
	db := database.GetDB()
	j, w, a := orgScope(f)
	rr := make([]models.DailySaleRecord, 0)
	db.Raw(`
		SELECT TO_CHAR(t.created_at,'YYYY-MM-DD') as date, t.currency,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.amount_total END),0) as revenue,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.quantity END),0) as tickets_sold,
			COUNT(*) as transactions
		FROM transactions t `+j+` WHERE `+w+`
		GROUP BY DATE(t.created_at), t.currency
		ORDER BY DATE(t.created_at), t.currency`,
		append([]interface{}{models.TransactionSucceeded, models.TransactionSucceeded}, a...)...).Scan(&rr)
	return rr
}

func (h *ReportHandler) currencySnapshots(f filters) []models.CurrencySnapshot {
	db := database.GetDB()
	j, w, a := orgScope(f)
	rr := make([]models.CurrencySnapshot, 0)
	db.Raw(`
		SELECT t.currency,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.amount_total END),0) as gross,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.platform_fee END),0) as fees,
			COALESCE(SUM(CASE WHEN ref.status=? THEN ref.amount END),0) as refunds,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.amount_total END),0)
			 - COALESCE(SUM(CASE WHEN ref.status=? THEN ref.amount END),0) as net,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.quantity END),0) as tickets_sold,
			COUNT(DISTINCT t.id) as transactions
		FROM transactions t `+j+`
		LEFT JOIN refunds ref ON ref.transaction_id = t.id
		WHERE `+w+`
		GROUP BY t.currency ORDER BY t.currency`,
		append([]interface{}{
			models.TransactionSucceeded, models.TransactionSucceeded,
			models.RefundSucceeded,
			models.TransactionSucceeded, models.RefundSucceeded,
			models.TransactionSucceeded,
		}, a...)...).Scan(&rr)
	return rr
}

// ── 1. Overview ──

func (h *ReportHandler) adminOverview(f filters) *models.AdminOverviewReport {
	db := database.GetDB()
	var totalEv, activeEv, totalOrg int64
	db.Raw(`SELECT COUNT(*) FROM events WHERE deleted_at IS NULL`).Scan(&totalEv)
	db.Raw(`SELECT COUNT(*) FROM events WHERE status IN (?,?) AND deleted_at IS NULL`, models.EventStatusOnSale, models.EventStatusLive).Scan(&activeEv)
	db.Raw(`SELECT COUNT(*) FROM users WHERE organizer_status='approved' AND deleted_at IS NULL`).Scan(&totalOrg)
	return &models.AdminOverviewReport{
		Events:      map[string]int64{"total": totalEv, "active": activeEv},
		Organizers:  map[string]int64{"total": totalOrg},
		Currencies:  h.currencySnapshots(f),
		Trend:       h.dailySales(f),
	}
}

func (h *ReportHandler) organizerOverview(f filters) *models.OrganizerOverviewReport {
	db := database.GetDB()
	var totalEv, activeEv int64
	db.Raw(`SELECT COUNT(*) FROM events WHERE organizer_id=? AND deleted_at IS NULL`, *f.orgID).Scan(&totalEv)
	db.Raw(`SELECT COUNT(*) FROM events WHERE organizer_id=? AND status IN (?,?) AND deleted_at IS NULL`, *f.orgID, models.EventStatusOnSale, models.EventStatusLive).Scan(&activeEv)
	return &models.OrganizerOverviewReport{
		Events:     map[string]int64{"total": totalEv, "active": activeEv},
		Currencies: h.currencySnapshots(f),
		Trend:      h.dailySales(f),
	}
}

// ── 2. Sales ──

func (h *ReportHandler) salesReport(f filters) *models.SalesReport {
	return &models.SalesReport{
		Currencies: h.currencySnapshots(f),
		DailySales: h.dailySales(f),
	}
}

// ── 3. Customer Analytics ──

func (h *ReportHandler) customerAnalytics(f filters) *models.CustomerAnalyticsReport {
	db := database.GetDB()
	j, w, a := orgScope(f)

	// user counts
	var totalUsers, activeUsers, inactiveUsers int64
	db.Raw(`SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&totalUsers)
	db.Raw(`SELECT COUNT(*) FROM users WHERE account_status='active' AND deleted_at IS NULL`).Scan(&activeUsers)
	db.Raw(`SELECT COUNT(*) FROM users WHERE account_status='inactive' AND deleted_at IS NULL`).Scan(&inactiveUsers)

	// guest counts
	var totalGuests int64
	db.Raw(`SELECT COUNT(*) FROM guest_users`).Scan(&totalGuests)

	// total unique customers via transactions
	var totalCust int64
	db.Raw(`SELECT COUNT(DISTINCT t.actor_id) FROM transactions t `+j+` WHERE t.status=? AND `+w,
		append([]interface{}{models.TransactionSucceeded}, a...)...).Scan(&totalCust)

	var newCust, repeatCust int64
	var custStats struct {
		New    int64
		Repeat int64
	}
	db.Raw(`
		SELECT COUNT(*) FILTER (WHERE first_purchase BETWEEN ? AND ?) as new,
			COUNT(*) FILTER (WHERE first_purchase<? AND last_purchase BETWEEN ? AND ?) as repeat
		FROM (SELECT t.actor_id as cid, MIN(t.created_at) first_purchase, MAX(t.created_at) last_purchase
			FROM transactions t `+j+` WHERE t.status=? AND `+w+` GROUP BY t.actor_id) sub`,
		append([]interface{}{f.start, f.eod, f.start, f.start, f.eod, models.TransactionSucceeded}, a...)...).
		Scan(&custStats)
	newCust = custStats.New
	repeatCust = custStats.Repeat

	rr := 0.0
	if totalCust > 0 {
		rr = float64(repeatCust) / float64(totalCust) * 100
	}

	return &models.CustomerAnalyticsReport{
		Users:          map[string]int64{"total": totalUsers, "active": activeUsers, "inactive": inactiveUsers},
		Guests:         map[string]int64{"total": totalGuests},
		TotalCustomers: totalCust,
		NewCustomers:   newCust,
		RepeatRate:     rr,
	}
}

// ── 4. Financial ──

func (h *ReportHandler) financialReport(f filters) *models.FinancialReport {
	db := database.GetDB()

	// billing stats
	var billingTotal, billingPending, billingPaid int64
	db.Raw(`SELECT COUNT(*) FROM payment_bills WHERE bill_type='payout'`).Scan(&billingTotal)
	db.Raw(`SELECT COUNT(*) FROM payment_bills WHERE bill_type='payout' AND status='pending'`).Scan(&billingPending)
	db.Raw(`SELECT COUNT(*) FROM payment_bills WHERE bill_type='payout' AND status='paid'`).Scan(&billingPaid)

	// payout stats
	var payoutTotal, payoutPending, payoutPaid int64
	pq := `SELECT COUNT(*) FROM payment_bills WHERE bill_type='payout'`
	if f.orgID != nil {
		pq += ` AND organizer_id=`+(*f.orgID).String()
	}
	db.Raw(pq).Scan(&payoutTotal)
	db.Raw(pq+` AND status='pending'`).Scan(&payoutPending)
	db.Raw(pq+` AND status='paid'`).Scan(&payoutPaid)

	// payment method per-currency earnings
	type pmRow struct {
		Gateway  string
		Currency string
		Gross    float64
		Fees     float64
	}
	pmRows := make([]pmRow, 0)
	j, w, a := orgScope(f)
	db.Raw(`
		SELECT t.payment_gateway as gateway, t.currency,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.amount_total END),0) as gross,
			COALESCE(SUM(CASE WHEN t.status=? THEN t.platform_fee END),0) as fees
		FROM transactions t `+j+` WHERE `+w+`
		GROUP BY t.payment_gateway, t.currency ORDER BY t.payment_gateway, t.currency`,
		append([]interface{}{models.TransactionSucceeded, models.TransactionSucceeded}, a...)...).Scan(&pmRows)

	// group into PaymentMethodSummary
	pmMap := make(map[string][]models.PaymentEarning)
	for _, r := range pmRows {
		pmMap[r.Gateway] = append(pmMap[r.Gateway], models.PaymentEarning{
			Currency: r.Currency,
			Gross:    r.Gross,
			Fees:     r.Fees,
			Net:      r.Gross - r.Fees,
		})
	}
	pmSummary := make([]models.PaymentMethodSummary, 0, len(pmMap))
	for name, earnings := range pmMap {
		pmSummary = append(pmSummary, models.PaymentMethodSummary{Name: name, Earnings: earnings})
	}

	return &models.FinancialReport{
		Currencies:     h.currencySnapshots(f),
		Billing:        map[string]interface{}{"total": billingTotal, "pending": billingPending, "paid": billingPaid},
		Payouts:        map[string]interface{}{"total": payoutTotal, "pending": payoutPending, "paid": payoutPaid},
		PaymentMethods: pmSummary,
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

	tiers := make([]models.TierPerformance, 0)
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
		TicketsSold:    r.Sold,
		Revenue:        r.Rev,
		Transactions:   r.Trans,
		SoldPercentage: soldPct,
		CheckedIn:      checkedIn,
		TierBreakdown:  tiers,
	}
}