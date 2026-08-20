# Sub2API Adapter Purification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在保持现有外部行为、协议、调度、OAuth、失败切换和计费结果不变的前提下，移除 Sub2API Adapter 内的 Gin/旧 Handler 兼容桥，使所有生产运行时端点只依赖纯 GatewayRuntime 合同。

> **2026-08-12 范围确认：** 本轮优先完成 OpenAI Images、Chat Completions、Responses 和 Messages 的生产 runtime 入口与 executor；Grok、Gemini、Anthropic 其他入口以及 Live/WebSocket 暂不接入本轮。普通 OpenAI HTTP/SSE（含 GPT-5/Codex Messages 状态）已完成 exchange 迁移；Passthrough/WSv2 等显式协议模式仍保留私有兼容 seam，后续单独处理，不把延后模式误记为已完成。

**Architecture:** Gin 仅在最外层 ingress 中转换认证快照、payload、metadata 和 `HTTPExchange`；ApplicationGateway 继续冻结 ProductDecision 并绑定 UsageSink；Sub2API executor 独立拥有账号选择、OAuth、协议 Forward、流式和 failover。按低风险同步端点到高风险流式/状态端点逐步迁移，每阶段保持单一生产路径并可独立发布。

**Tech Stack:** Go 1.26、Gin（仅 HTTP ingress）、标准库 `net/http`、现有 Sub2API service、ApplicationGateway/GatewayRuntime、Go `testing`/`testify`、GitHub Actions、Docker。

---

## 实施约束

- 本计划不新增第二 Runtime、不拆服务、不改数据库 schema、前端、外部 API 或部署拓扑。
- 不改账号调度、OAuth、协议转换、冷却、重试和失败切换算法；只替换调用边界和所有权。
- `gatewayruntime.Request` 不得增加 APIKey、UserSubscription、Balance、BillingAsset 或 Ent 类型。
- Product 预检留在 ingress/ApplicationGateway；账号级并发和失败切换留在 Runtime。
- 生产路径不允许“新 executor 失败后调用 legacy Handler”。
- 每个 Task 先写失败测试，再做最小实现；完成定向测试后才进入下一 Task。
- Git 提交、Tag、推送、部署属于受控操作；执行到相应步骤时必须再次取得用户授权。
- 不暂存或修改用户文件 `backend/entgen_tmp/`、`my2.0.drawio`。

## 文件结构

新增文件按职责拆分：

- `runtime_ingress.go`：Gin 到 ApplicationGateway 的唯一映射，不包含 runtime 算法。
- `sub2api_execution.go`：纯 executor 的请求、route、exchange 和 terminal helper。
- `sub2api_executor_contract_test.go`：所有 executor 共享的终态/failover 合同。
- `sub2api_sync_executor.go`：CountTokens、Embeddings、Alpha Search。
- `sub2api_gemini_media_executor.go`：Gemini Native、图片和视频。
- `sub2api_messages_executor.go`：Gateway Messages/Chat/Responses。
- `sub2api_openai_executor.go`：OpenAI/Grok Messages/Chat/Responses。
- `sub2api_realtime_executor.go`：Live/WebSocket 能力适配。
- `sub2api_adapter_purity_guard_test.go`：legacy 调用点递减和最终零调用点守卫。

### Task 0: 固化 legacy 清单和行为基线

**Files:**
- Create: `backend/internal/architecture/sub2api_adapter_purity_guard_test.go`
- Modify: `backend/internal/handler/runtime_boundary_contract_test.go`
- Modify: `backend/internal/handler/openai_gateway_endpoint_normalization_test.go`
- Modify: `backend/internal/handler/openai_gateway_credential_failover_test.go`
- Modify: `backend/internal/handler/gateway_handler_stream_failover_test.go`
- Modify: `backend/internal/service/gateway_record_usage_test.go`

- [ ] **Step 1: 写 legacy 清单守卫并确认失败**

先写 `TestSub2APIAdapterLegacyCallsitesMatchBaseline`，扫描非测试 Go 文件中以下标识符：

```go
var forbiddenGrowth = []string{
    "legacyGinHandler",
    "legacyEndpointExecutor",
    "dispatchLegacyEndpoint",
    "GinContext() *gin.Context",
}
```

测试把当前精确 `file:identifier` 集合与显式 allowlist 比较；出现新增或漏列都失败。首次只写扫描器、不写 allowlist。

Run:

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/architecture -run Sub2APIAdapterLegacyCallsitesMatchBaseline -count=1
```

Expected: FAIL，输出当前 legacy 调用点列表。

- [ ] **Step 2: 固化当前 allowlist**

把失败输出中的生产调用点逐项写入测试常量。只允许当前文件，不使用目录通配符；后续 Task 只能删除条目。

- [ ] **Step 3: 补齐黑盒行为矩阵**

增加表驱动测试，至少包含：

```go
type runtimeBoundaryCase struct {
    endpoint           string
    mode               openai_compat.ResponsesSupportMode
    firstStatus        int
    secondStatus       int
    wantUpstream       string
    wantAccountID      int64
    wantSuccessfulUses int
}
```

覆盖 Chat 自动、Chat 强制 Responses、Responses 自动、Responses 强制 Chat、Messages->Responses、首账号 502 后第二账号成功、流已写后禁止切换、全部失败不扣费、客户端取消。

- [ ] **Step 4: 运行基线**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/architecture ./internal/handler ./internal/service -run 'Sub2APIAdapterLegacy|RuntimeBoundary|Endpoint|Failover|RecordUsage' -count=1 -timeout=20m
```

Expected: PASS；记录测试数量和耗时，不修改生产行为。

- [ ] **Step 5: 用户授权后提交基线**

