# 平台与资产解耦 Implementation Plan

> **历史计划，不再执行：** 本文件对应 `my2-v0.2.x` 第一阶段。后续不得继续执行其中的
> 双读兼容、Legacy Key 回退或全局唯一模型平台步骤；当前设计以
> `docs/superpowers/specs/2026-08-06-platform-pool-account-adapter-design.md` 为准。

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **实施进度（2026-08-05）：** 本计划是最初的执行蓝图。平台、迁移、API Key
> 资产权限、套餐优先资产解析、网关归属、账号表单、平台管理与使用记录的实现已进入
> `my2.0` 工作区；当前正在执行最终回归与人工验收前检查。未逐项回填的原始复选框
> 不代表功能未实现，实际状态和已运行命令以 `docs/memory/当前状态.md` 为准。

**Goal:** 在 `my2.0` 预发布分支把账号调度的平台领域与套餐、余额资产领域拆开，使 API Key 独立授权平台、套餐和余额，并保持既有模型定价与历史数据安全兼容。

**Architecture:** 新增 `Platform` 和平台模型规则作为账号池、模型解析、端点能力的唯一配置；套餐、订阅实例和余额不再以 `Group` 决定可调度账号。通过 `api_key_platforms`、`api_key_subscription_plans`、`allow_balance` 描述 API Key 权限；网关先解析唯一平台，再从该平台账号池调度，最后按套餐优先、余额兜底选择资产并套用对应倍率。首次发布仅扩展结构和双读兼容，旧 `groups`、`account_groups`、`group_id` 保留一个完整预发布周期，且任何不唯一映射都必须人工处理后才能启用新路径。

**Tech Stack:** Go 1.26、Ent、PostgreSQL、Gin、Redis/Ristretto 缓存、Vue 3、TypeScript、Vitest、Go testing。

---

## 文件与职责

| 文件 | 职责 |
| --- | --- |
| `.gitignore` | 将仓库文档纳入 Git 管理。 |
| `docs/superpowers/specs/2026-08-04-platform-assets-decoupling-design.md` | 已确认的领域设计与迁移边界。 |
| `docs/memory/决策/2026-08-04-平台与资产解耦.md` | 长期产品决策。 |
| `backend/migrations/194_platform_assets_expand.sql` | 仅新增表、列、索引和默认设置；不删除旧数据。 |
| `backend/ent/schema/platform.go` | 平台账号池配置实体。 |
| `backend/ent/schema/platform_model_rule.go` | 客户端模型名到唯一平台、上游模型的映射。 |
| `backend/ent/schema/api_key.go` | API Key 的余额开关和平台、套餐多对多边。 |
| `backend/ent/schema/subscription_plan.go`、`backend/ent/schema/user_subscription.go` | 将遗留 `group_id` 明确为兼容字段，允许新记录不写入它。 |
| `backend/ent/schema/usage_log.go` | 记录真实平台和资产来源。 |
| `backend/internal/service/platform*.go` | 平台模型规则校验、解析和预检报告。 |
| `backend/internal/repository/platform_repo.go` | 平台、模型规则和 API Key 新权限的持久化实现。 |
| `backend/internal/service/api_key*.go` | API Key 新 DTO、权限校验、缓存失效和旧权限受限兼容。 |
| `backend/internal/service/subscription_candidates.go`、`billing_multiplier_selection.go` | 套餐候选统一排序、资产倍率与余额兜底。 |
| `backend/internal/service/gateway*_scheduling.go`、`gateway*_usage.go` | 先平台路由再调度，写入实际资产归属。 |
| `backend/internal/handler/admin/platform_handler.go`、`backend/internal/server/routes/admin.go` | 平台配置、预检和显式启用新路径的管理 API。 |
| `frontend/src/views/admin/PlatformsView.vue` | 平台、模型规则和迁移报告管理页面。 |
| `frontend/src/components/account/CreateAccountModal.vue`、`EditAccountModal.vue` | 账号表单不再显示或写入分组。 |
| `frontend/src/components/ApiKeyCreate.vue`、`frontend/src/components/ApiKeyEdit.vue` | 平台多选、套餐多选、余额开关；新 Key 默认允许余额。 |
| `frontend/src/types/index.ts`、`frontend/src/api/*` | 新平台与 API Key 权限传输类型。 |

## Task 1: 文档纳管与实施边界

**Files:**
- Modify: `.gitignore`
- Modify: `docs/superpowers/specs/2026-08-04-platform-assets-decoupling-design.md`
- Modify: `docs/memory/当前状态.md`
- Modify: `docs/memory/项目概览.md`
- Create: `docs/superpowers/plans/2026-08-04-platform-assets-decoupling.md`

- [x] **Step 1: 取消对 `docs/` 的忽略并确认既有文档可见**

```gitignore
# 删除这整段，不再对 docs 建立例外白名单：
docs/*
!docs/PAYMENT.md
!docs/PAYMENT_CN.md
!docs/ADMIN_PAYMENT_INTEGRATION_API.md
!docs/ASYNC_IMAGE_TASKS.md
!docs/legal/
!docs/legal/*.md
```

- [ ] **Step 2: 检查文档纳管范围**

Run: `git status --short -- docs .gitignore`

Expected: `docs/` 下此前忽略的计划、规格、决策均作为未跟踪文件出现；不得包含 `backend/entgen_tmp/` 或 `my2.0.drawio`。

- [ ] **Step 3: 更新已确认的规格和当前状态**

将规格状态改为“已确认，进入实施”，并在当前状态中写入以下固定边界：

```markdown
- 平台负责账号池、模型规则、端点能力和调度；套餐、订阅实例、余额仅负责资产扣费。
- API Key 分别授权平台、套餐以及余额；套餐优先按 expires_at、created_at 消耗，余额仅在允许且套餐不可用时兜底。
- 旧 groups 及其关联仅作预发布兼容，迁移不得自动猜测平台、套餐权限或删除数据。
```

- [ ] **Step 4: 校验文档格式和空白差异**

Run: `git diff --check -- .gitignore docs`

Expected: Exit code 0。

- [ ] **Step 5: 提交文档与忽略规则**

```powershell
git add .gitignore docs
git commit -m "docs(架构): 记录平台资产解耦方案"
```

Expected: 只提交 `.gitignore` 与 `docs/`；不提交用户的 `.drawio` 文件或 `backend/entgen_tmp/`。

