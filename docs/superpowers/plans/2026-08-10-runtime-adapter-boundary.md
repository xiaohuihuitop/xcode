# Runtime Adapter Boundary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不改变 HTTP、数据库、前端、Docker、调度、协议和计费结果的前提下，将现有 Sub2API 运行链路收口为唯一 `Sub2APIRuntimeAdapter`，使 ProductCore 只通过稳定的 Runtime、Pricing 和 Usage 端口使用内核。

**Architecture:** `applicationgateway.Gateway` 负责把 ProductCore 决策变成纯 `gatewayruntime.Request`，`sub2apiadapter.Runtime` 负责桥接现有账号、OAuth、协议、流式、重试与失败切换实现，`productusage.Sink` 负责唯一终态用量和扣费。迁移期间按端点逐条切换；每条生产路由只有一个执行入口，禁止新路径失败后回退旧路径。

**Tech Stack:** Go 1.26、Gin、Wire、Ent/PostgreSQL、Redis、现有 Sub2API Gateway 服务、Go `testing`/`testify`、Vitest、Docker/GitHub Actions。

---

## 实施约束

- 本计划只重构进程内边界，不增加第二套 Runtime、HTTP/RPC 内部服务、数据库 schema、前端功能或依赖。
- 当前 `GatewayService`、`OpenAIGatewayService`、Gemini/Antigravity 服务中的调度、OAuth、协议转换、冷却、重试、流式解析算法保持原样；只移动调用所有权并增加适配。
- `productcore` 与 `gatewayruntime` 只能依赖标准库，不得导入 Gin、Ent、`internal/service`。
- Runtime 请求中不得出现 API Key、User、Subscription、Balance、倍率或 Ent 实体；这些对象仅存在于 ProductCore/Usage 侧。
- 每切换一个端点，必须先让原行为契约测试通过，再删除该端点的旧直连调用；禁止长期双写或静默 fallback。
- 第一阶段保持当前定价缺失时的外部结果，不借重构顺带改变为 fail-closed。定价 fail-closed 作为边界完成后的独立行为变更处理，避免违反“行为不变”。
- 不修改已发布 migration `192-200`；本阶段不新增 SQL migration。
- 工作树中的 `backend/entgen_tmp/` 与 `my2.0.drawio` 属于用户文件，不纳入任何提交。

## Task 0：建立可比较的行为基线

**Files:**
- Create: `backend/internal/handler/runtime_boundary_contract_test.go`
- Modify: `backend/internal/handler/openai_gateway_endpoint_tracking_test.go`
- Modify: `backend/internal/service/gateway_record_usage_test.go`
- Modify: `backend/internal/service/openai_gateway_usage_test.go`
- Test: `backend/internal/handler/runtime_boundary_contract_test.go`

- [x] **Step 1: 写端点行为契约测试**

在 `runtime_boundary_contract_test.go` 用现有 Handler 与 stub upstream 固定以下场景：

```go
func TestRuntimeBoundaryBaseline(t *testing.T) {
    tests := []struct {
        name             string
        inboundEndpoint  string
        responseMode     string
        expectedUpstream string
        expectedStatus   int
    }{
        {"chat auto", "/v1/chat/completions", "auto", "/v1/chat/completions", http.StatusOK},
        {"chat force responses", "/v1/chat/completions", "force_responses", "/v1/responses", http.StatusOK},
        {"responses force chat", "/v1/responses", "force_chat_completions", "/v1/chat/completions", http.StatusOK},
        {"messages via responses", "/v1/messages", "auto", "/v1/responses", http.StatusOK},
    }
    // 使用现有测试夹具运行每个 case，并断言实际端点、状态和用量字段。
}
```

- [x] **Step 2: 补齐成功、失败和切换的计费基线**

测试至少断言：成功只生成一条用量且只扣一次；首账号 502、第二账号成功只按第二账号归因；所有账号失败不扣费；平台业务 ID 为正数，调度命名空间保持负数；套餐优先、套餐耗尽切下一实例、无有效套餐时按 Key 授权回退余额。

- [x] **Step 3: 运行基线测试并保存结果**

Run:

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler ./internal/service -run 'RuntimeBoundaryBaseline|EndpointTracking|RecordUsage|BillingAsset|Failover' -count=1 -timeout=20m
```

Expected: PASS。若现状失败，只修测试夹具或确认真实缺陷；不得在本任务改生产行为。

- [ ] **Step 4: 提交基线**

```powershell
git add backend/internal/handler/runtime_boundary_contract_test.go backend/internal/handler/openai_gateway_endpoint_tracking_test.go backend/internal/service/gateway_record_usage_test.go backend/internal/service/openai_gateway_usage_test.go
git commit -m "test(runtime): 固化现有网关与计费行为"
```

## Task 1：建立纯 GatewayRuntime 契约

**Files:**
- Modify: `backend/internal/gatewayruntime/intent.go`
- Create: `backend/internal/gatewayruntime/types.go`
- Create: `backend/internal/gatewayruntime/runtime.go`
- Create: `backend/internal/gatewayruntime/errors.go`
- Create: `backend/internal/gatewayruntime/exchange.go`
- Create: `backend/internal/gatewayruntime/terminal.go`
- Create: `backend/internal/gatewayruntime/runtime_test.go`
- Modify: `backend/internal/architecture/legacy_configuration_guard_test.go`

- [x] **Step 1: 先写纯契约和终态幂等测试**

```go
func TestTerminalRecorderRecordsExactlyOnce(t *testing.T) {
    sink := &recordingSink{}
    recorder := gatewayruntime.NewTerminalRecorder(sink)
    require.NoError(t, recorder.RecordFinal(context.Background(), successEvent()))
    require.ErrorIs(t, recorder.RecordFinal(context.Background(), successEvent()), gatewayruntime.ErrTerminalAlreadyRecorded)
    require.Len(t, sink.events, 1)
}
```

再写 AST/导入守卫，断言 `internal/productcore` 和 `internal/gatewayruntime` 不导入 Gin、Ent、`internal/service`。

- [x] **Step 2: 运行测试，确认先失败**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/gatewayruntime ./internal/architecture -run 'GatewayRuntime|TerminalRecorder|PureBoundary' -count=1
```

