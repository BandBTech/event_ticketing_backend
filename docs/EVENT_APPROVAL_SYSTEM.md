# Event Approval System

## Overview

The event ticketing system now includes a comprehensive event approval workflow with role-based access control. This document outlines the complete workflow from event creation to approval.

## Role Hierarchy

### Main Admin

- **Role**: `admin`
- **Permissions**: All permissions including event approval
- **Access**: Can approve, hold, or reject any event
- **Event Listing**: Can view all events regardless of status

### Sub-Admin

- **Role**: `subadmin`
- **Permissions**: Event management and approval permissions
- **Access**: Can approve, hold, or reject events
- **Event Listing**: Can view all events and pending events for approval

### Organizers

- **Role**: `organizer`
- **Permissions**: Event creation and management
- **Access**: Can create, update events; cannot approve their own events
- **Event Listing**: Can view only their own events (all statuses)

### Managers

- **Role**: `manager`
- **Permissions**: Expanded event management within organization
- **Access**: Can create, update events; view events within their organization
- **Event Listing**: Can view events within their organization scope

### Staff

- **Role**: `staff`
- **Permissions**: Read-only access to events
- **Access**: Can view events within their scope
- **Event Listing**: Can view approved events

### Users/Customers

- **Role**: `user`
- **Permissions**: Basic read access
- **Access**: Can view and purchase tickets for approved events
- **Event Listing**: Can view only approved events

## Event Status Workflow

### Event Statuses

1. **`draft`** - Initial status when event is being created (not used in current implementation)
2. **`pending`** - Default status when organizer submits event
3. **`approved`** - Event approved by admin/subadmin, visible to public
4. **`on_sale`** - Event approved and has ticket tiers available for sale (automatic)
5. **`live`** - Event is currently happening (automatic when start time reached)
6. **`completed`** - Event has ended (automatic when end time passed)
7. **`held`** - Event temporarily held by admin/subadmin with remarks
8. **`rejected`** - Event rejected by admin/subadmin with remarks

### Status Transitions

**Manual Transitions (Admin/Sub-admin):**

- **`pending`** → **`approved`**: Admin/subadmin approves the event
- **`pending`** → **`held`**: Admin/subadmin puts event on hold
- **`pending`** → **`rejected`**: Admin/subadmin rejects the event
- **`held`** → **`approved`**: Admin/subadmin approves previously held event
- **`held`** → **`rejected`**: Admin/subadmin rejects previously held event
- **`approved`** → **`on_sale`**: Admin can manually set approved events to on_sale
- **`on_sale`** → **`live`**: Admin can manually set events to live
- **`live`** → **`completed`**: Admin can manually complete events

**Automatic Transitions (System):**

- **`approved`** → **`on_sale`**: Automatic when event has ticket tiers with available seats
- **`on_sale`** → **`live`**: Automatic when event start time is reached
- **`live`** → **`completed`**: Automatic when event end time has passed

### Automatic Status Transitions

The system automatically updates event statuses based on time-based and data-driven conditions:

1. **`approved`** → **`on_sale`**: When an approved event has ticket tiers with available seats
2. **`on_sale`** → **`live`**: When an event's start time has been reached
3. **`live`** → **`completed`**: When an event's end time has passed

**Automatic Transition Rules:**

- Status updates run every 5 minutes for general updates
- Live status checks run every 1 minute for time-sensitive transitions
- All automatic changes are logged in the event status history
- Manual admin changes always take precedence over automatic updates

## API Endpoints

### Public Endpoints

#### Get All Approved Events

```http
GET /api/v1/events?page=1&limit=10
```

- **Access**: Public
- **Description**: Returns paginated list of approved events only
- **Query Parameters**:
  - `page` (optional): Page number (default: 1)
  - `limit` (optional): Items per page (default: 10)

#### Get Event by ID

```http
GET /api/v1/events/{id}
```

- **Access**: Public
- **Description**: Get details of a specific event (any status for now)

### Organizer Endpoints

#### Create Event

```http
POST /api/v1/events
```

- **Access**: Organizers, Managers, Admins
- **Description**: Create new event (automatically set to `pending` status)
- **Body**:

```json
{
  "title": "Concert Event",
  "description": "Amazing concert",
  "location": "City Hall",
  "start_date": "2025-12-01T18:00:00Z",
  "end_date": "2025-12-01T22:00:00Z",
  "price": 50.0,
  "capacity": 1000
}
```

#### Get Organizer Events

```http
GET /api/v1/organizer/events?page=1&limit=10
```

