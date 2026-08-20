//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPlatformRepositoryDeleteControlledRemovesOnlyTargetHistory(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	suffix := uuid.NewString()

	var targetID, controlID int64
	for _, item := range []struct {
		code string
		id   *int64
	}{
		{code: "del-target-" + suffix, id: &targetID},
		{code: "del-control-" + suffix, id: &controlID},
	} {
		require.LessOrEqual(t, len(item.code), 50, "platform fixture code must fit the database constraint")
		err := integrationDB.QueryRowContext(ctx, `
			INSERT INTO platforms (code, name, account_platform, status, endpoint_capabilities)
			VALUES ($1, $1, 'openai', 'disabled', '[]'::jsonb)
			RETURNING id`, item.code).Scan(item.id)
		require.NoError(t, err)
	}

	user := mustCreateUser(t, client, &service.User{Email: fmt.Sprintf("platform-delete-%s@example.com", suffix)})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-platform-delete-" + suffix})
	account := mustCreateAccount(t, client, &service.Account{Name: "platform-delete-" + suffix})

	var oldSetting sql.NullString
	err := integrationDB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'prompt_audit_config'`).Scan(&oldSetting.String)
	if err == nil {
		oldSetting.Valid = true
	} else {
		require.ErrorIs(t, err, sql.ErrNoRows)
	}

	t.Cleanup(func() {
		if oldSetting.Valid {
			_, _ = integrationDB.ExecContext(context.Background(), `
				INSERT INTO settings (key, value) VALUES ('prompt_audit_config', $1)
				ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, oldSetting.String)
		} else {
			_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM settings WHERE key = 'prompt_audit_config'`)
		}
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM usage_logs WHERE request_id IN ($1, $2)`, "target-"+suffix, "control-"+suffix)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM platforms WHERE id IN ($1, $2)`, targetID, controlID)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM api_keys WHERE id = $1`, apiKey.ID)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id = $1`, account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, user.ID)
	})

	for _, item := range []struct {
		requestID  string
		platformID int64
	}{
		{requestID: "target-" + suffix, platformID: targetID},
		{requestID: "control-" + suffix, platformID: controlID},
	} {
		_, err = integrationDB.ExecContext(ctx, `
			INSERT INTO usage_logs (
				user_id, api_key_id, account_id, request_id, model, platform_id,
				input_tokens, output_tokens, total_cost, actual_cost, created_at
			) VALUES ($1, $2, $3, $4, 'gpt-test', $5, 1, 1, 0.01, 0.01, $6)`,
			user.ID, apiKey.ID, account.ID, item.requestID, item.platformID, time.Now().UTC())
		require.NoError(t, err)
	}

	_, err = integrationDB.ExecContext(ctx, `
		INSERT INTO settings (key, value)
		VALUES ('prompt_audit_config', jsonb_build_object(
			'all_platforms', false,
			'platform_ids', jsonb_build_array($1::bigint, $2::bigint),
			'keep_field', 'unchanged'
		)::text)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, targetID, controlID)
	require.NoError(t, err)

	repo := newPlatformRepository(integrationDB)
	impact, err := repo.PreviewDelete(ctx, targetID)
	require.NoError(t, err)
	require.Equal(t, int64(1), impact.UsageLogs)
	require.Equal(t, int64(1), impact.Configs)
	require.True(t, impact.CanDelete)

	result, err := repo.DeleteControlled(ctx, targetID)
	require.NoError(t, err)
	require.Equal(t, targetID, result.PlatformID)
	require.Equal(t, int64(1), result.Cleaned.UsageLogs)

	var count int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM platforms WHERE id = $1`, targetID).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM platforms WHERE id = $1`, controlID).Scan(&count))
	require.Equal(t, int64(1), count)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE request_id = $1`, "target-"+suffix).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE request_id = $1`, "control-"+suffix).Scan(&count))
	require.Equal(t, int64(1), count)

	var targetPresent, controlPresent bool
	var keepField string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT
			COALESCE(value::jsonb->'platform_ids', '[]'::jsonb) @> jsonb_build_array($1::bigint),
			COALESCE(value::jsonb->'platform_ids', '[]'::jsonb) @> jsonb_build_array($2::bigint),
			value::jsonb->>'keep_field'
		FROM settings WHERE key = 'prompt_audit_config'`, targetID, controlID).Scan(&targetPresent, &controlPresent, &keepField))
	require.False(t, targetPresent)
	require.True(t, controlPresent)
	require.Equal(t, "unchanged", keepField)
}
