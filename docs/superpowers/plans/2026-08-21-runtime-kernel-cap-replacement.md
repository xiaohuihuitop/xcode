# Runtime Kernel CAP Replacement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task with verification checkpoints.

**Goal:** 将 XCode 的 AI 账号域和运行时执行收口到统一 Runtime Kernel，并在同一 Go 进程内用 CLIProxyAPI（CAP）整体替换当前 Sub2API Runtime。

**Architecture:** XCode 继续管理用户、API Key、Platform、套餐、余额、定价、扣费和用户 usage。Runtime Kernel 管理 AI 账号、凭据、quota、冷却、账号选择、重试、失败切换和上游执行；生产组合根只绑定一个 Runtime 实现。第一阶段通过 `RuntimeAIAccountStore` 包装现有账号存储，保持历史 AI 账号 ID 和 usage 引用不变；CAP 通过同进程 SDK 适配器接入。

**Tech Stack:** Go 1.26.6, `pkg/runtimebridge/v1`, PostgreSQL/Ent, Redis, CLIProxyAPI v7 SDK, 现有 GatewayRuntime/UsageSink。

---

## 文件结构与责任

### 新增

- `backend/pkg/runtimebridge/v1/accounts.go`：Runtime AI 账号引用、账号快照、账号规格和 quota 观察的纯 Go contract。
- `backend/pkg/runtimebridge/v1/control.go`：Runtime Control Plane 的请求/响应类型和校验。
- `backend/pkg/runtimebridge/v1/accounts_test.go`：账号引用和快照 contract 测试。
- `backend/pkg/runtimebridge/v1/control_test.go`：Control Plane 输入校验测试。
- `backend/internal/runtime/aiaccount/port.go`：Runtime AI 账号存储和 ID 映射端口。
- `backend/internal/runtime/aiaccount/xcode_store.go`：现有 XCode account repository 到 Runtime store 的适配器。
- `backend/internal/runtime/aiaccount/store_test.go`：账号 ID、凭据边界和快照映射测试。
- `backend/internal/runtime/cap/sdk_probe_test.go`：CAP v7 SDK 可消费性和最小构建探针；若 SDK 的 `internal` 依赖阻止外部导入，先停止后处理依赖方案。
- `backend/internal/runtime/cap/driver.go`：CAP 实现 `runtimebridge.Driver` 的执行适配器。
- `backend/internal/runtime/cap/control.go`：CAP auth manager 与 Runtime Control Plane 的账号管理适配器。
- `backend/internal/runtime/cap/account_mapping.go`：历史整数 AI 账号 ID 与 CAP 字符串 auth ID 的双向映射。
- `backend/internal/runtime/cap/stream.go`：CAP 流式 chunk 到现有 HTTPExchange/UsageFacts 的转换。
- `backend/internal/runtime/cap/driver_test.go`：CAP Driver 的请求、响应、错误和 usage contract 测试。
- `backend/internal/runtime/cap/account_mapping_test.go`：账号映射稳定性、冲突和未知 ID 测试。
- `backend/internal/runtime/control.go`：RuntimeControlPlane 的组合和审计端口。
- `backend/internal/runtime/control_test.go`：控制面鉴权、凭据脱敏和状态更新测试。

### 修改