Expected: FAIL，缺少新类型和接口。

- [x] **Step 3: 实现最小纯契约**

`types.go` 定义只读值对象：

```go
type Request struct {
    RequestID       string
    PlatformID      int64
    PlatformCode    string
    Adapter         string
    Endpoint        Endpoint
    InboundEndpoint string
    RequestedModel  string
    UpstreamModel   string
    Stream          bool
    Payload         []byte
    Metadata        RequestMetadata
    Exchange        HTTPExchange
}

type Result struct {
    StatusCode       int
    AccountID        int64
    UpstreamEndpoint string
    UpstreamModel    string
    Response         Response
}

type Response struct {
    Header   http.Header
    Body     []byte
    Streamed bool
}

type UsageFacts struct {
    InputTokens, OutputTokens                 int
    CacheCreationTokens, CacheReadTokens      int
    ImageInputTokens, ImageOutputTokens       int
    ImageCount, VideoCount                    int
    FirstTokenMilliseconds, DurationMilliseconds int64
    AccountID                                 int64
    UpstreamEndpoint, UpstreamModel           string
}
```

`exchange.go` 定义标准库 HTTP 传输端口，支持现有流式响应但不暴露 Gin：

```go
type HTTPExchange interface {
    Request() *http.Request
    Header() http.Header
    WriteHeader(int)
    Write([]byte) (int, error)
    Flush()
    Written() bool
    Size() int
    SetState(string, any)
    State(string) (any, bool)
}
```

`Endpoint` 使用固定枚举区分 Messages、Chat Completions、Responses、Gemini Native、Embeddings、Alpha Search、Image、Video、Count Tokens 和 Live/WebSocket。`SetState/State` 只允许运行时 marker（实际端点、stream started、Ops 时延），不得存放 ProductDecision 或 BillingAsset。

`runtime.go` 定义：

```go
type GatewayRuntime interface {
    Dispatch(context.Context, Request, UsageSink) (Result, error)
}

type TokenCounter interface {
    CountTokens(context.Context, Request) (TokenCountResult, error)
}

type AccountRuntime interface {
    ProbeAccount(context.Context, AccountProbeRequest) (AccountProbeResult, error)
}

type PricingEngine interface {
    Quote(context.Context, PricingRequest) (PricingQuote, error)
}

type UsageSink interface {
    RecordFinal(context.Context, UsageEvent) error
}
```

`errors.go` 使用固定类别：`credential_invalid`、`upstream_forbidden`、`rate_limited`、`upstream_timeout`、`upstream_5xx`、`no_available_account`、`invalid_upstream_response`、`client_cancelled`。

- [x] **Step 4: 将旧 DispatchIntent 改为运行时路由，不再携带 BillingAsset**

保留现有文件名以减少 import churn，但删除 `productcore` import 和 `BillingAsset` 字段；上下文仅复制 `PlatformID`、`PlatformCode`、`Adapter`、模型和端点信息。

- [x] **Step 5: 运行测试**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/gatewayruntime ./internal/productcore ./internal/architecture -count=1
```

Expected: PASS。

- [ ] **Step 6: 提交契约**

```powershell
git add backend/internal/gatewayruntime backend/internal/architecture/legacy_configuration_guard_test.go
git commit -m "refactor(runtime): 建立纯运行时契约"
```

## Task 2：建立 ApplicationGateway 与 ProductDecision 快照

**Files:**
- Create: `backend/internal/applicationgateway/gateway.go`
- Create: `backend/internal/applicationgateway/request.go`
- Create: `backend/internal/applicationgateway/gateway_test.go`
- Modify: `backend/internal/productcore/types.go`
- Modify: `backend/internal/service/product_core_adapter.go`
- Create: `backend/internal/service/product_decision_provider.go`
- Create: `backend/internal/service/product_decision_provider_test.go`

- [x] **Step 1: 写应用门面测试**

覆盖：ProductCore 失败时 Runtime 不被调用；成功时只把平台路由写入 Runtime Request；BillingAsset 不进入 Runtime；Runtime 成功/失败均由同一个 terminal recorder 收口。

```go
func TestGatewayDoesNotExposeBillingAssetToRuntime(t *testing.T) {
    runtime := &capturingRuntime{}
    gateway := applicationgateway.New(deciderWithSubscription(), runtime, recordingSink())
    _, err := gateway.Dispatch(context.Background(), requestWithPayload())
    require.NoError(t, err)
    require.Equal(t, int64(7), runtime.request.PlatformID)
    require.NotContains(t, fmt.Sprintf("%#v", runtime.request), "Subscription")
}
```

- [x] **Step 2: 运行并确认失败**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/applicationgateway ./internal/service -run 'Gateway|ProductDecisionProvider' -count=1
```

