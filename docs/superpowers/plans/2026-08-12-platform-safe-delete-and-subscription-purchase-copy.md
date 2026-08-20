# Platform Safe Delete and Subscription Purchase Copy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a guarded administrator platform deletion flow and make all repeat-subscription purchase wording match the existing independent-subscription behavior.

**Architecture:** PostgreSQL remains the authority for platform references. The platform repository locks the target row, briefly prevents writes to the no-FK operational/configuration tables, counts every direct and JSON-based reference, and deletes only when all external counts are zero; the service exposes a stable `PLATFORM_IN_USE` conflict with count metadata. Frontend changes stay within the existing admin platform API/view and payment/subscription components, while the payment backend and schema remain unchanged.

**Tech Stack:** Go 1.26, Gin, PostgreSQL, `database/sql`, Testify, sqlmock, Vue 3, TypeScript, Pinia, vue-i18n, Vitest, Vue Test Utils.

---

## File map

### Backend

- Modify `backend/internal/service/platform_service.go`: add the stable conflict error, deletion repository contract, and `PlatformService.Delete`.
- Modify `backend/internal/service/platform_service_test.go`: service-level deletion contract and error propagation tests.
- Modify `backend/internal/repository/platform_repo.go`: transactional reference counting and physical deletion.
- Create `backend/internal/repository/platform_repo_delete_test.go`: sqlmock tests for lock, blockers, rollback, deletion, and commit failures.
- Create `backend/internal/repository/platform_repo_delete_integration_test.go`: PostgreSQL coverage for direct columns, JSON configuration references, and successful cascade of model rules.
- Modify `backend/internal/handler/admin/platform_handler.go`: expose `Delete` through the administrator handler contract.
- Modify `backend/internal/handler/admin/platform_handler_test.go`: HTTP 200/400/404/409 coverage.
- Modify `backend/internal/server/routes/admin.go`: register `DELETE /admin/platforms/:id`.

### Frontend

- Modify `frontend/src/api/admin/platforms.ts`: add the typed delete call.
- Modify `frontend/src/views/admin/PlatformsView.vue`: trash action, danger confirmation, success/error handling, and list refresh.
- Modify `frontend/src/views/admin/__tests__/PlatformsView.spec.ts`: interaction and blocked-delete regression tests.
- Modify `frontend/src/i18n/locales/zh/admin/platforms.ts`: Chinese delete and blocker copy.
- Modify `frontend/src/i18n/locales/en/admin/platforms.ts`: English delete and blocker copy.
- Modify `frontend/src/components/payment/SubscriptionPlanCard.vue`: replace renewal semantics with “same active plan / add another”.
- Modify `frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts`: default and repeat-purchase button tests.
- Modify `frontend/src/views/user/PaymentView.vue`: add the independent-subscription notice in list and confirmation states; remove renewal naming.
- Modify `frontend/src/views/user/__tests__/PaymentView.spec.ts`: notice visibility and repeat-purchase route tests.
- Modify `frontend/src/views/user/SubscriptionsView.vue`: use the same “add another” wording and function naming.
- Modify `frontend/src/views/user/__tests__/SubscriptionsView.v2.spec.ts`: assert no renewal wording remains.
- Modify `frontend/src/i18n/locales/zh/common.ts` and `frontend/src/i18n/locales/en/common.ts`: rename the sale-template navigation label.
- Modify `frontend/src/i18n/locales/zh/misc.ts` and `frontend/src/i18n/locales/en/misc.ts`: add “add another”, purchase notice, page title, and description copy.
- Create `frontend/src/i18n/__tests__/subscriptionPurchaseCopy.spec.ts`: lock the exact Chinese and English business wording.

### Project memory

- Create `docs/memory/决策/2026-08-12-平台安全删除与独立订阅购买语义.md`: long-term deletion and purchase semantics.
- Modify `docs/memory/当前状态.md`: track implementation and verification status.

## Reference inventory locked by this plan

The repository check must cover the current platform-ID persistence surface:

| Category | Storage |
| --- | --- |
| Accounts | `accounts.platform_id` |
| API Key authorization | `api_key_platforms.platform_id` |
| Usage | `usage_logs.platform_id` |
| Audit/risk history | `prompt_audit_jobs.platform_id`, `prompt_audit_events.platform_id`, `content_moderation_logs.platform_id` |
| Scheduler/ops history | `scheduler_outbox.platform_id`, `ops_error_logs.platform_id`, `ops_system_metrics.platform_id`, `ops_metrics_hourly.platform_id`, `ops_metrics_daily.platform_id`, `ops_alert_silences.platform_id` |
| Ops JSON scope/history | `ops_alert_rules.filters.platform_id`, `ops_alert_events.dimensions.platform_id` |
| Runtime configuration JSON | `settings.content_moderation_config.platform_ids`, `settings.prompt_audit_config.platform_ids` when `all_platforms=false` |

`platform_model_rules.platform_id` is intentionally excluded from blockers because it is owned by the platform and has `ON DELETE CASCADE`. General administrator `audit_logs` are not a platform-domain foreign reference: they remain append-only and retain their path/request snapshot independently. Counting the current DELETE request itself would otherwise make a later retry permanently self-blocking.

The direct business/history tables have foreign keys to `platforms`; the target-row lock and the eventual `DELETE` therefore serialize with new FK inserts. The operational tables and settings JSON deliberately have no platform foreign key, so `DeleteUnused` must acquire `SHARE` table locks on `scheduler_outbox`, the `ops_*` blocker tables, and `settings` before counting. This rare administrator operation may pause writes to those tables for the duration of one short transaction, but it closes the check/delete race without adding a migration or asking every writer to participate in a new lock protocol.

### Task 1: Define the backend deletion contract

**Files:**
- Modify: `backend/internal/service/platform_service.go`
- Modify: `backend/internal/service/platform_service_test.go`

