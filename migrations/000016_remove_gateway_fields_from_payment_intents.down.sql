-- +migrate Down
-- Restore GatewayPaymentID, GatewayConfigID, and GatewayClientSecret columns to payment_intents table

ALTER TABLE payment_intents ADD COLUMN gateway_payment_id VARCHAR(255);
ALTER TABLE payment_intents ADD COLUMN gateway_config_id UUID;
ALTER TABLE payment_intents ADD COLUMN gateway_client_secret VARCHAR(500);

-- Add index for gateway_payment_id
CREATE INDEX idx_payment_intents_gateway_payment_id ON payment_intents(gateway_payment_id);

-- Add index for gateway_config_id
CREATE INDEX idx_payment_intents_gateway_config_id ON payment_intents(gateway_config_id);