Expected: FAIL，应用包和 provider 尚不存在。

- [x] **Step 3: 实现应用门面**

`applicationgateway.Request` 只包含 ProductCore 请求、授权快照、payload 和传输 metadata。`Gateway.Dispatch` 顺序固定为：resolve decision -> build runtime request -> create terminal recorder -> runtime dispatch -> require terminal state。不得读取 Gin context 或 service 实体。

- [x] **Step 4: 实现 ProductDecisionProvider**

将当前 `PlatformAssetProductCoreAdapter.Resolve` 变成 provider 的唯一生产实现。返回：

```go
type DecisionSnapshot struct {
    Decision productcore.Decision
    Grant    productcore.AccessGrant
}
```

ApplicationGateway 通过应用层小端口把决策绑定到 UsageSink：

```go
type UsageSinkFactory interface {
    ForDecision(DecisionSnapshot) gatewayruntime.UsageSink
}
```

Runtime 只看到工厂返回的 `gatewayruntime.UsageSink`，看不到决策内容。订阅实体不进入 Runtime；Usage 侧后续凭 `DecisionSnapshot.Decision.BillingAsset.SubscriptionID` 加载/锁定当前实体。保持现有错误映射。

- [x] **Step 5: 运行测试**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/applicationgateway ./internal/productcore ./internal/service -run 'Gateway|ProductDecision|Authorizer|PlatformAsset' -count=1
```

Expected: PASS。

- [ ] **Step 6: 提交应用门面**

```powershell
git add backend/internal/applicationgateway backend/internal/productcore/types.go backend/internal/service/product_core_adapter.go backend/internal/service/product_decision_provider.go backend/internal/service/product_decision_provider_test.go
git commit -m "refactor(gateway): 建立产品决策应用门面"
```

## Task 3：独立 PricingEngine 与 Product UsageSink

**Files:**
- Create: `backend/internal/service/sub2api_pricing_adapter.go`
- Create: `backend/internal/service/sub2api_pricing_adapter_test.go`
- Create: `backend/internal/service/product_usage_sink.go`
- Create: `backend/internal/service/product_usage_sink_test.go`
- Modify: `backend/internal/service/gateway_usage_billing.go`
- Modify: `backend/internal/service/openai_gateway_usage.go`
- Modify: `backend/internal/service/usage_repository_test_stub_test.go`

- [x] **Step 1: 写定价适配器测试**

覆盖普通 token、cache read/write、图片、视频、service tier、long context 和模型候选链；断言适配器只返回基础价格，不应用套餐或余额倍率。

- [x] **Step 2: 写 UsageSink 测试**

覆盖：成功事件按 `DecisionSnapshot.Decision.BillingAsset` 应用一次倍率和扣费；订阅 ID 被重新加载并在事务内扣减；余额不填 subscription ID；失败/取消只记录 Ops 终态且不调用 `UsageBillingRepository.Apply`；重复幂等键只产生一条用量。

- [x] **Step 3: 运行并确认失败**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run 'Sub2APIPricingAdapter|ProductUsageSink' -count=1
```

Expected: FAIL，新适配器尚不存在。

- [x] **Step 4: 实现 Sub2APIPricingAdapter**

复用现有 `PricingService`、模型候选、图片/视频和 long-context 计算函数；适配器不得读取 Subscription 或余额倍率。现有端点在找不到价格时的行为保持不变并由 Task 0 契约锁定。

- [x] **Step 5: 实现 ProductUsageSink**

`RecordFinal` 接收不可变 Decision 快照和 UsageFacts，构造现有 `UsageLog` 与 `UsageBillingCommand`，复用 `applyUsageBilling` 的事务和幂等语义。先让旧 `GatewayService.RecordUsage` 与 `OpenAIGatewayService.RecordUsage` 委托 Sink，再切端点，避免一次性改所有调用者。

- [x] **Step 6: 验证定价和扣费行为不变**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service ./internal/repository -run 'Pricing|RecordUsage|UsageBilling|Subscription|Balance|Idempot' -count=1 -timeout=20m
```

Expected: PASS。

- [ ] **Step 7: 提交 Pricing/Usage 边界**

```powershell
git add backend/internal/service/sub2api_pricing_adapter.go backend/internal/service/sub2api_pricing_adapter_test.go backend/internal/service/product_usage_sink.go backend/internal/service/product_usage_sink_test.go backend/internal/service/gateway_usage_billing.go backend/internal/service/openai_gateway_usage.go backend/internal/service/usage_repository_test_stub_test.go
git commit -m "refactor(billing): 收口定价与终态用量入口"
```

## Task 4：建立 Sub2APIRuntimeAdapter 和端点执行器注册表

**Files:**
- Create: `backend/internal/handler/runtime_http_exchange.go`
- Create: `backend/internal/handler/runtime_http_exchange_test.go`
- Create: `backend/internal/handler/sub2api_runtime_adapter.go`
- Create: `backend/internal/handler/sub2api_runtime_registry.go`
- Create: `backend/internal/handler/sub2api_runtime_adapter_test.go`
- Modify: `backend/internal/service/gateway_runtime_bridge.go`
- Modify: `backend/internal/service/platform_asset_request.go`
- Modify: `backend/internal/service/platform_asset_request_test.go`
- Modify: `backend/internal/handler/wire.go`

- [x] **Step 1: 写注册、映射和错误标准化测试**

断言每个支持端点都有且只有一个 executor；未知端点返回 `invalid_upstream_response`；503 无账号、429、403、timeout、502 和客户端取消映射到固定 RuntimeError；错误中不含 credential/token/payload。

- [x] **Step 2: 运行并确认失败**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run 'Sub2APIRuntimeAdapter|RuntimeRegistry|RuntimeError' -count=1
```

