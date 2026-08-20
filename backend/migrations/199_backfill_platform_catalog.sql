-- Backfill the platform catalog from the last legacy group configuration.
-- This is intentionally one-way: request-time code reads platforms after this
-- migration, while the legacy group tables remain available only for the
-- subsequent physical cleanup migration.

INSERT INTO platforms (
    code,
    name,
    account_platform,
    status,
    endpoint_capabilities,
    legacy_group_id
)
SELECT
    'legacy-group-' || g.id::text,
    g.name,
    g.platform,
    CASE WHEN g.status = 'active' THEN 'active' ELSE 'disabled' END,
    CASE
        WHEN g.platform = 'openai' THEN '["chat_completions", "responses"]'::jsonb
        ELSE '["chat_completions"]'::jsonb
    END,
    g.id
FROM groups g
WHERE g.deleted_at IS NULL
  AND g.platform IN ('openai', 'anthropic', 'gemini', 'antigravity', 'grok')
  AND EXISTS (
      SELECT 1
      FROM account_groups ag
      JOIN accounts a ON a.id = ag.account_id
      WHERE ag.group_id = g.id
        AND a.deleted_at IS NULL
        AND a.platform_id IS NULL
  )
  AND NOT EXISTS (
      SELECT 1 FROM platforms p WHERE p.legacy_group_id = g.id
  )
ON CONFLICT (code) DO NOTHING;

-- Preserve the configured /v1/models list when present. An empty list means
-- the old group allowed all models, so the wildcard keeps that behavior until
-- an administrator narrows the platform rule explicitly.
INSERT INTO platform_model_rules (
    platform_id,
    model_pattern,
    upstream_model,
    endpoint_capabilities,
    status
)
SELECT
    p.id,
    models.model_pattern,
    '',
    p.endpoint_capabilities,
    'active'
FROM platforms p
JOIN groups g ON g.id = p.legacy_group_id
LEFT JOIN LATERAL (
    SELECT trim(value) AS model_pattern
    FROM jsonb_array_elements_text(COALESCE(g.models_list_config->'models', '[]'::jsonb))
    WHERE trim(value) <> ''
    UNION ALL
    SELECT '*'
    WHERE COALESCE(g.models_list_config->'models', '[]'::jsonb) = '[]'::jsonb
) models ON TRUE
WHERE g.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM platform_model_rules r
      WHERE r.platform_id = p.id
        AND r.model_pattern = models.model_pattern
  );

-- An account belongs to one platform pool. If historical data attached it to
-- several groups, the smallest group priority wins, then the group id breaks
-- ties deterministically.
WITH ranked_account_groups AS (
    SELECT
        ag.account_id,
        p.id AS platform_id,
        row_number() OVER (
            PARTITION BY ag.account_id
            ORDER BY ag.priority ASC, ag.group_id ASC
        ) AS rank
    FROM account_groups ag
    JOIN platforms p ON p.legacy_group_id = ag.group_id
    JOIN accounts a ON a.id = ag.account_id
    WHERE a.platform = p.account_platform
)
UPDATE accounts a
SET platform_id = r.platform_id
FROM ranked_account_groups r
WHERE r.rank = 1
  AND a.id = r.account_id
  AND a.platform_id IS NULL;
