# Debugging Guide: Using Timestamps and Request IDs

## Overview

The `timestamp` and `request_id` fields in your API responses are powerful debugging tools that help track and diagnose issues in your Timro Tickets application.

## Understanding the Fields

### Timestamp

- **Format**: ISO 8601 UTC format (`2025-10-15T16:51:21Z`)
- **Purpose**: Exact time when the response was generated
- **Usage**: Track request timing, identify performance issues, correlate with logs

### Request ID

- **Format**: UUID (`c5e5ea5f-1445-4623-96c0-98649c1d49a4`)
- **Purpose**: Unique identifier for each HTTP request
- **Usage**: Trace request flow through microservices, correlate logs, debug specific issues

## 🔍 Debugging Techniques

### 1. **Time-based Debugging**

```bash
# Find all logs around a specific time
grep "2025-10-15T16:51" /var/log/timro-tickets/app.log

# Check for errors within 1 minute of the timestamp
grep -E "2025-10-15T16:5[0-2]" /var/log/timro-tickets/error.log
```

### 2. **Request ID Tracking**

```bash
# Track complete request flow
grep "c5e5ea5f-1445-4623-96c0-98649c1d49a4" /var/log/timro-tickets/*.log

# Database query logs for this request
grep "c5e5ea5f-1445-4623-96c0-98649c1d49a4" /var/log/timro-tickets/db.log
```

### 3. **Performance Analysis**

```bash
# Find slow requests around the same time
grep -E "2025-10-15T16:5[0-2].*[0-9]{3,}ms" /var/log/timro-tickets/app.log
```

## 🛠️ Enhanced Logging Implementation

### Structured Logging Example

```go
// In your handlers, add structured logging
log.WithFields(log.Fields{
    "request_id":   getRequestID(c),
    "method":       c.Request.Method,
    "path":         c.Request.URL.Path,
    "user_id":      getUserID(c),
    "ip":           c.ClientIP(),
    "user_agent":   c.Request.UserAgent(),
    "duration_ms":  time.Since(startTime).Milliseconds(),
}).Info("Request processed")

// For errors, include stack trace
log.WithFields(log.Fields{
    "request_id": getRequestID(c),
    "error":      err.Error(),
    "stack":      string(debug.Stack()),
}).Error("Request failed")
```

## 📊 Monitoring and Alerting

### 1. **Error Rate Monitoring**

```bash
# Count errors by request ID pattern (last hour)
grep -E "$(date -d '1 hour ago' '+%Y-%m-%dT%H')" /var/log/timro-tickets/error.log | \
wc -l
```

### 2. **Performance Monitoring**

```bash
# Find requests taking more than 5 seconds
grep -E "[0-9]{4,}ms" /var/log/timro-tickets/app.log
```

### 3. **User Impact Analysis**

```sql
-- Find all failed requests for a specific user (if logged)
SELECT request_id, timestamp, error_message, endpoint
FROM request_logs
WHERE user_id = 'user123'
  AND timestamp BETWEEN '2025-10-15T16:50:00Z' AND '2025-10-15T16:55:00Z'
  AND status_code >= 400;
```

## 🔧 Debugging Workflow

### Step 1: Identify the Issue

```json
{
  "success": false,
  "message": "Invalid request data",
  "error": {
    "code": "VALIDATION_ERROR",
    "details": "Confirm password and New password do not match"
  },
  "timestamp": "2025-10-15T16:51:21Z",
  "request_id": "c5e5ea5f-1445-4623-96c0-98649c1d49a4"
}
```

### Step 2: Trace the Request

```bash
# 1. Search all logs for this request ID
grep -r "c5e5ea5f-1445-4623-96c0-98649c1d49a4" /var/log/timro-tickets/

# 2. Check the timeframe around the issue
grep -A5 -B5 "2025-10-15T16:51:21" /var/log/timro-tickets/app.log

# 3. Look for related errors in the same minute
grep "2025-10-15T16:51" /var/log/timro-tickets/error.log
```

### Step 3: Analyze Context

```bash
# Check what happened before and after
grep -E "2025-10-15T16:5[0-2]" /var/log/timro-tickets/app.log | \
grep -E "(password|reset|validation)"
```

## 🐛 Common Debugging Scenarios

### Scenario 1: User Reports "Password Reset Not Working"

```bash
# User provides timestamp: 2025-10-15T16:51:21Z
# 1. Find their request
grep "2025-10-15T16:51:21" /var/log/timro-tickets/app.log

# 2. Check email service logs
grep -E "2025-10-15T16:5[0-2]" /var/log/timro-tickets/email.log

# 3. Verify OTP generation
grep -E "2025-10-15T16:5[0-2].*otp" /var/log/timro-tickets/app.log
```

