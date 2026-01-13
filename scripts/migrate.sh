#!/bin/bash

# Migration helper script
# Usage: ./scripts/migrate.sh [up|down|version|force] [steps]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

# Load .env file more safely
if [ -f .env ]; then
    while IFS= read -r line; do
        # Skip empty lines and comments
        [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
        # Export valid environment variables
        if [[ "$line" =~ ^[A-Z_][A-Z0-9_]*= ]]; then
            export "$line"
        fi
    done < .env
fi

COMMAND=${1:-up}
STEPS=${2:-0}

echo "🔄 Running migration command: $COMMAND"

go run cmd/migrate/main.go -direction "$COMMAND" -steps "$STEPS"

echo "✅ Migration operation completed"
