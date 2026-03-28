-- Rollback: Revert CountryCode field size to VARCHAR(2)
ALTER TABLE payment_intents
ALTER COLUMN country_code TYPE character varying(2);