- [ ] **Step 1: Write failing service tests**

Extend `platformManagementRepositoryStub` with deletion recording and add these cases:

```go
type platformManagementRepositoryStub struct {
    platformRepositoryStub
    platform  *Platform
    updated   *Platform
    deletedID int64
    deleteErr error
}

func (r *platformManagementRepositoryStub) DeleteUnused(_ context.Context, id int64) error {
    r.deletedID = id
    return r.deleteErr
}

func TestPlatformServiceDeleteUsesAtomicRepositoryOperation(t *testing.T) {
    repo := &platformManagementRepositoryStub{}

    err := NewPlatformService(repo).Delete(context.Background(), 17)

    require.NoError(t, err)
    require.Equal(t, int64(17), repo.deletedID)
}

func TestPlatformServiceDeleteRejectsInvalidID(t *testing.T) {
    err := NewPlatformService(&platformManagementRepositoryStub{}).Delete(context.Background(), 0)

    require.ErrorIs(t, err, ErrPlatformNotFound)
}

func TestPlatformServiceDeletePreservesInUseError(t *testing.T) {
    repo := &platformManagementRepositoryStub{deleteErr: ErrPlatformInUse.WithMetadata(map[string]string{"accounts": "1"})}

    err := NewPlatformService(repo).Delete(context.Background(), 17)

    require.ErrorIs(t, err, ErrPlatformInUse)
    require.Equal(t, "1", infraerrors.FromError(err).Metadata["accounts"])
}
```

- [ ] **Step 2: Run the service tests and verify RED**

Run:

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run 'TestPlatformServiceDelete' -count=1
```

Expected: FAIL because `ErrPlatformInUse`, `DeleteUnused`, and `PlatformService.Delete` do not exist.

- [ ] **Step 3: Implement the service contract**

Add the stable error, repository method, and thin service method:

```go
var (
    ErrPlatformNotFound = infraerrors.NotFound("PLATFORM_NOT_FOUND", "platform not found")
    ErrPlatformExists   = infraerrors.Conflict("PLATFORM_EXISTS", "platform code already exists")
    ErrPlatformInUse    = infraerrors.Conflict("PLATFORM_IN_USE", "platform is referenced and cannot be deleted")
    ErrPlatformInvalid  = infraerrors.BadRequest("INVALID_PLATFORM", "invalid platform configuration")
)

type PlatformManagementRepository interface {
    PlatformRepository
    GetByID(ctx context.Context, id int64) (*Platform, error)
    Update(ctx context.Context, platform *Platform) error
    DeleteUnused(ctx context.Context, id int64) error
}

func (s *PlatformService) Delete(ctx context.Context, id int64) error {
    if id <= 0 {
        return ErrPlatformNotFound
    }
    repo, err := s.managementRepository()
    if err != nil {
        return err
    }
    if err := repo.DeleteUnused(ctx, id); err != nil {
        return fmt.Errorf("delete platform: %w", err)
    }
    return nil
}
```

- [ ] **Step 4: Run the service tests and verify GREEN**

Run the command from Step 2.

Expected: PASS.

- [ ] **Step 5: Commit the service contract**

```powershell
git add backend/internal/service/platform_service.go backend/internal/service/platform_service_test.go
git commit -m "feat(platform): 定义平台安全删除契约"
```

### Task 2: Implement atomic reference checks and deletion

**Files:**
- Modify: `backend/internal/repository/platform_repo.go`
- Create: `backend/internal/repository/platform_repo_delete_test.go`
- Create: `backend/internal/repository/platform_repo_delete_integration_test.go`

- [ ] **Step 1: Write failing sqlmock tests**

Create unit cases that expect this exact transaction sequence. Use `sql.LevelReadCommitted`; safety for FK-backed rows comes from PostgreSQL row/FK locking, and safety for no-FK rows comes from the explicit table locks:

```go
func TestPlatformRepositoryDeleteUnusedRejectsReferences(t *testing.T) {
    db, mock, err := sqlmock.New()
    require.NoError(t, err)
    t.Cleanup(func() { require.NoError(t, db.Close()) })
    repo := newPlatformRepository(db)

    mock.ExpectBegin()
    mock.ExpectQuery(`SELECT id FROM platforms WHERE id = \$1 FOR UPDATE`).
        WithArgs(int64(7)).
        WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(7)))
    mock.ExpectExec(`LOCK TABLE scheduler_outbox, ops_error_logs, ops_system_metrics, ops_metrics_hourly, ops_metrics_daily, ops_alert_silences, ops_alert_rules, ops_alert_events, settings IN SHARE MODE`).
        WillReturnResult(sqlmock.NewResult(0, 0))
    mock.ExpectQuery(`(?s)SELECT.*FROM accounts.*FROM api_key_platforms.*FROM settings`).
        WithArgs(int64(7)).
        WillReturnRows(sqlmock.NewRows(platformReferenceCountColumns()).
            AddRow(1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0))
    mock.ExpectRollback()

    err = repo.DeleteUnused(context.Background(), 7)

    require.ErrorIs(t, err, service.ErrPlatformInUse)
    require.Equal(t, "1", infraerrors.FromError(err).Metadata["accounts"])
    require.NoError(t, mock.ExpectationsWereMet())
}
```

Add these named tests with the same explicit sqlmock sequence:

```go
func TestPlatformRepositoryDeleteUnusedReturnsNotFound(t *testing.T)       // lock query returns sql.ErrNoRows; expect rollback
func TestPlatformRepositoryDeleteUnusedDeletesAndCommits(t *testing.T)    // 16 zero counts; expect one-row DELETE and commit
func TestPlatformRepositoryDeleteUnusedRejectsLostDelete(t *testing.T)    // DELETE affects zero rows; expect ErrPlatformNotFound and rollback
func TestPlatformRepositoryDeleteUnusedReportsLockFailure(t *testing.T)   // LOCK TABLE fails; expect wrapped error and rollback
func TestPlatformRepositoryDeleteUnusedReportsCountFailure(t *testing.T)  // count query fails; expect wrapped error and rollback
func TestPlatformRepositoryDeleteUnusedReportsDeleteFailure(t *testing.T) // DELETE fails; expect wrapped error and rollback
func TestPlatformRepositoryDeleteUnusedReportsCommitFailure(t *testing.T) // commit fails; expect wrapped error
func TestPlatformReferenceCountsMetadata(t *testing.T)                    // exact category sums below
```

For `TestPlatformReferenceCountsMetadata`, construct counts `1..16` in field order and assert exact metadata: `accounts=1`, `api_keys=2`, `usage=3`, `audits=15`, `ops=84`, and `configs=31`. Every test must finish with `require.NoError(t, mock.ExpectationsWereMet())`.

- [ ] **Step 2: Run repository unit tests and verify RED**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/repository -run 'TestPlatformRepositoryDeleteUnused' -count=1
```

