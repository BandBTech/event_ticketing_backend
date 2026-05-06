# Atlas Migration Setup Guide

## Issue Fixed ✅

**Original Error:**

```
Error: schema.sql:1: pq: syntax error at or near "{" at position 1:20 (42601)
```

## Root Cause

The `schema.sql` file contained **HCL format** (HashiCorp Configuration Language), not SQL. This caused PostgreSQL to fail when trying to parse it as SQL.

## Solution Applied

### 1. **File Renaming**

- Renamed `schema.sql` → `schema.hcl` (proper HCL format file)
- Updated `atlas.hcl` to reference `file://schema.hcl` instead of `file://schema.sql`

### 2. **PostgreSQL Extension Setup**

- Enabled `uuid-ossp` extension in both production and dev databases:
  ```sql
  CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
  ```

### 3. **Development Database Configuration**

- Created separate dev database: `event_ticketing_dev`
- Enabled the same extensions in dev database for Atlas schema comparison
- Updated `atlas.hcl` to use: `dev = "postgres://postgres:postgres@localhost:5436/event_ticketing_dev?sslmode=disable"`

### 4. **Migration Generated**

- Successfully generated migration file: `20260506093545_final_sync.sql`
- This migration includes all 42 tables and their indexes

## Current Status

```
Migration Status: PENDING
  -- Current Version: 20260506090810 (baseline - empty)
  -- Next Version:    20260506093545 (final_sync - generated)
  -- Executed Files:  1
  -- Pending Files:   1
```

## Atlas Configuration (atlas.hcl)

```hcl
variable "database_url" {
  type = string
  default = "postgres://postgres:postgres@localhost:5436/event_ticketing?sslmode=disable"
}

env "local" {
  src = "file://schema.hcl"
  dev = "postgres://postgres:postgres@localhost:5436/event_ticketing_dev?sslmode=disable"
  url = var.database_url
  migration {
    dir = "file://migrations"
  }
}
```

## Usage Commands

### Check Migration Status

```bash
atlas migrate status --env local
```

### Generate New Migration (when schema changes)

```bash
atlas migrate diff <migration_name> --env local
```

### Apply Pending Migrations

```bash
atlas migrate apply --env local
```

### Generate New Migration via Script

```bash
./atlas.sh migrate diff <migration_name> --env local
```

## Schema Management (schema.hcl)

The `schema.hcl` file is the **single source of truth** for your database schema:

- Defined in **HCL (HashiCorp Configuration Language)** format
- Contains all 42 tables with columns, indexes, and constraints
- Atlas compares this against the actual database to generate migrations

## Best Practices

1. **Always update schema.hcl first** when adding/modifying tables
2. **Generate migrations** using `atlas migrate diff` after schema changes
3. **Review generated SQL** before applying to production
4. **Keep dev database in sync** with production schema structure
5. **Version control** both `schema.hcl` and migrations in the `migrations/` folder

## Environment Setup

### Local Development

- Main database: `event_ticketing` (port 5436)
- Dev database: `event_ticketing_dev` (port 5436)
- Both must have `uuid-ossp` extension enabled

### Staging/Production

- Update `staging_db_url` and `production_db_url` in `atlas.hcl`
- Ensure all databases have `uuid-ossp` extension installed

## Troubleshooting

### Error: "function public.uuid_generate_v4() does not exist"

**Solution:** Ensure the database has the `uuid-ossp` extension:

```bash
docker-compose exec postgres psql -U postgres -d event_ticketing -c 'CREATE EXTENSION IF NOT EXISTS "uuid-ossp";'
```

### Error: "pq: syntax error at or near "{""

**Solution:** Verify you're using `schema.hcl` (not `schema.sql`) and it contains valid HCL format.

### Migration not being recognized

1. Check `migrations/` directory exists
2. Verify dev database URL is correct and accessible
3. Run `atlas migrate status --env local` to debug

## Next Steps

1. Review the generated `20260506093545_final_sync.sql` migration
2. Apply it to your database: `atlas migrate apply --env local`
3. Verify migration status returns `OK`: `atlas migrate status --env local`
4. Create `.env` entries for staging/production databases
5. Set up CI/CD to run migrations on deployment

---

**Created:** May 6, 2026
**Atlas Version Requirement:** Community edition or higher