Expected: FAIL。

- [x] **Step 3: 实现 Adapter 骨架**

```go
type Sub2APIRuntimeAdapter struct {
    executors map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor
}

func (a *Sub2APIRuntimeAdapter) Dispatch(
    ctx context.Context,
    request gatewayruntime.Request,
    sink gatewayruntime.UsageSink,
) (gatewayruntime.Result, error) {
    executor, ok := a.executors[request.Endpoint]
    if !ok {
        return gatewayruntime.Result{}, gatewayruntime.NewError(gatewayruntime.ErrorInvalidUpstreamResponse, false)
    }
    return executor.Execute(ctx, request, gatewayruntime.NewTerminalRecorder(sink))
}
```

Adapter 内部可以构造兼容上下文，但 `gatewayruntime.Request` 和返回值不得反向携带兼容对象。当前兼容阶段允许在 Adapter 内部通过私有 `ginContextCarrier` 取回 Gin，仅用于调用尚未提取的旧 handler；纯 Runtime/ApplicationGateway 合同本身仍不依赖 Gin。后续逐端点提取 executor 时必须删除对应 carrier 使用。

`runtime_http_exchange.go` 是唯一公开的 Gin bridge，将 `*gin.Context` 适配为 `gatewayruntime.HTTPExchange`。兼容 Adapter 内部的私有 carrier 是过渡措施，不得进入纯合同或 ProductCore；现有依赖 Gin 的转发 helper 随对应端点任务逐步改为接收 `HTTPExchange`，确保边界逐步脱离旧 handler。

- [x] **Step 4: 收窄 gateway_runtime_bridge**

旧 bridge 只保留 Runtime route -> 现有调度 context 的转换；移除 BillingAsset 从 `DispatchIntent` 的双写。BillingAsset 只由 Product UsageSink 持有。

- [x] **Step 5: Wire 注册唯一生产 Runtime**

在 `handler/wire.go` 提供 `Sub2APIRuntimeAdapter` 并绑定 `gatewayruntime.GatewayRuntime`。Adapter 位于 HTTP 适配层，可以依赖现有 service，但 Runtime 契约和 ApplicationGateway 不反向依赖 handler。此时先不切公开 Handler，确保编译但不产生双路径。

- [x] **Step 6: 运行测试**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/gatewayruntime ./internal/service -run 'Runtime|PlatformAsset|SchedulingScope' -count=1
& 'C:\Program Files\Go\bin\go.exe' test ./cmd/... -run '^$' -count=1
```

Expected: PASS。

- [ ] **Step 7: 提交 Adapter 骨架**

```powershell
git add backend/internal/handler/runtime_http_exchange.go backend/internal/handler/runtime_http_exchange_test.go backend/internal/handler/sub2api_runtime_adapter.go backend/internal/handler/sub2api_runtime_registry.go backend/internal/handler/sub2api_runtime_adapter_test.go backend/internal/service/gateway_runtime_bridge.go backend/internal/service/platform_asset_request.go backend/internal/service/platform_asset_request_test.go backend/internal/handler/wire.go
git commit -m "refactor(runtime): 建立 Sub2API 运行时适配器"
```

## Task 5：切换 Claude Messages、兼容 Chat/Responses

**Files:**
- Create: `backend/internal/handler/sub2api_messages_executor.go`
- Create: `backend/internal/handler/sub2api_messages_executor_test.go`
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/handler/gateway_handler_chat_completions.go`
- Modify: `backend/internal/handler/gateway_handler_responses.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/handler/sub2api_runtime_registry.go`

- [ ] **Step 1: 提取现有端点执行逻辑的黑盒测试**

对 `/v1/messages`、Gateway `/v1/chat/completions`、Gateway `/v1/responses` 分别覆盖流式/非流式、账号切换、客户端取消、实际端点、平台 ID、模型映射和成功/失败终态。

- [ ] **Step 2: 运行测试，确认新 executor 测试失败**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler -run 'Sub2APIMessagesExecutor' -count=1
```

Expected: FAIL。

- [ ] **Step 3: 提取 executor，保持算法原样**

将三个公开 Handler 方法缩为：读取认证主体和请求 envelope -> 构造 `applicationgateway.Request` -> 调用 ApplicationGateway -> 写统一错误。账号选择、并发、Forward、failover、stream started 和 terminal event 全部进入 executor；不改现有 helper 和 service 算法。

- [ ] **Step 4: 删除这三个端点的旧直连 RecordUsage**

executor 只调用 `TerminalRecorder.RecordFinal`。成功/失败不能再直接调用 `GatewayService.RecordUsage`；兼容方法可保留给尚未切换端点，但这三个路由不得引用。

- [ ] **Step 5: 运行定向与基线测试**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler ./internal/service -run 'Messages|ChatCompletions|Responses|RuntimeBoundaryBaseline|Failover|RecordUsage' -count=1 -timeout=20m
```