## Task 2: 只读预检报告和新路径开关

**Files:**
- Create: `backend/internal/service/platform_migration_preflight.go`
- Create: `backend/internal/service/platform_migration_preflight_test.go`
- Modify: `backend/internal/service/setting_service.go`
- Modify: `backend/internal/service/setting.go`
- Modify: `backend/internal/handler/admin/platform_handler.go`
- Modify: `backend/internal/server/routes/admin.go`

- [ ] **Step 1: 编写预检失败测试，覆盖不允许自动猜测的典型冲突**

```go
func TestPlatformMigrationPreflightReportsAmbiguities(t *testing.T) {
	result := collectPlatformMigrationConflicts(legacySnapshot{
		ModelRules: []legacyModelRule{
			{GroupID: 10, Platform: PlatformOpenAI, Pattern: "gpt-*"},
			{GroupID: 11, Platform: PlatformGrok, Pattern: "gpt-4o"},
		},
		Accounts: []legacyAccountGroups{{AccountID: 7, Platforms: []string{PlatformOpenAI, PlatformGemini}}},
		GroupPlanIDs: map[int64][]int64{10: {100, 101}, 11: {200}},
		APIKeys: []legacyAPIKey{{ID: 3, AllowedGroupIDs: []int64{10}}},
	})
	require.Contains(t, result.Conflicts, "model_pattern_overlap")
	require.Contains(t, result.Conflicts, "account_platform_ambiguity")
	require.Contains(t, result.Conflicts, "api_key_plan_ambiguity")
}
```

- [ ] **Step 2: 运行预检测试确认在实现前失败**

Run: `go test -tags=unit ./internal/service -run TestPlatformMigrationPreflightReportsAmbiguities -count=1`

Expected: FAIL，提示 `collectPlatformMigrationConflicts` 未定义。

- [ ] **Step 3: 实现只读预检结构和设置键**

```go
const (
	SettingKeyPlatformAssetsV2Enabled       = "platform_assets_v2_enabled"
	SettingKeyGlobalBalanceRateMultiplier   = "global_balance_rate_multiplier"
)

type PlatformMigrationPreflight struct {
	Ready     bool                        `json:"ready"`
	Conflicts []PlatformMigrationConflict `json:"conflicts"`
}

type PlatformMigrationConflict struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Detail  string `json:"detail"`
}
```

`POST /api/v1/admin/platforms/preflight` 只能读取和返回报告；`PUT /api/v1/admin/platforms/migration-mode` 必须在 `Ready == true` 时才允许将 `platform_assets_v2_enabled` 写为 `true`。
多个旧 `group_ids` 可明确映射到多个平台时不是冲突，因为新 API Key 允许多平台；只有旧分组映射到零个或多个套餐、无法判定余额权限、没有平台映射或存在复合平台时才报告 Key 迁移歧义。

- [ ] **Step 4: 运行预检测试确认通过**

Run: `go test -tags=unit ./internal/service -run TestPlatformMigrationPreflightReportsAmbiguities -count=1`

Expected: PASS。

- [ ] **Step 5: 提交预检与开关**

```powershell
git add backend/internal/service/platform_migration_preflight.go backend/internal/service/platform_migration_preflight_test.go backend/internal/service/setting.go backend/internal/service/setting_service.go backend/internal/handler/admin/platform_handler.go backend/internal/server/routes/admin.go
git commit -m "feat(迁移): 增加平台资产预检开关"
```

## Task 3: 扩展数据库和 Ent 模型，不删除旧关系

**Files:**
- Create: `backend/migrations/194_platform_assets_expand.sql`
- Create: `backend/migrations/194_platform_assets_expand_test.go`
- Create: `backend/ent/schema/platform.go`
- Create: `backend/ent/schema/platform_model_rule.go`
- Modify: `backend/ent/schema/api_key.go`
- Modify: `backend/ent/schema/subscription_plan.go`
- Modify: `backend/ent/schema/user_subscription.go`
- Modify: `backend/ent/schema/usage_log.go`
- Modify: generated `backend/ent/**`

- [ ] **Step 1: 编写迁移结构测试**

```go
func TestPlatformAssetsMigrationContainsForwardOnlyExpansion(t *testing.T) {
	sql := readMigration(t, "194_platform_assets_expand.sql")
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS platforms",
		"CREATE TABLE IF NOT EXISTS platform_model_rules",
		"CREATE TABLE IF NOT EXISTS api_key_platforms",
		"CREATE TABLE IF NOT EXISTS api_key_subscription_plans",
		"ADD COLUMN IF NOT EXISTS allow_balance",
		"ADD COLUMN IF NOT EXISTS platform_id",
		"ADD COLUMN IF NOT EXISTS billing_source_type",
	} {
		require.Contains(t, sql, fragment)
	}
	require.NotContains(t, sql, "DROP TABLE")
}
```

- [ ] **Step 2: 运行迁移结构测试确认在实现前失败**

Run: `go test ./migrations -run TestPlatformAssetsMigrationContainsForwardOnlyExpansion -count=1`

Expected: FAIL，提示迁移文件不存在。

- [ ] **Step 3: 新增前向迁移**

```sql
CREATE TABLE IF NOT EXISTS platforms (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(64) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    endpoint_capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
    scheduling_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    legacy_group_id BIGINT UNIQUE REFERENCES groups(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS platform_model_rules (
    id BIGSERIAL PRIMARY KEY,
    platform_id BIGINT NOT NULL REFERENCES platforms(id) ON DELETE CASCADE,
    model_pattern VARCHAR(255) NOT NULL,
    upstream_model VARCHAR(255) NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_platform_model_rules_platform_pattern_lower
    ON platform_model_rules(platform_id, lower(model_pattern));

CREATE TABLE IF NOT EXISTS api_key_platforms (
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    platform_id BIGINT NOT NULL REFERENCES platforms(id) ON DELETE CASCADE,
    PRIMARY KEY (api_key_id, platform_id)
);
CREATE TABLE IF NOT EXISTS api_key_subscription_plans (
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    subscription_plan_id BIGINT NOT NULL REFERENCES subscription_plans(id) ON DELETE CASCADE,
    PRIMARY KEY (api_key_id, subscription_plan_id)
);

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS allow_balance BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE subscription_plans ALTER COLUMN group_id DROP NOT NULL;
ALTER TABLE user_subscriptions ALTER COLUMN group_id DROP NOT NULL;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS platform_id BIGINT REFERENCES platforms(id) ON DELETE SET NULL;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS billing_source_type VARCHAR(20) NOT NULL DEFAULT 'legacy_group';
CREATE INDEX IF NOT EXISTS idx_usage_logs_platform_id_created_at ON usage_logs(platform_id, created_at DESC);
INSERT INTO settings(key, value) VALUES
    ('platform_assets_v2_enabled', 'false'),
    ('global_balance_rate_multiplier', '1.0')
ON CONFLICT (key) DO NOTHING;
```

