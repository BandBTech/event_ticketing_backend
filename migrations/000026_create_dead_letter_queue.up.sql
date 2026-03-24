-- Create Dead Letter Queue table for tracking failed payment processing tasks
-- This table stores tasks that failed after max retries for manual investigation and recovery

CREATE TABLE dead_letter_queues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_type VARCHAR(100) NOT NULL,
    payload JSONB,
    error TEXT,
    retries INT DEFAULT 0,
    max_retry INT DEFAULT 5,
    status VARCHAR(20) NOT NULL DEFAULT 'pending', -- pending, manual_review, resolved, discarded
    failed_at TIMESTAMP NOT NULL,
    resolved_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Debugging metadata
    stripe_event_id VARCHAR(255),
    request_id VARCHAR(255),
    metadata JSONB,
    
    -- Indexes for quick lookups
    CONSTRAINT chk_status CHECK (status IN ('pending', 'manual_review', 'resolved', 'discarded'))
);

-- Index for pending tasks (most common query)
CREATE INDEX idx_dlq_status_pending ON dead_letter_queues(created_at DESC) WHERE status = 'pending';

-- Index for Stripe event tracking and debugging
CREATE INDEX idx_dlq_stripe_event_id ON dead_letter_queues(stripe_event_id);

-- Index for request tracing
CREATE INDEX idx_dlq_request_id ON dead_letter_queues(request_id);

-- Index for finding failed tasks by type
CREATE INDEX idx_dlq_task_type_status ON dead_letter_queues(task_type, status);

-- Index for finding old pending tasks that might need cleanup
CREATE INDEX idx_dlq_failed_at ON dead_letter_queues(failed_at DESC) WHERE status = 'pending';
