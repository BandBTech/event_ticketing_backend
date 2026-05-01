-- +migrate Up
ALTER TABLE events DROP COLUMN country;

-- +migrate Down
ALTER TABLE events ADD COLUMN country VARCHAR(100);