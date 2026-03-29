# API Endpoints Reference with Sorting

This document provides a complete matrix of all available API endpoints organized by module, showing which sorting options are available for each.

## Financial & Payment Endpoints

### Transactions

| Property                | Value                                                                                                                               |
| ----------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| **Endpoint**            | `GET /api/v1/admin/transactions`                                                                                                    |
| **Auth Required**       | Yes (Admin)                                                                                                                         |
| **Default Sort Field**  | `created_at`                                                                                                                        |
| **Available Sort Keys** | `created_at`, `amount`, `commission_amount`, `organizer_share`, `quantity`, `event_title`, `user_name`, `payment_gateway`, `status` |
| **Filters**             | `status`, `payment_gateway`, `event_id`, `user_id`, `guest_user_id`, `start_date`, `end_date`, `search`                             |
| **Documentation**       | See [SORTING_OPTIONS.md#transactions](SORTING_OPTIONS.md#transactions)                                                              |

### Refunds

| Property                | Value                                                                                          |
| ----------------------- | ---------------------------------------------------------------------------------------------- |
| **Endpoints**           | `GET /api/v1/admin/payments/refunds`<br/>`GET /api/v1/admin/refunds`                           |
| **Auth Required**       | Yes (Admin)                                                                                    |
| **Default Sort Field**  | `created_at`                                                                                   |
| **Available Sort Keys** | `created_at`, `amount`, `status`, `refund_reason`, `processed_at`                              |
| **Filters**             | `status`, `page`, `limit`                                                                      |
| **Documentation**       | See [SORTING_OPTIONS.md#refunds-admin--organizer](SORTING_OPTIONS.md#refunds-admin--organizer) |

### Payment Bills (Payouts)

| Property                | Value                                                                                                                    |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| **Endpoint**            | `GET /api/v1/admin/payments/bills`                                                                                       |
| **Auth Required**       | Yes (Admin)                                                                                                              |
| **Default Sort Field**  | `created_at`                                                                                                             |
| **Available Sort Keys** | `created_at`, `event_title`, `organizer_name`, `billed_amount`, `status`                                                 |
| **Filters**             | `status`, `organizer_id`, `start_date`, `end_date`, `search`, `page`, `limit`                                            |
| **Documentation**       | See [SORTING_OPTIONS.md#payment-bills--payouts-billing-system](SORTING_OPTIONS.md#payment-bills--payouts-billing-system) |

### Audit Logs

| Property                | Value                                                                                                   |
| ----------------------- | ------------------------------------------------------------------------------------------------------- |
| **Endpoint**            | `GET /api/v1/admin/payments/audit-logs`                                                                 |
| **Auth Required**       | Yes (Admin)                                                                                             |
| **Default Sort Field**  | `created_at`                                                                                            |
| **Available Sort Keys** | `created_at`, `action`, `entity_type`, `actor_type`                                                     |
| **Filters**             | `action`, `entity_type`, `entity_id`, `actor_id`, `event_id`, `start_date`, `end_date`, `page`, `limit` |
| **Documentation**       | See [SORTING_OPTIONS.md#audit-logs-admin](SORTING_OPTIONS.md#audit-logs-admin)                          |

---

## Payout Endpoints

### Organizer Payout Requests

| Property                | Value                                                                                            |
| ----------------------- | ------------------------------------------------------------------------------------------------ |
| **Endpoint**            | `GET /api/v1/organizer/payouts`                                                                  |
| **Auth Required**       | Yes (Organizer)                                                                                  |
| **Default Sort Field**  | `created_at`                                                                                     |
| **Available Sort Keys** | `created_at`, `amount`, `event_title`, `event_status`, `status`, `request_type`                  |
| **Filters**             | `status`, `page`, `limit`                                                                        |
| **Documentation**       | See [SORTING_OPTIONS.md#payout-requests-organizer](SORTING_OPTIONS.md#payout-requests-organizer) |

### Admin Payout Requests

| Property                | Value                                                                                            |
| ----------------------- | ------------------------------------------------------------------------------------------------ |
| **Endpoint**            | `GET /api/v1/admin/payouts`                                                                      |
| **Auth Required**       | Yes (Admin)                                                                                      |
| **Default Sort Field**  | `created_at`                                                                                     |
| **Available Sort Keys** | `created_at`, `amount`, `event_title`, `event_status`, `status`, `request_type`                  |
| **Filters**             | `status`, `page`, `limit`                                                                        |
| **Documentation**       | See [SORTING_OPTIONS.md#payout-requests-organizer](SORTING_OPTIONS.md#payout-requests-organizer) |

---

## Event Endpoints

### Events (Admin)

| Property                | Value                                                                                                                            |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| **Endpoint**            | `GET /api/v1/admin/events`                                                                                                       |
| **Auth Required**       | Yes (Admin)                                                                                                                      |
| **Default Sort Field**  | `created_at`                                                                                                                     |
| **Available Sort Keys** | `created_at`, `title`, `start_date`, `end_date`, `status`, `ticket_sold`, `revenue`                                              |
| **Filters**             | `status`, `organizer_id`, `start_date`, `end_date`, `page`, `limit`                                                              |
| **Documentation**       | See [SORTING_OPTIONS.md#additional-endpoints-with-sorting-support](SORTING_OPTIONS.md#additional-endpoints-with-sorting-support) |

---

## Ticket Endpoints

### Event Tickets (Organizer)

| Property                | Value                                                                               |
| ----------------------- | ----------------------------------------------------------------------------------- |
| **Endpoint**            | `GET /api/v1/organizer/events/{id}/tickets`                                         |
| **Auth Required**       | Yes (Organizer)                                                                     |
| **Default Sort Field**  | `created_at`                                                                        |
| **Available Sort Keys** | `created_at`, `ticket_number`, `total_amount`, `status`, `event_title`, `user_name` |
| **Filters**             | `status`, `page`, `limit`                                                           |
| **Documentation**       | See [SORTING_OPTIONS.md#tickets](SORTING_OPTIONS.md#tickets)                        |

### Checkout Sessions (Admin)

| Property                | Value                                                                            |
| ----------------------- | -------------------------------------------------------------------------------- |
| **Endpoint**            | `GET /api/v1/admin/tickets/checkout-sessions`                                    |
| **Auth Required**       | Yes (Admin)                                                                      |
| **Default Sort Field**  | `created_at`                                                                     |
| **Available Sort Keys** | `created_at`, `status`, `payment_gateway`, `total_amount`, `expires_at`          |
| **Filters**             | `status`, `payment_gateway`, `event_id`, `page`, `limit`                         |
| **Documentation**       | See [SORTING_OPTIONS.md#checkout-sessions](SORTING_OPTIONS.md#checkout-sessions) |

---

## User Endpoints

### Users (Admin)

| Property                | Value                                                               |
| ----------------------- | ------------------------------------------------------------------- |
| **Endpoint**            | `GET /api/v1/admin/users`                                           |
| **Auth Required**       | Yes (Admin)                                                         |
| **Default Sort Field**  | `created_at`                                                        |
| **Available Sort Keys** | `created_at`, `name`, `email`, `account_status`, `organizer_status` |
| **Filters**             | `status`, `organizer_status`, `page`, `limit`                       |
| **Documentation**       | See [SORTING_OPTIONS.md#users](SORTING_OPTIONS.md#users)            |

---

## Sorting Behavior Matrix

### Default Behavior

```
sort_by = "created_at"      (if not provided)
sort_order = "desc"         (if not provided)
```

### Invalid Input Fallback

| Scenario                   | Behavior                                       |
| -------------------------- | ---------------------------------------------- |
| Invalid `sort_by` field    | Falls back to `created_at`                     |
| Invalid `sort_order` value | Falls back to `desc`                           |
| Both invalid               | Defaults to sorting by `created_at` descending |

### Field Validation

All sort fields are **whitelisted** per endpoint type:

- Required fields are validated against a centralized configuration in `/pkg/utils/sorting.go`
- Invalid fields are silently replaced with defaults (no error thrown)
- This ensures API resilience and predictable behavior

---

## Query Parameter Examples

### Basic Sorting

```
GET /api/v1/admin/transactions?sort_by=amount&sort_order=desc
```

### Combined with Filters

```
GET /api/v1/admin/transactions?status=completed&sort_by=amount&sort_order=desc&page=1&limit=20
```

### Date Range + Sorting

```
GET /api/v1/admin/refunds?start_date=2026-01-01&end_date=2026-03-29&sort_by=amount&sort_order=asc
```

### Search + Filter + Sort

```
GET /api/v1/admin/transactions?search=txn123&payment_gateway=stripe&sort_by=created_at&sort_order=desc
```

---

## Field Type Reference

### String Fields (Usually Sortable)

- `title`, `name`, `email`, `event_title`, `organizer_name`, `user_name`, `payment_gateway`, `status`, `action`, `entity_type`, `actor_type`, etc.

### Numeric Fields (Usually Sortable)

- `amount`, `commission_amount`, `organizer_share`, `quantity`, `billed_amount`, `total_amount`, `revenue`, `ticket_sold`, etc.

### Timestamp Fields (Usually Sortable)

- `created_at`, `updated_at`, `start_date`, `end_date`, `processed_at`, `expires_at`, etc.

---

## Common Sorting Use Cases

### Most Recent Transactions

```bash
GET /api/v1/admin/transactions?sort_by=created_at&sort_order=desc
```

### Highest Revenue Events

```bash
GET /api/v1/admin/events?sort_by=revenue&sort_order=desc
```

### Largest Refunds First

```bash
GET /api/v1/admin/refunds?sort_by=amount&sort_order=desc
```

### Oldest Pending Payouts

```bash
GET /api/v1/admin/payouts?status=pending&sort_by=created_at&sort_order=asc
```

### Highest Value Customers

```bash
GET /api/v1/admin/users?sort_by=email&sort_order=asc
```

### Upcoming Events

```bash
GET /api/v1/admin/events?sort_by=start_date&sort_order=asc
```

---

## Related Documentation

- **Complete Reference:** [SORTING_OPTIONS.md](SORTING_OPTIONS.md) - Detailed descriptions of all sorting fields
- **Quick Lookup:** [SORTING_QUICK_REFERENCE.md](SORTING_QUICK_REFERENCE.md) - Quick lookup by entity type
- **Implementation:** `/pkg/utils/sorting.go` - Centralized sorting validation logic
- **Swagger UI:** `/swagger/index.html` - Interactive API documentation

---

## Notes for Developers

1. **Sorting is always optional** - If not provided, returns results sorted by `created_at` DESC
2. **No error thrown for invalid sort keys** - API gracefully defaults instead of rejecting
3. **All sort keys are pre-validated** - Use the whitelisted sets for your endpoint type
4. **Performance consideration** - Sort fields should have database indexes for large datasets
5. **Consistent behavior across all APIs** - Same `sort_by`/`sort_order` parameters everywhere

---

## Changelog

### Version 1.0 (Current)

- Documented all sorting options for financial, payout, event, ticket, and user APIs
- Updated Swagger documentation for all endpoints
- Created centralized sorting reference guides
- Implemented sorting validation utilities in `/pkg/utils/sorting.go`
