-- +migrate Down
ALTER TABLE event_status_histories DROP CONSTRAINT IF EXISTS chk_event_status_histories_status_type;
ALTER TABLE event_status_histories ADD CONSTRAINT chk_event_status_histories_status_type CHECK (status_type IN ('approval', 'sales'));