package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration9000UnifiesAPIKeySubscriptionPermissions(t *testing.T) {
	content, err := FS.ReadFile("9000_api_key_allow_all_subscriptions.sql")
	require.NoError(t, err)

	sql := strings.ToLower(strings.Join(strings.Fields(string(content)), " "))
	for _, fragment := range []string{
		"alter table api_keys add column if not exists allow_all_subscriptions boolean not null default false",
		"update api_keys as keys",
		"from api_key_subscription_plans",
		"set allow_all_subscriptions = true",
		"where keys.id = links.api_key_id",
		"keys.deleted_at is null",
		"delete from api_key_subscription_plans",
	} {
		require.Contains(t, sql, fragment)
	}
}