- `backend/pkg/runtimebridge/v1/types.go`：确认 RuntimeRequest 不携带具体 AI 账号和产品资产；保留 v1 兼容字段但不再把 `RuntimeAdapter` 当作 Runtime 选择开关。
- `backend/internal/runtimebridge/port.go`、`backend/internal/runtimebridge/local.go`：增加 Control Plane 组合点，保持 Dispatch 的 exactly-once terminal 语义。
- `backend/internal/handler/sub2api_runtime_composition.go`：抽出产品网关公共组装函数，改为只绑定一个 Runtime Kernel；新增 CAP 组合实现。
- `backend/internal/handler/admin/account_handler.go`、`backend/internal/handler/admin/account_data.go`：AI 账号 CRUD/刷新/额度操作改为调用 RuntimeControlPlane，保留用户侧后台 API 语义和审计。
- `backend/internal/service/admin_account.go`、`backend/internal/service/account_service.go`：将直接调度、OAuth、quota 操作移到 Runtime 控制端口；保留用户权限、平台授权和审计校验。
- `backend/internal/handler/dto/account_platform_pool_mapper.go`：把 Runtime AI 账号快照映射成脱敏后台 DTO，禁止输出凭据。
- `backend/internal/service/openai_account_scheduler.go`、`backend/internal/service/openai_quota_service.go`、`backend/internal/service/antigravity_quota_fetcher.go`、`backend/internal/service/grok_quota_fetcher.go`：先建立调用边界和迁移兼容层，再逐项移除 ProductCore 对 AI 账号运行时状态的直接写入。
- `backend/go.mod`、`backend/go.sum`：只在 SDK 探针通过后加入 CLIProxyAPI v7 依赖。
- `docs/ARCHITECTURE.md`：更新用户账号/AI 账号边界、单 Runtime 绑定和 Control Plane 语义。
- `docs/memory/当前状态.md`：实施完成后记录 CAP 替换阶段、生产验证和剩余风险。

### 不应修改

- 用户、API Key、Subscription、余额和扣费 schema 的业务语义。
- `UsageSink` 的 exactly-once 规则。
- 外部用户 API 的认证和套餐扣费 contract。
- PostgreSQL/Redis 数据卷和历史 usage 数据。

---

### Task 1: 验证 CAP SDK 可嵌入性

**Files:**
- Create: `backend/internal/runtime/cap/sdk_probe_test.go`
- Modify: `backend/go.mod`, `backend/go.sum` only if the probe compiles

- [ ] **Step 1: Add the compile probe**

Create a test that imports the public CAP SDK packages and checks the builder and core manager symbols without starting a network listener:

```go
func TestCAPSDKIsEmbeddable(t *testing.T) {
    t.Helper()
    _ = cliproxy.NewBuilder
    _ = coreauth.NewManager
    _ = clipexec.Options{}
}
```

Use the exact module path from CAP's current `go.mod`; do not guess a v6 path when the module declares v7.

- [ ] **Step 2: Run the probe**

Run: `go test ./internal/runtime/cap -run TestCAPSDKIsEmbeddable -count=1`

Expected: PASS, or a compile error identifying an inaccessible `internal/config` dependency, unavailable public builder API, or incompatible Go/module version.

- [ ] **Step 3: Resolve only an evidenced SDK boundary**

If the probe fails because CAP's public SDK requires an inaccessible internal package, stop implementation and record the exact compiler error. The next design choice must be one of: CAP exposing a public config package, a local fork with that narrow export, or a different public SDK entry point. Do not copy CAP internals or add a replacement dependency silently.

- [ ] **Step 4: Commit the probe result**

Run: `git add backend/internal/runtime/cap/sdk_probe_test.go backend/go.mod backend/go.sum && git commit -m "test(runtime): verify CAP SDK embedding boundary"`

---

### Task 2: Add the public AI-account and Control Plane contract

**Files:**
- Create: `backend/pkg/runtimebridge/v1/accounts.go`
- Create: `backend/pkg/runtimebridge/v1/control.go`
- Create: `backend/pkg/runtimebridge/v1/accounts_test.go`
- Create: `backend/pkg/runtimebridge/v1/control_test.go`
- Modify: `backend/pkg/runtimebridge/v1/types.go`

- [ ] **Step 1: Define stable account references**

Add pure standard-library types:

