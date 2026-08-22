# API Key 统一套餐授权与默认 Key Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 API Key 套餐授权收敛为“允许套餐/允许余额”两个开关，新建 Key 默认授权当前全部具体平台与全部套餐，并为所有注册路径提供幂等默认 Key。

**Architecture:** 在 `APIKey` 领域模型增加 `AllowAllSubscriptions`，平台仍以具体 ID 快照保存；计费解析在开关开启时查询用户全部有效订阅。AuthService 只依赖窄的默认 Key provisioner 接口，由 APIKeyService 提供实现，注册钩子失败 fail-open、登录时补建。

**Tech Stack:** Go、Ent、PostgreSQL migration、Vue 3、TypeScript、Vitest、Go `testing`/`testify`。

---

### Task 1: 套餐开关领域模型与认证缓存

**Files:**
- Modify: `backend/ent/schema/api_key.go`
- Modify: `backend/internal/service/api_key.go`
- Modify: `backend/internal/service/api_key_asset_permissions.go`
- Modify: `backend/internal/service/api_key_service.go`
- Modify: `backend/internal/service/api_key_auth_cache.go`
- Modify: `backend/internal/service/api_key_auth_cache_impl.go`
- Test: `backend/internal/service/api_key_asset_permissions_test.go`
- Test: `backend/internal/service/api_key_auth_cache_test.go` (或现有同主题测试文件)

- [ ] **Step 1: Write failing tests** for `allow_all_subscriptions` default true on create, independent validation with `allow_balance`, update preservation, and cache snapshot round-trip.
- [ ] **Step 2: Run** `go test ./internal/service -run 'Test(APIKeyAssetPermissions|APIKeyAuth)' -count=1`; confirm failures reference the missing field/behavior.
- [ ] **Step 3: Add** Ent field `allow_all_subscriptions` with default false, add the service/domain field and request fields, and change permission normalization/validation so create defaults are `true/true` while update preserves omitted values.
- [ ] **Step 4: Extend** auth snapshot serialization/deserialization and increment its version so old snapshots decode with `false` and permission changes cannot reuse stale authorization.
- [ ] **Step 5: Run** the same service tests and the existing API key service tests; confirm green before moving on.

### Task 2: 全部套餐计费解析与仓储持久化

**Files:**
- Modify: `backend/internal/service/api_key_asset_resolver.go`
- Modify: `backend/internal/service/api_key_service.go`
- Modify: `backend/internal/repository/api_key_repo.go`
- Modify: `backend/internal/repository/user_subscription_repo.go` (only if an all-active-subscriptions method is missing)
- Test: `backend/internal/service/api_key_asset_resolver_test.go`
- Test: `backend/internal/service/api_key_asset_permissions_test.go`

- [ ] **Step 1: Write failing resolver tests** for all-subscriptions selecting a newly purchased plan, exhausted candidates falling back to balance, and balance-only keys not querying subscriptions.
- [ ] **Step 2: Run** `go test ./internal/service -run 'Test.*BillingAsset|Test.*Subscription' -count=1`; confirm the all-subscriptions cases fail under current plan-ID-only logic.
- [ ] **Step 3: Implement** a lister path that returns all active user subscriptions, reuse `firstUsableSubscriptionAsset`, and keep the old explicit plan-ID path only for rolling compatibility.
- [ ] **Step 4: Update** API key create/update persistence and `ReplaceAssetPermissions` so only `allow_all_subscriptions`/`allow_balance` are authoritative for new writes while platform links remain atomically diff-updated; clear legacy plan links when the new contract is written.
- [ ] **Step 5: Run** resolver, repository, and API key service tests; inspect SQL arguments for parameterization and verify cache invalidation remains on permission updates.

### Task 3: ProductCore migration and Ent generated code

**Files:**
- Create: `backend/migrations/9000_api_key_allow_all_subscriptions.sql`
- Create: `backend/migrations/9000_api_key_allow_all_subscriptions_test.go` (or repository migration test location matching existing conventions)
- Modify: generated Ent files under `backend/ent/` using the repository generation command

