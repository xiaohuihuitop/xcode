# 官方 v0.1.173 重点更新同步实施计划

## 执行结果（2026-08-10）

- 已完成：OAuth pending flow 越权绑定修复、前端 refresh token 轮换并发保护、内容风控 fail-closed、Ops 日志落库退避。
- 已完成：TCP/TLS 显式超时、Codex/OpenAI 容量错误切换、count_tokens 403 回退、错误模型冷却保护、图片请求脱离客户端取消。
- 已完成：金额统一量化到 `NUMERIC(20,8)`、订阅每日额度按配置时区零点重置。
- 已完成：Responses 转 Anthropic 无效块过滤、tool schema `type: null` 修复、Gemini 池模式 429、实际图片数计费和 Gemini 3.6 Flash 模型映射。
- 明确排除：Grok 扩展、Group 利润控制、依赖旧 Channel 的 Channel Monitor V2；未新增任何 Grok 生产代码。
- 暂缓：官方 `db0bff82c` 上游实际响应模型审计。该提交跨越 UsageLog schema、Ent、迁移、所有网关、Repository、Handler 和前端共 76 个文件，必须另立高风险迁移任务适配 Platform/ProductCore 后再实施。
- 验证状态：定向回归、后端全量单测、服务端构建、前端 199 个测试文件/1348 项用例、typecheck、lint 和 production build 均通过；构建仅保留既有 chunk 警告。
- 全量回归曾发现旧 flush 测试仍断言可重试 `error` 帧立即刷新；已同步官方 `14a27f196` 的新契约，并补充不可重试错误立即刷新的反例，复测通过。

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不恢复 Grok、Group/Channel 或旧计费权威的前提下，将官方 v0.1.172、v0.1.173 及当前 main 的安全、稳定性和核心网关修复同步到 `my2.0`。

**Architecture:** 继续保持 ProductCore 与 GatewayRuntime 边界。官方认证、网络传输、OpenAI/Codex、Responses 和 Gemini 修复按原行为移植；涉及订阅、计费、用量和数据库的改动必须适配 Platform、SubscriptionPlan 与独立模型价格目录。官方 Group 利润控制、Grok 扩展和 Channel Monitor V2 不进入本次范围。

**Tech Stack:** Go 1.26、PostgreSQL migration、Ent、Vue 3、TypeScript、Vitest。

---

### Task 1: 认证与控制面安全修复

**Official commits:** `02e50cc22`、`38081ef72`、`e01c917a9`、`e687ca3e9`

**Files:**
- Modify: `backend/internal/handler/auth_oauth_pending_flow.go`
- Modify: `backend/internal/service/content_moderation.go`
- Modify: `backend/internal/service/ops_system_log_sink.go`
- Modify: `frontend/src/api/client.ts`
- Create: `frontend/src/api/tokenRefresh.ts`
- Test: 对应官方提交中的 Go/Vitest 回归文件

- [ ] **Step 1: 仅移植官方回归测试**

```powershell
git diff 02e50cc22^ 02e50cc22 -- '*_test.go' | git apply --3way
git diff 38081ef72^ 38081ef72 -- '*spec.ts' | git apply --3way
git diff e01c917a9^ e01c917a9 -- '*_test.go' | git apply --3way
git diff e687ca3e9^ e687ca3e9 -- '*_test.go' | git apply --3way
```

- [ ] **Step 2: 运行测试并确认因缺少修复而失败**

```powershell
go test -tags=unit ./internal/handler ./internal/service -run 'PendingOAuth|ContentModeration|SystemLogSink' -count=1
npm run test:run -- src/api/__tests__/client.spec.ts src/api/__tests__/tokenRefresh.spec.ts
```

- [ ] **Step 3: 移植最小生产实现并解决本分支契约差异**

```powershell
git diff 02e50cc22^ 02e50cc22 -- ':!*_test.go' | git apply --3way
git diff 38081ef72^ 38081ef72 -- ':!*spec.ts' | git apply --3way
git diff e01c917a9^ e01c917a9 -- ':!*_test.go' | git apply --3way
git diff e687ca3e9^ e687ca3e9 -- ':!*_test.go' | git apply --3way
```

- [ ] **Step 4: 运行定向测试、前端类型检查和架构守卫**

