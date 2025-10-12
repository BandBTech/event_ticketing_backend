# Event Ticketing Backend - Architecture

## System Overview

This document provides a high-level overview of the Event Ticketing Backend architecture.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                        Load Balancer                         │
│                     (Nginx/HAProxy/ALB)                      │
└────────────────────────┬────────────────────────────────────┘
                         │
         ┌───────────────┼───────────────┐
         │               │               │
┌────────▼────────┐ ┌───▼──────────┐ ┌─▼──────────────┐
│   API Server    │ │ API Server   │ │  API Server    │
│   Instance 1    │ │ Instance 2   │ │  Instance 3    │
│   (Port 8080)   │ │ (Port 8080)  │ │  (Port 8080)   │
└────────┬────────┘ └───┬──────────┘ └─┬──────────────┘
         │              │              │
         └──────────────┼──────────────┘
                        │
                ┌───────▼────────┐
                │   PostgreSQL   │
                │    Database    │
                │   (Port 5432)  │
                └────────────────┘
```

## Components

### 1. API Server (Go + Gin)

- **Responsibility**: Handle HTTP requests, business logic, and data validation
- **Technology**: Go 1.21+, Gin framework
- **Scalability**: Stateless, horizontally scalable
- **Port**: 8080

### 2. Database (PostgreSQL)

- **Responsibility**: Persistent data storage
- **Technology**: PostgreSQL 15+
- **ORM**: GORM
- **Features**: ACID compliance, indexes, constraints

### 3. Load Balancer

- **Responsibility**: Distribute traffic across API instances
- **Options**: Nginx, HAProxy, AWS ALB, GCP Load Balancer
- **Features**: Health checks, SSL termination, rate limiting

## Project Structure Explained

```
cmd/
  api/
    main.go           # Application entry point, server initialization

internal/             # Private application code
  database/          # Database connection and management
  handlers/          # HTTP request handlers (controllers)
  middleware/        # HTTP middleware (logging, CORS, auth)
  models/            # Data models and DTOs
  routes/            # Route definitions
  services/          # Business logic layer

pkg/                 # Public, reusable packages
  config/            # Configuration management
  utils/             # Utility functions

docs/                # Documentation and API specs
configs/             # Configuration files
deployments/         # Deployment scripts and configs
```

## Request Flow

```
1. Client Request
   ↓
2. Load Balancer (SSL termination, routing)
   ↓
3. Middleware (CORS, logging, authentication)
   ↓
4. Router (route matching)
   ↓
5. Handler (request validation)
   ↓
6. Service (business logic)
   ↓
7. Database (GORM ORM)
   ↓
8. Response (standardized JSON)
```

## Layer Responsibilities

### Handler Layer

- Parse and validate HTTP requests
- Call appropriate service methods
- Format and return HTTP responses
- Handle HTTP-specific errors

### Service Layer

- Implement business logic
- Orchestrate operations
- Perform data transformations
- Handle business rules

### Database Layer

- Execute database operations
- Manage transactions
- Handle database errors
- Connection pooling

## API Versioning Strategy

Current: **v1**

```
/api/v1/events
/api/v1/tickets
/api/v1/users
```

Future versions:

- v2: Breaking changes
- v1: Maintained for backward compatibility
- Deprecation policy: 6 months notice

## Data Models

### Event

```go
type Event struct {
    ID          uint
    Title       string
    Description string
    Location    string
    StartDate   time.Time
    EndDate     time.Time
    Price       float64
    Capacity    int
    Available   int
    Status      string
    CreatedAt   time.Time
    UpdatedAt   time.Time
    DeletedAt   gorm.DeletedAt
}
```

## Error Handling

Standard error response:

```json
{
  "success": false,
  "message": "User-friendly message",
  "error": "Technical error details"
}
```

Status codes:

- 200: Success
- 201: Created
- 400: Bad Request
- 404: Not Found
- 500: Internal Server Error
- 503: Service Unavailable

## Configuration Management

Environment-based configuration:

- `.env` - Local development
- `.env.staging` - Staging environment
- `.env.production` - Production environment

Configuration is loaded at startup and cached.

## Database Schema

```sql
CREATE TABLE events (
    id SERIAL PRIMARY KEY,
    title VARCHAR(200) NOT NULL,
    description TEXT,
    location VARCHAR(200),
    start_date TIMESTAMP NOT NULL,
    end_date TIMESTAMP NOT NULL,
    price DECIMAL(10,2) NOT NULL,
    capacity INTEGER NOT NULL,
    available INTEGER NOT NULL,
    status VARCHAR(50) DEFAULT 'active',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX idx_events_status ON events(status);
CREATE INDEX idx_events_start_date ON events(start_date);
CREATE INDEX idx_events_deleted_at ON events(deleted_at);
```

## Middleware Stack

1. **Recovery**: Panic recovery
2. **Logger**: Request logging
3. **CORS**: Cross-origin resource sharing
4. **Authentication**: JWT validation (future)
5. **Rate Limiter**: Request throttling (future)

## Security Measures

- Input validation using Gin binding
- SQL injection prevention via GORM
- CORS configuration
- Environment variable for secrets
- SSL/TLS for database connections
- Graceful shutdown

## Scalability Considerations

### Horizontal Scaling

- Stateless API servers
- Session data in Redis (future)
- Load balancer distribution

### Database Scaling

- Read replicas for read-heavy operations
- Connection pooling (10-100 connections)
- Query optimization with indexes

### Caching Strategy (Future)

- Redis for frequently accessed data
- Cache invalidation on updates
- TTL-based expiration

## Monitoring and Observability

### Metrics (Future)

- Request count and latency
- Error rates
- Database query performance
- Resource utilization

### Logging

- Structured logging
- Request/response logging
- Error logging
- Audit logging

### Health Checks

- `/health` - API availability
- `/health/db` - Database connectivity

## Deployment Architecture

### Local (Docker Compose)

- Single node
- PostgreSQL container
- API container

### Staging

- Multiple API instances
- Managed PostgreSQL
- Load balancer

### Production

- Kubernetes cluster
- Managed database (RDS, Cloud SQL)
- Auto-scaling
- Multiple availability zones

## Future Enhancements

1. **Authentication & Authorization**

   - JWT-based authentication
   - Role-based access control (RBAC)
   - OAuth2 integration

2. **Ticket Booking System**

   - Booking model
   - Payment integration
   - Inventory management

3. **Notification Service**

   - Email notifications
   - SMS notifications
   - WebSocket real-time updates

4. **Analytics**

   - Event analytics
   - User behavior tracking
   - Sales reporting

5. **Search & Filtering**

   - Full-text search
   - Elasticsearch integration
   - Advanced filtering

6. **Caching**

   - Redis integration
   - Cache warming
   - Cache invalidation strategy

7. **Message Queue**
   - RabbitMQ/Kafka
   - Asynchronous processing
   - Event-driven architecture

## Performance Targets

- Response time: < 200ms (p95)
- Availability: 99.9%
- Throughput: 1000 req/s per instance
- Database queries: < 50ms (p95)

## Backup and Recovery

- Automated database backups (daily)
- Point-in-time recovery
- Backup retention: 30 days
- Disaster recovery plan

## Dependencies

Core dependencies:

- Gin: HTTP framework
- GORM: ORM
- PostgreSQL driver
- Swagger: API documentation
- godotenv: Environment variables

## Conclusion

This architecture provides a solid foundation for a scalable event ticketing system. It follows Go best practices, separates concerns, and is designed for horizontal scaling and maintainability.
