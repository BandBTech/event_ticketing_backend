-- +migrate Up
CREATE TABLE stripe_event_reconciliation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stripe_event_id VARCHAR(255) NOT NULL UNIQUE,
    event_type VARCHAR(100) NOT NULL,
    payment_intent_id VARCHAR(255),
    status VARCHAR(20) DEFAULT 'pending', -- pending, processed, reconciled, failed
    raw_event JSONB NOT NULL,
    processed_at TIMESTAMP,
    reconciled_at TIMESTAMP,
    error_message TEXT,
    retry_count INTEGER DEFAULT 0,
    max_retries INTEGER DEFAULT 3,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),

    INDEX idx_stripe_events_event_id (stripe_event_id),
    INDEX idx_stripe_events_status (status),
    INDEX idx_stripe_events_payment_intent (payment_intent_id),
    INDEX idx_stripe_events_created (created_at)
);

-- +migrate Down
DROP TABLE IF EXISTS stripe_event_reconciliation;