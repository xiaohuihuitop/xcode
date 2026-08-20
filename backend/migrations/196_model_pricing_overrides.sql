-- Independent adapter/model price catalog. It is intentionally unrelated to
-- Groups and Channels so it can survive the legacy configuration cleanup.
CREATE TABLE IF NOT EXISTS model_pricing_overrides (
    id BIGSERIAL PRIMARY KEY,
    adapter VARCHAR(50) NOT NULL,
    model_pattern VARCHAR(100) NOT NULL,
    billing_mode VARCHAR(20) NOT NULL DEFAULT 'token',
    input_price DOUBLE PRECISION,
    output_price DOUBLE PRECISION,
    cache_write_price DOUBLE PRECISION,
    cache_read_price DOUBLE PRECISION,
    image_input_price DOUBLE PRECISION,
    image_output_price DOUBLE PRECISION,
    per_request_price DOUBLE PRECISION,
    intervals JSONB NOT NULL DEFAULT '[]'::jsonb,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT model_pricing_overrides_billing_mode_check
        CHECK (billing_mode IN ('token', 'per_request', 'image')),
    CONSTRAINT model_pricing_overrides_status_check
        CHECK (status IN ('active', 'disabled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS model_pricing_overrides_adapter_pattern_key
    ON model_pricing_overrides (LOWER(adapter), model_pattern);
CREATE INDEX IF NOT EXISTS model_pricing_overrides_adapter_status_idx
    ON model_pricing_overrides (LOWER(adapter), status);