迁移不得插入 `platforms`、API Key 权限或套餐权限数据；这些均要经预检并由管理员显式确认。

- [ ] **Step 4: 建立 Ent 关系和兼容字段**

```go
// APIKey.Fields
field.Bool("allow_balance").Default(true),

// APIKey.Edges
edge.To("platforms", Platform.Type).
	StorageKey(edge.Table("api_key_platforms"), edge.Columns("api_key_id", "platform_id")),
edge.To("subscription_plans", SubscriptionPlan.Type).
	StorageKey(edge.Table("api_key_subscription_plans"), edge.Columns("api_key_id", "subscription_plan_id")),
```

`SubscriptionPlan.group_id` 和 `UserSubscription.group_id` 改为 `Optional().Nillable()`，但保留字段名、索引和旧边，供旧路径只读兼容。`UsageLog` 增加可空 `platform_id` 边及 `billing_source_type` 字段，默认值 `legacy_group`。

- [ ] **Step 5: 生成 Ent 并运行迁移结构测试**

Run: `go generate ./ent`

Expected: Exit code 0，生成 `ent/platform*`、`ent/platformmodelrule*` 及 API Key 新边代码。

Run: `go test ./migrations -run TestPlatformAssetsMigrationContainsForwardOnlyExpansion -count=1`

Expected: PASS。

- [ ] **Step 6: 提交数据模型**

```powershell
git add backend/migrations/194_platform_assets_expand.sql backend/migrations/194_platform_assets_expand_test.go backend/ent
git commit -m "feat(数据): 扩展平台资产领域模型"
```

## Task 4: 平台配置、模型唯一解析和管理接口

**Files:**
- Create: `backend/internal/service/platform.go`
- Create: `backend/internal/service/platform_service.go`
- Create: `backend/internal/service/platform_model_rules.go`
- Create: `backend/internal/service/platform_model_rules_test.go`
- Create: `backend/internal/repository/platform_repo.go`
- Create: `backend/internal/repository/platform_repo_integration_test.go`
- Create: `backend/internal/handler/admin/platform_handler.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/cmd/server/wire.go`
- Modify: generated `backend/cmd/server/wire_gen.go`

- [ ] **Step 1: 编写模型规则冲突和解析的失败测试**

```go
func TestValidatePlatformModelRulesRejectsCrossPlatformOverlap(t *testing.T) {
	err := validatePlatformModelRules([]PlatformModelRule{
		{PlatformID: 1, ModelPattern: "gpt-*", Enabled: true},
		{PlatformID: 2, ModelPattern: "gpt-4o", Enabled: true},
	})
	require.ErrorContains(t, err, "overlaps")
}

func TestResolvePlatformModelUsesExactBeforeSuffixWildcard(t *testing.T) {
	resolver := newPlatformModelResolver([]PlatformModelRule{
		{PlatformID: 1, ModelPattern: "gpt-*", UpstreamModel: "", Enabled: true},
		{PlatformID: 2, ModelPattern: "gpt-4o", UpstreamModel: "gpt-4o-2024-08-06", Enabled: true},
	})
	got, err := resolver.Resolve("gpt-4o")
	require.NoError(t, err)
	require.Equal(t, int64(2), got.PlatformID)
	require.Equal(t, "gpt-4o-2024-08-06", got.UpstreamModel)
}
```

- [ ] **Step 2: 运行测试确认在实现前失败**

Run: `go test -tags=unit ./internal/service -run "TestValidatePlatformModelRulesRejectsCrossPlatformOverlap|TestResolvePlatformModelUsesExactBeforeSuffixWildcard" -count=1`

Expected: FAIL，提示平台类型或解析器不存在。

- [ ] **Step 3: 实现平台服务的明确输入输出**

```go
type Platform struct {
	ID                   int64
	Code                 string
	Name                 string
	Status               string
	EndpointCapabilities map[string]bool
	SchedulingConfig     map[string]any
	LegacyGroupID        *int64
	ModelRules           []PlatformModelRule
}

type ResolvedPlatformModel struct {
	PlatformID    int64
	PlatformCode  string
	RequestedModel string
	UpstreamModel string
}

func (s *PlatformService) ResolveModel(ctx context.Context, requestedModel string) (*ResolvedPlatformModel, error)
func (s *PlatformService) ValidateCandidate(ctx context.Context, platform *Platform) error
```

规则只接受精确名称或单个末尾 `*` 通配符；同一平台内不允许大小写重复，跨启用平台不允许精确与通配符的交集。不存在匹配返回 `ErrPlatformModelNotFound`，多匹配返回 `ErrPlatformModelAmbiguous`，两者均不得回退到其他平台。

- [ ] **Step 4: 增加平台管理路由和 Wire 注入**

```go
platforms := admin.Group("/platforms")
{
	platforms.GET("", h.Admin.Platform.List)
	platforms.POST("", h.Admin.Platform.Create)
	platforms.GET("/:id", h.Admin.Platform.GetByID)
	platforms.PUT("/:id", h.Admin.Platform.Update)
	platforms.DELETE("/:id", h.Admin.Platform.Delete)
	platforms.POST("/preflight", h.Admin.Platform.Preflight)
	platforms.PUT("/migration-mode", h.Admin.Platform.UpdateMigrationMode)
}
```

- [ ] **Step 5: 运行服务、仓储和路由测试**

Run: `go test -tags=unit ./internal/service -run "TestValidatePlatformModelRulesRejectsCrossPlatformOverlap|TestResolvePlatformModelUsesExactBeforeSuffixWildcard" -count=1`

Expected: PASS。

Run: `go test -tags=integration ./internal/repository -run TestPlatformRepository -count=1`

