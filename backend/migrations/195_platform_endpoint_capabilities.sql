ALTER TABLE platforms ADD COLUMN IF NOT EXISTS endpoint_capabilities JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE platforms p
SET endpoint_capabilities = COALESCE(
    (
        SELECT jsonb_agg(DISTINCT caps.capability ORDER BY caps.capability)
        FROM platform_model_rules r
        CROSS JOIN LATERAL jsonb_array_elements_text(r.endpoint_capabilities) AS caps(capability)
        WHERE r.platform_id = p.id
          AND r.status = 'active'
    ),
    '[]'::jsonb
)
WHERE p.endpoint_capabilities = '[]'::jsonb;
