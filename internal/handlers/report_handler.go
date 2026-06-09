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

// ─────────────────────────────────────────────
// Route handlers
// ─────────────────────────────────────────────

// GetAdminReport godoc
// @Summary Get admin report
// @Tags Reports
// @Security ApiKeyAuth
// @Param type      query string false "overview|sales|payments|refunds|event_performance|customers"
// @Param start_date query string false "YYYY-MM-DD (default: 30 days ago)"
// @Param end_date   query string false "YYYY-MM-DD (default: today)"
// @Param currency   query string false "Filter by currency code e.g. JPY, USD"
// @Param country    query string false "Filter by country code e.g. JP, US"
// @Param event_id   query string false "Required for event_performance"
// @Produce json
// @Success 200 {object} utils.Response
// @Router /api/v1/admin/reports [get]
func (h *ReportHandler) GetAdminReport(c *gin.Context) {
    f := parseFilters(c, nil)
    h.dispatch(c, f)
}

// GetOrganizerReport godoc
// @Summary Get organizer report
// @Tags Reports
// @Security ApiKeyAuth
// @Param type      query string false "overview|sales|payments|refunds|event_performance|customers"
// @Param start_date query string false "YYYY-MM-DD"
// @Param end_date   query string false "YYYY-MM-DD"
// @Param currency   query string false "Filter by currency"
// @Param country    query string false "Filter by country"
// @Param event_id   query string false "Required for event_performance"
// @Produce json
// @Success 200 {object} utils.Response
// @Router /api/v1/organizer/reports [get]
func (h *ReportHandler) GetOrganizerReport(c *gin.Context) {
    userIDVal, exists := c.Get("userID")
    if !exists {
        utils.ErrorResponse(c, http.StatusUnauthorized, "User not authenticated", nil)
        return
    }
    orgID, err := utils.GetOrganizerIDForUser(database.GetDB(), userIDVal.(uuid.UUID))
    if err != nil {
        utils.ErrorResponse(c, http.StatusForbidden, "User is not an organizer", nil)
        return
    }
    f := parseFilters(c, &orgID)
    h.dispatch(c, f)
}

func (h *ReportHandler) dispatch(c *gin.Context, f filters) {
    switch c.DefaultQuery("type", "overview") {
    case "overview":
        if f.orgID != nil {
            utils.SuccessResponse(c, http.StatusOK, "Overview report", h.organizerOverview(f))
        } else {
            utils.SuccessResponse(c, http.StatusOK, "Overview report", h.adminOverview(f))
        }
    case "sales":
        utils.SuccessResponse(c, http.StatusOK, "Sales report", h.salesReport(f))
    case "payments":
        utils.SuccessResponse(c, http.StatusOK, "Payments report", h.paymentsReport(f))
    case "refunds":
        utils.SuccessResponse(c, http.StatusOK, "Refunds report", h.refundsReport(f))
    case "event_performance":
        eventIDStr := c.Query("event_id")
        if eventIDStr == "" {
            utils.ErrorResponse(c, http.StatusBadRequest, "event_id required for event_performance", nil)
            return
        }
        eventID, err := uuid.Parse(eventIDStr)
        if err != nil {
            utils.ErrorResponse(c, http.StatusBadRequest, "Invalid event_id", nil)
            return
        }
        utils.SuccessResponse(c, http.StatusOK, "Event performance report", h.eventPerformance(eventID, f))
    case "customers":
        utils.SuccessResponse(c, http.StatusOK, "Customers report", h.customersReport(f))
    default:
        utils.ErrorResponse(c, http.StatusBadRequest,
            "Invalid type. Allowed: overview, sales, payments, refunds, event_performance, customers", nil)
    }
}

// ─────────────────────────────────────────────
// Filter struct & parser
// ─────────────────────────────────────────────

type filters struct {
    start    time.Time
    end      time.Time
    eod      time.Time   // end-of-day (end + 1 day for BETWEEN)
    orgID    *uuid.UUID
    currency string      // optional: "JPY", "USD" …
    country  string      // optional: "JP", "US" …
}

