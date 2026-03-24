-- Add reservation tracking and TTL cleanup support
-- This prevents inventory leakage from abandoned payments

-- Add reserved count to event_tiers table
ALTER TABLE event_tiers
ADD COLUMN reserved INT NOT NULL DEFAULT 0 COMMENT 'Number of tickets reserved but not yet confirmed via payment';

-- Set proper expiry time on PaymentIntent if null (15 minutes = trade standard)
UPDATE payment_intents 
SET expires_at = created_at + INTERVAL 15 MINUTE 
WHERE expires_at IS NULL AND status = 'pending';

-- Add index for cleanup job query (find expired pending payments)
CREATE INDEX idx_payment_intents_status_expires_at 
ON payment_intents(status, expires_at) 
WHERE status = 'pending';

-- Add index for finding pending reservations by event
CREATE INDEX idx_event_tiers_reserved 
ON event_tiers(id, reserved) 
WHERE reserved > 0;

-- Create audit table for reservation lifecycle if not exists
CREATE TABLE IF NOT EXISTS reservation_audits (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    payment_intent_id CHAR(36) NOT NULL,
    event_tier_id CHAR(36) NOT NULL,
    action VARCHAR(50) NOT NULL COMMENT 'reserved, confirmed, expired, released',
    quantity INT NOT NULL,
    reason VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    
    FOREIGN KEY (payment_intent_id) REFERENCES payment_intents(id) ON DELETE RESTRICT,
    FOREIGN KEY (event_tier_id) REFERENCES event_tiers(id) ON DELETE RESTRICT,
    INDEX idx_payment_intent_id (payment_intent_id),
    INDEX idx_action_created (action, created_at)
) COMMENT='Audit trail for ticket reservations and lifecycle events';

-- Add trigger to prevent over-reserving (safety check)
-- This ensures reserved + sold never exceeds quantity
DELIMITER $$
CREATE TRIGGER prevent_over_reservation 
BEFORE UPDATE ON event_tiers 
FOR EACH ROW
BEGIN
    IF (NEW.reserved + NEW.sold) > NEW.quantity THEN
        SIGNAL SQLSTATE '45000'
        SET MESSAGE_TEXT = 'Reservation would exceed tier quantity';
    END IF;
END$$
DELIMITER ;
