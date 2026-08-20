package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformEndpointCapabilitiesMigration(t *testing.T) {
	content, err := FS.ReadFile("195_platform_endpoint_capabilities.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "ALTER TABLE platforms ADD COLUMN IF NOT EXISTS endpoint_capabilities JSONB")
	require.Contains(t, sql, "jsonb_array_elements_text(r.endpoint_capabilities)")
	require.Contains(t, sql, "r.status = 'active'")
	require.NotContains(t, strings.ToUpper(sql), "DROP COLUMN ENDPOINT_CAPABILITIES")
}