func parseFilters(c *gin.Context, orgID *uuid.UUID) filters {
    now := time.Now()
    start := now.AddDate(0, -1, 0)
    end := now
    if s := c.Query("start_date"); s != "" {
        if p, err := time.Parse("2006-01-02", s); err == nil {
            start = p
        }
    }
    if s := c.Query("end_date"); s != "" {
        if p, err := time.Parse("2006-01-02", s); err == nil {
            end = p
        }
    }
    return filters{
        start:    start,
        end:      end,
        eod:      end.AddDate(0, 0, 1),
        orgID:    orgID,
        currency: c.Query("currency"),
        country:  c.Query("country"),
    }
}

// ─────────────────────────────────────────────
// Query helpers
// ─────────────────────────────────────────────

// txnJoin builds the optional INNER JOIN + WHERE clauses for organizer scope,
// currency filter, and country filter. Returns the JOIN clause and args slice.
// baseArgs are the args that come BEFORE the WHERE date args.
func txnScope(f filters) (join string, where string, args []interface{}) {
    if f.orgID != nil {
        join = `INNER JOIN events e ON t.event_id = e.id`
        where = `e.organizer_id = ? AND `
        args = append(args, *f.orgID)
    }
    if f.currency != "" {
        where += `t.currency = ? AND `
        args = append(args, f.currency)
    }
    if f.country != "" {
        where += `t.country = ? AND `
        args = append(args, f.country)
    }
    // Always cap to date range
    where += `t.created_at BETWEEN ? AND ?`
    args = append(args, f.start, f.eod)
    return
}

func (h *ReportHandler) getDailySales(f filters) []models.DailySaleRecord {
    db := database.GetDB()
    join, where, args := txnScope(f)
    records := make([]models.DailySaleRecord, 0)
    db.Raw(`
        SELECT
            TO_CHAR(t.created_at, 'YYYY-MM-DD') as date,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.amount_total ELSE 0 END), 0) as revenue,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.quantity ELSE 0 END), 0) as tickets_sold,
            COUNT(*) as transactions
        FROM transactions t
        `+join+`
        WHERE `+where+`
        GROUP BY DATE(t.created_at)
        ORDER BY DATE(t.created_at) ASC`,
        args...).Scan(&records)
    return records
}

func (h *ReportHandler) getCurrencyBreakdown(f filters) []models.CurrencyBreakdown {
    db := database.GetDB()
    join, where, args := txnScope(f)
    // Add refund join separately
    result := make([]models.CurrencyBreakdown, 0)
    db.Raw(`
        SELECT
            t.currency,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.amount_total ELSE 0 END), 0) as revenue,
            COALESCE(SUM(CASE WHEN r.status = 'succeeded' THEN r.amount ELSE 0 END), 0) as refunds,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.amount_total ELSE 0 END), 0)
                - COALESCE(SUM(CASE WHEN r.status = 'succeeded' THEN r.amount ELSE 0 END), 0) as net_revenue,
            COUNT(DISTINCT t.id) as transactions
        FROM transactions t
        `+join+`
        LEFT JOIN refunds r ON r.transaction_id = t.id
        WHERE `+where+`
        GROUP BY t.currency
        ORDER BY revenue DESC`,
        args...).Scan(&result)
    return result
}

func (h *ReportHandler) getCountryBreakdown(f filters) []models.CountryBreakdown {
    db := database.GetDB()
    join, where, args := txnScope(f)
    result := make([]models.CountryBreakdown, 0)
    db.Raw(`
        SELECT
            COALESCE(t.country, 'Unknown') as country,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.amount_total ELSE 0 END), 0) as revenue,
            COUNT(*) as transactions,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.quantity ELSE 0 END), 0) as tickets_sold
        FROM transactions t
        `+join+`
        WHERE `+where+`
        GROUP BY t.country
        ORDER BY revenue DESC
        LIMIT 20`,
        args...).Scan(&result)
    return result
}

// ─────────────────────────────────────────────
// 1. Overview
// ─────────────────────────────────────────────

