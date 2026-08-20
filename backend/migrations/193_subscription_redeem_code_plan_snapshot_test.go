package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionRedeemCodePlanSnapshotMigrationPreservesTerms(t *testing.T) {
	content, err := FS.ReadFile("193_subscription_redeem_code_plan_snapshot.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "subscription_plan_id bigint")
	require.Contains(t, sql, "on delete set null")
	require.Contains(t, sql, "plan_name_snapshot")
	require.Contains(t, sql, "daily_limit_usd_snapshot")
	require.Contains(t, sql, "rate_multiplier_snapshot")
	require.Contains(t, sql, "idx_redeem_codes_subscription_plan_id")
}
