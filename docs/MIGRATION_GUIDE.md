# Production-Ready Migration Setup with golang-migrate

This project uses `golang-migrate/migrate` for database schema migrations.

## Quick Start

### 1. Install Dependencies

```bash
go get -u github.com/golang-migrate/migrate/v4
go get -u github.com/golang-migrate/migrate/v4/database/postgres
go get -u github.com/lib/pq
```

### 2. Run Migrations

```bash
# Apply all pending migrations
./scripts/migrate.sh up

# Rollback last migration
./scripts/migrate.sh down

# Check current version
./scripts/migrate.sh version

# Apply specific number of migrations
./scripts/migrate.sh up 1
```

Or use the Go binary directly:

```bash
go run cmd/migrate/main.go -direction up
go run cmd/migrate/main.go -direction down
go run cmd/migrate/main.go -direction version
```

## Creating New Migrations

### Manual Creation

Create two files in the `migrations/` directory:

```bash
# Up migration
migrations/000002_your_migration_name.up.sql

# Down migration
migrations/000002_your_migration_name.down.sql
```

**Naming Convention:** `{version}_{description}.{up|down}.sql`

- Version: Sequential number with leading zeros (000001, 000002, etc.)
- Description: Snake_case description of the change

### Using migrate CLI (Optional)

Install the CLI tool:

```bash
# macOS
brew install golang-migrate

# Or using Go
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

Create migration:

```bash
migrate create -ext sql -dir migrations -seq add_user_index
```

## Migration Best Practices

### 1. Always Test Migrations

```bash
# Test up
./scripts/migrate.sh up 1

# Test down
./scripts/migrate.sh down 1

# Re-apply
./scripts/migrate.sh up 1
```

### 2. Zero-Downtime Migrations

- Use `CREATE INDEX CONCURRENTLY` for indexes
- Add columns without NOT NULL first, backfill data, then add constraint
- Use partial indexes with `WHERE deleted_at IS NULL` for soft deletes
- Avoid long-running ALTER TABLE statements

Example:

```sql
-- Good: Non-blocking index creation
CREATE INDEX CONCURRENTLY idx_users_email ON users(email);

-- Bad: Blocking index creation
CREATE INDEX idx_users_email ON users(email);
```

### 3. Always Write Down Migrations

Every `.up.sql` must have a corresponding `.down.sql` that reverses the changes.

### 4. Use Transactions Carefully

PostgreSQL wraps each migration file in a transaction by default. Some operations like `CREATE INDEX CONCURRENTLY` cannot run in a transaction.

To disable transactions for a specific migration:

```sql
-- Add this comment at the top
-- +migrate NoTransaction
CREATE INDEX CONCURRENTLY idx_users_email ON users(email);
```

## Environment Variables

The migration tool uses the following environment variables (from `.env`):

```bash
# Option 1: Use DATABASE_URL (preferred for migrations)
DATABASE_URL=postgres://user:password@host:port/dbname?sslmode=disable

# Option 2: Use individual variables (fallback)
DB_HOST=postgres
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=postgres
DB_NAME=event_ticketing
DB_SSLMODE=disable
```

## Migration Workflow

### Development

```bash
# Create migration
touch migrations/000002_add_feature.up.sql
touch migrations/000002_add_feature.down.sql

# Edit migration files with SQL

# Apply migration
./scripts/migrate.sh up

# Test rollback
./scripts/migrate.sh down

# Re-apply
./scripts/migrate.sh up
```

### Staging

```bash
# Pull latest code
git pull origin staging

# Apply migrations
./scripts/migrate.sh up

# Verify application works

# If issues, rollback
./scripts/migrate.sh down
```

### Production

**Manual Approach:**

```bash
# 1. Backup database first
pg_dump -h host -U user -d dbname > backup_$(date +%Y%m%d_%H%M%S).sql

# 2. Apply migrations
./scripts/migrate.sh up

# 3. Verify
./scripts/migrate.sh version

