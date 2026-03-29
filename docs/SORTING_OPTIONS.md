# Sorting Options Documentation

This document provides a comprehensive guide to all available sorting options for each API endpoint in the Timro Ticket System backend.

## Query Parameter Format

All sorting is controlled via two query parameters:

- **`sort_by`**: The field name to sort by (defaults to `created_at`)
- **`sort_order`**: The sort direction - either `asc` (ascending) or `desc` (descending) (defaults to `desc`)

### Example Usage

```
GET /api/v1/admin/transactions?sort_by=amount&sort_order=desc
GET /api/v1/admin/refunds?sort_by=created_at&sort_order=asc
GET /api/v1/admin/payments/payouts?sort_by=status&sort_order=asc
```

---

## Transactions (Admin)

**Endpoint:** `GET /api/v1/admin/transactions`

**Sort Fields Available:**

| Field               | Type      | Description                                               |
| ------------------- | --------- | --------------------------------------------------------- |
| `created_at`        | timestamp | Creation date (default)                                   |
| `amount`            | decimal   | Transaction amount                                        |
| `commission_amount` | decimal   | Commission charged on transaction                         |
| `organizer_share`   | decimal   | Amount paid to organizer after commission                 |
| `quantity`          | integer   | Number of tickets in transaction                          |
| `event_title`       | string    | Event name                                                |
| `user_name`         | string    | Customer/buyer name                                       |
| `payment_gateway`   | string    | Payment method used (stripe, khalti, etc.)                |
| `status`            | string    | Transaction status (completed, pending, failed, refunded) |

**Example Requests:**

```
# Sort by amount descending
GET /api/v1/admin/transactions?sort_by=amount&sort_order=desc

# Sort by event title ascending
GET /api/v1/admin/transactions?sort_by=event_title&sort_order=asc

# Sort by organizer share
GET /api/v1/admin/transactions?sort_by=organizer_share&sort_order=desc

# Sort by status
GET /api/v1/admin/transactions?sort_by=status&sort_order=asc
```

---

## Refunds (Admin & Organizer)

**Endpoints:**

- `GET /api/v1/admin/payments/refunds`
- `GET /api/v1/admin/refunds`

**Sort Fields Available:**

| Field           | Type      | Description                                                    |
| --------------- | --------- | -------------------------------------------------------------- |
| `created_at`    | timestamp | Refund request date (default)                                  |
| `amount`        | decimal   | Refund amount                                                  |
| `status`        | string    | Refund status (pending, approved, rejected, completed, failed) |
| `refund_reason` | string    | Reason for refund                                              |
| `processed_at`  | timestamp | Date refund was processed                                      |

**Example Requests:**

```
# Sort by refund amount descending
GET /api/v1/admin/refunds?sort_by=amount&sort_order=desc

# Sort by status ascending
GET /api/v1/admin/refunds?sort_by=status&sort_order=asc

# Sort by processed date
GET /api/v1/admin/refunds?sort_by=processed_at&sort_order=desc

# Sort by refund reason
GET /api/v1/admin/refunds?sort_by=refund_reason&sort_order=asc
```

---

## Payment Bills / Payouts (Billing System)

**Endpoint:** `GET /api/v1/admin/payments/payouts`

**Sort Fields Available:**

| Field            | Type      | Description                                                                 |
| ---------------- | --------- | --------------------------------------------------------------------------- |
| `created_at`     | timestamp | Bill/payout creation date (default)                                         |
| `event_title`    | string    | Associated event name                                                       |
| `organizer_name` | string    | Organizer/business name                                                     |
| `billed_amount`  | decimal   | Amount billed/to be paid out                                                |
| `status`         | string    | Payout status (pending, approving, approved, processing, completed, failed) |

**Example Requests:**

```
# Sort by billed amount descending
GET /api/v1/admin/payments/payouts?sort_by=billed_amount&sort_order=desc

# Sort by organizer name ascending
GET /api/v1/admin/payments/payouts?sort_by=organizer_name&sort_order=asc

# Sort by status
GET /api/v1/admin/payments/payouts?sort_by=status&sort_order=asc

# Sort by event title
GET /api/v1/admin/payments/payouts?sort_by=event_title&sort_order=asc
```

---

## Audit Logs (Admin)

**Endpoint:** `GET /api/v1/admin/payments/audit-logs`

**Sort Fields Available:**

| Field         | Type      | Description                                                       |
| ------------- | --------- | ----------------------------------------------------------------- |
| `created_at`  | timestamp | Log entry creation date (default)                                 |
| `action`      | string    | Action performed (create, update, delete, approve, reject, etc.)  |
| `entity_type` | string    | Type of entity affected (transaction, refund, payout, bill, etc.) |
| `actor_type`  | string    | Type of user performing action (admin, organizer, system)         |

**Example Requests:**

```
# Sort by action ascending
GET /api/v1/admin/payments/audit-logs?sort_by=action&sort_order=asc

# Sort by entity type
GET /api/v1/admin/payments/audit-logs?sort_by=entity_type&sort_order=asc

# Sort by actor type
GET /api/v1/admin/payments/audit-logs?sort_by=actor_type&sort_order=asc

# Sort by creation date descending (default)
GET /api/v1/admin/payments/audit-logs?sort_by=created_at&sort_order=desc
```