func (h *ReportHandler) adminOverview(f filters) *models.AdminOverviewReport {
    db := database.GetDB()
    join, where, args := txnScope(f)

    var r struct {
        TotalEvents     int64
        ActiveEvents    int64
        TotalOrganizers int64
        Revenue         float64
        Refunds         float64
        TicketsSold     int64
    }
    db.Raw(`
        SELECT
            (SELECT COUNT(*) FROM events WHERE deleted_at IS NULL) as total_events,
            (SELECT COUNT(*) FROM events WHERE status IN ('on_sale','live') AND deleted_at IS NULL) as active_events,
            (SELECT COUNT(*) FROM users WHERE organizer_status = 'approved' AND deleted_at IS NULL) as total_organizers,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.amount_total ELSE 0 END), 0) as revenue,
            COALESCE(SUM(CASE WHEN ref.status = 'succeeded' THEN ref.amount ELSE 0 END), 0) as refunds,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.quantity ELSE 0 END), 0) as tickets_sold
        FROM transactions t
        `+join+`
        LEFT JOIN refunds ref ON ref.transaction_id = t.id
        WHERE `+where,
        args...).Scan(&r)

    net := r.Revenue - r.Refunds
    if net < 0 {
        net = 0
    }

    var pendingPayouts float64
    db.Raw(`SELECT COALESCE(SUM(amount - paid_amount), 0) FROM payment_bills
            WHERE status IN ('pending','partially_paid') AND bill_type = 'payout'`).
        Scan(&pendingPayouts)

    return &models.AdminOverviewReport{
        TotalEvents:       r.TotalEvents,
        ActiveEvents:      r.ActiveEvents,
        TotalOrganizers:   r.TotalOrganizers,
        TotalRevenue:      r.Revenue,
        TotalRefunds:      r.Refunds,
        NetRevenue:        net,
        TotalTicketsSold:  r.TicketsSold,
        PendingPayouts:    pendingPayouts,
        SalesTrend:        h.getDailySales(f),
        CurrencyBreakdown: h.getCurrencyBreakdown(f),
    }
}

func (h *ReportHandler) organizerOverview(f filters) *models.OrganizerOverviewReport {
    db := database.GetDB()
    join, where, args := txnScope(f)

    var r struct {
        TotalEvents  int64
        ActiveEvents int64
        Revenue      float64
        Refunds      float64
        TicketsSold  int64
    }
    db.Raw(`
        SELECT
            (SELECT COUNT(*) FROM events WHERE organizer_id = ? AND deleted_at IS NULL) as total_events,
            (SELECT COUNT(*) FROM events WHERE organizer_id = ? AND status IN ('on_sale','live') AND deleted_at IS NULL) as active_events,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.amount_total ELSE 0 END), 0) as revenue,
            COALESCE(SUM(CASE WHEN ref.status = 'succeeded' THEN ref.amount ELSE 0 END), 0) as refunds,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.quantity ELSE 0 END), 0) as tickets_sold
        FROM transactions t
        `+join+`
        LEFT JOIN refunds ref ON ref.transaction_id = t.id
        WHERE `+where,
        append([]interface{}{*f.orgID, *f.orgID}, args...)...).Scan(&r)

    net := r.Revenue - r.Refunds
    if net < 0 {
        net = 0
    }

    var pendingPayouts float64
    db.Raw(`SELECT COALESCE(SUM(amount - paid_amount), 0) FROM payment_bills
            WHERE organizer_id = ? AND status IN ('pending','partially_paid') AND bill_type = 'payout'`,
        *f.orgID).Scan(&pendingPayouts)

    return &models.OrganizerOverviewReport{
        TotalEvents:       r.TotalEvents,
        ActiveEvents:      r.ActiveEvents,
        TotalRevenue:      r.Revenue,
        TotalRefunds:      r.Refunds,
        NetRevenue:        net,
        TotalTicketsSold:  r.TicketsSold,
        PendingPayouts:    pendingPayouts,
        SalesTrend:        h.getDailySales(f),
        CurrencyBreakdown: h.getCurrencyBreakdown(f),
    }
}

// ─────────────────────────────────────────────
// 2. Sales
// ─────────────────────────────────────────────

