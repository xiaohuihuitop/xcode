# 彻底移除旧分组配置 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `my2.0` 只使用 Platform 路由、SubscriptionPlan/UserSubscription 套餐和全局余额倍率，彻底移除旧 Group/Channel 配置、旧账号模型策略以及所有运行时兼容回退。

**Architecture:** Platform 是唯一请求路由、端点能力、模型规则和账号池权威；API Key 只授权 Platform、SubscriptionPlan 和余额。模型基础价格使用独立的 `ModelPricingCatalog`，按最终 adapter 与上游模型解析；用户资产倍率只来自套餐快照或全局余额倍率。历史用户资产和审计记录通过一次性数据迁移转成新实体或不可配置快照，迁移完成后删除旧字段、旧表和旧 API，不保留双读、开关或回退路径。

**Tech Stack:** Go 1.26.5、Gin、Ent、PostgreSQL、Redis、Vue 3、TypeScript、Vite、Vitest、GitHub Actions Docker Release

---

## 已锁定边界

### 必须删除

- `Group`、`BillingProfile`、`CompositeModelRoute` 及其管理员配置入口。
- API Key 的 `group_id`、`group_ids`、`api_key_allowed_groups` 及分组回退。
- Account 的 `group_ids`、`account_groups`、混合渠道检查及普通 `credentials.model_mapping`、`model_whitelist`、`openai_capabilities`。
- Platform 的 `legacy_group_id`、`PricingGroupID`、`LegacyPricingGroupID`。
- SubscriptionPlan、UserSubscription、RedeemCode、PaymentOrder 中的旧分组关联。
- 旧 Channel 的分组关联、模型映射、限制模型、计费来源和账号统计规则。
- `platform_assets_v2_enabled` 以及所有“兼容期”“旧路径”“回退旧分组”的运行时代码。

### 必须保留

- 账号凭证、OAuth 刷新、失败切换、并发、优先级、代理和协议适配。
- `compact_model_mapping`、Responses/Chat/Compact 模式及 Adapter 内置技术规范化。
- Spark shadow 的 `parent_account_id`、`quota_dimension`；其调度资格改由这些结构化字段和 Platform 规则决定，不再写 `credentials.model_mapping`。
- LiteLLM/静态 fallback 模型价格、手工模型价格覆盖能力。
- 套餐日/周/月额度、套餐独立倍率、全局余额倍率、多套餐实例和套餐优先扣费。
- 用户、API Key 本体、余额、套餐、订阅实例、支付审计、用量与安全审计主体数据。
- Channel Monitor、Auth Identity Channel 等名称中含 `channel` 但不依赖旧 Channel 配置的独立模块。

### 历史数据策略

- 不保留可配置的旧 Group/Channel 实体。
- 活跃旧订阅、兑换码和未完成订阅订单必须先绑定到迁移生成的 `for_sale=false` 套餐，再删除旧分组引用。
- 历史用量、支付和安全审计可以保留金额、Token、时间、请求、平台和套餐快照；不能继续通过旧分组表查询或回填。
- 已执行的旧 SQL migration 不修改；只新增未占用编号的清理 migration（当前分支已使用 196/197/198，最终清理需使用后续空闲编号）。
- migration 必须幂等，并在无法安全转换仍有效的用户资产时 `RAISE EXCEPTION`，禁止静默丢失资产。

### 执行与提交安全规则

- 当前工作区不是干净基线。执行每个 Task 前先运行 `git status --short --branch`，确认并保留既有改动、`backend/entgen_tmp/` 和 `my2.0.drawio`。
- 每次提交只能用 `git add -- <本 Task 已复核的精确路径>` 暂存，随后运行 `git diff --cached --name-only`；禁止以 `backend`、`frontend` 或 `docs` 整个目录作为暂存参数，禁止把临时生成目录或用户文件带入提交。
- 任一 Task 的定向测试未通过时不得进入下一 Task；共享 contract、schema 和 migration 只按本计划的既定顺序串行修改。
- 本计划授权本地代码、测试、文档和 migration 实现；push、Tag、GitHub Release 与服务器部署仍需用户届时明确要求。

## 文件结构

### 新增

- `backend/internal/architecture/legacy_configuration_guard_test.go`：禁止活动源码重新出现旧配置标识。
- `backend/ent/schema/model_pricing_override.go`：独立模型价格覆盖实体。
- `backend/internal/domain/model_pricing.go`：跨 schema/service 使用的价格区间值对象。
- `backend/internal/service/model_pricing_catalog.go`：模型价格覆盖领域对象和解析接口。
- `backend/internal/repository/model_pricing_override_repo.go`：价格覆盖持久化。
- `backend/internal/handler/admin/model_pricing_handler.go`：管理员价格 CRUD 与 LiteLLM 同步接口。
- `backend/migrations/<next_free>_remove_legacy_configuration.sql`：一次性转换和物理清理。
- `backend/migrations/<next_free>_remove_legacy_configuration_test.go`：迁移顺序、保护条件和删除项测试。
- `frontend/src/api/admin/modelPricing.ts`：独立价格 API。
- `frontend/src/views/admin/ModelPricingView.vue`：模型价格维护页。
- `frontend/src/views/admin/__tests__/ModelPricingView.spec.ts`：价格页回归。
- `backend/internal/service/platform_available.go`：基于 Platform 的用户可用服务视图。
- `backend/internal/service/platform_plaza.go`：基于 Platform 的模型广场聚合。
- `backend/internal/handler/available_platform_handler.go`：用户可用 Platform API。
- `frontend/src/api/platformCatalog.ts`：用户侧 Platform 目录 API。
- `frontend/src/views/user/AvailablePlatformsView.vue`：用户可用 Platform 页面。
- `frontend/src/components/platforms/AvailablePlatformsTable.vue`：Platform 目录表格。

### 删除

