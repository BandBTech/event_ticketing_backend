-- Add indexes for performance optimization on frequently queried columns

-- Users table indexes
CREATE INDEX IF NOT EXISTS idx_users_account_status ON users(account_status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_organizer_status ON users(organizer_status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_email_lower ON users(LOWER(email));

-- Events table indexes
CREATE INDEX IF NOT EXISTS idx_events_status_start_date ON events(status, start_date) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_events_is_featured ON events(is_featured) WHERE deleted_at IS NULL AND status IN ('on_sale', 'completed', 'approved');
CREATE INDEX IF NOT EXISTS idx_events_organizer_status ON events(organizer_id, status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_events_is_cancelled ON events(is_cancelled) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_events_category ON events USING gin (category);

-- Transactions table indexes
CREATE INDEX IF NOT EXISTS idx_transactions_status ON transactions(status);
CREATE INDEX IF NOT EXISTS idx_transactions_event_id_status ON transactions(event_id, status);
CREATE INDEX IF NOT EXISTS idx_transactions_user_id_status ON transactions(user_id, status);
CREATE INDEX IF NOT EXISTS idx_transactions_created_at ON transactions(created_at DESC);

-- Tickets table indexes
CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets(status);
CREATE INDEX IF NOT EXISTS idx_tickets_event_id_status ON tickets(event_id, status);
CREATE INDEX IF NOT EXISTS idx_tickets_user_id ON tickets(user_id) WHERE user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tickets_guest_user_id ON tickets(guest_user_id) WHERE guest_user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tickets_qr_code ON tickets(qr_code);

-- Payment Bills table indexes
CREATE INDEX IF NOT EXISTS idx_payment_bills_status ON payment_bills(status);
CREATE INDEX IF NOT EXISTS idx_payment_bills_organizer_id_status ON payment_bills(organizer_id, status);

-- Event Tiers table indexes
CREATE INDEX IF NOT EXISTS idx_event_tiers_event_id ON event_tiers(event_id);
CREATE INDEX IF NOT EXISTS idx_event_tiers_sales_dates ON event_tiers(sales_start, sales_end);

-- OTP table indexes for faster lookups
CREATE INDEX IF NOT EXISTS idx_otps_email_type_verified ON otps(email, otp_type, is_verified, expires_at);

-- Organizer Onboarding indexes
CREATE INDEX IF NOT EXISTS idx_organizer_onboarding_organizer_id ON organizer_onboardings(organizer_id);

-- Categories indexes
CREATE INDEX IF NOT EXISTS idx_categories_is_active ON categories(is_active, sort_order);