- **Access**: Organizers only
- **Description**: Get paginated list of events created by the authenticated organizer
- **Returns**: Events in all statuses (pending, approved, held, rejected)

#### Update Event

```http
PUT /api/v1/events/{id}
```

- **Access**: Organizers, Managers, Admins
- **Description**: Update event details
- **Note**: Organizers can only update their own events

### Admin/Sub-Admin Endpoints

#### Get Pending Events

```http
GET /api/v1/admin/events/pending?page=1&limit=10
```

- **Access**: Admins, Sub-admins only
- **Description**: Get paginated list of events pending approval

#### Approve/Hold/Reject Event

```http
PUT /api/v1/admin/events/{id}/approval
```

- **Access**: Admins, Sub-admins only
- **Description**: Approve, hold, or reject an event
- **Body**:

```json
{
  "status": "approved",
  "admin_remark": "Event meets all requirements"
}
```

- **Valid Status Values**: `approved`, `held`, `rejected`

## Database Schema Updates

### Event Model

```go
type Event struct {
    ID          uint           `gorm:"primaryKey" json:"id"`
    Title       string         `gorm:"not null;size:200" json:"title"`
    Description string         `gorm:"type:text" json:"description"`
    Location    string         `gorm:"size:200" json:"location"`
    StartDate   time.Time      `gorm:"not null" json:"start_date"`
    EndDate     time.Time      `gorm:"not null" json:"end_date"`
    Price       float64        `gorm:"not null" json:"price"`
    Capacity    int            `gorm:"not null" json:"capacity"`
    Available   int            `gorm:"not null" json:"available"`
    Status      string         `gorm:"not null;default:'pending'" json:"status"`
    OrganizerID string         `gorm:"size:36;index" json:"organizer_id"`
    AdminRemark string         `gorm:"type:text" json:"admin_remark"`
    CreatedAt   time.Time      `json:"created_at"`
    UpdatedAt   time.Time      `json:"updated_at"`
    DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}
```

### Key Changes

- **`Status`**: Default changed from `'active'` to `'pending'`
- **`AdminRemark`**: New field for admin comments during approval process
- **`UserID`**: Removed (events are not directly linked to users)

## Security Considerations

### Role-Based Access Control

- All event approval endpoints require admin or subadmin roles
- Organizers can only manage their own events
- Role verification is performed at the service layer and middleware level

### Status Validation

- Only events in `pending` or `held` status can be modified by admins
- Status transitions are validated to prevent invalid state changes
- Approved events cannot be directly modified without proper authorization

### Audit Trail

- All approval actions include admin remarks for accountability
- Event status changes are logged with timestamps
- User actions are tracked through the authentication system

## Usage Examples

### Complete Event Lifecycle

1. **Organizer creates event**:

   ```bash
   curl -X POST /api/v1/events \
     -H "Authorization: Bearer <organizer_token>" \
     -H "Content-Type: application/json" \
     -d '{"title": "Music Festival", "start_date": "2025-12-01T18:00:00Z", ...}'
   ```

2. **Admin reviews pending events**:

   ```bash
   curl -X GET /api/v1/admin/events/pending \
     -H "Authorization: Bearer <admin_token>"
   ```

3. **Admin approves event**:

   ```bash
   curl -X PUT /api/v1/admin/events/123/approval \
     -H "Authorization: Bearer <admin_token>" \
     -H "Content-Type: application/json" \
     -d '{"status": "approved", "admin_remark": "All requirements met"}'
   ```

4. **Public can now see approved event**:
   ```bash
   curl -X GET /api/v1/events
   ```

## Error Handling

### Common Error Responses

#### Insufficient Permissions

```json
{
  "status": "error",
  "message": "insufficient permissions: only admin or subadmin can approve events",
  "error": "FORBIDDEN"
}
```

#### Invalid Status Transition

```json
{
  "status": "error",
  "message": "event cannot be modified, current status: approved",
  "error": "BAD_REQUEST"
}
```

#### Event Not Found

```json
{
  "status": "error",
  "message": "event not found",
  "error": "NOT_FOUND"
}
```

## Migration Notes

### Database Migration

If upgrading from a previous version:

1. Run database migration to add `admin_remark` column
2. Update existing events with `status = 'active'` to `status = 'approved'`
3. Seed the database with new role permissions

### Backward Compatibility

- Existing API endpoints remain functional
- Public event listing automatically filters to approved events only
- Admin endpoints are additive and don't break existing functionality