```powershell
git add backend/internal/architecture/sub2api_adapter_purity_guard_test.go backend/internal/handler/runtime_boundary_contract_test.go backend/internal/handler/openai_gateway_endpoint_normalization_test.go backend/internal/handler/openai_gateway_credential_failover_test.go backend/internal/handler/gateway_handler_stream_failover_test.go backend/internal/service/gateway_record_usage_test.go
git commit -m "test(runtime): 固化适配器纯化行为基线"
```

### Task 1: 建立纯 ingress 和 executor 执行上下文

**Files:**
- Create: `backend/internal/handler/runtime_ingress.go`
- Create: `backend/internal/handler/runtime_ingress_test.go`
- Create: `backend/internal/handler/sub2api_execution.go`
- Create: `backend/internal/handler/sub2api_execution_test.go`
- Create: `backend/internal/handler/sub2api_executor_contract_test.go`
- Create: `backend/internal/handler/sub2api_runtime_composition.go`
- Modify: `backend/internal/handler/runtime_http_exchange.go`
- Modify: `backend/internal/handler/runtime_http_exchange_test.go`
- Modify: `backend/internal/handler/sub2api_legacy_dispatch.go`

- [ ] **Step 1: 写 ingress 请求映射测试**

测试 `newRuntimeIngress(c, endpoint)`：

```go
require.Equal(t, apiKey.ID, got.Dispatch.Grant.KeyID)
require.Equal(t, subject.UserID, got.Dispatch.Grant.UserID)
require.Equal(t, endpoint, got.Dispatch.Runtime.Endpoint)
require.Equal(t, c.Request.URL.Path, got.Dispatch.Runtime.InboundEndpoint)
require.Equal(t, route.Platform.RequestedModel, got.Dispatch.Product.Model)
require.Same(t, exchange, got.Dispatch.Runtime.Exchange)
```

还要断言 runtime request 的格式化输出不含 `APIKey`、`Subscription`、`Balance` 或 `BillingAsset`。

- [ ] **Step 2: 写纯 execution 测试并确认失败**

定义目标构造器：

```go
execution, err := newSub2APIExecution(ctx, request, sink)
require.NoError(t, err)
require.Equal(t, request.PlatformID, service.PlatformAssetID(execution.Context()))
require.NotNil(t, execution.Exchange())
require.NotNil(t, execution.Request())
```

测试 exchange 缺失、request 缺失、PlatformID 非正数时返回结构化错误；测试生成的 service compatibility context 不含 `BillingAsset`。

Run:

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler -run 'RuntimeIngress|Sub2APIExecution' -count=1
```

Expected: FAIL，目标类型尚不存在。

- [x] **Step 3: 实现 ingress**

`runtime_ingress.go` 只完成：从 middleware 读取 API Key/AuthSubject、复制授权 ID、从已解析 Platform route 取得模型、创建 `NewGinHTTPExchange(c)`、调用 `ApplicationGateway.Dispatch`。公开入口使用：

```go
func dispatchRuntimeEndpoint(c *gin.Context, endpoint gatewayruntime.Endpoint, gateway *applicationgateway.Gateway) error
```

此函数没有 legacy callback 参数；未认证请求返回现有认证错误，不能调用旧 Handler。

同时将 `contextDecisionProvider`、`NewSub2APIProductionApplicationGateway`、端点能力映射、stream 判断和服务引用 helper 从 `sub2api_legacy_dispatch.go` 移到 `sub2api_runtime_composition.go`。这样最终删除 legacy dispatch 文件时不会误删生产 Wire 组合逻辑。

- [x] **Step 4: 实现纯 execution**

`sub2api_execution.go` 定义：

```go
type sub2APIExecution struct {
    ctx      context.Context
    request  gatewayruntime.Request
    exchange gatewayruntime.HTTPExchange
    sink     gatewayruntime.UsageSink
}
```

构造时克隆 `request.Payload`/metadata，使用 `runtimeCompatibilityRoute(request)` 仅附加平台和调度 route，并把 sink 写入 context。提供 `Context()`、`Request()`、`Exchange()`、`Sink()` 和 `RecordFinal()`，不导入 Gin。

- [ ] **Step 5: 建立 executor conformance helper**

共享测试函数固定：成功 exactly-once、失败终态、第二账号归因、部分响应后不切换、client cancel、endpoint marker reset、PlatformAssetID/PlatformSchedulingID。helper 接收 executor factory 和端点列表，不接收 Handler。

- [x] **Step 6: 运行定向测试并缩减 allowlist**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler ./internal/architecture -run 'RuntimeIngress|Sub2APIExecution|ExecutorContract|Sub2APIAdapterLegacy' -count=1
```

Expected: PASS；Task 1 只删除 `dispatchLegacyEndpointWithGateway` 的 ingress 职责，不提前删除仍被端点使用的 legacy executor。

- [ ] **Step 7: 用户授权后提交基础设施**

```powershell
git add backend/internal/handler/runtime_ingress.go backend/internal/handler/runtime_ingress_test.go backend/internal/handler/sub2api_execution.go backend/internal/handler/sub2api_execution_test.go backend/internal/handler/sub2api_executor_contract_test.go backend/internal/handler/runtime_http_exchange.go backend/internal/handler/runtime_http_exchange_test.go backend/internal/handler/sub2api_legacy_dispatch.go backend/internal/architecture/sub2api_adapter_purity_guard_test.go
git commit -m "refactor(runtime): 建立纯入口与执行上下文"
```

### Task 2: 迁移 CountTokens、Embeddings 和 Alpha Search

**Files:**
- Create: `backend/internal/handler/sub2api_sync_executor.go`
- Create: `backend/internal/handler/sub2api_sync_executor_test.go`
- Modify: `backend/internal/handler/openai_gateway_count_tokens.go`
- Modify: `backend/internal/handler/openai_embeddings.go`
- Modify: `backend/internal/handler/openai_alpha_search.go`
- Modify: `backend/internal/handler/sub2api_auxiliary_executor.go`
- Modify: `backend/internal/service/gateway_count_tokens.go`
- Modify: `backend/internal/service/openai_gateway_count_tokens.go`
- Modify: `backend/internal/service/openai_embeddings.go`
- Modify: `backend/internal/service/openai_alpha_search.go`
- Modify: `backend/internal/architecture/sub2api_adapter_purity_guard_test.go`

