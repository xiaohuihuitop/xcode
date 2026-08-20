# Platform Controlled Cascade Delete Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow administrators to delete a platform with only historical/audit/ops references by atomically clearing those references, while accounts or API keys still block deletion.

**Architecture:** Extend the platform management repository with a reusable deletion-impact reader and an atomic controlled-delete transaction. The admin API exposes the impact preview, while the existing DELETE endpoint performs a fresh locked recount, cleans only approved reference classes, and returns the cleanup result. The Vue page loads the preview before confirmation and never treats preview data as authorization.

**Tech Stack:** Go 1.26, PostgreSQL, Gin, sqlmock/testify, Vue 3, TypeScript, Vitest, vue-i18n, pnpm 9.

---

## File Map

- Modify `backend/internal/service/platform_service.go`: public impact/result types and service methods.
- Modify `backend/internal/repository/platform_repo.go`: impact query, table locking, approved cleanup SQL, setting JSON updates, atomic delete.
- Modify `backend/internal/repository/platform_repo_delete_test.go`: repository TDD for preview, blockers, cleanup order, optional table and rollback.
- Modify `backend/internal/handler/admin/platform_handler.go`: impact response and delete cleanup response.
- Modify `backend/internal/handler/admin/platform_handler_test.go`: API contracts and status behavior.
- Modify `backend/internal/server/routes/admin.go`: register `GET /platforms/:id/delete-impact`.
- Modify `frontend/src/types/index.ts`: impact and result contracts.
- Modify `frontend/src/api/admin/platforms.ts`: preview and typed deletion calls.
- Modify `frontend/src/views/admin/PlatformsView.vue`: preview-driven confirm state and messages.
- Modify `frontend/src/views/admin/__tests__/PlatformsView.spec.ts`: loading, blocking, cleanup confirmation and stale-preview conflict tests.
- Modify `frontend/src/i18n/locales/zh/admin/platforms.ts` and `frontend/src/i18n/locales/en/admin/platforms.ts`: accurate destructive-copy text.
- Modify `docs/memory/决策/2026-08-13-平台安全删除与独立订阅购买语义.md`: replace the superseded no-cleanup rule.
- Modify `docs/memory/当前状态.md`: record implementation and verification status.

### Task 1: Define deletion impact and service contract

**Files:**
- Modify: `backend/internal/service/platform_service.go`
- Test: `backend/internal/service/platform_service_test.go`

- [ ] **Step 1: Write failing service tests**

Add a management repository stub that implements:

```go
PreviewDelete(ctx context.Context, id int64) (*PlatformDeleteImpact, error)
DeleteControlled(ctx context.Context, id int64) (*PlatformDeleteResult, error)
```

Test that `PreviewDelete` rejects non-positive IDs, clones the repository result, and that `Delete` delegates to `DeleteControlled` and returns its cleanup result.

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```powershell
go test -tags=unit ./internal/service -run 'PlatformService.*Delete' -count=1
```

Expected: FAIL because the new types and methods do not exist.

- [ ] **Step 3: Add the minimal service types and methods**

Define:

```go
type PlatformDeleteImpact struct {
    Accounts  int64 `json:"accounts"`
    APIKeys   int64 `json:"api_keys"`
    UsageLogs int64 `json:"usage_logs"`
    Audits    int64 `json:"audits"`
    Ops       int64 `json:"ops"`
    Configs   int64 `json:"configs"`
    CanDelete bool  `json:"can_delete"`
}

type PlatformDeleteResult struct {
    PlatformID int64                `json:"platform_id"`
    Cleaned    PlatformDeleteImpact `json:"cleaned"`
}
```

Replace `DeleteUnused` in `PlatformManagementRepository` with `PreviewDelete` and `DeleteControlled`. `CanDelete` must be derived only from `Accounts == 0 && APIKeys == 0`.

- [ ] **Step 4: Run the focused tests and verify GREEN**

Run the command from Step 2. Expected: PASS.

- [ ] **Step 5: Commit the service contract**

```powershell
git add backend/internal/service/platform_service.go backend/internal/service/platform_service_test.go
git commit -m "refactor(platform): 定义受控删除契约"
```

### Task 2: Implement repository preview and atomic cleanup

