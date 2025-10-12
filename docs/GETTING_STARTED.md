# Getting Started

Welcome to the Event Ticketing Backend API! This guide will help you get started quickly.

## Prerequisites

- Go 1.21+ installed
- Docker & Docker Compose (for containerized setup)
- PostgreSQL 15+ (for local setup without Docker)

## Quick Start (< 5 minutes)

### Option 1: Automated Setup (Recommended)

Run the setup script:

```bash
./setup.sh
```

This will:

- Check prerequisites
- Copy `.env.example` to `.env`
- Download dependencies
- Install Swagger CLI
- Generate API documentation

Then start the services:

```bash
# With Docker
docker-compose up -d

# Without Docker (requires PostgreSQL running)
go run cmd/api/main.go
```

### Option 2: Manual Setup

1. **Copy environment file:**

```bash
cp .env.example .env
```

2. **Download dependencies:**

```bash
go mod download
go mod tidy
```

3. **Install Swagger CLI:**

```bash
go install github.com/swaggo/swag/cmd/swag@latest
```

4. **Generate Swagger docs:**

```bash
swag init -g cmd/api/main.go -o docs
```

5. **Start with Docker:**

```bash
docker-compose up -d
```

6. **Or run locally:**

```bash
go run cmd/api/main.go
```

## Verify Installation

Check if the API is running:

```bash
# Health check
curl http://localhost:8080/health

# Expected response:
# {"success":true,"message":"API is healthy","data":{"status":"up"}}
```

## Access Points

Once running, you can access:

- **API Base URL**: http://localhost:8080
- **Health Check**: http://localhost:8080/health
- **Database Health**: http://localhost:8080/health/db
- **Swagger UI**: http://localhost:8080/swagger/index.html
- **API v1 Endpoints**: http://localhost:8080/api/v1/\*

## Test the API

Create your first event:

```bash
curl -X POST http://localhost:8080/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "title": "My First Event",
    "description": "This is a test event",
    "location": "Online",
    "start_date": "2024-12-01T10:00:00Z",
    "end_date": "2024-12-01T18:00:00Z",
    "price": 49.99,
    "capacity": 100
  }'
```

Get all events:

```bash
curl http://localhost:8080/api/v1/events
```

## Common Commands

### Using Make

```bash
make help          # Show all available commands
make build         # Build the application
make run           # Run the application
make test          # Run tests
make swagger       # Generate Swagger docs
make docker-up     # Start Docker containers
make docker-down   # Stop Docker containers
make docker-logs   # View container logs
```

### Using Docker Compose

```bash
docker-compose up -d              # Start services
docker-compose down               # Stop services
docker-compose logs -f api        # View API logs
docker-compose logs -f postgres   # View database logs
docker-compose ps                 # Check service status
```

### Using Go directly

```bash
go run cmd/api/main.go           # Run the application
go build -o bin/api cmd/api/main.go  # Build binary
go test ./...                     # Run tests
go fmt ./...                      # Format code
```

## Project Structure

```
event_ticketing_backend/
├── cmd/api/main.go          # Main entry point
├── internal/                # Private application code
│   ├── database/           # Database connection
│   ├── handlers/           # HTTP handlers
│   ├── middleware/         # HTTP middleware
│   ├── models/             # Data models
│   ├── routes/             # Routes setup
│   └── services/           # Business logic
├── pkg/                    # Public packages
│   ├── config/            # Configuration
│   └── utils/             # Utilities
├── docs/                   # Documentation
├── docker-compose.yml     # Docker setup
├── Dockerfile             # Docker image
├── Makefile              # Build commands
├── .env.example          # Environment template
└── README.md             # Main documentation
```

## Environment Configuration

Edit `.env` file to configure:

```env
# Application
APP_ENV=local
PORT=8080

# Database
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=postgres
DB_NAME=event_ticketing
DB_SSLMODE=disable
```

## API Endpoints

### Health Checks

- `GET /health` - API health status
- `GET /health/db` - Database connectivity

### Events API (v1)

- `POST /api/v1/events` - Create event
- `GET /api/v1/events` - List all events
- `GET /api/v1/events/:id` - Get event by ID
- `PUT /api/v1/events/:id` - Update event
- `DELETE /api/v1/events/:id` - Delete event

### Documentation

- `GET /swagger/index.html` - Interactive API docs

## Troubleshooting

### Port already in use

```bash
# Kill process on port 8080
lsof -ti:8080 | xargs kill -9
```

### Docker issues

```bash
# Clean up Docker
docker-compose down -v
docker system prune -a

# Rebuild containers
docker-compose build --no-cache
docker-compose up -d
```

### Database connection failed

```bash
# Check PostgreSQL is running
docker-compose ps

# Check database logs
docker-compose logs postgres

# Verify connection settings in .env
```

### Swagger not loading

```bash
# Regenerate Swagger docs
swag init -g cmd/api/main.go -o docs

# Rebuild and restart
make build
make run
```

## Next Steps

1. **Read the documentation:**

   - [README.md](../README.md) - Project overview
   - [docs/API.md](API.md) - API documentation
   - [docs/DEPLOYMENT.md](DEPLOYMENT.md) - Deployment guide
   - [docs/ARCHITECTURE.md](ARCHITECTURE.md) - Architecture details

2. **Explore the API:**

   - Open Swagger UI: http://localhost:8080/swagger/index.html
   - Try the example requests
   - Create your own events

3. **Customize the code:**

   - Add new models in `internal/models/`
   - Create new handlers in `internal/handlers/`
   - Add business logic in `internal/services/`
   - Define new routes in `internal/routes/`

4. **Deploy:**
   - Follow [DEPLOYMENT.md](DEPLOYMENT.md) for deployment instructions
   - Configure for staging/production environments
   - Set up monitoring and logging

## Support

If you encounter any issues:

1. Check the logs: `docker-compose logs -f`
2. Review the documentation in `docs/`
3. Check environment variables in `.env`
4. Verify all prerequisites are installed

## Resources

- [Go Documentation](https://golang.org/doc/)
- [Gin Framework](https://gin-gonic.com/)
- [GORM](https://gorm.io/)
- [Swagger](https://swagger.io/)
- [Docker](https://docs.docker.com/)

Happy coding! 🚀
