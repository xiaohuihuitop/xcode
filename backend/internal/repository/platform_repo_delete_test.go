//go:build unit

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newPlatformDeleteRepo(t *testing.T) (*platformRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return newPlatformRepository(db), mock
}

func platformReferenceRows(values ...int64) *sqlmock.Rows {
	columns := []string{"accounts", "api_keys", "usage_logs", "prompt_audit_jobs", "prompt_audit_events", "content_moderation_logs", "scheduler_outbox", "ops_error_logs", "ops_system_metrics", "ops_metrics_hourly", "ops_metrics_daily", "ops_alert_silences", "ops_alert_rules", "ops_alert_events", "content_moderation_config", "prompt_audit_config"}
	row := make([]driver.Value, len(values))
	for i := range values {
		row[i] = values[i]
	}
	return sqlmock.NewRows(columns).AddRow(row...)
}

func TestPlatformReferenceCountIncludesEverySettingThatCleanupWillModify(t *testing.T) {
	require.NotContains(t, platformReferenceCountSQLTemplate, "all_platforms")
	require.Contains(t, platformReferenceCountSQLTemplate, "platform_ids")
	require.NotContains(t, platformReferenceCountSQLTemplate, "jsonb_build_object('platform_id', $1)")
	require.NotContains(t, platformReferenceCountSQLTemplate, "jsonb_build_array($1)")
	require.NotContains(t, platformSettingsCleanupSQL, "jsonb_build_array($1)")
}

func expectPlatformImpactQuery(mock sqlmock.Sqlmock, id int64, hasSilences bool, values ...int64) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT to_regclass('public.ops_alert_silences') IS NOT NULL")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(hasSilences))
	pattern := "SELECT.*accounts.*prompt_audit_config"
	if !hasSilences {
		pattern = "SELECT.*accounts.*0::bigint AS ops_alert_silences.*prompt_audit_config"
	}
	mock.ExpectQuery(pattern).WithArgs(id).WillReturnRows(platformReferenceRows(values...))
}

func expectControlledDeletePrelude(mock sqlmock.Sqlmock, id int64, hasSilences bool, values ...int64) {
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM platforms WHERE id = $1 FOR UPDATE")).
		WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(id))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT to_regclass('public.ops_alert_silences') IS NOT NULL")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(hasSilences))
	if hasSilences {
		mock.ExpectExec("LOCK TABLE scheduler_outbox.*ops_alert_silences.*settings IN SHARE MODE").WillReturnResult(sqlmock.NewResult(0, 0))
	} else {
		mock.ExpectExec("LOCK TABLE scheduler_outbox.*ops_alert_events, settings IN SHARE MODE").WillReturnResult(sqlmock.NewResult(0, 0))
	}
	pattern := "SELECT.*accounts.*prompt_audit_config"
	if !hasSilences {
		pattern = "SELECT.*accounts.*0::bigint AS ops_alert_silences.*prompt_audit_config"
	}
	mock.ExpectQuery(pattern).WithArgs(id).WillReturnRows(platformReferenceRows(values...))
}

