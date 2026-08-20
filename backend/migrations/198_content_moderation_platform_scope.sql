-- Content moderation is scoped by Platform, which is the account-pool authority.
-- This migration is intentionally separate from 196: 196 is already used by the
-- model-pricing override migration in this branch.

ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS platform_id BIGINT REFERENCES platforms(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS platform_name VARCHAR(255) NOT NULL DEFAULT '';

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'content_moderation_logs' AND column_name = 'group_id'
    ) AND EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'platforms' AND column_name = 'legacy_group_id'
    ) THEN
        UPDATE content_moderation_logs l
        SET platform_id = p.id,
            platform_name = p.name
        FROM platforms p
        WHERE l.platform_id IS NULL
          AND p.legacy_group_id = l.group_id;
    END IF;

    IF EXISTS (
        SELECT 1 FROM settings
        WHERE key = 'content_moderation_config'
          AND value::jsonb ? 'group_ids'
    ) THEN
        UPDATE settings s
        SET value = (
            (s.value::jsonb - 'all_groups' - 'group_ids')
            || jsonb_build_object(
                'all_platforms', COALESCE((s.value::jsonb->>'all_groups')::boolean, TRUE),
                'platform_ids', COALESCE((
                    SELECT jsonb_agg(p.id ORDER BY p.id)
                    FROM jsonb_array_elements_text(COALESCE(s.value::jsonb->'group_ids', '[]'::jsonb)) ids(value)
                    JOIN platforms p ON p.legacy_group_id = ids.value::bigint
                ), '[]'::jsonb)
            )
        )
        WHERE s.key = 'content_moderation_config';
    END IF;
END $$;

DROP INDEX IF EXISTS idx_content_moderation_logs_group_created_at;
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_platform_created_at
    ON content_moderation_logs(platform_id, created_at DESC);

ALTER TABLE content_moderation_logs
    DROP CONSTRAINT IF EXISTS content_moderation_logs_group_id_fkey,
    DROP COLUMN IF EXISTS group_id,
    DROP COLUMN IF EXISTS group_name;
