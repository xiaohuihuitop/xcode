-- Platform and user-asset expansion for the my2.0 billing model.
-- This migration is intentionally additive: legacy groups and their relations
-- remain available for the compatibility read path until manual cutover.

CREATE TABLE IF NOT EXISTS platforms (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(50) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    -- The adapter used by accounts in this pool, for example openai or gemini.
    -- It is distinct from code so GPT and GLM can have isolated OpenAI-compatible pools.
    account_platform VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    legacy_group_id BIGINT UNIQUE REFERENCES groups(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- A new platform owns its account pool. Existing account_groups are preserved
-- untouched for the legacy request path; administrators explicitly assign a
-- platform before enabling the V2 path.
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS platform_id BIGINT
    REFERENCES platforms(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS platform_model_rules (
    id BIGSERIAL PRIMARY KEY,
    platform_id BIGINT NOT NULL REFERENCES platforms(id) ON DELETE CASCADE,
    model_pattern VARCHAR(100) NOT NULL,
    upstream_model VARCHAR(100) NOT NULL DEFAULT '',
    endpoint_capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (platform_id, model_pattern)
);

CREATE TABLE IF NOT EXISTS api_key_platforms (
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    platform_id BIGINT NOT NULL REFERENCES platforms(id) ON DELETE CASCADE,
    PRIMARY KEY (api_key_id, platform_id)
);

CREATE TABLE IF NOT EXISTS api_key_subscription_plans (
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    subscription_plan_id BIGINT NOT NULL REFERENCES subscription_plans(id) ON DELETE CASCADE,
    PRIMARY KEY (api_key_id, subscription_plan_id)
);

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS allow_balance BOOLEAN NOT NULL DEFAULT TRUE;

-- New plans and subscription instances no longer require a legacy routing group.
ALTER TABLE subscription_plans ALTER COLUMN group_id DROP NOT NULL;
ALTER TABLE user_subscriptions ALTER COLUMN group_id DROP NOT NULL;

-- Historical logs keep group_id. New writes record the actual platform and asset
-- source independently, so reporting does not conflate platforms with plans.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS platform_id BIGINT
    REFERENCES platforms(id) ON DELETE SET NULL;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS billing_source_type VARCHAR(20);

CREATE INDEX IF NOT EXISTS idx_platform_model_rules_platform_id
    ON platform_model_rules(platform_id);
CREATE INDEX IF NOT EXISTS idx_accounts_platform_id_schedulable
    ON accounts(platform_id, platform, priority)
    WHERE deleted_at IS NULL AND status = 'active' AND schedulable = TRUE;
CREATE INDEX IF NOT EXISTS idx_platform_model_rules_status
    ON platform_model_rules(status);
CREATE INDEX IF NOT EXISTS idx_api_key_platforms_platform_id
    ON api_key_platforms(platform_id);
CREATE INDEX IF NOT EXISTS idx_api_key_subscription_plans_plan_id
    ON api_key_subscription_plans(subscription_plan_id);
CREATE INDEX IF NOT EXISTS idx_user_subscriptions_plan_candidates
    ON user_subscriptions(user_id, subscription_plan_id, status, expires_at, created_at)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_usage_logs_platform_created_at
    ON usage_logs(platform_id, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_logs_billing_source_created_at
    ON usage_logs(billing_source_type, created_at);

INSERT INTO settings (key, value, updated_at)
VALUES
    ('platform_assets_v2_enabled', 'false', NOW()),
    ('global_balance_rate_multiplier', '1.0', NOW())
ON CONFLICT (key) DO NOTHING;