Expected: PASS；若本机未配置集成数据库，记录为未执行，不以成功替代。

- [ ] **Step 6: 提交平台配置层**

```powershell
git add backend/internal/service/platform*.go backend/internal/repository/platform_repo*.go backend/internal/handler/admin/platform_handler.go backend/internal/server/routes/admin.go backend/cmd/server/wire.go backend/cmd/server/wire_gen.go
git commit -m "feat(平台): 增加账号池与模型规则管理"
```

## Task 5: API Key 的平台、套餐与余额权限

**Files:**
- Modify: `backend/internal/service/api_key.go`
- Modify: `backend/internal/service/api_key_service.go`
- Modify: `backend/internal/service/api_key_group_selection.go`
- Create: `backend/internal/service/api_key_asset_permissions.go`
- Create: `backend/internal/service/api_key_asset_permissions_test.go`
- Modify: `backend/internal/repository/api_key_repo.go`
- Modify: `backend/internal/handler/api_key_handler.go`
- Modify: `backend/internal/handler/dto/types.go`
- Modify: `backend/internal/handler/dto/mappers.go`
- Modify: `backend/internal/handler/admin/api_key_handler.go`

- [ ] **Step 1: 编写新 Key 默认余额、最小授权和旧数据兼容的失败测试**

```go
func TestValidateAPIKeyAssetPermissions(t *testing.T) {
	require.ErrorIs(t, ValidateAPIKeyAssetPermissions(APIKeyAssetPermissions{}), ErrAPIKeyPlatformRequired)
	require.ErrorIs(t, ValidateAPIKeyAssetPermissions(APIKeyAssetPermissions{PlatformIDs: []int64{9}}), ErrAPIKeyBillingSourceRequired)
	require.NoError(t, ValidateAPIKeyAssetPermissions(APIKeyAssetPermissions{
		PlatformIDs: []int64{9}, AllowBalance: true,
	}))
}

func TestNewAPIKeyDefaultsToBalanceAllowed(t *testing.T) {
	key := newAPIKeyFromCreateRequest(CreateAPIKeyRequest{Name: "default-balance"})
	require.True(t, key.AllowBalance)
}
```

- [ ] **Step 2: 运行测试确认在实现前失败**

Run: `go test -tags=unit ./internal/service -run "TestValidateAPIKeyAssetPermissions|TestNewAPIKeyDefaultsToBalanceAllowed" -count=1`

Expected: FAIL，提示 `APIKeyAssetPermissions` 不存在。

- [ ] **Step 3: 为领域对象和 DTO 增加新字段**

```go
type APIKeyAssetPermissions struct {
	PlatformIDs         []int64 `json:"platform_ids"`
	SubscriptionPlanIDs []int64 `json:"subscription_plan_ids"`
	AllowBalance        bool    `json:"allow_balance"`
}

type APIKey struct {
	// 保留 GroupID 与 AllowedGroupIDs，仅用于 migration-mode=false 的旧读路径。
	AllowedPlatformIDs         []int64
	AllowedPlatforms           []Platform
	AllowedSubscriptionPlanIDs []int64
	AllowBalance               bool
}
```

`CreateAPIKeyRequest` 接受 `platform_ids`、`subscription_plan_ids`、可空 `allow_balance`；未提供 `allow_balance` 时写入 `true`。`UpdateAPIKeyRequest` 的 `allow_balance` 使用 `*bool` 保留“未修改”和“明确关闭”的区别。启用新路径后，`group_id`、`group_ids` 只能作为响应中的兼容字段，禁止写入。

- [ ] **Step 4: 实现仓储原子替换和缓存失效**

```go
func (r *apiKeyRepository) ReplaceAssetPermissions(
	ctx context.Context,
	apiKeyID int64,
	permissions service.APIKeyAssetPermissions,
) error {
	return r.withTx(ctx, func(tx *ent.Tx) error {
		if err := tx.APIKey.UpdateOneID(apiKeyID).SetAllowBalance(permissions.AllowBalance).Exec(ctx); err != nil { return err }
		if err := tx.APIKey.UpdateOneID(apiKeyID).ClearPlatforms().AddPlatformIDs(permissions.PlatformIDs...).Exec(ctx); err != nil { return err }
		return tx.APIKey.UpdateOneID(apiKeyID).ClearSubscriptionPlans().AddSubscriptionPlanIDs(permissions.SubscriptionPlanIDs...).Exec(ctx)
	})
}
```

写入后调用既有 API Key auth-cache invalidator；缓存副本必须深拷贝两个 ID 切片，避免权限变更后残留旧数组。

- [ ] **Step 5: 运行 API Key 定向测试**

Run: `go test -tags=unit ./internal/service -run "TestValidateAPIKeyAssetPermissions|TestNewAPIKeyDefaultsToBalanceAllowed|TestAPIKey.*Permission" -count=1`

Expected: PASS。

Run: `go test -tags=unit ./internal/handler/dto -run TestAPIKey -count=1`

Expected: PASS。

- [ ] **Step 6: 提交 API Key 新授权层**

```powershell
git add backend/internal/service/api_key*.go backend/internal/repository/api_key_repo.go backend/internal/handler/api_key_handler.go backend/internal/handler/dto backend/internal/handler/admin/api_key_handler.go
git commit -m "feat(密钥): 支持平台套餐余额独立授权"
```

## Task 6: 套餐优先资产选择与全局余额倍率

**Files:**
- Modify: `backend/internal/service/user_subscription_port.go`
- Modify: `backend/internal/repository/user_subscription_active_repo.go`
- Modify: `backend/internal/service/subscription_candidates.go`
- Modify: `backend/internal/service/api_key_billing_group_resolver.go`
- Create: `backend/internal/service/api_key_asset_resolver.go`
- Create: `backend/internal/service/api_key_asset_resolver_test.go`
- Modify: `backend/internal/service/billing_multiplier_selection.go`
- Create: `backend/internal/service/global_balance_rate.go`
- Create: `backend/internal/service/global_balance_rate_test.go`

- [ ] **Step 1: 写出跨套餐统一排序和余额兜底的失败测试**