Expected: PASS，Task 0 断言完全不变。

- [ ] **Step 6: 提交第一组端点**

```powershell
git add backend/internal/handler/sub2api_messages_executor.go backend/internal/handler/sub2api_messages_executor_test.go backend/internal/handler/gateway_handler.go backend/internal/handler/gateway_handler_chat_completions.go backend/internal/handler/gateway_handler_responses.go backend/internal/handler/wire.go backend/internal/handler/sub2api_runtime_registry.go
git commit -m "refactor(runtime): 接管 Messages 与兼容端点"
```

## Task 6：切换 OpenAI Chat、Responses、Messages 与 WebSocket

**Files:**
- Create: `backend/internal/handler/sub2api_openai_executor.go`
- Create: `backend/internal/handler/sub2api_openai_executor_test.go`
- Modify: `backend/internal/handler/openai_chat_completions.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/handler/openai_gateway_responses_chat_fallback.go`
- Modify: `backend/internal/handler/openai_gateway_messages_chat_fallback.go`
- Modify: `backend/internal/handler/openai_gateway_reasoning_failover.go`
- Modify: `backend/internal/handler/sub2api_runtime_registry.go`

- [ ] **Step 1: 写 OpenAI executor 回归**

覆盖 Chat/Responses/Messages 的 auto、force_chat_completions、force_responses；每次 attempt 主动覆盖实际上游端点；首账号失败后第二账号使用另一协议时不继承旧 marker；WebSocket 每 turn 仅形成一条终态用量。

- [ ] **Step 2: 运行并确认失败**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler -run 'Sub2APIOpenAIExecutor' -count=1
```

Expected: FAIL。

- [ ] **Step 3: 提取并注册 OpenAI executor**

保持现有 `OpenAIGatewayService` 的协议转换、OAuth、失败切换、WS state 和 endpoint marker；公开 Handler 只做 HTTP envelope 与 ApplicationGateway 调用。

- [ ] **Step 4: 收口异步 cyber/usage 上下文**

异步记录必须复制 DecisionSnapshot、request ID、PlatformAssetID 和 BillingAsset，再脱离 parent cancellation；调度和粘连仍使用 `PlatformSchedulingID`。不得从 `context.Background()` 重新推导业务资产。

- [ ] **Step 5: 运行 OpenAI 全链测试**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler ./internal/service -run 'OpenAI|Responses|ChatCompletions|Messages|WebSocket|Cyber|Endpoint|Live|PlatformIdentity' -count=1 -timeout=20m
```

Expected: PASS。

- [ ] **Step 6: 提交 OpenAI 端点**

```powershell
git add backend/internal/handler/sub2api_openai_executor.go backend/internal/handler/sub2api_openai_executor_test.go backend/internal/handler/openai_chat_completions.go backend/internal/handler/openai_gateway_handler.go backend/internal/handler/openai_gateway_responses_chat_fallback.go backend/internal/handler/openai_gateway_messages_chat_fallback.go backend/internal/handler/openai_gateway_reasoning_failover.go backend/internal/handler/sub2api_runtime_registry.go
git commit -m "refactor(runtime): 接管 OpenAI 多协议运行链路"
```

## Task 7：切换 Gemini、Antigravity 与剩余媒体端点

**Files:**
- Create: `backend/internal/handler/sub2api_gemini_executor.go`
- Create: `backend/internal/handler/sub2api_gemini_executor_test.go`
- Create: `backend/internal/handler/sub2api_media_executor.go`
- Create: `backend/internal/handler/sub2api_media_executor_test.go`
- Modify: `backend/internal/handler/gemini_v1beta_handler.go`
- Modify: `backend/internal/handler/openai_embeddings.go`
- Modify: `backend/internal/handler/openai_alpha_search.go`
- Modify: `backend/internal/handler/grok_media.go`
- Modify: `backend/internal/handler/image_task_handler.go`
- Modify: `backend/internal/handler/batch_image_handler.go`
- Modify: `backend/internal/handler/sub2api_runtime_registry.go`

- [ ] **Step 1: 写 Gemini/媒体 executor 回归**

Gemini 覆盖 native generateContent/streamGenerateContent、429 冷却、图片实际数量、平台模型映射和失败不扣费；Embeddings、Alpha Search、图片/视频覆盖当前计价模式和终态用量。

- [ ] **Step 2: 运行并确认失败**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler -run 'Sub2APIGeminiExecutor|Sub2APIMediaExecutor' -count=1
```

Expected: FAIL。

- [ ] **Step 3: 提取并注册 executor**

Gemini/Antigravity 的协议和账号能力判断留在 Runtime；平台授权、套餐/余额与倍率留在 ProductCore/UsageSink。媒体任务若跨请求完成，终态幂等键使用 task ID + terminal type，不使用请求内临时指针。

- [ ] **Step 4: 运行相关测试**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler ./internal/service -run 'Gemini|Antigravity|Embedding|AlphaSearch|Image|Video|Media|Billing' -count=1 -timeout=20m
```

Expected: PASS。

- [ ] **Step 5: 提交剩余生产端点**

