# Organizer Approval System

## Overview

The event ticketing system now includes a comprehensive organizer approval workflow alongside the event approval system. This document outlines the complete workflow from organizer registration to approval and event management.

## Organizer Status Workflow

### Organizer Statuses

1. **`inactive`** - Default status for regular users (not organizers)
2. **`pending`** - Organizer has registered and is awaiting admin approval
3. **`approved`** - Organizer approved by admin/subadmin, can create events
4. **`rejected`** - Organizer rejected by admin/subadmin with remarks

### Status Transitions

- **Self-registration** → **`pending`**: User registers as organizer
- **`pending`** → **`approved`**: Admin/subadmin approves the organizer
- **`pending`** → **`rejected`**: Admin/subadmin rejects the organizer
- **Admin creation** → **`approved`**: Admin directly creates approved organizer

## User Registration Flows

### Option 1: Self-Registration as Organizer

1. User visits organizer registration endpoint
2. Provides required information
3. System creates user account with:
   - `organizer_status: "pending"`
   - `organizer` role assigned
   - Account is functional but cannot create events
4. Admin/subadmin reviews and approves/rejects

### Option 2: Admin Creates Organizer Directly

1. Admin creates organizer account through admin panel
2. System creates user account with:
   - `organizer_status: "approved"`
   - `organizer` role assigned
   - Can immediately create events

## API Endpoints

### Public Endpoints

#### Register as Organizer

```http
POST /api/v1/auth/register-organizer
```

- **Access**: Public
- **Description**: Register as an organizer (requires approval)
- **Body**:

```json
{
  "email": "organizer@example.com",
  "password": "Password123!",
  "first_name": "John",
  "last_name": "Doe",
  "phone": "8765432109",
  "country_code": "+1"
}
```

- **Response**: User with `organizer_status: "pending"`

### Admin/Sub-Admin Endpoints

#### Get Pending Organizers

```http
GET /api/v1/admin/organizers/pending?page=1&limit=10
```

- **Access**: Admins, Sub-admins only
- **Description**: Get paginated list of organizers pending approval
- **Response**:

```json
{
  "status": "success",
  "message": "Pending organizers fetched successfully",
  "data": {
    "organizers": [
      {
        "id": "uuid",
        "email": "organizer@example.com",
        "first_name": "John",
        "last_name": "Doe",
        "organizer_status": "pending",
        "admin_remark": "",
        "roles": [{ "name": "organizer" }],
        "created_at": "2025-10-26T10:00:00Z"
      }
    ],
    "total": 5,
    "page": 1,
    "limit": 10
  }
}
```

#### Approve/Reject Organizer

```http
PUT /api/v1/admin/organizers/{id}/approval
```

- **Access**: Admins, Sub-admins only
- **Description**: Approve or reject a pending organizer
- **Body**:

```json
{
  "status": "approved",
  "admin_remark": "Verified credentials and business documents"
}
```

- **Valid Status Values**: `approved`, `rejected`

### Organizer Endpoints (Approved Only)

All organizer endpoints now require:

1. `organizer` role
2. `organizer_status: "approved"`

#### Create Event (Updated)

```http
POST /api/v1/events
```

- **Access**: Approved organizers, Admins only
- **Middleware**: `IsApprovedOrganizer()`

#### Get Organizer Events (Updated)

```http
GET /api/v1/organizer/events
```

- **Access**: Approved organizers only
- **Middleware**: `IsApprovedOrganizer()`

## Database Schema Updates

### User Model Updates

```go
type User struct {
    // ... existing fields
    OrganizerStatus  string        `gorm:"default:'inactive'" json:"organizer_status"` // inactive, pending, approved, rejected
    AdminRemark      string        `gorm:"type:text" json:"admin_remark"`
    // ... existing fields
}
```

### New Request/Response Types

```go
type OrganizerRegistrationRequest struct {
    Email       string `json:"email" binding:"required,email"`
    Password    string `json:"password" binding:"required"`
    FirstName   string `json:"first_name" binding:"required,min=2,max=50"`
    LastName    string `json:"last_name" binding:"required,min=2,max=50"`
    Phone       string `json:"phone" binding:"omitempty"`
    CountryCode string `json:"country_code" binding:"omitempty"`
}

type OrganizerApprovalRequest struct {
    Status      string `json:"status" binding:"required,oneof=approved rejected"`
    AdminRemark string `json:"admin_remark,omitempty"`
}
```

## Security & Access Control

### Middleware Updates

#### IsApprovedOrganizer()

New middleware that checks:

1. User has `organizer` role OR `admin` role
2. If organizer: `organizer_status` must be `"approved"`
3. Admins bypass organizer approval check

#### Role-Based Access Matrix

| Role                 | Self-Register | Create Events | Approve Organizers | Approve Events |
| -------------------- | ------------- | ------------- | ------------------ | -------------- |
| User                 | ✅            | ❌            | ❌                 | ❌             |
| Organizer (pending)  | N/A           | ❌            | ❌                 | ❌             |
| Organizer (approved) | N/A           | ✅            | ❌                 | ❌             |
| Staff                | ❌            | ❌            | ❌                 | ❌             |
| Manager              | ❌            | ✅            | ❌                 | ❌             |
| Sub-admin            | ❌            | ✅            | ✅                 | ✅             |
| Admin                | ❌            | ✅            | ✅                 | ✅             |

