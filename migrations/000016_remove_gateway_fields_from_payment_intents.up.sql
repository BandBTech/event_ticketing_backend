-- +migrate Up
-- Remove GatewayPaymentID, GatewayConfigID, and GatewayClientSecret columns from payment_intents table
-- These fields are no longer needed as per simplification requirements

ALTER TABLE payment_intents DROP COLUMN IF EXISTS gateway_payment_id;
ALTER TABLE payment_intents DROP COLUMN IF EXISTS gateway_config_id;
ALTER TABLE payment_intents DROP COLUMN IF EXISTS gateway_client_secret;