- `backend/ent/schema/group.go`
- `backend/ent/schema/account_group.go`
- `backend/ent/schema/user_allowed_group.go`
- `backend/ent/schema/billing_profile.go`
- `backend/ent/schema/composite_model_route.go`
- `backend/internal/service/admin_group.go`
- `backend/internal/service/group.go`
- `backend/internal/service/account_group.go`
- `backend/internal/service/billing_profile.go`
- `backend/internal/service/api_key_billing_group_resolver.go`
- `backend/internal/service/api_key_group_selection.go`
- `backend/internal/service/channel.go`
- `backend/internal/service/channel_service.go`
- `backend/internal/service/group_service.go`
- `backend/internal/service/group_capacity_service.go`
- `backend/internal/service/group_models_list.go`
- `backend/internal/service/admin_group_duplicate.go`
- `backend/internal/service/user_group_rate.go`
- `backend/internal/service/user_group_rate_resolver.go`
- `backend/internal/service/composite_platform.go`
- `backend/internal/service/composite_model_route.go`
- `backend/internal/service/composite_route_resolver.go`
- `backend/internal/repository/group_repo.go`
- `backend/internal/repository/user_group_rate_repo.go`
- `backend/internal/repository/composite_model_route_repo.go`
- `backend/internal/repository/channel_repo.go`
- `backend/internal/repository/channel_repo_pricing.go`
- `backend/internal/repository/channel_repo_account_stats_pricing.go`
- `backend/internal/repository/simple_mode_default_groups.go`
- `backend/internal/handler/admin/group_handler.go`
- `backend/internal/handler/admin/channel_handler.go`
- `backend/internal/handler/admin/apikey_handler.go`
- `frontend/src/views/admin/GroupsView.vue`
- `frontend/src/views/admin/ChannelsView.vue`
- `frontend/src/api/groups.ts`
- `frontend/src/api/admin/groups.ts`
- `frontend/src/api/admin/channels.ts`
- `frontend/src/api/admin/apiKeys.ts`
- `frontend/src/constants/channel.ts`
- `frontend/src/views/admin/apiKeyGroupFilterOptions.ts`
- `frontend/src/views/admin/groupsImagePricing.ts`
- `frontend/src/views/admin/groupsMessagesDispatch.ts`
- `frontend/src/views/admin/groupsModelsList.ts`
- `frontend/src/views/admin/groupsModelsListCandidates.ts`
- `frontend/src/views/admin/groupsReasoningEffort.ts`
- `frontend/src/views/admin/groupsSupportedModelScopes.ts`
- `frontend/src/components/common/GroupSelector.vue`
- `frontend/src/components/common/GroupOptionItem.vue`
- `frontend/src/components/common/GroupCapacityBadge.vue`
- `frontend/src/components/common/GroupBadge.vue`
- `frontend/src/components/account/AccountGroupsCell.vue`
- `frontend/src/components/admin/user/UserAllowedGroupsModal.vue`
- `frontend/src/components/admin/user/GroupReplaceModal.vue`
- `frontend/src/components/admin/group/GroupBillingProfileDialog.vue`
- `frontend/src/components/admin/group/GroupRPMOverridesModal.vue`
- `frontend/src/components/admin/group/GroupRateMultipliersModal.vue`
- `frontend/src/components/admin/group/ReasoningEffortPolicyFields.vue`
- `frontend/src/components/admin/channel/PricingEntryCard.vue`
- `frontend/src/components/admin/channel/ModelTagInput.vue`
- `frontend/src/components/admin/channel/IntervalRow.vue`
- `frontend/src/components/admin/channel/types.ts`
- `frontend/src/components/charts/GroupDistributionChart.vue`
- `frontend/src/components/modelPlaza/PlazaGroupSection.vue`
- `backend/internal/service/channel_available.go`
- `backend/internal/service/channel_plaza.go`
- `backend/internal/handler/available_channel_handler.go`
- `frontend/src/api/channels.ts`
- `frontend/src/views/user/AvailableChannelsView.vue`
- `frontend/src/components/channels/AvailableChannelsTable.vue`

删除文件前必须先完成调用方迁移；执行中不得使用递归删除命令，逐文件通过 `apply_patch` 删除。

## Task 0: 固化当前平台修复并建立残留基线

**Files:**
- Modify: `docs/memory/当前状态.md`
- Test: 当前平台管理页改动涉及的 service 与前端组件

- [ ] **Step 1: 固化当前未提交的平台管理页修复**

运行：

```powershell
git status --short --branch
git diff --check
Set-Location frontend
npm run test:run -- src/components/admin/platform/__tests__/PlatformPoolDialog.spec.ts src/components/admin/platform/__tests__/platformModelRules.spec.ts src/views/admin/__tests__/PlatformsView.spec.ts
npm run typecheck
Set-Location ../backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run Platform -count=1
Set-Location ..
```

预期：测试退出码为 `0`；只提交当前平台页修复，不混入本计划代码。

- [ ] **Step 2: 记录旧配置残留基线**

```powershell
rg -l 'group_id|group_ids|AllowedGroups|account_groups|api_key_allowed_groups|legacy_group_id|PricingGroupID|platform_assets_v2_enabled' backend/internal backend/ent/schema frontend/src
```

将命中文件按 Task 1-9 归属写入 `docs/memory/当前状态.md`。不在此时创建必然失败的架构测试；最终守卫在 Task 10 清理完成后创建并一次通过。

- [ ] **Step 3: 隔离提交当前平台修复**

只暂存 Task 0 开始前已经存在且通过验证的平台管理页相关文件；通过 `git diff --cached --name-only` 确认没有 `backend/entgen_tmp/`、`my2.0.drawio` 或本计划后续代码后，提交 `fix(platform): 修复平台模型管理契约`。

## Task 1: 强制所有网关请求使用 Platform 授权

**Files:**
- Modify: `backend/internal/server/middleware/api_key_auth.go`
- Modify: `backend/internal/server/middleware/api_key_auth_google.go`
- Modify: `backend/internal/server/middleware/ingress_reject.go`
- Modify: `backend/internal/server/routes/gateway.go`
- Modify: `backend/internal/service/ops_upstream_context.go`
- Test: `backend/internal/server/middleware/api_key_auth_test.go`
- Test: `backend/internal/server/middleware/api_key_auth_google_test.go`
- Test: `backend/internal/server/middleware/platform_asset_auth_test.go`
- Test: `backend/internal/server/middleware/openai_fast_policy_forwarding_test.go`
- Test: `backend/internal/server/routes/gateway_test.go`

- [ ] **Step 1: 写入 group-only Key 被拒绝的测试**

测试固定两个可执行契约：构造“用户有效、余额可用、旧 `group_id` 存在、但没有 `api_key_platforms`”的 Key，逐一请求 Chat、Responses、Gemini 和 `/v1/models`，断言 HTTP 403、错误码 `API_KEY_PLATFORM_REQUIRED`，并断言旧订阅分组解析器调用次数为 0；构造含旧 `APIKey.Group.Platform` 但不含 `PlatformSchedulingScope` 的 Gin context，断言 `getRequestAdapter` 返回空值和 `false`。

- [ ] **Step 2: 运行测试确认旧回退导致失败**

```powershell
Set-Location backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/server/middleware ./internal/server/routes -run 'PlatformGrant|FallsBackToAPIKeyGroup' -count=1
```

预期：FAIL，当前代码仍进入 `ResolveBillingGroupForRequest` 或读取 `apiKey.Group.Platform`。

- [ ] **Step 3: 收口认证中间件**

主认证与 Google 认证统一执行：

```go
if !service.UsesPlatformAssetPermissions(apiKey) {
	MarkIngressRejected(c, IngressRejectPlatformNotGranted)
	AbortWithError(c, http.StatusForbidden, "API_KEY_PLATFORM_REQUIRED", "API Key 未授权任何平台")
	return
}
completePlatformAssetAPIKeyAuth(c, apiKey, apiKeyService, cfg, billingInfoRequest)
```

Google 路径使用同一错误码和 Google 错误 envelope。删除 `abortIfAPIKeyGroupUnavailable`、`validateAPIKeyGroupAllowed`、`setGroupContext` 及 `RequireGroupAssignment` 的调用。

- [ ] **Step 4: 收口路由 adapter 解析**

将 `getGroupPlatform` 改名为 `getRequestAdapter`，只读取 `PlatformSchedulingScope.AccountPlatform`。`/v1/models` 直接走授权 Platform 聚合；需要明确 adapter 的 count/media/Live 接口必须先得到唯一 Platform 解析结果，否则返回 `PLATFORM_REQUIRED` 或 `PLATFORM_AMBIGUOUS`。

- [ ] **Step 5: 删除旧分组解析器并运行定向测试**