```powershell
git add backend/internal/handler/sub2api_gemini_executor.go backend/internal/handler/sub2api_gemini_executor_test.go backend/internal/handler/sub2api_media_executor.go backend/internal/handler/sub2api_media_executor_test.go backend/internal/handler/gemini_v1beta_handler.go backend/internal/handler/openai_embeddings.go backend/internal/handler/openai_alpha_search.go backend/internal/handler/grok_media.go backend/internal/handler/image_task_handler.go backend/internal/handler/batch_image_handler.go backend/internal/handler/sub2api_runtime_registry.go
git commit -m "refactor(runtime): 接管 Gemini 与媒体运行链路"
```

## Task 8：切换 CountTokens 与账号探测能力

**Files:**
- Create: `backend/internal/service/sub2api_token_counter.go`
- Create: `backend/internal/service/sub2api_token_counter_test.go`
- Create: `backend/internal/service/sub2api_account_runtime.go`
- Create: `backend/internal/service/sub2api_account_runtime_test.go`
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/handler/openai_gateway_count_tokens.go`
- Modify: `backend/internal/service/upstream_billing_probe.go`
- Modify: `backend/internal/service/wire.go`

- [ ] **Step 1: 写能力端口测试**

CountTokens 不创建用量、不选择 BillingAsset、不扣费；AccountRuntime 只返回健康、能力和结构化错误，不返回 credential/token。

- [ ] **Step 2: 运行并确认失败**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service ./internal/handler -run 'Sub2APITokenCounter|Sub2APIAccountRuntime' -count=1
```

Expected: FAIL。

- [ ] **Step 3: 实现并接线**

包装现有 `ForwardCountTokens`、`ForwardCountTokensAsAnthropic` 和 `UpstreamBillingProbeService`，不复制协议/OAuth 逻辑。Handler 和管理探测服务只依赖 `gatewayruntime.TokenCounter`/`AccountRuntime`。

- [ ] **Step 4: 运行测试**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler ./internal/service -run 'CountTokens|ProbeAccount|UpstreamBillingProbe|AccountRuntime' -count=1 -timeout=20m
```

Expected: PASS。

- [ ] **Step 5: 提交辅助能力**

```powershell
git add backend/internal/service/sub2api_token_counter.go backend/internal/service/sub2api_token_counter_test.go backend/internal/service/sub2api_account_runtime.go backend/internal/service/sub2api_account_runtime_test.go backend/internal/handler/gateway_handler.go backend/internal/handler/openai_gateway_count_tokens.go backend/internal/service/upstream_billing_probe.go backend/internal/service/wire.go
git commit -m "refactor(runtime): 隔离计数与账号探测能力"
```

## Task 9：收口唯一 ApplicationGateway 生产路径

**Files:**
- Modify: `backend/internal/server/middleware/platform_asset_auth.go`
- Modify: `backend/internal/server/middleware/platform_asset_auth_test.go`
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/service/gateway_runtime_bridge.go`
- Modify: `backend/internal/service/platform_asset_request.go`
- Modify: `backend/internal/architecture/legacy_configuration_guard_test.go`

- [ ] **Step 1: 写单路径架构守卫**

AST 扫描所有公开生产 Handler，禁止直接调用 `SelectAccount*`、`Forward*`、`RecordUsage*`；允许这些调用只存在于 `sub2api_*_executor.go` 和 Adapter 文件。断言 executor 不接受 `*gin.Context`，生产 Wire 只绑定一个 `gatewayruntime.GatewayRuntime`。

- [ ] **Step 2: 运行并确认失败**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/architecture -run 'SingleApplicationGatewayPath|PureBoundary' -count=1
```

Expected: FAIL，仍有旧直连点。

- [ ] **Step 3: 移除旧上下文双写**

Platform middleware 只附加不可变 `applicationgateway.DecisionSnapshot`；Runtime route 由 ApplicationGateway 创建。移除 `GatewayPlatformAssetContextFromContext` 对旧 `DispatchIntent` 的反向重建和“legacy preferred”分支。调度兼容 context 只能在 Sub2API Adapter 内创建。

- [ ] **Step 4: 收窄 Handler 依赖**

公开 Handler 构造器注入 `ApplicationGateway`、TokenCounter 和必要 HTTP helper，不再直接持有用于生产转发的多个 Gateway Service；管理员/模型列表等非转发用途保留明确小接口。

- [ ] **Step 5: 验证单路径和无双写**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/architecture ./internal/server/middleware ./internal/handler ./internal/service -run 'SingleApplicationGatewayPath|PlatformAsset|RuntimeBoundaryBaseline|RecordUsage' -count=1 -timeout=20m
```

Expected: PASS。

- [ ] **Step 6: 提交入口收口**

```powershell
git add backend/internal/server/middleware/platform_asset_auth.go backend/internal/server/middleware/platform_asset_auth_test.go backend/internal/handler/gateway_handler.go backend/internal/handler/openai_gateway_handler.go backend/internal/handler/wire.go backend/internal/service/gateway_runtime_bridge.go backend/internal/service/platform_asset_request.go backend/internal/architecture/legacy_configuration_guard_test.go
git commit -m "refactor(gateway): 收口唯一应用网关路径"
```

## Task 10：固化 migration 命名空间与同步守卫

**Files:**
- Create: `backend/internal/architecture/migration_namespace_guard_test.go`
- Modify: `backend/migrations/README.md`
- Modify: `docs/memory/决策/2026-08-10-运行时适配器边界与可替换内核.md`

