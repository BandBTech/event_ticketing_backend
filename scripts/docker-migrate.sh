#!/bin/bash

# Docker Migration helper script
# Usage: ./scripts/docker-migrate.sh [up|down|version|force] [steps]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

COMMAND=${1:-up}
STEPS=${2:-0}

# Read APP_ENV from .env and set network name
APP_ENV=$(grep '^APP_ENV=' .env | cut -d'=' -f2)
if [ "$APP_ENV" = "local" ]; then
    NETWORK_NAME="event_ticketing_backend_default"
else
    NETWORK_NAME="sandboxtimroticketcom_default"
fi

echo "🔄 Running migration in Docker: $COMMAND (Network: $NETWORK_NAME)"

# Run migration command inside a temporary container
docker run --rm \
    --network $NETWORK_NAME \
    --env-file .env \
    -v "$PROJECT_ROOT/migrations:/migrations" \
    -w /app \
    golang:1.24-alpine \
    sh -c "
        apk add --no-cache git &&
        go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest &&
        migrate -path /migrations -database \"\$DATABASE_URL\" $COMMAND $STEPS
    "

echo "✅ Migration operation completed"