- [x] **Step 1: 写同步端点失败测试**

测试三个端点只使用 `gatewayruntime.HTTPExchange`：

```go
require.NotPanics(t, func() { _, err = executor.Execute(ctx, request, sink) })
require.NoError(t, err)
require.Equal(t, http.StatusOK, recorder.Code)
require.Len(t, sink.events, 1)
```

CountTokens 断言 non-billing sink 且不选择账号（Grok 本地估算）或只执行 token bridge；Embeddings/Alpha Search 断言首账号 502 后第二账号成功并归因第二账号。

- [x] **Step 2: 运行并确认失败**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler ./internal/service -run 'Sub2APISyncExecutor|CountTokens|Embeddings|AlphaSearch' -count=1
```

原始基线曾因 Forward API 仍要求 `*gin.Context` 而失败；该失败已被记录并由后续运行时桥接实现消除。

- [ ] **Step 3: 将 service Forward 改为 transport-neutral**

把上述 service 的 Forward 参数从 `*gin.Context` 改为 `gatewayruntime.HTTPExchange`；读取请求用 `exchange.Request()`，写响应用 exchange 方法，marker 用 `SetState/State`。不改上游 URL、headers、body/SSE 字节或错误分类。

- [x] **Step 4: 实现 `sub2APISyncExecutor`**

按 `request.Endpoint` 分派 `executeCountTokens`、`executeEmbeddings`、`executeAlphaSearch`。产品预检留在 ingress；executor 只做请求协议校验、账号选择/并发、Forward、failover 和 UsageFacts。

- [x] **Step 5: 替换公开入口**

三个公开 Handler 改为调用 `dispatchRuntimeEndpoint`。删除对应 `legacy*` 方法和 `sub2APIAuxiliaryExecutor.handlerFor` 分支，精确删除 allowlist 条目。

- [x] **Step 6: 验证**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler ./internal/service ./internal/architecture -run 'Sub2APISyncExecutor|CountTokens|Embeddings|AlphaSearch|Failover|RecordUsage|Sub2APIAdapterLegacy' -count=1 -timeout=20m
```

Expected: PASS；生产扫描中三个端点不再命中 legacy 标识符。

> 当前限制：Embeddings、OpenAI `count_tokens`、Alpha Search、Anthropic `count_tokens` 和 Gemini Native `countTokens` 已改为纯 `HTTPExchange` Forward；Gemini generate/stream、媒体和其余 Service Forward 仍通过 `gateway_runtime_exchange.go` 的 Gin 兼容桥承接旧实现，彻底删除 service 内 Gin 依赖仍属于后续收尾工作。

- [ ] **Step 7: 用户授权后提交**

```powershell
git add backend/internal/handler/sub2api_sync_executor.go backend/internal/handler/sub2api_sync_executor_test.go backend/internal/handler/openai_gateway_count_tokens.go backend/internal/handler/openai_embeddings.go backend/internal/handler/openai_alpha_search.go backend/internal/handler/sub2api_auxiliary_executor.go backend/internal/service/gateway_count_tokens.go backend/internal/service/openai_gateway_count_tokens.go backend/internal/service/openai_embeddings.go backend/internal/service/openai_alpha_search.go backend/internal/architecture/sub2api_adapter_purity_guard_test.go
git commit -m "refactor(runtime): 纯化同步端点执行器"
```

### Task 3: 迁移 Gemini Native 与媒体端点

**Files:**
- Create: `backend/internal/handler/sub2api_gemini_media_executor.go`
- Create: `backend/internal/handler/sub2api_gemini_media_executor_test.go`
- Modify: `backend/internal/handler/gemini_v1beta_handler.go`
- Modify: `backend/internal/handler/openai_images.go`
- Modify: `backend/internal/handler/grok_media.go`
- Modify: `backend/internal/handler/sub2api_auxiliary_executor.go`
- Modify: `backend/internal/service/gemini_messages_compat_service.go`
- Modify: `backend/internal/service/antigravity_gateway_gemini.go`
- Modify: `backend/internal/service/openai_images.go`
- Modify: `backend/internal/service/grok_media.go`
- Modify: `backend/internal/architecture/sub2api_adapter_purity_guard_test.go`

- [x] **Step 1: 写 Gemini/媒体 executor 测试**

覆盖 native generateContent/streamGenerateContent、Gemini 429 冷却、平台模型映射、OpenAI/Grok 图片、视频生成/编辑/扩展和视频状态。异步媒体终态幂等键固定为 `taskID + terminalType`。

- [x] **Step 2: 运行并确认失败**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler ./internal/service -run 'Sub2APIGeminiMediaExecutor|Gemini|Image|Video|GrokMedia' -count=1
```

Expected: FAIL，生产 Forward 仍要求 Gin。

- [ ] **Step 3: 中立化 Forward transport**

将本 Task 所列 service 的生产 Forward 改为 `context.Context + gatewayruntime.HTTPExchange`；Multipart/Content-Type、SSE flush、图片/视频响应 body 和 task ID 保持原样。

- [x] **Step 4: 实现并注册 executor**

Gemini 的账号能力、session、429、Antigravity/native 分支留在 Runtime；API Key/Subscription/安全审计/用户并发移到 ingress preflight。Images/Videos 只上报事实，不直接选择 BillingAsset。

- [x] **Step 5: 删除对应 legacy 分支并验证**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler ./internal/service ./internal/architecture -run 'Sub2APIGeminiMediaExecutor|Gemini|Antigravity|Image|Video|Media|Billing|Sub2APIAdapterLegacy' -count=1 -timeout=20m
```

