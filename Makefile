.PHONY: help build run test clean docker-build docker-up docker-down swagger

help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

build: ## Build the application
	@echo "Building..."
	@go build -o bin/api cmd/api/main.go

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

docker-up: ## Start docker containers
	@echo "Starting docker containers..."
	@docker-compose up -d

docker-down: ## Stop docker containers
	@echo "Stopping docker containers..."
	@docker-compose down

docker-logs: ## View docker logs
	@docker-compose logs -f

migrate-up: ## Run database migrations
	@echo "Running migrations..."
	@go run cmd/api/main.go migrate

migrate-down: ## Rollback database migrations
	@echo "Rolling back migrations..."
	@go run cmd/api/main.go migrate-down