```powershell
go test -tags=unit ./internal/handler ./internal/service -run 'PendingOAuth|ContentModeration|SystemLogSink' -count=1
npm run test:run -- src/api/__tests__/client.spec.ts src/api/__tests__/tokenRefresh.spec.ts
npm run typecheck
go test ./internal/architecture -count=1
```

### Task 2: 网络传输与 Codex/OpenAI 故障切换

**Official commits:** `66ad405dd`、`c33c3208e`、`e1b76e224`、`2eb24814f`、`dbb42881c`、`815035fcc`、`915cc7e7b`、`e93f6b995`、`b5d9fd21b`、`cbf2be05a`

**Files:**
- Modify: `backend/internal/pkg/proxyutil/dialer.go`
- Modify: `backend/internal/repository/http_upstream.go`
- Modify: `backend/internal/service/openai_gateway_passthrough.go`
- Modify: `backend/internal/service/openai_gateway_response_handling.go`
- Modify: `backend/internal/service/openai_codex_identity.go`
- Modify: `backend/internal/service/openai_gateway_forward.go`
- Modify: `backend/internal/service/openai_gateway_count_tokens.go`
- Modify: `backend/internal/service/ratelimit_service.go`
- Modify: `backend/internal/service/openai_images.go`
- Test: 对应官方提交中的 Go 回归文件

- [ ] **Step 1: 按提交顺序仅引入回归测试并逐组运行 RED**

```powershell
go test -tags=unit ./internal/pkg/proxyutil ./internal/repository ./internal/service -run 'Dial|Capacity|LoadShed|CodexIdentity|RoutingHint|CountTokens|RateLimit|Image' -count=1
```

- [ ] **Step 2: 移植 transport、failover、Codex 身份和路由提示实现**

实现必须保留 `PlatformSchedulingID` 与 `PlatformAssetID` 隔离，并保证每次兼容转发覆盖实际上游端点。

- [ ] **Step 3: 移植 count_tokens、图片冷却和非流式生图脱离客户端取消修复**

```powershell
go test -tags=unit ./internal/service -run 'CountTokens|RateLimit|ImageUpstreamContext' -count=1
```

- [ ] **Step 4: 运行 OpenAI/Codex 全部定向测试**

```powershell
go test -tags=unit ./internal/pkg/openai ./internal/pkg/proxyutil ./internal/repository ./internal/service -run 'OpenAI|Codex|Capacity|RoutingHint|DialTimeout|CountTokens|Image' -count=1
```

### Task 3: 账单精度与订阅每日重置

**Official commits:** `e2652eb85`、`99b357083`

**Files:**
- Modify: `backend/internal/service/usage_billing.go`
- Modify: `backend/internal/service/subscription_service.go`
- Modify: `backend/internal/service/user_subscription.go`
- Modify: `backend/internal/service/user_subscription_port.go`
- Modify: `backend/internal/repository/user_subscription_repo.go`
- Test: `backend/internal/service/usage_billing_quantize_test.go`
- Test: `backend/internal/service/subscription_daily_midnight_reset_test.go`

- [ ] **Step 1: 移植量化和午夜重置回归测试并确认 RED**

```powershell
go test -tags=unit ./internal/service ./internal/repository -run 'Quantize|Midnight|DailyReset' -count=1
```

- [ ] **Step 2: 将金额统一量化到 NUMERIC(20,8)**

量化只作用于最终基础费用、套餐倍率或余额倍率计算结果，不恢复 Group 倍率或叠乘。

- [ ] **Step 3: 将每日额度锚点恢复为配置时区零点**

套餐实例继续使用 `UserSubscription` 快照和独立 daily/weekly/monthly 窗口，不能回退到官方 Group 套餐解析。

- [ ] **Step 4: 运行套餐、计费和 API Key 资产授权回归**

```powershell
go test -tags=unit ./internal/service ./internal/repository ./internal/server/middleware -run 'Billing|Subscription|AssetPermission|DailyQuota|Reset' -count=1
```

### Task 4: Responses 与 Gemini 兼容修复

**Official commits:** `64090de66`、`f3c94d209`、`cbc2a3dd4`、`b6eb6c1ef`、`ce1498313`