**Files:**
- Modify: `backend/internal/repository/platform_repo.go`
- Test: `backend/internal/repository/platform_repo_delete_test.go`

- [ ] **Step 1: Replace repository tests with the new expected behavior**

Add focused tests for:

```go
func TestPlatformRepositoryPreviewDeleteAggregatesImpact(t *testing.T)
func TestPlatformRepositoryDeleteControlledBlocksAccounts(t *testing.T)
func TestPlatformRepositoryDeleteControlledBlocksAPIKeys(t *testing.T)
func TestPlatformRepositoryDeleteControlledClearsHistoricalReferences(t *testing.T)
func TestPlatformRepositoryDeleteControlledAllowsMissingOptionalOpsTable(t *testing.T)
func TestPlatformRepositoryDeleteControlledRollsBackOnCleanupFailure(t *testing.T)
```

The successful cleanup test must expect parameterized statements in dependency order:

```sql
DELETE FROM prompt_audit_events WHERE platform_id = $1;
DELETE FROM prompt_audit_jobs WHERE platform_id = $1;
DELETE FROM content_moderation_logs WHERE platform_id = $1;
DELETE FROM scheduler_outbox WHERE platform_id = $1;
DELETE FROM ops_error_logs WHERE platform_id = $1;
DELETE FROM ops_system_metrics WHERE platform_id = $1;
DELETE FROM ops_metrics_hourly WHERE platform_id = $1;
DELETE FROM ops_metrics_daily WHERE platform_id = $1;
DELETE FROM ops_alert_events WHERE COALESCE(dimensions, '{}'::jsonb) @> jsonb_build_object('platform_id', $1);
DELETE FROM ops_alert_rules WHERE COALESCE(filters, '{}'::jsonb) @> jsonb_build_object('platform_id', $1);
```

For `ops_alert_silences`, expect a DELETE only when `to_regclass` reports the table exists. Settings expectations must update JSON without deleting the row:

```sql
UPDATE settings
SET value = jsonb_set(
  value::jsonb,
  '{platform_ids}',
  COALESCE((SELECT jsonb_agg(v) FROM jsonb_array_elements(COALESCE(value::jsonb->'platform_ids','[]'::jsonb)) v WHERE v <> to_jsonb($1::bigint)), '[]'::jsonb),
  true
)::text
WHERE key IN ('content_moderation_config', 'prompt_audit_config')
  AND COALESCE(value::jsonb->'platform_ids', '[]'::jsonb) @> jsonb_build_array($1);
```

- [ ] **Step 2: Run repository tests and verify RED**

```powershell
go test -tags=unit ./internal/repository -run 'PlatformRepository.*Delete' -count=1
```

Expected: FAIL because preview and controlled cleanup are not implemented.

- [ ] **Step 3: Extract a shared impact query helper**

Keep `platformReferenceCounts`, add `impact()` returning aggregate audit/ops/config counts, and implement one helper that accepts `*sql.Tx`, locks conditionally, probes `ops_alert_silences`, and executes `platformReferenceCountSQLTemplate`. Preview uses a read-only transaction without table locks; deletion uses the platform row lock plus reference-table locks and a fresh count.

- [ ] **Step 4: Implement blocking and approved cleanup**

In `DeleteControlled`, return `ErrPlatformInUse.WithMetadata(...)` only when `accounts > 0 || apiKeys > 0`. Otherwise execute all approved cleanup statements, delete platform model rules explicitly if the database relationship does not cascade, then delete the platform and commit. Populate `PlatformDeleteResult.Cleaned` from the locked pre-cleanup counts.

- [ ] **Step 5: Verify repository GREEN and rollback behavior**

Run the command from Step 2. Expected: PASS, including the injected cleanup error test expecting `ROLLBACK` and no platform DELETE.

- [ ] **Step 6: Run PostgreSQL integration coverage**

Add or extend a repository integration test that creates two platforms, inserts one historical row per supported class for only the target platform, deletes it, and proves the control platform remains unchanged.

```powershell
go test -tags=integration ./internal/repository -run 'Platform.*DeleteControlled' -count=1 -timeout=10m
```

Expected: PASS when Docker/PostgreSQL is available; otherwise record the explicit skip and rely on GitHub's integration gate before release.

- [ ] **Step 7: Commit repository behavior**

