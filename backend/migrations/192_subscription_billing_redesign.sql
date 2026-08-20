-- Split routing groups from customer billing. Legacy group pricing remains for
-- one release cycle as a rollback read path; new code writes the new fields.

CREATE TABLE IF NOT EXISTS billing_profiles (
    id BIGSERIAL PRIMARY KEY,
    group_id BIGINT NOT NULL UNIQUE REFERENCES groups(id) ON DELETE CASCADE,
    balance_rate_multiplier DECIMAL(10,4) NOT NULL DEFAULT 1.0,
    peak_rate_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    peak_start VARCHAR(5) NOT NULL DEFAULT '',
    peak_end VARCHAR(5) NOT NULL DEFAULT '',
    peak_rate_multiplier DECIMAL(10,4) NOT NULL DEFAULT 1.0,
    image_rate_independent BOOLEAN NOT NULL DEFAULT FALSE,
    image_rate_multiplier DECIMAL(10,4) NOT NULL DEFAULT 1.0,
    image_price_1k DECIMAL(20,8),
    image_price_2k DECIMAL(20,8),
    image_price_4k DECIMAL(20,8),
    batch_image_discount_multiplier DECIMAL(10,4) NOT NULL DEFAULT 0.5,
    batch_image_hold_multiplier DECIMAL(10,4) NOT NULL DEFAULT 0.6,
    video_rate_independent BOOLEAN NOT NULL DEFAULT FALSE,
    video_rate_multiplier DECIMAL(10,4) NOT NULL DEFAULT 1.0,
    video_price_480p DECIMAL(20,8),
    video_price_720p DECIMAL(20,8),
    video_price_1080p DECIMAL(20,8),
    web_search_price_per_call DECIMAL(20,8),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE subscription_plans ADD COLUMN IF NOT EXISTS daily_limit_usd DECIMAL(20,8);
ALTER TABLE subscription_plans ADD COLUMN IF NOT EXISTS weekly_limit_usd DECIMAL(20,8);
ALTER TABLE subscription_plans ADD COLUMN IF NOT EXISTS monthly_limit_usd DECIMAL(20,8);
ALTER TABLE subscription_plans ADD COLUMN IF NOT EXISTS rate_multiplier DECIMAL(10,4) NOT NULL DEFAULT 1.0;

ALTER TABLE user_subscriptions ADD COLUMN IF NOT EXISTS subscription_plan_id BIGINT REFERENCES subscription_plans(id) ON DELETE SET NULL;
ALTER TABLE user_subscriptions ADD COLUMN IF NOT EXISTS plan_name_snapshot VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE user_subscriptions ADD COLUMN IF NOT EXISTS daily_limit_usd_snapshot DECIMAL(20,8);
ALTER TABLE user_subscriptions ADD COLUMN IF NOT EXISTS weekly_limit_usd_snapshot DECIMAL(20,8);
ALTER TABLE user_subscriptions ADD COLUMN IF NOT EXISTS monthly_limit_usd_snapshot DECIMAL(20,8);
ALTER TABLE user_subscriptions ADD COLUMN IF NOT EXISTS rate_multiplier_snapshot DECIMAL(10,4) NOT NULL DEFAULT 1.0;

INSERT INTO billing_profiles (
    group_id, balance_rate_multiplier, peak_rate_enabled, peak_start, peak_end,
    peak_rate_multiplier, image_rate_independent, image_rate_multiplier,
    image_price_1k, image_price_2k, image_price_4k,
    batch_image_discount_multiplier, batch_image_hold_multiplier,
    video_rate_independent, video_rate_multiplier, video_price_480p,
    video_price_720p, video_price_1080p, web_search_price_per_call
)
SELECT
    g.id, g.rate_multiplier, g.peak_rate_enabled, g.peak_start, g.peak_end,
    g.peak_rate_multiplier, g.image_rate_independent, g.image_rate_multiplier,
    g.image_price_1k, g.image_price_2k, g.image_price_4k,
    g.batch_image_discount_multiplier, g.batch_image_hold_multiplier,
    g.video_rate_independent, g.video_rate_multiplier, g.video_price_480p,
    g.video_price_720p, g.video_price_1080p, g.web_search_price_per_call
FROM groups g
ON CONFLICT (group_id) DO NOTHING;

UPDATE subscription_plans sp
SET
    daily_limit_usd = g.daily_limit_usd,
    weekly_limit_usd = g.weekly_limit_usd,
    monthly_limit_usd = g.monthly_limit_usd,
    rate_multiplier = g.rate_multiplier,
    updated_at = NOW()
FROM groups g
WHERE sp.group_id = g.id;

-- Old user subscriptions need a durable plan relationship even when a group
-- never exposed a sale plan. These migration-only plans are never for sale.
INSERT INTO subscription_plans (
    group_id, name, description, price, original_price, currency,
    validity_days, validity_unit, features, product_name, for_sale, sort_order,
    daily_limit_usd, weekly_limit_usd, monthly_limit_usd, rate_multiplier,
    created_at, updated_at
)
SELECT
    g.id, g.name || ' Legacy Plan', 'Migrated legacy subscription terms',
    0, NULL, '', GREATEST(g.default_validity_days, 1), 'day', '', '', FALSE, 0,
    g.daily_limit_usd, g.weekly_limit_usd, g.monthly_limit_usd,
    g.rate_multiplier, NOW(), NOW()
FROM groups g
WHERE (
      g.subscription_type = 'subscription'
      OR EXISTS (
          SELECT 1 FROM user_subscriptions us WHERE us.group_id = g.id
      )
  )
  AND NOT EXISTS (
      SELECT 1 FROM subscription_plans sp WHERE sp.group_id = g.id
  );

-- Historical records retain a full term snapshot. The deterministic plan choice
-- only supplies provenance; later edits or deletion cannot change the snapshot.
WITH selected_plans AS (
    SELECT
        us.id AS subscription_id,
        (
            SELECT sp.id
            FROM subscription_plans sp
            WHERE sp.group_id = us.group_id
            ORDER BY sp.sort_order ASC, sp.id ASC
            LIMIT 1
        ) AS subscription_plan_id
    FROM user_subscriptions us
)
UPDATE user_subscriptions us
SET
    subscription_plan_id = selected_plans.subscription_plan_id,
    plan_name_snapshot = sp.name,
    daily_limit_usd_snapshot = sp.daily_limit_usd,
    weekly_limit_usd_snapshot = sp.weekly_limit_usd,
    monthly_limit_usd_snapshot = sp.monthly_limit_usd,
    rate_multiplier_snapshot = sp.rate_multiplier,
    updated_at = NOW()
FROM selected_plans
JOIN subscription_plans sp ON sp.id = selected_plans.subscription_plan_id
WHERE us.id = selected_plans.subscription_id
  AND us.subscription_plan_id IS NULL;

DROP INDEX IF EXISTS user_subscriptions_user_group_unique_active;
CREATE INDEX IF NOT EXISTS idx_user_subscriptions_active_candidates
    ON user_subscriptions(user_id, group_id, status, expires_at, created_at)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_subscriptions_subscription_plan_id
    ON user_subscriptions(subscription_plan_id);
