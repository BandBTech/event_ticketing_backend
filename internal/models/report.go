package models

import (
	"time"

	"github.com/google/uuid"
)

// ===========================================
// REQUEST/FILTER MODELS
// ===========================================

// ReportFilter represents filtering and pagination options for reports
type ReportFilter struct {
	StartDate   time.Time  `form:"start_date" time_format:"2006-01-02"`
	EndDate     time.Time  `form:"end_date" time_format:"2006-01-02"`
	EventID     *uuid.UUID `form:"event_id"`
	OrganizerID *uuid.UUID `form:"organizer_id"`
	Status      *string    `form:"status"` // Event/Transaction status filter
	SortBy      string     `form:"sort_by" binding:"omitempty"`
	SortOrder   string     `form:"sort_order" binding:"omitempty,oneof=asc desc"` // asc or desc
	Page        int        `form:"page" binding:"omitempty,min=1"`
	PageSize    int        `form:"page_size" binding:"omitempty,min=1,max=100"`
}

// ===========================================
// REPORT RESPONSE MODELS
// ===========================================

// AdminOverviewReport represents the admin overview dashboard
type AdminOverviewReport struct {
	SummaryMetrics      SummaryMetrics            `json:"summary_metrics"`
	TopPerformingEvents []TopPerformingEvent      `json:"top_performing_events"`
	RecentTransactions  []RecentTransactionRecord `json:"recent_transactions"`
	RevenueTrend        []MonthlyTrendData        `json:"revenue_trend"`
	TicketSalesTrend    []MonthlyTicketSaleData   `json:"ticket_sales_trend"`
	EventsStatistics    EventsStatistics          `json:"events_statistics"`
}

// OrganizerOverviewReport represents the organizer overview dashboard
type OrganizerOverviewReport struct {
	SummaryMetrics      OrganizerSummaryMetrics  `json:"summary_metrics"`
	TopPerformingEvents []TopPerformingEvent     `json:"top_performing_events"`
	RevenueTrend        []MonthlyTrendData       `json:"revenue_trend"`
	TicketSalesTrend    []MonthlyTicketSaleData  `json:"ticket_sales_trend"`
	EventsStatistics    OrganizerEventStatistics `json:"events_statistics"`
}

// SummaryMetrics represents key metrics for admin dashboard
type SummaryMetrics struct {
	TotalRevenue          float64 `json:"total_revenue"`
	TotalCommission       float64 `json:"total_commission"`
	OrganizerShare        float64 `json:"organizer_share"`
	TotalTicketsSold      int64   `json:"total_tickets_sold"`
	ActiveEvents          int64   `json:"active_events"`
	TotalEvents           int64   `json:"total_events"`
	TotalTransactions     int64   `json:"total_transactions"`
	CompletedTransactions int64   `json:"completed_transactions"`
	PendingTransactions   int64   `json:"pending_transactions"`
	FailedTransactions    int64   `json:"failed_transactions"`
	AverageOrderValue     float64 `json:"average_order_value"`
	ConversionRate        float64 `json:"conversion_rate"`
}

// OrganizerSummaryMetrics represents key metrics for organizer dashboard
type OrganizerSummaryMetrics struct {
	TotalRevenue           float64 `json:"total_revenue"`
	TotalEarnings          float64 `json:"total_earnings"`
	TotalRefunds           float64 `json:"total_refunds"`
	NetRevenue             float64 `json:"net_revenue"`
	NetEarnings            float64 `json:"net_earnings"`
	TotalTicketsSold       int64   `json:"total_tickets_sold"`
	ActiveEvents           int64   `json:"active_events"`
	TotalEvents            int64   `json:"total_events"`
	TotalTransactions      int64   `json:"total_transactions"`
	CompletedTransactions  int64   `json:"completed_transactions"`
	PendingPayouts         float64 `json:"pending_payouts"`
	AverageTicketsPerEvent float64 `json:"average_tickets_per_event"`
	ConversionRate         float64 `json:"conversion_rate"`
}

