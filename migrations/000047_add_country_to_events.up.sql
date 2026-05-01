-- +migrate Up
ALTER TABLE events ADD COLUMN country VARCHAR(100);

-- +migrate Down
ALTER TABLE events DROP COLUMN country;