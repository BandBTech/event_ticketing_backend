-- +migrate Up
CREATE TABLE email_outboxes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type VARCHAR(50) NOT NULL, -- 'ticket_confirmation', 'payment_failed', etc.
    recipient_email VARCHAR(255) NOT NULL,
    subject TEXT NOT NULL,
    body_html TEXT,
    body_text TEXT,
    template_data JSONB, -- Template variables for email rendering
    priority INTEGER DEFAULT 1, -- 1=normal, 2=high, 3=critical
    status VARCHAR(20) DEFAULT 'pending', -- pending, processing, sent, failed
    max_retries INTEGER DEFAULT 3,
    retry_count INTEGER DEFAULT 0,
    last_attempt_at TIMESTAMP,
    next_attempt_at TIMESTAMP DEFAULT NOW(),
    error_message TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),

    -- Indexes for efficient processing
    INDEX idx_email_outbox_status_next_attempt (status, next_attempt_at),
    INDEX idx_email_outbox_event_type (event_type),
    INDEX idx_email_outbox_recipient (recipient_email)
);

-- +migrate Down
DROP TABLE IF EXISTS email_outboxes;