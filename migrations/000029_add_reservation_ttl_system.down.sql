-- Rollback reservation TTL system

DROP TRIGGER IF EXISTS prevent_over_reservation;

DROP TABLE IF EXISTS reservation_audits;

DROP INDEX idx_event_tiers_reserved ON event_tiers;
DROP INDEX idx_payment_intents_status_expires_at ON payment_intents;

ALTER TABLE event_tiers DROP COLUMN reserved;
