-- ProductCore migration: make API Key subscription authorization a durable
-- business switch instead of a snapshot of individual plan IDs.
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS allow_all_subscriptions BOOLEAN NOT NULL DEFAULT FALSE;

-- Any active key that previously authorized at least one plan keeps that
-- ability through the new dynamic switch. Keys without a plan link remain
-- balance-only unless allow_balance is enabled.
UPDATE api_keys AS keys
SET allow_all_subscriptions = TRUE
FROM api_key_subscription_plans AS links
WHERE keys.id = links.api_key_id
  AND keys.deleted_at IS NULL;

-- The new column is the sole authority for new writes. Keep no stale
-- per-plan links that could make old and new readers disagree.
DELETE FROM api_key_subscription_plans;
