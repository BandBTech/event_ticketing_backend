-- +migrate Down
ALTER TABLE event_status_histories 
ALTER COLUMN changed_by SET NOT NULL;

ALTER TABLE event_status_histories
DROP CONSTRAINT IF EXISTS event_status_histories_changed_by_fkey;
