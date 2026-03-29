# Sorting Keys Quick Reference

This is a **quick lookup** for sorting keys across all API endpoints. For detailed documentation, see [SORTING_OPTIONS.md](SORTING_OPTIONS.md).

## Quick Access by Entity Type

### Transactions

**API Endpoint:** `/api/v1/admin/transactions`

Available sort keys:

```
created_at (default)
amount
commission_amount
organizer_share
quantity
event_title
user_name
payment_gateway
status
```

**Example:**

```bash
curl "http://localhost:8000/api/v1/admin/transactions?sort_by=amount&sort_order=desc"
```

---

### Refunds

**API Endpoints:**

- `/api/v1/admin/payments/refunds`
- `/api/v1/admin/refunds`

Available sort keys:

```
created_at (default)
amount
status
refund_reason
processed_at
```

**Example:**

```bash
curl "http://localhost:8000/api/v1/admin/refunds?sort_by=amount&sort_order=desc"
```

---

### Payment Bills / Payouts

**API Endpoint:** `/api/v1/admin/payments/bills`

Available sort keys:

```
created_at (default)
event_title
organizer_name
billed_amount
status
```

**Example:**

```bash
curl "http://localhost:8000/api/v1/admin/payments/bills?sort_by=billed_amount&sort_order=desc"
```

---

### Audit Logs

**API Endpoint:** `/api/v1/admin/payments/audit-logs`

Available sort keys:

```
created_at (default)
action
entity_type
actor_type
```

**Example:**

```bash
curl "http://localhost:8000/api/v1/admin/payments/audit-logs?sort_by=action&sort_order=asc"
```

---

### Payout Requests (Organizer)

**API Endpoint:** `/api/v1/organizer/payouts`

Available sort keys:

```
created_at (default)
amount
event_title
event_status
status
request_type
```

**Example:**

```bash
curl "http://localhost:8000/api/v1/organizer/payouts?sort_by=amount&sort_order=desc"
```

---

### Payout Requests (Admin)

**API Endpoint:** `/api/v1/admin/payouts`

Available sort keys:

```
created_at (default)
amount
event_title
event_status
status
request_type
```

**Example:**

```bash
curl "http://localhost:8000/api/v1/admin/payouts?sort_by=status&sort_order=asc"
```

---

### Tickets (Organizer)

**API Endpoint:** `/api/v1/organizer/events/{id}/tickets`

Available sort keys:

```
created_at (default)
ticket_number
total_amount
status
event_title
user_name
```

**Example:**

```bash
curl "http://localhost:8000/api/v1/organizer/events/{event_id}/tickets?sort_by=total_amount&sort_order=desc"
```

---

### Events (Admin)

**API Endpoint:** `GET /api/v1/admin/events`

Available sort keys:

```
created_at (default)
title
start_date
end_date
status
ticket_sold
revenue
```

**Example:**

```bash
curl "http://localhost:8000/api/v1/admin/events?sort_by=revenue&sort_order=desc"
```

---

### Users (Admin)

**API Endpoint:** `GET /api/v1/admin/users`

Available sort keys:

```
created_at (default)
name
email
account_status
organizer_status
```

**Example:**

```bash
curl "http://localhost:8000/api/v1/admin/users?sort_by=name&sort_order=asc"
```

---

### Checkout Sessions (Admin)

**API Endpoint:** `GET /api/v1/admin/tickets/checkout-sessions`

Available sort keys:

```
created_at (default)
status
payment_gateway
total_amount
expires_at
```

**Example:**

```bash
curl "http://localhost:8000/api/v1/admin/tickets/checkout-sessions?sort_by=total_amount&sort_order=desc"
```

---

## Query Parameter Syntax

All endpoints use the same two query parameters:

| Parameter    | Values                              | Default      | Required |
| ------------ | ----------------------------------- | ------------ | -------- |
| `sort_by`    | Any valid sort key for the endpoint | `created_at` | No       |
| `sort_order` | `asc` or `desc`                     | `desc`       | No       |

### Combined Query Example

```bash
# Sort transactions by amount(descending) with pagination
curl "http://localhost:8000/api/v1/admin/transactions?sort_by=amount&sort_order=desc&page=1&limit=20"

# Sort refunds by status, filter by status, with sorting
curl "http://localhost:8000/api/v1/admin/refunds?status=pending&sort_by=created_at&sort_order=desc"
```

---

## Invalid Sort Key Behavior

If you provide an invalid `sort_by` key:

- API will **default to `created_at`** instead of throwing an error
- This ensures robustness and consistent results

If you provide an invalid `sort_order` value:

- API will **default to `desc`** instead of throwing an error

---

## Implementation Details

All sorting is centralized in `/pkg/utils/sorting.go` with these validator functions:

```go
func ValidateSortForAdminTransactions(sortBy, sortOrder string) (string, string)
func ValidateSortForRefunds(sortBy, sortOrder string) (string, string)
func ValidateSortForPaymentBills(sortBy, sortOrder string) (string, string)
func ValidateSortForAuditLogs(sortBy, sortOrder string) (string, string)
func ValidateSortForPayoutRequests(sortBy, sortOrder string) (string, string)
func ValidateSortForTickets(sortBy, sortOrder string) (string, string)
func ValidateSortForEvents(sortBy, sortOrder string) (string, string)
func ValidateSortForUsers(sortBy, sortOrder string) (string, string)
func ValidateSortForCheckoutSessions(sortBy, sortOrder string) (string, string)
```

Each function validates against a **whitelisted set of allowed fields** for that entity type.

---

## Database Indexes

For optimal performance, the following fields should be indexed in the database:

### Transactions

- `created_at`
- `amount`
- `status`
- `payment_gateway`

### Refunds

- `created_at`
- `amount`
- `status`
- `processed_at`

### Events

- `created_at`
- `start_date`
- `status`
- `revenue`

### Users

- `created_at`
- `email`

Tip: Check your database migration or schema to ensure these indexes are created for fast sorting performance.

---

## Testing Sorting

### Quick Test Commands

```bash
# Test transaction sorting by amount
curl "http://localhost:8000/api/v1/admin/transactions?sort_by=amount&sort_order=desc" \
  -H "Authorization: Bearer YOUR_API_KEY"

# Test refund sorting by status
curl "http://localhost:8000/api/v1/admin/refunds?sort_by=status&sort_order=asc" \
  -H "Authorization: Bearer YOUR_API_KEY"

# Test payout sorting by amount
curl "http://localhost:8000/api/v1/organizer/payouts?sort_by=amount&sort_order=desc" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

---

## Swagger/OpenAPI Documentation

All endpoints now include **comprehensive sorting documentation** in their Swagger comments. The sorting options are:

1. **Visible in Swagger UI** at `/swagger/index.html`
2. **Listed in Swagger YAML** at `/docs/swagger.yaml`
3. **Listed in Swagger JSON** at `/docs/swagger.json`

To regenerate Swagger documentation after code changes:

```bash
swag init -g main.go
```

---

## Related Documentation

- **Full Reference:** [SORTING_OPTIONS.md](SORTING_OPTIONS.md) - Comprehensive guide with detailed descriptions
- **Implementation:** `/pkg/utils/sorting.go` - Centralized sorting validation utilities
- **Config:** `/pkg/utils/sorting.go` - Sort configuration objects (SortConfig)