```powershell
git add backend/internal/repository/platform_repo.go backend/internal/repository/platform_repo_delete_test.go backend/internal/repository/*platform*integration_test.go
git commit -m "feat(platform): 自动清理历史引用后删除"
```

### Task 3: Expose impact preview and cleanup result APIs

**Files:**
- Modify: `backend/internal/handler/admin/platform_handler.go`
- Modify: `backend/internal/handler/admin/platform_handler_test.go`
- Modify: `backend/internal/server/routes/admin.go`

- [ ] **Step 1: Write failing handler and route tests**

Cover `GET /api/v1/admin/platforms/7/delete-impact` returning the exact typed payload, DELETE returning `PlatformDeleteResult`, invalid IDs, not found, and a stale preview followed by `409 PLATFORM_IN_USE`.

- [ ] **Step 2: Run tests and verify RED**

```powershell
go test -tags=unit ./internal/handler/admin ./internal/server/routes -run 'Platform.*(Delete|Impact)' -count=1
```

Expected: FAIL because the preview handler and route do not exist.

- [ ] **Step 3: Implement handler methods and route**

Extend `platformManagementService` with `PreviewDelete`. Add `DeleteImpact`, return `response.Success(c, impact)`, return the delete result from `Delete`, and register:

```go
platforms.GET("/:id/delete-impact", h.Admin.Platform.DeleteImpact)
```

- [ ] **Step 4: Run handler and route tests and verify GREEN**

Run the command from Step 2. Expected: PASS.

- [ ] **Step 5: Commit API behavior**

```powershell
git add backend/internal/handler/admin/platform_handler.go backend/internal/handler/admin/platform_handler_test.go backend/internal/server/routes/admin.go
git commit -m "feat(platform): 提供删除影响预览"
```

### Task 4: Add typed frontend preview API

**Files:**
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/admin/platforms.ts`
- Test: `frontend/src/views/admin/__tests__/PlatformsView.spec.ts`

- [ ] **Step 1: Add failing API-usage assertions to the view test**

Mock `adminAPI.platforms.previewDelete` and require it to be called with the selected platform ID before the dialog appears. Change `remove` to resolve a typed cleanup result.

- [ ] **Step 2: Run focused Vitest and verify RED**

```powershell
pnpm test:run -- src/views/admin/__tests__/PlatformsView.spec.ts
```

Expected: FAIL because `previewDelete` and typed contracts do not exist.

- [ ] **Step 3: Add TypeScript contracts and API methods**

```ts
export interface PlatformDeleteImpact {
  accounts: number
  api_keys: number
  usage_logs: number
  audits: number
  ops: number
  configs: number
  can_delete: boolean
}