Expected: PASS；Gemini/Images/Videos 生产入口无 legacy 调用。

> 当前进度：已完成 executor 注册、公开入口切换、Gemini/媒体基础路径解析、真实账号 failover、视频任务绑定和直接 `HTTPExchange` 响应桥；Gemini CLI 粘连键与内容摘要回退、Grok multipart header 读取、Runtime request ID、媒体 endpoint 事实兜底已补齐，OpenAI 图片请求解析已改为无 Gin 的协议核心，并通过 Handler/Service 全包单测。仍需将 Gemini/图片/视频 Forward 完整改为纯 `HTTPExchange`，未将本 Task 标记为完成。

> 预检风险：平台/套餐授权和 BillingAsset 冻结已在 ingress/ApplicationGateway；用户级并发、内容安全审计等旧 Handler 产品预检仍需在后续端点迁移中抽出，不能在删除兼容桥前省略。

- [ ] **Step 6: 用户授权后提交**

```powershell
git add backend/internal/handler/sub2api_gemini_media_executor.go backend/internal/handler/sub2api_gemini_media_executor_test.go backend/internal/handler/gemini_v1beta_handler.go backend/internal/handler/openai_images.go backend/internal/handler/grok_media.go backend/internal/handler/sub2api_auxiliary_executor.go backend/internal/service/gemini_messages_compat_service.go backend/internal/service/antigravity_gateway_gemini.go backend/internal/service/openai_images.go backend/internal/service/grok_media.go backend/internal/architecture/sub2api_adapter_purity_guard_test.go
git commit -m "refactor(runtime): 纯化 Gemini 与媒体执行器"
```

### Task 4: 完成 Gateway Messages/Chat/Responses 纯化

**Files:**
- Modify: `backend/internal/handler/sub2api_messages_executor.go`
- Modify: `backend/internal/handler/sub2api_messages_executor_test.go`
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/handler/gateway_handler_chat_completions.go`
- Modify: `backend/internal/handler/gateway_handler_responses.go`
- Modify: `backend/internal/service/gateway_forward.go`
- Modify: `backend/internal/service/gateway_forward_as_chat_completions.go`
- Modify: `backend/internal/service/gateway_forward_as_responses.go`
- Modify: `backend/internal/service/gateway_upstream_response.go`
- Modify: `backend/internal/service/gemini_chat_completions_compat_service.go`
- Modify: `backend/internal/service/antigravity_gateway_claude.go`
- Modify: `backend/internal/service/antigravity_gateway_compat.go`
- Modify: `backend/internal/architecture/sub2api_adapter_purity_guard_test.go`

- [ ] **Step 1: 把现有 executor 测试改为拒绝 Gin carrier**

删除测试中的 `gin.CreateTestContext`，改用纯 recording exchange。增加静态断言：`sub2api_messages_executor.go` 不导入 Gin、不出现 `GinContext()`。

- [ ] **Step 2: 运行并确认失败**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler -run Sub2APIMessagesExecutor -count=1
```

Expected: FAIL，`executeGatewayEndpoint` 仍 type assert `ginContextCarrier`。

- [ ] **Step 3: 中立化 Gateway/Antigravity/Gemini Forward**

修改本 Task 的 service 方法，只用 context、parsed request、account 和 HTTPExchange。保留 session hash、模型映射、流式 ping、writer size、429/502 分类和 account release 顺序。

- [ ] **Step 4: 迁移 executor 方法体**

`executeMessages`、`executeChatCompletions`、`executeResponses` 改为接收 `*sub2APIExecution`，不接收 Gin。产品 preflight 在入口完成；Runtime 循环继续维护 failed account IDs 和 attempt usage。

- [ ] **Step 5: 删除 Gateway legacy 方法**

公开 Handler 只调用 `dispatchRuntimeEndpoint`。删除 `legacyMessages`、`legacyChatCompletions`、`legacyResponses` 和 `openAIHandlerForEndpoint()`；同步缩减 allowlist。

- [ ] **Step 6: 回归**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler ./internal/service ./internal/architecture -run 'Messages|ChatCompletions|Responses|RuntimeBoundary|Failover|RecordUsage|Sub2APIAdapterLegacy' -count=1 -timeout=25m
```

Expected: PASS；Gateway family executor conformance 全通过。

- [ ] **Step 7: 用户授权后提交**

```powershell
git add backend/internal/handler/sub2api_messages_executor.go backend/internal/handler/sub2api_messages_executor_test.go backend/internal/handler/gateway_handler.go backend/internal/handler/gateway_handler_chat_completions.go backend/internal/handler/gateway_handler_responses.go backend/internal/service/gateway_forward.go backend/internal/service/gateway_forward_as_chat_completions.go backend/internal/service/gateway_forward_as_responses.go backend/internal/service/gateway_upstream_response.go backend/internal/service/gemini_chat_completions_compat_service.go backend/internal/service/antigravity_gateway_claude.go backend/internal/service/antigravity_gateway_compat.go backend/internal/architecture/sub2api_adapter_purity_guard_test.go
git commit -m "refactor(runtime): 纯化 Gateway 多协议执行器"
```

### Task 5: 纯化 OpenAI Chat、Responses 与 Messages（Grok 延后）

**Files:**
- Modify: `backend/internal/handler/sub2api_openai_executor.go`
- Modify: `backend/internal/handler/sub2api_openai_executor_test.go`
- Modify: `backend/internal/handler/openai_chat_completions.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/handler/openai_gateway_reasoning_failover.go`
- Modify: `backend/internal/handler/openai_gateway_endpoint_normalization_test.go`
- Modify: `backend/internal/handler/openai_gateway_credential_failover_test.go`
- Modify: `backend/internal/service/openai_gateway_forward.go`
- Modify: `backend/internal/service/openai_gateway_chat_completions.go`
- Modify: `backend/internal/service/openai_gateway_messages.go`
- Modify: `backend/internal/service/openai_gateway_responses_chat_fallback.go`
- Modify: `backend/internal/service/openai_gateway_messages_chat_fallback.go`
- Modify: `backend/internal/service/openai_proxy_stream_circuit.go`
- Modify: `backend/internal/architecture/sub2api_adapter_purity_guard_test.go`

- [x] **Step 1: 写 OpenAI 纯 executor 核心合同**

已覆盖端点分派、502 后切换、实际端点 marker 清理、第二账号归因、成功 exactly-once 用量和失败不扣费。Grok/首输出/部分 SSE 的完整矩阵留到 Grok 或 service transport 抽取阶段，不在本轮扩大范围。

每个 attempt 断言：

```go
require.Equal(t, wantEndpoint, event.Facts.UpstreamEndpoint)
require.Equal(t, wantAccountID, event.Facts.AccountID)
require.Equal(t, 1, terminalCount)
```

- [x] **Step 2: 运行基线并确认旧 fallback 可被替换**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler ./internal/service -run 'Sub2APIOpenAIExecutor|Endpoint|CredentialFailover|ReasoningFailover|FirstOutput' -count=1 -timeout=20m
```

