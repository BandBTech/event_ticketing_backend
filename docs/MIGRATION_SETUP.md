# Migration Setup - Quick Start Guide

## ✅ Setup Complete

Your project now has production-ready migrations using **golang-migrate**.

### What Changed

1. **OrganizerTierTemplate Unique Constraint Fixed**

   - Before: `template_name` was unique globally (only one organizer could use "VIP")
   - After: `(organizer_id, template_name)` is unique (each organizer can have their own "VIP" template)

2. **Migration System Setup**
   - Added `cmd/migrate/main.go` - Migration runner
   - Added `migrations/` directory with versioned SQL files
   - Added Makefile commands for dev workflow
   - Added `scripts/docker-migrate.sh` for staging/production

---

## Development Workflow (Local)

Use **Makefile** commands for quick development:

```bash
# 1. Create new migration
make migrate-create name=add_featured_to_events

# 2. Edit the generated SQL files
# migrations/000002_add_featured_to_events.up.sql
# migrations/000002_add_featured_to_events.down.sql

# 3. Apply migration
make migrate-up

# 4. Check version
make migrate-version

# 5. Rollback if needed
make migrate-down
```

**Example:**

```bash
# Create migration for adding a column
make migrate-create name=add_capacity_to_events

# Edit migrations/000002_add_capacity_to_events.up.sql:
# ALTER TABLE events ADD COLUMN capacity INTEGER;

# Edit migrations/000002_add_capacity_to_events.down.sql:
# ALTER TABLE events DROP COLUMN IF EXISTS capacity;

# Apply it
make migrate-up
```

---

## Staging/Production Workflow (VPS)

Use **scripts** for server deployments:

```bash
# On your VPS/staging server

# 1. Pull latest code with migration files
git pull origin staging

# 2. Apply migrations
./scripts/docker-migrate.sh up

# 3. Verify
./scripts/docker-migrate.sh version

# 4. If issues, rollback
./scripts/docker-migrate.sh down
```

---

## Quick Command Reference

### Development (Makefile)

```bash
make migrate-create name=description  # Create new migration
make migrate-up                       # Apply all migrations
make migrate-down                     # Rollback last migration
make migrate-version                  # Check current version
make migrate-force version=N          # Force version (fix dirty state)
```

### Staging/Production (Scripts)

```bash
./scripts/docker-migrate.sh up        # Apply migrations
./scripts/docker-migrate.sh down      # Rollback last
./scripts/docker-migrate.sh version   # Check version
```

---

## Creating New Migrations (Detailed)

### Method 1: Using Makefile (Recommended for Dev)

```bash
make migrate-create name=your_description
```

This automatically:

- Finds the next migration number
- Creates both `.up.sql` and `.down.sql` files
- Adds timestamps and templates

### Method 2: Manual Creation

```bash
# Check highest number
ls migrations/

# Create files
touch migrations/000002_your_change.up.sql
touch migrations/000002_your_change.down.sql
```

### Real Example: Add Email Index

```bash
# 1. Create migration
make migrate-create name=add_email_index_to_users

# 2. Edit migrations/000002_add_email_index_to_users.up.sql
CREATE INDEX CONCURRENTLY idx_users_email ON users(email) WHERE deleted_at IS NULL;

# 3. Edit migrations/000002_add_email_index_to_users.down.sql
DROP INDEX IF EXISTS idx_users_email;

# 4. Apply
make migrate-up
```

---

### Quick Commands

```bash
# Check current migration version
./scripts/docker-migrate.sh version

# Apply all pending migrations
./scripts/docker-migrate.sh up

# Rollback last migration
./scripts/docker-migrate.sh down

# Apply specific number of migrations
./scripts/docker-migrate.sh up 1
```

### Creating New Migrations

1. **Create migration files** (sequential numbering):

   ```bash
   # Using Makefile (Dev)
   make migrate-create name=add_new_field

   # Or manually
   touch migrations/000002_add_new_field.up.sql
   touch migrations/000002_add_new_field.down.sql
   ```

2. **Write the SQL**:

   ```sql
   -- migrations/000002_add_new_field.up.sql
   ALTER TABLE events ADD COLUMN is_featured BOOLEAN NOT NULL DEFAULT false;

   -- migrations/000002_add_new_field.down.sql
   ALTER TABLE events DROP COLUMN IF EXISTS is_featured;
   ```

3. **Apply the migration**:
   ```bash
   make migrate-up              # Dev
   ./scripts/docker-migrate.sh up  # Staging/Prod
   ```

### Environment Setup

The system uses `DATABASE_URL` from your `.env` file:

```env
DATABASE_URL=postgres://postgres:postgres@postgres:5432/event_ticketing?sslmode=disable
```

### Docker Compose Integration

Migrations run using Docker, connecting to your `postgres` service in docker-compose.

```bash
# Make sure database is running
docker-compose up -d postgres

# Then run migrations
./scripts/docker-migrate.sh up
```

### Best Practices

1. **Always create both up and down migrations**
2. **Test migrations locally before deploying**:
   ```bash
   ./scripts/docker-migrate.sh up 1    # Test up
   ./scripts/docker-migrate.sh down 1  # Test down
   ./scripts/docker-migrate.sh up 1    # Re-apply
   ```
3. **Use zero-downtime techniques for production**:

   - `CREATE INDEX CONCURRENTLY` for indexes
   - Add columns without NOT NULL first, then add constraint later
   - Use partial indexes: `WHERE deleted_at IS NULL`

4. **Never modify applied migrations** - Create a new migration instead

### Troubleshooting

**If migration fails:**

```bash
# Check the current version and dirty state
./scripts/docker-migrate.sh version

# If dirty, force to last known good version
docker run --rm \
  --network event_ticketing_backend_default \
  --env-file .env \
  -v "$PWD/migrations:/migrations" \
  golang:1.24-alpine sh -c "
    apk add git && \
    go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest && \
    migrate -path /migrations -database \$DATABASE_URL force <version>
  "
```

### Additional Resources

- See [MIGRATION_GUIDE.md](MIGRATION_GUIDE.md) for comprehensive documentation
- Migration files: [migrations/](migrations/)
- Migration runner: [cmd/migrate/main.go](cmd/migrate/main.go)

## Current Status

✅ Migration 000001 applied: Organizer template names are now unique per organizer
