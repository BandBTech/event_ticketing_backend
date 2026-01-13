#!/bin/bash

# Migration Creation Helper
# Usage: ./scripts/create-migration.sh "add_new_column_to_users"

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
MIGRATIONS_DIR="$PROJECT_ROOT/migrations"

if [ -z "$1" ]; then
    echo "❌ Error: Migration name is required"
    echo "Usage: ./scripts/create-migration.sh \"description_of_change\""
    echo ""
    echo "Examples:"
    echo "  ./scripts/create-migration.sh \"add_email_index\""
    echo "  ./scripts/create-migration.sh \"add_featured_column_to_events\""
    exit 1
fi

MIGRATION_NAME="$1"

# Find the highest existing migration number
if ls "$MIGRATIONS_DIR"/*.up.sql 1> /dev/null 2>&1; then
    LAST_VERSION=$(ls "$MIGRATIONS_DIR"/*.up.sql | grep -o '[0-9]\{6\}' | sort -n | tail -1)
    NEXT_VERSION=$(printf "%06d" $((10#$LAST_VERSION + 1)))
else
    NEXT_VERSION="000001"
fi

UP_FILE="$MIGRATIONS_DIR/${NEXT_VERSION}_${MIGRATION_NAME}.up.sql"
DOWN_FILE="$MIGRATIONS_DIR/${NEXT_VERSION}_${MIGRATION_NAME}.down.sql"

# Create up migration with template
cat > "$UP_FILE" << 'EOF'
-- Migration: Add description here
-- Created: $(date +"%Y-%m-%d %H:%M:%S")

-- Example: Add a new column
-- ALTER TABLE table_name ADD COLUMN column_name TYPE;

-- Example: Create an index (zero-downtime)
-- CREATE INDEX CONCURRENTLY idx_table_column ON table_name(column_name);

-- Example: Add a unique constraint
-- ALTER TABLE table_name ADD CONSTRAINT constraint_name UNIQUE (column1, column2);

-- Write your migration SQL here

EOF

# Create down migration with template
cat > "$DOWN_FILE" << 'EOF'
-- Rollback Migration: Add description here
-- Created: $(date +"%Y-%m-%d %H:%M:%S")

-- Example: Drop a column
-- ALTER TABLE table_name DROP COLUMN IF EXISTS column_name;

-- Example: Drop an index
-- DROP INDEX IF EXISTS idx_table_column;

-- Example: Drop a constraint
-- ALTER TABLE table_name DROP CONSTRAINT IF EXISTS constraint_name;

-- Write your rollback SQL here

EOF

echo "✅ Migration files created:"
echo "   UP:   $UP_FILE"
echo "   DOWN: $DOWN_FILE"
echo ""
echo "📝 Next steps:"
echo "   1. Edit the migration files with your SQL"
echo "   2. Test the migration: ./scripts/docker-migrate.sh up 1"
echo "   3. Test rollback: ./scripts/docker-migrate.sh down 1"
echo "   4. Apply for real: ./scripts/docker-migrate.sh up"