**Files:**
- Modify: `backend/internal/pkg/apicompat/responses_to_anthropic_request.go`
- Create: `backend/internal/service/openai_responses_tool_schema.go`
- Modify: `backend/internal/service/openai_gateway_forward.go`
- Modify: `backend/internal/service/gemini_chat_completions_compat_service.go`
- Modify: `backend/internal/service/gemini_messages_compat_service.go`
- Create: `backend/internal/service/gemini_image_output_accounting.go`
- Modify: `backend/internal/domain/constants.go`
- Test: 对应官方提交中的 Go 回归文件

- [ ] **Step 1: 引入无效 content block、null tool schema、Gemini 429 和图片计数测试并确认 RED**

```powershell
go test -tags=unit ./internal/pkg/apicompat ./internal/service ./internal/domain -run 'InvalidBlocks|ToolSchema|Gemini|ImageOutput' -count=1
```

- [ ] **Step 2: 移植 Responses 请求净化和 tool schema 修复**

每条上游尝试仍必须写入正确的 Chat/Responses 实际端点 marker。

- [ ] **Step 3: 移植 Gemini 池模式 429 和实际图片数量计费**

计费结果必须继续写入 Platform、SubscriptionPlan 或余额资产，不引入 Group 价格读取。

- [ ] **Step 4: 运行 Responses、Chat、Messages 和 Gemini 定向回归**

```powershell
go test -tags=unit ./internal/pkg/apicompat ./internal/service -run 'Responses|ChatCompletions|Messages|ActualEndpoint|Gemini' -count=1
```

### Task 5: 上游实际模型审计

**Official commit:** `db0bff82c`

**Files:**
- Modify: `backend/ent/schema/usage_log.go`
- Regenerate: `backend/ent/`
- Create: `backend/migrations/194_add_usage_log_upstream_response_model.sql`
- Create: `backend/migrations/195_add_usage_log_upstream_model_mismatch_index_notx.sql`
- Create: `backend/internal/service/upstream_response_model.go`
- Modify: `backend/internal/service/*gateway*`
- Modify: `backend/internal/repository/usage_log_repo_*.go`
- Modify: `backend/internal/handler/admin/usage_handler.go`
- Modify: `frontend/src/components/admin/usage/UsageFilters.vue`
- Modify: `frontend/src/components/admin/usage/UsageTable.vue`
- Test: 官方提交中的 upstream response model、usage handler/repository 与前端用量测试

- [ ] **Step 1: 先移植服务层解析测试并确认 RED**

```powershell
go test -tags=unit ./internal/service -run 'UpstreamResponseModel' -count=1
```

- [ ] **Step 2: 新增 UsageLog 字段和 migration**

migration 文件名保持官方名称，runner 按完整 filename 管理；SQL 不得引用已删除的 Group/Channel。

- [ ] **Step 3: 在各网关成功路径记录上游声明模型**

审计字段使用最终上游模型和 Platform 业务 ID，不得写入负数调度 namespace。

- [ ] **Step 4: 接入 Repository、Handler 与前端筛选展示**

```powershell
go test -tags=unit ./internal/repository ./internal/handler ./internal/service -run 'UpstreamResponseModel|Usage' -count=1
npm run test:run -- src/components/admin/usage src/views/admin/__tests__/UsageView.spec.ts
npm run typecheck
```

### Task 6: 全量验证、迁移演练与记忆回填

**Files:**
- Modify: `docs/memory/当前状态.md`
- Create: `docs/memory/决策/2026-08-10-官方重点更新同步边界.md`

- [ ] **Step 1: 验证未重新引入排除域**

```powershell
go test ./internal/architecture -count=1
rg -n 'GroupID|group_id|group_ids|account_groups|BillingProfile' backend/internal frontend/src
```

- [ ] **Step 2: 运行后端全量单测和构建**

```powershell
go test -tags=unit ./internal/... -count=1 -timeout=20m
go test ./cmd/... -count=1
go build ./cmd/server
```

- [ ] **Step 3: 运行前端全量验证**

```powershell
npm run test:run
npm run typecheck
npm run lint:check
npm run build
```

- [ ] **Step 4: 使用服务器备份数据库执行新增 migration 演练**

确认新 migration 在已执行 migration 200、已删除 Group/Channel 的 schema 上成功，且不修改现网数据。

- [ ] **Step 5: 回填项目记忆并检查差异**

```powershell
git diff --check
git status --short
```
