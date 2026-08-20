-- Final one-way removal of Group/Channel configuration.
-- Platform owns routing and account pools; SubscriptionPlan and balance own billing.

SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '15min';

-- Preserve plan-backed assets before removing their legacy group provenance.
-- Administrators may have deleted a sale plan after issuing a subscription
-- asset. Materialize a non-sale plan for those assets before the legacy Group
-- disappears so their original duration, limits, and multiplier remain usable.
INSERT INTO subscription_plans (
    group_id, name, description, price, original_price, currency,
    validity_days, validity_unit, features, product_name, for_sale, sort_order,
    daily_limit_usd, weekly_limit_usd, monthly_limit_usd, rate_multiplier,
    created_at, updated_at
)
SELECT
    g.id,
    g.name || ' Legacy Plan',
    'Migrated legacy subscription asset',
    0,
    NULL,
    '',
    GREATEST(
        g.default_validity_days,
        COALESCE((
            SELECT MAX(rc.validity_days)
            FROM redeem_codes rc
            WHERE rc.group_id = g.id
              AND rc.type = 'subscription'
              AND rc.status = 'unused'
              AND rc.subscription_plan_id IS NULL
        ), 0),
        1
    ),
    'day',
    '',
    '',
    FALSE,
    0,
    g.daily_limit_usd,
    g.weekly_limit_usd,
    g.monthly_limit_usd,
    g.rate_multiplier,
    NOW(),
    NOW()
FROM groups g
WHERE NOT EXISTS (
        SELECT 1 FROM subscription_plans sp WHERE sp.group_id = g.id
    )
  AND (
      EXISTS (
          SELECT 1 FROM user_subscriptions us
          WHERE us.group_id = g.id
            AND us.deleted_at IS NULL
            AND us.status = 'active'
            AND us.expires_at > NOW()
            AND us.subscription_plan_id IS NULL
      )
      OR EXISTS (
          SELECT 1 FROM redeem_codes rc
          WHERE rc.group_id = g.id
            AND rc.type = 'subscription'
            AND rc.status = 'unused'
            AND rc.subscription_plan_id IS NULL
      )
      OR EXISTS (
          SELECT 1 FROM payment_orders po
          WHERE po.subscription_group_id = g.id
            AND po.order_type = 'subscription'
            AND po.status = 'paid'
            AND po.plan_id IS NULL
      )
  );

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'user_subscriptions' AND column_name = 'group_id') THEN
        UPDATE user_subscriptions us
        SET subscription_plan_id = (
            SELECT sp.id
            FROM subscription_plans sp
            WHERE sp.group_id = us.group_id
            ORDER BY sp.for_sale DESC, sp.sort_order ASC, sp.id ASC
            LIMIT 1
        )
        WHERE us.subscription_plan_id IS NULL AND us.group_id IS NOT NULL;
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'redeem_codes' AND column_name = 'group_id') THEN
        UPDATE redeem_codes rc
        SET subscription_plan_id = (
            SELECT sp.id
            FROM subscription_plans sp
            WHERE sp.group_id = rc.group_id
            ORDER BY sp.for_sale DESC, sp.sort_order ASC, sp.id ASC
            LIMIT 1
        )
        WHERE rc.type = 'subscription' AND rc.subscription_plan_id IS NULL AND rc.group_id IS NOT NULL;

        UPDATE redeem_codes rc
        SET
            plan_name_snapshot = CASE
                WHEN BTRIM(rc.plan_name_snapshot) = '' THEN sp.name
                ELSE rc.plan_name_snapshot
            END,
            validity_days = CASE
                WHEN rc.validity_days > 0 THEN rc.validity_days
                ELSE GREATEST(
                    CASE sp.validity_unit
                        WHEN 'week' THEN sp.validity_days * 7
                        WHEN 'weeks' THEN sp.validity_days * 7
                        WHEN 'month' THEN sp.validity_days * 30
                        WHEN 'months' THEN sp.validity_days * 30
                        WHEN 'year' THEN sp.validity_days * 365
                        WHEN 'years' THEN sp.validity_days * 365
                        ELSE sp.validity_days
                    END,
                    1
                )
            END,
            daily_limit_usd_snapshot = COALESCE(rc.daily_limit_usd_snapshot, sp.daily_limit_usd),
            weekly_limit_usd_snapshot = COALESCE(rc.weekly_limit_usd_snapshot, sp.weekly_limit_usd),
            monthly_limit_usd_snapshot = COALESCE(rc.monthly_limit_usd_snapshot, sp.monthly_limit_usd),
            rate_multiplier_snapshot = CASE
                WHEN BTRIM(rc.plan_name_snapshot) = '' THEN sp.rate_multiplier
                ELSE rc.rate_multiplier_snapshot
            END
        FROM subscription_plans sp
        WHERE rc.type = 'subscription'
          AND rc.subscription_plan_id = sp.id;
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'payment_orders' AND column_name = 'subscription_group_id') THEN
        UPDATE payment_orders po
        SET plan_id = (
            SELECT sp.id
            FROM subscription_plans sp
            WHERE sp.group_id = po.subscription_group_id
            ORDER BY sp.for_sale DESC, sp.sort_order ASC, sp.id ASC
            LIMIT 1
        )
        WHERE po.plan_id IS NULL AND po.subscription_group_id IS NOT NULL;
    END IF;

    IF EXISTS (
        SELECT 1 FROM user_subscriptions
        WHERE deleted_at IS NULL AND status = 'active' AND expires_at > NOW()
          AND subscription_plan_id IS NULL
    ) THEN
        RAISE EXCEPTION 'active subscription cannot be mapped to a subscription plan';
    END IF;
    IF EXISTS (
        SELECT 1 FROM redeem_codes
        WHERE type = 'subscription' AND status = 'unused' AND subscription_plan_id IS NULL
    ) THEN
        RAISE EXCEPTION 'unused subscription redeem code cannot be mapped to a subscription plan';
    END IF;
    IF EXISTS (
        SELECT 1 FROM payment_orders
        WHERE order_type = 'subscription' AND status = 'paid' AND plan_id IS NULL
    ) THEN
        RAISE EXCEPTION 'paid subscription order cannot be mapped to a subscription plan';
    END IF;