```go
func TestResolveBillingAssetUsesEarliestUsableSubscriptionThenBalance(t *testing.T) {
	resolver := newTestAssetResolver(
		[]UserSubscription{
			activeSubscription(2, 20, time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)),
			activeSubscription(1, 10, time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)),
		},
	)
	asset, err := resolver.Resolve(context.Background(), apiKeyWithAssets([]int64{10, 20}, true), 7)
	require.NoError(t, err)
	require.Equal(t, BillingSourceSubscription, asset.Source)
	require.Equal(t, int64(1), *asset.SubscriptionID)
}

func TestResolveBillingAssetSkipsExhaustedSubscriptionAndHonorsBalanceFlag(t *testing.T) {
	resolver := newTestAssetResolver([]UserSubscription{exhaustedSubscription(10)})
	_, err := resolver.Resolve(context.Background(), apiKeyWithAssets([]int64{10}, false), 7)
	require.ErrorIs(t, err, ErrNoUsableBillingSource)
}
```

- [ ] **Step 2: 运行资产选择测试确认在实现前失败**

Run: `go test -tags=unit ./internal/service -run "TestResolveBillingAsset" -count=1`

Expected: FAIL，提示资产解析器不存在。

- [ ] **Step 3: 让仓储按套餐而非 Group 查询候选订阅**

```go
type ActiveUserSubscriptionPlanLister interface {
	ListActiveByUserIDAndPlanIDs(ctx context.Context, userID int64, planIDs []int64) ([]UserSubscription, error)
}

// 查询条件固定为 active、未软删、expires_at > now、subscription_plan_id IN (...)
// 排序固定为 expires_at ASC, created_at ASC, id ASC。
```

额度检查沿用 `ValidateAndCheckLimits` 与窗口维护；候选不可用（过期、暂停、日/周/月耗尽）只跳过该实例，不中断后续套餐候选。

- [ ] **Step 4: 引入唯一的资产解析结果和倍率规则**

```go
const (
	BillingSourceSubscription = "subscription"
	BillingSourceBalance      = "balance"
)

type ResolvedBillingAsset struct {
	Source         string
	SubscriptionID *int64
	PlanID         *int64
	RateMultiplier float64
}

func (s *APIKeyService) ResolveBillingAssetForRequest(
	ctx context.Context,
	apiKey *APIKey,
	subscriptions apiKeySubscriptionResolver,
	skipBilling bool,
) (*ResolvedBillingAsset, error)
```

套餐请求仅使用 `UserSubscription.RateMultiplierSnapshot`；余额请求仅使用 `GlobalBalanceRateProvider.Get(ctx)`；这两个值不得相乘。`ModelPricingResolver` 的 `PricingInput.GroupID` 只从 `Platform.LegacyGroupID` 传入，因此既有 LiteLLM、fallback、渠道模型自定义价格不被改写。

- [ ] **Step 5: 运行资产与倍率回归测试**

Run: `go test -tags=unit ./internal/service -run "TestResolveBillingAsset|Test.*BalanceRate|Test.*Subscription.*Multiplier" -count=1`

Expected: PASS，且套餐倍率与余额倍率互不叠加。

- [ ] **Step 6: 提交资产选择层**

```powershell
git add backend/internal/service/user_subscription_port.go backend/internal/repository/user_subscription_active_repo.go backend/internal/service/subscription_candidates.go backend/internal/service/api_key_billing_group_resolver.go backend/internal/service/api_key_asset_resolver.go backend/internal/service/api_key_asset_resolver_test.go backend/internal/service/billing_multiplier_selection.go backend/internal/service/global_balance_rate.go backend/internal/service/global_balance_rate_test.go
git commit -m "feat(计费): 按套餐优先选择用户资产"
```

## Task 7: 网关按平台调度并精确记录计费归属

**Files:**
- Modify: `backend/internal/service/gateway_scheduling.go`
- Modify: `backend/internal/service/openai_gateway_scheduling.go`
- Modify: `backend/internal/service/gateway_usage_billing.go`
- Modify: `backend/internal/service/openai_gateway_usage.go`
- Modify: `backend/internal/service/usage_log.go`
- Modify: `backend/internal/repository/usage_log_repo_insert.go`
- Create: `backend/internal/service/gateway_platform_asset_routing_test.go`
- Create: `backend/internal/service/gateway_platform_asset_billing_test.go`
- Create: `backend/internal/repository/usage_log_platform_asset_integration_test.go`

- [ ] **Step 1: 写入平台权限、账号池隔离、套餐回退和 usage attribution 的失败测试**

```go
func TestGatewayRejectsKeyWithoutResolvedPlatformPermission(t *testing.T) {
	service := newGatewayForPlatformAssetTest(platformRule("gpt-4o", 1))
	_, err := service.ResolveRequestRoute(context.Background(), keyWithPlatforms(2), "gpt-4o")
	require.ErrorIs(t, err, ErrAPIKeyPlatformForbidden)
}

func TestGatewaySchedulesOnlyResolvedPlatformAccounts(t *testing.T) {
	account, err := newGatewayForPlatformAssetTest(platformRule("gpt-4o", 1)).SelectAccountForPlatformModel(
		context.Background(), 1, "gpt-4o", nil,
	)
	require.NoError(t, err)
	require.Equal(t, PlatformOpenAI, account.Platform)
}

func TestUsageLogStoresPlatformAndBillingSource(t *testing.T) {
	log := usageLogFromResolvedAsset(1, 44, ResolvedBillingAsset{Source: BillingSourceSubscription, SubscriptionID: ptr(int64(8)), RateMultiplier: 1.5})
	require.Equal(t, int64(1), *log.PlatformID)
	require.Equal(t, "subscription", log.BillingSourceType)
	require.Equal(t, int64(8), *log.SubscriptionID)
}
```

- [ ] **Step 2: 运行网关测试确认在实现前失败**

Run: `go test -tags=unit ./internal/service -run "TestGateway(RejectsKeyWithoutResolvedPlatformPermission|SchedulesOnlyResolvedPlatformAccounts)|TestUsageLogStoresPlatformAndBillingSource" -count=1`

Expected: FAIL，提示平台资产路由 API 不存在。

- [ ] **Step 3: 加入标准请求上下文，先平台后资产**

```go
type GatewayPlatformAssetContext struct {
	Platform      *ResolvedPlatformModel
	BillingAsset  *ResolvedBillingAsset
	PricingGroupID *int64 // 仅取 Platform.LegacyGroupID，供 ModelPricingResolver 兼容读取
}
```