export interface PlatformDeleteResult {
  platform_id: number
  cleaned: PlatformDeleteImpact
}
```

Add `previewDelete(id)` using GET and make `remove(id)` return `Promise<PlatformDeleteResult>`.

- [ ] **Step 4: Run focused Vitest and typecheck**

```powershell
pnpm test:run -- src/views/admin/__tests__/PlatformsView.spec.ts
pnpm typecheck
```

Expected: PASS.

- [ ] **Step 5: Commit frontend API contracts**

```powershell
git add frontend/src/types/index.ts frontend/src/api/admin/platforms.ts frontend/src/views/admin/__tests__/PlatformsView.spec.ts
git commit -m "feat(platform): 增加删除影响接口契约"
```

### Task 5: Implement preview-driven confirmation UI

**Files:**
- Modify: `frontend/src/views/admin/PlatformsView.vue`
- Modify: `frontend/src/views/admin/__tests__/PlatformsView.spec.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/platforms.ts`
- Modify: `frontend/src/i18n/locales/en/admin/platforms.ts`

- [ ] **Step 1: Add failing UI tests**

Test these four cases:

1. Clicking trash loads the preview and shows permanent cleanup counts.
2. `can_delete: false` with accounts/API keys does not call DELETE.
3. `can_delete: true` allows confirmation and refreshes the list.
4. DELETE returning a stale-preview `PLATFORM_IN_USE` closes no data and shows the localized blocker.

- [ ] **Step 2: Run focused Vitest and verify RED**

Use the Task 4 Vitest command. Expected: FAIL on missing preview state and text.

- [ ] **Step 3: Implement explicit preview state**

Add `deleteImpact`, `deleteImpactLoading`, and `deleteImpactError`. `confirmDelete` must await `previewDelete`; the dialog opens only with a successful preview. Disable or hide the confirm command when `can_delete` is false, and keep the final DELETE independent of preview authorization.

- [ ] **Step 4: Add accurate Chinese and English copy**

Chinese copy must state that history is permanently removed and user assets remain intact. Replace the old text claiming history is never automatically cleared. English must communicate identical semantics.

- [ ] **Step 5: Run focused tests, typecheck, lint and build**

```powershell
pnpm test:run -- src/views/admin/__tests__/PlatformsView.spec.ts
pnpm typecheck
pnpm lint:check
pnpm build
```

Expected: all commands exit 0.

- [ ] **Step 6: Commit UI behavior**

```powershell
git add frontend/src/views/admin/PlatformsView.vue frontend/src/views/admin/__tests__/PlatformsView.spec.ts frontend/src/i18n/locales/zh/admin/platforms.ts frontend/src/i18n/locales/en/admin/platforms.ts
git commit -m "feat(platform): 展示并确认历史清理影响"
```

### Task 6: Update decisions and run full gates

**Files:**
- Modify: `docs/memory/决策/2026-08-13-平台安全删除与独立订阅购买语义.md`
- Modify: `docs/memory/当前状态.md`

- [ ] **Step 1: Correct the superseded long-term decision**

Replace “不清除历史记录” with the confirmed controlled cascade policy. Record that only accounts and API keys block deletion, and all approved historical cleanup remains atomic and previewed.

- [ ] **Step 2: Run backend gates**

```powershell
cd backend
make test-unit
make test-integration
go build ./cmd/server
```

Expected: all exit 0; integration may only be reported as skipped if the framework itself explicitly skips for unavailable Docker.

- [ ] **Step 3: Run frontend gates**

```powershell
cd frontend
pnpm install --frozen-lockfile
pnpm test:run
pnpm typecheck
pnpm lint:check
pnpm build
```

Expected: all exit 0.

- [ ] **Step 4: Run repository hygiene checks**

```powershell
git diff --check
git status --short
```

Expected: no whitespace errors and only planned files are modified.

- [ ] **Step 5: Review the complete diff against the approved design**

Verify every cleanup table appears exactly once, every DELETE is parameterized, accounts/API keys cannot be removed by this path, settings retain non-target fields, and preview does not bypass the locked recount.

- [ ] **Step 6: Commit memory and final verification status**

```powershell
git add docs/memory/决策/2026-08-13-平台安全删除与独立订阅购买语义.md docs/memory/当前状态.md
git commit -m "docs(platform): 记录受控级联删除规则"
```

### Task 7: Release and production verification after explicit authorization

**Files:**
- No source edits expected.

- [ ] **Step 1: Confirm remote state and choose the next unused My2 tag**

```powershell
git fetch origin --tags
git ls-remote --tags origin "refs/tags/my2-v0.2.*"
```

Expected: select the next unused tag; never move or reuse an existing tag.

- [ ] **Step 2: Push branch and tag**

Only after the user explicitly authorizes remote operations:

```powershell
git push origin HEAD:my2.0
git tag -a <next-tag> -m "<next-tag>"
git push origin <next-tag>
```

- [ ] **Step 3: Wait for all GitHub gates and verify offline asset hash**

Require CI, Security Scan and My2 Release success. Download `xcode_latest.tar` and its `.sha256`; run `sha256sum -c` before deployment.

- [ ] **Step 4: Back up and deploy only the app container**

Back up compose, environment file and PostgreSQL. Load `xcode:latest` and recreate only the application container; do not recreate PostgreSQL or Redis.

- [ ] **Step 5: Run non-destructive production validation first**

Verify `/health`, the platform list, and deletion impact preview for one disabled legacy platform. Record current platform count and current `codex` references.

- [ ] **Step 6: Delete one approved legacy platform and verify isolation**

Only after the user confirms the exact platform shown by preview, invoke DELETE. Verify the target platform and its historical references are gone, while users, balances, subscriptions, accounts, API keys, other platforms and the current `codex` records are unchanged.

