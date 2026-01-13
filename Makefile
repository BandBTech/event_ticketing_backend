.PHONY: help build run test clean docker-build docker-up docker-down swagger

help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

build: ## Build the application
	@echo "Building..."
	@go build -o bin/api cmd/api/main.go

build-all: ## Generate swagger, build application and build docker image
	@echo "Running full build process..."
	@echo "1. Generating swagger docs..."
	@swag init -g cmd/api/main.go -o docs
	@echo "2. Building application..."
	@go build -o bin/api cmd/api/main.go
	@echo "3. Building docker image with BuildKit..."
	@DOCKER_BUILDKIT=1 docker-compose build
	@echo "✅ Full build completed successfully!"

build-all-up: ## Generate swagger, build application, build docker image and start containers
	@echo "Running full build and start process..."
	@echo "1. Generating swagger docs..."
	@swag init -g cmd/api/main.go -o docs
	@echo "2. Building application..."
	@go build -o bin/api cmd/api/main.go
	@echo "3. Building and starting docker containers..."
	@DOCKER_BUILDKIT=1 docker-compose up -d --build
	@echo "✅ Full build and start completed successfully!"

run: ## Run the application
	@echo "Running..."
	@go run cmd/api/main.go

test: ## Run tests
	@echo "Running tests..."
	@go test -v ./...

clean: ## Clean build files
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -rf tmp/

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

swagger: ## Generate swagger documentation
	@echo "Generating swagger docs..."
	@swag init -g cmd/api/main.go -o docs

docker-build: ## Build docker image
	@echo "Building docker image..."
	@docker-compose build

docker-build-fast: ## Build docker image with BuildKit (faster)
	@echo "Building docker image with BuildKit..."
	@DOCKER_BUILDKIT=1 docker-compose build

docker-up: ## Start docker containers
	@echo "Starting docker containers..."
	@docker-compose up -d

docker-down: ## Stop docker containers
	@echo "Stopping docker containers..."
	@docker-compose down

docker-logs: ## View docker logs
	@docker-compose logs -f

# ============================================
# Migration Commands (Dev Mode)
# ============================================

migrate-create: ## Create new migration files (usage: make migrate-create name=add_column_name)
	@if [ -z "$(name)" ]; then \
		echo "❌ Error: Migration name is required"; \
		echo "Usage: make migrate-create name=your_migration_name"; \
		echo "Example: make migrate-create name=add_featured_to_events"; \
		exit 1; \
	fi; \
	LAST_VERSION=$$(ls migrations/*.up.sql 2>/dev/null | grep -o '[0-9]\{6\}' | sort -n | tail -1); \
	if [ -z "$$LAST_VERSION" ]; then \
		NEXT_VERSION="000001"; \
	else \
		NEXT_VERSION=$$(printf "%06d" $$((10#$$LAST_VERSION + 1))); \
	fi; \
	UP_FILE="migrations/$${NEXT_VERSION}_$(name).up.sql"; \
	DOWN_FILE="migrations/$${NEXT_VERSION}_$(name).down.sql"; \
	echo "-- Migration: $(name)" > $$UP_FILE; \
	echo "-- Created: $$(date '+%Y-%m-%d %H:%M:%S')" >> $$UP_FILE; \
	echo "" >> $$UP_FILE; \
	echo "-- Write your migration SQL here" >> $$UP_FILE; \
	echo "" >> $$UP_FILE; \
	echo "-- Rollback: $(name)" > $$DOWN_FILE; \
	echo "-- Created: $$(date '+%Y-%m-%d %H:%M:%S')" >> $$DOWN_FILE; \
	echo "" >> $$DOWN_FILE; \
	echo "-- Write your rollback SQL here" >> $$DOWN_FILE; \
	echo "" >> $$DOWN_FILE; \
	echo "✅ Migration files created:"; \
	echo "   UP:   $$UP_FILE"; \
	echo "   DOWN: $$DOWN_FILE"

migrate-up: ## Apply all pending migrations (Docker)
	@echo "🔄 Running migrations..."
	@./scripts/docker-migrate.sh up

migrate-down: ## Rollback last migration (Docker)
	@echo "🔄 Rolling back last migration..."
	@./scripts/docker-migrate.sh down

migrate-version: ## Show current migration version
	@./scripts/docker-migrate.sh version

migrate-force: ## Force migration version (usage: make migrate-force version=1)
	@if [ -z "$(version)" ]; then \
		echo "❌ Error: Version number is required"; \
		echo "Usage: make migrate-force version=N"; \
		exit 1; \
	fi; \
	docker run --rm \
		--network event_ticketing_backend_default \
		--env-file .env \
		-v "$(PWD)/migrations:/migrations" \
		golang:1.24-alpine sh -c " \
			apk add --no-cache git && \
			go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest && \
			migrate -path /migrations -database \$$DATABASE_URL force $(version) \
		"
