-- +migrate Up
DROP TABLE IF EXISTS transactions;

-- +migrate Down
CREATE TABLE IF NOT EXISTS transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id VARCHAR(100) NOT NULL UNIQUE,
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    guest_user_id UUID REFERENCES guest_users(id) ON DELETE SET NULL,
    ticket_ids UUID[] NOT NULL,
    payment_gateway VARCHAR(50) NOT NULL,
    amount DECIMAL(10,2) NOT NULL,
    currency VARCHAR(10) NOT NULL DEFAULT 'NPR',
    quantity INTEGER NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'completed',
    gateway_txn_id VARCHAR(255),
    gateway_data JSONB,
    commission_rate DECIMAL(5,2) NOT NULL,
    commission_amount DECIMAL(10,2) NOT NULL,
    organizer_share DECIMAL(10,2) NOT NULL,
    processed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP
);

-- Create indexes for better performance
CREATE INDEX IF NOT EXISTS idx_transactions_event_id ON transactions(event_id);
CREATE INDEX IF NOT EXISTS idx_transactions_user_id ON transactions(user_id);
CREATE INDEX IF NOT EXISTS idx_transactions_guest_user_id ON transactions(guest_user_id);
CREATE INDEX IF NOT EXISTS idx_transactions_payment_gateway ON transactions(payment_gateway);
CREATE INDEX IF NOT EXISTS idx_transactions_status ON transactions(status);
CREATE INDEX IF NOT EXISTS idx_transactions_created_at ON transactions(created_at);