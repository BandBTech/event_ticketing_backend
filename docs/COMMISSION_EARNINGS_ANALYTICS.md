# Commission Earnings Analytics

## Overview

The analytics endpoints now include **commission earning calculations** for both admin and organizer views. This allows platform administrators to track earnings and organizers to understand the split between their revenue and platform commissions.

## New Fields

### Event-Level Analytics

Both organizer and admin analytics now include:

| Field                | Type    | Description                                    |
| -------------------- | ------- | ---------------------------------------------- |
| `commission_earning` | float64 | Platform commission earned from this event     |
| `organizer_share`    | float64 | Organizer's revenue after commission deduction |
| `total_revenue`      | float64 | Total ticket sales revenue (before commission) |
| `commission_rate`    | float64 | Commission percentage (0-100)                  |

### Tier-Level Analytics

Each tier breakdown now includes:

| Field                | Type    | Description                                |
| -------------------- | ------- | ------------------------------------------ |
| `commission_earning` | float64 | Platform commission from this tier's sales |
| `revenue`            | float64 | Revenue from this tier (before commission) |

## Calculation Logic

### Commission Earning Formula

```
commission_earning = total_revenue × (commission_rate / 100)
organizer_share = total_revenue - commission_earning
```

### Example

If an event sells:

- Total Revenue: $1,000
- Commission Rate: 10%

Then:

- Commission Earning: $1,000 × (10/100) = **$100** ← Platform gets this
- Organizer Share: $1,000 - $100 = **$900** ← Organizer gets this

### Multi-Tier Example

Event with 3 tiers:

| Tier       | Revenue    | Commission Rate | Commission Earning | Organizer Share |
| ---------- | ---------- | --------------- | ------------------ | --------------- |
| Early Bird | $500       | 10%             | $50                | $450            |
| Standard   | $300       | 10%             | $30                | $270            |
| VIP        | $200       | 10%             | $20                | $180            |
| **TOTAL**  | **$1,000** |                 | **$100**           | **$900**        |

Each tier's commission is calculated independently and summed at event level.

## API Endpoints

### Organizer Event Analytics

```
GET /api/v1/organizer/events/{id}/analytics
```

**Response Structure:**

```json
{
  "code": 200,
  "message": "Success",
  "data": {
    "event_id": "uuid",
    "event_title": "Summer Festival 2024",
    "event_status": "on_sale",
    "sales_status": "active",
    "total_seats": 5000,
    "sold_seats": 3800,
    "available_seats": 1200,
    "total_revenue": 95000.0,
    "commission_rate": 10.0,
    "commission_earning": 9500.0,
    "organizer_share": 85500.0,
    "tier_count": 3,
    "tiers": [
      {
        "tier_id": "uuid",
        "tier_name": "Early Bird",
        "price": 25.0,
        "currency": "USD",
        "total_seats": 2000,
        "sold_seats": 2000,
        "available_seats": 0,
        "revenue": 50000.0,
        "commission_earning": 5000.0,
        "sales_start": "2024-01-01T00:00:00Z",
        "sales_end": "2024-02-28T23:59:59Z",
        "is_active": false
      },
      {
        "tier_id": "uuid",
        "tier_name": "Standard",
        "price": 35.0,
        "currency": "USD",
        "total_seats": 2500,
        "sold_seats": 1500,
        "available_seats": 1000,
        "revenue": 35000.0,
        "commission_earning": 3500.0,
        "sales_start": "2024-03-01T00:00:00Z",
        "sales_end": "2024-05-31T23:59:59Z",
        "is_active": true
      },
      {
        "tier_id": "uuid",
        "tier_name": "VIP",
        "price": 50.0,
        "currency": "USD",
        "total_seats": 500,
        "sold_seats": 300,
        "available_seats": 200,
        "revenue": 10000.0,
        "commission_earning": 1000.0,
        "sales_start": "2024-03-01T00:00:00Z",
        "sales_end": "2024-06-20T23:59:59Z",
        "is_active": true
      }
    ],
    "created_at": "2024-01-10T10:00:00Z"
  }
}
```

### Admin Event Analytics

```
GET /api/v1/admin/events/{id}/analytics
```

Same response structure as organizer endpoint, but admin can access any event (no organizer scoping).

### All Events Analytics (Organizer)

For organizers with multiple events, use the reporting system:

```
GET /api/v1/organizer/reports?report_type=event-performance
```

This returns analytics for all events of the authenticated organizer, including commission earnings.

## Implementation Details

### Files Modified

1. **`/internal/models/event_tier.go`**
   - Added `CommissionEarning float64` to `EventTierAnalytics`
   - Added `CommissionEarning float64` to `EventAnalyticsResponse`

2. **`/internal/services/event_management_service.go`**
   - Updated `buildEventAnalytics()` - Calculates commission earning for each tier and event total
   - Updated `GetAllEventsAnalytics()` - Consistent calculation for paginated event lists
   - Uses formula: `commissionEarning = revenue * commissionRate / 100`

