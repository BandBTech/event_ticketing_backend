-- +migrate Up
-- Create ticket_sequences table for atomic ticket number generation
-- This allows concurrent purchases while ensuring unique sequential ticket numbers

CREATE TABLE ticket_sequences (
    event_id UUID PRIMARY KEY,
    next_sequence BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Add comment
COMMENT ON TABLE ticket_sequences IS 'Tracks the next sequence number for ticket generation per event. Allows concurrent purchases with atomic increments.';

-- Add index for performance (though event_id is already primary key)
CREATE INDEX idx_ticket_sequences_updated_at ON ticket_sequences(updated_at);