-- Rollback script for race condition optimization indexes
-- Run this if indexes cause issues

-- Drop all race condition optimization indexes
DROP INDEX CONCURRENTLY IF EXISTS idx_event_tiers_event_tier_available;
DROP INDEX CONCURRENTLY IF EXISTS idx_tickets_event_checkin;
DROP INDEX CONCURRENTLY IF EXISTS idx_tickets_tier_status_created;
DROP INDEX CONCURRENTLY IF EXISTS idx_events_available_status;
DROP INDEX CONCURRENTLY IF EXISTS idx_transactions_unique_gateway;
DROP INDEX CONCURRENTLY IF EXISTS idx_checkout_sessions_expires;
DROP INDEX CONCURRENTLY IF EXISTS idx_event_tiers_lock_order;
DROP INDEX CONCURRENTLY IF EXISTS idx_tickets_active_user;
DROP INDEX CONCURRENTLY IF EXISTS idx_tickets_active_guest;
DROP INDEX CONCURRENTLY IF EXISTS idx_event_tiers_inventory_cover;
DROP INDEX CONCURRENTLY IF EXISTS idx_tickets_unique_number;
DROP INDEX CONCURRENTLY IF EXISTS idx_event_tiers_deleted;
DROP INDEX CONCURRENTLY IF EXISTS idx_tickets_deleted;
DROP INDEX CONCURRENTLY IF EXISTS idx_events_deleted;

-- Drop performance baseline table
DROP TABLE IF EXISTS performance_baseline;

-- Note: This will revert to default indexes only
-- System will still function but with reduced performance under high concurrency
