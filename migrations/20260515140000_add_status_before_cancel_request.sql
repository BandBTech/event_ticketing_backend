-- Add status_before_cancel_request column to events table
-- This tracks the event status before a cancellation request was made,
-- allowing us to revert to it if the cancellation is rejected by admin

ALTER TABLE events
ADD COLUMN status_before_cancel_request VARCHAR(50);

-- Create an index for faster lookups if needed
CREATE INDEX idx_events_status_before_cancel_request 
ON events(status_before_cancel_request) 
WHERE status_before_cancel_request IS NOT NULL;
