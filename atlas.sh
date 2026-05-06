#!/bin/bash

# Load environment variables from .env file
if [ -f .env ]; then
    while IFS='=' read -r key value; do
        # Skip comments and empty lines
        [[ $key =~ ^[[:space:]]*# ]] && continue
        [[ -z $key ]] && continue
        # Export only valid variable names
        if [[ $key =~ ^[A-Z_][A-Z0-9_]*$ ]]; then
            export "$key=$value"
        fi
    done < .env
fi

# Override DATABASE_URL for local environment to use host port
if [ "$APP_ENV" = "local" ]; then
    export DATABASE_URL="postgres://$DB_USER:$DB_PASSWORD@localhost:5436/$DB_NAME?sslmode=$DB_SSLMODE"
fi

# Run atlas with the loaded environment
exec atlas "$@"