新路径严格执行：

```text
requested model -> PlatformService.ResolveModel
-> API key AllowedPlatformIDs check
-> accountRepo.ListSchedulableByPlatform(platform.code)
-> endpoint capability and account failover
-> APIKeyService.ResolveBillingAssetForRequest
-> current ModelPricingResolver with Platform.LegacyGroupID
-> asset multiplier and atomic debit
```

不得调用 `ListSchedulableByGroupIDAndPlatform`、`account_groups` 或 `billingGroupCandidates` 作为新路径回退。`platform_assets_v2_enabled=false` 保持现有组路径不变；开关为 true 时，缺少新平台或资产授权返回明确迁移错误，不放大为所有平台权限。

- [ ] **Step 4: 写入新的使用记录字段**

```go
usage.PlatformID = &route.Platform.PlatformID
usage.AccountID = &account.ID
usage.SubscriptionID = asset.SubscriptionID
usage.BillingSourceType = asset.Source
usage.RateMultiplier = asset.RateMultiplier
```

旧 `GroupID` 保持写入 `Platform.LegacyGroupID`（若存在）以支持历史查询；新列表、筛选和聚合优先使用 `platform_id` 与 `billing_source_type`。

- [ ] **Step 5: 运行网关、用量与端点回归**

Run: `go test -tags=unit ./internal/service -run "TestGateway.*Platform.*|TestUsageLogStoresPlatformAndBillingSource|TestOpenAI.*(ChatCompletions|Responses)" -count=1`

Expected: PASS。

Run: `go test -tags=integration ./internal/repository -run TestUsageLogPlatformAsset -count=1`

Expected: PASS；若无集成数据库，保留真实未执行状态。

- [ ] **Step 6: 提交网关切换层**

```powershell
git add backend/internal/service/gateway_scheduling.go backend/internal/service/openai_gateway_scheduling.go backend/internal/service/gateway_usage_billing.go backend/internal/service/openai_gateway_usage.go backend/internal/service/usage_log.go backend/internal/repository/usage_log_repo_insert.go backend/internal/service/gateway_platform_asset_routing_test.go backend/internal/service/gateway_platform_asset_billing_test.go backend/internal/repository/usage_log_platform_asset_integration_test.go
git commit -m "feat(网关): 按平台调度并记录资产来源"
```

## Task 8: 账号管理去除分组输入，保留隐式兼容绑定

**Files:**
- Modify: `backend/internal/service/account_service.go`
- Modify: `backend/internal/handler/admin/account_handler.go`
- Modify: `backend/internal/handler/dto/types.go`
- Modify: `backend/internal/repository/account_repo.go`
- Modify: `frontend/src/components/account/CreateAccountModal.vue`
- Modify: `frontend/src/components/account/EditAccountModal.vue`
- Modify: `frontend/src/views/admin/AccountsView.vue`
- Create: `frontend/src/components/account/__tests__/AccountPlatformOnlyForm.spec.ts`
- Modify: `backend/internal/service/account_service_delete_test.go`

- [ ] **Step 1: 写入账号请求不再暴露 group_ids 的失败测试**

```ts
it('submits only platform-owned account fields', async () => {
  const wrapper = mount(CreateAccountModal, { props: { modelValue: true } })
  await wrapper.get('[data-test="submit-account"]').trigger('click')
  expect(createAccount).toHaveBeenCalledWith(expect.not.objectContaining({ group_ids: expect.anything() }))
})
```

```go
func TestCreateAccountDoesNotRequireGroupIDs(t *testing.T) {
	account, err := service.Create(ctx, CreateAccountRequest{Name: "oa", Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	require.NoError(t, err)
	require.Equal(t, PlatformOpenAI, account.Platform)
}
```

- [ ] **Step 2: 运行账号表单和服务测试确认在实现前失败**

Run: `npm run test:run -- src/components/account/__tests__/AccountPlatformOnlyForm.spec.ts`

Expected: FAIL，测试文件或断言尚不存在。

Run: `go test -tags=unit ./internal/service -run TestCreateAccountDoesNotRequireGroupIDs -count=1`

Expected: FAIL，现有服务仍接收并校验 `GroupIDs`。

- [ ] **Step 3: 移除账号公开输入字段并保留迁移期内部绑定**

```go
type CreateAccountRequest struct {
	Name, Platform, Type string
	Credentials, Extra   map[string]any
	// 不含 GroupIDs。
}

func (s *AccountService) bindLegacyPlatformGroup(ctx context.Context, account *Account) error {
	platform, err := s.platformRepo.GetByCode(ctx, account.Platform)
	if err != nil || platform.LegacyGroupID == nil {
		return err
	}
	return s.accountRepo.BindGroups(ctx, account.ID, []int64{*platform.LegacyGroupID})
}
```

该隐藏绑定只在 `platform_assets_v2_enabled=false` 且平台明确配置了 `legacy_group_id` 时执行，永不从表单接收分组；新路径开启后不再写入 `account_groups`。

- [ ] **Step 4: 调整前端表单和列表**

删除 `GroupSelector`、`form.group_ids`、混合调度提示及分组列；列表显示账户自身 `platform`。批量编辑、复制、导入和影子账号流程不得再传 `group_ids`。

- [ ] **Step 5: 运行账号定向验证**

Run: `npm run test:run -- src/components/account/__tests__/AccountPlatformOnlyForm.spec.ts src/components/account/__tests__/CreateAccountModal.spec.ts src/components/account/__tests__/EditAccountModal.spec.ts`

Expected: PASS。

Run: `go test -tags=unit ./internal/service -run "TestCreateAccountDoesNotRequireGroupIDs|TestAccount" -count=1`

Expected: PASS。

- [ ] **Step 6: 提交账号平台化表单**

```powershell
git add backend/internal/service/account_service.go backend/internal/handler/admin/account_handler.go backend/internal/handler/dto/types.go backend/internal/repository/account_repo.go frontend/src/components/account frontend/src/views/admin/AccountsView.vue
git commit -m "refactor(账号): 以平台替代分组选择"
```

## Task 9: 平台、套餐和 API Key 的前端管理体验