# 4. If issues, rollback
./scripts/migrate.sh down
```

**CI/CD Approach (Recommended):**

```yaml
# Example GitHub Actions workflow
- name: Run Database Migrations
  env:
    DATABASE_URL: ${{ secrets.DATABASE_URL }}
  run: |
    go run cmd/migrate/main.go -direction up
```

## Troubleshooting

### Migration Failed - Database is Dirty

If a migration fails halfway, the database may be marked as "dirty":

```bash
# Force to the last known good version
go run cmd/migrate/main.go -direction force -steps <version_number>

# Then fix the issue and re-run
./scripts/migrate.sh up
```

### Check Migration Status

```bash
# View current version
./scripts/migrate.sh version

# Or check the database directly
psql -h host -U user -d dbname -c "SELECT * FROM schema_migrations;"
```

### Reset Database (Development Only!)

```bash
# Rollback all migrations
./scripts/migrate.sh down

# Or manually
go run cmd/migrate/main.go -direction down -steps 999
```

## PostgreSQL-Specific Tips

### 1. Adding Columns Safely

```sql
-- Step 1: Add nullable column
ALTER TABLE users ADD COLUMN phone_number VARCHAR(20);

-- Step 2: Backfill data (separate migration if large table)
UPDATE users SET phone_number = 'default' WHERE phone_number IS NULL;

-- Step 3: Add NOT NULL constraint (separate migration)
ALTER TABLE users ALTER COLUMN phone_number SET NOT NULL;
```

### 2. Renaming Columns

```sql
-- Safe: PostgreSQL handles this instantly
ALTER TABLE users RENAME COLUMN old_name TO new_name;
```

### 3. Adding Indexes

```sql
-- Zero-downtime
CREATE INDEX CONCURRENTLY idx_users_created_at ON users(created_at);

-- With partial index for soft deletes
CREATE INDEX CONCURRENTLY idx_users_email
ON users(email)
WHERE deleted_at IS NULL;
```

### 4. Composite Unique Constraints

```sql
-- Ensure unique combination
CREATE UNIQUE INDEX idx_organizer_template
ON organizer_tier_templates(organizer_id, template_name)
WHERE deleted_at IS NULL;
```

## Current Migrations

### 000001_fix_organizer_template_unique_constraint

**Purpose:** Fix the unique constraint on `organizer_tier_templates` table to allow the same template name across different organizers.

**Change:**

- **Before:** `template_name` had a unique constraint globally
- **After:** Composite unique constraint on `(organizer_id, template_name)`

**Impact:** Different organizers can now use the same template name (e.g., "VIP", "General Admission").

## Integration with Existing Code

The GORM AutoMigrate in `cmd/api/main.go` is still present but you should:

1. **Development:** Use golang-migrate for schema changes
2. **Production:** Never use AutoMigrate, only use golang-migrate
3. **Model Changes:** Update the model, then create a migration file

### Example: Adding a New Field

```go
// 1. Update the model (internal/models/event.go)
type Event struct {
    // ... existing fields
    IsVirtual bool `gorm:"not null;default:false" json:"is_virtual"`
}

// 2. Create migration
touch migrations/000003_add_is_virtual_to_events.up.sql
touch migrations/000003_add_is_virtual_to_events.down.sql

// 3. Write SQL
-- 000003_add_is_virtual_to_events.up.sql
ALTER TABLE events ADD COLUMN is_virtual BOOLEAN NOT NULL DEFAULT false;

-- 000003_add_is_virtual_to_events.down.sql
ALTER TABLE events DROP COLUMN IF EXISTS is_virtual;

// 4. Apply migration
./scripts/migrate.sh up
```

## Resources

- [golang-migrate Documentation](https://github.com/golang-migrate/migrate)
- [PostgreSQL ALTER TABLE](https://www.postgresql.org/docs/current/sql-altertable.html)
- [PostgreSQL CREATE INDEX](https://www.postgresql.org/docs/current/sql-createindex.html)
