package models

import "github.com/google/uuid"

// ─────────────────────────────────────────────
// Shared primitives
// ─────────────────────────────────────────────

type DailySaleRecord struct {
	Date         string  `json:"date"`
	Revenue      float64 `json:"revenue"`
	TicketsSold  int64   `json:"tickets_sold"`
	Transactions int64   `json:"transactions"`
}

type CurrencyBreakdown struct {
	Currency     string  `json:"currency"`
	Revenue      float64 `json:"revenue"`
	Refunds      float64 `json:"refunds"`
	NetRevenue   float64 `json:"net_revenue"`
	Transactions int64   `json:"transactions"`
}

type CountryBreakdown struct {
	Country      string  `json:"country"`
	Revenue      float64 `json:"revenue"`
	Transactions int64   `json:"transactions"`
	TicketsSold  int64   `json:"tickets_sold"`
}

type PaymentMethodBreakdown struct {
	Method         string  `json:"method"`   // "card", "konbini", "bank_transfer"
	Provider       string  `json:"provider"` // "stripe", "konbini"
	Revenue        float64 `json:"revenue"`
	Transactions   int64   `json:"transactions"`
	PendingCount   int64   `json:"pending_count"` // unpaid konbini orders
	PendingAmount  float64 `json:"pending_amount"`
	ExpiredCount   int64   `json:"expired_count"`   // konbini expired without payment
	ConversionRate float64 `json:"conversion_rate"` // % of initiated that succeeded
}

type TicketTierSummary struct {
	TierID    uuid.UUID `json:"tier_id"`
	TierName  string    `json:"tier_name"`
	Price     float64   `json:"price"`
	Currency  string    `json:"currency"`
	Capacity  int64     `json:"capacity"`
	Sold      int64     `json:"sold"`
	Available int64     `json:"available"`
	SoldPct   float64   `json:"sold_percentage"`
	Revenue   float64   `json:"revenue"`
}

type TopCustomer struct {
	CustomerID       *uuid.UUID `json:"customer_id"`
	CustomerName     string     `json:"customer_name"`
	CustomerEmail    string     `json:"customer_email"`
	IsGuest          bool       `json:"is_guest"`
	Country          string     `json:"country"`
	TotalSpent       float64    `json:"total_spent"`
	TicketsPurchased int64      `json:"tickets_purchased"`
	OrderCount       int64      `json:"order_count"`
}

// ─────────────────────────────────────────────
// 1. Overview
// ─────────────────────────────────────────────

type AdminOverviewReport struct {
	TotalEvents       int64               `json:"total_events"`
	ActiveEvents      int64               `json:"active_events"`
	TotalOrganizers   int64               `json:"total_organizers"`
	TotalRevenue      float64             `json:"total_revenue"`
	TotalRefunds      float64             `json:"total_refunds"`
	NetRevenue        float64             `json:"net_revenue"`
	TotalTicketsSold  int64               `json:"total_tickets_sold"`
	PendingPayouts    float64             `json:"pending_payouts"`
	SalesTrend        []DailySaleRecord   `json:"sales_trend"`
	CurrencyBreakdown []CurrencyBreakdown `json:"currency_breakdown"`
}

type OrganizerOverviewReport struct {
	TotalEvents       int64               `json:"total_events"`
	ActiveEvents      int64               `json:"active_events"`
	TotalRevenue      float64             `json:"total_revenue"`
	TotalRefunds      float64             `json:"total_refunds"`
	NetRevenue        float64             `json:"net_revenue"`
	TotalTicketsSold  int64               `json:"total_tickets_sold"`
	PendingPayouts    float64             `json:"pending_payouts"`
	SalesTrend        []DailySaleRecord   `json:"sales_trend"`
	CurrencyBreakdown []CurrencyBreakdown `json:"currency_breakdown"`
}

// ─────────────────────────────────────────────
// 2. Sales
// ─────────────────────────────────────────────

type SalesReport struct {
	TotalRevenue      float64             `json:"total_revenue"`
	TotalTicketsSold  int64               `json:"total_tickets_sold"`
	TotalTransactions int64               `json:"total_transactions"`
	AverageOrderValue float64             `json:"average_order_value"`
	DailySales        []DailySaleRecord   `json:"daily_sales"`
	CurrencyBreakdown []CurrencyBreakdown `json:"currency_breakdown"`
	CountryBreakdown  []CountryBreakdown  `json:"country_breakdown"`
}

// ─────────────────────────────────────────────
// 3. Payments
// ─────────────────────────────────────────────

type PaymentsReport struct {
	TotalTransactions int64                    `json:"total_transactions"`
	SucceededCount    int64                    `json:"succeeded_count"`
	FailedCount       int64                    `json:"failed_count"`
	PendingCount      int64                    `json:"pending_count"` // konbini awaiting payment
	ExpiredCount      int64                    `json:"expired_count"` // konbini expired
	OverallConversion float64                  `json:"overall_conversion_rate"`
	MethodBreakdown   []PaymentMethodBreakdown `json:"method_breakdown"`
}

// ─────────────────────────────────────────────
// 4. Refunds
// ─────────────────────────────────────────────

type RefundStatusBreakdown struct {
	Status string  `json:"status"` // "succeeded", "pending", "failed"
	Count  int64   `json:"count"`
	Amount float64 `json:"amount"`
}

type RefundMethodBreakdown struct {
	Method string  `json:"method"` // "stripe", "konbini_reversal", "manual"
	Count  int64   `json:"count"`
	Amount float64 `json:"amount"`
}

type RefundsReport struct {
	TotalRefunds    float64                 `json:"total_refunds"`
	RefundCount     int64                   `json:"refund_count"`
	RefundRate      float64                 `json:"refund_rate"` // % of succeeded txns that were refunded
	AvgRefundAmount float64                 `json:"avg_refund_amount"`
	StatusBreakdown []RefundStatusBreakdown `json:"status_breakdown"`
	MethodBreakdown []RefundMethodBreakdown `json:"method_breakdown"`
}

// ─────────────────────────────────────────────
// 5. Event performance
// ─────────────────────────────────────────────

type EventPerformanceReport struct {
	EventID        uuid.UUID           `json:"event_id"`
	EventTitle     string              `json:"event_title"`
	Status         string              `json:"status"`
	Capacity       int64               `json:"capacity"`
	TicketsSold    int64               `json:"tickets_sold"`
	Available      int64               `json:"available"`
	SoldPercentage float64             `json:"sold_percentage"`
	Revenue        float64             `json:"revenue"`
	Refunds        float64             `json:"refunds"`
	NetRevenue     float64             `json:"net_revenue"`
	Transactions   int64               `json:"transactions"`
	TierBreakdown  []TicketTierSummary `json:"tier_breakdown"`
}

// ─────────────────────────────────────────────
// 6. Customers
// ─────────────────────────────────────────────

type CustomersReport struct {
	TotalCustomers  int64         `json:"total_customers"`
	RegisteredCount int64         `json:"registered_count"`
	GuestCount      int64         `json:"guest_count"`
	NewCustomers    int64         `json:"new_customers"`
	RepeatCustomers int64         `json:"repeat_customers"`
	RepeatRate      float64       `json:"repeat_rate"`
	TopCustomers    []TopCustomer `json:"top_customers"`
}
