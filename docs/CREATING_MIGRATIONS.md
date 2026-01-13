# How to Create Database Migrations

## Quick Method (Recommended)

### 1. Use the Helper Script

```bash
./scripts/create-migration.sh "description_of_your_change"
```

**Examples:**

```bash
# Adding a column
./scripts/create-migration.sh "add_featured_to_events"

# Adding an index
./scripts/create-migration.sh "add_email_index_to_users"

# Modifying a constraint
./scripts/create-migration.sh "change_organizer_template_unique"
```

This will:

- Auto-increment the migration number (000001, 000002, etc.)
- Create both `.up.sql` and `.down.sql` files
- Add helpful templates with examples

### 2. Edit the SQL Files

Open the generated files and write your SQL:

**Example: Adding a column**

```sql
-- migrations/000002_add_featured_to_events.up.sql
ALTER TABLE events
ADD COLUMN is_featured BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN events.is_featured IS 'Whether event is featured on homepage';
```

```sql
-- migrations/000002_add_featured_to_events.down.sql
ALTER TABLE events
DROP COLUMN IF EXISTS is_featured;
```

### 3. Test & Apply

```bash
# Test up migration
./scripts/docker-migrate.sh up 1

# Verify it worked (check your table)
docker exec -it event_ticketing_db psql -U postgres -d event_ticketing -c "\d events"

# Test rollback
./scripts/docker-migrate.sh down 1

# If all good, apply it
./scripts/docker-migrate.sh up
```

---

## Manual Method (Alternative)

If you prefer to create files manually:

```bash
# 1. Check current highest migration number
ls migrations/

# 2. Create new files (increment the number)
touch migrations/000002_your_change.up.sql
touch migrations/000002_your_change.down.sql

# 3. Write your SQL in both files
```

---

## Real-World Examples

### Example 1: Add a New Column

**Scenario:** Add `phone_number` to users table

```sql
-- 000002_add_phone_to_users.up.sql
ALTER TABLE users
ADD COLUMN phone_number VARCHAR(20);

-- Add an index if you'll search by phone
CREATE INDEX idx_users_phone ON users(phone_number);
```

```sql
-- 000002_add_phone_to_users.down.sql
DROP INDEX IF EXISTS idx_users_phone;
ALTER TABLE users DROP COLUMN IF EXISTS phone_number;
```

### Example 2: Add NOT NULL Constraint (Zero-Downtime)

**Scenario:** Make email required (already has data)

**Step 1: Add nullable column**

```sql
-- 000003_add_email_column.up.sql
ALTER TABLE organizers ADD COLUMN email VARCHAR(255);
```

**Step 2: Backfill data** (separate migration)

```sql
-- 000004_backfill_organizer_emails.up.sql
UPDATE organizers
SET email = CONCAT(id, '@temp.example.com')
WHERE email IS NULL;
```

**Step 3: Add NOT NULL constraint**

```sql
-- 000005_make_email_required.up.sql
ALTER TABLE organizers
ALTER COLUMN email SET NOT NULL;

CREATE UNIQUE INDEX idx_organizers_email ON organizers(email);
```

### Example 3: Create Index (Zero-Downtime)

**Scenario:** Speed up event queries by date

```sql
-- 000006_add_event_date_index.up.sql
CREATE INDEX CONCURRENTLY idx_events_start_date
ON events(start_date)
WHERE deleted_at IS NULL;
```

```sql
-- 000006_add_event_date_index.down.sql
DROP INDEX IF EXISTS idx_events_start_date;
```

### Example 4: Add Composite Unique Constraint

**Scenario:** Ensure unique (user_id, event_id) for bookings

```sql
-- 000007_unique_user_event_booking.up.sql
CREATE UNIQUE INDEX idx_unique_user_event_booking
ON bookings(user_id, event_id)
WHERE deleted_at IS NULL;
```

```sql
-- 000007_unique_user_event_booking.down.sql
DROP INDEX IF EXISTS idx_unique_user_event_booking;
```

### Example 5: Rename Column

**Scenario:** Rename `tier_name` to `name` in event_tiers table

```sql
-- 000008_rename_tier_name_column.up.sql
ALTER TABLE event_tiers
RENAME COLUMN tier_name TO name;
```

```sql
-- 000008_rename_tier_name_column.down.sql
ALTER TABLE event_tiers
RENAME COLUMN name TO tier_name;
```

### Example 6: Add Foreign Key

**Scenario:** Link tickets to events

```sql
-- 000009_add_event_foreign_key_to_tickets.up.sql
ALTER TABLE tickets
ADD CONSTRAINT fk_tickets_event_id
FOREIGN KEY (event_id)
REFERENCES events(id)
ON DELETE CASCADE;
```

```sql
-- 000009_add_event_foreign_key_to_tickets.down.sql
ALTER TABLE tickets
DROP CONSTRAINT IF EXISTS fk_tickets_event_id;
```

### Example 7: Create New Table

**Scenario:** Add notifications table

```sql
-- 000010_create_notifications_table.up.sql
CREATE TABLE notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    type VARCHAR(50) NOT NULL,
    is_read BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_notifications_user_id ON notifications(user_id);
CREATE INDEX idx_notifications_is_read ON notifications(is_read);
```