func TestPlatformRepositoryPreviewDeleteAggregatesImpact(t *testing.T) {
	repo, mock := newPlatformDeleteRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM platforms WHERE id = $1")).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(7)))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT to_regclass('public.ops_alert_silences') IS NOT NULL")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("SELECT.*ops_alert_silences.*rule_id IN.*ops_alert_events.*rule_id IN.*prompt_audit_config").WithArgs(int64(7)).
		WillReturnRows(platformReferenceRows(0, 0, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16))
	mock.ExpectCommit()

	impact, err := repo.PreviewDelete(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, service.PlatformDeleteImpact{UsageLogs: 3, Audits: 15, Ops: 84, Configs: 31, CanDelete: true}, *impact)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPlatformRepositoryDeleteControlledBlocksAccounts(t *testing.T) {
	repo, mock := newPlatformDeleteRepo(t)
	expectControlledDeletePrelude(mock, 7, true, 1, 0, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	mock.ExpectRollback()

	_, err := repo.DeleteControlled(context.Background(), 7)

	require.ErrorIs(t, err, service.ErrPlatformInUse)
	require.Equal(t, "1", infraerrors.FromError(err).Metadata["accounts"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPlatformRepositoryDeleteControlledBlocksAPIKeys(t *testing.T) {
	repo, mock := newPlatformDeleteRepo(t)
	expectControlledDeletePrelude(mock, 7, true, 0, 2, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	mock.ExpectRollback()

	_, err := repo.DeleteControlled(context.Background(), 7)

	require.ErrorIs(t, err, service.ErrPlatformInUse)
	require.Equal(t, "2", infraerrors.FromError(err).Metadata["api_keys"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func expectHistoricalCleanup(mock sqlmock.Sqlmock, id int64, hasSilences bool) {
	if hasSilences {
		mock.ExpectExec("DELETE FROM ops_alert_silences.*platform_id.*rule_id").WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 2))
	}
	for _, pattern := range []string{
		"DELETE FROM ops_alert_events.*rule_id.*dimensions",
		"DELETE FROM ops_alert_rules.*filters",
		"DELETE FROM usage_logs WHERE platform_id = \\$1",
		"DELETE FROM prompt_audit_events WHERE platform_id = \\$1",
		"DELETE FROM prompt_audit_jobs WHERE platform_id = \\$1",
		"DELETE FROM content_moderation_logs WHERE platform_id = \\$1",
		"DELETE FROM scheduler_outbox WHERE platform_id = \\$1",
		"DELETE FROM ops_error_logs WHERE platform_id = \\$1",
		"DELETE FROM ops_system_metrics WHERE platform_id = \\$1",
		"DELETE FROM ops_metrics_hourly WHERE platform_id = \\$1",
		"DELETE FROM ops_metrics_daily WHERE platform_id = \\$1",
	} {
		mock.ExpectExec(pattern).WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectExec("UPDATE settings.*jsonb_set.*platform_ids").WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM platform_model_rules WHERE platform_id = $1")).WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM platforms WHERE id = $1")).WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestPlatformRepositoryDeleteControlledClearsHistoricalReferences(t *testing.T) {
	repo, mock := newPlatformDeleteRepo(t)
	expectControlledDeletePrelude(mock, 7, true, 0, 0, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16)
	expectHistoricalCleanup(mock, 7, true)
	mock.ExpectCommit()

	result, err := repo.DeleteControlled(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, int64(7), result.PlatformID)
	require.Equal(t, service.PlatformDeleteImpact{UsageLogs: 3, Audits: 15, Ops: 84, Configs: 31, CanDelete: true}, result.Cleaned)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPlatformRepositoryDeleteControlledAllowsMissingOptionalOpsTable(t *testing.T) {
	repo, mock := newPlatformDeleteRepo(t)
	expectControlledDeletePrelude(mock, 7, false, make([]int64, 16)...)
	expectHistoricalCleanup(mock, 7, false)
	mock.ExpectCommit()

	_, err := repo.DeleteControlled(context.Background(), 7)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPlatformRepositoryDeleteControlledRollsBackOnCleanupFailure(t *testing.T) {
	repo, mock := newPlatformDeleteRepo(t)
	expectControlledDeletePrelude(mock, 7, false, make([]int64, 16)...)
	mock.ExpectExec("DELETE FROM ops_alert_events.*rule_id.*dimensions").WithArgs(int64(7)).WillReturnError(errors.New("delete failed"))
	mock.ExpectRollback()

	_, err := repo.DeleteControlled(context.Background(), 7)

	require.ErrorContains(t, err, "delete platform historical references")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPlatformRepositoryPreviewDeleteReturnsNotFound(t *testing.T) {
	repo, mock := newPlatformDeleteRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM platforms WHERE id = $1")).WithArgs(int64(7)).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err := repo.PreviewDelete(context.Background(), 7)

	require.ErrorIs(t, err, service.ErrPlatformNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}
