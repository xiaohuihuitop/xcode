package migrations

import (
	"strings"
	"testing"
)

func TestRemoveLegacyConfigurationMigration(t *testing.T) {
	content, err := FS.ReadFile("200_remove_legacy_configuration.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(content))
	compactSQL := strings.Join(strings.Fields(sql), " ")
	for _, fragment := range []string{
		"migrated legacy subscription asset",
		"select 1 from redeem_codes rc",
		"select 1 from payment_orders po",
		"plan_name_snapshot = case",
		"rate_multiplier_snapshot = case",
		"active subscription cannot be mapped",
		"unused subscription redeem code cannot be mapped",
		"paid subscription order cannot be mapped",
		"update usage_logs",
		"set group_id = -p.id",
		"target_table in ('ops_metrics_hourly', 'ops_metrics_daily')",
		"delete from %i where group_id >= 0",
		"rename column group_id to platform_id",
		"credentials - 'model_mapping' - 'model_whitelist' - 'openai_capabilities'",
		"drop column if exists legacy_group_id",
		"drop table if exists api_key_allowed_groups",
		"drop table if exists account_groups",
		"drop table if exists billing_profiles",
		"drop table if exists composite_model_routes",
		"drop table if exists channels",
		"drop table if exists groups",
		"'available_channels_enabled'",
		"'allow_ungrouped_key_scheduling'",
		"'{{group_name}}', '{{platform_name}}'",
		"'{{subscription_group}}', '{{subscription_plan}}'",
		"delete from ops_alert_rules",
		"'group_available_accounts'",
		"to_regclass('ops_alert_silences')",
		"trg_api_key_platforms_auth_cache_invalidation",
		"trg_api_key_subscription_plans_auth_cache_invalidation",
	} {
		if !strings.Contains(compactSQL, fragment) {
			t.Fatalf("migration missing %q", fragment)
		}
	}
	if strings.Contains(sql, "drop table groups cascade") {
		t.Fatal("migration must explicitly remove dependencies before dropping groups")
	}
}