- [ ] **Step 1: Write migration tests** covering default false, backfill true for keys with legacy plan links, unchanged pure-balance keys, legacy link cleanup, and repeat execution.
- [ ] **Step 2: Run** the focused migration test; confirm it fails because migration `9000` and the column do not exist.
- [ ] **Step 3: Add** an idempotent transactional migration using `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`, update keys with legacy links, then delete legacy links; do not touch users, subscriptions, balances, usage logs, or API key rows.
- [ ] **Step 4: Regenerate Ent code** with the repository’s documented generation command and compile the backend.
- [ ] **Step 5: Run** the focused migration test plus `go test ./ent/... ./internal/repository/...`; confirm the migration checksum and generated field access compile.

### Task 4: 注册默认 Key 与首次登录补建

**Files:**
- Modify: `backend/internal/service/auth_service.go`
- Modify: `backend/internal/service/auth_email_oauth_auto.go` only where a non-central path bypasses the bootstrap hook
- Modify: `backend/internal/service/auth_oauth_email_flow.go` if finalization currently bypasses the hook
- Modify: `backend/internal/service/wire.go`
- Test: `backend/internal/service/auth_service_test.go` or a new focused default-key test file

- [ ] **Step 1: Write failing tests** with a fake `DefaultAPIKeyProvisioner` for email and OAuth bootstrap, proving creation failure does not fail registration, repeated bootstrap is idempotent, and successful login retries a missing key.
- [ ] **Step 2: Run** the focused auth tests; confirm AuthService has no provisioner and the expected calls do not occur.
- [ ] **Step 3: Define** `DefaultAPIKeyProvisioner` with a single idempotent `EnsureDefaultAPIKey(ctx, userID)` method; inject it into AuthService through the existing constructor/wire path without importing concrete APIKeyService types into auth logic.
- [ ] **Step 4: Call** the provisioner from `postAuthUserBootstrap` and from the successful-login path, log failures with user ID and error, and preserve current registration/OAuth return behavior.
- [ ] **Step 5: Implement** APIKeyService’s default provisioner using current platform discovery and default `allow_all_subscriptions=true`, `allow_balance=true`; treat an existing active key as success and handle unique conflicts as idempotent success.
- [ ] **Step 6: Run** focused auth and API key tests plus all existing OAuth flow tests.

### Task 5: 前端 Key 表单和 API 类型

**Files:**
- Modify: `frontend/src/components/keys/KeyAssetPermissionsForm.vue`
- Modify: `frontend/src/components/keys/__tests__/KeyAssetPermissionsForm.spec.ts`
- Modify: `frontend/src/views/user/KeysView.vue`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/i18n/locales/zh/dashboard.ts`
- Modify: `frontend/src/i18n/locales/en/dashboard.ts`

- [ ] **Step 1: Write failing component tests** for no per-plan checkboxes, two billing toggles, create defaults, and edit preservation of platform IDs.
- [ ] **Step 2: Run** `pnpm vitest run frontend/src/components/keys/__tests__/KeyAssetPermissionsForm.spec.ts`; confirm current per-plan UI fails the new assertions.
- [ ] **Step 3: Replace** the plan list with `allowAllSubscriptions` and `allowBalance` props/events; keep platform checkboxes as concrete platform IDs and initialize create forms with all currently loaded platform IDs.
- [ ] **Step 4: Update** Key view payloads, display summaries, validation, TypeScript request/response types, and localized labels to send `allow_all_subscriptions` and no `subscription_plan_ids` for new writes.
- [ ] **Step 5: Run** the focused Vitest file, related user-view tests, and production typecheck/build; confirm no old per-plan event or field remains in the user Key workflow.

### Task 6: 集成回归、文档状态和交付检查

**Files:**
- Modify: `docs/memory/当前状态.md`
- Add decision record only if implementation introduces a durable rule not already captured: `docs/memory/决策/2026-08-22-API-Key统一套餐授权与默认Key.md`

- [ ] **Step 1: Run** `go test ./internal/service ./internal/repository ./ent/... -count=1` and the focused migration tests; record any real failures and fix them with tests first.
- [ ] **Step 2: Run** `pnpm vitest run frontend/src/components/keys frontend/src/views/user` and the repository frontend build/typecheck command.
- [ ] **Step 3: Run** `git diff --check` and inspect the complete diff for accidental schema/data changes, leaked credentials, and unintended platform expansion.
- [ ] **Step 4: Update** `docs/memory/当前状态.md` with the implementation phase and verified results; preserve the user’s existing production notes.
- [ ] **Step 5: Commit** application changes with `feat(api-key): 统一套餐授权并创建注册默认 key` only after all directly related tests pass; do not include unrelated working-tree changes.