已先以未定义 runtime seam 的测试得到预期编译失败，再完成实现并通过同组回归。

- [x] **Step 3: 中立化 OpenAI Forward transport**

已完成 handler/executor 到 `gatewayruntime.HTTPExchange` 的公开调用边界、每次 attempt 清空 marker 以及 Chat/Responses/Messages 的 endpoint 事实回传。OpenAI API Key 的原生 Chat、Responses->Chat、Messages->Chat 兼容路径已完成纯 exchange 抽取，并通过 service/handler/architecture 全量回归；原生 Responses 的上游请求构造已抽为标准 `*http.Request` 纯函数并切入生产 builder；非流式 JSON/SSE 终态、命名空间/工具还原、compact SSE 桥接和完整流式状态机已抽为纯 exchange sink，并由 Gin/HTTPExchange 两个薄适配器共用。原生 Responses 的请求预处理、账号级协议选择和上游 HTTP attempt 核心也已抽为 `context.Context + gatewayruntime.HTTPExchange`，覆盖代理/账号并发、延迟记录、传输错误、HTTP 错误、failover、JSON/SSE 响应与 usage 事实；Passthrough/WSv2 及其 `Forward` 外层兼容桥仍保留为显式后续协议。

当前拆分状态：

- [x] 请求构造：`openai_gateway_request_runtime.go` 已切入生产 `buildUpstreamRequest`。
- [x] 上游 HTTP attempt：`openai_gateway_forward_runtime.go` 已承接原生 Responses 的标准 HTTP 发送、代理/账号并发、延迟、错误/failover、JSON/SSE 终态与 usage；普通 OpenAI HTTP/SSE 的请求预处理和协议选择也已在 runtime，生产 `Forward` 仅保留 Passthrough/WSv2 等未迁移传输分支的兼容入口。
- [x] 非流式响应：OpenAI 平台的 JSON/SSE 终态、usage、模型替换、命名空间/工具还原和 compact SSE 已切入 runtime exchange；Grok 仍保持旧路径。
- [x] 流式响应：首输出 staging、SSE scanner、客户端断线 drain、OAuth/Codex 事件转换、错误透传、超时和 failover 主循环已迁入无 Gin 的 stream core；Gin/HTTPExchange 仅负责 sink/hooks 适配。普通 OpenAI HTTP/SSE 的请求预处理、账号级协议选择和 executor orchestration 已接入纯 runtime，Passthrough/WSv2 等显式协议仍待独立抽取。

- [x] **Step 4: 把运行时循环迁入 executor**

`sub2APIOpenAIExecutor.Execute` 直接调用 endpoint-specific execution，不保留 `legacyHandler()`。保留原 failed account map、same-account retry、OAuth 429 budget、first-output timeout、stream circuit 和 account release 次序。

- [x] **Step 5: 删除 OpenAI legacy 入口**

普通 OpenAI HTTP/SSE 的 Chat、Images、Embeddings、Alpha Search、Count Tokens、Responses 和 Messages 公开入口及其无生产调用者的旧 Handler 方法体已删除；Passthrough、WSv2、Live/WebSocket 兼容分支仍保留，后续按独立协议处理。

生产配置下公开 Chat/Responses/Messages Handler 已只调用 ingress/ApplicationGateway；普通 OpenAI HTTP/SSE 对应的旧 Handler 方法体已删除。Passthrough、WSv2、Live/WebSocket 等显式协议模式仍保留独立兼容 seam，不属于本轮迁移。