END $$;

-- Backfill the durable Platform dimension while the legacy mapping still exists.
DO $$
DECLARE
    target_table TEXT;
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'platforms' AND column_name = 'legacy_group_id') THEN
        IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'usage_logs' AND column_name = 'group_id') THEN
            UPDATE usage_logs ul
            SET platform_id = p.id
            FROM platforms p
            WHERE ul.platform_id IS NULL AND p.legacy_group_id = ul.group_id;
        END IF;
    END IF;

    FOREACH target_table IN ARRAY ARRAY[
            'scheduler_outbox',
            'ops_error_logs',
            'ops_system_metrics',
            'ops_metrics_hourly',
            'ops_metrics_daily',
            'ops_alert_silences'
        ] LOOP
        IF EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE information_schema.columns.table_name = target_table AND column_name = 'group_id'
        ) THEN
            IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'platforms' AND column_name = 'legacy_group_id') THEN
                EXECUTE format(
                    'UPDATE %I row SET group_id = -p.id FROM platforms p WHERE row.group_id = p.legacy_group_id',
                    target_table
                );
            END IF;

            -- Aggregate rows are derived from raw operational events. An old
            -- billing-only Group has no Platform meaning, so discard only that
            -- obsolete aggregate dimension. Raw operational rows retain their
            -- event data and receive a NULL Platform instead.
            IF target_table IN ('ops_metrics_hourly', 'ops_metrics_daily') THEN
                EXECUTE format('DELETE FROM %I WHERE group_id >= 0', target_table);
            ELSE
                EXECUTE format('UPDATE %I SET group_id = NULL WHERE group_id >= 0', target_table);
            END IF;

            -- Negative IDs avoid transient unique-key collisions when legacy
            -- Group IDs and destination Platform IDs overlap.
            EXECUTE format('UPDATE %I SET group_id = -group_id WHERE group_id < 0', target_table);
            EXECUTE format('ALTER TABLE %I RENAME COLUMN group_id TO platform_id', target_table);
        END IF;
    END LOOP;
