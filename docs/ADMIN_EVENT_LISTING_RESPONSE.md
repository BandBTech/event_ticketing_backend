# Admin Event Listing Response Structure

## Overview

The admin event listing API (`GET /api/v1/admin/events`) now uses the `EventAdminListResponse` structure which provides a cleaner, more focused interface by excluding implementation details and sensitive fields.

## Fields Removed from Admin Response

The following fields have been removed from the admin event listing response:

- **status** - Implementation detail (handled via background workers)
- **price** - Base price field (use tier pricing instead)
- **currency** - Redundant (tier pricing includes currency)
- **description** - Detailed content not needed for admin listing
- **timezone** - Implementation detail (dates are UTC)
- **location** - Replaced by venue_name and address fields
- **organizer_id** - UUID only; full organizer object included instead

## EventAdminListResponse Structure

```json
{
  "id": "uuid",
  "title": "Event Title",
  "category": "Music",
  "venue_name": "Venue Name",
  "start_date": "2024-01-20T00:00:00Z",
  "end_date": "2024-01-21T00:00:00Z",
  "banner_image": "https://...",
  "sales_status": "active|paused|stopped",
  "is_featured": false,
  "is_cancelled": false,
  "cancelled_at": null,
  "cancel_reason": null,
  "capacity": 1000,
  "available": 950,
  "commission_rate": 10.5,
  "admin_remark": "Admin notes here",
  "total_sold_tickets": 50,
  "total_revenue": 5000.0,
  "organizer": {
    "id": "uuid",
    "business_name": "Business Name",
    "business_logo_url": "https://...",
    "business_description": "Business description"
  },
  "tiers": [
    {
      "id": "uuid",
      "name": "General Admission",
      "quantity": 100,
      "available": 95,
      "price": 50.0,
      "currency": "USD",
      "sales_start": "2024-01-15T00:00:00Z",
      "sales_end": "2024-01-20T23:59:59Z",
      "event_start": "2024-01-20T10:00:00Z",
      "event_end": "2024-01-20T22:00:00Z"
    }
  ],
  "created_at": "2024-01-10T10:00:00Z",
  "updated_at": "2024-01-10T15:30:00Z"
}
```

## Key Information

### Kept Fields

- **Core Event Info**: id, title, category, venue_name
- **Date/Time**: start_date, end_date (UTC, no timezone field)
- **Display**: banner_image, sales_status, is_featured, is_cancelled
- **Business**: capacity, available, commission_rate, admin_remark
- **Calculations**: total_sold_tickets, total_revenue (real-time from tiers)
- **Relations**: organizer (full object), tiers (with pricing and date windows)
- **Metadata**: created_at, updated_at

### Why Removed

- **status**: The background worker handles status transitions based on tier dates. Admin doesn't need to see this field.
- **price**: Deprecated base price field. Use tier pricing for accurate information.
- **description**: Full HTML content not necessary for admin listings.
- **timezone**: All dates are stored and returned in UTC.
- **location**: Field is redundant when venues have both name and address.
- **organizer_id**: UUID only; the full organizer object provides more context.

## Event Status Logic (Background Worker)

Event status is automatically managed by the background worker:

- **scheduled** → Initial state after admin approval
- **on_sale** → When current time falls within a tier's sales window (SalesStart < now < SalesEnd)
- **sales_end** → When now is between tier windows (no active sales window)
- **live** → Event is currently happening (EventStart < now < EventEnd)
- **completed** → Event has ended

Admin can explicitly filter events by status using the `status` query parameter during listing, but the response does not include the status field.

## Example API Call

```bash
curl -X GET "http://localhost:8080/api/v1/admin/events?page=1&limit=10&sort=-created_at" \
  -H "Authorization: Bearer YOUR_ADMIN_TOKEN"
```

## Response Example

```json
{
  "code": 200,
  "message": "Events fetched successfully",
  "data": {
    "events": [
      {
        "id": "550e8400-e29b-41d4-a716-446655440000",
        "title": "Summer Music Festival 2024",
        "category": "Music",
        "venue_name": "Central Park",
        "start_date": "2024-06-21T00:00:00Z",
        "end_date": "2024-06-23T23:59:59Z",
        "banner_image": "https://example.com/banner.jpg",
        "sales_status": "active",
        "is_featured": true,
        "is_cancelled": false,
        "cancelled_at": null,
        "cancel_reason": null,
        "capacity": 5000,
        "available": 1200,
        "commission_rate": 10.0,
        "admin_remark": "High demand event - monitor closely",
        "total_sold_tickets": 3800,
        "total_revenue": 152000.0,
        "organizer": {
          "id": "660e8400-e29b-41d4-a716-446655440001",
          "business_name": "Festival Productions Inc",
          "business_logo_url": "https://example.com/logo.png",
          "business_description": "Professional event organization company"
        },
        "tiers": [
          {
            "id": "770e8400-e29b-41d4-a716-446655440002",
            "name": "Early Bird",
            "quantity": 2000,
            "available": 500,
            "price": 35.0,
            "currency": "USD",
            "sales_start": "2024-01-01T00:00:00Z",
            "sales_end": "2024-03-31T23:59:59Z",
            "event_start": "2024-06-21T09:00:00Z",
            "event_end": "2024-06-21T22:00:00Z"
          }
        ],
        "created_at": "2024-01-15T10:30:00Z",
        "updated_at": "2024-02-01T14:20:00Z"
      }
    ],
    "pagination": {
      "total": 45,
      "page": 1,
      "limit": 10,
      "total_pages": 5,
      "has_next": true,
      "has_previous": false
    }
  }
}
```

## Implementation Details

### Model Definition

File: `/internal/models/event.go`

- `EventAdminListResponse` struct: Lines 357-383
- `ToAdminListResponse()` method: Lines 385-412

### Handler Implementation

File: `/internal/handlers/event_handler.go`

- `AdminGetAllEvents()`: Lines 585-650
- Response transformation: Converts Event objects to EventAdminListResponse before serialization

### Data Flow

1. Admin requests events via GET `/api/v1/admin/events`
2. Handler calls service `GetFilteredEvents()` which returns `[]models.Event`
3. Each event is transformed to `EventAdminListResponse` via `ToAdminListResponse()`
4. Response is serialized to JSON with only the allowed fields

## Query Parameters

All existing query parameters remain unchanged:

- `page` - Pagination page number (default: 1)
- `limit` - Items per page (default: 10)
- `search` - Search by title or description
- `location` - Filter by location
- `status` - Filter by status (draft, pending, approved, on_sale, live, completed, etc.)
- `organizer_id` - Filter by organizer UUID
- `start_date` - Filter by start date (YYYY-MM-DD)
- `end_date` - Filter by end date (YYYY-MM-DD)
- `min_price` - Minimum tier price
- `max_price` - Maximum tier price
- `sort` - Sort field with optional `-` prefix for descending (default: `-created_at`)

## Benefits

1. **Cleaner API Response**: Only essential fields for admin decision-making
2. **Performance**: Smaller JSON payloads
3. **Security**: Removes internal implementation details from API response
4. **Maintainability**: Easier to extend admin response without affecting event core model
5. **Clarity**: Admin can focus on actionable information (tiers, sales, organizer) rather than technical fields

## Related Documents

- [API_ENDPOINTS_SORTING_REFERENCE.md](API_ENDPOINTS_SORTING_REFERENCE.md) - Sorting options
- [API.md](API.md) - Complete API documentation
