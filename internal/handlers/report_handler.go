package handlers

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/currency"
	"event-ticketing-backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ─────────────────────────────────────────────
// Handler
// ─────────────────────────────────────────────

type ReportHandler struct{}

func NewReportHandler() *ReportHandler { return &ReportHandler{} }

// ─────────────────────────────────────────────
// Currency helpers
// ─────────────────────────────────────────────

func sym(code string) string {
	if cfg, err := currency.Get(code); err == nil {
		return cfg.Symbol
	}
	return code
}

func fromSmallest(amount float64, curr string) float64 {
	if v, err := currency.FromSmallestUnit(int64(amount), curr); err == nil {
		return v
	}
	return amount
}

// ─────────────────────────────────────────────
// Filters
// ─────────────────────────────────────────────

type filters struct {
	start    time.Time
	end      time.Time
	eod      time.Time // end + 1 day, used in BETWEEN clauses
	hasRange bool      // true when caller explicitly provided at least one date
	orgID    *uuid.UUID
}

func parseFilters(c *gin.Context, orgID *uuid.UUID) filters {
	now := time.Now()
	f := filters{
		// Default: beginning of time → now (i.e. all-time when no dates given)
		start: time.Date(2000, 1, 1, 0, 0, 0, 0, now.Location()),
		end:   now,
		eod:   now.AddDate(0, 0, 1),
		orgID: orgID,
	}
	if v := c.Query("start_date"); v != "" {
		if p := parseDate(v); !p.IsZero() {
			f.start = p
			f.hasRange = true
		}
	}
	if v := c.Query("end_date"); v != "" {
		if p := parseDate(v); !p.IsZero() {
			f.end = p
			f.eod = p.AddDate(0, 0, 1)
			f.hasRange = true
		}
	}
	return f
}

func parseDate(v string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if p, err := time.Parse(layout, v); err == nil {
			return p
		}
	}
	return time.Time{}
}

// ─────────────────────────────────────────────
// Query scope builder
//
// Returns (JOIN clause, WHERE clause, args) that can be dropped directly
// into any query that aliases transactions as "t".
//
// Always applies the date range. Adds the organizer JOIN when scoped.
// All args are positional so GORM's ? binding works correctly.
// ─────────────────────────────────────────────

type scope struct {
	join  string
	where string
	args  []interface{}
}

// txScope builds the reusable JOIN + WHERE for every transaction-based query.
func txScope(f filters) scope {
	s := scope{}
	if f.orgID != nil {
		s.join = "INNER JOIN events oe ON t.event_id = oe.id"
		s.where = "oe.organizer_id = ? AND t.created_at BETWEEN ? AND ?"
		s.args = []interface{}{*f.orgID, f.start, f.eod}
	} else {
		s.where = "t.created_at BETWEEN ? AND ?"
		s.args = []interface{}{f.start, f.eod}
	}
	return s
}

// applyScope appends the scope's JOIN and WHERE to a *gorm.DB.
// Use this for simple single-table queries.
func applyScope(db *gorm.DB, s scope) *gorm.DB {
	q := db
	if s.join != "" {
		q = q.Joins(s.join)
	}
	return q.Where(s.where, s.args...)
}

// ─────────────────────────────────────────────
// Shared currency conversion helper
// ─────────────────────────────────────────────

func convertSaleRow(r *models.SaleRow) {
	r.CurrencySymbol = sym(r.Currency)
	r.GrossRevenue = fromSmallest(r.GrossRevenue, r.Currency)
	r.NetRevenue = fromSmallest(r.NetRevenue, r.Currency)
	r.PlatformFee = fromSmallest(r.PlatformFee, r.Currency)
	r.GatewayFee = fromSmallest(r.GatewayFee, r.Currency)
	r.Refund = fromSmallest(r.Refund, r.Currency)
}

func convertFinanceRow(r *models.FinanceRow) {
	r.CurrencySymbol = sym(r.Currency)
	r.GrossRevenue = fromSmallest(r.GrossRevenue, r.Currency)
	r.NetRevenue = fromSmallest(r.NetRevenue, r.Currency)
	r.OrganizerShare = fromSmallest(r.OrganizerShare, r.Currency)
	r.PlatformFee = fromSmallest(r.PlatformFee, r.Currency)
	r.GatewayFee = fromSmallest(r.GatewayFee, r.Currency)
	r.Refund = fromSmallest(r.Refund, r.Currency)
}

// ─────────────────────────────────────────────
// Route handlers
// ─────────────────────────────────────────────