func (h *ReportHandler) salesReport(f filters) *models.SalesReport {
    db := database.GetDB()
    join, where, args := txnScope(f)

    var r struct {
        Revenue    float64
        Sold       int64
        Trans      int64
        AvgOrder   float64
    }
    db.Raw(`
        SELECT
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.amount_total ELSE 0 END), 0) as revenue,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.quantity ELSE 0 END), 0) as sold,
            COUNT(*) as trans,
            COALESCE(AVG(CASE WHEN t.status = 'succeeded' THEN t.amount_total END), 0) as avg_order
        FROM transactions t
        `+join+`
        WHERE `+where,
        args...).Scan(&r)

    return &models.SalesReport{
        TotalRevenue:      r.Revenue,
        TotalTicketsSold:  r.Sold,
        TotalTransactions: r.Trans,
        AverageOrderValue: r.AvgOrder,
        DailySales:        h.getDailySales(f),
        CurrencyBreakdown: h.getCurrencyBreakdown(f),
        CountryBreakdown:  h.getCountryBreakdown(f),
    }
}

// ─────────────────────────────────────────────
// 3. Payments
// ─────────────────────────────────────────────

func (h *ReportHandler) paymentsReport(f filters) *models.PaymentsReport {
    db := database.GetDB()
    join, where, args := txnScope(f)

    var totals struct {
        Total     int64
        Succeeded int64
        Failed    int64
        Pending   int64
        Expired   int64
    }
    db.Raw(`
        SELECT
            COUNT(*) as total,
            COUNT(*) FILTER (WHERE t.status = 'succeeded') as succeeded,
            COUNT(*) FILTER (WHERE t.status = 'failed') as failed,
            COUNT(*) FILTER (WHERE t.status = 'pending') as pending,
            COUNT(*) FILTER (WHERE t.status = 'expired') as expired
        FROM transactions t
        `+join+`
        WHERE `+where,
        args...).Scan(&totals)

    conversion := 0.0
    if totals.Total > 0 {
        conversion = float64(totals.Succeeded) / float64(totals.Total) * 100
    }

    methods := make([]models.PaymentMethodBreakdown, 0)
    db.Raw(`
        SELECT
            COALESCE(t.payment_method_type, 'unknown') as method,
            COALESCE(t.payment_provider, 'unknown') as provider,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.amount_total ELSE 0 END), 0) as revenue,
            COUNT(*) as transactions,
            COUNT(*) FILTER (WHERE t.status = 'pending') as pending_count,
            COALESCE(SUM(CASE WHEN t.status = 'pending' THEN t.amount_total ELSE 0 END), 0) as pending_amount,
            COUNT(*) FILTER (WHERE t.status = 'expired') as expired_count,
            CASE WHEN COUNT(*) > 0
                THEN ROUND((COUNT(*) FILTER (WHERE t.status = 'succeeded')::numeric / COUNT(*)) * 100, 2)
                ELSE 0 END as conversion_rate
        FROM transactions t
        `+join+`
        WHERE `+where+`
        GROUP BY t.payment_method_type, t.payment_provider
        ORDER BY revenue DESC`,
        args...).Scan(&methods)

    return &models.PaymentsReport{
        TotalTransactions: totals.Total,
        SucceededCount:    totals.Succeeded,
        FailedCount:       totals.Failed,
        PendingCount:      totals.Pending,
        ExpiredCount:      totals.Expired,
        OverallConversion: conversion,
        MethodBreakdown:   methods,
    }
}

// ─────────────────────────────────────────────
// 4. Refunds
// ─────────────────────────────────────────────