Expected: FAIL because `DeleteUnused` and the reference-count helpers do not exist.

- [ ] **Step 3: Implement reference counts and metadata**

Add an internal count structure and fixed metadata projection:

```go
type platformReferenceCounts struct {
    Accounts             int64
    APIKeyPlatforms      int64
    UsageLogs            int64
    PromptAuditJobs      int64
    PromptAuditEvents    int64
    ContentModeration    int64
    SchedulerOutbox      int64
    OpsErrorLogs         int64
    OpsSystemMetrics     int64
    OpsMetricsHourly     int64
    OpsMetricsDaily      int64
    OpsAlertSilences     int64
    OpsAlertRules        int64
    OpsAlertEvents       int64
    ContentModerationCfg int64
    PromptAuditCfg       int64
}

func (c platformReferenceCounts) total() int64 {
    return c.Accounts + c.APIKeyPlatforms + c.UsageLogs + c.PromptAuditJobs +
        c.PromptAuditEvents + c.ContentModeration + c.SchedulerOutbox +
        c.OpsErrorLogs + c.OpsSystemMetrics + c.OpsMetricsHourly +
        c.OpsMetricsDaily + c.OpsAlertSilences + c.OpsAlertRules +
        c.OpsAlertEvents + c.ContentModerationCfg + c.PromptAuditCfg
}

func (c platformReferenceCounts) metadata() map[string]string {
    return map[string]string{
        "accounts": strconv.FormatInt(c.Accounts, 10),
        "api_keys": strconv.FormatInt(c.APIKeyPlatforms, 10),
        "usage": strconv.FormatInt(c.UsageLogs, 10),
        "audits": strconv.FormatInt(c.PromptAuditJobs+c.PromptAuditEvents+c.ContentModeration, 10),
        "ops": strconv.FormatInt(c.SchedulerOutbox+c.OpsErrorLogs+c.OpsSystemMetrics+c.OpsMetricsHourly+c.OpsMetricsDaily+c.OpsAlertSilences+c.OpsAlertRules+c.OpsAlertEvents, 10),
        "configs": strconv.FormatInt(c.ContentModerationCfg+c.PromptAuditCfg, 10),
    }
}
```

Acquire the target platform row lock first, then the write-blocking table locks before the count query. This ordering lets an already-running account update finish its scheduler-outbox write before deletion checks the final state, rather than forming a platform-row/table-lock cycle:

```go
const lockPlatformReferenceWritersSQL = `LOCK TABLE
  scheduler_outbox,
  ops_error_logs,
  ops_system_metrics,
  ops_metrics_hourly,
  ops_metrics_daily,
  ops_alert_silences,
  ops_alert_rules,
  ops_alert_events,
  settings
IN SHARE MODE`
```

Use one parameterized count query. Direct references use `COUNT(*)`; JSON references use `->>'platform_id'` or `jsonb_array_elements_text`. Configuration rows count only explicit platform selection (`all_platforms=false`). Both current config parsers initialize `AllPlatforms=true` before JSON unmarshal, so a legacy document without the field must also default to `true` here:

```sql
SELECT
  (SELECT COUNT(*) FROM accounts WHERE platform_id = $1),
  (SELECT COUNT(*) FROM api_key_platforms WHERE platform_id = $1),
  (SELECT COUNT(*) FROM usage_logs WHERE platform_id = $1),
  (SELECT COUNT(*) FROM prompt_audit_jobs WHERE platform_id = $1),
  (SELECT COUNT(*) FROM prompt_audit_events WHERE platform_id = $1),
  (SELECT COUNT(*) FROM content_moderation_logs WHERE platform_id = $1),
  (SELECT COUNT(*) FROM scheduler_outbox WHERE platform_id = $1),
  (SELECT COUNT(*) FROM ops_error_logs WHERE platform_id = $1),
  (SELECT COUNT(*) FROM ops_system_metrics WHERE platform_id = $1),
  (SELECT COUNT(*) FROM ops_metrics_hourly WHERE platform_id = $1),
  (SELECT COUNT(*) FROM ops_metrics_daily WHERE platform_id = $1),
  (SELECT COUNT(*) FROM ops_alert_silences WHERE platform_id = $1),
  (SELECT COUNT(*) FROM ops_alert_rules WHERE filters->>'platform_id' = $1::text),
  (SELECT COUNT(*) FROM ops_alert_events WHERE dimensions->>'platform_id' = $1::text),
  (SELECT COUNT(*) FROM settings s
     WHERE s.key = 'content_moderation_config'
       AND COALESCE((s.value::jsonb)->>'all_platforms', 'true') = 'false'
       AND EXISTS (
         SELECT 1 FROM jsonb_array_elements_text(COALESCE((s.value::jsonb)->'platform_ids', '[]'::jsonb)) v(id)
         WHERE v.id ~ '^[0-9]+$' AND v.id::bigint = $1
       )),
  (SELECT COUNT(*) FROM settings s
     WHERE s.key = 'prompt_audit_config'
       AND COALESCE((s.value::jsonb)->>'all_platforms', 'true') = 'false'
       AND EXISTS (
         SELECT 1 FROM jsonb_array_elements_text(COALESCE((s.value::jsonb)->'platform_ids', '[]'::jsonb)) v(id)
         WHERE v.id ~ '^[0-9]+$' AND v.id::bigint = $1
       ))
```

