# Timro Ticket API

A scalable, production-ready REST API for an event ticketing system built with Go, Gin, GORM, and PostgreSQL. Features Docker support for multiple environments, comprehensive Swagger documentation, and follows best practices for API versioning and project structure.

## 🚀 Features

- ✅ RESTful API with versioning (v1)
- ✅ Clean architecture with separation of concerns
- ✅ GORM ORM for database operations
- ✅ PostgreSQL database support
- ✅ Swagger/OpenAPI documentation
- ✅ Docker support (local, staging, production)
- ✅ Health check endpoints
- ✅ CORS middleware
- ✅ Request logging
- ✅ Graceful shutdown
- ✅ Environment-based configuration

## 📁 Project Structure

```
event_ticketing_backend/
├── cmd/
│   └── api/
│       └── main.go                 # Application entry point
├── internal/
│   ├── database/
│   │   └── database.go            # Database connection & config
│   ├── handlers/
│   │   ├── event_handler.go       # Event HTTP handlers
│   │   └── health_handler.go      # Health check handlers
│   ├── middleware/
│   │   ├── cors.go                # CORS middleware
│   │   └── logger.go              # Logging middleware
│   ├── models/
│   │   └── event.go               # Event model & DTOs
│   ├── routes/
│   │   └── routes.go              # Route definitions
│   └── services/
│       └── event_service.go       # Business logic layer
├── pkg/
│   ├── config/
│   │   └── config.go              # Configuration management
│   └── utils/
│       └── response.go            # Standard response helpers
├── docs/                          # Swagger documentation (auto-generated)
├── configs/                       # Configuration files
├── deployments/                   # Deployment scripts
├── .env.example                   # Environment variables template
├── .gitignore                     # Git ignore rules
├── docker-compose.yml             # Docker setup
├── Dockerfile                     # Docker image definition
├── Makefile                       # Build automation
├── go.mod                         # Go module definition
└── README.md                      # This file
```

## 🛠️ Prerequisites

- Go 1.21 or higher
- Docker & Docker Compose (for containerized deployment)
- PostgreSQL 15+ (if running locally without Docker)
- Make (optional, for using Makefile commands)

## 🏃 Quick Start

### 1. Clone the repository

```bash
cd event_ticketing_backend
```

### 2. Set up environment variables

```bash
cp .env.example .env
```

Edit `.env` with your configuration:

```env
APP_ENV=local
APP_NAME=Event Ticketing API
APP_VERSION=1.0.0
PORT=8080

DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=postgres
DB_NAME=event_ticketing
DB_SSLMODE=disable
```

### 3. Install dependencies

```bash
go mod download
```

### 4. Generate Swagger documentation

```bash
# Install swag CLI
go install github.com/swaggo/swag/cmd/swag@latest

# Generate docs
make swagger
# or
swag init -g cmd/api/main.go -o docs
```

### 5. Run with Docker (Recommended)

```bash
# Start all services (API + PostgreSQL)
make docker-up
# or
docker-compose up -d

# View logs
make docker-logs
# or
docker-compose logs -f

# Stop services
make docker-down
# or
docker-compose down
```

### 6. Run locally without Docker

```bash
# Make sure PostgreSQL is running
# Update .env with your local PostgreSQL connection details

# Run the application
make run
# or
go run cmd/api/main.go
```

## 📚 API Documentation

Once the application is running, access the interactive Swagger documentation at:

```
http://localhost:8080/swagger/index.html
```

### Available Endpoints

#### Health Checks

- `GET /health` - API health check
- `GET /health/db` - Database health check

#### Events (v1)

- `POST /api/v1/events` - Create a new event
- `GET /api/v1/events` - Get all events
- `GET /api/v1/events/:id` - Get event by ID
- `PUT /api/v1/events/:id` - Update event
- `DELETE /api/v1/events/:id` - Delete event

### Example Request

**Create Event:**

```bash
curl -X POST http://localhost:8080/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Tech Conference 2024",
    "description": "Annual technology conference",
    "location": "San Francisco, CA",
    "start_date": "2024-06-15T09:00:00Z",
    "end_date": "2024-06-17T18:00:00Z",
    "price": 299.99,
    "capacity": 500
  }'
```

