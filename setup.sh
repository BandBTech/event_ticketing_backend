#!/bin/bash

# Quick Start Script for Event Ticketing Backend
# This script helps you get started quickly

set -e

echo "🚀 Event Ticketing Backend - Quick Start"
echo "========================================"
echo ""

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.21 or higher."
    echo "   Visit: https://golang.org/doc/install"
    exit 1
fi

echo "✅ Go $(go version | awk '{print $3}') detected"

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "⚠️  Docker is not installed. Install Docker for containerized setup."
    echo "   Visit: https://docs.docker.com/get-docker/"
else
    echo "✅ Docker detected"
fi

echo ""
echo "Setting up the project..."
echo ""

# Copy environment file
if [ ! -f .env ]; then
    echo "📝 Creating .env file from .env.example..."
    cp .env.example .env
    echo "✅ .env file created. Please update it with your configuration."
else
    echo "✅ .env file already exists"
fi

# Download dependencies
echo ""
echo "📦 Downloading Go dependencies..."
go mod download
go mod tidy

# Install swag for Swagger documentation
echo ""
echo "📚 Installing Swagger CLI (swag)..."
if ! command -v swag &> /dev/null; then
    go install github.com/swaggo/swag/cmd/swag@latest
    echo "✅ Swagger CLI installed"
else
    echo "✅ Swagger CLI already installed"
fi

# Generate Swagger docs
echo ""
echo "📖 Generating Swagger documentation..."
swag init -g cmd/api/main.go -o docs
echo "✅ Swagger documentation generated"

echo ""
echo "========================================"
echo "✨ Setup Complete!"
echo "========================================"
echo ""
echo "Next steps:"
echo ""
echo "1. Update .env file with your configuration"
echo ""
echo "2. Start with Docker (recommended):"
echo "   $ make docker-up"
echo "   or"
echo "   $ docker-compose up -d"
echo ""
echo "3. Or run locally (requires PostgreSQL):"
echo "   $ make run"
echo "   or"
echo "   $ go run cmd/api/main.go"
echo ""
echo "4. Access the API:"
echo "   - API: http://localhost:8080"
echo "   - Health: http://localhost:8080/health"
echo "   - Swagger: http://localhost:8080/swagger/index.html"
echo ""
echo "5. View logs:"
echo "   $ make docker-logs"
echo "   or"
echo "   $ docker-compose logs -f"
echo ""
echo "6. Stop services:"
echo "   $ make docker-down"
echo "   or"
echo "   $ docker-compose down"
echo ""
echo "📚 Documentation:"
echo "   - README.md - General overview"
echo "   - docs/API.md - API documentation"
echo "   - docs/DEPLOYMENT.md - Deployment guide"
echo "   - docs/ARCHITECTURE.md - Architecture overview"
echo ""
echo "Happy coding! 🎉"