**Files:**
- Create: `frontend/src/api/admin/platforms.ts`
- Create: `frontend/src/views/admin/PlatformsView.vue`
- Create: `frontend/src/views/admin/__tests__/PlatformsView.spec.ts`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/api/keys.ts`
- Modify: `frontend/src/components/ApiKeyCreate.vue`
- Modify: `frontend/src/components/ApiKeyEdit.vue`
- Modify: `frontend/src/components/admin/user/UserApiKeysModal.vue`
- Create: `frontend/src/components/__tests__/ApiKeyAssetPermissions.spec.ts`
- Modify: `frontend/src/views/admin/GroupsView.vue`

- [ ] **Step 1: 编写 API Key 独立授权的失败组件测试**

```ts
it('defaults balance permission to true and submits separate platform and plan IDs', async () => {
  const wrapper = mount(ApiKeyCreate, { props: { platforms, subscriptionPlans } })
  expect(wrapper.get('[data-test="allow-balance"]').element).toBeChecked()
  await selectMulti(wrapper, 'platforms', [1, 2])
  await selectMulti(wrapper, 'subscription-plans', [10, 20])
  await wrapper.get('form').trigger('submit')
  expect(createApiKey).toHaveBeenCalledWith(expect.objectContaining({
    platform_ids: [1, 2], subscription_plan_ids: [10, 20], allow_balance: true,
  }))
  expect(createApiKey).not.toHaveBeenCalledWith(expect.objectContaining({ group_ids: expect.anything() }))
})
```

- [ ] **Step 2: 运行 API Key 组件测试确认在实现前失败**

Run: `npm run test:run -- src/components/__tests__/ApiKeyAssetPermissions.spec.ts`

Expected: FAIL，组件尚未提供新字段。

- [ ] **Step 3: 实现平台管理和 API Key 表单**

```ts
export interface Platform {
  id: number
  code: string
  name: string
  status: 'active' | 'inactive'
  endpoint_capabilities: Record<string, boolean>
  legacy_group_id?: number | null
  model_rules: PlatformModelRule[]
}

