-- +migrate Down
ALTER TABLE payment_bills
    ALTER COLUMN bill_amount DROP DEFAULT;