---

## Payout Requests (Organizer)

**Endpoint:** `GET /api/v1/organizer/payout-requests`

**Sort Fields Available:**

| Field          | Type      | Description                                                                |
| -------------- | --------- | -------------------------------------------------------------------------- |
| `created_at`   | timestamp | Request creation date (default)                                            |
| `amount`       | decimal   | Requested payout amount                                                    |
| `event_title`  | string    | Associated event name                                                      |
| `event_status` | string    | Status of the event                                                        |
| `status`       | string    | Payout request status (pending, approved, rejected, processing, completed) |
| `request_type` | string    | Type of payout request                                                     |

**Example Requests:**

```
# Sort by requested amount descending
GET /api/v1/organizer/payout-requests?sort_by=amount&sort_order=desc

# Sort by status ascending
GET /api/v1/organizer/payout-requests?sort_by=status&sort_order=asc

# Sort by event title
GET /api/v1/organizer/payout-requests?sort_by=event_title&sort_order=asc
```

---

## Additional Endpoints with Sorting Support

### Tickets

**Endpoint:** `GET /api/v1/organizer/events/{id}/tickets`

**Sort Fields Available:**

| Field           | Type      | Description                                       |
| --------------- | --------- | ------------------------------------------------- |
| `created_at`    | timestamp | Ticket purchase date (default)                    |
| `ticket_number` | string    | Unique ticket identifier                          |
| `total_amount`  | decimal   | Ticket price                                      |
| `status`        | string    | Ticket status (active, used, cancelled, refunded) |
| `event_title`   | string    | Associated event name                             |
| `user_name`     | string    | Ticket holder name                                |

### Checkout Sessions

**Endpoint:** `GET /api/v1/admin/tickets/checkout-sessions`

**Sort Fields Available:**

| Field             | Type      | Description                                           |
| ----------------- | --------- | ----------------------------------------------------- |
| `created_at`      | timestamp | Session creation date (default)                       |
| `status`          | string    | Checkout status (pending, completed, expired, failed) |
| `payment_gateway` | string    | Payment method (stripe, khalti, etc.)                 |
| `total_amount`    | decimal   | Total cart/order amount                               |
| `expires_at`      | timestamp | Session expiration time                               |

### Events

**Endpoint:** `GET /api/v1/admin/events`

**Sort Fields Available:**

| Field         | Type      | Description                                                                  |
| ------------- | --------- | ---------------------------------------------------------------------------- |
| `created_at`  | timestamp | Event creation date (default)                                                |
| `title`       | string    | Event name                                                                   |
| `start_date`  | timestamp | Event start date                                                             |
| `end_date`    | timestamp | Event end date                                                               |
| `status`      | string    | Event status (draft, pending, approved, on_sale, live, completed, cancelled) |
| `ticket_sold` | integer   | Number of tickets sold                                                       |
| `revenue`     | decimal   | Total revenue from event                                                     |

### Users

**Endpoint:** `GET /api/v1/admin/users`

**Sort Fields Available:**

| Field              | Type      | Description                                                 |
| ------------------ | --------- | ----------------------------------------------------------- |
| `created_at`       | timestamp | User registration date (default)                            |
| `name`             | string    | Full name                                                   |
| `email`            | string    | Email address                                               |
| `account_status`   | string    | Account status (active, suspended, inactive)                |
| `organizer_status` | string    | Organizer verification status (pending, approved, rejected) |

---

## Default Behavior

When no sorting parameters are provided:

- **Default Sort Field:** `created_at`
- **Default Sort Order:** `desc` (newest first)

Example:

```
GET /api/v1/admin/transactions
# Equivalent to: /api/v1/admin/transactions?sort_by=created_at&sort_order=desc
```

---

## Error Handling

If you provide:

- An invalid `sort_by` field → API defaults to `created_at`
- An invalid `sort_order` value → API defaults to `desc`
- Both invalid values → API uses defaults for both

This ensures the API is resilient and always returns results, even with invalid sort parameters.

---

## Combined Usage With Other Filters

Sorting works seamlessly with other query parameters:

```
# Multi-filter example
GET /api/v1/admin/transactions?status=completed&sort_by=amount&sort_order=desc&page=1&limit=20

# Date range + sorting
GET /api/v1/admin/refunds?start_date=2026-01-01&end_date=2026-03-29&sort_by=amount&sort_order=asc

# Status + sorting
GET /api/v1/admin/payments/payouts?status=pending&sort_by=billed_amount&sort_order=desc
```

---

## Implementation Notes

All sorting is implemented via the centralized `ValidateSortFor*` utility functions in `/pkg/utils/sorting.go`:

- `ValidateSortForAdminTransactions()`
- `ValidateSortForRefunds()`
- `ValidateSortForPaymentBills()`
- `ValidateSortForAuditLogs()`
- `ValidateSortForPayoutRequests()`
- `ValidateSortForTickets()`
- `ValidateSortForEvents()`
- `ValidateSortForUsers()`
- `ValidateSortForCheckoutSessions()`

Each function validates provided sort fields against a whitelisted set of allowed fields for that entity type.