An invalid JSON config must fail closed with an internal error; it must not silently permit deletion.

- [ ] **Step 4: Implement the locked delete transaction**

```go
func (r *platformRepository) DeleteUnused(ctx context.Context, id int64) error {
    tx, err := r.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin platform delete transaction: %w", err)
    }
    defer func() { _ = tx.Rollback() }()

    var lockedID int64
    if err := tx.QueryRowContext(ctx,
        `SELECT id FROM platforms WHERE id = $1 FOR UPDATE`, id,
    ).Scan(&lockedID); err != nil {
        if err == sql.ErrNoRows {
            return service.ErrPlatformNotFound
        }
        return fmt.Errorf("lock platform for deletion: %w", err)
    }

    if _, err := tx.ExecContext(ctx, lockPlatformReferenceWritersSQL); err != nil {
        return fmt.Errorf("lock platform reference writers: %w", err)
    }

    counts, err := queryPlatformReferenceCounts(ctx, tx, id)
    if err != nil {
        return fmt.Errorf("check platform references: %w", err)
    }
    if counts.total() > 0 {
        return service.ErrPlatformInUse.WithMetadata(counts.metadata())
    }

    result, err := tx.ExecContext(ctx, `DELETE FROM platforms WHERE id = $1`, id)
    if err != nil {
        return fmt.Errorf("delete platform: %w", err)
    }
    affected, err := result.RowsAffected()
    if err != nil {
        return fmt.Errorf("read platform delete result: %w", err)
    }
    if affected != 1 {
        return service.ErrPlatformNotFound
    }
    if err := tx.Commit(); err != nil {
        return fmt.Errorf("commit platform delete transaction: %w", err)
    }
    return nil
}
```

No new cache invalidator is introduced: current model resolution and `/v1/models` paths read `ListModelRules` from durable storage. Safe deletion also forbids accounts and API Key grants, so a deletable platform cannot own scheduler snapshots, sticky account bindings, or API Key authorization entries that need active invalidation. Redis keys derived from a deleted, never-reused bigint ID may expire naturally and cannot route a future request because the durable platform/rule lookup no longer returns that ID.

- [ ] **Step 5: Run repository unit tests and verify GREEN**

Run the command from Step 2.

Expected: PASS.

- [ ] **Step 6: Add PostgreSQL integration coverage**

Create integration tests using the existing `integrationDB` harness and unique platform codes. Cover:

```go
func TestPlatformRepositoryDeleteUnusedIntegration(t *testing.T) {
    repo := newPlatformRepository(integrationDB)
    platform := createDeleteTestPlatform(t, repo)

    require.NoError(t, repo.DeleteUnused(context.Background(), platform.ID))

    var platformCount, ruleCount int
    require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM platforms WHERE id=$1`, platform.ID).Scan(&platformCount))
    require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM platform_model_rules WHERE platform_id=$1`, platform.ID).Scan(&ruleCount))
    require.Zero(t, platformCount)
    require.Zero(t, ruleCount)
}
```

Use table-driven subtests with setup/cleanup functions for exactly these representative persistence shapes:

```go
tests := []struct {
    name     string
    category string
    setup    func(t *testing.T, platformID int64)
    cleanup  func(t *testing.T, platformID int64)
}{
    {name: "account foreign key", category: "accounts", setup: insertAccountPlatformReference, cleanup: deleteAccountPlatformReference},
    {name: "api key authorization", category: "api_keys", setup: insertAPIKeyPlatformReference, cleanup: deleteAPIKeyPlatformReference},
    {name: "usage history", category: "usage", setup: insertUsagePlatformReference, cleanup: deleteUsagePlatformReference},
    {name: "prompt audit history", category: "audits", setup: insertPromptAuditPlatformReference, cleanup: deletePromptAuditPlatformReference},
    {name: "scheduler no-fk history", category: "ops", setup: insertSchedulerPlatformReference, cleanup: deleteSchedulerPlatformReference},
    {name: "ops json scope", category: "ops", setup: insertOpsAlertRulePlatformReference, cleanup: deleteOpsAlertRulePlatformReference},
    {name: "content moderation config", category: "configs", setup: insertContentModerationPlatformConfig, cleanup: deleteContentModerationPlatformConfig},
    {name: "prompt audit config", category: "configs", setup: insertPromptAuditPlatformConfig, cleanup: deletePromptAuditPlatformConfig},
}
```

Each subtest creates a unique platform and one model rule, calls `DeleteUnused`, asserts `ErrPlatformInUse` and non-zero metadata for `category`, then queries both rows to prove no deletion occurred. The two config fixtures must use `{"all_platforms":false,"platform_ids":[<id>]}`. Add a separate legacy-default test using `{"platform_ids":[<id>]}` and assert deletion succeeds, because the production parsers default an omitted `all_platforms` field to `true`. Cleanup every inserted row and platform explicitly.

