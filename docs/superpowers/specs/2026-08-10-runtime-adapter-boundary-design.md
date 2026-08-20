# 可替换网关内核的运行时适配器边界设计

## 状态

- 状态：已确认
- 日期：2026-08-10
- 开发分支：`my2.0`
- 当前运行时：Sub2API
- 本阶段目标：建立行为不变的适配器边界，不接入第二套内核

## 背景

My2 已将平台、API Key 资产授权、套餐、余额、倍率和用量归属重构为自有 ProductCore；Sub2API 仍负责账号凭据、OAuth 刷新、账号调度、协议转换、流式转发和上游失败切换。

仓库已经存在 `internal/productcore`、`internal/gatewayruntime` 和 `gateway_runtime_bridge.go`，但隔离尚未完成：

- `gatewayruntime` 主要提供 `DispatchIntent`，还没有可替换的执行接口。
- ProductCore 的端口适配、运行时上下文、订阅实体和实际用量记录仍混合在 `internal/service`。
- Runtime 仍可间接接触 API Key、套餐、余额和 Ent 类型。
- 平台、订阅、UsageLog 和 Runtime 共用同一套 Ent schema 与迁移目录。
- 当前自定义迁移 `192-200` 与官方同编号迁移已经产生语义冲突，不能直接合并或覆盖。

因此，当前架构可以按提交挑选官方更新，但不能直接替换网关内核。本设计先建立稳定的进程内端口，使 Sub2API 成为第一个 Runtime Adapter。

## 目标

1. ProductCore 独立决定平台、模型、端点、套餐或余额和最终倍率。
2. Runtime 只负责账号、OAuth、调度、协议、重试、流和上游调用。
3. 模型基础价格通过独立 PricingEngine 端口计算，不与传输内核绑定。
4. Runtime 只返回实际请求事实，不直接执行用户资产决策。
5. 保持现有 HTTP API、数据库 schema、前端、Docker 和外部行为不变。
6. 让未来内核只需实现稳定端口，不修改 ProductCore 和前端业务。
7. 为后续官方同步建立迁移编号和依赖方向守卫。

## 非目标

- 本阶段不接入第二套内核。
- 不拆分微服务，不新增容器或内部 HTTP/RPC。
- 不重写 Sub2API 调度、OAuth、协议转换或失败切换算法。
- 不修改数据库 schema，不创建空迁移。
- 不调整前端目录或视觉设计。
- 不改变套餐优先、多套餐切换、余额回退、倍率和扣费规则。
- 不恢复 Group、Channel、BillingProfile 或旧运行时兼容路径。

## 目标架构

```text
HTTP Handler / API Key Middleware
              |
              v
ApplicationGateway
              |
              +--> ProductCore Authorizer
              |      - Platform / model / endpoint
              |      - API Key grants
              |      - subscription or balance asset
              |
              +--> GatewayRuntime
              |      - account pool / OAuth / protocol
              |      - retry / failover / stream
              |
              +--> PricingEngine
              |      - base model and media price
              |
              +--> BillingFinalizer / UsageSink
                     - multiplier / deduction / attribution
```

依赖方向固定为：

```text
productcore      -> 仅标准库和纯领域类型
runtimecontract  -> 仅标准库和只读值对象
application      -> productcore + runtimecontract
sub2apiadapter   -> application ports + 现有 Sub2API service
repository       -> ProductCore persistence ports + Ent/PostgreSQL
handler          -> application facade，不直接组合 Runtime 内部服务
```

ProductCore 和 runtime contract 禁止导入 Gin、Ent、`internal/service` 或具体 Sub2API 类型。

## 核心契约

### ProductDecision

ProductCore 对一次请求的不可变决定：

```go
type ProductDecision struct {
    Platform     PlatformRoute
    BillingAsset *BillingAsset
}
```

`PlatformRoute` 只包含业务平台 ID、平台编码、运行时适配器、客户端模型、上游模型和端点能力。`BillingAsset` 只包含 `subscription` 或 `balance`、订阅/套餐引用和倍率。