```powershell
Set-Location backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/server/middleware ./internal/server/routes ./internal/service -run 'APIKeyAuth|PlatformAsset|Gateway.*Platform' -count=1
```

预期：PASS；旧 group-only Key 在 Chat、Responses、Gemini 和模型列表入口全部失败关闭。

- [ ] **Step 6: 提交运行时收口**

按本 Task 的 Files 清单精确暂存并复核 staged diff，提交 `refactor(gateway): 移除旧分组路由回退`。

### Task 1 execution result (2026-08-09)

- Standard and Google API-key middleware now require an explicit Platform grant; legacy Group selection, availability checks, subscription resolution, and billing fallback are removed from the gateway authentication path.
- Adapter lookup reads only `PlatformSchedulingScope.AccountPlatform`; composite middleware does not override an already-resolved Platform asset route.
- Added no-platform-grant regression tests for standard OpenAI and Gemini paths. Legacy Group behavior tests are marked historical until the Group domain is removed in later tasks.
- Verification passed: `go test -tags=unit ./internal/server/middleware ./internal/server/routes -run 'APIKeyAuth|PlatformAsset|Gateway.*Platform|GetRequestAdapter' -count=1`.

## Task 2: 删除 API Key 的旧分组契约

**Files:**
- Modify: `backend/ent/schema/api_key.go`
- Modify: `backend/internal/service/api_key.go`
- Modify: `backend/internal/service/api_key_service.go`
- Modify: `backend/internal/repository/api_key_repo.go`
- Modify: `backend/internal/handler/api_key_handler.go`
- Modify: `backend/internal/handler/dto/types.go`
- Modify: `backend/internal/handler/dto/mappers.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `frontend/src/api/keys.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/components/admin/user/UserApiKeysModal.vue`
- Modify: `frontend/src/views/user/KeysView.vue`
- Delete: `backend/internal/handler/admin/apikey_handler.go`
- Delete: `frontend/src/api/admin/apiKeys.ts`
- Test: `backend/internal/handler/api_key_platform_pool_handler_test.go`
- Test: `backend/internal/service/api_key_asset_permissions_test.go`
- Test: `frontend/src/views/user/__tests__/KeysView.spec.ts`

- [ ] **Step 1: 写入 API 合约测试**

```go
func TestCreateAPIKeyContractHasNoLegacyGroupFields(t *testing.T) {
	body := postCreateKey(t, map[string]any{
		"name": "platform-key", "platform_ids": []int64{7},
		"subscription_plan_ids": []int64{11, 12}, "allow_balance": true,
	})
	require.NotContains(t, body, "group_id")
	require.NotContains(t, body, "group_ids")
}
```

同时断言传入 `group_id` 或 `group_ids` 不会被 DTO 接收，也不会改变 Key 授权。

- [ ] **Step 2: 删除 service/handler/DTO 的 Group 字段和方法**

`CreateAPIKeyRequest` 和 `UpdateAPIKeyRequest` 最终只保留：

```go
PlatformIDs         []int64  `json:"platform_ids"`
SubscriptionPlanIDs []int64  `json:"subscription_plan_ids"`
AllowBalance        *bool    `json:"allow_balance"`
```

删除 `ListByGroupID`、`ReplaceAllowedGroups`、`ClearGroupIDByGroupID`、`UpdateGroupIDByUserAndGroup`、`CountByGroupID`、`ListKeysByGroupID` 和按分组缓存失效。

- [ ] **Step 3: 删除 repository 的 group join 和回填**

API Key 查询只加载 `platforms`、`subscription_plans` 和用户；不得查询 `api_key_allowed_groups`，不得从 `api_keys.group_id` 回填主分组。

- [ ] **Step 4: 删除管理员重绑分组 UI 和 API**

`UserApiKeysModal.vue` 只显示 Platform、套餐和余额授权，不提供 Group 下拉框。移除 `/admin/api-keys/:id` 的 `UpdateGroup` 路由。

- [ ] **Step 5: 运行 API Key 回归**

```powershell
Set-Location backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service ./internal/handler ./internal/repository -run 'APIKey|PlatformPermission|SubscriptionPlanPermission' -count=1
Set-Location ../frontend
npm run test:run -- src/views/user/__tests__/KeysView.spec.ts
npm run typecheck
```

- [ ] **Step 6: 提交 API Key 清理**

按本 Task 的 Files 清单精确暂存并复核 staged diff，提交 `refactor(apikey): 删除旧分组授权契约`。

### Task 2 execution result (2026-08-09)

- Completed the external API Key contract cleanup: HTTP/service create and update requests, list filters, DTO mapping, frontend types, and user/admin UI no longer expose or submit `group_id`/`group_ids`.
- Removed the user available-groups endpoint and the registered admin API-key group-rebinding route. The frontend admin API module and legacy group-rebinding tests were removed.
- Updated API-key related consumers to derive platform behavior from `platform_ids`; batch-image and monitor key selectors no longer read `ApiKey.group`.
- Deferred internal Ent `group_id`/Group joins and legacy repository methods to the later Platform runtime migration and schema cleanup tasks. They remain unreachable from the API-key contract and are scheduled for removal in Tasks 6/8/9.
- Verification passed: targeted Go API-key tests, package compile-only checks, `KeysView.spec.ts`, and frontend typecheck.

## Task 3: 删除账号分组与账号级普通模型策略

**Files:**
- Modify: `backend/ent/schema/account.go`
- Modify: `backend/internal/service/account.go`
- Modify: `backend/internal/service/account_service.go`
- Modify: `backend/internal/service/admin_account.go`
- Modify: `backend/internal/service/account_credentials_redact.go`
- Modify: `backend/internal/service/platform_model_policy.go`
- Modify: `backend/internal/service/account_credentials_persistence.go`
- Modify: `backend/internal/service/openai_account_scheduler.go`
- Modify: `backend/internal/repository/account_repo.go`
- Modify: `backend/internal/repository/scheduler_cache.go`
- Modify: `backend/internal/handler/admin/account_handler.go`
- Modify: `frontend/src/components/account/CreateAccountModal.vue`
- Modify: `frontend/src/components/account/EditAccountModal.vue`
- Modify: `frontend/src/components/account/BulkEditAccountModal.vue`
- Test: `backend/internal/service/admin_account_platform_pool_test.go`
- Test: `backend/internal/service/account_credentials_redact_test.go`
- Test: `backend/internal/service/spark_shadow_integration_test.go`
- Test: `frontend/src/components/account/__tests__/CreateAccountModal.spec.ts`
- Test: `frontend/src/components/account/__tests__/EditAccountModal.spec.ts`
- Test: `frontend/src/components/account/__tests__/BulkEditAccountModal.spec.ts`

- [x] **Step 1: 写入账号保存净化测试**

```go
func TestMergeCredentialsDropsLegacyModelPolicy(t *testing.T) {
	out := MergePreservingSensitiveCreds(
		map[string]any{"refresh_token": "rt", "model_mapping": map[string]any{"old": "old"}},
		map[string]any{"access_token": "at"},
	)
	require.Equal(t, "rt", out["refresh_token"])
	require.NotContains(t, out, "model_mapping")
	require.NotContains(t, out, "model_whitelist")
	require.NotContains(t, out, "openai_capabilities")
}
```

另加测试：账号没有 `platform_id` 时不能创建或启用；Spark shadow 不依赖 `model_mapping` 仍能按 `quota_dimension=spark` 调度。

- [ ] **Step 2: 删除账号 GroupIDs 与 account_groups 读写**

删除 `CreateAccountInput.GroupIDs`、`UpdateAccountInput.GroupIDs`、bulk `group_ids`、`CheckMixedChannelRequest`、`BindGroups`、`ListSchedulableByGroupID*` 和 scheduler payload 中的 group IDs。

> 当前 Task 3 只完成外部 bulk `group_ids` 的拒绝和 Platform 路由隔离；Account/Ent 内部 Group 投影及旧 repository 方法按 Task 8/9 的运行时与 schema 一次性删除，避免在中间版本拆断仍在迁移的旧管理域。

- [x] **Step 3: 净化普通账号模型策略**

服务端合并凭证后统一执行：

```go
for _, key := range []string{"model_mapping", "model_whitelist", "openai_capabilities"} {
	delete(credentials, key)
}
```

删除前端所有普通模型白名单、普通模型映射和兼容回显代码。保留 `compact_model_mapping`、凭证字段和 Adapter 技术选项。

- [x] **Step 4: 替换 Spark shadow 的旧 mapping 依赖**

Spark 账号资格只根据 `quota_dimension == "spark"`、父账号关系、Platform 模型规则和 Adapter 内置 Spark 模型判断；不得再生成或读取 `credentials.model_mapping`。

- [x] **Step 5: 运行账号与调度回归**

```powershell
Set-Location backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service ./internal/repository ./internal/handler/admin -run 'Account|PlatformPool|SparkShadow|ModelPolicy' -count=1
Set-Location ../frontend
npm run test:run -- src/components/account
npm run typecheck
```

- [ ] **Step 6: 提交账号清理**

按本 Task 的 Files 清单精确暂存并复核 staged diff，提交 `refactor(account): 移除分组和旧模型配置`。

## Task 4: 抽离独立模型价格目录

**Files:**
- Create: `backend/ent/schema/model_pricing_override.go`
- Create: `backend/internal/domain/model_pricing.go`
- Create: `backend/internal/service/model_pricing_catalog.go`
- Create: `backend/internal/repository/model_pricing_override_repo.go`
- Create: `backend/internal/handler/admin/model_pricing_handler.go`
- Create: `frontend/src/api/admin/modelPricing.ts`
- Create: `frontend/src/views/admin/ModelPricingView.vue`
- Create: `frontend/src/views/admin/__tests__/ModelPricingView.spec.ts`
- Modify: `backend/internal/service/model_pricing_resolver.go`
- Modify: `backend/internal/service/account_stats_pricing.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/internal/handler/handler.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Test: `backend/internal/service/model_pricing_resolver_test.go`

