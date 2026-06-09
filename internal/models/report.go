package models

// ─────────────────────────────────────────────
// Shared primitives
// ─────────────────────────────────────────────

type DailySaleRow struct {
	Date           string  `json:"date"`
	Currency       string  `json:"currency"`
	CurrencySymbol string  `json:"currency_symbol"`
	GrossRevenue   float64 `json:"gross_revenue"`
}

type SaleRow struct {
	Currency       string  `json:"currency"`
	CurrencySymbol string  `json:"currency_symbol"`
	GrossRevenue   float64 `json:"gross_revenue"`
	NetRevenue     float64 `json:"net_revenue"`
	PlatformFee    float64 `json:"platform_fee"`
	GatewayFee     float64 `json:"gateway_fee"`
	Refund         float64 `json:"refund"`
}

type FinanceRow struct {
	Currency       string  `json:"currency"`
	CurrencySymbol string  `json:"currency_symbol"`
	GrossRevenue   float64 `json:"gross_revenue"`
	NetRevenue     float64 `json:"net_revenue"`
	OrganizerShare float64 `json:"organizer_share"`
	PlatformFee    float64 `json:"platform_fee"`
	GatewayFee     float64 `json:"gateway_fee"`
	Refund         float64 `json:"refund"`
}

type PaymentMethodSummary struct {
	Name     string           `json:"name"`
	Earnings []PaymentEarning `json:"earnings"`
}

type PaymentEarning struct {
	Currency       string  `json:"currency"`
	CurrencySymbol string  `json:"currency_symbol"`
	GrossRevenue   float64 `json:"gross_revenue"`
	NetRevenue     float64 `json:"net_revenue"`
	OrganizerShare float64 `json:"organizer_share"`
	PlatformFee    float64 `json:"platform_fee"`
	GatewayFee     float64 `json:"gateway_fee"`
	Refund         float64 `json:"refund"`
}

// ─────────────────────────────────────────────
// 1. Overview
// ─────────────────────────────────────────────

type AdminOverviewReport struct {
	Events       map[string]int64 `json:"events"`
	Users        map[string]int64 `json:"users"`
	Guests       map[string]int64 `json:"guests"`
	Organizers   map[string]int64 `json:"organizers"`
	Transactions map[string]int64 `json:"transactions"`
	Refunds      map[string]int64 `json:"refunds"`
	Billing      map[string]int64 `json:"billing"`
	Payouts      map[string]int64 `json:"payouts"`
	SalesTrend   []DailySaleRow   `json:"sales_trend"`
}

type OrganizerOverviewReport struct {
	Events       map[string]int64 `json:"events"`
	Transactions map[string]int64 `json:"transactions"`
	Refunds      map[string]int64 `json:"refunds"`
	Billing      map[string]int64 `json:"billing"`
	Payouts      map[string]int64 `json:"payouts"`
	SalesTrend   []DailySaleRow   `json:"sales_trend"`
}

// ─────────────────────────────────────────────
// 2. Sales
// ─────────────────────────────────────────────

type SalesReport struct {
	Sales      []SaleRow      `json:"sales"`
	DailySales []DailySaleRow `json:"daily_sales"`
}

// ─────────────────────────────────────────────
// 3. Customer Analytics
// ─────────────────────────────────────────────

type CustomerAnalyticsReport struct {
	Users          map[string]int64 `json:"users"`
	Guests         map[string]int64 `json:"guests"`
	TotalCustomers int64            `json:"total_customers"`
	NewCustomers   int64            `json:"new_customers"`
	RepeatRate     float64          `json:"repeat_rate"`
	TopActors      []TopActor       `json:"top_actors"`
}

type TopActor struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Email        string       `json:"email"`
	Type         string       `json:"type"` // "user" | "guest"
	Spending     []ActorSpend `json:"spending"`
	TotalTickets int64        `json:"total_tickets"`
}

type ActorSpend struct {
	Currency       string  `json:"currency"`
	CurrencySymbol string  `json:"currency_symbol"`
	TotalSpent     float64 `json:"total_spent"`
	Tickets        int64   `json:"tickets"`
	EventNames     string  `json:"event_names"`
}

// ─────────────────────────────────────────────
// 4. Financial
// ─────────────────────────────────────────────

type FinancialReport struct {
	Finances       []FinanceRow           `json:"finances"`
	PaymentMethods []PaymentMethodSummary `json:"payment_methods"`
}

// ─────────────────────────────────────────────
// 5. Event Performance
// ─────────────────────────────────────────────

type EventPerformanceReport struct {
	EventID        string           `json:"event_id"`
	EventTitle     string           `json:"event_title"`
	Status         string           `json:"status"`
	Currency       string           `json:"currency"`
	CurrencySymbol string           `json:"currency_symbol"`
	TicketsSold    int64            `json:"tickets_sold"`
	Revenue        float64          `json:"revenue"`
	Transactions   int64            `json:"transactions"`
	SoldPercentage float64          `json:"sold_percentage"`
	CheckedIn      int64            `json:"checked_in"`
	TierBreakdown  []TierPerformRow `json:"tier_breakdown"`
}

type TierPerformRow struct {
	TierName string  `json:"tier_name"`
	Capacity int     `json:"capacity"`
	Sold     int64   `json:"sold"`
	Revenue  float64 `json:"revenue"`
	SoldPct  float64 `json:"sold_percentage"`
}
