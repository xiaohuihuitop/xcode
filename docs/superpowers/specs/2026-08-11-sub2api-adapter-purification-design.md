# Sub2API 适配器纯化设计

## 状态

- 状态：已确认方向，待按实施计划执行
- 日期：2026-08-11
- 开发分支：`my2.0`
- 当前生产 Runtime：Sub2API
- 本轮范围：只形成设计与计划，不修改运行时代码

## 背景

现有架构已经建立 `ProductCore -> ApplicationGateway -> GatewayRuntime -> UsageSink` 边界，并把 Sub2API 收口为唯一 `Sub2APIRuntimeAdapter`。公开领域契约不依赖 Gin、Ent 或 `internal/service`，但 Adapter 内仍有一层迁移兼容桥：

- `sub2api_legacy_dispatch.go` 用 `legacyGinHandler` 和 `legacyEndpointExecutor` 把旧 Handler 回调包装成 executor。
- `ginHTTPExchange` 暴露原始 Gin context，executor 再从 exchange 反向取得 `*gin.Context`。
- OpenAI/Grok 和辅助端点仍直接调用 `legacy*` Handler。
- Gateway Messages 路径已把调度、Forward、失败切换和用量所有权迁入 executor，但实际执行仍依赖 Gin context。
- 旧 Handler 同时承担产品预检、HTTP 协议、账号调度、上游 Forward、失败切换、Ops 状态和终态用量，职责没有真正拆开。

因此，现状解决了“业务领域不直接依赖内核”，但尚未达到“Sub2API 是可以被替换的纯 Runtime 实现”。

## 目标

1. Gin 只存在于最外层 HTTP Handler 和 `ginHTTPExchange` 的实现，不越过运行时边界。
2. 所有生产 executor 只接收 `context.Context`、`gatewayruntime.Request`、`gatewayruntime.HTTPExchange` 和 `UsageSink`。
3. executor 不调用公开 Handler，不通过 type assertion 取回 Gin，也不从 Gin middleware 读取 API Key、用户、订阅或余额对象。
4. ProductCore/ApplicationGateway 继续决定平台和 BillingAsset；Sub2API 继续拥有账号调度、OAuth、协议转换、流式、重试、冷却和失败切换。
5. 保持外部 API、错误包络、数据库 schema、前端、Docker、模型路由、计费和真实请求结果不变。
6. 每个端点家族可独立迁移、验证、提交和发布，不保留“新实现失败后回旧 Handler”的静默 fallback。
7. 最终通过架构守卫禁止 Gin/旧 Handler 兼容桥回流。

## 非目标

- 不接入第二套 Runtime，不拆微服务，不新增内部 HTTP/RPC。
- 不重写或优化 Sub2API 的账号调度、OAuth、协议、重试、冷却和失败切换算法。
- 不改变套餐优先、多实例切换、余额回退、倍率或定价规则。
- 不修改数据库 schema，不新增 SQL migration，不改前端。
- 不顺带清理所有 Handler helper；只迁移运行时生产链路所需部分。
- 不用新的 fallback 掩盖迁移错误。

## 纯化定义

### 允许的依赖

Sub2API executor 可以依赖：

- `internal/gatewayruntime` 的纯请求、结果、HTTP exchange、错误和 UsageSink。
- 现有 Sub2API service 中的账号池、OAuth、调度、协议转换和上游 Forward 实现。
- Adapter 内部的日志、Ops marker、响应和错误映射 helper。
- 由 Runtime Request 映射出的只读调度路由；在底层 service API 完成显式参数化前，可由 Adapter 内部临时创建 `GatewayPlatformAssetContext`，但只能包含平台路由和调度信息，不能包含 BillingAsset。

### 禁止的依赖

生产 executor 不得：

- 接收、保存、提取或构造 `*gin.Context`。
- 调用 `legacyGinHandler`、`legacyEndpointExecutor`、`dispatchLegacyEndpoint` 或公开 HTTP Handler。
- 从 Gin middleware 读取 API Key、AuthSubject、UserSubscription 或余额对象。
- 决定套餐、余额、倍率或最终费用。
- 在失败后调用旧实现重试。

### 边界外的 Gin

`ginHTTPExchange` 可以继续作为最外层 `gatewayruntime.HTTPExchange` 实现，但它不再实现 `GinContext()`。Handler 负责把认证快照、原始 payload、请求 metadata 和 response writer 映射到 ApplicationGateway；Runtime 只能使用接口方法。

## 目标结构

```text
Gin route / middleware
        |
        v
runtime ingress
  - auth/grant snapshot
  - product preflight
  - raw payload/model/stream metadata
  - Gin -> HTTPExchange
        |
        v
ApplicationGateway
  - ProductDecision
  - UsageSink bound to decision
        |
        v
Sub2APIRuntimeAdapter
        |
        +--> protocol executor
        |      - request validation/normalization
        |      - account scheduling/OAuth
        |      - protocol conversion/forward
        |      - retry/failover/stream
        |
        +--> terminal recorder
               - UsageFacts exactly once
```