export interface ApiKey {
  // legacy group fields may be returned during migration, but are not editable.
  platform_ids: number[]
  subscription_plan_ids: number[]
  allow_balance: boolean
}
```

平台编辑页在保存前显示模型规则冲突响应；API Key 使用两个独立多选器和一个布尔开关，不显示任何可编辑的分组下拉。新建默认勾选余额；保存规则为至少一个平台且至少一个套餐或余额开关为真。

- [ ] **Step 4: 将旧“分组”管理界面迁移为兼容入口**

`GroupsView.vue` 不再作为账号路由、套餐设置、API Key 授权的入口；在迁移期只展示只读的旧兼容状态和平台 `legacy_group_id` 关联。新增“平台”导航作为唯一的可编辑账号池配置入口，不删除旧管理路由。

- [ ] **Step 5: 运行前端定向测试、类型检查和 lint**

Run: `npm run test:run -- src/components/__tests__/ApiKeyAssetPermissions.spec.ts src/views/admin/__tests__/PlatformsView.spec.ts src/components/account/__tests__/AccountPlatformOnlyForm.spec.ts`

Expected: PASS。

Run: `npm run typecheck`

Expected: Exit code 0。

Run: `npm run lint:check`

Expected: Exit code 0。

- [ ] **Step 6: 提交前端平台资产界面**

```powershell
git add frontend/src/api frontend/src/views/admin/PlatformsView.vue frontend/src/views/admin/GroupsView.vue frontend/src/components frontend/src/router/index.ts frontend/src/components/layout/AppSidebar.vue frontend/src/types/index.ts
git commit -m "feat(前端): 管理平台和密钥资产权限"
```

## Task 10: 套餐展示、使用记录和历史兼容

**Files:**
- Modify: `backend/internal/handler/subscription_handler.go`
- Modify: `backend/internal/handler/usage_handler.go`
- Modify: `backend/internal/handler/dto/types.go`
- Modify: `backend/internal/repository/usage_log_repo_query.go`
- Modify: `backend/internal/repository/usage_log_repo_stats.go`
- Modify: `frontend/src/api/subscriptions.ts`
- Modify: `frontend/src/api/usage.ts`
- Modify: `frontend/src/views/user/UsageView.vue`
- Modify: `frontend/src/components/user/dashboard/UserSubscriptionSummaryCard.vue`
- Create: `frontend/src/views/user/__tests__/UsageView.billingSource.spec.ts`

- [ ] **Step 1: 写出套餐与使用记录新归属字段的失败测试**

```ts
it('renders platform and subscription asset source independently', async () => {
  mockUsage([{ platform: 'openai', billing_source_type: 'subscription', subscription_name: '大包' }])
  const wrapper = mount(UsageView)
  await flushPromises()
  expect(wrapper.text()).toContain('openai')
  expect(wrapper.text()).toContain('大包')
})
```

```go
func TestUsageDTOIncludesPlatformAndBillingSource(t *testing.T) {
	dto := ToUsageLogDTO(&service.UsageLog{PlatformID: ptr(int64(1)), BillingSourceType: service.BillingSourceBalance})
	require.Equal(t, int64(1), *dto.PlatformID)
	require.Equal(t, service.BillingSourceBalance, dto.BillingSourceType)
}
```

- [ ] **Step 2: 运行定向测试确认在实现前失败**

Run: `go test -tags=unit ./internal/handler/dto -run TestUsageDTOIncludesPlatformAndBillingSource -count=1`

Expected: FAIL，DTO 字段不存在。

Run: `npm run test:run -- src/views/user/__tests__/UsageView.billingSource.spec.ts`

Expected: FAIL，页面尚未显示资产来源。

- [ ] **Step 3: 补齐 API、查询和前端展示**

使用记录 DTO 增加：

```go
PlatformID         *int64 `json:"platform_id,omitempty"`
PlatformCode       string `json:"platform_code,omitempty"`
BillingSourceType  string `json:"billing_source_type"`
SubscriptionID     *int64 `json:"subscription_id,omitempty"`
SubscriptionName   string `json:"subscription_name,omitempty"`
```

用户使用记录的“分组”列在新记录中显示套餐名称或“余额”，平台单列显示真实平台；旧记录保留原分组文案。仪表盘订阅卡继续依据 `UserSubscription` 快照显示日、周、月额度和套餐倍率，不以平台作为套餐归属。

- [ ] **Step 4: 运行 DTO、页面和现有使用记录回归测试**

Run: `go test -tags=unit ./internal/handler/dto ./internal/repository -run "TestUsageDTOIncludesPlatformAndBillingSource|TestUsageLog" -count=1`

Expected: PASS。

Run: `npm run test:run -- src/views/user/__tests__/UsageView.billingSource.spec.ts src/components/user/dashboard/__tests__/UserSubscriptionSummaryCard.spec.ts`

Expected: PASS。

- [ ] **Step 5: 提交可见归属信息**

```powershell
git add backend/internal/handler/subscription_handler.go backend/internal/handler/usage_handler.go backend/internal/handler/dto backend/internal/repository/usage_log_repo_query.go backend/internal/repository/usage_log_repo_stats.go frontend/src/api/subscriptions.ts frontend/src/api/usage.ts frontend/src/views/user/UsageView.vue frontend/src/components/user/dashboard/UserSubscriptionSummaryCard.vue frontend/src/views/user/__tests__/UsageView.billingSource.spec.ts
git commit -m "feat(记录): 显示平台与资产扣费来源"
```

## Task 11: 双读切换、全量验证与 `my2-v*` 预发布

**Files:**
- Modify: `docs/memory/当前状态.md`
- Modify: `docs/memory/项目概览.md`
- Create: `docs/memory/决策/2026-08-04-平台资产迁移启用条件.md`
- Modify: `docs/BRANCH_AND_IMAGE_BUILD_CN.md`
- Modify: `backend/internal/service/platform_migration_preflight.go`
- Modify: `backend/internal/service/gateway_scheduling.go`
- Modify: `backend/internal/service/openai_gateway_scheduling.go`

- [ ] **Step 1: 写出开关保护的失败测试**

```go
func TestPlatformAssetsV2DoesNotFallbackToLegacyGroups(t *testing.T) {
	gateway := newGatewayWithMigrationMode(true)
	_, err := gateway.ResolveRequestRoute(context.Background(), legacyOnlyKey(), "gpt-4o")
	require.ErrorIs(t, err, ErrPlatformAssetMigrationRequired)
}
```

- [ ] **Step 2: 运行开关保护测试确认在实现前失败**

Run: `go test -tags=unit ./internal/service -run TestPlatformAssetsV2DoesNotFallbackToLegacyGroups -count=1`

Expected: FAIL，旧路径仍会静默回退。

- [ ] **Step 3: 实现切换规则和人工启用条件**

启用前必须同时满足：

```text
1. Preflight.Ready == true。
2. 每个启用 Platform 的模型规则跨平台无冲突。
3. 每个将被使用的新 API Key 都有非空 platform_ids，且有套餐权限或 allow_balance=true。
4. 每个需要渠道自定义定价的平台都有唯一 legacy_group_id，或明确确认只使用全局模型定价。
5. 已在 my2.0 测试环境完成 chat/completions、responses、套餐耗尽到余额、账户故障切换和 usage attribution 验证。
```

开关开启后新请求不能读取旧 Key 分组作为隐式平台权限；若管理员希望继续使用旧 Key，必须在界面或 API 中显式写入平台和资产权限。

- [ ] **Step 4: 运行最终后端与前端验证**

Run: `go test -tags=unit ./internal/service ./internal/handler ./internal/handler/dto -count=1`

Expected: Exit code 0。

Run: `go test ./migrations -count=1`

Expected: Exit code 0。

Run: `npm run test:run`

Expected: Exit code 0。

Run: `npm run typecheck`

Expected: Exit code 0。

Run: `npm run lint:check`

Expected: Exit code 0。

Run: `npm run build`

Expected: Exit code 0。

Run: `git diff --check`

Expected: Exit code 0。

- [ ] **Step 5: 在测试环境执行人工验收清单**

```text
- 管理员创建 OpenAI、GLM、Grok 三个平台，每个平台绑定各自账号；账号表单没有分组选择。
- 一个 API Key 允许 OpenAI 和 GLM，勾选两个套餐并保持余额允许；两个套餐均有效时最早到期者被扣费。
- 耗尽首个套餐后继续使用下一个套餐；全部耗尽后仅在余额允许且余额足够时切换余额。
- 请求 /v1/chat/completions 与 /v1/responses 仅调度端点能力匹配的平台账号；失败账号会在同一平台池中执行既有故障切换。
- 使用记录准确显示平台、账号、套餐或余额、倍率和费用；渠道自定义模型价格与改造前对比一致。
```

- [ ] **Step 6: 回填项目记忆和预发布说明**

在 `当前状态.md` 中记录实际迁移状态、已执行的验证命令及未执行项；在决策文件中记录“必须先预检、显式授权、在 `my2-v*` 测试镜像验收后再合入 `my`”。不得声称未运行的完整 CI 已通过。

- [ ] **Step 7: 提交验证和文档收尾**

```powershell
git add docs backend frontend
git commit -m "test(平台): 验证资产解耦预发布链路"
```

发布仅在用户明确要求后执行：先确认 `my2.0` 干净且测试证据完整，再创建未占用的 `my2-vX.Y.Z` 标签并 `git push origin my2.0 --follow-tags`。在业务验收前不得把该标签当作 `my` 正式 Docker 镜像。

## 规格覆盖自检

| 已确认需求 | 覆盖任务 |
| --- | --- |
| 平台独立账号池，不跨平台混用 | Task 3、Task 4、Task 7、Task 8 |
| 账号表单无分组 | Task 8 |
| API Key 独立平台、套餐、余额权限 | Task 3、Task 5、Task 9 |
| 新 Key 默认允许余额 | Task 5、Task 9 |
| 套餐先于余额、最早到期优先 | Task 6、Task 7 |
| 日/周/月额度与套餐独立倍率 | Task 3、Task 6、Task 10 |
| 全局余额倍率且不叠乘 | Task 2、Task 3、Task 6 |
| 保留现有模型定价和自定义价格 | Task 4、Task 6、Task 7 |
| chat/completions 与 responses 能力隔离、同平台故障切换 | Task 4、Task 7、Task 11 |
| 使用记录归属真实平台与资产 | Task 3、Task 7、Task 10 |
| 不自动猜测、不删除历史数据 | Task 2、Task 3、Task 11 |
| `docs/` 随 Git 管理、长期决策可追溯 | Task 1、Task 11 |
