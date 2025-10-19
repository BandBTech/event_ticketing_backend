# Admin and Role-Based Access Control Guide

## Overview

This system uses role-based access control (RBAC) with the following user types:

- **Admin**: Full system access to all APIs
- **Organizer**: Can manage events and organization users
- **Manager**: Organization manager with expanded permissions
- **Staff**: Limited event management permissions
- **User**: Basic user with read-only access

## Authentication

All user types (admin, organizer, staff, user) use the **same login endpoint**:

```bash
POST /api/v1/auth/login
{
    "email": "admin@timroticket.com",
    "password": "your_password"
}
```

The JWT token returned contains user roles, which are automatically checked by route middleware.

## API Route Structure

### Admin Routes (`/api/v1/admin/*`)

**Access**: Admin users only

```bash
# Organization management
POST   /api/v1/admin/organizations          # Create organization
PUT    /api/v1/admin/organizations/:id      # Update organization
DELETE /api/v1/admin/organizations/:id      # Delete organization
GET    /api/v1/admin/organizations          # List all organizations

# Event management
GET    /api/v1/admin/events                 # List all events (admin view)
DELETE /api/v1/admin/events/:id             # Delete any event
```

### Organizer Routes (`/api/v1/organizer/*`)

**Access**: Organizer and Admin users

```bash
# Organization management (their own)
GET    /api/v1/organizer/organizations              # Get user's organizations
GET    /api/v1/organizer/organizations/:id          # Get specific organization

# Organization user management
POST   /api/v1/organizer/organizations/:id/users    # Add user to organization
GET    /api/v1/organizer/organizations/:id/users    # List organization users
PUT    /api/v1/organizer/organizations/:id/users/:userId  # Update organization user
DELETE /api/v1/organizer/organizations/:id/users/:userId  # Remove organization user

# Event management
POST   /api/v1/organizer/events            # Create event
PUT    /api/v1/organizer/events/:id        # Update event
GET    /api/v1/organizer/events            # List organizer's events
```

### User Routes (`/api/v1/user/*`)

**Access**: All authenticated users

```bash
# Profile management
GET    /api/v1/user/profile                # Get user profile
PUT    /api/v1/user/profile                # Update profile
POST   /api/v1/user/change-password        # Change password

# Event browsing
GET    /api/v1/user/events                 # Browse events (personalized)
```

### Public Routes (`/api/v1/public/*`)

**Access**: No authentication required

```bash
# Public event browsing
GET    /api/v1/public/events               # Browse all public events
GET    /api/v1/public/events/:id           # Get event details
```

## Creating Admin Users

### Method 1: Automatic Default Admin (on first startup)

When you run the application for the first time, it automatically creates:

- **Email**: `admin@timroticket.com`
- **Password**: `admin123456`
- **Role**: Admin with all permissions

⚠️ **IMPORTANT**: Change this password immediately after first login!

### Method 2: Interactive Admin Creation

```bash
make create-admin
```

This will prompt you to enter admin details interactively.

### Method 3: Command Line Admin Creation

```bash
# Quick admin creation for testing
make create-admin-fast

# Or manually with custom details
go run cmd/create-admin/main.go -email="john@company.com" -first-name="John" -last-name="Admin" -password="secure123"
```

### Method 4: Using CLI Flags

```bash
go run cmd/create-admin/main.go \
  -email="admin@yourcompany.com" \
  -first-name="System" \
  -last-name="Administrator" \
  -phone="+1234567890" \
  -password="your_secure_password"
```

## Testing the System

1. **Create an admin user**:

   ```bash
   make create-admin-fast
   ```

2. **Login as admin**:

   ```bash
   curl -X POST http://localhost:8082/api/v1/auth/login \
     -H "Content-Type: application/json" \
     -d '{"email":"admin@timroticket.com","password":"admin123456"}'
   ```

3. **Use the JWT token** in subsequent requests:
   ```bash
   curl -X GET http://localhost:8082/api/v1/admin/organizations \
     -H "Authorization: Bearer YOUR_JWT_TOKEN"
   ```

## Role Hierarchy

```
Admin
├── Can access all /admin/* routes
├── Can access all /organizer/* routes
├── Can access all /user/* routes
└── Can access all /public/* routes

Organizer
├── Can access all /organizer/* routes
├── Can access all /user/* routes
└── Can access all /public/* routes

Manager/Staff/User
├── Can access all /user/* routes
└── Can access all /public/* routes
```

## Security Features

- **JWT-based authentication**: Tokens contain user roles
- **Middleware protection**: Routes automatically check user permissions
- **Role inheritance**: Admin users can access lower-level routes
- **Organization isolation**: Organizers can only manage their own organization users
- **Password hashing**: All passwords are securely hashed with bcrypt

## Available Roles

The system seeds these roles automatically:

1. **admin**: Full system permissions
2. **organizer**: Event and organization management
3. **manager**: Organization manager with expanded permissions
4. **staff**: Limited event permissions
5. **user**: Basic read permissions

## Production Security

For production deployment:

1. **Change default admin credentials** immediately
2. **Use strong passwords** for all admin accounts
3. **Set up proper JWT secrets** in environment variables
4. **Enable HTTPS** for all API communication
5. **Implement rate limiting** (already included)
6. **Monitor admin actions** through application logs

## API Documentation

Visit `/api/docs` for complete Swagger documentation of all endpoints.
