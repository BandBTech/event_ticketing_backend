# Rate Limiting Configuration

This document describes the comprehensive rate limiting system implemented in the event ticketing backend.

## Overview

The system implements multiple rate limiters with different configurations for various API endpoint types:

- **Global Rate Limiter**: Automatically routes requests to appropriate limiters
- **Auth Rate Limiter**: For authentication endpoints
- **Password Rate Limiter**: For password-related operations (very restrictive)
- **Admin Rate Limiter**: For admin operations
- **Public Rate Limiter**: For public API endpoints (most permissive)
- **OTP Rate Limiter**: For OTP operations (extremely restrictive)
- **Event Rate Limiter**: For event operations
- **Strict Rate Limiter**: For highly sensitive operations

## Environment Variables

### Global Rate Limiting Control

```env
# Global rate limiting enable/disable
RATE_LIMIT_ENABLED=true
```

### Standard API Rate Limiter

```env
# Standard API endpoints (default: 120 requests/min, burst: 30)
STANDARD_RATE_LIMIT_ENABLED=true
STANDARD_RATE_LIMIT=120.0
STANDARD_BURST_LIMIT=30
```

### Authentication Rate Limiter

```env
# Authentication endpoints (default: 30 requests/min, burst: 10)
AUTH_RATE_LIMIT_ENABLED=true
AUTH_RATE_LIMIT=30.0
AUTH_BURST_LIMIT=10
```

### Password Operations Rate Limiter

```env
# Password reset/change operations (default: 5 requests/min, burst: 3)
PASSWORD_RATE_LIMIT_ENABLED=true
PASSWORD_RATE_LIMIT=5.0
PASSWORD_BURST_LIMIT=3
```

### Admin Operations Rate Limiter

```env
# Admin operations (default: 60 requests/min, burst: 20)
ADMIN_RATE_LIMIT_ENABLED=true
ADMIN_RATE_LIMIT=60.0
ADMIN_BURST_LIMIT=20
```

### Public API Rate Limiter

```env
# Public API endpoints (default: 200 requests/min, burst: 50)
PUBLIC_RATE_LIMIT_ENABLED=true
PUBLIC_RATE_LIMIT=200.0
PUBLIC_BURST_LIMIT=50
```

### OTP Operations Rate Limiter

```env
# OTP send/verify operations (default: 3 requests/min, burst: 2)
OTP_RATE_LIMIT_ENABLED=true
OTP_RATE_LIMIT=3.0
OTP_BURST_LIMIT=2
```

### Event Operations Rate Limiter

```env
# Event operations (default: 90 requests/min, burst: 25)
EVENT_RATE_LIMIT_ENABLED=true
EVENT_RATE_LIMIT=90.0
EVENT_BURST_LIMIT=25
```

### Strict Rate Limiter

```env
# Highly sensitive operations (default: 2 requests/min, burst: 1)
STRICT_RATE_LIMIT_ENABLED=true
```

## Rate Limiter Assignment

### Automatic Assignment (Global Middleware)

The global rate limiter middleware automatically routes requests to appropriate limiters:

- `/auth/send-otp`, `/auth/verify-otp` → **OTP Rate Limiter**
- `/auth/reset-password*`, `/auth/change-password` → **Password Rate Limiter**
- `/api/v1/auth/*` → **Auth Rate Limiter**
- `/api/v1/admin/*` → **Admin Rate Limiter**
- `*/events*` → **Event Rate Limiter**
- `/api/v1/public/*` → **Public Rate Limiter**
- All other endpoints → **Standard Rate Limiter**

### Manual Assignment

Specific middleware can be applied to route groups:

```go
// Apply auth rate limiter
auth.Use(middleware.AuthRateLimiter())

// Apply password rate limiter
passwordOps.Use(middleware.PasswordRateLimiter())

// Apply OTP rate limiter
otpOps.Use(middleware.OTPRateLimiter())

// Apply admin rate limiter
admin.Use(middleware.AdminRateLimiter())

// Apply public rate limiter
public.Use(middleware.PublicRateLimiter())

// Apply event rate limiter
events.Use(middleware.EventRateLimiter())

// Apply strict rate limiter
sensitiveOps.Use(middleware.StrictRateLimiter())
```

## Rate Limiting Strategy

### IP-Based Limiting

- Rate limits are applied per IP address
- Supports proxy headers (`X-Forwarded-For`, `X-Real-IP`)
- Automatic cleanup of expired IP entries

### Token Bucket Algorithm

- Uses Go's `golang.org/x/time/rate` package
- Implements token bucket algorithm with burst capacity
- Smooth rate limiting with burst handling

### Memory Management

- Automatic cleanup of expired IP entries every 5 minutes
- Different expiry durations for different limiter types:
  - Standard: 1 hour
  - Auth: 2 hours
  - Password: 4 hours
  - Admin: 2 hours
  - Public: 30 minutes
  - OTP: 6 hours
  - Event: 1 hour
  - Strict: 8 hours

## Rate Limit Response

When rate limit is exceeded, the API returns:

```json
{
  "error": "Rate limit exceeded",
  "message": "Too many [limiter_type] requests. Please try again later.",
  "type": "[limiter_type]_rate_limit"
}
```

HTTP Status Code: `429 Too Many Requests`

## Security Considerations

### Rate Limit Bypass Prevention

- Rate limits are enforced before authentication
- Cannot be bypassed by invalid credentials
- Applied to both authenticated and unauthenticated endpoints

### DDoS Protection

- Multiple layers of rate limiting
- IP-based tracking prevents single-source flooding
- Different limits for different operation sensitivity levels

### Brute Force Protection

- Very restrictive limits on password operations (5/min)
- Extremely restrictive limits on OTP operations (3/min)
- Long expiry times for sensitive operations

## Monitoring and Observability

### Metrics to Monitor

- Rate limit hit rates per limiter type
- IP addresses frequently hitting limits
- Peak request rates per endpoint category
- Rate limiter memory usage

### Recommended Alerts

- High rate limit hit rates (>10% of requests)
- Specific IPs hitting limits frequently
- OTP or password rate limits being hit (potential attacks)

## Performance Considerations

### Memory Usage

- Each IP address tracked in memory
- Automatic cleanup prevents memory leaks
- Consider Redis-based rate limiting for distributed deployments

### CPU Overhead

- Minimal CPU overhead per request
- Background cleanup goroutines
- Efficient token bucket implementation

## Configuration Examples

### Development Environment

```env
RATE_LIMIT_ENABLED=true
STANDARD_RATE_LIMIT=300.0
AUTH_RATE_LIMIT=100.0
PASSWORD_RATE_LIMIT=20.0
PUBLIC_RATE_LIMIT=500.0
```

### Production Environment

```env
RATE_LIMIT_ENABLED=true
STANDARD_RATE_LIMIT=120.0
AUTH_RATE_LIMIT=30.0
PASSWORD_RATE_LIMIT=5.0
OTP_RATE_LIMIT=3.0
PUBLIC_RATE_LIMIT=200.0
```

### Testing Environment

```env
RATE_LIMIT_ENABLED=false
```

## Troubleshooting

### Common Issues

1. **Rate Limits Too Restrictive**

   - Increase limits via environment variables
   - Monitor actual usage patterns
   - Consider burst capacity adjustments

2. **Rate Limits Not Working**

   - Check `RATE_LIMIT_ENABLED` setting
   - Verify specific limiter enabled flags
   - Check proxy header configuration

3. **Memory Usage Issues**
   - Monitor cleanup effectiveness
   - Consider shorter expiry durations
   - Implement Redis-based rate limiting for scale

### Debugging

- Enable debug logging for rate limiter hits
- Monitor rate limiter metrics
- Check IP extraction logic for proxy setups
