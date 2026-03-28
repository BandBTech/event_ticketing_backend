-- Increase CountryCode field size from VARCHAR(2) to VARCHAR(10) to support phone country codes with + prefix
ALTER TABLE payment_intents
ALTER COLUMN country_code TYPE character varying(10);