- [ ] **Step 1: 写入独立价格优先级测试**

```go
func TestModelPricingResolverUsesAdapterOverrideWithoutGroup(t *testing.T) {
	resolved := resolver.Resolve(ctx, PricingInput{Adapter: "openai", Model: "gpt-5.6"})
	require.Equal(t, PricingSourceOverride, resolved.Source)
	require.Equal(t, 2.5e-6, resolved.BasePricing.InputPricePerToken)
}
```

另测精确规则优先于最长通配符、无覆盖时 LiteLLM → static fallback、图片/按次/区间价格完整保留。

- [ ] **Step 2: 建立独立 schema**

核心字段固定为：

```go
field.String("adapter").MaxLen(50).NotEmpty()
field.String("model_pattern").MaxLen(100).NotEmpty()
field.String("billing_mode").MaxLen(20).Default("token")
field.Float("input_price").Optional().Nillable()
field.Float("output_price").Optional().Nillable()
field.Float("cache_write_price").Optional().Nillable()
field.Float("cache_read_price").Optional().Nillable()
field.Float("image_input_price").Optional().Nillable()
field.Float("image_output_price").Optional().Nillable()
field.Float("per_request_price").Optional().Nillable()
field.JSON("intervals", []domain.ModelPricingInterval{}).Optional()
field.String("status").MaxLen(20).Default("active")
```

唯一索引为 `(adapter, model_pattern)`。

完成 schema 后立即运行 `go generate ./ent`；完成依赖注入后运行 `go generate ./cmd/server`，确保 Task 4 自身可以编译和提交，不依赖 Task 9 才生成代码。

- [ ] **Step 3: 重写解析器契约**

`PricingInput` 删除 `GroupID`：

```go
type PricingInput struct {
	Model   string
	Adapter string
}
```

解析顺序固定为 `ModelPricingOverride -> LiteLLM -> static fallback`。删除 `GetChannelModelPricing` 和所有按 group 查价、渠道模型映射及渠道限制模型逻辑。

- [ ] **Step 4: 建立管理员价格页**

新页面路径 `/admin/model-pricing`，支持 adapter、模型或通配符、计费模式、Token/缓存/图片/按次/区间价格、从 LiteLLM 自动填充和同步模型名。不得出现 Group、Channel 或账号选择。

- [ ] **Step 5: 简化账号成本统计**

账号成本仅使用最终上游模型的独立价格与 `account.rate_multiplier`；删除按 Group/Account 的 Channel pricing rule 和 `apply_pricing_to_account_stats`。

- [ ] **Step 6: 运行价格回归**

```powershell
Set-Location backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service ./internal/repository ./internal/handler/admin -run 'Pricing|Cost|Channel' -count=1
Set-Location ../frontend
npm run test:run -- src/views/admin/__tests__/ModelPricingView.spec.ts
npm run typecheck
```

- [ ] **Step 7: 提交价格目录**

按本 Task 的 Files 清单精确暂存并复核 staged diff，提交 `feat(pricing): 抽离独立模型价格目录`。

## Task 5: 套餐、兑换码、支付和公告只使用 Plan

**Files:**
- Modify: `backend/ent/schema/subscription_plan.go`
- Modify: `backend/ent/schema/user_subscription.go`
- Modify: `backend/ent/schema/redeem_code.go`
- Modify: `backend/ent/schema/payment_order.go`
- Modify: `backend/internal/service/subscription_service.go`
- Modify: `backend/internal/service/payment_fulfillment.go`
- Modify: `backend/internal/service/payment_refund.go`
- Modify: `backend/internal/service/admin_user.go`
- Modify: `backend/internal/service/announcement_service.go`
- Modify: `backend/internal/domain/announcement.go`
- Modify: `backend/internal/repository/user_subscription_repo.go`
- Modify: `backend/internal/repository/redeem_code_repo.go`
- Modify: `backend/internal/handler/admin/subscription_handler.go`
- Modify: `backend/internal/handler/admin/redeem_handler.go`
- Modify: `frontend/src/types/index.ts`
- Test: `backend/internal/service/payment_fulfillment_test.go`
- Test: `backend/internal/service/payment_refund_test.go`
- Test: `backend/internal/service/announcement_targeting_test.go`

- [ ] **Step 1: 写入禁止旧 group fulfillment 的测试**

```go
func TestSubscriptionFulfillmentRequiresPlanID(t *testing.T) {
	order := subscriptionOrderWithPlanID(nil)
	err := paymentService.ExecuteSubscriptionFulfillment(ctx, order.ID)
	require.ErrorContains(t, err, "missing subscription plan")
}
```

