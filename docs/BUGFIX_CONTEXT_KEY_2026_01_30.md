# Context Key Bug Fix - January 30, 2026

## Issue

Profile API endpoints returning 401 Unauthorized for all roles (admin, organizer, etc.)

## Root Cause

Context key mismatch in authentication middleware:

- `AuthMiddleware` was setting: `c.Set("userID", ...)`
- Other middleware functions were getting: `c.Get("user_id", ...)`

This inconsistency caused authentication to fail silently in permission-checking middleware.

## Affected Endpoints

- `/api/v1/organizer/profile` (GET)
- Any endpoint using `RequirePermission()` middleware
- Any endpoint using role-checking middleware that reads from context

## Fix Applied

Standardized all context key access to use `"userID"` (camelCase) throughout the codebase:

### Files Modified

- `internal/middleware/auth.go` - Fixed 7 middleware functions:
  1. `IsApprovedOrganizer()`
  2. `IsApprovedOrganizerOrManager()`
  3. `IsOrganizerOrManager()`
  4. `IsTicketAccessAllowed()`
  5. `RequirePermission()` - **This was blocking /organizer/profile**
  6. `ValidateTicketAccessMiddleware()` - Removed duplicate set
  7. `IsOrganizerRole()`

## Testing

✅ Build successful - no compilation errors
✅ All middleware functions now use consistent context key

## Deployment

1. Build completed successfully
2. Ready to restart application
3. No database changes required
4. No breaking changes for existing API contracts

## Prevention

This type of issue could be prevented by:

- Using constants for context keys
- Type-safe context wrapper functions
- Better code review for context key usage
