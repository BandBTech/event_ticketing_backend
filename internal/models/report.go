package models

// ─────────────────────────────────────────────
// Simplified report models — all currency-binded
// ─────────────────────────────────────────────

// DailySaleRecord — daily sales per currency
type DailySaleRecord struct {
	Date         string  `json:"date"`
	Currency     string  `json:"currency"`
	Revenue      float64 `json:"revenue"`
	TicketsSold  int64   `json:"tickets_sold"`
	Transactions int64   `json:"transactions"`
}

// CurrencySnapshot — single currency financials
type CurrencySnapshot struct {
	Currency   string  `json:"currency"`
	Gross      float64 `json:"gross_revenue"`
	Fees       float64 `json:"fees"`
	Refunds    float64 `json:"refunds"`
	Net        float64 `json:"net_revenue"`
	TicketsSold int64  `json:"tickets_sold"`
	Transactions int64 `json:"transactions"`
}

// ── Overview ──

// AdminOverviewReport — all numbers per currency
type AdminOverviewReport struct {
	Events    map[string]int64  `json:"events"`     // {"total": 120, "active": 45}
	Organizers map[string]int64 `json:"organizers"` // {"total": 30}
	Currencies []CurrencySnapshot `json:"currencies"`
	Trend     []DailySaleRecord `json:"sales_trend"`
}

// OrganizerOverviewReport
type OrganizerOverviewReport struct {
	Events    map[string]int64  `json:"events"`
	Currencies []CurrencySnapshot `json:"currencies"`
	Trend     []DailySaleRecord `json:"sales_trend"`
}

// ── Sales ──

type SalesReport struct {
	Currencies []CurrencySnapshot `json:"currencies"`
	DailySales []DailySaleRecord  `json:"daily_sales"`
}

// ── Customer Analytics ──

type CustomerAnalyticsReport struct {
	Users         map[string]int64 `json:"users"`          // {"total": N, "active": N, "inactive": N}
	Guests        map[string]int64 `json:"guests"`         // {"total": N, "active": N, "inactive": N}
	TotalCustomers int64           `json:"total_customers"`
	NewCustomers   int64           `json:"new_customers"`
	RepeatRate     float64         `json:"repeat_rate"`
}

// ── Financial ──

type FinancialReport struct {
	Currencies  []CurrencySnapshot       `json:"currencies"`
	Billing     map[string]interface{}   `json:"billing"`     // {"total": N, "pending": N, "paid": N}
	Payouts     map[string]interface{}   `json:"payouts"`     // {"total": N, "pending": N, "paid": N}
	PaymentMethods []PaymentMethodSummary `json:"payment_methods"`
}

type PaymentMethodSummary struct {
	Name     string              `json:"name"`     // "stripe", "konbini"
	Earnings []PaymentEarning    `json:"earnings"` // per-currency
}

type PaymentEarning struct {
	Currency string  `json:"currency"`
	Gross    float64 `json:"gross_revenue"`
	Fees     float64 `json:"fees"`
	Net      float64 `json:"net_revenue"`
}

// ── Event Performance ──

type EventPerformanceReport struct {
	EventID        string            `json:"event_id"`
	EventTitle     string            `json:"event_title"`
	Status         string            `json:"status"`
	Currency       string            `json:"currency"`
	TicketsSold    int64             `json:"tickets_sold"`
	Revenue        float64           `json:"revenue"`
	Transactions   int64             `json:"transactions"`
	SoldPercentage float64           `json:"sold_percentage"`
	CheckedIn      int64             `json:"checked_in"`
	TierBreakdown  []TierPerformance `json:"tier_breakdown"`
}

type TierPerformance struct {
	TierName   string  `json:"tier_name"`
	Capacity   int     `json:"capacity"`
	Sold       int64   `json:"sold"`
	Revenue    float64 `json:"revenue"`
	SoldPct    float64 `json:"sold_percentage"`
}