- [ ] **Step 7: Run integration tests**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -p 1 -tags=integration ./internal/repository -run 'TestPlatformRepositoryDeleteUnusedIntegration' -count=1 -timeout=20m
```

Expected: PASS when Docker is available. If the local harness skips because Docker is unavailable, record the skip and require GitHub integration CI before release.

- [ ] **Step 8: Commit repository deletion**

```powershell
git add backend/internal/repository/platform_repo.go backend/internal/repository/platform_repo_delete_test.go backend/internal/repository/platform_repo_delete_integration_test.go
git commit -m "feat(platform): 实现平台引用检查与安全删除"
```

### Task 3: Expose the administrator DELETE endpoint

**Files:**
- Modify: `backend/internal/handler/admin/platform_handler.go`
- Modify: `backend/internal/handler/admin/platform_handler_test.go`
- Modify: `backend/internal/server/routes/admin.go`

- [ ] **Step 1: Write failing handler tests**

Extend the stub with `deleteErr` and `deletedID`, register DELETE in the test router, then add:

```go
func TestPlatformHandlerDeletesUnusedPlatform(t *testing.T) {
    stub := &platformHandlerServiceStub{}
    router := setupPlatformHandlerRouter(stub)
    recorder := httptest.NewRecorder()

    router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/v1/admin/platforms/7", nil))

    require.Equal(t, http.StatusOK, recorder.Code)
    require.Equal(t, int64(7), stub.deletedID)
    require.JSONEq(t, `{"code":0,"message":"success"}`, recorder.Body.String())
}

func TestPlatformHandlerReturnsConflictForReferencedPlatform(t *testing.T) {
    stub := &platformHandlerServiceStub{deleteErr: service.ErrPlatformInUse.WithMetadata(map[string]string{"accounts": "1"})}
    router := setupPlatformHandlerRouter(stub)
    recorder := httptest.NewRecorder()

    router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/v1/admin/platforms/7", nil))

    require.Equal(t, http.StatusConflict, recorder.Code)
    require.Contains(t, recorder.Body.String(), `"reason":"PLATFORM_IN_USE"`)
    require.Contains(t, recorder.Body.String(), `"accounts":"1"`)
}

