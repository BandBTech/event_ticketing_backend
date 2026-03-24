-- +migrate Up
CREATE TABLE ticket_reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    checkout_token VARCHAR(255) NOT NULL UNIQUE,
    event_id UUID NOT NULL,
    tier_id UUID NOT NULL,
    user_id UUID,
    guest_user_id UUID,
    customer_email VARCHAR(255) NOT NULL,
    quantity INTEGER NOT NULL,
    status VARCHAR(20) DEFAULT 'reserved', -- reserved, confirmed, expired, cancelled
    expires_at TIMESTAMP NOT NULL,
    confirmed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),

    -- Foreign keys
    FOREIGN KEY (event_id) REFERENCES events(id) ON DELETE CASCADE,
    FOREIGN KEY (tier_id) REFERENCES event_tiers(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL,
    FOREIGN KEY (guest_user_id) REFERENCES guest_users(id) ON DELETE SET NULL,

    -- Indexes
    INDEX idx_ticket_reservations_checkout_token (checkout_token),
    INDEX idx_ticket_reservations_status_expires (status, expires_at),
    INDEX idx_ticket_reservations_event_tier (event_id, tier_id),
    INDEX idx_ticket_reservations_expires_at (expires_at)
);

-- +migrate Down
DROP TABLE IF EXISTS ticket_reservations;