END $$;

-- The old account-level model configuration is not a supported compatibility path.
UPDATE accounts
SET credentials = credentials - 'model_mapping' - 'model_whitelist' - 'openai_capabilities'
WHERE credentials ?| ARRAY['model_mapping', 'model_whitelist', 'openai_capabilities'];
UPDATE accounts SET schedulable = FALSE WHERE platform_id IS NULL;

DELETE FROM settings
WHERE key IN (
    'platform_assets_v2_enabled',
    'default_standard_group_id',
    'default_subscription_group_id',
    'available_channels_enabled',
    'allow_ungrouped_key_scheduling'
);

-- Rename template placeholders while this one-way migration removes the old
-- Group vocabulary. Custom templates keep their content and receive the new
-- Platform/SubscriptionPlan variables.
UPDATE settings
SET value = replace(
    replace(value, '{{group_name}}', '{{platform_name}}'),
    '{{subscription_group}}', '{{subscription_plan}}'
)
WHERE key LIKE 'notification_email_template:%';

DELETE FROM ops_alert_rules
WHERE metric_type IN (
    'group_available_accounts',
    'group_available_ratio',
    'group_rate_limit_ratio'
);

-- Replace auth-cache triggers before removing the columns and tables referenced
-- by the legacy trigger functions.
DROP TRIGGER IF EXISTS trg_groups_auth_cache_invalidation ON groups;
DROP TRIGGER IF EXISTS trg_user_allowed_groups_auth_cache_invalidation ON user_allowed_groups;
DROP TRIGGER IF EXISTS trg_api_keys_auth_cache_invalidation ON api_keys;
DROP FUNCTION IF EXISTS enqueue_group_auth_cache_invalidation();
DROP FUNCTION IF EXISTS enqueue_allowed_group_auth_cache_invalidation();

CREATE OR REPLACE FUNCTION enqueue_api_key_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM enqueue_auth_cache_invalidation(OLD.key);
        RETURN OLD;
    END IF;
    IF OLD.key IS DISTINCT FROM NEW.key
       OR OLD.status IS DISTINCT FROM NEW.status
       OR OLD.deleted_at IS DISTINCT FROM NEW.deleted_at
       OR OLD.user_id IS DISTINCT FROM NEW.user_id
       OR OLD.allow_balance IS DISTINCT FROM NEW.allow_balance
       OR OLD.ip_whitelist IS DISTINCT FROM NEW.ip_whitelist
       OR OLD.ip_blacklist IS DISTINCT FROM NEW.ip_blacklist
       OR OLD.expires_at IS DISTINCT FROM NEW.expires_at THEN
        PERFORM enqueue_auth_cache_invalidation(OLD.key);
        IF NEW.deleted_at IS NULL AND NEW.key IS DISTINCT FROM OLD.key THEN
            PERFORM enqueue_auth_cache_invalidation(NEW.key);
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_api_keys_auth_cache_invalidation
AFTER UPDATE OR DELETE ON api_keys
FOR EACH ROW EXECUTE FUNCTION enqueue_api_key_auth_cache_invalidation();

CREATE OR REPLACE FUNCTION enqueue_api_key_asset_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_api_key_id BIGINT;
BEGIN
    target_api_key_id := CASE WHEN TG_OP = 'DELETE' THEN OLD.api_key_id ELSE NEW.api_key_id END;
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
    FROM api_keys k
    WHERE k.id = target_api_key_id AND k.deleted_at IS NULL AND k.key <> '';
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_api_key_platforms_auth_cache_invalidation ON api_key_platforms;
CREATE TRIGGER trg_api_key_platforms_auth_cache_invalidation
AFTER INSERT OR UPDATE OR DELETE ON api_key_platforms
FOR EACH ROW EXECUTE FUNCTION enqueue_api_key_asset_auth_cache_invalidation();