// GetAdminReport godoc
// @Summary      Admin report
// @Tags         Reports
// @Security     ApiKeyAuth
// @Param        type       query  string  false  "overview|sales|customer-analytics|financial|event-performance"
// @Param        start_date query  string  false  "YYYY-MM-DD or RFC3339 (default: all-time)"
// @Param        end_date   query  string  false  "YYYY-MM-DD or RFC3339 (default: now)"
// @Param        event_id   query  string  false  "Required for event-performance"
// @Produce      json
// @Success      200  {object}  utils.Response
// @Router       /api/v1/admin/reports [get]
func (h *ReportHandler) GetAdminReport(c *gin.Context) {
	h.dispatch(c, parseFilters(c, nil))
}

// GetOrganizerReport godoc
// @Summary      Organizer report
// @Tags         Reports
// @Security     ApiKeyAuth
// @Param        type       query  string  false  "overview|sales|customer-analytics|financial|event-performance"
// @Param        start_date query  string  false  "YYYY-MM-DD or RFC3339"
// @Param        end_date   query  string  false  "YYYY-MM-DD or RFC3339"
// @Param        event_id   query  string  false  "Required for event-performance"
// @Produce      json
// @Success      200  {object}  utils.Response
// @Router       /api/v1/organizer/reports [get]
func (h *ReportHandler) GetOrganizerReport(c *gin.Context) {
	uid, _ := c.Get("userID")
	orgID, err := utils.GetOrganizerIDForUser(database.GetDB(), uid.(uuid.UUID))
	if err != nil {
		utils.ErrorResponse(c, http.StatusForbidden, "Not an organizer", nil)
		return
	}
	h.dispatch(c, parseFilters(c, &orgID))
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
			utils.ErrorResponse(c, http.StatusNotFound, "Event not found or access denied", nil)
			return
		}
		utils.SuccessResponse(c, http.StatusOK, "Event performance", r)
	default:
		utils.ErrorResponse(c, http.StatusBadRequest,
			"Allowed types: overview, sales, customer-analytics, financial, event-performance", nil)
	}
}

// ─────────────────────────────────────────────
// Shared query functions
// ─────────────────────────────────────────────

// saleRows fetches per-currency aggregated revenue, fees, and refunds.
func (h *ReportHandler) saleRows(f filters) []models.SaleRow {
	s := txScope(f)
	rows := make([]models.SaleRow, 0)
	database.GetDB().Raw(`
		SELECT t.currency,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.amount_total  ELSE 0 END), 0) AS gross_revenue,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.amount_total  ELSE 0 END), 0)
			- COALESCE(SUM(CASE WHEN r.status = ? THEN r.amount       ELSE 0 END), 0) AS net_revenue,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.platform_fee  ELSE 0 END), 0) AS platform_fee,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.gateway_fee   ELSE 0 END), 0) AS gateway_fee,
			COALESCE(SUM(CASE WHEN r.status = ? THEN r.amount        ELSE 0 END), 0) AS refund
		FROM transactions t
		`+s.join+`
		LEFT JOIN refunds r ON r.transaction_id = t.id
		WHERE `+s.where+`
		GROUP BY t.currency
		ORDER BY t.currency`,
		append([]interface{}{
			models.TransactionSucceeded, models.TransactionSucceeded, models.RefundSucceeded,
			models.TransactionSucceeded, models.TransactionSucceeded, models.RefundSucceeded,
		}, s.args...)...,
	).Scan(&rows)

	for i := range rows {
		convertSaleRow(&rows[i])
	}
	return rows
}

// dailySales fetches per-day per-currency gross revenue for the date range.
func (h *ReportHandler) dailySales(f filters) []models.DailySaleRow {
	s := txScope(f)
	rows := make([]models.DailySaleRow, 0)
	database.GetDB().Raw(`
		SELECT TO_CHAR(t.created_at, 'YYYY-MM-DD') AS date,
			t.currency,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.amount_total ELSE 0 END), 0) AS gross_revenue
		FROM transactions t
		`+s.join+`
		WHERE `+s.where+`
		GROUP BY DATE(t.created_at), t.currency
		ORDER BY DATE(t.created_at), t.currency`,
		append([]interface{}{models.TransactionSucceeded}, s.args...)...,
	).Scan(&rows)

	for i := range rows {
		rows[i].CurrencySymbol = sym(rows[i].Currency)
		rows[i].GrossRevenue = fromSmallest(rows[i].GrossRevenue, rows[i].Currency)
	}
	return rows
}