### Scenario 2: 500 Internal Server Error

```bash
# Request ID: c5e5ea5f-1445-4623-96c0-98649c1d49a4
# 1. Find the exact error
grep "c5e5ea5f-1445-4623-96c0-98649c1d49a4" /var/log/timro-tickets/error.log

# 2. Check database connectivity
grep -E "2025-10-15T16:51.*database" /var/log/timro-tickets/db.log

# 3. Verify service health
curl -H "X-Request-ID: c5e5ea5f-1445-4623-96c0-98649c1d49a4" \
     http://localhost:8080/health
```

### Scenario 3: Performance Issues

```bash
# 1. Find slow requests around the timestamp
grep -E "2025-10-15T16:5[0-2].*[0-9]{3,}ms" /var/log/timro-tickets/app.log

# 2. Check resource usage
grep -E "2025-10-15T16:5[0-2]" /var/log/timro-tickets/metrics.log | \
grep -E "(cpu|memory|disk)"
```

## 📈 Log Aggregation with ELK Stack

### Elasticsearch Query Examples

```json
// Find all errors for a specific request ID
{
  "query": {
    "match": {
      "request_id": "c5e5ea5f-1445-4623-96c0-98649c1d49a4"
    }
  }
}

// Find errors in a time range
{
  "query": {
    "range": {
      "timestamp": {
        "gte": "2025-10-15T16:50:00Z",
        "lte": "2025-10-15T16:55:00Z"
      }
    }
  },
  "filter": {
    "term": { "level": "error" }
  }
}
```

## 🚨 Alerting Rules

### Prometheus Alerting Rules

```yaml
# High error rate alert
- alert: HighErrorRate
  expr: rate(http_requests_total{status=~"5.."}[5m]) > 0.1
  for: 2m
  labels:
    severity: warning
  annotations:
    summary: "High error rate detected"
    description: "Error rate is {{ $value }} errors per second"

# Slow response time alert
- alert: SlowResponseTime
  expr: histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m])) > 5
  for: 5m
  labels:
    severity: critical
  annotations:
    summary: "Slow response time detected"
    description: "95th percentile response time is {{ $value }} seconds"
```

## 🔍 Advanced Debugging Tools

### 1. **Request Tracing Script**

```bash
#!/bin/bash
# trace-request.sh
REQUEST_ID=$1
TIMESTAMP=$2

echo "=== Tracing Request: $REQUEST_ID ==="
echo "=== Timestamp: $TIMESTAMP ==="

# Extract minute for broader search
MINUTE=$(echo $TIMESTAMP | cut -c1-16)

echo -e "\n1. Application Logs:"
grep "$REQUEST_ID" /var/log/timro-tickets/app.log

echo -e "\n2. Error Logs:"
grep "$REQUEST_ID" /var/log/timro-tickets/error.log

echo -e "\n3. Context (same minute):"
grep "$MINUTE" /var/log/timro-tickets/app.log | grep -E "(ERROR|WARN)"

echo -e "\n4. Database Logs:"
grep "$REQUEST_ID" /var/log/timro-tickets/db.log
```

### 2. **Performance Analysis Script**

```bash
#!/bin/bash
# analyze-performance.sh
TIMESTAMP=$1
MINUTE=$(echo $TIMESTAMP | cut -c1-16)

echo "=== Performance Analysis for $MINUTE ==="

echo -e "\n1. Slow Requests (>1s):"
grep "$MINUTE" /var/log/timro-tickets/app.log | grep -E "[0-9]{4,}ms"

echo -e "\n2. Database Query Times:"
grep "$MINUTE" /var/log/timro-tickets/db.log | grep -E "duration:[0-9]+"

echo -e "\n3. Memory Usage:"
grep "$MINUTE" /var/log/timro-tickets/metrics.log | grep "memory"
```

## 💡 Best Practices

### 1. **Always Include Request Context**

- Log request ID with every log entry
- Include user ID when available
- Add endpoint and method information

### 2. **Structured Logging**

- Use JSON format for easier parsing
- Include consistent field names
- Add severity levels

### 3. **Correlation**

- Link related operations with request ID
- Include parent request IDs for async operations
- Use distributed tracing for microservices

### 4. **Retention Policy**

- Keep detailed logs for 30 days
- Archive summary logs for 1 year
- Implement log rotation to manage disk space

This debugging framework will help you quickly identify and resolve issues in your Timro Tickets application using the timestamp and request_id data!