- [x] **Step 1: 写 migration 守卫测试**

测试规则：已发布 `001-200` 为冻结历史；新增官方适配只允许 `8000-8999`；ProductCore 只允许 `9000-9999`；文件名唯一；SQL 内容非空；不得恢复 Group/Channel 表；`192-200` checksum 必须精确匹配：

```text
192_subscription_billing_redesign.sql e4bff3777da6eef4673064951d706450ac1cc9777bf3844035e14e559bf6ecd1
193_subscription_redeem_code_plan_snapshot.sql 2c9997f315497cbea376c16c76ab9680aabdb65b186498e89895ba77817ec1f3
194_platform_assets_expand.sql 3adcb8eae04cc2b22af5c978ba6077bb7f4e064f82592bc8ccedcf36eb1b0c90
195_platform_endpoint_capabilities.sql 8315ba2a0dbe34853eff250e383aad381fc7a212a70053ac2b4e18b635807e83
196_model_pricing_overrides.sql f911b0bccdacb9388bc5f716b99d9cf43d1c1b0de2eeb8762d71e2737dfd63f3
197_prompt_audit_platform_scope.sql 94b54cc3e799b800e57980a7da78ad0ef6f48814d428e3f33e67eaf94bb348d1
198_content_moderation_platform_scope.sql f8bbba2ba51b1b8fa05a39f2f9d520a56dd20371aa6bd7c2e5520e7cb4fedba3
199_backfill_platform_catalog.sql 24a2fbac08ccbd2b4aff18559f182317c2ee470533055021f9caee11d6d100be
200_remove_legacy_configuration.sql 94628ba0d91318bd5f1328daeccf9ed96abdc25c2305815ab88ab7a03c6495ba
```

- [x] **Step 2: 运行并确认规则测试通过**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/architecture ./migrations -run 'MigrationNamespace|MigrationChecksum|Legacy' -count=1
```

Expected: PASS；本任务不新增 SQL。

- [x] **Step 3: 更新迁移 README 和长期决策**

写明官方同步流程：先分类 upstream migration，审查是否属于 Runtime，按当前 schema 重写到 `8000-8999`，文件名包含上游短哈希；ProductCore 变更使用 `9000-9999`；不得修改 `192-200`。

- [ ] **Step 4: 提交迁移守卫**

```powershell
git add backend/internal/architecture/migration_namespace_guard_test.go backend/migrations/README.md docs/memory/决策/2026-08-10-运行时适配器边界与可替换内核.md
git commit -m "test(migrations): 固化运行时与产品迁移命名空间"
```

## Task 11：全量验证、独立评审与项目记忆

**Files:**
- Modify: `docs/memory/项目概览.md`
- Modify: `docs/memory/当前状态.md`
- Modify: `docs/memory/决策/2026-08-10-运行时适配器边界与可替换内核.md`
- Test: all changed backend and frontend files

- [ ] **Step 1: 格式化并检查 diff**

```powershell
cd backend
Get-ChildItem internal/applicationgateway,internal/gatewayruntime,internal/service,internal/handler,internal/server/middleware,internal/architecture -Recurse -Filter *.go | ForEach-Object { & 'C:\Program Files\Go\bin\gofmt.exe' -w $_.FullName }
cd ..
git diff --check
git status --short
```

Expected: `git diff --check` 无输出；状态中不暂存 `backend/entgen_tmp/`、`my2.0.drawio`。

- [ ] **Step 2: 后端全量验证**

```powershell
cd backend
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/... -count=1 -timeout=25m
& 'C:\Program Files\Go\bin\go.exe' test ./migrations ./cmd/... -count=1 -timeout=20m
& 'C:\Program Files\Go\bin\go.exe' build ./cmd/server
```

Expected: 全部退出码 0。

- [ ] **Step 3: 前端回归**

尽管不改 UI，仍验证共享 DTO/路由没有被破坏：

```powershell
cd ../frontend
npm run test:run
npm run typecheck
npm run lint:check
npm run build
```

Expected: 全部退出码 0；只允许记录既有 chunk/dynamic import 警告。

- [ ] **Step 4: 独立代码评审**

评审重点：是否仍有 Handler 直连 runtime service；是否有 Runtime 读取资产；每个流式/异步路径是否 exactly once；错误与 endpoint marker 是否跨 attempt 污染；PlatformAssetID 与 PlatformSchedulingID 是否混用；Wire 是否可能创建第二生产路径。

- [ ] **Step 5: 更新项目记忆**

将当前状态改为“适配器边界代码完成，待发布”或记录真实阻塞；写入最终验证命令和结果。不得把未执行的 Docker/服务器测试写成通过。

- [ ] **Step 6: 提交收尾文档**

```powershell
git add docs/memory/项目概览.md docs/memory/当前状态.md docs/memory/决策/2026-08-10-运行时适配器边界与可替换内核.md
git commit -m "docs(memory): 记录运行时适配器边界结果"
```

## Task 12：发布、镜像与腾讯云两轮验收

**Files:**
- Verify: `.github/workflows/release.yml`
- Verify: generated GitHub release asset `sub2api_my2_latest.tar`
- Verify: Tencent Cloud `/opt/sub2api`

- [ ] **Step 1: 确认发布前状态**

```powershell
git status --short --branch
git log -1 --oneline
git ls-remote --tags origin "refs/tags/my2-v0.2.*"
```

Expected: 只有用户未跟踪文件；分支为 `my2.0`；新 Tag 不复用现有版本。

- [ ] **Step 2: 用户授权后推送分支并创建递增 Tag**

版本号由命令读取远程最高 `my2-v0.2.N` 后递增；不得猜测或覆盖已存在 Tag。

```powershell
$versions = git ls-remote --tags --refs origin 'refs/tags/my2-v0.2.*' |
    ForEach-Object { if ($_ -match 'refs/tags/my2-v0\.2\.(\d+)$') { [int]$Matches[1] } }