另测退款只能按订单审计关联的 `subscription_id` 或 `plan_id` 定位，不能按 group 找“当前有效订阅”。

- [ ] **Step 2: 收口订阅服务和 repository**

删除 `GetActiveSubscription(userID, groupID)`、`ListByGroupID`、`AssignSubscription.GroupID` 和 Group 统计。所有发放输入必须有 `SubscriptionPlanID`，活动订阅按用户、Plan、状态和到期时间选择。

- [ ] **Step 3: 收口支付与兑换码**

订阅订单和订阅兑换码必须有 `plan_id/subscription_plan_id`。支付成功审计直接记录实际创建或延长的 `user_subscription_id`；退款只使用该审计关联，禁止按旧分组猜测。

- [ ] **Step 4: 将公告订阅条件改为 Plan IDs**

```go
type AnnouncementCondition struct {
	Type                string  `json:"type"`
	MinBalance          float64 `json:"min_balance,omitempty"`
	SubscriptionPlanIDs []int64 `json:"subscription_plan_ids,omitempty"`
}
```

旧 `group_ids` 设置在 migration 中按 `subscription_plans.group_id` 转换；启用中的条件存在无法映射项时 migration 直接失败，管理员修正数据后重试。

SubscriptionPlan 以 `for_sale=false` 退役；被 UserSubscription、RedeemCode 或 PaymentOrder 引用时禁止物理删除。`UserSubscription.subscription_plan_id` 在回填后改为必填并使用 `ON DELETE RESTRICT`。

- [ ] **Step 5: 运行资产回归**

```powershell
Set-Location backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service ./internal/repository ./internal/handler/admin -run 'Subscription|Payment|Refund|Redeem|Announcement' -count=1
```

- [ ] **Step 6: 提交资产收口**

按本 Task 的 Files 清单精确暂存并复核 staged diff，提交 `refactor(billing): 套餐支付统一使用计划ID`。

## Task 6: 将安全策略、运维与统计维度改为 Platform/Plan

**Files:**
- Create: `frontend/src/components/charts/PlatformDistributionChart.vue`
- Modify: `backend/internal/service/content_moderation.go`
- Modify: `backend/internal/handler/content_moderation_helper.go`
- Modify: `backend/internal/handler/admin/content_moderation_handler.go`
- Modify: `backend/internal/securityaudit/prompt_config.go`
- Modify: `backend/internal/securityaudit/prompt_enqueue.go`
- Modify: `backend/internal/securityaudit/prompt_repository.go`
- Modify: `backend/internal/service/ops_account_availability.go`
- Modify: `backend/internal/service/ops_alert_evaluator_service.go`
- Modify: `backend/internal/service/ops_alert_models.go`
- Modify: `backend/internal/service/ops_concurrency.go`
- Modify: `backend/internal/service/ops_dashboard_models.go`
- Modify: `backend/internal/service/ops_models.go`
- Modify: `backend/internal/service/ops_openai_token_stats.go`
- Modify: `backend/internal/service/ops_openai_token_stats_models.go`
- Modify: `backend/internal/service/ops_port.go`
- Modify: `backend/internal/service/ops_realtime_models.go`
- Modify: `backend/internal/service/ops_realtime_traffic_models.go`
- Modify: `backend/internal/service/ops_request_details.go`
- Modify: `backend/internal/service/ops_scheduled_report_service.go`
- Modify: `backend/internal/service/ops_trend_models.go`
- Modify: `backend/internal/service/ops_user_error.go`
- Modify: `backend/internal/repository/ops_repo.go`
- Modify: `backend/internal/repository/ops_repo_alerts.go`
- Modify: `backend/internal/repository/ops_repo_dashboard.go`
- Modify: `backend/internal/repository/ops_repo_histograms.go`
- Modify: `backend/internal/repository/ops_repo_metrics.go`
- Modify: `backend/internal/repository/ops_repo_openai_token_stats.go`
- Modify: `backend/internal/repository/ops_repo_preagg.go`
- Modify: `backend/internal/repository/ops_repo_realtime_traffic.go`
- Modify: `backend/internal/repository/ops_repo_request_details.go`
- Modify: `backend/internal/repository/ops_repo_trends.go`
- Modify: `backend/internal/handler/admin/ops_alerts_handler.go`
- Modify: `backend/internal/handler/admin/ops_dashboard_handler.go`
- Modify: `backend/internal/handler/admin/ops_handler.go`
- Modify: `backend/internal/handler/admin/ops_realtime_handler.go`
- Modify: `backend/internal/handler/admin/ops_snapshot_v2_handler.go`
- Modify: `backend/internal/service/dashboard_service.go`
- Modify: `backend/internal/repository/usage_log_repo_dashboard.go`
- Modify: `backend/internal/repository/usage_log_repo_insert.go`
- Modify: `backend/internal/repository/usage_log_repo_query.go`
- Modify: `backend/internal/repository/usage_log_repo_stats.go`
- Modify: `backend/internal/repository/usage_log_repo_trend.go`
- Modify: `backend/internal/handler/admin/dashboard_handler.go`
- Modify: `backend/internal/handler/admin/dashboard_query_cache.go`
- Modify: `backend/internal/handler/admin/dashboard_snapshot_v2_handler.go`
- Modify: `backend/internal/handler/admin/usage_handler.go`
- Modify: `frontend/src/api/admin/ops.ts`
- Modify: `frontend/src/api/admin/dashboard.ts`
- Modify: `frontend/src/views/admin/ops/OpsDashboard.vue`
- Modify: `frontend/src/views/admin/ops/components/OpsAlertEventsCard.vue`
- Modify: `frontend/src/views/admin/ops/components/OpsAlertRulesCard.vue`
- Modify: `frontend/src/views/admin/ops/components/OpsConcurrencyCard.vue`
- Modify: `frontend/src/views/admin/ops/components/OpsDashboardHeader.vue`
- Modify: `frontend/src/views/admin/ops/components/OpsErrorDetailModal.vue`
- Modify: `frontend/src/views/admin/ops/components/OpsErrorDetailsModal.vue`
- Modify: `frontend/src/views/admin/ops/components/OpsErrorLogTable.vue`
- Modify: `frontend/src/views/admin/ops/components/OpsOpenAITokenStatsCard.vue`
- Modify: `frontend/src/views/admin/ops/components/OpsRequestDetailsModal.vue`
- Modify: `frontend/src/views/admin/ops/components/OpsThroughputTrendChart.vue`
- Modify: `frontend/src/views/admin/UsageView.vue`
- Modify: `frontend/src/views/user/UsageView.vue`
- Delete: `frontend/src/components/charts/GroupDistributionChart.vue`
- Test: `backend/internal/securityaudit/prompt_config_test.go`
- Test: `backend/internal/service/content_moderation_test.go`
- Test: `backend/internal/handler/admin/dashboard_handler_user_breakdown_test.go`

- [ ] **Step 1: 写入 Platform scope 测试**

内容审核和 Prompt Audit 配置改为：

```go
AllPlatforms bool    `json:"all_platforms"`
PlatformIDs []int64 `json:"platform_ids"`
```

测试必须证明平台 7 的请求只命中 `platform_ids:[7]`，API Key 的旧 Group 信息不能影响匹配。