// financeRows fetches per-currency detailed finance including organizer share.
func (h *ReportHandler) financeRows(f filters) []models.FinanceRow {
	s := txScope(f)
	rows := make([]models.FinanceRow, 0)
	database.GetDB().Raw(`
		SELECT t.currency,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.amount_total    ELSE 0 END), 0) AS gross_revenue,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.amount_total    ELSE 0 END), 0)
			- COALESCE(SUM(CASE WHEN r.status = ? THEN r.amount        ELSE 0 END), 0) AS net_revenue,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.organizer_share ELSE 0 END), 0) AS organizer_share,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.platform_fee    ELSE 0 END), 0) AS platform_fee,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.gateway_fee     ELSE 0 END), 0) AS gateway_fee,
			COALESCE(SUM(CASE WHEN r.status = ? THEN r.amount          ELSE 0 END), 0) AS refund
		FROM transactions t
		`+s.join+`
		LEFT JOIN refunds r ON r.transaction_id = t.id
		WHERE `+s.where+`
		GROUP BY t.currency
		ORDER BY t.currency`,
		append([]interface{}{
			models.TransactionSucceeded, models.TransactionSucceeded, models.RefundSucceeded,
			models.TransactionSucceeded, models.TransactionSucceeded, models.TransactionSucceeded,
			models.RefundSucceeded,
		}, s.args...)...,
	).Scan(&rows)

	for i := range rows {
		convertFinanceRow(&rows[i])
	}
	return rows
}