func (h *ReportHandler) refundsReport(f filters) *models.RefundsReport {
    db := database.GetDB()

    // Build organizer scope for refunds via transactions
    refundJoin := `INNER JOIN transactions t ON t.id = r.transaction_id`
    refundWhere := `r.created_at BETWEEN ? AND ?`
    args := []interface{}{f.start, f.eod}

    if f.orgID != nil {
        refundJoin += ` INNER JOIN events e ON t.event_id = e.id`
        refundWhere = `e.organizer_id = ? AND ` + refundWhere
        args = append([]interface{}{*f.orgID}, args...)
    }
    if f.currency != "" {
        refundWhere += ` AND t.currency = ?`
        args = append(args, f.currency)
    }

    var r struct {
        TotalAmount float64
        Count       int64
        AvgAmount   float64
    }
    db.Raw(`
        SELECT
            COALESCE(SUM(CASE WHEN r.status = 'succeeded' THEN r.amount ELSE 0 END), 0) as total_amount,
            COUNT(*) as count,
            COALESCE(AVG(CASE WHEN r.status = 'succeeded' THEN r.amount END), 0) as avg_amount
        FROM refunds r
        `+refundJoin+`
        WHERE `+refundWhere,
        args...).Scan(&r)

    // Refund rate: refunded txns / total succeeded txns
    var succeededCount int64
    {
        join, where, wargs := txnScope(f)
        db.Raw(`SELECT COUNT(*) FROM transactions t `+join+` WHERE t.status = 'succeeded' AND `+where, wargs...).
            Scan(&succeededCount)
    }
    refundRate := 0.0
    if succeededCount > 0 {
        refundRate = float64(r.Count) / float64(succeededCount) * 100
    }

    statusBreakdown := make([]models.RefundStatusBreakdown, 0)
    db.Raw(`
        SELECT r.status, COUNT(*) as count,
            COALESCE(SUM(r.amount), 0) as amount
        FROM refunds r `+refundJoin+`
        WHERE `+refundWhere+`
        GROUP BY r.status`,
        args...).Scan(&statusBreakdown)

    methodBreakdown := make([]models.RefundMethodBreakdown, 0)
    db.Raw(`
        SELECT COALESCE(r.method, 'unknown') as method, COUNT(*) as count,
            COALESCE(SUM(r.amount), 0) as amount
        FROM refunds r `+refundJoin+`
        WHERE `+refundWhere+`
        GROUP BY r.method`,
        args...).Scan(&methodBreakdown)

    return &models.RefundsReport{
        TotalRefunds:    r.TotalAmount,
        RefundCount:     r.Count,
        RefundRate:      refundRate,
        AvgRefundAmount: r.AvgAmount,
        StatusBreakdown: statusBreakdown,
        MethodBreakdown: methodBreakdown,
    }
}

// ─────────────────────────────────────────────
// 5. Event performance
// ─────────────────────────────────────────────

func (h *ReportHandler) eventPerformance(eventID uuid.UUID, f filters) *models.EventPerformanceReport {
    db := database.GetDB()

    var event models.Event
    if err := db.First(&event, "id = ?", eventID).Error; err != nil {
        return nil
    }

    // If organizer-scoped, verify ownership
    if f.orgID != nil && event.OrganizerID != *f.orgID {
        return nil
    }

    var r struct {
        Sold    int64
        Revenue float64
        Refunds float64
        Trans   int64
    }
    db.Raw(`
        SELECT
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.quantity ELSE 0 END), 0) as sold,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.amount_total ELSE 0 END), 0) as revenue,
            COALESCE(SUM(CASE WHEN ref.status = 'succeeded' THEN ref.amount ELSE 0 END), 0) as refunds,
            COUNT(DISTINCT t.id) as trans
        FROM transactions t
        LEFT JOIN refunds ref ON ref.transaction_id = t.id
        WHERE t.event_id = ?`,
        eventID).Scan(&r)

    soldPct := 0.0
    if event.Capacity > 0 {
        soldPct = float64(r.Sold) / float64(event.Capacity) * 100
    }
    net := r.Revenue - r.Refunds
    if net < 0 {
        net = 0
    }

    // Per-tier breakdown
    tiers := make([]models.TicketTierSummary, 0)
    db.Raw(`
        SELECT
            tt.id as tier_id,
            tt.name as tier_name,
            tt.price,
            tt.currency,
            tt.capacity,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.quantity ELSE 0 END), 0) as sold,
            tt.capacity - COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.quantity ELSE 0 END), 0) as available,
            CASE WHEN tt.capacity > 0
                THEN ROUND((COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.quantity ELSE 0 END), 0)::numeric / tt.capacity) * 100, 2)
                ELSE 0 END as sold_pct,
            COALESCE(SUM(CASE WHEN t.status = 'succeeded' THEN t.amount_total ELSE 0 END), 0) as revenue
        FROM ticket_tiers tt
        LEFT JOIN transactions t ON t.ticket_tier_id = tt.id
        WHERE tt.event_id = ? AND tt.deleted_at IS NULL
        GROUP BY tt.id, tt.name, tt.price, tt.currency, tt.capacity
        ORDER BY tt.price ASC`,
        eventID).Scan(&tiers)

    return &models.EventPerformanceReport{
        EventID:        event.ID,
        EventTitle:     event.Title,
        Status:         event.Status,
        Capacity:       int64(event.Capacity),
        TicketsSold:    r.Sold,
        Available:      int64(event.Capacity) - r.Sold,
        SoldPercentage: soldPct,
        Revenue:        r.Revenue,
        Refunds:        r.Refunds,
        NetRevenue:     net,
        Transactions:   r.Trans,
        TierBreakdown:  tiers,
    }
}