3. **`/internal/handlers/event_handler.go`**
   - Updated Swagger docs for `GetEventAnalytics()` (organizer)
   - Updated Swagger docs for `AdminGetEventAnalytics()` (admin)

### Calculation Consistency

- **Tier-level calculation**: Each tier's commission is calculated independently
- **Event-level calculation**: Sum of all tier commissions
- **Data source**: Calculated from actual ticket sales (from `tickets` table)
- **Status filter**: Only counts tickets with `payment_status = 'completed'` OR `status = 'active'`

## Use Cases

### For Platform Admins

1. **Revenue Tracking**: Monitor total platform commission earnings across all events
2. **Financial Reporting**: Generate commission revenue reports by organizer, category, or time period
3. **Dashboard Analytics**: Display real-time commission earning statistics
4. **Financial Planning**: Forecast cash flow based on event tier sales patterns

### For Organizers

1. **Revenue Understanding**: See exactly what commission is deducted from sales
2. **Payout Estimation**: Calculate expected payout from event sales
3. **Tier Performance**: Compare commission impact across different pricing tiers
4. **Financial Planning**: Project income from upcoming events and sales periods

## Example Workflows

### Scenario 1: Organizer Checks Event Performance

```bash
# Organizer views analytics for their event
curl -X GET "https://api.example.com/api/v1/organizer/events/550e8400-e29b-41d4-a716-446655440000/analytics" \
  -H "Authorization: Bearer ORGANIZER_TOKEN"

# Response includes:
# - total_revenue: $95,000
# - commission_earning: $9,500 (kept by platform)
# - organizer_share: $85,500 (paid to organizer)
```

### Scenario 2: Admin Monitors Commission Income

```bash
# Admin views analytics for verification
curl -X GET "https://api.example.com/api/v1/admin/events/550e8400-e29b-41d4-a716-446655440000/analytics" \
  -H "Authorization: Bearer ADMIN_TOKEN"

# Response shows breakdown by tier with commission earning for each
```

### Scenario 3: Organizer Uses Dashboard

Dashboard displays:

- Total Revenue: $95,000
- Commission Rate: 10%
- **Commission Earning: $9,500** ← NEW
- Your Earnings: $85,500

## Related Documentation

- [PAYMENT_SYSTEM_OVERVIEW.md](PAYMENT_SYSTEM_OVERVIEW.md) - Payment processing
- [API_ENDPOINTS_SORTING_REFERENCE.md](API_ENDPOINTS_SORTING_REFERENCE.md) - Analytics endpoints
- [PAYMENT_REDESIGN_COMPLETE.md](PAYMENT_REDESIGN_COMPLETE.md) - Revenue calculation details
- [API.md](API.md) - Complete API reference

## Field Reference

### EventAnalyticsResponse Fields

```go
type EventAnalyticsResponse struct {
    EventID              uuid.UUID            // Event unique identifier
    EventTitle           string               // Event name
    EventStatus          string               // draft, pending, approved, on_sale, live, etc.
    SalesStatus          string               // active, paused, stopped
    TotalSeats           int                  // Total capacity across all tiers
    SoldSeats            int                  // Total tickets sold
    AvailSeats           int                  // Total available tickets
    TotalRevenue         float64              // Sum of revenue from all tiers
    CommissionRate       float64              // Platform commission percentage (0-100)
    CommissionEarning    float64              // Platform's total commission (NEW)
    OrganizerShare       float64              // Organizer's share after commission
    TierCount            int                  // Number of pricing tiers
    Tiers                []EventTierAnalytics // Per-tier breakdown
    CreatedAt            time.Time            // Event creation timestamp
}

type EventTierAnalytics struct {
    TierID              uuid.UUID   // Tier unique identifier
    TierName            string      // Tier name (e.g., "Early Bird", "Standard")
    Price               float64     // Ticket price per seat
    Currency            string      // ISO 4217 currency code
    TotalSeats          int         // Total capacity
    SoldSeats           int         // Tickets sold
    AvailSeats          int         // Remaining available
    Revenue             float64     // Ticket sales revenue
    SalesStart          *time.Time  // When sales open
    SalesEnd            *time.Time  // When sales close
    IsActive            bool        // Is tier currently active
    CommissionEarning   float64     // Platform commission for this tier (NEW)
}
```

## Verification Checklist

- ✅ Commission earning calculated correctly: `commission_earning = revenue × rate / 100`
- ✅ Organizer share calculated correctly: `organizer_share = revenue - commission_earning`
- ✅ Tier-level commission earnings calculated independently
- ✅ Event-level totals sum all tier values
- ✅ Both organizer and admin endpoints return commission data
- ✅ Swagger documentation updated with new fields
- ✅ Build passes without errors

## Future Enhancements

1. **Commission History**: Track commission rate changes over time
2. **Tiered Commission**: Support different commission rates by event category/organizer tier
3. **Commission Reports**: Dedicated endpoint for commission-only reports
4. **Export Functionality**: Export analytics with commission data (CSV, PDF)
5. **Commission Alerts**: Notify admins when commission earnings reach thresholds