## 职责拆分

### HTTP ingress

保留在 `internal/handler`，只负责：

- 从 middleware 获取认证主体并构造 `productcore.AccessGrant`。
- 读取并恢复原始 body，提取模型、流式标记、Content-Type 和必要 headers。
- 执行仍属于产品侧的安全审计、BillingAsset 可用性和用户级并发预检。
- 构造 `applicationgateway.DispatchRequest` 和 `gatewayruntime.Request`。
- 将结构化应用/运行时错误映射为现有 HTTP 或 SSE 错误包络。

HTTP ingress 不选择账号、不刷新 OAuth、不调用 Forward、不操作失败账号集合。

### ApplicationGateway

继续负责：

- 解析并冻结 ProductDecision。
- 将业务平台 ID、适配器、模型和端点写入 Runtime Request。
- 把 BillingAsset 只绑定到 Product UsageSink，不暴露给 Runtime。
- 强制每次 dispatch 产生唯一终态。

### Sub2API executor

每个 executor 拥有一个端点家族的运行时行为：

- 解析/校验该协议的 payload，并生成上游请求。
- 使用显式 Runtime Request 字段构造 Adapter 内部调度路由。
- 获取用户级预检已经批准的请求，执行账号级并发、选择、OAuth、Forward、重试和失败切换。
- 通过 HTTPExchange 写响应，通过 UsageSink 上报最终 UsageFacts。
- 每个 attempt 对称设置实际端点和账号 marker，避免跨账号切换污染。

### Product UsageSink

保持现状：成功终态定价并按决策资产扣费；失败终态不扣费。Adapter 不直接调用 `GatewayService.RecordUsage` 或 `OpenAIGatewayService.RecordUsage` 作为第二条成功用量路径。

## Transport 与状态

现有 `gatewayruntime.HTTPExchange` 保留标准库接口。新增或收口的 Adapter helper 只基于：

- `Request() *http.Request`
- `Header()/WriteHeader()/Write()/Flush()`
- `Written()/Size()`
- `SetState()/State()`

运行时 marker 只能保存：实际账号、实际端点、stream started、上游/响应时延和安全诊断码。ProductDecision、BillingAsset、API Key、订阅或凭证不能进入 exchange state。

现有 Forward service 若接收 `*gin.Context`，按端点迁移为接收 `context.Context`、`gatewayruntime.HTTPExchange` 或标准库 `http.ResponseWriter`/`*http.Request`。协议算法和响应字节保持原样，只替换传输依赖。

## 产品预检迁移

旧 Handler 中以下逻辑不能随协议循环搬进 Runtime：

- API Key/AuthSubject/Subscription 获取。
- BillingAsset/余额可用性检查。
- 用户级并发限制。
- 内容安全审计和产品级阻断。

它们在每个端点迁移时提取为 ingress preflight。账号级并发、账号能力、账号冷却和 OAuth 错误仍属于 Runtime。若现有 ProductDecision 已等价完成 BillingAsset 可用性判断，应通过契约测试证明后删除重复检查；不能直接省略。

## 端点迁移顺序

### 阶段 0：基线与防扩散

- 建立当前 legacy 调用点清单和 allowlist 守卫。
- 固定 HTTP 状态、响应 body、实际端点、平台/账号归属、终态用量和失败不扣费行为。
- 在兼容桥删除前，禁止新增 legacy 调用点。

### 阶段 1：纯 ingress 与 transport helper

- 新增通用 runtime ingress 和基于 HTTPExchange 的响应/Ops helper。
- `ginHTTPExchange` 删除 `GinContext()` 能力的最终目标先由测试约束。
- 建立可复用 executor conformance suite。

### 阶段 2：低风险同步端点

- CountTokens、Embeddings、Alpha Search。
- 优先验证 body/error/headers、无计费 CountTokens、Embeddings/Alpha Search 的账号切换和实际端点。

### 阶段 3：Gemini 与媒体

- Gemini Native、Images、Videos、视频状态。
- 保留异步媒体 task ID 的终态幂等语义，失败不扣费。

### 阶段 4：Gateway Messages 家族

- Anthropic/Gateway 的 Messages、兼容 Chat 和 Responses。
- 将已经位于 `sub2APIMessagesExecutor` 的实际调度/Forward 方法改为纯 exchange，不再取 Gin context。

### 阶段 5：OpenAI/Grok Chat、Responses 与 Messages

- 最高风险阶段，覆盖 SSE、Chat/Responses 互转、实际端点 marker、OAuth 429、502 失败切换、客户端取消和部分响应计费。
- 每个 upstream attempt 开始时显式重置实际端点，成功后写最终端点。

### 阶段 6：Live 与 WebSocket

- 不把长连接控制流硬塞进一次性 `GatewayRuntime.Dispatch`。
- 在 `gatewayruntime` 增加小型 `LiveRuntime`、`WebSocketRuntime` 能力接口，Sub2API 分别实现创建/代理/关闭生命周期。
- Live sideband 和 Responses WebSocket 的身份、并发、业务平台 ID、调度命名空间和断线终态保持现状。