func TestPlatformHandlerRejectsInvalidDeleteID(t *testing.T) {
    router := setupPlatformHandlerRouter(&platformHandlerServiceStub{})
    recorder := httptest.NewRecorder()

    router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/v1/admin/platforms/0", nil))

    require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestPlatformHandlerReturnsNotFoundWhenDeletingMissingPlatform(t *testing.T) {
    stub := &platformHandlerServiceStub{deleteErr: service.ErrPlatformNotFound}
    router := setupPlatformHandlerRouter(stub)
    recorder := httptest.NewRecorder()

    router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/v1/admin/platforms/7", nil))

    require.Equal(t, http.StatusNotFound, recorder.Code)
    require.Contains(t, recorder.Body.String(), `"reason":"PLATFORM_NOT_FOUND"`)
}
```

Extend `platformHandlerServiceStub` with `deletedID int64` and `deleteErr error`; its `Delete` method records the ID and returns `deleteErr`. Register `router.DELETE("/api/v1/admin/platforms/:id", handler.Delete)` in `setupPlatformHandlerRouter` before running the tests.

- [ ] **Step 2: Run handler tests and verify RED**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler/admin -run 'TestPlatformHandler.*Delete|TestPlatformHandlerReturnsConflict' -count=1
```

Expected: FAIL because the handler contract and route do not expose deletion.

- [ ] **Step 3: Implement handler and route**

```go
type platformManagementService interface {
    List(ctx context.Context) ([]service.Platform, error)
    GetByID(ctx context.Context, id int64) (*service.Platform, error)
    Create(ctx context.Context, input service.CreatePlatformInput) (*service.Platform, error)
    Update(ctx context.Context, id int64, input service.UpdatePlatformInput) (*service.Platform, error)
    Delete(ctx context.Context, id int64) error
}

func (h *PlatformHandler) Delete(c *gin.Context) {
    id, ok := parsePositiveIDParam(c, "id")
    if !ok {
        return
    }
    if err := h.platforms.Delete(c.Request.Context(), id); err != nil {
        response.ErrorFrom(c, err)
        return
    }
    response.Success(c, nil)
}
```

Register:

```go
platforms.DELETE("/:id", h.Admin.Platform.Delete)
```

Remove the outdated comments that say deletion is not exposed.

- [ ] **Step 4: Run handler tests and verify GREEN**

Run the command from Step 2.

Expected: PASS.

- [ ] **Step 5: Commit the endpoint**

```powershell
git add backend/internal/handler/admin/platform_handler.go backend/internal/handler/admin/platform_handler_test.go backend/internal/server/routes/admin.go
git commit -m "feat(platform): 开放管理员安全删除接口"
```

### Task 4: Add platform deletion to the administrator UI

**Files:**
- Modify: `frontend/src/api/admin/platforms.ts`
- Modify: `frontend/src/views/admin/PlatformsView.vue`
- Modify: `frontend/src/views/admin/__tests__/PlatformsView.spec.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/platforms.ts`
- Modify: `frontend/src/i18n/locales/en/admin/platforms.ts`

- [ ] **Step 1: Write failing view tests**

Mock `adminAPI.platforms.remove` and add this functional dialog stub so the test controls confirmation without depending on Teleport/BaseDialog behavior:

```ts
const ConfirmDialogStub = defineComponent({
  props: {
    show: { type: Boolean, required: true },
    title: { type: String, required: true },
    message: { type: String, required: true },
  },
  emits: ['confirm', 'cancel'],
  template: `
    <div data-test="platform-delete-dialog" :data-show="String(show)">
      <button data-test="confirm-platform-delete" @click="$emit('confirm')">confirm</button>
      <button data-test="cancel-platform-delete" @click="$emit('cancel')">cancel</button>
    </div>
  `,
})
```

Register it as `ConfirmDialog: ConfirmDialogStub`, render one platform row, and add:

```ts
it('deletes an unused platform only after confirmation and reloads the list', async () => {
  vi.mocked(adminAPI.platforms.list).mockResolvedValue([platformFixture])
  vi.mocked(adminAPI.platforms.remove).mockResolvedValue(undefined)
  const wrapper = mountView()
  await flushPromises()

  await wrapper.get('[data-test="delete-platform-1"]').trigger('click')
  expect(wrapper.get('[data-test="platform-delete-dialog"]').attributes('data-show')).toBe('true')
  await wrapper.get('[data-test="confirm-platform-delete"]').trigger('click')
  await flushPromises()

  expect(adminAPI.platforms.remove).toHaveBeenCalledWith(1)
  expect(showSuccess).toHaveBeenCalledWith('admin.platforms.deleted')
  expect(adminAPI.platforms.list).toHaveBeenCalledTimes(2)
})

it('shows a localized blocker and keeps the row when the platform is in use', async () => {
  vi.mocked(adminAPI.platforms.list).mockResolvedValue([platformFixture])
  vi.mocked(adminAPI.platforms.remove).mockRejectedValue({
    reason: 'PLATFORM_IN_USE',
    metadata: { accounts: '1', api_keys: '2', usage: '3', audits: '0', ops: '0', configs: '0' },
  })

  const wrapper = mountView()
  await flushPromises()
  await wrapper.get('[data-test="delete-platform-1"]').trigger('click')
  await wrapper.get('[data-test="confirm-platform-delete"]').trigger('click')
  await flushPromises()

  expect(adminAPI.platforms.remove).toHaveBeenCalledWith(1)
  expect(showError).toHaveBeenCalledWith('admin.platforms.errors.PLATFORM_IN_USE')
  expect(wrapper.findAll('[data-test="platform-row"]')).toHaveLength(1)
})
```

The test `t` mock must accept interpolation parameters. Reset `list`, `remove`, `showError`, and `showSuccess` in `beforeEach`.

- [ ] **Step 2: Run the view tests and verify RED**

```powershell
cd frontend
pnpm run test:run -- src/views/admin/__tests__/PlatformsView.spec.ts
```

Expected: FAIL because the API and delete controls do not exist.

- [ ] **Step 3: Add the API method and confirmation flow**

```ts
export async function remove(id: number): Promise<void> {
  await apiClient.delete(`/admin/platforms/${id}`)
}

const platformsAPI = { list, getById, create, update, remove }
```

In `PlatformsView.vue`, add `ConfirmDialog`, a trash action, and guarded state:

```ts
const deleteTarget = ref<PlatformPool | null>(null)
const deleting = ref(false)

function requestDelete(platform: PlatformPool) {
  deleteTarget.value = platform
}

function cancelDelete() {
  if (!deleting.value) deleteTarget.value = null
}

async function confirmDelete() {
  if (!deleteTarget.value || deleting.value) return
  deleting.value = true
  try {
    await adminAPI.platforms.remove(deleteTarget.value.id)
    appStore.showSuccess(t('admin.platforms.deleted'))
    deleteTarget.value = null
    await loadPlatforms()
  } catch (error) {
    appStore.showError(extractI18nErrorMessage(error, t, 'admin.platforms.errors', t('admin.platforms.deleteFailed')))
  } finally {
    deleting.value = false
  }
}
```

Render edit and delete buttons in a flex action group. The delete button uses `Icon name="trash"`, an i18n title/aria-label, and `data-test="delete-platform-${row.id}"`. Render a danger `ConfirmDialog` whose message interpolates the platform name and explicitly states that referenced platforms cannot be deleted.

- [ ] **Step 4: Add localized deletion copy**

Add these Chinese keys:

```ts
delete: '删除平台',
deleteTitle: '删除平台',
deleteConfirm: '确定删除平台“{name}”吗？只有从未被账号、密钥、使用记录、审计、运维记录或配置引用的平台才能删除。',
deleted: '平台已删除',
deleteFailed: '删除平台失败',
errors: {
  PLATFORM_IN_USE: '该平台仍在使用中（账号 {accounts}、密钥授权 {api_keys}、使用记录 {usage}、审计/风控 {audits}、运维记录 {ops}、系统配置 {configs}），不能删除。',
},
```

Add the English equivalents:

```ts
delete: 'Delete Platform',
deleteTitle: 'Delete Platform',
deleteConfirm: 'Delete platform “{name}”? Only platforms never referenced by accounts, API keys, usage, audits, operations, or configuration can be deleted.',
deleted: 'Platform deleted',
deleteFailed: 'Failed to delete platform',
errors: {
  PLATFORM_IN_USE: 'This platform is still in use (accounts {accounts}, API key grants {api_keys}, usage {usage}, audit/risk {audits}, operations {ops}, configuration {configs}) and cannot be deleted.',
},
```

- [ ] **Step 5: Run the view tests and verify GREEN**

Run the command from Step 2.

Expected: PASS.

- [ ] **Step 6: Commit the administrator UI**

```powershell
git add frontend/src/api/admin/platforms.ts frontend/src/views/admin/PlatformsView.vue frontend/src/views/admin/__tests__/PlatformsView.spec.ts frontend/src/i18n/locales/zh/admin/platforms.ts frontend/src/i18n/locales/en/admin/platforms.ts
git commit -m "feat(platform): 增加平台安全删除界面"
```

### Task 5: Correct independent-subscription purchase semantics

**Files:**
- Modify: `frontend/src/components/payment/SubscriptionPlanCard.vue`
- Modify: `frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts`
- Modify: `frontend/src/views/user/PaymentView.vue`
- Modify: `frontend/src/views/user/__tests__/PaymentView.spec.ts`
- Modify: `frontend/src/views/user/SubscriptionsView.vue`
- Modify: `frontend/src/views/user/__tests__/SubscriptionsView.v2.spec.ts`
- Modify: `frontend/src/i18n/locales/zh/misc.ts`
- Modify: `frontend/src/i18n/locales/en/misc.ts`

- [ ] **Step 1: Write failing plan-card tests**

Add an active subscription fixture with the same `subscription_plan_id` and assert:

```ts
it('offers another independent subscription when the same plan is active', () => {
  const wrapper = mountCard({
    activeSubscriptions: [{
      id: 9,
      subscription_plan_id: plan.id,
      status: 'active',
    } as UserSubscription],
  })

  expect(wrapper.get('button').text()).toBe('payment.addAnother')
  expect(wrapper.text()).not.toContain('payment.renewNow')
})
```

Keep the existing no-match test expecting `payment.subscribeNow`.

- [ ] **Step 2: Write failing payment-view and subscription-view tests**

For the plan-list and selected-plan mounts, assert exactly one notice marker in each state:

```ts
expect(wrapper.get('[data-test="subscription-instance-notice"]').text())
  .toContain('payment.subscriptionInstanceNotice')
```

Replace the source assertion in `SubscriptionsView.v2.spec.ts` with:

```ts
it('offers another independent subscription without legacy routing or renewal semantics', () => {
  expect(source).toContain('subscription.subscription_plan_id')
  expect(source).toContain("t('payment.addAnother')")
  expect(source).toContain('buyAnotherSubscription(subscription)')
  expect(source).toContain("query: { tab: 'subscription', plan: String(subscription.subscription_plan_id) }")
  expect(source).not.toContain("t('payment.renewNow')")
  expect(source).not.toContain('renewSubscription')
  expect(source).not.toContain('renewal')
  expect(source).not.toContain('subscription.group?.platform')
  expect(source).not.toContain('subscription.group?.description')
})
```

- [ ] **Step 3: Run focused tests and verify RED**

```powershell
cd frontend
pnpm run test:run -- src/components/payment/__tests__/SubscriptionPlanCard.spec.ts src/views/user/__tests__/PaymentView.spec.ts src/views/user/__tests__/SubscriptionsView.v2.spec.ts
```

Expected: FAIL on the new translation keys and notice markers.

- [ ] **Step 4: Replace renewal state and copy**

In `SubscriptionPlanCard.vue`:

```ts
const hasSameActivePlan = computed(() =>
  props.activeSubscriptions?.some(subscription =>
    subscription.subscription_plan_id === props.plan.id && subscription.status === 'active',
  ) ?? false,
)
```

Render:

```vue
{{ hasSameActivePlan ? t('payment.addAnother') : t('payment.subscribeNow') }}
```

In `SubscriptionsView.vue`, rename `renewSubscription` to `buyAnotherSubscription` and use `payment.addAnother`. Keep navigation by immutable `subscription_plan_id` unchanged.

In `PaymentView.vue`, rename the comment to “Handle repeat-purchase navigation by immutable plan ID” and add the same notice block immediately before the selected-plan card and immediately before the non-empty plan grid:

```vue
<div
  data-test="subscription-instance-notice"
  class="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200"
>
  {{ t('payment.subscriptionInstanceNotice') }}
</div>
```

- [ ] **Step 5: Add Chinese and English payment copy**

Replace `renewNow` with:

```ts
addAnother: '再来一个',
subscriptionInstanceNotice: '每次购买都会新增一个独立订阅，不会延长已有订阅的有效期；多个订阅可同时存在，并按系统规则依次使用。',
```

English:

```ts
addAnother: 'Get Another',
subscriptionInstanceNotice: 'Each purchase creates a separate subscription and does not extend an existing subscription. Multiple subscriptions can coexist and are used in the system-defined order.',
```

Remove the unused `renewNow` keys so source and locale files no longer encode false renewal semantics.

- [ ] **Step 6: Run focused tests and verify GREEN**

Run the command from Step 3.

Expected: PASS.

- [ ] **Step 7: Scan for misleading renewal names**

```powershell
rg -n "renewNow|isRenewal|renewSubscription|renewal navigation|续费" frontend/src
```

Expected: no application matches. Test fixture prose such as third-party token renewal is outside subscription purchasing and must not be renamed.

- [ ] **Step 8: Commit the purchase semantics**

```powershell
git add frontend/src/components/payment/SubscriptionPlanCard.vue frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts frontend/src/views/user/PaymentView.vue frontend/src/views/user/__tests__/PaymentView.spec.ts frontend/src/views/user/SubscriptionsView.vue frontend/src/views/user/__tests__/SubscriptionsView.v2.spec.ts frontend/src/i18n/locales/zh/misc.ts frontend/src/i18n/locales/en/misc.ts
git commit -m "fix(subscription): 明确重复购买创建独立订阅"
```

### Task 6: Rename sale-template administration to “上架订阅”

**Files:**
- Modify: `frontend/src/i18n/locales/zh/common.ts`
- Modify: `frontend/src/i18n/locales/en/common.ts`
- Modify: `frontend/src/i18n/locales/zh/misc.ts`
- Modify: `frontend/src/i18n/locales/en/misc.ts`
- Create: `frontend/src/i18n/__tests__/subscriptionPurchaseCopy.spec.ts`

- [ ] **Step 1: Write failing exact-copy tests**

```ts
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

it('uses sale-listing language for administrator plan templates', () => {
  expect(zh.nav.paymentPlans).toBe('上架订阅')
  expect(zh.payment.admin.plansPageTitle).toBe('上架订阅')
  expect(zh.payment.admin.plansPageDesc).toBe('管理可售订阅套餐')
  expect(en.nav.paymentPlans).toBe('List Subscriptions')
  expect(en.payment.admin.plansPageTitle).toBe('List Subscriptions')
  expect(en.payment.admin.plansPageDesc).toBe('Manage subscription plans available for sale')
})

it('locks the independent-subscription purchase disclosure', () => {
  expect(zh.payment.addAnother).toBe('再来一个')
  expect(zh.payment.subscriptionInstanceNotice).toContain('不会延长已有订阅的有效期')
  expect(en.payment.addAnother).toBe('Get Another')
})
```

- [ ] **Step 2: Run the exact-copy test and verify RED**

```powershell
cd frontend
pnpm run test:run -- src/i18n/__tests__/subscriptionPurchaseCopy.spec.ts
```

Expected: FAIL on the old administrator labels.

- [ ] **Step 3: Update only the sale-template labels**

Set:

```ts
paymentPlans: '上架订阅'
plansPageTitle: '上架订阅'
plansPageDesc: '管理可售订阅套餐'
```

Set the English values explicitly:

```ts
paymentPlans: 'List Subscriptions'
plansPageTitle: 'List Subscriptions'
plansPageDesc: 'Manage subscription plans available for sale'
```

Do not rename `nav.subscriptions`, user subscription instances, API Key subscription authorization, or usage billing-source text.

- [ ] **Step 4: Run the exact-copy test and sidebar regression**

```powershell
cd frontend
pnpm run test:run -- src/i18n/__tests__/subscriptionPurchaseCopy.spec.ts src/components/layout/__tests__/AppSidebar.spec.ts
```

Expected: PASS.

- [ ] **Step 5: Commit the administrator wording**

```powershell
git add frontend/src/i18n/locales/zh/common.ts frontend/src/i18n/locales/en/common.ts frontend/src/i18n/locales/zh/misc.ts frontend/src/i18n/locales/en/misc.ts frontend/src/i18n/__tests__/subscriptionPurchaseCopy.spec.ts
git commit -m "fix(i18n): 将可售套餐入口改为上架订阅"
```

### Task 7: Record the durable decision and run release-quality verification

**Files:**
- Create: `docs/memory/决策/2026-08-12-平台安全删除与独立订阅购买语义.md`
- Modify: `docs/memory/当前状态.md`

- [ ] **Step 1: Write the project decision**

Record:

```markdown
# 平台安全删除与独立订阅购买语义

- 状态：已确认并实施
- 日期：2026-08-12
- 结论：平台只允许在无业务、历史、运维和配置引用时物理删除；重复购买套餐始终创建独立订阅，不延长已有订阅。
- 原因：避免级联或 SET NULL 丢失平台归属，并让购买文案与现有多订阅调度行为一致。
- 影响：新增引用平台 ID 的表或 JSON 配置时，必须同步扩展平台删除引用检查；界面统一使用“再来一个”和独立订阅提示。
- 相关文件：`backend/internal/repository/platform_repo.go`、`backend/internal/service/platform_service.go`、`frontend/src/views/admin/PlatformsView.vue`、`frontend/src/views/user/PaymentView.vue`、`frontend/src/components/payment/SubscriptionPlanCard.vue`
```

Update current status with the implementation result, exact test commands, and any integration skip. Do not overwrite the existing user-authored history with speculative results.

- [ ] **Step 2: Run complete relevant backend verification**

```powershell
cd backend
$env:GOFLAGS='-p=1'
$env:GOMAXPROCS='2'
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service ./internal/repository ./internal/handler/admin ./internal/server/routes -run 'Platform' -count=1 -timeout=20m
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service ./internal/repository ./internal/handler/admin ./internal/server/routes -count=1 -timeout=30m
& 'C:\Program Files\Go\bin\go.exe' build ./cmd/server
```

Expected: all commands exit 0.

- [ ] **Step 3: Run complete relevant frontend verification**

```powershell
cd frontend
pnpm run test:run -- src/views/admin/__tests__/PlatformsView.spec.ts src/components/payment/__tests__/SubscriptionPlanCard.spec.ts src/views/user/__tests__/PaymentView.spec.ts src/views/user/__tests__/SubscriptionsView.v2.spec.ts src/i18n/__tests__/subscriptionPurchaseCopy.spec.ts src/components/layout/__tests__/AppSidebar.spec.ts
pnpm run typecheck
pnpm run lint:check
pnpm run build
```

Expected: all commands exit 0.

- [ ] **Step 4: Run static safety scans**

```powershell
rg -n "renewNow|isRenewal|renewSubscription|renewal navigation|续费" frontend/src
rg -n "platform_id" backend/migrations backend/ent/schema backend/internal | Out-File -Encoding utf8 $env:TEMP\platform-id-audit.txt
git diff --check
git status --short
```

Expected:

- no misleading subscription-renewal application matches;
- every persisted platform-ID source in the audit is either covered by `DeleteUnused` or documented as non-domain/cache-only;
- `git diff --check` exits 0;
- `frontend/pnpm-lock.yaml` and `my2.0.drawio` remain untouched and unstaged unless the user separately authorizes them.

- [ ] **Step 5: Review against the confirmed specification**

Check each section of `docs/superpowers/specs/2026-08-12-platform-safe-delete-and-subscription-purchase-copy-design.md`:

- unused platform deletes; all reference categories block;
- no force parameter, automatic unbinding, or history nulling;
- model rules cascade only after all blockers are zero;
- administrator wording says “上架订阅” only in the sale-template context;
- repeat purchase says “再来一个” and displays the notice in both list and confirmation states;
- payment fulfillment and database schema are unchanged.

- [ ] **Step 6: Commit memory and final test adjustments**

```powershell
git add docs/memory/决策/2026-08-12-平台安全删除与独立订阅购买语义.md docs/memory/当前状态.md
git commit -m "docs(memory): 记录平台删除与独立订阅规则"
```

- [ ] **Step 7: Stop before release operations**

Report commit IDs and verification results. Do not create a Tag, push, wait for Docker artifacts, or deploy until the user explicitly authorizes those remote and infrastructure operations for this implementation.
