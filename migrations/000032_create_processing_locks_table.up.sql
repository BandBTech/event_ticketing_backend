-- +migrate Up
CREATE TABLE processing_locks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lock_key VARCHAR(255) NOT NULL UNIQUE, -- e.g., "payment:stripe:pi_123456"
    lock_type VARCHAR(50) NOT NULL, -- payment, webhook, reconciliation
    owner_id VARCHAR(255) NOT NULL, -- worker ID or instance ID
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),

    INDEX idx_processing_locks_key (lock_key),
    INDEX idx_processing_locks_type_expires (lock_type, expires_at),
    INDEX idx_processing_locks_expires (expires_at)
);

-- +migrate Down
DROP TABLE IF EXISTS processing_locks;