```go
type RuntimeAccountRef struct {
    LegacyAccountID int64  `json:"legacy_account_id"`
    RuntimeID       string `json:"runtime_id"`
    Provider        string `json:"provider"`
}

type AccountSnapshot struct {
    Ref              RuntimeAccountRef `json:"ref"`
    Status           string             `json:"status"`
    SupportedModels  []string           `json:"supported_models,omitempty"`
    CooldownUntil    time.Time          `json:"cooldown_until,omitempty"`
    Quota            []QuotaWindow      `json:"quota,omitempty"`
    InFlight         int                `json:"in_flight"`
    ObservedAt       time.Time          `json:"observed_at"`
    LastErrorCategory string            `json:"last_error_category,omitempty"`
}
```

`RuntimeID` is opaque to XCode request routing. `LegacyAccountID` is retained solely for history and audit correlation.

- [ ] **Step 2: Define Control Plane requests**

Add `AccountQuery`, `AccountSpec`, `AccountCredentialRef`, `AccountOperationResult` and validation that rejects empty provider, zero legacy IDs where a legacy mapping is required, and any credential field in snapshot responses.

- [ ] **Step 3: Write failing validation tests**

Cover empty runtime IDs, duplicate legacy IDs, invalid status, negative quota values, and credential redaction.

- [ ] **Step 4: Run contract tests**

Run: `go test ./pkg/runtimebridge/v1 -run 'Test(RuntimeAccount|AccountControl)' -count=1`

Expected: PASS with no imports outside the standard library.

- [ ] **Step 5: Commit the contract**

Run: `git add backend/pkg/runtimebridge/v1 && git commit -m "feat(runtime): define AI account control contract"`

---

### Task 3: Introduce the Runtime AI Account Store boundary

**Files:**
- Create: `backend/internal/runtime/aiaccount/port.go`
- Create: `backend/internal/runtime/aiaccount/xcode_store.go`
- Create: `backend/internal/runtime/aiaccount/store_test.go`
- Modify: `backend/internal/repository/account_repo.go` only where an adapter method is required
- Modify: `backend/internal/service/account_service.go` to expose the adapter without leaking product entities

- [ ] **Step 1: Define the store ports**

Use separate durable and observation operations:

```go
type Store interface {
    List(ctx context.Context, query Query) ([]Descriptor, error)
    Get(ctx context.Context, ref v1.RuntimeAccountRef) (*Descriptor, error)
    Upsert(ctx context.Context, spec v1.AccountSpec) (*Descriptor, error)
    Delete(ctx context.Context, ref v1.RuntimeAccountRef) error
    SaveObservation(ctx context.Context, ref v1.RuntimeAccountRef, observation Observation) error
}
```

The adapter may use the existing account table during the first phase, but callers receive Runtime descriptors rather than `service.Account` or Ent entities.

- [ ] **Step 2: Add stable ID mapping**

Store the CAP auth ID in the existing runtime-owned account metadata only after validating uniqueness. A repeated sync of the same legacy account must return the same `RuntimeAccountRef`.

- [ ] **Step 3: Test preservation and isolation**

Test that listing AI accounts does not expose User/API Key/Subscription fields, that existing IDs round-trip, and that a missing or conflicting CAP auth ID fails closed.

- [ ] **Step 4: Run targeted repository tests**

Run: `go test ./internal/runtime/aiaccount ./internal/repository -run 'Test.*Account' -count=1`

Expected: PASS with existing account repository integration tests unchanged.

- [ ] **Step 5: Commit the boundary**

Run: `git add backend/internal/runtime/aiaccount backend/internal/repository/account_repo.go backend/internal/service/account_service.go && git commit -m "refactor(runtime): isolate AI account store"`

---

### Task 4: Implement CAP Runtime execution and account mapping

**Files:**
- Create: `backend/internal/runtime/cap/driver.go`
- Create: `backend/internal/runtime/cap/control.go`
- Create: `backend/internal/runtime/cap/account_mapping.go`
- Create: `backend/internal/runtime/cap/stream.go`
- Create: `backend/internal/runtime/cap/driver_test.go`
- Create: `backend/internal/runtime/cap/account_mapping_test.go`

