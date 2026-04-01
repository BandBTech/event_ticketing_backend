-- +migrate Up
ALTER TABLE event_status_histories 
ALTER COLUMN changed_by DROP NOT NULL;

ALTER TABLE event_status_histories
DROP CONSTRAINT IF EXISTS event_status_histories_changed_by_fkey;

ALTER TABLE event_status_histories
ADD CONSTRAINT event_status_histories_changed_by_fkey 
FOREIGN KEY (changed_by) REFERENCES users(id) ON DELETE SET NULL;
