-- +migrate Up
-- Remove unique constraint from gateway_payment_id in payment_intents table
-- and gateway_refund_id in refunds table
-- Since these fields now just contain the gateway name (stripe, cash, etc.)

ALTER TABLE payment_intents DROP CONSTRAINT IF EXISTS payment_intents_gateway_payment_id_key;
DROP INDEX IF EXISTS payment_intents_gateway_payment_id_key;

ALTER TABLE refunds DROP CONSTRAINT IF EXISTS refunds_gateway_refund_id_key;
DROP INDEX IF EXISTS refunds_gateway_refund_id_key;