- [ ] **Step 1: Implement account synchronization**

Load Runtime descriptors into CAP auth entries without exposing credentials to ProductCore. On every sync, preserve the `RuntimeAccountRef` mapping and reject duplicate CAP IDs or duplicate legacy IDs.

- [ ] **Step 2: Implement non-streaming dispatch**

Map `RuntimeRequest` to CAP's executor request, select by provider/model capability inside CAP, and convert CAP status, headers, body, upstream model and selected auth into `RuntimeResult`.

- [ ] **Step 3: Implement streaming dispatch**

Forward every CAP stream chunk to the existing exchange writer, record first-byte and duration timestamps, propagate client cancellation, and publish exactly one terminal usage/failure event.

- [ ] **Step 4: Normalize CAP errors**

Map unauthorized, quota, cooldown, transient upstream, unsupported model and client cancellation into `v1.RuntimeError`. Diagnostics must exclude credentials and complete upstream bodies.

- [ ] **Step 5: Add usage mapping**

Map CAP usage detail to `v1.UsageFacts`. If a provider does not return token usage, mark the fact as unknown rather than fabricating zero usage; the ProductCore billing policy must decide whether that endpoint is billable.

- [ ] **Step 6: Run CAP adapter tests**

Run: `go test ./internal/runtime/cap ./internal/runtimebridge -run 'Test(CAP|Runtime)' -count=1`

Expected: PASS for JSON, SSE, cancellation, one-terminal-event, ID mapping and sanitized errors.

- [ ] **Step 7: Commit the adapter**

Run: `git add backend/internal/runtime/cap && git commit -m "feat(runtime): add CAP kernel adapter"`

---

### Task 5: Add Runtime Control Plane and preserve the admin surface

**Files:**
- Create: `backend/internal/runtime/control.go`
- Create: `backend/internal/runtime/control_test.go`
- Modify: `backend/internal/handler/admin/account_handler.go`
- Modify: `backend/internal/handler/admin/account_data.go`
- Modify: `backend/internal/service/admin_account.go`
- Modify: `backend/internal/handler/dto/account_platform_pool_mapper.go`

- [ ] **Step 1: Compose the control facade**

The facade must authenticate the XCode admin request, authorize the operation, call `RuntimeControlPlane`, write an audit event, and return only `AccountSnapshot` data.

- [ ] **Step 2: Preserve existing user-facing admin API shapes**

Keep pagination, filters, account IDs and error status codes stable where they represent AI accounts. Replace direct repository/service calls with the facade; do not expose CAP auth IDs as the primary API identifier.

- [ ] **Step 3: Test credential redaction and audit**

Add tests proving list/get/update responses omit credentials, failed authorization causes no Runtime mutation, and successful create/update/delete/refresh operations produce an audit record.

- [ ] **Step 4: Run admin tests**

Run: `go test ./internal/handler/admin ./internal/handler/dto ./internal/runtime -run 'Test.*Account|Test.*Runtime' -count=1`

Expected: PASS with existing user admin tests unchanged.

- [ ] **Step 5: Commit the control plane**

Run: `git add backend/internal/runtime/control.go backend/internal/runtime/control_test.go backend/internal/handler/admin backend/internal/service/admin_account.go backend/internal/handler/dto/account_platform_pool_mapper.go && git commit -m "refactor(runtime): route AI account admin through control plane"`

---

### Task 6: Replace the production composition root with one CAP Runtime

**Files:**
- Modify: `backend/internal/handler/sub2api_runtime_composition.go`
- Modify: `backend/internal/runtimebridge/port.go`
- Modify: `backend/internal/runtimebridge/local.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/pkg/runtimebridge/v1/types.go` only if the legacy `RuntimeAdapter` hint is removed
- Create: `backend/internal/handler/cap_runtime_composition_test.go`

