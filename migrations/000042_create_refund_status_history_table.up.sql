-- +migrate Up
CREATE TABLE refund_status_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    refund_id UUID NOT NULL REFERENCES refunds(id) ON DELETE CASCADE,
    old_status VARCHAR(50),
    new_status VARCHAR(50) NOT NULL,
    changed_by_id UUID REFERENCES users(id),
    changed_by_type VARCHAR(50) DEFAULT 'system',
    remarks TEXT,
    metadata JSONB,
    changed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Indexes for performance
CREATE INDEX idx_refund_status_history_refund_id ON refund_status_history(refund_id);
CREATE INDEX idx_refund_status_history_changed_by_id ON refund_status_history(changed_by_id);
CREATE INDEX idx_refund_status_history_changed_at ON refund_status_history(changed_at DESC);

-- +migrate Down
DROP TABLE IF EXISTS refund_status_history;