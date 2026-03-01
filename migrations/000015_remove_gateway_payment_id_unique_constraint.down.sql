-- +migrate Down
-- Restore unique constraints on gateway_payment_id and gateway_refund_id

CREATE UNIQUE INDEX payment_intents_gateway_payment_id_key ON payment_intents(gateway_payment_id);
CREATE UNIQUE INDEX refunds_gateway_refund_id_key ON refunds(gateway_refund_id);