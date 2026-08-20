package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformAssetsExpansionMigrationIsForwardOnly(t *testing.T) {
	content, err := FS.ReadFile("194_platform_assets_expand.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "create table if not exists platforms")
	require.Contains(t, sql, "create table if not exists platform_model_rules")
	require.Contains(t, sql, "create table if not exists api_key_platforms")
	require.Contains(t, sql, "create table if not exists api_key_subscription_plans")
	require.Contains(t, sql, "account_platform varchar(50) not null")
	require.Contains(t, sql, "alter table accounts add column if not exists platform_id")
	require.Contains(t, sql, "alter table api_keys add column if not exists allow_balance")
	require.Contains(t, sql, "alter table subscription_plans alter column group_id drop not null")
	require.Contains(t, sql, "alter table user_subscriptions alter column group_id drop not null")
	require.Contains(t, sql, "alter table usage_logs add column if not exists platform_id")
	require.Contains(t, sql, "alter table usage_logs add column if not exists billing_source_type")
	require.Contains(t, sql, "global_balance_rate_multiplier")
	require.Contains(t, sql, "idx_accounts_platform_id_schedulable")
	require.NotContains(t, sql, "drop table")
	require.NotContains(t, sql, "delete from")
}