- [x] **Step 6: 运行 OpenAI 高风险回归**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler ./internal/service ./internal/architecture -run 'OpenAI|Grok|ChatCompletions|Responses|Messages|Endpoint|Failover|Stream|Usage|Sub2APIAdapterLegacy' -count=1 -timeout=30m
```

已通过；跨协议/跨账号 marker 无污染，成功一次、失败不扣费，且 OpenAI 三种协议的 Cyber 封禁 envelope 在统一预检中保持一致。

- [ ] **Step 7: 用户授权后提交**

```powershell
git add backend/internal/handler/sub2api_openai_executor.go backend/internal/handler/sub2api_openai_executor_test.go backend/internal/handler/openai_chat_completions.go backend/internal/handler/openai_gateway_handler.go backend/internal/handler/openai_gateway_reasoning_failover.go backend/internal/handler/openai_gateway_endpoint_normalization_test.go backend/internal/handler/openai_gateway_credential_failover_test.go backend/internal/service/openai_gateway_forward.go backend/internal/service/openai_gateway_chat_completions.go backend/internal/service/openai_gateway_messages.go backend/internal/service/openai_gateway_responses_chat_fallback.go backend/internal/service/openai_gateway_messages_chat_fallback.go backend/internal/service/openai_proxy_stream_circuit.go backend/internal/architecture/sub2api_adapter_purity_guard_test.go
git commit -m "refactor(runtime): 纯化 OpenAI 与 Grok 执行器"
```

### Task 6: 为 Live 和 WebSocket 建立独立纯能力端口

**Files:**
- Modify: `backend/internal/gatewayruntime/runtime.go`
- Modify: `backend/internal/gatewayruntime/types.go`
- Modify: `backend/internal/gatewayruntime/runtime_test.go`
- Create: `backend/internal/handler/sub2api_realtime_executor.go`
- Create: `backend/internal/handler/sub2api_realtime_executor_test.go`
- Modify: `backend/internal/handler/openai_live.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/service/openai_live.go`
- Modify: `backend/internal/service/openai_ws_forwarder_ingress.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/architecture/sub2api_adapter_purity_guard_test.go`

- [ ] **Step 1: 写能力合同测试**

定义目标小接口：

```go
type LiveRuntime interface {
    CreateLive(context.Context, LiveRequest) (LiveResult, error)
    ProxyLive(context.Context, LiveProxyRequest) error
}

type WebSocketRuntime interface {
    ServeResponses(context.Context, WebSocketRequest) error
}
```

值对象只包含业务平台 ID、调度 route、模型、payload、连接端口和安全 metadata；不含 APIKey/UserSubscription/Balance/Ent。

- [ ] **Step 2: 运行并确认失败**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/gatewayruntime ./internal/handler -run 'LiveRuntime|WebSocketRuntime|Sub2APIRealtime' -count=1
```

Expected: FAIL，新接口尚不存在。

- [ ] **Step 3: 实现 Sub2API realtime adapter**

包装现有 `CreateLiveCall`、`ProxyLiveSideband` 和 `ProxyResponsesWebSocketFromClient`；连接生命周期和 upstream protocol 不变。Live 调度使用 `PlatformSchedulingID`，持久化 identity 使用正数 `PlatformAssetID`。

- [ ] **Step 4: 接线并移除 generic legacy Live**

Live/sideband/WebSocket 入口做产品身份、BillingAsset 和用户并发预检，再调用小能力端口。删除 `EndpointLive` 的 auxiliary legacy executor；`EndpointWebSocket` 不再作为未注册的保留 Dispatch endpoint。

- [ ] **Step 5: 验证状态链路**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/gatewayruntime ./internal/handler ./internal/service ./internal/architecture -run 'Live|WebSocket|PlatformAssetID|SchedulingNamespace|ClientCancel|Sub2APIAdapterLegacy' -count=1 -timeout=25m
```

Expected: PASS；身份、并发、断线和平台 ID 语义不变。

- [ ] **Step 6: 用户授权后提交**

```powershell
git add backend/internal/gatewayruntime/runtime.go backend/internal/gatewayruntime/types.go backend/internal/gatewayruntime/runtime_test.go backend/internal/handler/sub2api_realtime_executor.go backend/internal/handler/sub2api_realtime_executor_test.go backend/internal/handler/openai_live.go backend/internal/handler/openai_gateway_handler.go backend/internal/service/openai_live.go backend/internal/service/openai_ws_forwarder_ingress.go backend/internal/handler/wire.go backend/internal/architecture/sub2api_adapter_purity_guard_test.go
git commit -m "refactor(runtime): 隔离实时运行时能力"
```

### Task 7: 删除兼容桥并固化零 legacy 架构守卫

**Files:**
- Delete: `backend/internal/handler/sub2api_legacy_dispatch.go`
- Delete: `backend/internal/handler/sub2api_legacy_dispatch_test.go`
- Keep: `backend/internal/handler/sub2api_runtime_composition.go`
- Modify: `backend/internal/handler/runtime_http_exchange.go`
- Modify: `backend/internal/handler/sub2api_auxiliary_executor.go`
- Modify: `backend/internal/handler/sub2api_runtime_registry.go`
- Modify: `backend/internal/handler/sub2api_runtime_registry_test.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/architecture/sub2api_adapter_purity_guard_test.go`
- Modify: `backend/internal/architecture/legacy_configuration_guard_test.go`

- [ ] **Step 1: 将 allowlist 改为零容忍并确认失败**

最终守卫扫描生产文件，以下标识符出现即失败：

```go
var forbiddenRuntimeBridge = []string{
    "legacyGinHandler",
    "legacyEndpointExecutor",
    "ginContextCarrier",
    "dispatchLegacyEndpoint",
    ".GinContext()",
}
```

另用 AST 检查所有 `sub2api_*executor.go` 不导入 Gin，公开 Handler 不调用 `SelectAccount*`、`Forward*`、`RecordUsage*`。

- [ ] **Step 2: 运行并确认剩余点**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/architecture -run 'Sub2APIAdapter|SingleApplicationGatewayPath|PureBoundary' -count=1
```

Expected: FAIL，并只列出待删除兼容桥文件自身。

- [ ] **Step 3: 删除兼容桥和 Gin 反向能力**

删除两个 legacy dispatch 文件；从 `ginHTTPExchange` 删除 `GinContext()`；registry 只注册纯 executor 和明确能力端口。不得新增 fallback 替代它们。

- [ ] **Step 4: 静态扫描和定向测试**

```powershell
rg -n "legacyGinHandler|legacyEndpointExecutor|ginContextCarrier|dispatchLegacyEndpoint|GinContext\(\)" backend/internal -g '*.go' -g '!**/*_test.go'
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/architecture ./internal/applicationgateway ./internal/gatewayruntime ./internal/handler ./internal/service -run 'Sub2APIAdapter|RuntimeBoundary|ExecutorContract|Failover|RecordUsage|Live|WebSocket' -count=1 -timeout=35m
```

Expected: `rg` 无输出；全部测试 PASS。