### Permission System Updates

#### New Permissions

- `approve:organizer` - Approve organizer applications
- `reject:organizer` - Reject organizer applications

#### Permission Assignment

- **Admin**: All permissions
- **Sub-admin**: Event permissions + organizer approval permissions
- **Organizer**: Event creation/management (when approved)

## Complete Workflow Examples

### Organizer Self-Registration Flow

1. **Registration**:

   ```bash
   curl -X POST /api/v1/auth/register-organizer \
     -H "Content-Type: application/json" \
     -d '{"email": "john@events.com", "password": "Pass123!", "first_name": "John", "last_name": "Doe"}'
   ```

2. **Admin Reviews Pending Organizers**:

   ```bash
   curl -X GET /api/v1/admin/organizers/pending \
     -H "Authorization: Bearer <admin_token>"
   ```

3. **Admin Approves Organizer**:

   ```bash
   curl -X PUT /api/v1/admin/organizers/{id}/approval \
     -H "Authorization: Bearer <admin_token>" \
     -H "Content-Type: application/json" \
     -d '{"status": "approved", "admin_remark": "Verified business license"}'
   ```

4. **Approved Organizer Creates Event**:
   ```bash
   curl -X POST /api/v1/events \
     -H "Authorization: Bearer <organizer_token>" \
     -H "Content-Type: application/json" \
     -d '{"title": "Music Festival", "start_date": "2025-12-01T18:00:00Z", ...}'
   ```

### Admin Direct Creation Flow

```bash
# Admin creates organizer directly (implementation needed)
curl -X POST /api/v1/admin/organizers \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{"email": "pro@events.com", "password": "Pass123!", "first_name": "Pro", "last_name": "Organizer", "organizer_status": "approved"}'
```

## Error Handling

### Organizer Not Approved

```json
{
  "status": "error",
  "message": "Permission denied: Organizer approval required",
  "error": "FORBIDDEN"
}
```

### Invalid Organizer Status Transition

```json
{
  "status": "error",
  "message": "organizer cannot be modified, current status: approved",
  "error": "BAD_REQUEST"
}
```

### Insufficient Permissions

```json
{
  "status": "error",
  "message": "insufficient permissions: only admin or subadmin can approve organizers",
  "error": "FORBIDDEN"
}
```

## Integration with Event System

### Event Creation Restrictions

- Only **approved organizers** and **admins** can create events
- Events created by approved organizers start with `status: "pending"`
- Admin/subadmin must approve events before they become public

### Combined Approval Workflow

1. User registers as organizer → `organizer_status: "pending"`
2. Admin approves organizer → `organizer_status: "approved"`
3. Organizer creates event → `event.status: "pending"`
4. Admin approves event → `event.status: "approved"`
5. Event becomes visible to public

## Database Migration

### Migration Steps

1. Add `organizer_status` column to users table
2. Add `admin_remark` column to users table
3. Set existing organizers to `"approved"` status
4. Add new permissions to permissions table
5. Update role-permission relationships

### SQL Migration Example

```sql
-- Add new columns
ALTER TABLE users ADD COLUMN organizer_status VARCHAR(20) DEFAULT 'inactive';
ALTER TABLE users ADD COLUMN admin_remark TEXT;

-- Update existing organizers to approved status
UPDATE users SET organizer_status = 'approved'
WHERE id IN (
  SELECT DISTINCT user_id FROM user_roles ur
  JOIN roles r ON ur.role_id = r.id
  WHERE r.name = 'organizer'
);

-- Insert new permissions
INSERT INTO permissions (name, description, resource, action) VALUES
('approve:organizer', 'Approve organizers', 'organizers', 'approve'),
('reject:organizer', 'Reject organizers', 'organizers', 'reject');
```

## Testing Checklist

### Registration Testing

- [ ] Organizer can self-register
- [ ] Registration sets status to "pending"
- [ ] Organizer role is assigned
- [ ] Pending organizer cannot create events

### Approval Testing

- [ ] Admin can view pending organizers
- [ ] Admin can approve organizers
- [ ] Admin can reject organizers
- [ ] Sub-admin has same permissions as admin
- [ ] Regular users cannot approve organizers

### Event Creation Testing

- [ ] Approved organizers can create events
- [ ] Pending organizers cannot create events
- [ ] Rejected organizers cannot create events
- [ ] Admins can always create events

### Security Testing

- [ ] Middleware correctly blocks unapproved organizers
- [ ] JWT tokens include correct role information
- [ ] Permission checks work at service layer
- [ ] Status transitions are validated

## Monitoring & Analytics

### Key Metrics to Track

- Number of organizer registrations per day
- Average time from registration to approval
- Organizer approval/rejection rates
- Events created by approved organizers
- Revenue generated by approved organizers

### Audit Logs

- Organizer registration events
- Approval/rejection actions with admin ID
- Status change timestamps
- Admin remarks for compliance