// paymentMethodRows fetches per-gateway per-currency earnings.
func (h *ReportHandler) paymentMethodRows(f filters) []models.PaymentMethodSummary {
	s := txScope(f)

	type rawRow struct {
		Gateway        string
		Currency       string
		GrossRevenue   float64
		NetRevenue     float64
		OrganizerShare float64
		PlatformFee    float64
		GatewayFee     float64
		Refund         float64
	}
	raw := make([]rawRow, 0)
	database.GetDB().Raw(`
		SELECT t.payment_gateway AS gateway, t.currency,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.amount_total    ELSE 0 END), 0) AS gross_revenue,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.amount_total    ELSE 0 END), 0)
			- COALESCE(SUM(CASE WHEN r.status = ? THEN r.amount        ELSE 0 END), 0) AS net_revenue,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.organizer_share ELSE 0 END), 0) AS organizer_share,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.platform_fee    ELSE 0 END), 0) AS platform_fee,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.gateway_fee     ELSE 0 END), 0) AS gateway_fee,
			COALESCE(SUM(CASE WHEN r.status = ? THEN r.amount          ELSE 0 END), 0) AS refund
		FROM transactions t
		`+s.join+`
		LEFT JOIN refunds r ON r.transaction_id = t.id
		WHERE `+s.where+`
		GROUP BY t.payment_gateway, t.currency
		ORDER BY t.payment_gateway, t.currency`,
		append([]interface{}{
			models.TransactionSucceeded, models.TransactionSucceeded, models.RefundSucceeded,
			models.TransactionSucceeded, models.TransactionSucceeded, models.TransactionSucceeded,
			models.RefundSucceeded,
		}, s.args...)...,
	).Scan(&raw)

	// Group into PaymentMethodSummary map then flatten
	byGateway := make(map[string][]models.PaymentEarning, 4)
	for _, r := range raw {
		byGateway[r.Gateway] = append(byGateway[r.Gateway], models.PaymentEarning{
			Currency:       r.Currency,
			CurrencySymbol: sym(r.Currency),
			GrossRevenue:   fromSmallest(r.GrossRevenue, r.Currency),
			NetRevenue:     fromSmallest(r.NetRevenue, r.Currency),
			OrganizerShare: fromSmallest(r.OrganizerShare, r.Currency),
			PlatformFee:    fromSmallest(r.PlatformFee, r.Currency),
			GatewayFee:     fromSmallest(r.GatewayFee, r.Currency),
			Refund:         fromSmallest(r.Refund, r.Currency),
		})
	}

	out := make([]models.PaymentMethodSummary, 0, len(byGateway))
	for name, earnings := range byGateway {
		out = append(out, models.PaymentMethodSummary{Name: name, Earnings: earnings})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ─────────────────────────────────────────────
// 1. Overview
// ─────────────────────────────────────────────

func (h *ReportHandler) adminOverview(f filters) *models.AdminOverviewReport {
	db := database.GetDB()

	// ── Events: one query with conditional aggregation ──
	var evRow struct {
		Total         int64
		Draft         int64
		Pending       int64
		Approved      int64
		Scheduled     int64
		SalesUpcoming int64
		OnSale        int64
		SalesEnd      int64
		Live          int64
		Hold          int64
		Held          int64
		Rejected      int64
		CancelPending int64
		Cancelled     int64
		Completed     int64
	}
	db.Raw(`
		SELECT COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status = 'draft')          AS draft,
			COUNT(*) FILTER (WHERE status = 'pending')        AS pending,
			COUNT(*) FILTER (WHERE status = 'approved')       AS approved,
			COUNT(*) FILTER (WHERE status = 'scheduled')      AS scheduled,
			COUNT(*) FILTER (WHERE status = 'sales_upcoming') AS sales_upcoming,
			COUNT(*) FILTER (WHERE status = 'on_sale')        AS on_sale,
			COUNT(*) FILTER (WHERE status = 'sales_end')      AS sales_end,
			COUNT(*) FILTER (WHERE status = 'live')           AS live,
			COUNT(*) FILTER (WHERE status = 'hold')           AS hold,
			COUNT(*) FILTER (WHERE status = 'held')           AS held,
			COUNT(*) FILTER (WHERE status = 'rejected')       AS rejected,
			COUNT(*) FILTER (WHERE status = 'cancel_pending') AS cancel_pending,
			COUNT(*) FILTER (WHERE status = 'cancelled')      AS cancelled,
			COUNT(*) FILTER (WHERE status = 'completed')      AS completed
		FROM events WHERE deleted_at IS NULL`,
	).Scan(&evRow)

	// ── Users + Organizers: one query ──
	var usrRow struct {
		Total       int64
		Active      int64
		Inactive    int64
		OrgTotal    int64
		OrgApproved int64
		OrgPending  int64
		OrgRejected int64
	}
	db.Raw(`
		SELECT COUNT(*)                                                                  AS total,
			COUNT(*) FILTER (WHERE account_status = 'active')                          AS active,
			COUNT(*) FILTER (WHERE account_status = 'inactive')                        AS inactive,
			COUNT(*) FILTER (WHERE organizer_status IS NOT NULL
			                   AND organizer_status != 'inactive')                     AS org_total,
			COUNT(*) FILTER (WHERE organizer_status = 'approved')                      AS org_approved,
			COUNT(*) FILTER (WHERE organizer_status = 'pending')                       AS org_pending,
			COUNT(*) FILTER (WHERE organizer_status = 'rejected')                      AS org_rejected
		FROM users WHERE deleted_at IS NULL`,
	).Scan(&usrRow)

	var guestTotal int64
	db.Raw(`SELECT COUNT(*) FROM guest_users`).Scan(&guestTotal)

	// ── Transactions: one query ──
	var txRow struct {
		Total      int64
		Pending    int64
		Processing int64
		Succeeded  int64
		Failed     int64
		Canceled   int64
		Expired    int64
	}
	db.Raw(`
		SELECT COUNT(*)                                              AS total,
			COUNT(*) FILTER (WHERE status = 'pending')             AS pending,
			COUNT(*) FILTER (WHERE status = 'processing')          AS processing,
			COUNT(*) FILTER (WHERE status = 'succeeded')           AS succeeded,
			COUNT(*) FILTER (WHERE status = 'failed')              AS failed,
			COUNT(*) FILTER (WHERE status = 'canceled')            AS canceled,
			COUNT(*) FILTER (WHERE status = 'expired')             AS expired
		FROM transactions`,
	).Scan(&txRow)

	// ── Refunds: one query ──
	var refRow struct {
		Total      int64
		Pending    int64
		Processing int64
		Succeeded  int64
		Failed     int64
		Cancelled  int64
		Rejected   int64
	}
	db.Raw(`
		SELECT COUNT(*)                                              AS total,
			COUNT(*) FILTER (WHERE status = 'pending')             AS pending,
			COUNT(*) FILTER (WHERE status = 'processing')          AS processing,
			COUNT(*) FILTER (WHERE status = 'succeeded')           AS succeeded,
			COUNT(*) FILTER (WHERE status = 'failed')              AS failed,
			COUNT(*) FILTER (WHERE status = 'cancelled')           AS cancelled,
			COUNT(*) FILTER (WHERE status = 'rejected')            AS rejected
		FROM refunds`,
	).Scan(&refRow)

	// ── Payment bills + payouts: one query ──
	var billRow struct {
		Total           int64
		Pending         int64
		PartiallyPaid   int64
		Paid            int64
		Cancelled       int64
		PayoutTotal     int64
		PayoutPending   int64
		PayoutPartial   int64
		PayoutPaid      int64
		PayoutCancelled int64
	}
	db.Raw(`
		SELECT COUNT(*)                                                          AS total,
			COUNT(*) FILTER (WHERE status = 'pending')                         AS pending,
			COUNT(*) FILTER (WHERE status = 'partially_paid')                  AS partially_paid,
			COUNT(*) FILTER (WHERE status = 'paid')                            AS paid,
			COUNT(*) FILTER (WHERE status = 'cancelled')                       AS cancelled,
			COUNT(*) FILTER (WHERE bill_type = 'payout')                       AS payout_total,
			COUNT(*) FILTER (WHERE bill_type = 'payout' AND status = 'pending')         AS payout_pending,
			COUNT(*) FILTER (WHERE bill_type = 'payout' AND status = 'partially_paid')  AS payout_partial,
			COUNT(*) FILTER (WHERE bill_type = 'payout' AND status = 'paid')            AS payout_paid,
			COUNT(*) FILTER (WHERE bill_type = 'payout' AND status = 'cancelled')       AS payout_cancelled
		FROM payment_bills`,
	).Scan(&billRow)

	return &models.AdminOverviewReport{
		Events: map[string]int64{
			"total": evRow.Total, "draft": evRow.Draft, "pending": evRow.Pending,
			"approved": evRow.Approved, "scheduled": evRow.Scheduled,
			"sales_upcoming": evRow.SalesUpcoming, "on_sale": evRow.OnSale,
			"sales_end": evRow.SalesEnd, "live": evRow.Live,
			"hold": evRow.Hold, "held": evRow.Held, "rejected": evRow.Rejected,
			"cancel_pending": evRow.CancelPending, "cancelled": evRow.Cancelled,
			"completed": evRow.Completed,
		},
		Users:  map[string]int64{"total": usrRow.Total, "active": usrRow.Active, "inactive": usrRow.Inactive},
		Guests: map[string]int64{"total": guestTotal},
		Organizers: map[string]int64{
			"total": usrRow.OrgTotal, "approved": usrRow.OrgApproved,
			"pending": usrRow.OrgPending, "rejected": usrRow.OrgRejected,
		},
		Transactions: map[string]int64{
			"total": txRow.Total, "pending": txRow.Pending, "processing": txRow.Processing,
			"succeeded": txRow.Succeeded, "failed": txRow.Failed,
			"canceled": txRow.Canceled, "expired": txRow.Expired,
		},
		Refunds: map[string]int64{
			"total": refRow.Total, "pending": refRow.Pending, "processing": refRow.Processing,
			"succeeded": refRow.Succeeded, "failed": refRow.Failed,
			"cancelled": refRow.Cancelled, "rejected": refRow.Rejected,
		},
		Billing: map[string]int64{
			"total": billRow.Total, "pending": billRow.Pending,
			"partially_paid": billRow.PartiallyPaid, "paid": billRow.Paid, "cancelled": billRow.Cancelled,
		},
		Payouts: map[string]int64{
			"total": billRow.PayoutTotal, "pending": billRow.PayoutPending,
			"partially_paid": billRow.PayoutPartial, "paid": billRow.PayoutPaid,
			"cancelled": billRow.PayoutCancelled,
		},
		SalesTrend: h.dailySales(f),
	}
}

func (h *ReportHandler) organizerOverview(f filters) *models.OrganizerOverviewReport {
	db := database.GetDB()
	oid := *f.orgID

	// ── Events ──
	var evRow struct {
		Total         int64
		Draft         int64
		Pending       int64
		Approved      int64
		Scheduled     int64
		SalesUpcoming int64
		OnSale        int64
		SalesEnd      int64
		Live          int64
		Hold          int64
		Held          int64
		Rejected      int64
		CancelPending int64
		Cancelled     int64
		Completed     int64
	}
	db.Raw(`
		SELECT COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status = 'draft')          AS draft,
			COUNT(*) FILTER (WHERE status = 'pending')        AS pending,
			COUNT(*) FILTER (WHERE status = 'approved')       AS approved,
			COUNT(*) FILTER (WHERE status = 'scheduled')      AS scheduled,
			COUNT(*) FILTER (WHERE status = 'sales_upcoming') AS sales_upcoming,
			COUNT(*) FILTER (WHERE status = 'on_sale')        AS on_sale,
			COUNT(*) FILTER (WHERE status = 'sales_end')      AS sales_end,
			COUNT(*) FILTER (WHERE status = 'live')           AS live,
			COUNT(*) FILTER (WHERE status = 'hold')           AS hold,
			COUNT(*) FILTER (WHERE status = 'held')           AS held,
			COUNT(*) FILTER (WHERE status = 'rejected')       AS rejected,
			COUNT(*) FILTER (WHERE status = 'cancel_pending') AS cancel_pending,
			COUNT(*) FILTER (WHERE status = 'cancelled')      AS cancelled,
			COUNT(*) FILTER (WHERE status = 'completed')      AS completed
		FROM events WHERE organizer_id = ? AND deleted_at IS NULL`, oid,
	).Scan(&evRow)

	// ── Transactions scoped to organizer ──
	var txRow struct {
		Total      int64
		Pending    int64
		Processing int64
		Succeeded  int64
		Failed     int64
		Canceled   int64
		Expired    int64
	}
	db.Raw(`
		SELECT COUNT(*)                                              AS total,
			COUNT(*) FILTER (WHERE t.status = 'pending')           AS pending,
			COUNT(*) FILTER (WHERE t.status = 'processing')        AS processing,
			COUNT(*) FILTER (WHERE t.status = 'succeeded')         AS succeeded,
			COUNT(*) FILTER (WHERE t.status = 'failed')            AS failed,
			COUNT(*) FILTER (WHERE t.status = 'canceled')          AS canceled,
			COUNT(*) FILTER (WHERE t.status = 'expired')           AS expired
		FROM transactions t
		INNER JOIN events oe ON t.event_id = oe.id
		WHERE oe.organizer_id = ?`, oid,
	).Scan(&txRow)

	// ── Refunds scoped to organizer ──
	var refRow struct {
		Total      int64
		Pending    int64
		Processing int64
		Succeeded  int64
		Failed     int64
		Cancelled  int64
		Rejected   int64
	}
	db.Raw(`
		SELECT COUNT(*)                                              AS total,
			COUNT(*) FILTER (WHERE r.status = 'pending')           AS pending,
			COUNT(*) FILTER (WHERE r.status = 'processing')        AS processing,
			COUNT(*) FILTER (WHERE r.status = 'succeeded')         AS succeeded,
			COUNT(*) FILTER (WHERE r.status = 'failed')            AS failed,
			COUNT(*) FILTER (WHERE r.status = 'cancelled')         AS cancelled,
			COUNT(*) FILTER (WHERE r.status = 'rejected')          AS rejected
		FROM refunds r
		INNER JOIN transactions t  ON r.transaction_id = t.id
		INNER JOIN events oe       ON t.event_id = oe.id
		WHERE oe.organizer_id = ?`, oid,
	).Scan(&refRow)

	// ── Bills + payouts ──
	var billRow struct {
		Total           int64
		Pending         int64
		PartiallyPaid   int64
		Paid            int64
		Cancelled       int64
		PayoutTotal     int64
		PayoutPending   int64
		PayoutPartial   int64
		PayoutPaid      int64
		PayoutCancelled int64
	}
	db.Raw(`
		SELECT COUNT(*)                                                          AS total,
			COUNT(*) FILTER (WHERE status = 'pending')                         AS pending,
			COUNT(*) FILTER (WHERE status = 'partially_paid')                  AS partially_paid,
			COUNT(*) FILTER (WHERE status = 'paid')                            AS paid,
			COUNT(*) FILTER (WHERE status = 'cancelled')                       AS cancelled,
			COUNT(*) FILTER (WHERE bill_type = 'payout')                       AS payout_total,
			COUNT(*) FILTER (WHERE bill_type = 'payout' AND status = 'pending')        AS payout_pending,
			COUNT(*) FILTER (WHERE bill_type = 'payout' AND status = 'partially_paid') AS payout_partial,
			COUNT(*) FILTER (WHERE bill_type = 'payout' AND status = 'paid')           AS payout_paid,
			COUNT(*) FILTER (WHERE bill_type = 'payout' AND status = 'cancelled')      AS payout_cancelled
		FROM payment_bills WHERE organizer_id = ?`, oid,
	).Scan(&billRow)

	return &models.OrganizerOverviewReport{
		Events: map[string]int64{
			"total": evRow.Total, "draft": evRow.Draft, "pending": evRow.Pending,
			"approved": evRow.Approved, "scheduled": evRow.Scheduled,
			"sales_upcoming": evRow.SalesUpcoming, "on_sale": evRow.OnSale,
			"sales_end": evRow.SalesEnd, "live": evRow.Live,
			"hold": evRow.Hold, "held": evRow.Held, "rejected": evRow.Rejected,
			"cancel_pending": evRow.CancelPending, "cancelled": evRow.Cancelled,
			"completed": evRow.Completed,
		},
		Transactions: map[string]int64{
			"total": txRow.Total, "pending": txRow.Pending, "processing": txRow.Processing,
			"succeeded": txRow.Succeeded, "failed": txRow.Failed,
			"canceled": txRow.Canceled, "expired": txRow.Expired,
		},
		Refunds: map[string]int64{
			"total": refRow.Total, "pending": refRow.Pending, "processing": refRow.Processing,
			"succeeded": refRow.Succeeded, "failed": refRow.Failed,
			"cancelled": refRow.Cancelled, "rejected": refRow.Rejected,
		},
		Billing: map[string]int64{
			"total": billRow.Total, "pending": billRow.Pending,
			"partially_paid": billRow.PartiallyPaid, "paid": billRow.Paid, "cancelled": billRow.Cancelled,
		},
		Payouts: map[string]int64{
			"total": billRow.PayoutTotal, "pending": billRow.PayoutPending,
			"partially_paid": billRow.PayoutPartial, "paid": billRow.PayoutPaid,
			"cancelled": billRow.PayoutCancelled,
		},
		SalesTrend: h.dailySales(f),
	}
}

// ─────────────────────────────────────────────
// 2. Sales
// ─────────────────────────────────────────────

func (h *ReportHandler) salesReport(f filters) *models.SalesReport {
	return &models.SalesReport{
		Sales:      h.saleRows(f),
		DailySales: h.dailySales(f),
	}
}

// ─────────────────────────────────────────────
// 3. Customer Analytics
// ─────────────────────────────────────────────

func (h *ReportHandler) customerAnalytics(f filters) *models.CustomerAnalyticsReport {
	db := database.GetDB()

	// ── Users + guests: two queries (cross-table, can't join meaningfully) ──
	var usrRow struct {
		Total    int64
		Active   int64
		Inactive int64
	}
	db.Raw(`
		SELECT COUNT(*) AS total,
			COUNT(*) FILTER (WHERE account_status = 'active')   AS active,
			COUNT(*) FILTER (WHERE account_status = 'inactive') AS inactive
		FROM users WHERE deleted_at IS NULL`,
	).Scan(&usrRow)

	var guestTotal int64
	db.Raw(`SELECT COUNT(*) FROM guest_users`).Scan(&guestTotal)

	// ── New vs repeat within date range ──
	var custRow struct {
		Total  int64
		New    int64
		Repeat int64
	}
	db.Raw(`
		SELECT COUNT(*) AS total,
			COUNT(*) FILTER (WHERE first_purchase BETWEEN ? AND ?)                                 AS new,
			COUNT(*) FILTER (WHERE first_purchase < ? AND last_purchase BETWEEN ? AND ?) AS "repeat"
		FROM (
			SELECT actor_id,
				MIN(created_at) AS first_purchase,
				MAX(created_at) AS last_purchase
			FROM transactions
			WHERE status = ?
			GROUP BY actor_id
		) sub`,
		f.start, f.eod,
		f.start, f.start, f.eod,
		models.TransactionSucceeded,
	).Scan(&custRow)

	repeatRate := 0.0
	if custRow.Total > 0 {
		repeatRate = float64(custRow.Repeat) / float64(custRow.Total) * 100
	}

	// ── Top actors: one query, per (actor, currency, event) ──
	type actorCurrRow struct {
		ActorID    string
		ActorName  string
		ActorEmail string
		ActorType  string
		Currency   string
		EventTitle string
		TotalSpent float64
		Tickets    int64
	}
	raw := make([]actorCurrRow, 0)
	db.Raw(`
		SELECT t.actor_id::text AS actor_id,
			COALESCE(CASE
				WHEN t.actor_type = 'user'  THEN CONCAT(u.first_name, ' ', u.last_name)
				WHEN t.actor_type = 'guest' THEN gu.name
			END, 'Guest')                                    AS actor_name,
			COALESCE(CASE
				WHEN t.actor_type = 'user'  THEN u.email
				WHEN t.actor_type = 'guest' THEN gu.email
			END, '')                                         AS actor_email,
			t.actor_type                                     AS actor_type,
			t.currency,
			COALESCE(ev.title, 'Unknown')                    AS event_title,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.amount_total ELSE 0 END), 0) AS total_spent,
			COALESCE(SUM(CASE WHEN t.status = ? THEN t.quantity     ELSE 0 END), 0) AS tickets
		FROM transactions t
		LEFT JOIN users        u  ON t.actor_type = 'user'  AND t.actor_id = u.id
		LEFT JOIN guest_users  gu ON t.actor_type = 'guest' AND t.actor_id = gu.id
		LEFT JOIN events       ev ON t.event_id = ev.id
		WHERE t.status = ?
		GROUP BY t.actor_id, t.actor_type, t.currency, ev.title, u.id, gu.id
		ORDER BY tickets DESC
		LIMIT 50`,
		models.TransactionSucceeded, models.TransactionSucceeded, models.TransactionSucceeded,
	).Scan(&raw)

	// Group into TopActor structs
	type actorKey struct{ id, currency string }
	actorIndex := make(map[string]int)                  // actor_id → index in topActors
	spendIndex := make(map[actorKey]int)                // (actor_id, currency) → index in Spending
	eventSets := make(map[actorKey]map[string]struct{}) // event name dedup
	topActors := make([]models.TopActor, 0, 10)

	for _, r := range raw {
		ai, exists := actorIndex[r.ActorID]
		if !exists {
			topActors = append(topActors, models.TopActor{
				ID: r.ActorID, Name: r.ActorName,
				Email: r.ActorEmail, Type: r.ActorType,
			})
			ai = len(topActors) - 1
			actorIndex[r.ActorID] = ai
		}

		ak := actorKey{r.ActorID, r.Currency}
		si, hasCurr := spendIndex[ak]
		if !hasCurr {
			topActors[ai].Spending = append(topActors[ai].Spending, models.ActorSpend{
				Currency:       r.Currency,
				CurrencySymbol: sym(r.Currency),
				TotalSpent:     fromSmallest(r.TotalSpent, r.Currency),
				Tickets:        r.Tickets,
			})
			si = len(topActors[ai].Spending) - 1
			spendIndex[ak] = si
			eventSets[ak] = make(map[string]struct{})
		} else {
			topActors[ai].Spending[si].TotalSpent += fromSmallest(r.TotalSpent, r.Currency)
			topActors[ai].Spending[si].Tickets += r.Tickets
		}
		eventSets[ak][r.EventTitle] = struct{}{}
		topActors[ai].TotalTickets += r.Tickets
	}

	// Populate event names and sort
	for _, actor := range topActors {
		for i, sp := range actor.Spending {
			ak := actorKey{actor.ID, sp.Currency}
			names := make([]string, 0, len(eventSets[ak]))
			for n := range eventSets[ak] {
				names = append(names, n)
			}
			sort.Strings(names)
			actor.Spending[i].EventNames = strings.Join(names, ", ")
		}
	}

	sort.Slice(topActors, func(i, j int) bool {
		return topActors[i].TotalTickets > topActors[j].TotalTickets
	})
	if len(topActors) > 5 {
		topActors = topActors[:5]
	}

	return &models.CustomerAnalyticsReport{
		Users:          map[string]int64{"total": usrRow.Total, "active": usrRow.Active, "inactive": usrRow.Inactive},
		Guests:         map[string]int64{"total": guestTotal},
		TotalCustomers: custRow.Total,
		NewCustomers:   custRow.New,
		RepeatRate:     repeatRate,
		TopActors:      topActors,
	}
}

// ─────────────────────────────────────────────
// 4. Financial
// ─────────────────────────────────────────────

func (h *ReportHandler) financialReport(f filters) *models.FinancialReport {
	return &models.FinancialReport{
		Finances:       h.financeRows(f),
		PaymentMethods: h.paymentMethodRows(f),
	}
}

// ─────────────────────────────────────────────
// 5. Event Performance
// ─────────────────────────────────────────────

func (h *ReportHandler) eventPerformance(eventID uuid.UUID, f filters) *models.EventPerformanceReport {
	db := database.GetDB()

	var ev models.Event
	if err := db.First(&ev, "id = ?", eventID).Error; err != nil {
		return nil
	}
	// Organizer scope check — never trust the client
	if f.orgID != nil && ev.OrganizerID != *f.orgID {
		return nil
	}

	var r struct {
		Sold  int64
		Rev   float64
		Trans int64
	}
	db.Raw(`
		SELECT COALESCE(SUM(CASE WHEN status = ? THEN quantity     ELSE 0 END), 0) AS sold,
			   COALESCE(SUM(CASE WHEN status = ? THEN amount_total ELSE 0 END), 0) AS rev,
			   COUNT(DISTINCT id) AS trans
		FROM transactions WHERE event_id = ?`,
		models.TransactionSucceeded, models.TransactionSucceeded, eventID,
	).Scan(&r)

	soldPct := 0.0
	if ev.Capacity > 0 {
		soldPct = float64(r.Sold) / float64(ev.Capacity) * 100
	}

	var checkedIn int64
	db.Raw(`
		SELECT COUNT(DISTINCT tci.ticket_id)
		FROM ticket_check_ins tci
		INNER JOIN tickets tk ON tk.id = tci.ticket_id
		WHERE tk.event_id = ?`, eventID,
	).Scan(&checkedIn)

	// ── Tier breakdown ──
	tiers := make([]models.TierPerformRow, 0)
	db.Raw(`
		SELECT et.tier_name, et.quantity AS capacity,
			COALESCE(s.sold, 0)    AS sold,
			COALESCE(s.revenue, 0) AS revenue,
			CASE WHEN et.quantity > 0
				THEN ROUND((COALESCE(s.sold, 0)::numeric / et.quantity) * 100, 2)
				ELSE 0
			END AS sold_pct
		FROM event_tiers et
		LEFT JOIN (
			SELECT tier_id,
				SUM(CASE WHEN status = ? THEN quantity     ELSE 0 END) AS sold,
				SUM(CASE WHEN status = ? THEN amount_total ELSE 0 END) AS revenue
			FROM transactions
			WHERE event_id = ?
			GROUP BY tier_id
		) s ON s.tier_id = et.id
		WHERE et.event_id = ? AND et.deleted_at IS NULL
		ORDER BY et.sort_order, et.price`,
		models.TransactionSucceeded, models.TransactionSucceeded, eventID, eventID,
	).Scan(&tiers)

	return &models.EventPerformanceReport{
		EventID:        eventID.String(),
		EventTitle:     ev.Title,
		Status:         ev.Status,
		Currency:       ev.Currency,
		CurrencySymbol: sym(ev.Currency),
		TicketsSold:    r.Sold,
		Revenue:        fromSmallest(r.Rev, ev.Currency),
		Transactions:   r.Trans,
		SoldPercentage: soldPct,
		CheckedIn:      checkedIn,
		TierBreakdown:  tiers,
	}
}
