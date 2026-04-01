-- +migrate Down
ALTER TABLE event_status_histories DROP CONSTRAINT IF EXISTS event_status_histories_status_type_check;
ALTER TABLE event_status_histories ADD CONSTRAINT event_status_histories_status_type_check CHECK (status_type IN ('approval', 'sales'));