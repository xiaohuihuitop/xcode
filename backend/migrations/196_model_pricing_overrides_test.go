package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelPricingOverridesMigrationIsIndependent(t *testing.T) {
	sql, err := FS.ReadFile("196_model_pricing_overrides.sql")
	require.NoError(t, err)
	content := string(sql)
	require.Contains(t, content, "CREATE TABLE IF NOT EXISTS model_pricing_overrides")
	require.Contains(t, content, "LOWER(adapter), model_pattern")
	require.Contains(t, content, "intervals JSONB")
	require.NotContains(t, strings.ToLower(content), "group_id")
	require.NotContains(t, strings.ToLower(content), "channel_id")
}
