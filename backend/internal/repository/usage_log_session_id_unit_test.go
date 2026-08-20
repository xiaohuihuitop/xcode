//go:build unit

package repository

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newSessionIDUsageLog(sessionID *string) *service.UsageLog {
	return &service.UsageLog{
		UserID:       1,
		APIKeyID:     2,
		AccountID:    3,
		RequestID:    "req-session-id",
		Model:        "claude-3",
		InputTokens:  10,
		OutputTokens: 5,
		TotalCost:    1.0,
		ActualCost:   1.0,
		SessionID:    sessionID,
		CreatedAt:    time.Now().UTC(),
	}
}

// TestPrepareUsageLogInsert_SessionIDArgWiring pins the session_id column to the
// arg slice / arg-type table so the five INSERT column lists stay in sync. session_id
// is the penultimate arg (created_at is always last).
func TestPrepareUsageLogInsert_SessionIDArgWiring(t *testing.T) {
	require.Len(t, usageLogInsertArgTypes, 57, "arg-type table must include platform attribution and session_id")

	sessionID := "sess-persisted-123"
	prepared := prepareUsageLogInsert(newSessionIDUsageLog(&sessionID))

	require.Len(t, prepared.args, len(usageLogInsertArgTypes),
		"prepared args must match the arg-type table length")

	// created_at is last; session_id is the arg immediately before it.
	sessionArg := prepared.args[len(prepared.args)-2]
	ns, ok := sessionArg.(sql.NullString)
	require.True(t, ok, "session_id arg should be a sql.NullString, got %T", sessionArg)
	require.True(t, ns.Valid)
	require.Equal(t, sessionID, ns.String)

	require.Equal(t, "text", usageLogInsertArgTypes[len(usageLogInsertArgTypes)-2],
		"session_id arg type must be text")
}

// TestPrepareUsageLogInsert_SessionIDNullWhenAbsent proves an absent session id is
// persisted as SQL NULL rather than an empty string.
func TestPrepareUsageLogInsert_SessionIDNullWhenAbsent(t *testing.T) {
	prepared := prepareUsageLogInsert(newSessionIDUsageLog(nil))
	sessionArg := prepared.args[len(prepared.args)-2]
	ns, ok := sessionArg.(sql.NullString)
	require.True(t, ok, "session_id arg should be a sql.NullString, got %T", sessionArg)
	require.False(t, ns.Valid, "absent session id must be NULL, not empty string")

	empty := ""
	preparedEmpty := prepareUsageLogInsert(newSessionIDUsageLog(&empty))
	nsEmpty, ok := preparedEmpty.args[len(preparedEmpty.args)-2].(sql.NullString)
	require.True(t, ok, "empty session_id arg should be a sql.NullString")
	require.False(t, nsEmpty.Valid, "empty session id must also be NULL")
}

// TestPrepareUsageLogInsert_PlatformAssetAttributionWiring keeps the platform
// and selected billing source next to the subscription column in
// every INSERT path. This prevents a V2 request from losing its actual asset
// attribution when the usage log is persisted.
func TestPrepareUsageLogInsert_PlatformAssetAttributionWiring(t *testing.T) {
	platformID := int64(42)
	billingSourceType := "subscription"
	log := newSessionIDUsageLog(nil)
	log.PlatformID = &platformID
	log.BillingSourceType = &billingSourceType

	prepared := prepareUsageLogInsert(log)

	const (
		platformIDArgIndex        = 8
		billingSourceTypeArgIndex = 9
	)
	require.Equal(t, "bigint", usageLogInsertArgTypes[platformIDArgIndex])
	require.Equal(t, "text", usageLogInsertArgTypes[billingSourceTypeArgIndex])

	platformIDArg, ok := prepared.args[platformIDArgIndex].(sql.NullInt64)
	require.True(t, ok, "platform_id arg should be a sql.NullInt64, got %T", prepared.args[platformIDArgIndex])
	require.True(t, platformIDArg.Valid)
	require.Equal(t, platformID, platformIDArg.Int64)

	billingSourceTypeArg, ok := prepared.args[billingSourceTypeArgIndex].(sql.NullString)
	require.True(t, ok, "billing_source_type arg should be a sql.NullString, got %T", prepared.args[billingSourceTypeArgIndex])
	require.True(t, billingSourceTypeArg.Valid)
	require.Equal(t, billingSourceType, billingSourceTypeArg.String)
}

// TestUsageLogInsertQueries_IncludeSessionID guards that every generated INSERT path
// and the SELECT column list reference session_id.
func TestUsageLogInsertQueries_IncludeSessionID(t *testing.T) {
	require.Contains(t, usageLogSelectColumns, "session_id",
		"SELECT column list must include session_id")
	require.Contains(t, usageLogSelectColumns, "platform_id",
		"SELECT column list must include platform_id")
	require.Contains(t, usageLogSelectColumns, "billing_source_type",
		"SELECT column list must include billing_source_type")

	sessionID := "sess-in-query"
	log := newSessionIDUsageLog(&sessionID)
	prepared := prepareUsageLogInsert(log)
	key := usageLogBatchKey(log.RequestID, log.APIKeyID)

	batchQuery, batchArgs := buildUsageLogBatchInsertQuery([]string{key},
		map[string]usageLogInsertPrepared{key: prepared})
	require.Contains(t, batchQuery, "session_id")
	require.Contains(t, batchQuery, "platform_id")
	require.Contains(t, batchQuery, "billing_source_type")
	// Two column references (INSERT column list + SELECT ... FROM input) plus the CTE def.
	require.GreaterOrEqual(t, strings.Count(batchQuery, "session_id"), 3)
	require.Len(t, batchArgs, len(prepared.args)+1,
		"batch args include the synthetic input_index before usage-log values")

	bestEffortQuery, bestEffortArgs := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})
	require.Contains(t, bestEffortQuery, "session_id")
	require.Contains(t, bestEffortQuery, "platform_id")
	require.Contains(t, bestEffortQuery, "billing_source_type")
	require.Len(t, bestEffortArgs, len(prepared.args))
}
