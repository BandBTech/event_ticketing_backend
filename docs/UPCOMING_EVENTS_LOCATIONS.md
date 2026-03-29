# Upcoming Events Fetching Locations - Complete Reference

## Summary

This document lists ALL handlers and service methods that fetch and display upcoming events across the entire system, including their file paths, line numbers, current sorting (Order clauses), and query filters.

---

## 1. ADMIN DASHBOARD

### Handler: GetAdminDashboard

- **File:** [internal/handlers/dashboard_handler.go](internal/handlers/dashboard_handler.go#L28)
- **Line Range:** 28-250
- **Description:** Displays admin dashboard with aggregated system statistics and upcoming events list (limit to 3 months)
- **Query Filters:**
  - `start_date > NOW() AND start_date <= NOW() + 3 months`
  - `status IN ('on_sale', 'approved')`
  - `deleted_at IS NULL`
  - `Limit 12`
- **Order Clause:** [Line 168](internal/handlers/dashboard_handler.go#L168)
  ```sql
  Order("is_featured DESC, start_date DESC")
  ```
- **Method Type:** Raw SQL with GORM
- **Query Location:** [Line 162-170](internal/handlers/dashboard_handler.go#L162)
  ```go
  database.GetDB().Model(&models.Event{}).
    Select("id, title, banner_image, category, start_date, end_date, status, sales_status, is_featured, venue_name, created_at, updated_at").
    Where("start_date > ? AND start_date <= ? AND status IN (?)", now, threeMonthsFromNow, []string{"on_sale", "approved"}).
    Order("is_featured DESC, start_date DESC").
    Limit(12).
    Scan(&upcomingEventsResponse)
  ```

---

## 2. ADMIN EVENT APPROVAL/LISTING

### Handler: AdminGetEventsForApproval

- **File:** [internal/handlers/event_handler.go](internal/handlers/event_handler.go#L953)
- **Line Range:** 953-985
- **Description:** Lists events pending approval (administrative review)
- **Service Method Called:** `GetEventsByStatus("pending", page, limit, sortParam)`
- **Service Method File:** [internal/services/event_service.go](internal/services/event_service.go#L235)
- **Service Method Line Range:** 235-260
- **Query Filters:**
  - `status = 'pending'`
  - `deleted_at IS NULL` (implicit in GORM)
- **Order Clause:** [Line 254](internal/services/event_service.go#L254)

  ```go
  orderClause := "is_featured DESC, " + sortBy + " " + sortOrder
  ```

  - Default `sortBy` = "created_at", `sortOrder` = "desc"
  - **Default Order:** `is_featured DESC, created_at DESC`
  - **Supports custom sorting** via `sort` query parameter
  - Valid sort fields: `title`, `start_date`, `price`, `created_at`, `is_featured`

---

## 3. ORGANIZER DASHBOARD

### Handler: GetOrganizerDashboard

- **File:** [internal/handlers/dashboard_handler.go](internal/handlers/dashboard_handler.go#L258)
- **Line Range:** 258-378
- **Description:** Displays organizer dashboard with their event statistics and upcoming events (3 months)
- **Query Filters:**
  - `organizer_id = ?` (specific organizer)
  - `start_date > NOW() AND start_date <= NOW() + 3 months`
  - `status IN ('on_sale', 'approved')`
  - `deleted_at IS NULL`
  - `Limit 12`
- **Order Clause:** [Line 337](internal/handlers/dashboard_handler.go#L337)
  ```sql
  Order("is_featured DESC, start_date DESC")
  ```
- **Method Type:** GORM direct query
- **Query Location:** [Line 331-339](internal/handlers/dashboard_handler.go#L331)
  ```go
  database.GetDB().Model(&models.Event{}).
    Select("id, title, banner_image, category, start_date, end_date, status, sales_status, is_featured, venue_name, created_at, updated_at").
    Where("organizer_id = ? AND start_date > ? AND start_date <= ? AND status IN (?)", organizerID, now, threeMonthsFromNow, []string{"on_sale", "approved"}).
    Order("is_featured DESC, start_date DESC").
    Limit(12).
    Scan(&upcomingEventsResponse)
  ```

### Handler: GetOrganizerReport (Report Dashboard)

- **File:** [internal/handlers/report_handler.go](internal/handlers/report_handler.go#L154)
- **Line Range:** 154-250
- **Description:** Alternative organizer report view with detailed analytics including upcoming events
- **Service Method Called:** `getUpcomingEvents(organizerID)`
- **Service Method File:** [internal/handlers/report_handler.go](internal/handlers/report_handler.go#L1459)
- **Service Method Line Range:** 1459-1489
- **Query Filters:**
  - `e.organizer_id = ?` (specific organizer)
  - `e.start_date > NOW()`
  - `e.deleted_at IS NULL`
  - `Limit 10`
- **Order Clause:** [Line 1476](internal/handlers/report_handler.go#L1476)
  ```sql
  ORDER BY e.start_date ASC
  ```
- **Query Type:** Raw SQL
- **Query Location:** [Line 1465-1476](internal/handlers/report_handler.go#L1465)
  ```sql
  SELECT
    e.id as event_id,
    e.title as event_title,
    e.banner_image,
    e.status,
    e.start_date,
    COALESCE(SUM(et.quantity), 0) as ticket_capacity,
    COALESCE(COUNT(DISTINCT CASE WHEN t.status = 'completed' THEN t.id END), 0) as tickets_sold,
    EXTRACT(DAY FROM e.start_date - now()) as days_until_start
  FROM events e
  LEFT JOIN event_tiers et ON e.id = et.event_id AND et.deleted_at IS NULL
  LEFT JOIN transactions t ON e.id = t.event_id AND t.status = 'completed' AND t.deleted_at IS NULL
  WHERE e.organizer_id = ? AND e.start_date > now() AND e.deleted_at IS NULL
  GROUP BY e.id, e.title, e.banner_image, e.status, e.start_date
  ORDER BY e.start_date ASC
  LIMIT 10
  ```

### Handler: OrganizerGetEvents

- **File:** [internal/handlers/event_handler.go](internal/handlers/event_handler.go#L1071)
- **Line Range:** 1071-1103
- **Description:** Get all events for organizer (including past and scheduled)
- **Service Method Called:** `GetEventsByOrganizer(organizerID, page, limit, sortParam)`
- **Service Method File:** [internal/services/event_service.go](internal/services/event_service.go#L395)
- **Service Method Line Range:** 395-420
- **Query Filters:**
  - `organizer_id = ?` (specific organizer)
  - `deleted_at IS NULL` (implicit)
- **Order Clause:** [Line 410-412](internal/services/event_service.go#L410)

  ```go
  orderClause := "is_featured DESC, " + sortBy + " " + sortOrder
  ```

  - Default `sortBy` = "created_at", `sortOrder` = "desc"
  - Valid sort fields: `title`, `start_date`, `price`, `created_at`, `status`, `is_featured`

---

## 4. USER DASHBOARD

### Handler: GetUserDashboard

- **File:** [internal/handlers/dashboard_handler.go](internal/handlers/dashboard_handler.go#L390)
- **Line Range:** 390-439
- **Description:** Displays user dashboard with purchased tickets and upcoming events (3 months)
- **Query Filters:**
  - `start_date > NOW() AND start_date <= NOW() + 3 months`
  - `status IN ('scheduled', 'on_sale', 'approved')`
  - `deleted_at IS NULL` (implicit)
  - `Limit 12`
- **Order Clause:** [Line 426](internal/handlers/dashboard_handler.go#L426)

  ```sql
  Order("is_featured DESC, start_date ASC")
  ```

  - **NOTE:** User dashboard uses **ASCENDING** order for start_date (soonest first)

- **Method Type:** GORM direct query
- **Query Location:** [Line 420-428](internal/handlers/dashboard_handler.go#L420)
  ```go
  database.GetDB().Model(&models.Event{}).
    Select("id, title, banner_image, category, start_date, end_date, status, sales_status, is_featured, venue_name, created_at, updated_at").
    Where("start_date > ? AND start_date <= ? AND status IN (?)", now, threeMonthsFromNow, []string{"scheduled", "on_sale", "approved"}).
    Order("is_featured DESC, start_date ASC").
    Limit(12).
    Scan(&upcomingEventsResponse)
  ```

---

## 5. PUBLIC API - UPCOMING EVENTS

### Handler: GetUpcomingEvents

- **File:** [internal/handlers/public_handler.go](internal/handlers/public_handler.go#L152)
- **Line Range:** 152-193
- **Description:** Public API endpoint for upcoming events (public-facing)
- **Query Filters:**
  - `status IN ('scheduled', 'on_sale')`
  - `start_date > NOW()`
  - Optional: `category = ?` (if provided)
  - Pagination: `OFFSET` and `LIMIT`
- **Order Clause:** [Line 174](internal/handlers/public_handler.go#L174)

  ```go
  Order("is_featured DESC, start_date ASC")
  ```

  - **Soonest first** (ascending by start_date)

- **Method Type:** GORM direct query
- **Query Location:** [Line 168-180](internal/handlers/public_handler.go#L168)
  ```go
  if err := query.Order("is_featured DESC, start_date ASC").
    Offset(offset).
    Limit(pagination.Limit).
    Find(&events).Error; err != nil {
  ```

---

## 6. PUBLIC API - FEATURED EVENTS

### Handler: GetFeaturedEvents

- **File:** [internal/handlers/public_handler.go](internal/handlers/public_handler.go#L101)
- **Line Range:** 101-138
- **Description:** Public API endpoint for featured events
- **Query Filters:**
  - `is_featured = true`
  - `status IN ('on_sale', 'hold', 'live')`
  - `start_date > NOW()`
  - `Limit 3` (or custom limit via parameter)
- **Order Clause:** [Line 114](internal/handlers/public_handler.go#L114)

  ```sql
  Order("created_at DESC")
  ```

  - **Newest first** (by created_at)

- **Method Type:** GORM direct query
- **Query Location:** [Line 110-117](internal/handlers/public_handler.go#L110)
  ```go
  if err := h.db.Select("id, title, banner_image, category, start_date, end_date, status, sales_status, is_featured, venue_name, organizer_id, created_at, updated_at").
    Where("is_featured = ? AND status IN (?) AND start_date > ?", true, []string{"on_sale", "hold", "live"}, utils.Now()).
    Order("created_at DESC").
    Limit(limit).
    Find(&events).Error; err != nil {
  ```

---

## 7. PUBLIC API - ALL EVENTS (Filtered)

### Handler: PublicGetAllEvents

- **File:** [internal/handlers/event_handler.go](internal/handlers/event_handler.go#L535)
- **Line Range:** 535-583
- **Description:** Public API get all events with full filtering and search
- **Service Method Called:** `GetPublicEvents(page, limit, search, location, startDate, endDate, minPrice, maxPrice, sortBy, sortOrder)`
- **Service Method File:** [internal/services/event_service.go](internal/services/event_service.go#L333)
- **Service Method Line Range:** 333-389
- **Query Filters:**
  - `status IN ('scheduled', 'on_sale')` OR
  - `status = 'sales_end' AND multi-tier event with future tier sales`
  - Optional: `search` (ILIKE on title/description)
  - Optional: `location` (ILIKE)
  - Optional: `startDate`, `endDate` (date range)
  - Optional: `minPrice`, `maxPrice` (price range)
  - Supports custom sorting via `sortBy` and `sortOrder`
- **Order Clause:** [Line 379-381](internal/services/event_service.go#L379)

  ```go
  orderClause := "is_featured DESC, created_at DESC"
  ```

  - Default: Featured events first, then newest first

- **Valid Sort Fields:** `title`, `start_date`, `price`, `created_at`, `is_featured`
- **Query Location:** [Line 345-381](internal/services/event_service.go#L345)

  ```go
  // Apply sorting - always prioritize featured events first, then sort by newest first
  orderClause := "is_featured DESC, created_at DESC"
  query := db.Offset(offset).Limit(limit).Order(orderClause)

  // Preload tiers for public events (scheduled, on_sale, live events)
  query = query.Preload("Tiers").Preload("Organizer").Preload("Organizer.OrganizerOnboarding")

  if err := query.Find(&events).Error; err != nil {
    return nil, 0, err
  }
  ```

---

## 8. PUBLIC API - EVENTS BY CATEGORY

### Handler: GetEventsByCategory

- **File:** [internal/handlers/public_handler.go](internal/handlers/public_handler.go#L207)
- **Line Range:** 207-250
- **Description:** Public API events filtered by category
- **Query Filters:**
  - `status IN ('on_sale', 'hold', 'live')`
  - `start_date > NOW()`
  - `category = ?` (specific category)
  - Pagination: `OFFSET` and `LIMIT`
- **Order Clause:** [Line 230](internal/handlers/public_handler.go#L230)

  ```go
  Order("is_featured DESC, start_date DESC")
  ```

  - Featured events first, then most recent start dates

- **Method Type:** GORM direct query
- **Query Location:** [Line 224-232](internal/handlers/public_handler.go#L224)
  ```go
  if err := query.Order("is_featured DESC, start_date DESC").
    Offset(offset).
    Limit(pagination.Limit).
    Find(&events).Error; err != nil {
  ```

---

## 9. PUBLIC API - SEARCH EVENTS

### Handler: SearchEvents

- **File:** [internal/handlers/public_handler.go](internal/handlers/public_handler.go#L257)
- **Line Range:** 257-310
- **Description:** Public API search events by title/description
- **Query Filters:**
  - `status IN ('on_sale', 'hold', 'live')`
  - `start_date > NOW()`
  - `title ILIKE ? OR description ILIKE ?` (search query)
  - Optional: `category = ?` (if provided)
  - Pagination: `OFFSET` and `LIMIT`
- **Order Clause:** [Line 295](internal/handlers/public_handler.go#L295)

  ```go
  Order("is_featured DESC, start_date DESC")
  ```

  - Featured events first, then most recent start dates

- **Method Type:** GORM direct query
- **Query Location:** [Line 289-297](internal/handlers/public_handler.go#L289)
  ```go
  if err := query.Order("is_featured DESC, start_date DESC").
    Offset(offset).
    Limit(pagination.Limit).
    Find(&events).Error; err != nil {
  ```

---

## 10. ADMIN MANAGEMENT - GET MINIMAL EVENTS

### Handler/Method Location:

- **File:** [internal/handlers/admin_management_handler.go](internal/handlers/admin_management_handler.go#L655)
- **Line Range:** 655-680
- **Description:** Minimal event listing for admin management (generic event list)
- **Query Filters:**
  - Optional: `organizer_id = ?` (if provided)
  - No status or date filters
- **Order Clause:** [Line 665](internal/handlers/admin_management_handler.go#L665)

  ```sql
  Order("created_at DESC")
  ```

  - Newest events first

- **Method Type:** GORM direct query
- **Preloads:** None (minimal fields only)
- **Response:** Minimal ID and Title only

---

## 11. ORGANIZER USER MANAGEMENT

### Handler/Method Location:

- **File:** [internal/handlers/organizer_user_handler.go](internal/handlers/organizer_user_handler.go#L351)
- **Line Range:** ~351
- **Description:** Organizer user management event listing
- **Query Filters:** Similar to generic event listing
- **Order Clause:** [Line 351](internal/handlers/organizer_user_handler.go#L351)
  ```sql
  Order("created_at DESC")
  ```

---

## COMPARISON TABLE: Upcoming Events Sorting

| Location            | Handler                     | Order Clause                             | Sort Direction      | Featured First?    |
| ------------------- | --------------------------- | ---------------------------------------- | ------------------- | ------------------ |
| Admin Dashboard     | `GetAdminDashboard`         | `is_featured DESC, start_date DESC`      | Descending (Latest) | ✓ Yes              |
| Admin Approval      | `AdminGetEventsForApproval` | `is_featured DESC, {sortBy} {sortOrder}` | Default DESC        | ✓ Yes              |
| Organizer Dashboard | `GetOrganizerDashboard`     | `is_featured DESC, start_date DESC`      | Descending (Latest) | ✓ Yes              |
| Organizer Report    | `getUpcomingEvents`         | `start_date ASC`                         | Ascending (Soonest) | ✗ No               |
| Organizer Events    | `OrganizerGetEvents`        | `is_featured DESC, {sortBy} {sortOrder}` | Default DESC        | ✓ Yes              |
| User Dashboard      | `GetUserDashboard`          | `is_featured DESC, start_date ASC`       | Ascending (Soonest) | ✓ Yes              |
| Public Upcoming     | `GetUpcomingEvents`         | `is_featured DESC, start_date ASC`       | Ascending (Soonest) | ✓ Yes              |
| Public Featured     | `GetFeaturedEvents`         | `created_at DESC`                        | Descending (Newest) | N/A (pre-filtered) |
| Public All Events   | `PublicGetAllEvents`        | `is_featured DESC, created_at DESC`      | Descending (Newest) | ✓ Yes              |
| Public By Category  | `GetEventsByCategory`       | `is_featured DESC, start_date DESC`      | Descending (Latest) | ✓ Yes              |
| Public Search       | `SearchEvents`              | `is_featured DESC, start_date DESC`      | Descending (Latest) | ✓ Yes              |

---

## KEY OBSERVATIONS

### Sort Order Inconsistencies:

1. **Admin/Organizer Dashboard:** `start_date DESC` (latest/farthest first)
2. **User Dashboard & Public Upcoming:** `start_date ASC` (soonest first)
3. **Organizer Report:** `start_date ASC` (soonest first)
4. **Featured Events:** `created_at DESC` (newest events first)

### Date Range for "Upcoming":

- **Admin Dashboard:** 3 months window (NOW to NOW+3m)
- **Organizer Dashboard:** 3 months window (NOW to NOW+3m)
- **User Dashboard:** 3 months window (NOW to NOW+3m)
- **Public APIs:** No upper bound (displays all future events)

### Status Filters for "Upcoming":

- **Admin Dashboard:** `status IN ('on_sale', 'approved')`
- **Organizer Dashboard:** `status IN ('on_sale', 'approved')`
- **User Dashboard:** `status IN ('scheduled', 'on_sale', 'approved')`
- **Public Upcoming:** `status IN ('scheduled', 'on_sale')`
- **Public Featured:** `status IN ('on_sale', 'hold', 'live')`

### Featured Events Priority:

- Most handlers prioritize `is_featured DESC` first
- **Exception:** Featured Events handler filters by `is_featured = true` directly
- **Exception:** Organizer Report doesn't use featured field

---

## Service Method Quick Reference

| Service Method         | File              | Line      | Purpose                                              |
| ---------------------- | ----------------- | --------- | ---------------------------------------------------- |
| `GetEventsByStatus`    | event_service.go  | 235-260   | Get events by status with custom sorting             |
| `GetFilteredEvents`    | event_service.go  | 268-330   | Get filtered events by multiple criteria             |
| `GetPublicEvents`      | event_service.go  | 333-389   | Get public-facing events (includes multi-tier logic) |
| `GetEventsByOrganizer` | event_service.go  | 395-420   | Get organizer's events with custom sorting           |
| `getUpcomingEvents`    | report_handler.go | 1459-1489 | Get upcoming events for organizer report             |