// TopPerformingEvent represents an event in the top performing events list
type TopPerformingEvent struct {
	EventID               uuid.UUID `json:"event_id"`
	EventTitle            string    `json:"event_title"`
	BannerImage           string    `json:"banner_image"`
	OrganizerID           uuid.UUID `json:"organizer_id"`
	OrganizerName         string    `json:"organizer_name"`
	OrganizerBusinessName string    `json:"organizer_business_name"`
	OrganizerLogo         string    `json:"organizer_logo"`
	TicketsSold           int64     `json:"tickets_sold"`
	Revenue               float64   `json:"revenue"`
	Conversion            float64   `json:"conversion"` // Conversion rate as percentage
	Status                string    `json:"status"`     // Event status
	StartDate             time.Time `json:"start_date"`
	Rank                  int       `json:"rank"`
}

// RecentTransactionRecord represents a recent transaction
type RecentTransactionRecord struct {
	TransactionID  uuid.UUID `json:"transaction_id"`
	EventID        uuid.UUID `json:"event_id"`
	EventTitle     string    `json:"event_title"`
	CustomerName   string    `json:"customer_name"`
	CustomerEmail  string    `json:"customer_email"`
	Amount         float64   `json:"amount"`
	TicketQuantity int       `json:"ticket_quantity"`
	PaymentGateway string    `json:"payment_gateway"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

// MonthlyTrendData represents monthly revenue trend data
type MonthlyTrendData struct {
	Month   string  `json:"month"` // Jan, Feb, Mar etc.
	Year    int     `json:"year"`
	Revenue float64 `json:"revenue"` // Total revenue in NPR or other currency
}

// MonthlyTicketSaleData represents monthly ticket sales data
type MonthlyTicketSaleData struct {
	Month        string `json:"month"` // Jan, Feb, Mar etc.
	Year         int    `json:"year"`
	TicketsSold  int64  `json:"tickets_sold"`  // Total tickets sold
	DisplayLabel string `json:"display_label"` // e.g., "580 tickets"
}

// EventsStatistics represents event statistics
type EventsStatistics struct {
	TotalEvents     int64 `json:"total_events"`
	DraftEvents     int64 `json:"draft_events"`
	PendingEvents   int64 `json:"pending_events"`
	ApprovedEvents  int64 `json:"approved_events"`
	RejectedEvents  int64 `json:"rejected_events"`
	OnSaleEvents    int64 `json:"on_sale_events"`
	LiveEvents      int64 `json:"live_events"`
	CompletedEvents int64 `json:"completed_events"`
	CancelledEvents int64 `json:"cancelled_events"`
}

// OrganizerEventStatistics represents event statistics for organizers
type OrganizerEventStatistics struct {
	TotalEvents     int64 `json:"total_events"`
	DraftEvents     int64 `json:"draft_events"`
	PendingEvents   int64 `json:"pending_events"`
	ApprovedEvents  int64 `json:"approved_events"`
	RejectedEvents  int64 `json:"rejected_events"`
	OnSaleEvents    int64 `json:"on_sale_events"`
	LiveEvents      int64 `json:"live_events"`
	CompletedEvents int64 `json:"completed_events"`
	CancelledEvents int64 `json:"cancelled_events"`
}

// ===========================================
// SALES REPORT MODELS
// ===========================================

// SalesReportData represents the sales report view
type SalesReportData struct {
	SummaryMetrics        SummaryMetrics        `json:"summary_metrics"`
	DailySales            []DailySaleRecord     `json:"daily_sales"`
	SalesByPaymentGateway []PaymentGatewayStats `json:"sales_by_payment_gateway"`
}

// DailySaleRecord represents daily sales data
type DailySaleRecord struct {
	Date              time.Time `json:"date"`
	Revenue           float64   `json:"revenue"`
	TicketsSold       int64     `json:"tickets_sold"`
	Transactions      int64     `json:"transactions"`
	AverageOrderValue float64   `json:"average_order_value"`
}

// PaymentGatewayStats represents sales statistics by payment gateway
type PaymentGatewayStats struct {
	GatewayName       string  `json:"gateway_name"`
	TotalTransactions int64   `json:"total_transactions"`
	TotalRevenue      float64 `json:"total_revenue"`
	PercentageOfTotal float64 `json:"percentage_of_total"`
	Status            string  `json:"status"`
}

// ===========================================
// EVENT PERFORMANCE MODELS
// ===========================================

// EventPerformanceReport represents event performance metrics
type EventPerformanceReport struct {
	EventID            uuid.UUID         `json:"event_id"`
	EventTitle         string            `json:"event_title"`
	BannerImage        string            `json:"banner_image"`
	Status             string            `json:"status"`
	StartDate          time.Time         `json:"start_date"`
	EndDate            time.Time         `json:"end_date"`
	Capacity           int               `json:"capacity"`
	TicketsSold        int64             `json:"tickets_sold"`
	SoldPercentage     float64           `json:"sold_percentage"`
	Revenue            float64           `json:"revenue"`
	Commission         float64           `json:"commission"`
	OrganizerEarnings  float64           `json:"organizer_earnings"`
	AverageTicketPrice float64           `json:"average_ticket_price"`
	TotalTransactions  int64             `json:"total_transactions"`
	TopTier            TierPerformance   `json:"top_tier"`
	TierPerformance    []TierPerformance `json:"tier_performance"`
	RevenueByTier      []RevenueByTier   `json:"revenue_by_tier"`
}

// TierPerformance represents performance metrics for an event tier
type TierPerformance struct {
	TierID         uuid.UUID `json:"tier_id"`
	TierName       string    `json:"tier_name"`
	TicketPrice    float64   `json:"ticket_price"`
	TicketCapacity int       `json:"ticket_capacity"`
	TicketsSold    int64     `json:"tickets_sold"`
	SoldPercentage float64   `json:"sold_percentage"`
	Revenue        float64   `json:"revenue"`
}

// RevenueByTier represents revenue breakdown by tier for an event
type RevenueByTier struct {
	TierID   uuid.UUID `json:"tier_id"`
	TierName string    `json:"tier_name"`
	Revenue  float64   `json:"revenue"`
}

// ===========================================
// CUSTOMER ANALYTICS MODELS
// ===========================================

// CustomerAnalyticsReport represents customer analytics data
type CustomerAnalyticsReport struct {
	TotalCustomers    int64             `json:"total_customers"`
	RegisteredUsers   int64             `json:"registered_users"`
	GuestPurchases    int64             `json:"guest_purchases"`
	RepeatCustomers   int64             `json:"repeat_customers"`
	AverageOrderValue float64           `json:"average_order_value"`
	CustomerSegments  []CustomerSegment `json:"customer_segments"`
	TopCustomers      []TopCustomer     `json:"top_customers"`
	CustomerRetention CustomerRetention `json:"customer_retention"`
}

// CustomerSegment represents customer segmentation analysis
type CustomerSegment struct {
	SegmentName       string  `json:"segment_name"` // e.g., "High Value", "Regular", "One-time"
	CustomerCount     int64   `json:"customer_count"`
	TotalSpent        float64 `json:"total_spent"`
	AverageSpent      float64 `json:"average_spent"`
	PercentageOfTotal float64 `json:"percentage_of_total"`
}

// TopCustomer represents top spending customers
type TopCustomer struct {
	CustomerID       *uuid.UUID `json:"customer_id"`
	CustomerName     string     `json:"customer_name"`
	CustomerEmail    string     `json:"customer_email"`
	TotalSpent       float64    `json:"total_spent"`
	TicketsPurchased int64      `json:"tickets_purchased"`
	EventsAttended   int64      `json:"events_attended"`
	LastPurchaseDate time.Time  `json:"last_purchase_date"`
}

// CustomerRetention represents customer retention metrics
type CustomerRetention struct {
	NewCustomers       int64   `json:"new_customers"`
	ReturningCustomers int64   `json:"returning_customers"`
	RetentionRate      float64 `json:"retention_rate"` // Percentage returning customers
	ChurnRate          float64 `json:"churn_rate"`     // Percentage of customers who didn't return
}

// ===========================================
// FINANCIAL REPORT MODELS
// ===========================================

// FinancialReport represents comprehensive financial data
type FinancialReport struct {
	SummaryMetrics    FinancialSummary   `json:"summary_metrics"`
	RevenueBreakdown  []RevenueBreakdown `json:"revenue_breakdown"`
	CommissionHistory []CommissionRecord `json:"commission_history"`
	BillHistory       []BillRecord       `json:"bill_history"`
}

// FinancialSummary represents financial summary
type FinancialSummary struct {
	TotalGrossRevenue   float64 `json:"total_gross_revenue"`
	TotalCommission     float64 `json:"total_commission"`
	TotalOrganizerShare float64 `json:"total_organizer_share"`
	TotalRefunds        float64 `json:"total_refunds"`
	NetRevenue          float64 `json:"net_revenue"`
	PendingPayouts      float64 `json:"pending_payouts"`
	CompletedPayouts    float64 `json:"completed_payouts"`
	AverageTicketPrice  float64 `json:"average_ticket_price"`
	TotalTransactions   int64   `json:"total_transactions"`
}

// RevenueBreakdown represents revenue breakdown by event
type RevenueBreakdown struct {
	EventTitle     string  `json:"event_title"`
	GrossRevenue   float64 `json:"gross_revenue"`
	Commission     float64 `json:"commission"`
	OrganizerShare float64 `json:"organizer_share"`
	Refunds        float64 `json:"refunds"`
	NetRevenue     float64 `json:"net_revenue"`
}

// CommissionRecord represents a commission calculation record
type CommissionRecord struct {
	ID               uuid.UUID `json:"id"`
	EventID          uuid.UUID `json:"event_id"`
	EventTitle       string    `json:"event_title"`
	Revenue          float64   `json:"revenue"`
	CommissionAmount float64   `json:"commission_amount"`
	CommissionRate   float64   `json:"commission_rate"`
	CreatedAt        time.Time `json:"created_at"`
}

// BillRecord represents a payment bill record
type BillRecord struct {
	ID          uuid.UUID  `json:"id"`
	BillNumber  string     `json:"bill_number"`
	Amount      float64    `json:"amount"`
	Status      string     `json:"status"`
	ProcessedAt *time.Time `json:"processed_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// PayoutRecord represents a payout record (deprecated, use BillRecord instead)
type PayoutRecord struct {
	Amount      float64    `json:"amount"`
	Status      string     `json:"status"`
	ProcessedAt *time.Time `json:"processed_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// CurrencyFinancial represents financial data for a specific currency
type CurrencyFinancial struct {
	Currency          string  `json:"currency"`
	GrossRevenue      float64 `json:"gross_revenue"`
	Commission        float64 `json:"commission"`
	OrganizerShare    float64 `json:"organizer_share"`
	TransactionCount  int64   `json:"transaction_count"`
	PercentageOfTotal float64 `json:"percentage_of_total"`
}

// ===========================================
// ORGANIZER DASHBOARD MODELS
// ===========================================

// OrganizerDashboardReport represents comprehensive organizer dashboard data
type OrganizerDashboardReport struct {
	SummaryMetrics     OrganizerDashboardMetrics `json:"summary_metrics"`
	TicketsByEvent     []TicketsByEventData      `json:"tickets_by_event"`     // For graph
	RevenueOverTime    []RevenueOverTimeData     `json:"revenue_over_time"`    // For graph/trend
	EventPerformance   []EventPerformanceSummary `json:"event_performance"`    // Events breakdown
	AttendanceMetrics  AttendanceMetrics         `json:"attendance_metrics"`   // Attendee data
	PaymentMethodStats []PaymentMethodData       `json:"payment_method_stats"` // Payment breakdown
	TopEventTiers      []TopEventTierData        `json:"top_event_tiers"`      // Best performing tiers
	UpcomingEvents     []UpcomingEventData       `json:"upcoming_events"`      // Next scheduled events
}

// OrganizerDashboardMetrics represents key summary metrics for organizer
type OrganizerDashboardMetrics struct {
	TotalRevenue           float64 `json:"total_revenue"`
	TotalEarnings          float64 `json:"total_earnings"` // After commission
	TotalEvents            int64   `json:"total_events"`
	ActiveEvents           int64   `json:"active_events"` // Live or on-sale events
	TotalTicketsSold       int64   `json:"total_tickets_sold"`
	TotalAttendees         int64   `json:"total_attendees"` // Unique attendees
	AverageTicketPrice     float64 `json:"average_ticket_price"`
	ConversionRate         float64 `json:"conversion_rate"` // Percentage
	AverageEventRevenue    float64 `json:"average_event_revenue"`
	AverageTicketsPerEvent float64 `json:"average_tickets_per_event"`
	TotalTicketsCapacity   int64   `json:"total_tickets_capacity"` // Total capacity across all events
	OccupancyRate          float64 `json:"occupancy_rate"`         // Percentage of capacity used
}

// TicketsByEventData represents ticket sales broken down by individual events
type TicketsByEventData struct {
	EventID      uuid.UUID `json:"event_id"`
	EventTitle   string    `json:"event_title"`
	TicketsSold  int64     `json:"tickets_sold"`
	EventRevenue float64   `json:"event_revenue"`
	Attendees    int64     `json:"attendees"`     // Unique attendee count for event
	Status       string    `json:"status"`        // Event status
	DisplayLabel string    `json:"display_label"` // For UI display, e.g., "1,234 tickets"
}

// RevenueOverTimeData represents revenue aggregated over time periods for graphing
type RevenueOverTimeData struct {
	Period           string  `json:"period"`         // YYYY-MM format for month, YYYY-MM-DD for day
	Revenue          float64 `json:"revenue"`        // Total platform revenue
	OrganizerEarn    float64 `json:"organizer_earn"` // Organizer portion after commission
	TicketsSold      int64   `json:"tickets_sold"`
	TransactionCount int64   `json:"transaction_count"`
}

// EventPerformanceSummary represents summary performance data for each event
type EventPerformanceSummary struct {
	EventID            uuid.UUID `json:"event_id"`
	EventTitle         string    `json:"event_title"`
	BannerImage        string    `json:"banner_image"`
	Status             string    `json:"status"`
	StartDate          time.Time `json:"start_date"`
	TicketsCapacity    int       `json:"tickets_capacity"`
	TicketsSold        int64     `json:"tickets_sold"`
	SoldPercentage     float64   `json:"sold_percentage"`
	Revenue            float64   `json:"revenue"`
	OrganizerEarnings  float64   `json:"organizer_earnings"`
	Attendees          int64     `json:"attendees"`
	ConversionRate     float64   `json:"conversion_rate"`
	AverageTicketPrice float64   `json:"average_ticket_price"`
	TotalTransactions  int64     `json:"total_transactions"`
	RefundedCount      int64     `json:"refunded_count"`
}

// AttendanceMetrics represents attendee-related metrics
type AttendanceMetrics struct {
	TotalAttendees      int64   `json:"total_attendees"`      // Unique attendees across all events
	RegisteredAttendees int64   `json:"registered_attendees"` // User account holders
	GuestAttendees      int64   `json:"guest_attendees"`      // Non-registered/anonymous
	AveragePerEvent     float64 `json:"average_per_event"`    // Average attendees per event
	RepeatAttendeeRate  float64 `json:"repeat_attendee_rate"` // % of attendees who bought multiple tickets
	NewAttendees        int64   `json:"new_attendees"`        // First-time ticket buyers
	ReturningAttendees  int64   `json:"returning_attendees"`  // Repeat buyers
}

// PaymentMethodData represents payment method distribution
type PaymentMethodData struct {
	PaymentMethod     string  `json:"payment_method"` // e.g., stripe, khalti, etc
	TransactionCount  int64   `json:"transaction_count"`
	TotalAmount       float64 `json:"total_amount"`
	PercentageOfTotal float64 `json:"percentage_of_total"`
	SuccessRate       float64 `json:"success_rate"` // Percentage of successful transactions
}

// TopEventTierData represents top-performing event tiers
type TopEventTierData struct {
	EventID        uuid.UUID `json:"event_id"`
	EventTitle     string    `json:"event_title"`
	TierID         uuid.UUID `json:"tier_id"`
	TierName       string    `json:"tier_name"`
	TicketPrice    float64   `json:"ticket_price"`
	TicketCapacity int       `json:"ticket_capacity"`
	TicketsSold    int64     `json:"tickets_sold"`
	SoldPercentage float64   `json:"sold_percentage"`
	Revenue        float64   `json:"revenue"`
	Rank           int       `json:"rank"`
}

// UpcomingEventData represents upcoming organizer events
type UpcomingEventData struct {
	EventID        uuid.UUID `json:"event_id"`
	EventTitle     string    `json:"event_title"`
	BannerImage    string    `json:"banner_image"`
	Status         string    `json:"status"`
	StartDate      time.Time `json:"start_date"`
	TicketCapacity int       `json:"ticket_capacity"`
	TicketsSold    int64     `json:"tickets_sold"`
	SoldPercentage float64   `json:"sold_percentage"`
	DaysUntilStart int       `json:"days_until_start"`
}