- [ ] **Step 1: Extract provider-neutral gateway construction**

Move ProductCore, UsageSink and ingress construction into a provider-neutral constructor. The constructor receives exactly one `runtimebridge.Driver` and one `RuntimeControlPlane`.

- [ ] **Step 2: Bind CAP as the sole Runtime**

Replace `runtimebridge.NewLocalRuntime(newSub2APILegacyDriver(adapter))` with the CAP Driver binding. Do not add a request-time switch, platform-level switch or silent fallback.

- [ ] **Step 3: Add composition assertions**

Test that the production gateway has exactly one Runtime binding, ProductCore cannot access AI account credentials, and the selected Runtime account reference reaches usage without changing user billing inputs.

- [ ] **Step 4: Run gateway regression tests**

Run: `go test ./internal/handler ./internal/runtimebridge ./internal/applicationgateway ./internal/productcore -run 'Test.*(Runtime|Gateway|Usage)' -count=1`

Expected: PASS with no Sub2API/CAP parallel dispatch path.

- [ ] **Step 5: Commit the composition change**

Run: `git add backend/internal/handler/sub2api_runtime_composition.go backend/internal/handler/wire.go backend/internal/handler/cap_runtime_composition_test.go backend/internal/runtimebridge backend/pkg/runtimebridge/v1/types.go && git commit -m "refactor(runtime): bind CAP as the single kernel"`

---

### Task 7: Run full conformance, migration rehearsal, and release verification

**Files:**
- Modify: `backend/pkg/runtimebridge/v1/conformance_test.go`
- Modify: `backend/internal/runtimebridge/conformance_test.go`
- Create: `docs/CAP_RUNTIME_MIGRATION.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/memory/当前状态.md`

- [ ] **Step 1: Expand contract conformance**

Run the same request matrix against a deterministic fake Sub2API Driver and CAP Driver. Compare status, stream chunks, terminal kind, usage facts, selected Runtime account reference and sanitized errors.

- [ ] **Step 2: Rehearse the existing AI account migration**

On a database copy, enumerate all current AI accounts, generate stable CAP auth IDs, verify one-to-one mappings, and confirm historical usage joins still resolve to the legacy account ID.

- [ ] **Step 3: Execute product regression**

Run: `go test ./...`

Expected: PASS for backend unit/integration tests. Also run the repository's frontend test, lint and production build commands from `docs/DEVELOPMENT_GUIDE.md`.

- [ ] **Step 4: Verify real endpoint behavior in staging**

Exercise Chat, Responses and SSE with `gpt-5.6-sol`; exercise GLM only after a valid upstream credential exists. Verify account selection, quota observations, error terminal events, usage rows, subscription deduction and no duplicate charge.

- [ ] **Step 5: Prepare the single-runtime release and rollback package**

Build the CAP-backed image and offline artifact, retain the pre-CAP Sub2API image and database backup for deployment rollback, and verify that rollback restores the same AI account ID mapping and usage behavior.

- [ ] **Step 6: Update project memory**

Record the active single Runtime, AI-account ownership, CAP validation results, unresolved provider limitations and the exact production release in `docs/memory/当前状态.md`.

- [ ] **Step 7: Commit docs and verification results**

Run: `git add backend/pkg/runtimebridge/v1 backend/internal/runtimebridge docs/CAP_RUNTIME_MIGRATION.md docs/ARCHITECTURE.md docs/memory/当前状态.md && git commit -m "docs(runtime): record CAP migration and verification"`

## Verification Gates

- CAP SDK probe must pass before adding or relying on the SDK dependency.
- AI account IDs must map one-to-one before any CAP request is allowed.
- Runtime Control Plane must redact credentials before admin responses.
- CAP Driver must pass terminal-event and usage conformance tests before composition-root replacement.
- Full product regression must pass before a Hong Kong deployment.
- Production must contain one Runtime binding; rollback is deployment-level only.