- [ ] **Step 2: 迁移安全审计快照**

事件快照使用 `platform_id/platform_name`，保留请求、用户、Key、模型、协议、命中结果和时间。删除活动查询中的 `group_id/group_name` 过滤。

- [ ] **Step 3: 重写 Ops 和 Dashboard 维度**

- 账号并发、可用率、失败切换：按 Platform 和 Account 聚合。
- 用户用量、成本、Token：按 Platform、SubscriptionPlan 和 `billing_source_type` 聚合。
- 删除 `top_groups`、group availability、group alert metric；替换为 `top_platforms` 和 platform availability。
- 告警过滤器使用 `platform_id`，套餐用量告警使用 `subscription_plan_id`。

- [ ] **Step 4: 重写使用记录字段与筛选**

API 和前端删除 `group_id` 筛选。原“分组”列改为“计费来源”，内容为套餐名称或“余额”；Platform 保持独立列。历史行没有 Platform 时显示 `-`，不得查询旧 Group 补名称。

- [ ] **Step 5: 运行安全、Ops 与统计回归**

```powershell
Set-Location backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/securityaudit ./internal/service ./internal/repository ./internal/handler/admin -run 'Prompt|Moderation|Ops|Dashboard|Usage' -count=1
Set-Location ../frontend
npm run test:run -- src/views/admin/ops src/views/admin/__tests__ src/views/user/__tests__/UsageView.spec.ts
npm run typecheck
```

- [ ] **Step 6: 提交控制面迁移**

按本 Task 的 Files 清单精确暂存并复核 staged diff，提交 `refactor(ops): 统计与策略切换平台维度`。

## Task 7: 用 Platform 重建模型广场和可用服务页面

**Files:**
- Create: `backend/internal/service/platform_available.go`
- Create: `backend/internal/service/platform_plaza.go`
- Create: `backend/internal/handler/available_platform_handler.go`
- Modify: `backend/internal/handler/model_plaza_handler.go`
- Modify: `backend/internal/server/routes/user.go`
- Create: `frontend/src/api/platformCatalog.ts`
- Modify: `frontend/src/api/modelPlaza.ts`
- Create: `frontend/src/views/user/AvailablePlatformsView.vue`
- Create: `frontend/src/components/platforms/AvailablePlatformsTable.vue`
- Modify: `frontend/src/components/modelPlaza/ModelPlazaContent.vue`
- Create: `backend/internal/service/platform_available_test.go`
- Create: `backend/internal/service/platform_plaza_test.go`
- Test: `backend/internal/handler/model_plaza_handler_test.go`
- Delete: `backend/internal/service/channel_available.go`
- Delete: `backend/internal/service/channel_available_test.go`
- Delete: `backend/internal/service/channel_plaza.go`
- Delete: `backend/internal/service/channel_plaza_test.go`
- Delete: `backend/internal/handler/available_channel_handler.go`
- Delete: `frontend/src/api/channels.ts`
- Delete: `frontend/src/views/user/AvailableChannelsView.vue`
- Delete: `frontend/src/components/channels/AvailableChannelsTable.vue`

- [ ] **Step 1: 写入 Platform 展示契约测试**

响应顶层字段改为 `platforms`，每项只包含 Platform 名称、编码、统一端点、启用模型和解析后的参考价格；不得包含 Group ID、Group 倍率、Channel ID 或 Channel 模型映射。

- [ ] **Step 2: 将数据源改为 PlatformRepository 与 ModelPricingCatalog**

可用服务和模型广场不再依赖 `ChannelService`/`GroupRepository`。套餐倍率不混入官方模型基础价格；需要展示套餐时单独列出当前用户可用的 SubscriptionPlan。

- [ ] **Step 3: 更新路由与 UI 名称**

用户路由改为 `/available-platforms`；删除 `/available-channels` 兼容重定向。组件可以在本任务内重命名为 Platform 语义，避免源码继续保留旧配置名称。

- [ ] **Step 4: 运行展示回归并提交**

```powershell
Set-Location backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service ./internal/handler -run 'AvailablePlatform|ModelPlaza' -count=1
Set-Location ../frontend
npm run test:run -- src/components/modelPlaza src/components/platforms
npm run typecheck
```

测试通过后按本 Task 的 Files 清单精确暂存并复核 staged diff，提交 `refactor(platform): 重建可用服务与模型广场`。

## Task 8: 删除旧 Group/Channel 管理域和依赖注入

**Files:**
- Delete: 本计划“文件结构 / 删除”中的全部文件
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/internal/handler/handler.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/internal/repository/wire.go`
- Modify: `backend/cmd/server/wire_gen.go`（通过生成器）
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Delete: `frontend/src/i18n/locales/zh/admin/channels.ts`
- Delete: `frontend/src/i18n/locales/en/admin/channels.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/index.ts`
- Modify: `frontend/src/i18n/locales/en/admin/index.ts`
- Modify: `frontend/src/i18n/locales/zh/common.ts`
- Modify: `frontend/src/i18n/locales/en/common.ts`
- Modify: `frontend/src/types/index.ts`
- Test: `backend/internal/server/api_contract_test.go`
- Test: `frontend/src/components/layout/__tests__/AppSidebar.spec.ts`

- [ ] **Step 1: 写入旧路由不存在的合约测试**

```go
for _, path := range []string{"/api/v1/admin/groups", "/api/v1/admin/channels", "/api/v1/admin/api-keys/1"} {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	require.Equal(t, http.StatusNotFound, serve(req).Code)
}
```

同时测试 `/admin/platforms`、`/admin/model-pricing`、`/admin/subscriptions`、`/admin/subscription-plans` 和 Channel Monitor 路由仍存在。

- [ ] **Step 2: 删除后端旧路由、handler、service 和 repository**

删除注册函数 `registerGroupRoutes`、`registerChannelRoutes` 和管理员 Key 分组重绑。Channel Monitor 注册函数保持不变。

- [ ] **Step 3: 删除前端旧页面、导航和类型**

Sidebar 不再显示“分组管理”“渠道定价”；显示“平台”和“模型价格”。删除 onboarding 中 `/admin/groups` selector 和所有旧 group/channel API 类型。

- [ ] **Step 4: 重新生成 Wire 并运行合约测试**

```powershell
Set-Location backend
& 'C:\Program Files\Go\bin\go.exe' generate ./cmd/server
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/server ./internal/handler/... -run 'Contract|Route|Platform|Pricing' -count=1
Set-Location ../frontend
npm run test:run -- src/components/layout
npm run typecheck
```

- [ ] **Step 5: 提交管理域删除**

按本 Task 的 Files 清单精确暂存并复核 staged diff，提交 `refactor(admin): 删除旧分组渠道管理域`。

## Task 9: 执行一次性数据库转换并物理删除旧 schema

**Files:**
- Create: `backend/migrations/196_remove_legacy_configuration.sql`
- Create: `backend/migrations/196_remove_legacy_configuration_test.go`
- Modify: `backend/ent/schema/platform.go`
- Modify: `backend/ent/schema/subscription_plan.go`
- Modify: `backend/ent/schema/user_subscription.go`
- Modify: `backend/ent/schema/redeem_code.go`
- Modify: `backend/ent/schema/payment_order.go`
- Modify: `backend/ent/schema/usage_log.go`
- Modify: `backend/ent/schema/user.go`
- Modify: `backend/ent/schema/account.go`
- Modify: `backend/ent/schema/api_key.go`
- Regenerate: `backend/ent/`
- Test: `backend/migrations/196_remove_legacy_configuration_test.go`
- Test: `backend/internal/repository/migrations_schema_integration_test.go`

- [ ] **Step 1: 写入迁移静态测试**

测试要求 migration 按以下顺序出现：资产回填、有效资产保护断言、价格迁移、JSON 净化、外键/索引删除、旧列删除、旧表删除、废弃 setting 删除。测试必须拒绝无保护的 `DROP TABLE groups CASCADE`。

- [ ] **Step 2: 写入资产回填 SQL**

核心回填逻辑：

```sql
UPDATE user_subscriptions us
SET subscription_plan_id = (
    SELECT sp.id FROM subscription_plans sp
    WHERE sp.group_id = us.group_id
    ORDER BY sp.for_sale DESC, sp.sort_order ASC, sp.id ASC
    LIMIT 1
)
WHERE us.subscription_plan_id IS NULL
  AND us.group_id IS NOT NULL
  AND EXISTS (SELECT 1 FROM subscription_plans sp WHERE sp.group_id = us.group_id);