```sql
-- 000010_create_notifications_table.down.sql
DROP TABLE IF EXISTS notifications;
```

---

## PostgreSQL-Specific Tips

### 1. Use CONCURRENTLY for Indexes (Zero-Downtime)

❌ **Bad** (locks table):

```sql
CREATE INDEX idx_users_email ON users(email);
```

✅ **Good** (no lock):

```sql
CREATE INDEX CONCURRENTLY idx_users_email ON users(email);
```

### 2. Use Partial Indexes for Soft Deletes

```sql
CREATE INDEX idx_active_events
ON events(created_at)
WHERE deleted_at IS NULL;
```

### 3. Add Constraints Safely

For large tables, add constraint in steps:

```sql
-- Step 1: Add as NOT VALID (fast)
ALTER TABLE large_table
ADD CONSTRAINT check_positive_amount
CHECK (amount > 0) NOT VALID;

-- Step 2: Validate in background (in a separate migration)
ALTER TABLE large_table
VALIDATE CONSTRAINT check_positive_amount;
```

### 4. Use Comments for Documentation

```sql
ALTER TABLE events ADD COLUMN capacity INTEGER;
COMMENT ON COLUMN events.capacity IS 'Maximum number of attendees allowed';
```

---

## Workflow

### Standard Workflow

```bash
# 1. Create migration
./scripts/create-migration.sh "add_feature_x"

# 2. Write SQL in the generated files
vim migrations/000XXX_add_feature_x.up.sql
vim migrations/000XXX_add_feature_x.down.sql

# 3. Apply migration
./scripts/docker-migrate.sh up

# 4. Verify
docker exec -it event_ticketing_db psql -U postgres -d event_ticketing -c "\d your_table"
```

### Testing Workflow

```bash
# 1. Apply one migration
./scripts/docker-migrate.sh up 1

# 2. Check database
docker exec -it event_ticketing_db psql -U postgres -d event_ticketing

# 3. Test rollback
./scripts/docker-migrate.sh down 1

# 4. Verify rollback worked
docker exec -it event_ticketing_db psql -U postgres -d event_ticketing

# 5. Re-apply if good
./scripts/docker-migrate.sh up 1
```

---

## Common Migration Patterns

### Pattern 1: Add Column with Default

```sql
-- Safe for large tables
ALTER TABLE events
ADD COLUMN status VARCHAR(20) DEFAULT 'draft';
```

### Pattern 2: Change Column Type

```sql
-- For compatible types
ALTER TABLE events
ALTER COLUMN capacity TYPE BIGINT;

-- For incompatible types (create new, copy, drop old)
ALTER TABLE events ADD COLUMN capacity_new BIGINT;
UPDATE events SET capacity_new = capacity::BIGINT;
ALTER TABLE events DROP COLUMN capacity;
ALTER TABLE events RENAME COLUMN capacity_new TO capacity;
```

### Pattern 3: Add Enum Type

```sql
-- up
CREATE TYPE event_status AS ENUM ('draft', 'published', 'cancelled');
ALTER TABLE events
ADD COLUMN status event_status DEFAULT 'draft';

-- down
ALTER TABLE events DROP COLUMN status;
DROP TYPE event_status;
```

### Pattern 4: Add JSON Column

```sql
-- up
ALTER TABLE events
ADD COLUMN metadata JSONB DEFAULT '{}';

CREATE INDEX idx_events_metadata ON events USING GIN(metadata);

-- down
DROP INDEX IF EXISTS idx_events_metadata;
ALTER TABLE events DROP COLUMN metadata;
```

---

## Troubleshooting

### Migration Failed Midway

```bash
# Check if database is dirty
./scripts/docker-migrate.sh version
# Output: 5 (dirty: true)

# Force to last good version
docker run --rm \
  --network event_ticketing_backend_default \
  --env-file .env \
  -v "$PWD/migrations:/migrations" \
  golang:1.24-alpine sh -c "
    apk add git && \
    go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest && \
    migrate -path /migrations -database \$DATABASE_URL force 5
  "

# Fix the migration SQL and re-run
./scripts/docker-migrate.sh up
```

### Check What Migrations Are Applied

```bash
docker exec -it event_ticketing_db psql -U postgres -d event_ticketing \
  -c "SELECT * FROM schema_migrations;"
```

---

## Best Practices Checklist

✅ Always create both `.up.sql` and `.down.sql`
✅ Test up and down migrations before committing
✅ Use `CREATE INDEX CONCURRENTLY` for large tables
✅ Add comments to document complex changes
✅ Keep migrations small and focused
✅ Never modify already-applied migrations
✅ Use transactions (PostgreSQL does this automatically)
✅ Consider data backfill in separate migrations
✅ Document breaking changes in migration comments

---

## Quick Reference

```bash
# Create new migration
./scripts/create-migration.sh "description"

# Apply all pending
./scripts/docker-migrate.sh up

# Rollback last migration
./scripts/docker-migrate.sh down

# Check version
./scripts/docker-migrate.sh version

# Apply specific number
./scripts/docker-migrate.sh up 2
```