### RuntimeRequest

Runtime 接收的传输请求：

```go
type RuntimeRequest struct {
    RequestID       string
    PlatformID      int64
    PlatformCode    string
    Adapter         string
    InboundEndpoint string
    UpstreamModel   string
    Stream          bool
    Payload         []byte
    Metadata        RequestMetadata
}
```

`RuntimeRequest` 不包含 API Key 实体、用户余额、套餐实体、订阅实体、倍率或 Ent 对象。Payload 在边界处复制或以受控只读引用传递，避免下游修改共享决策。

### RuntimeResult 与 UsageFacts

```go
type RuntimeResult struct {
    StatusCode       int
    AccountID        int64
    UpstreamEndpoint string
    UpstreamModel    string
    Response         RuntimeResponse
}
```

`RuntimeResult` 只承载响应和供调用方显示或诊断的安全运行元数据。最终用量不在返回值中重复携带，统一通过 `UsageSink.RecordFinal` 发送。

`UsageFacts` 保存输入、输出、缓存读写、图片/视频、TTFT、总延迟、实际账号、实际端点和实际上游模型。它不包含最终套餐或余额费用。

### 接口隔离

不使用一个庞大的 Runtime 接口，按能力拆分：

```go
type GatewayRuntime interface {
    Dispatch(context.Context, RuntimeRequest, UsageSink) (RuntimeResult, error)
}

type TokenCounter interface {
    CountTokens(context.Context, RuntimeRequest) (TokenCountResult, error)
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

未来替换请求内核时必须实现 `GatewayRuntime`；若继续沿用现有定价目录，不需要替换 `PricingEngine`。`UsageSink` 始终由 ProductCore 基础设施实现，不属于可替换内核。

## 请求数据流

### 标准请求

1. Handler 读取请求模型和标准化端点，不执行资产选择。
2. ApplicationGateway 调用 ProductCore，得到 ProductDecision。
3. ApplicationGateway 将 PlatformRoute 转为 RuntimeRequest。
4. Sub2APIRuntimeAdapter 将 RuntimeRequest 映射到现有 Sub2API 上下文和服务调用。
5. Sub2API 内部完成账号选择、OAuth、协议转换、重试和失败切换。
6. Adapter 返回 RuntimeResult，并在请求唯一终态调用一次 `UsageSink.RecordFinal`。
7. 成功终态由 PricingEngine 按实际上游模型和 UsageFacts 计算基础费用。
8. BillingFinalizer 使用 ProductDecision 中的资产倍率完成套餐或余额扣费。
9. UsageSink 写入实际平台、账号、订阅、资产来源、倍率和最终费用；失败终态只写 Ops/Error 事实。

### 流式请求

- 响应流可在上游建立后立即交给客户端。
- Adapter 对流结束、客户端取消和上游错误进行唯一终态判定。
- 每个请求必须且最多产生一次 `RecordFinal`；成功、失败或取消都要形成明确终态。
- 流结束前的片段统计只能保存在 Runtime 内部，不能提前扣费。
- 上游已经返回可计费用量但客户端断开时，沿用当前 Sub2API 的计费语义，由契约测试固定。

### Count Tokens 与账号探测

- Count Tokens 通过独立 TokenCounter 端口，不伪装成普通计费请求。
- 账号探测通过 AccountRuntime 端口，只返回健康、能力和错误事实。
- 两类操作都不能读取或扣减套餐、余额。

## Sub2API Adapter

`Sub2APIRuntimeAdapter` 第一阶段只包装现有实现：

- `GatewayService`、`OpenAIGatewayService`、Antigravity/Gemini 转发服务。
- `gateway_scheduling.go`、`openai_account_scheduler.go` 和相关账号池选择。
- OAuth 登录、刷新、凭据轮换和账号健康状态。
- Chat、Responses、Messages、Gemini、Live 和媒体请求的协议实现。
- 上游重试、失败切换、冷却、流式读取和错误分类。

Adapter 可以在内部创建现有兼容上下文，但兼容上下文不得越过 Adapter 返回给 ProductCore。现有 `GatewayPlatformAssetContext` 只作为迁移桥保留，所有新调用点使用 runtime contract。

生产环境只保留一条请求路径。Handler 切换到 ApplicationGateway 后，不保留“新路径失败再回旧路径”的静默 fallback。

## Pricing 与扣费

- 当前 Sub2API 模型价格目录由 `Sub2APIPricingAdapter` 实现 PricingEngine。
- ProductCore 只在基础费用上应用套餐快照倍率或全局余额倍率，两者绝不叠乘。
- Runtime 的账号倍率继续只用于上游成本或账号统计，不成为用户资产倍率。
- 定价失败保持 fail-closed，不以 `1.0` 或零费用继续。
- BillingFinalizer 继续使用现有经过验证的订阅窗口、额度边界、并发和缓存失效语义。

## 错误模型

Runtime 对外只返回结构化错误类别：

| 类别 | Runtime 行为 | ProductCore/HTTP 行为 |
| --- | --- | --- |
| `credential_invalid` | 标记账号异常并尝试切换 | 最终失败映射为现有上游错误 |
| `upstream_forbidden` | 按现有规则切换账号 | 最终返回 502 |
| `rate_limited` | 记录冷却并切换账号 | 无账号后返回现有错误 |
| `upstream_timeout` | 按现有重试预算执行 | 最终返回 502/504 现有语义 |
| `upstream_5xx` | 触发失败切换 | 最终返回 502 |
| `no_available_account` | 停止调度 | 返回 503 |
| `invalid_upstream_response` | 记录 Ops 错误 | 不扣费 |
| `client_cancelled` | 停止流和后续请求 | 按现有终态计费规则处理 |

RuntimeError 可以包含 request ID、账号 ID、实际上游端点、是否可重试和安全诊断信息，但禁止包含凭证、Token 或完整敏感请求体。

## 用量幂等与归属

- 最终 UsageEvent 使用稳定幂等键，至少由 request ID、终态标记和 usage 类型组成；UsageSink 是最终用量的唯一生产记录入口。
- 失败切换中的单次 attempt 只进入 Ops/Error 事实，不形成最终用户扣费。
- 成功终态必须记录正数业务 `PlatformAssetID`；调度、粘连和并发缓存继续使用 `PlatformSchedulingID`。
- 订阅扣费记录实际 `subscription_id` 和套餐快照倍率；余额扣费的订阅字段为空。
- 异步记录必须复制 ProductDecision 和 request ID 后再脱离父请求取消信号。

## 迁移隔离

已部署的自定义迁移 `192-200` 永久保持文件名和 checksum 不变。

后续约定：

- `8000-8999`：从官方挑选并按当前 schema 适配的 Runtime 迁移。
- `9000-9999`：My2 ProductCore 自有迁移。
- 官方迁移不得以冲突的原文件名直接复制进当前迁移目录。
- 适配后的 Runtime 迁移文件名包含官方提交短哈希，保留来源追踪。
- 没有实际 schema 变化时不创建空迁移。
- 架构测试检查迁移编号范围、文件名唯一性、已发布 migration checksum 和旧 Group/Channel 关键词。

继续使用当前 `schema_migrations` 表和迁移执行器，本阶段不新增迁移表。

## 实施阶段

### 阶段 1：契约基线

- 为现有 Chat、Responses、Messages、Gemini、Count Tokens 和账号探测建立行为测试。
- 固定平台隔离、实际端点、账号失败切换、套餐优先和失败不扣费语义。

### 阶段 2：纯契约与应用门面

- 新增 runtime contract 值对象、小接口和结构化错误。
- 新增 ApplicationGateway，不修改外部路由和 DTO。
- ProductCore 保持纯 Go 依赖。

### 阶段 3：Sub2API Adapter

- 包装现有 Runtime、Token Counter、Account Probe 和 Pricing 实现。
- 将现有上下文桥限制在 Adapter 内部。
- Handler 逐端点委托 ApplicationGateway，每次切换后运行相同契约测试。

### 阶段 4：依赖收口

- 禁止 Runtime 读取 API Key 资产、UserSubscription、余额或套餐实体。
- 禁止 ProductCore 导入 Sub2API service、Gin 或 Ent。
- 删除已经没有调用者的双写或兼容上下文，保持单一生产路径。

### 阶段 5：迁移与同步守卫

- 固化 `192-200` checksum。
- 为 `8000/9000` 编号域和上游迁移冲突增加自动检查。
- 更新官方同步检查清单和架构守卫。

## 验证

### 契约测试

- Chat、Responses、Messages、Gemini 的非流式和流式请求。
- 自动、强制 Chat、强制 Responses 的实际上游端点。
- 同平台多账号调度、失败账号排除和第二账号切换。
- OAuth 刷新、凭据失败、429 冷却、502/503 和客户端取消。
- 套餐优先、同套餐多实例、额度耗尽切换和余额回退。
- 基础价格、套餐倍率、余额倍率、缓存 Token、图片/视频费用。
- 平台、账号、订阅、实际端点和最终费用归属。

### 架构测试

- `productcore` 和 runtime contract 不导入 Gin、Ent 或 `internal/service`。
- Runtime 接口签名不出现 APIKey、UserSubscription、Balance 或套餐实体。
- 活动源码不重新出现 Group/Channel 运行时权威。
- 新迁移只能落入批准的编号域。

### 发布验证

- 后端 unit/integration、server build 和迁移测试全部通过。
- 前端全量测试、typecheck、lint 和 production build 通过，尽管本阶段不改 UI。
- Docker 离线镜像构建成功。
- 腾讯云只替换应用容器，保留 PostgreSQL/Redis 和数据卷。
- 部署后执行两轮 GPT Chat、GPT Responses、GLM Chat、失败切换和扣费核对。

## 验收标准

1. 外部 API、数据库 schema、前端和 Docker 部署方式不变。
2. 当前 Sub2API Runtime 的调度、OAuth、协议和失败切换结果不变。
3. 成功请求只扣一次；失败请求不扣费。
4. ProductCore 与 Runtime 的依赖方向由自动测试保护。
5. Sub2APIRuntimeAdapter 成为唯一生产 Runtime 实现。
6. ProductCore 测试不需要初始化 Sub2API GatewayService、Gin 或 Ent。
7. 新 Runtime 可以在不改 ProductCore 领域规则和前端 API 的情况下实现 GatewayRuntime。

## 风险与防护

| 风险 | 防护 |
| --- | --- |
| 包装时改变既有行为 | 先建立黑盒契约测试，再逐端点切换 |
| 新旧路径双实现漂移 | 生产只保留 ApplicationGateway 单一路径，不设 fallback |
| 流式用量重复 | 最终事件幂等键和 exactly-once 单测 |
| OAuth/失败切换被业务层侵入 | RuntimeError 只暴露最终结构化事实 |
| 定价与内核一起被替换 | PricingEngine 独立于 GatewayRuntime |
| 官方迁移覆盖自定义迁移 | `192-200` checksum 守卫和 `8000/9000` 编号域 |
| 大范围一次性重构 | 按端点和能力分阶段，每阶段保持可发布 |

## 相关现有文件

- `backend/internal/productcore/`
- `backend/internal/gatewayruntime/`
- `backend/internal/service/gateway_runtime_bridge.go`
- `backend/internal/service/product_core_adapter.go`
- `backend/internal/service/platform_asset_request.go`
- `backend/internal/service/api_key_asset_resolver.go`
- `backend/internal/service/model_pricing_catalog.go`
- `backend/internal/server/middleware/platform_asset_auth.go`
- `backend/internal/architecture/legacy_configuration_guard_test.go`
- `backend/internal/repository/migrations_runner.go`