### 阶段 7：删除兼容桥

- 所有生产调用点归零后删除 `legacyGinHandler`、`legacyEndpointExecutor`、`ginContextCarrier` 和 `dispatchLegacyEndpoint`。
- 公开 Handler 只保留 ingress/ApplicationGateway 调用。
- 架构测试扫描生产 AST，禁止上述标识符、executor 的 Gin import，以及 Handler 直接调用 `SelectAccount*`/`Forward*`/`RecordUsage*`。

## 错误与流式语义

- Runtime 返回结构化 `RuntimeError`；ingress 使用现有协议格式写 HTTP/SSE 错误。
- 响应已写出后禁止换账号；上游失败但未写响应时按现有预算切换。
- 客户端取消停止继续调度；若上游已产生当前规则认可的可计费用量，沿用现有部分结果计费语义。
- 每个请求最多一次成功/失败终态；attempt 失败只进入 Ops，不进入最终扣费。
- 终态记录失败不能静默吞掉，必须与原运行时错误合并返回。

## 测试策略

### 架构测试

- `productcore`、`gatewayruntime`、`applicationgateway` 不导入 Gin/Ent/service。
- 生产 executor 不导入 Gin，不出现 `GinContext()` 或 legacy bridge 标识符。
- 公开生产 Handler 不直接调用账号选择、Forward 或成功用量记录。
- Runtime Request/Metadata 不出现 APIKey、Subscription、Balance、BillingAsset 或 Ent 类型。

### Executor conformance

每个 executor 都运行同一组契约：

- 成功终态 exactly-once。
- 所有账号失败只产生失败终态且不扣费。
- 首账号失败、第二账号成功归因第二账号。
- 流式部分响应后不切换账号。
- 客户端取消不会产生第二终态。
- 实际端点 marker 在跨协议 attempt 间不会继承旧值。
- 持久化使用正数 PlatformAssetID，调度/粘连使用 PlatformSchedulingID。
- detached worker 保留平台路由、request ID 和 UsageSink，但不保留父请求取消信号。

### 全量与真实环境

- 后端 unit/integration、迁移、cmd、server build、Go lint。
- 前端全量测试、typecheck、lint、build，确认外部契约未变。
- Docker 离线包和镜像架构校验。
- 腾讯云两轮 GPT Chat、GPT Responses、GLM Chat、失败切换、用量和扣费核对。

## 风险与控制

| 风险 | 控制 |
| --- | --- |
| 一次性移动大 Handler 导致行为漂移 | 按端点家族切分，每次先锁定黑盒基线 |
| 产品预检被误搬进 Runtime | 单独 ingress preflight，Runtime Request 禁止产品资产类型 |
| service 改签名改变协议字节 | 只替换 transport 参数，使用 recorder 比较状态、headers、body/SSE |
| failover 端点 marker 污染 | 每 attempt 对称 reset/set，并补跨账号跨协议测试 |
| 流式重复扣费 | TerminalRecorder + executor conformance + UsageSink 幂等 |
| 状态端点被硬套普通 Dispatch | Live/WebSocket 使用独立小接口 |
| 兼容桥长期残留 | allowlist 只能减少，最终 AST 守卫要求零调用点 |
| 官方同步重新引入旧 Handler 路径 | 架构测试和 My2 发布门禁阻止发布 |

## 验收标准

1. `rg` 在生产 Go 文件中找不到 `legacyGinHandler`、`legacyEndpointExecutor`、`ginContextCarrier` 或 `dispatchLegacyEndpoint`。
2. `sub2api_*executor.go` 不导入 Gin，不接收或构造 `*gin.Context`。
3. 公开 Handler 不直接选择账号、调用 Forward 或记录成功用量。
4. 所有端点仍只走 `ApplicationGateway -> Sub2APIRuntimeAdapter` 或明确的纯状态能力端口。
5. 调度、OAuth、协议、错误、流式、失败切换和计费行为通过回归，无外部 API/schema/UI 变化。
6. 成功只扣一次，失败不扣费；实际平台、账号、端点、套餐/余额、倍率和费用归属正确。
7. My2 完整发布门禁通过，并完成服务器两轮真实验收后，纯化阶段才标记完成。

## 相关文件

- `backend/internal/applicationgateway/gateway.go`
- `backend/internal/gatewayruntime/`
- `backend/internal/handler/runtime_http_exchange.go`
- `backend/internal/handler/sub2api_legacy_dispatch.go`
- `backend/internal/handler/sub2api_openai_executor.go`
- `backend/internal/handler/sub2api_auxiliary_executor.go`
- `backend/internal/handler/sub2api_messages_executor.go`
- `backend/internal/handler/gateway_handler*.go`
- `backend/internal/handler/openai_*.go`
- `backend/internal/handler/gemini_v1beta_handler.go`
- `backend/internal/handler/grok_media.go`
- `backend/internal/service/gateway_runtime_bridge.go`
- `backend/internal/architecture/legacy_configuration_guard_test.go`
