package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionBillingRedesignMigrationPreservesTermsAndAllowsParallelSubscriptions(t *testing.T) {
	content, err := FS.ReadFile("192_subscription_billing_redesign.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "create table if not exists billing_profiles")
	require.Contains(t, sql, "group_id bigint not null unique references groups(id)")
	require.Contains(t, sql, "alter table subscription_plans add column if not exists daily_limit_usd")
	require.Contains(t, sql, "alter table subscription_plans add column if not exists rate_multiplier")
	require.Contains(t, sql, "alter table user_subscriptions add column if not exists subscription_plan_id")
	require.Contains(t, sql, "alter table user_subscriptions add column if not exists plan_name_snapshot")
	require.Contains(t, sql, "alter table user_subscriptions add column if not exists rate_multiplier_snapshot")
	require.Contains(t, sql, "drop index if exists user_subscriptions_user_group_unique_active")
	require.Contains(t, sql, "idx_user_subscriptions_active_candidates")
	require.Contains(t, sql, "insert into billing_profiles")
	require.Contains(t, sql, "insert into subscription_plans")
	require.Contains(t, sql, "select 1 from user_subscriptions us where us.group_id = g.id")
	require.Contains(t, sql, "update user_subscriptions")
}
