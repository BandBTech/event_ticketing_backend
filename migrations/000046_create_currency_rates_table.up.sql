-- +migrate Up
CREATE TABLE currency_rates (
    id SERIAL PRIMARY KEY,
    base_currency VARCHAR(3) NOT NULL,
    target_currency VARCHAR(3) NOT NULL,
    rate DECIMAL(20,10) NOT NULL,
    source VARCHAR(50) NOT NULL DEFAULT 'exchangerate.host',
    valid_from TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    valid_until TIMESTAMP WITH TIME ZONE,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    UNIQUE(base_currency, target_currency, valid_from)
);

-- Indexes for performance
CREATE INDEX idx_currency_rates_base_target ON currency_rates(base_currency, target_currency);
CREATE INDEX idx_currency_rates_active ON currency_rates(is_active, valid_from DESC);
CREATE INDEX idx_currency_rates_valid_from ON currency_rates(valid_from DESC);

-- +migrate Down
DROP TABLE IF EXISTS currency_rates;