- [ ] **Step 5: 用户授权后提交**

```powershell
git add -A backend/internal/handler/sub2api_legacy_dispatch.go backend/internal/handler/sub2api_legacy_dispatch_test.go backend/internal/handler/runtime_http_exchange.go backend/internal/handler/sub2api_auxiliary_executor.go backend/internal/handler/sub2api_runtime_registry.go backend/internal/handler/sub2api_runtime_registry_test.go backend/internal/handler/wire.go backend/internal/architecture/sub2api_adapter_purity_guard_test.go backend/internal/architecture/legacy_configuration_guard_test.go
git commit -m "refactor(runtime): 移除旧 Handler 兼容桥"
```

### Task 8: 全量验证、代码评审和项目记忆

**Files:**
- Modify: `docs/memory/当前状态.md`
- Modify: `docs/memory/项目概览.md`
- Modify: `docs/memory/决策/2026-08-11-Sub2API适配器纯化边界.md`
- Verify: `.github/workflows/my2-release.yml`
- Verify: `backend/Makefile`

- [x] **Step 1: 格式化和 diff 检查**

```powershell
cd backend
Get-ChildItem internal/applicationgateway,internal/gatewayruntime,internal/handler,internal/service,internal/architecture -Recurse -Filter *.go | ForEach-Object { & 'C:\Program Files\Go\bin\gofmt.exe' -w $_.FullName }
cd ..
git diff --check
git status --short --branch
```

Expected: `git diff --check` 无错误；用户文件未暂存。

- [x] **Step 2: 后端完整门禁（本地正式包已通过）**

```powershell
cd backend
make test-unit
make test-integration
& 'C:\Program Files\Go\bin\go.exe' test ./migrations ./cmd/... -count=1 -timeout=20m
& 'C:\Program Files\Go\bin\go.exe' build ./cmd/server
```

`backend/Makefile` 的 `test`/`test-unit` 已显式枚举 `./internal/... ./ent/... ./migrations ./cmd/...`，排除被 `.gitignore` 忽略的旧 `tmp/ent` 生成物；本机 `make test-unit`、`make test-integration`、`go build ./cmd/server` 和架构门禁均通过。正式 CI 仍需在后续授权发布时按 Tag 再确认。

- [x] **Step 3: 前端完整门禁**

```powershell
cd ../frontend
pnpm run test:run
pnpm run typecheck
pnpm run lint:check
pnpm run build
```

Expected: 全部退出码 0；只允许既有 chunk/dynamic import 警告。

- [x] **Step 4: 独立代码评审**

逐项审查：Gin 是否越界、产品资产是否进入 Runtime、Handler 是否仍直连 runtime service、endpoint marker 是否跨 attempt 污染、terminal 是否 exactly-once、失败是否扣费、PlatformAssetID/SchedulingID 是否混用、Live/WebSocket 生命周期是否泄漏。

- [x] **Step 5: 更新项目记忆**

只写真实执行结果：完成的 Task、验证命令、残余风险、是否待发布。不得把未执行的 Tag、镜像或服务器测试写成通过。本轮结果已回填 `docs/memory/当前状态.md`。

- [ ] **Step 6: 用户授权后提交收尾**

```powershell
git add docs/memory/当前状态.md docs/memory/项目概览.md docs/memory/决策/2026-08-11-Sub2API适配器纯化边界.md
git commit -m "docs(memory): 记录适配器纯化结果"
```

### Task 9: 发布与腾讯云两轮验收

**Files:**
- Verify: `.github/workflows/my2-release.yml`
- Verify: GitHub Release asset `sub2api_my2_latest.tar`
- Verify: Tencent Cloud `/opt/sub2api`

- [ ] **Step 1: 用户明确授权后提交剩余门禁改动**

精确暂存 `.github/workflows/my2-release.yml`、`backend/Makefile`、`backend/internal/architecture/my2_release_gate_test.go` 及对应文档；再次确认不含 `backend/entgen_tmp/`、`my2.0.drawio`。

- [ ] **Step 2: 推送分支并创建递增 Tag**

```powershell
git push origin my2.0
$versions = git ls-remote --tags --refs origin 'refs/tags/my2-v0.2.*' | ForEach-Object { if ($_ -match 'refs/tags/my2-v0\.2\.(\d+)$') { [int]$Matches[1] } }
$next = (($versions | Measure-Object -Maximum).Maximum) + 1
$tag = "my2-v0.2.$next"
if (git ls-remote --exit-code --tags origin "refs/tags/$tag") { throw "tag already exists: $tag" }
git tag -a $tag -m $tag
git push origin $tag
```

Expected: `source-gate` 确认 Tag commit 属于 `origin/my2.0`，完整门禁开始运行。

- [ ] **Step 3: 等待并验证离线镜像**

确认 GitHub Actions 的 source/backend/frontend/lint/release job 全部成功；下载 tar，校验 SHA-256、gzip、`linux/amd64` 和镜像标签。

- [ ] **Step 4: 服务器备份并只替换应用容器**

在 `/opt/sub2api/backups/<tag>-pre-<timestamp>` 保存 PostgreSQL dump、data、Compose、环境和镜像 inspect。只加载新镜像并重建应用容器；PostgreSQL、Redis、证书和域名不变。

- [ ] **Step 5: 两轮真实验收**

每轮执行 GPT Chat、GPT Responses、GLM Chat；另验证首账号失败后第二账号成功。数据库核对每次成功只有一条 usage、正确平台/账号/实际端点/订阅或余额/倍率/费用；所有失败请求无扣费。

- [ ] **Step 6: 记录发布结果**

把 Tag、commit、workflow、镜像 ID、备份路径、健康状态、两轮结果和数据库核对写入 `docs/memory/当前状态.md`。任何门禁失败都不得更新 `my2-latest` 或标记完成。

### 2026-08-12 OpenAI ordinary Responses HTTP slice