DROP TRIGGER IF EXISTS trg_api_key_subscription_plans_auth_cache_invalidation ON api_key_subscription_plans;
CREATE TRIGGER trg_api_key_subscription_plans_auth_cache_invalidation
AFTER INSERT OR UPDATE OR DELETE ON api_key_subscription_plans
FOR EACH ROW EXECUTE FUNCTION enqueue_api_key_asset_auth_cache_invalidation();

-- Remove indexes whose names encode the deleted dimension.
DROP INDEX IF EXISTS idx_api_keys_group_id;
DROP INDEX IF EXISTS idx_subscription_plans_group_id;
DROP INDEX IF EXISTS idx_redeem_codes_group_id;
DROP INDEX IF EXISTS idx_usage_logs_group_id;
DROP INDEX IF EXISTS idx_usage_logs_channel_id;
DROP INDEX IF EXISTS idx_scheduler_outbox_group_id;
DROP INDEX IF EXISTS idx_ops_error_logs_group_created;
DROP INDEX IF EXISTS idx_ops_system_metrics_group_created;
DROP INDEX IF EXISTS idx_ops_metrics_hourly_group_bucket;
DROP INDEX IF EXISTS idx_ops_metrics_daily_group_bucket;
DROP INDEX IF EXISTS idx_ops_alert_silences_lookup;
DROP INDEX IF EXISTS user_subscriptions_user_group_unique_active;
DROP INDEX IF EXISTS idx_user_subscriptions_active_candidates;

-- Remove legacy columns after every active read/write path has moved to
-- Platform or SubscriptionPlan.
ALTER TABLE api_keys DROP COLUMN IF EXISTS group_id;
ALTER TABLE platforms DROP COLUMN IF EXISTS legacy_group_id;
ALTER TABLE subscription_plans DROP COLUMN IF EXISTS group_id;
ALTER TABLE user_subscriptions DROP COLUMN IF EXISTS group_id;
ALTER TABLE redeem_codes DROP COLUMN IF EXISTS group_id;
ALTER TABLE payment_orders DROP COLUMN IF EXISTS subscription_group_id;
ALTER TABLE usage_logs DROP COLUMN IF EXISTS group_id;
ALTER TABLE usage_logs DROP COLUMN IF EXISTS channel_id;
ALTER TABLE users DROP COLUMN IF EXISTS allowed_groups;

-- Recreate renamed operational indexes with Platform terminology.
DO $$
BEGIN
    IF to_regclass('ops_error_logs') IS NOT NULL THEN
        CREATE INDEX IF NOT EXISTS idx_ops_error_logs_platform_created
            ON ops_error_logs(platform_id, created_at DESC) WHERE platform_id IS NOT NULL;
    END IF;
    IF to_regclass('ops_system_metrics') IS NOT NULL THEN
        CREATE INDEX IF NOT EXISTS idx_ops_system_metrics_platform_created
            ON ops_system_metrics(platform_id, created_at DESC) WHERE platform_id IS NOT NULL;
    END IF;
    IF to_regclass('ops_alert_silences') IS NOT NULL THEN
        CREATE INDEX IF NOT EXISTS idx_ops_alert_silences_lookup
            ON ops_alert_silences(rule_id, platform, platform_id, region, until);
    END IF;
END $$;

-- Drop dependent configuration tables from leaves to roots.
DROP TABLE IF EXISTS api_key_allowed_groups;
DROP TABLE IF EXISTS account_groups;
DROP TABLE IF EXISTS user_allowed_groups;
DROP TABLE IF EXISTS user_group_rate_multipliers;
DROP TABLE IF EXISTS billing_profiles;
DROP TABLE IF EXISTS composite_model_routes;
DROP TABLE IF EXISTS channel_groups;
DROP TABLE IF EXISTS channel_pricing_intervals;
DROP TABLE IF EXISTS channel_model_pricing;
DROP TABLE IF EXISTS channel_account_stats_pricing_intervals;
DROP TABLE IF EXISTS channel_account_stats_model_pricing;
DROP TABLE IF EXISTS channel_account_stats_pricing_rules;
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS groups;
