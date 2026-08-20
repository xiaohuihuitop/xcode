-- Prompt audit scope and snapshots follow the Platform request boundary.
-- Existing audit rows are retained, but their obsolete group dimensions are
-- removed because historical audit filtering must not depend on deleted groups.

ALTER TABLE prompt_audit_jobs
    ADD COLUMN IF NOT EXISTS platform_id BIGINT REFERENCES platforms(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS platform_name VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE prompt_audit_events
    ADD COLUMN IF NOT EXISTS platform_id BIGINT REFERENCES platforms(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS platform_name VARCHAR(255) NOT NULL DEFAULT '';

DROP INDEX IF EXISTS idx_prompt_audit_jobs_group_created;
DROP INDEX IF EXISTS idx_prompt_audit_events_group_created;

ALTER TABLE prompt_audit_jobs
    DROP COLUMN IF EXISTS group_id,
    DROP COLUMN IF EXISTS group_name;
ALTER TABLE prompt_audit_events
    DROP COLUMN IF EXISTS group_id,
    DROP COLUMN IF EXISTS group_name;

CREATE INDEX IF NOT EXISTS idx_prompt_audit_jobs_platform_created
    ON prompt_audit_jobs(platform_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_platform_created
    ON prompt_audit_events(platform_id, created_at DESC, id DESC);