// ─────────────────────────────────────────────
// 6. Customers
// ─────────────────────────────────────────────

func (h *ReportHandler) customersReport(f filters) *models.CustomersReport {
    db := database.GetDB()
    join, where, args := txnScope(f)

    var totals struct {
        Total      int64
        Registered int64
        Guest      int64
    }
    db.Raw(`
        SELECT
            COUNT(DISTINCT COALESCE(t.user_id::text, t.guest_user_id::text)) as total,
            COUNT(DISTINCT t.user_id) FILTER (WHERE t.user_id IS NOT NULL) as registered,
            COUNT(DISTINCT t.guest_user_id) FILTER (WHERE t.guest_user_id IS NOT NULL AND t.user_id IS NULL) as guest
        FROM transactions t
        `+join+`
        WHERE t.status = 'succeeded' AND `+where,
        args...).Scan(&totals)

    var custStats struct {
        New    int64
        Repeat int64
    }
    db.Raw(`
        SELECT
            COUNT(*) FILTER (WHERE first_purchase BETWEEN ? AND ?) as new,
            COUNT(*) FILTER (WHERE first_purchase < ? AND last_purchase BETWEEN ? AND ?) as repeat
        FROM (
            SELECT COALESCE(t.user_id::text, t.guest_user_id::text) as cid,
                MIN(t.created_at) as first_purchase,
                MAX(t.created_at) as last_purchase
            FROM transactions t
            `+join+`
            WHERE t.status = 'succeeded' AND `+where+`
            GROUP BY cid
        ) sub`,
        append([]interface{}{f.start, f.eod, f.start, f.start, f.eod}, args...)...).Scan(&custStats)

    repeatRate := 0.0
    if totals.Total > 0 {
        repeatRate = float64(custStats.Repeat) / float64(totals.Total) * 100
    }

    top := make([]models.TopCustomer, 0)
    db.Raw(`
        SELECT
            t.user_id as customer_id,
            COALESCE(CONCAT(u.first_name, ' ', u.last_name), gu.name, 'Guest') as customer_name,
            COALESCE(u.email, gu.email, '') as customer_email,
            (t.user_id IS NULL) as is_guest,
            COALESCE(t.country, '') as country,
            SUM(CASE WHEN t.status = 'succeeded' THEN t.amount_total ELSE 0 END) as total_spent,
            SUM(CASE WHEN t.status = 'succeeded' THEN t.quantity ELSE 0 END) as tickets_purchased,
            COUNT(DISTINCT t.id) as order_count
        FROM transactions t
        LEFT JOIN users u ON t.user_id = u.id
        LEFT JOIN guest_users gu ON t.guest_user_id = gu.id
        `+join+`
        WHERE t.status = 'succeeded' AND `+where+`
        GROUP BY t.user_id, u.id, gu.id, t.country
        ORDER BY total_spent DESC
        LIMIT 20`,
        args...).Scan(&top)

    return &models.CustomersReport{
        TotalCustomers:  totals.Total,
        RegisteredCount: totals.Registered,
        GuestCount:      totals.Guest,
        NewCustomers:    custStats.New,
        RepeatCustomers: custStats.Repeat,
        RepeatRate:      repeatRate,
        TopCustomers:    top,
    }
}