- [x] Added `openai_gateway_forward_preparation_runtime.go` for ordinary native OpenAI Responses HTTP/SSE requests. It performs request normalization, Codex restriction checks, namespace handling, model mapping, image intent/billing metadata, OAuth transforms, fast-policy handling, and pure `HTTPExchange` forwarding.
- [x] `ForwardRuntime` now selects the pure path only for non-passthrough OpenAI HTTP/SSE accounts whose Responses capability is enabled. API-key raw Chat, Responses-to-Chat, Messages-to-Chat, compact, passthrough, and WebSocket paths remain explicit legacy/compatibility branches.
- [x] Added exchange-only request decoding and request-based Codex restriction detection; the default detector is transport-neutral and legacy-only custom detectors fail closed.
- [x] Preserved client `stream` semantics when OAuth preparation changes the upstream body to `stream=true`; response mode is never inferred from the transformed upstream payload.
- [x] Focused service/architecture regressions and the full `internal/service` unit suite pass. Remaining work is limited to the explicitly deferred compatibility branches and outer orchestration seam.
- [x] 2026-08-12 boundary recheck passed: OpenAI-focused Service/Handler/Architecture regression, server build, and diff check all pass; no remote release or deployment was performed.

### 2026-08-12 OpenAI Messages Responses HTTP slice

- [x] Ordinary OpenAI Messages requests on HTTP/SSE accounts now use the pure `gatewayruntime.HTTPExchange` preparation and attempt pipeline; both OAuth and Responses-enabled API-key accounts are covered.
- [x] Added pure Responses-to-Anthropic JSON/SSE conversion, usage extraction, `response.failed` handling, cyber-policy marking, error passthrough and account failover behavior.
- [x] Added service regressions for OAuth non-streaming/streaming Messages, API-key Responses Messages, 502 failover, endpoint marker facts, and the no-Gin architecture guard.
- [x] GPT-5/Codex Messages replay guards, digest/session continuation and OAuth turn-state metadata now use scalar runtime state plus the exchange-only HTTP pipeline. Missing `previous_response_id` is retried once without writing a duplicate downstream response; no legacy fallback is silently triggered after a pure attempt.
- [x] Focused service, handler and architecture tests pass after this slice; no commit, tag, remote push or deployment was performed.

### 2026-08-12 OpenAI GPT-5/Codex Messages state seam closure

### 2026-08-12 OpenAI public HTTP ingress slice

- [x] Gateway `/v1/messages`、`/v1/chat/completions` 和 `/v1/responses` 的公共入口已统一调用 `dispatchRuntimeEndpoint`；Chat 的图片模型前置拒绝保留在 ingress。
- [x] 更新 `sub2api_adapter_purity_guard_test.go` 基线：上述三个公共入口不再包含 `dispatchLegacyEndpoint` 调用点。
- [x] 通过 Handler、Service、Architecture 的 OpenAI/Images/Chat/Responses/Messages/Failover/Usage 定向回归。
- [ ] OpenAI Passthrough、WSv2、Live/WebSocket 仍是明确延后项；本计划不在本切片中删除其兼容桥。

### 2026-08-12 OpenAI public legacy fallback removal

- [x] OpenAI Chat、Responses、Messages 公开 Handler 不再因 `applicationGateway == nil` 回退旧 Handler；Chat 图片模型拒绝和 Responses compact 结果日志保留在 runtime ingress 外层。
- [x] 删除 `sub2APIMessagesExecutor.openAIHandlerForEndpoint` 这条无调用者的 OpenAI legacy 映射。
- [x] 已删除边界清晰且无生产调用者的 OpenAI Chat、Images、Embeddings、Alpha Search、Count Tokens、Responses、Messages 旧方法体；Passthrough/WSv2/Live/WebSocket 等共享兼容体仍保留，后续清理必须先补等价行为测试。

- [x] Added `openai_messages_session_runtime.go` so response ID, turn-state and continuation-disabled state use the API key id supplied by the runtime request instead of Gin context.
- [x] The pure Messages preparation now preserves digest-derived cache keys, replay trimming, todo guard, `previous_response_id`, OAuth turn state and response binding for GPT-5/Codex.
- [x] The pure HTTP attempt loop retries one `previous_response_not_found`/unsupported response without committing a second downstream response, then updates the runtime session state.
- [x] Added regressions for OAuth GPT-5 pure routing, API-key continuation across requests, OAuth turn state and missing-previous-response recovery; architecture guard includes the new runtime state file.

## 完成判据

- [ ] 生产源码 legacy bridge 扫描为零。
- [ ] 所有 executor 不导入 Gin。
- [ ] 公开 Handler 不直连账号选择、Forward 或成功用量记录。
- [ ] 所有端点合同、后端/前端完整门禁、server build 通过。
- [ ] GitHub 发布门禁和离线镜像校验通过。
- [ ] 腾讯云两轮真实请求、失败切换、用量和扣费核对通过。

### 2026-08-12 OpenAI 独立旧入口清理

- [x] 删除 `OpenAIGatewayHandler` 中已无生产调用者的 Chat Completions、Images、Embeddings、Alpha Search 和 OpenAI Count Tokens 旧方法体；对应公开入口只保留 runtime dispatch，图片任务继续复用 keepalive 与 multipart 判定辅助。
- [x] 将这些入口的安全审计顺序覆盖迁移到统一 `runtimeProductPreflight` 静态回归，并补充公开 Images、Embeddings、Alpha Search、Count Tokens 不得回退 legacy 的测试。
- [x] 保留 `Responses`/`Messages` 旧方法体以及 Gateway/Grok/Live/Passthrough/WSv2 兼容分支；它们仍包含共享协议辅助逻辑，后续若继续清理必须先补等价行为测试。
- [x] 本次删除后 handler 与 service 定向回归通过；尚未提交、打 Tag、推送或部署。