$next = (($versions | Measure-Object -Maximum).Maximum) + 1
$tag = "my2-v0.2.$next"
if (git ls-remote --exit-code --tags origin "refs/tags/$tag") { throw "tag already exists: $tag" }
git push origin my2.0
git tag -a $tag -m $tag
git push origin $tag
```

Expected: 分支和 Tag 推送成功，GitHub Release workflow 被触发。

- [ ] **Step 3: 等待并验证离线镜像**

通过 GitHub CLI/Actions 查看 workflow 直到结束；下载 `sub2api_my2_latest.tar`，确认大小不是异常的全构建缓存包，并执行：

```powershell
docker load -i sub2api_my2_latest.tar
docker image inspect sub2api:my2-latest
```

Expected: workflow success；tar 可加载；镜像架构为 `linux/amd64`。

- [ ] **Step 4: 服务器部署前备份**

在 `/opt/sub2api/backups/<tag>-pre-<timestamp>` 保存 PostgreSQL compressed dump、data（排除活动日志）、Compose、环境和当前镜像 inspect。确认目标路径位于 `/opt/sub2api` 后再替换应用镜像；不修改 PostgreSQL、Redis、证书或域名。

- [ ] **Step 5: 只重建应用容器**

加载离线 tar，更新 Compose 使用新镜像，只执行应用服务重建。确认应用 `healthy`、重启次数 0，PostgreSQL/Redis 容器 ID 和数据卷未变化。

- [ ] **Step 6: 真实环境测试两轮**

每轮执行：GPT `/v1/chat/completions`、GPT `/v1/responses`、GLM `/v1/chat/completions`；如 GLM 上游仍 502，记录为上游事实但继续核对本地路由、失败不扣费和冷却。另执行首账号失败、第二账号成功场景（需要平台至少两个可用账号）。

数据库核对每次成功请求：单条 usage、正确 `platform_id`、`account_id`、`subscription_id`/balance、实际端点、模型、倍率和费用；失败请求无扣费；两轮扣费增量等于用量实际费用合计。

- [ ] **Step 7: 发布验收记录**

更新 `docs/memory/当前状态.md`，记录 Tag、commit、镜像 ID、备份路径、健康状态、两轮请求数量、成功/失败、用量与扣费核对结果。若任一步失败，不标记阶段完成，保留备份并给出回滚命令。

## 本轮执行记录（2026-08-11）

- Task 0-4：契约、ApplicationGateway、Pricing/Usage 和 Runtime Adapter/Registry 已实现并验证。
- Task 5-8：Messages、Chat/Responses 兼容入口、Gemini/媒体/CountTokens/Live 等模型相关入口已接入统一 Adapter；账号探测保留为独立能力适配器。
- Task 9-10：生产入口统一走 ApplicationGateway，迁移命名空间与已发布 migration checksum 守卫已验证；无模型的 Live sideband、WebSocket 和视频状态查询保留专用控制状态机，不通过需要模型决策的产品入口。
- Task 11：Go、前端、迁移、cmd 和构建验证已完成；独立审计修复了安全审计静态测试与新 executor 方法名不一致的问题。
- Task 12：已完成。commit `866daf195`、Tag `my2-v0.2.16` 已推送；GitHub Actions `31407266992` 成功生成并发布离线镜像，服务器校验 SHA-256 后加载 `sub2api:my2-0.2.16`。部署前备份为 `/opt/sub2api/backups/my2-v0.2.16-pre-20260810T161503Z`，仅应用容器重建，PostgreSQL/Redis 保持原容器且三者均健康。两轮真实验收中 GPT Chat/Responses 共 4 次均 HTTP 200 并产生 `platform_id/account_id/subscription_id` 用量记录；GLM Chat 共 2 次均由上游 HTTP 502，服务记录失败切换与账号冷却，未产生成功用量/扣费。

## 最终验收矩阵

| 边界 | 验收标准 |
| --- | --- |
| ProductCore | 不导入 Gin/Ent/service；独立决定平台和 BillingAsset |
| GatewayRuntime | 不含 API Key、用户资产、订阅、余额或倍率；只处理运行事实 |
| Pricing | 独立端口；当前 Sub2API 目录作为唯一实现；倍率不叠乘 |
| Usage | 一个终态入口；成功只扣一次；失败不扣费；异步保留决策快照 |
| 调度 | OAuth、账号选择、冷却、失败切换和协议结果与基线一致 |
| 入口 | 所有生产转发只走 ApplicationGateway -> Sub2APIRuntimeAdapter |
| 迁移 | 192-200 checksum 冻结；未来仅使用 8000/9000 编号域 |
| 发布 | 全量测试、镜像、健康检查和腾讯云两轮真实验收均有证据 |
