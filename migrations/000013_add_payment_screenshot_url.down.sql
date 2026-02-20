-- +migrate Down
ALTER TABLE payment_bills
    DROP COLUMN IF EXISTS payment_screenshot_url;