UPDATE payment_orders po
SET plan_id = (
    SELECT sp.id FROM subscription_plans sp
    WHERE sp.group_id = po.subscription_group_id
    ORDER BY sp.for_sale DESC, sp.sort_order ASC, sp.id ASC
    LIMIT 1
)
WHERE po.plan_id IS NULL
  AND po.subscription_group_id IS NOT NULL
  AND EXISTS (SELECT 1 FROM subscription_plans sp WHERE sp.group_id = po.subscription_group_id);
```

RedeemCode 和公告条件使用同一确定性规则。对仍有效但无法映射的订阅、未使用订阅码、已支付未完成订单执行 `RAISE EXCEPTION`。

所有引用待删除列的回填都必须放在检查 `information_schema.columns` 的 `DO $$ ... $$` 块中，保证 migration 在列已删除后重复执行也不会报错。

- [ ] **Step 3: 迁移独立模型价格**

将活动 Channel 的 `channel_model_pricing.models` 展开为 `(adapter, model_pattern)`。若同一键存在不同价格，migration 必须终止并输出冲突键；不得用不确定的“第一条”覆盖。

在删除 `platforms.legacy_group_id` 前，先用它回填 `usage_logs`、Prompt Audit、Ops 事件和策略配置的 `platform_id`；无对应 Platform 的历史事件保留主体数据但 Platform 为空。启用中的 Platform scope 策略无法映射时 migration 失败。

- [ ] **Step 4: 净化账号 JSON 和废弃设置**

```sql
UPDATE accounts
SET credentials = credentials - 'model_mapping' - 'model_whitelist' - 'openai_capabilities'
WHERE credentials ?| ARRAY['model_mapping', 'model_whitelist', 'openai_capabilities'];

