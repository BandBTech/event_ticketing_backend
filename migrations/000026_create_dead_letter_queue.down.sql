-- Rollback: Drop Dead Letter Queue table
DROP INDEX IF EXISTS idx_dlq_failed_at;
DROP INDEX IF EXISTS idx_dlq_task_type_status;
DROP INDEX IF EXISTS idx_dlq_request_id;
DROP INDEX IF EXISTS idx_dlq_stripe_event_id;
DROP INDEX IF EXISTS idx_dlq_status_pending;
DROP TABLE IF EXISTS dead_letter_queues;