**Response:**

```json
{
  "success": true,
  "message": "Event created successfully",
  "data": {
    "id": 1,
    "title": "Tech Conference 2024",
    "description": "Annual technology conference",
    "location": "San Francisco, CA",
    "start_date": "2024-06-15T09:00:00Z",
    "end_date": "2024-06-17T18:00:00Z",
    "price": 299.99,
    "capacity": 500,
    "available": 500,
    "status": "active",
    "created_at": "2024-01-15T10:30:00Z",
    "updated_at": "2024-01-15T10:30:00Z"
  }
}
```

## 🐳 Docker Deployment

### Deployment

```bash
# Using Docker Compose
docker-compose up -d

# Using the deployment script
./deploy.sh deploy
```

## 🔧 Development

### Build the application

```bash
make build
```

### Run tests

```bash
make test
```

### Clean build artifacts

```bash
make clean
```

### Format code

```bash
go fmt ./...
```

### Lint code

```bash
golangci-lint run
```

## 🗄️ Database

The application uses GORM for ORM and automatically runs migrations on startup. The Event model includes:

- `id` - Primary key
- `title` - Event title (required)
- `description` - Event description
- `location` - Event location
- `start_date` - Event start date/time (required)
- `end_date` - Event end date/time (required)
- `price` - Ticket price (required, min: 0)
- `capacity` - Total capacity (required, min: 1)
- `available` - Available tickets (auto-set to capacity)
- `status` - Event status (default: "active")
- `created_at` - Creation timestamp
- `updated_at` - Last update timestamp
- `deleted_at` - Soft delete timestamp

## 🌍 Environment Variables

| Variable             | Description                            | Default             |
| -------------------- | -------------------------------------- | ------------------- |
| APP_ENV              | Environment (local/staging/production) | local               |
| APP_NAME             | Application name                       | Event Ticketing API |
| APP_VERSION          | Application version                    | 1.0.0               |
| PORT                 | Server port                            | 8080                |
| DB_HOST              | Database host                          | localhost           |
| DB_PORT              | Database port                          | 5432                |
| DB_USER              | Database user                          | postgres            |
| DB_PASSWORD          | Database password                      | postgres            |
| DB_NAME              | Database name                          | event_ticketing     |
| DB_SSLMODE           | PostgreSQL SSL mode                    | disable             |
| SERVER_READ_TIMEOUT  | HTTP read timeout                      | 30s                 |
| SERVER_WRITE_TIMEOUT | HTTP write timeout                     | 30s                 |
| SERVER_IDLE_TIMEOUT  | HTTP idle timeout                      | 60s                 |

## 🚦 Health Checks

The API includes two health check endpoints:

1. **API Health Check**: `GET /health`
   - Returns 200 if the API is running

2. **Database Health Check**: `GET /health/db`
   - Returns 200 if database connection is healthy
   - Returns 503 if database is unavailable

## 🔐 Security Considerations

For production deployment:

1. Use environment variables for sensitive data
2. Enable SSL/TLS for database connections (set `DB_SSLMODE=require`)
3. Implement authentication & authorization middleware
4. Add rate limiting
5. Use HTTPS for API endpoints
6. Implement input validation and sanitization
7. Set up proper CORS policies
8. Use secrets management (e.g., Vault, AWS Secrets Manager)

## 📈 Scaling Considerations

This architecture is designed to scale:

1. **Horizontal Scaling**: Run multiple API instances behind a load balancer
2. **Database**: Use PostgreSQL read replicas for read-heavy workloads
3. **Caching**: Add Redis for frequently accessed data
4. **Message Queue**: Implement RabbitMQ/Kafka for async operations
5. **Monitoring**: Add Prometheus & Grafana for metrics
6. **Logging**: Centralize logs with ELK stack or similar

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📝 License

This project is licensed under the MIT License.

## 📧 Contact

For questions or support, please open an issue in the repository.

---

**Happy Coding! 🎉**