UPDATE accounts SET schedulable = FALSE WHERE platform_id IS NULL;
DELETE FROM settings WHERE key = 'platform_assets_v2_enabled';
```

- [ ] **Step 5: 删除旧列和表**

显式删除相关外键和索引后，使用 `IF EXISTS` 删除：

```text
api_keys.group_id
platforms.legacy_group_id
subscription_plans.group_id
user_subscriptions.group_id
redeem_codes.group_id
payment_orders.subscription_group_id
usage_logs.group_id
usage_logs.channel_id
scheduler_outbox.group_id
api_key_allowed_groups
account_groups
user_allowed_groups
user_group_rate_multipliers
billing_profiles
composite_model_routes
channel_groups
channel_pricing_intervals
channel_model_pricing
channel_account_stats_pricing_intervals
channel_account_stats_model_pricing
channel_account_stats_pricing_rules
channels
groups
```

Prompt Audit、Ops 和内容审核的旧 group 列/JSON 在 Task 6 已迁移为 Platform 后一并删除。删除 migration 184 中依赖 Group 的旧 auth-cache trigger，改为监听 `api_key_platforms`、`api_key_subscription_plans` 和 Platform 状态。`scheduler_outbox` 增加 `platform_id` 后清空未消费旧 Group 事件并触发一次 Platform 全量缓存重建。不得使用 `CASCADE` 掩盖遗漏依赖。

- [ ] **Step 6: 更新 Ent schema 并生成代码**

```powershell
Set-Location backend
& 'C:\Program Files\Go\bin\go.exe' generate ./ent
& 'C:\Program Files\Go\bin\go.exe' generate ./cmd/server
```

预期：生成目录不再包含 `ent/group*`、`ent/accountgroup*`、`ent/billingprofile*`、`ent/userallowedgroup*` 或 `legacy_group_id`。

- [ ] **Step 7: 运行 migration 与 repository 集成测试**

```powershell
Set-Location backend
& 'C:\Program Files\Go\bin\go.exe' test ./migrations -run TestRemoveLegacyConfigurationMigration -count=1
& 'C:\Program Files\Go\bin\go.exe' test -tags=integration ./internal/repository -run 'Migration|Schema|Subscription|Payment|Pricing' -count=1
```

- [ ] **Step 8: 提交 schema 清理**

按本 Task 的 Files 清单精确暂存 migration、schema 与生成结果，并确认未暂存 `backend/entgen_tmp/`，提交 `refactor(schema): 物理删除旧分组配置`。

## Task 10: 文档一致性、全量验证与部署门禁

**Files:**
- Create: `backend/internal/architecture/legacy_configuration_guard_test.go`
- Modify: `docs/superpowers/specs/2026-08-07-platform-model-authority-design.md`
- Modify: `docs/memory/项目概览.md`
- Modify: `docs/memory/当前状态.md`
- Modify: `docs/memory/决策/2026-08-09-彻底移除旧分组配置.md`
- Test: 全仓库

- [ ] **Step 1: 修正文档冲突**

将旧文档中的“保留旧字段用于回滚”“账号旧 model_mapping 保留”“兼容 read path”标记为已被本计划取代。历史 migration 文档保留原始事实，但文件头注明“历史方案，不作为当前实现依据”。

- [ ] **Step 2: 创建并运行旧配置守卫**

守卫扫描 `backend/internal`、`backend/ent/schema` 和 `frontend/src`，禁止以下活动配置标识：

```go
var forbiddenLegacyConfiguration = []string{
	`json:"group_id"`,
	`json:"group_ids"`,
	"AllowedGroups",
	"AllowedGroupIDs",
	"ResolveBillingGroupForRequest",
	"PricingGroupID",
	"LegacyPricingGroupID",
	"legacy_group_id",
	"api_key_allowed_groups",
	"account_groups",
	"user_allowed_groups",
	"user_group_rate_multipliers",
	"platform_assets_v2_enabled",
}
```

允许范围只能是旧 migration 和明确标记的历史文档；活动 service、handler、repository、router、schema、前端源码与测试均不得进入允许列表。

```powershell
Set-Location backend
& 'C:\Program Files\Go\bin\go.exe' test ./internal/architecture -run TestNoLegacyConfigurationInActiveSource -count=1
```

预期：PASS。允许命中的内容只能位于不可修改的旧 migration 和已标记历史文档。

- [ ] **Step 3: 运行后端全量验证**

```powershell
Set-Location backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/... -count=1 -timeout=20m
& 'C:\Program Files\Go\bin\go.exe' test ./cmd/... ./migrations -count=1
```

预期：全部退出码 `0`。

- [ ] **Step 4: 运行前端全量验证**

```powershell
Set-Location frontend
npm run test:run
npm run typecheck
npm run lint:check
npm run build
```

预期：全部退出码 `0`；只允许既有 Vite chunk 警告。

- [ ] **Step 5: 运行静态残留扫描**

```powershell
Set-Location ..
rg -n 'group_id|group_ids|AllowedGroups|account_groups|api_key_allowed_groups|legacy_group_id|PricingGroupID|platform_assets_v2_enabled' backend/internal backend/ent/schema frontend/src
git diff --check
git status --short --branch
```

预期：`rg` 对旧业务配置零命中；`git diff --check` 退出码 `0`。若 `group` 仅表示 UI 折叠菜单、OAuth group claim 或测试并发 `WaitGroup`，不属于本守卫模式。

- [ ] **Step 6: 服务器迁移前备份和只读盘点**

在腾讯云测试服务器保存 PostgreSQL dump、`data/`、compose、环境文件和当前镜像信息。运行只读 SQL 统计无法转换的活动订阅、兑换码、订单、无 Platform 账号和无 Platform 授权 Key；任何有效资产无法转换时停止部署。

- [ ] **Step 7: 本地提交，但不自动发布**

按本 Task 的 Files 清单精确暂存守卫和文档，运行 `git diff --cached --name-only` 与 `git diff --cached --check`，提交 `docs(my2): 完成旧配置清理验收`。

只有用户明确要求后，才执行 push、创建 `my2-v0.2.x` Tag、等待 GitHub 离线镜像并部署。

- [ ] **Step 8: 真实环境验收矩阵**

至少验证：

```text
GPT /v1/chat/completions：200，套餐扣费，用量记录 Platform/套餐正确
GPT /v1/responses：200，套餐扣费，用量记录 Platform/套餐正确
GLM /v1/chat/completions：按账号三态走 Chat；上游正常时 200
同 Key 两个套餐：先最早到期套餐，用尽后自动切第二个套餐
套餐全部不可用且 Key 允许余额：自动回退余额并使用全局倍率
Key 未授权 Platform：403 API_KEY_PLATFORM_REQUIRED
Key 未授权请求模型：model_not_found
同 Platform 第一账号可重试失败：自动切换同 Platform 第二账号
/v1/models：只返回 Key 授权 Platform 的精确模型规则
```

## 计划自审结果

## 2026-08-09 发布前审计结论

- 已完成：运行时 Platform 鉴权收口、API Key 外部旧字段删除、账号普通模型策略写入净化、Plan-only 资产路径、独立模型价格目录、平台管理/模型价格 UI、前端全量校验和后端 `internal/...` unit 全量校验。
- 未完成且不得标记为完成：Task 6 的 Ops/Usage/安全审计维度迁移、Task 8 的旧 Group/Channel 管理域与依赖注入物理删除、Task 9 的 `196_remove_legacy_configuration.sql` 数据回填/旧列旧表删除/Ent 重新生成、Task 10 的架构守卫和真实环境验收。
- 当前静态残留扫描仍命中活动源码，且计划要求的物理迁移与架构守卫文件尚不存在。直接创建删除旧表的迁移会让仍引用旧实体的内部路径在启动或请求阶段失败，因此本审计结论不允许提交正式发布 Tag。
- 后续必须先完成上述迁移并通过数据库集成测试、全量后端/前端验证和服务器只读盘点，再进入提交、Tag、镜像和部署步骤。

### 需求覆盖

- 旧运行时回退：Task 1。
- API Key 旧字段/API/UI：Task 2。
- Account 分组和旧模型配置：Task 3。
- 保留模型自动价格和手工价格：Task 4。
- 套餐、兑换码、支付和公告去 Group：Task 5。
- 审核、运维、Dashboard、用量去 Group：Task 6。
- 模型广场和可用服务不再依赖 Channel/Group：Task 7。
- 删除管理端旧模块：Task 8。
- 物理删除字段和表：Task 9。
- 文档、全量测试、备份和真实环境验收：Task 10。

### 反向风险审查

1. **误删模型价格入口：已修正。** 先完成 Task 4 的独立价格目录，最后才删除旧 Channel。
2. **误删协议适配：已隔离。** `compact_model_mapping`、OAuth、Responses 模式和 Adapter 内置技术映射不在删除列表。
3. **Spark shadow 回归：已补任务。** 先用 `quota_dimension`/父账号关系替代旧 `model_mapping`，再清理 JSON。
4. **用户资产丢失：已设硬门禁。** migration 对有效订阅、未使用订阅码、已支付未完成订单无法映射时直接失败。
5. **历史审计丢失：已区分。** 删除可配置关联，但保留请求、金额、Token、时间、Platform 和 Plan 快照。
6. **运维盲区：已覆盖。** Ops、Dashboard、Prompt Audit 和内容审核在删除 Group 前先迁移到 Platform/Plan 维度。
7. **一次提交过大：已拆分。** 每个 Task 形成可测试提交；Task 9 才进行物理 schema 删除。
8. **部署不可回滚：已控制。** 服务器先备份，migration 前做只读盘点，Tag/推送/部署仍需用户明确指令。
9. **中间提交携带失败测试：已修正。** 架构守卫放到所有清理完成后的 Task 10 创建，不允许提交预期失败测试。
10. **脏工作区误暂存：已修正。** 所有提交都按精确路径暂存并复核，明确排除临时生成目录和用户文件。
11. **迁移表名漂移：已修正。** 删除清单与 migration 081/082/101/106 的真实表名逐项核对。

### 占位符与一致性检查

- 计划不含 `TBD`、`TODO`、`implement later` 或“类似前一步”的省略描述。
- `PlatformID`、`SubscriptionPlanID`、`billing_source_type`、`adapter/model_pattern` 在各阶段含义一致。
- Channel Monitor 与 Auth Identity Channel 明确排除，不会因名称相同被误删。
- 旧 migration 保持不可变，只新增未占用编号的清理 migration。

## 最终执行记录（2026-08-10）

- 状态：本地实现与验证完成，旧配置清理由 migration 200 和架构守卫收口；服务器备份、部署与真实验收待执行。
- 运行时：Platform 是唯一调度与模型权威；SubscriptionPlan/UserSubscription 和全局余额倍率是唯一资产计费来源。
- 控制面：旧 Group/Channel/BillingProfile/Composite API、页面、注入和设置均已删除；Ops、Usage、风控、通知和模型广场已平台化。
- 数据层：Ent 已重新生成；migration 200 先保护并转换有效资产，再物理删除旧列、旧表、旧凭据键和旧设置。
- 本地验证：后端 internal unit、Ent/migrations/cmd、Repository integration 编译、server build；前端 198/198 Vitest 文件（1340 用例）、typecheck、lint、production build；`git diff --check` 与禁用词扫描通过。
- 发布与部署：用户已明确授权提交、Tag、推送和腾讯云部署；进入 `my2-v0.2.10` 发布阶段，服务器迁移前仍必须完成备份和只读盘点。
