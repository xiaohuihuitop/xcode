-- Subscription redeem codes are issued from a concrete plan. Keep a complete
-- term snapshot so a later plan edit or deletion cannot change an unused code.

ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS subscription_plan_id BIGINT
    REFERENCES subscription_plans(id) ON DELETE SET NULL;
ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS plan_name_snapshot VARCHAR(100) NOT NULL DEFAULT '';
ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS daily_limit_usd_snapshot DECIMAL(20,8);
ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS weekly_limit_usd_snapshot DECIMAL(20,8);
ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS monthly_limit_usd_snapshot DECIMAL(20,8);
ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS rate_multiplier_snapshot DECIMAL(10,4) NOT NULL DEFAULT 1.0;

CREATE INDEX IF NOT EXISTS idx_redeem_codes_subscription_plan_id
    ON redeem_codes(subscription_plan_id);
