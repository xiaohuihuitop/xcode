# RuntimeBridge 与 Sub2API Driver 分离设计

## 状态

- 状态：已确认
- 日期：2026-08-20
- 分支：`my2.0`
- 当前 Runtime：Sub2API
- 本设计的目标：提高内核可替换性，同时保持当前单进程部署和 Sub2API 行为不变

## 背景与结论

XCode 已经拥有 ProductCore、ApplicationGateway 和 GatewayRuntime 的初步边界，但当前生产组合仍在 Handler 中直接组装 Sub2API executor。Sub2API 的账号调度、OAuth、协议转换、失败切换和流式转发已经与产品域分开一部分，然而运行时实现仍能通过旧 Service、Gin carrier 和旧 Handler 访问应用上下文。

本轮采用“模块化单体优先、进程外置可选”的路线：

1. 先定义可独立版本化的 RuntimeBridge contract。
2. 在同一进程内把 Sub2API 收口为唯一 Driver，保持现有数据库、HTTP API、前端和 Docker 不变。
3. 用契约测试固定请求、流式、账号事实、失败切换和终态用量语义。
4. 只有当 Driver 不再依赖产品实体、旧 Handler 和隐式 Gin 状态后，才增加独立 RuntimeBridge 进程。

立即拆成两个服务不是当前最佳方案。现有 GatewayService、OpenAIGatewayService、OAuth 状态、调度缓存、账号仓库和用量终结器仍然是同一进程内的强耦合点；在这些耦合没有被端口化之前，跨进程只会把隐式依赖变成网络协议、重试、鉴权和一致性风险。

## 目标

1. ProductCore 只负责用户、API Key、平台、模型能力、订阅、余额、倍率、定价和最终扣费。
2. Runtime 只负责账号池、OAuth 刷新、协议适配、调度、重试、冷却、失败切换、流式传输和上游调用。
3. Runtime 不接收或返回 API Key、用户、订阅、余额、套餐或 Ent 实体；只接收标量关联 ID 和不可变路由快照。
4. Sub2API Driver 可以替换而不修改 ProductCore、前端、数据库业务 schema 和计费规则。
5. RuntimeBridge contract 可在未来由独立进程或其他仓库实现。
6. 保持当前默认单进程部署，不引入新的运行时容器、外部 RPC 或数据库迁移。

## 非目标

- 本阶段不切换 CLIProxyAPI，也不实现第二个 Driver。
- 本阶段不把 Sub2API 拆成独立 Docker 服务。
- 本阶段不重写 Sub2API 的调度、OAuth、协议转换、重试或失败切换算法。
- 不修改外部 HTTP API、前端、数据库 schema、套餐优先、余额回退和倍率规则。
- 不恢复旧 Group、Channel、BillingProfile 或旧运行时兼容配置。
- 不为了迁移而新增 silent fallback；生产路径始终只有一个明确的 Runtime 实现。
- 不在 Runtime 中执行定价、套餐选择或余额扣费。

## 当前事实与问题边界

### 已经成立的边界

- `backend/internal/productcore` 是产品决策层，已使用纯领域值对象。
- `backend/internal/applicationgateway` 负责把产品决策快照绑定到 Runtime 和 UsageSink。
- `backend/internal/gatewayruntime` 已包含请求、响应、HTTP exchange、UsageFacts、UsageEvent 和终态记录器。
- OpenAI Chat、Responses、Messages、Images 已有大量纯 exchange 生产路径和回归测试。

### 尚未完成的隔离

- `NewSub2APIProductionApplicationGateway` 仍接收 `GatewayHandler`、`OpenAIGatewayHandler` 和 Service 实例。
- 部分 executor 仍通过 `ginContextCarrier` 取回 Gin context，再调用旧 Handler。
- `GatewayService` 和 `OpenAIGatewayService` 同时持有账号仓库、用户订阅、计费、缓存、并发和调度依赖。
- `Sub2APIProductUsageFinalizer` 仍把 Runtime 事实重建为旧 Service 的 `RecordUsage` 输入。
- `gatewayruntime` 位于 `internal` 目录，独立 Driver 无法从其他 Go module 导入。

这些事实决定了本轮必须先做包级边界和端口化，不能先做网络拆分。

## 目标架构

```text
HTTP Handler / Middleware
        |
        v
Runtime Ingress
  - 认证快照
  - 请求体和端点标准化
  - ProductCore 预检
        |
        v
XCode ApplicationGateway
  - ProductCore Decision
  - RuntimeBridge Client
  - Product UsageSink
        |
        v
RuntimeBridge Contract
        |
        v
Sub2API Driver
  - 账号池 / OAuth
  - 协议转换
  - 重试 / 冷却 / 失败切换
  - 上游 HTTP / SSE / WebSocket
        |
        v
上游服务
```

默认部署仍为一个 Go 进程。`RuntimeBridge Contract` 是编译和测试边界，不等同于立即拆服务。未来外置时，仅替换 `RuntimeBridge Client` 的传输实现，并在独立 Bridge 内保留 Sub2API Driver。

## 包与依赖边界

### `backend/pkg/runtimebridge/v1`

这是未来可被其他仓库导入的稳定 contract，只允许标准库依赖。它包含：

- 请求和平台路由快照；
- 端点、模型、stream 和请求体元数据；
- 非流式响应和流式事件；
- 实际账号、实际上游端点、实际上游模型；
- Token、缓存 Token、图片/视频数量、首字延迟和总耗时；
- RuntimeError 分类、是否可重试和失败切换事实；
- Capability、AccountProbe 和健康结果；
- contract 版本、RequestID 和终态序号。

它禁止出现 Gin、Ent、`internal/service`、APIKey、UserSubscription、余额、套餐和具体 Sub2API 类型。

### `backend/internal/gatewayruntime`

现有内部类型先作为迁移兼容层保留，并逐步改为引用或别名到 `pkg/runtimebridge/v1`。生产代码不得继续新增只存在于内部包的运行时字段。兼容层完全收口后再删除，避免一次性改动全部 Handler。

### `backend/internal/runtimebridge`

负责 XCode 侧的 Bridge client/runner 组合：

- 把 ProductCore 的决策快照转换为 contract request；
- 在进程内调用 Driver；
- 未来通过 HTTP/SSE transport 调用独立 Bridge；
- 将 terminal event exactly-once 投递给 Product UsageSink；
- 不持有产品实体，不实现扣费。

### `backend/internal/runtime/sub2api`

这是 Sub2API Driver 的目标目录。迁移期间可以调用现有账号、OAuth、协议和调度 Service，但调用必须通过明确的 Runtime 端口；不得从 Driver 反向调用公开 Handler，也不得从 Exchange 取出 Gin context。

### `productcore` 与 `applicationgateway`

只依赖 `pkg/runtimebridge/v1` 的值对象和自己的产品端口。它们不得导入 Sub2API Driver、Gin、Ent 或旧 Service。

## RuntimeBridge Contract

### 请求

请求必须包含以下不可变字段：

```go
type Request struct {
    ContractVersion string
    RequestID       string
    Platform        PlatformRoute
    Endpoint        Endpoint
    Stream          bool
    Payload         []byte
    Headers         map[string]string
    Owner           OwnerRef
    Session         SessionMetadata
}

type PlatformRoute struct {
    ID                   int64
    Code                 string
    RuntimeAdapter       string
    RequestedModel       string
    UpstreamModel        string
    EndpointCapabilities []string
}

type OwnerRef struct {
    UserID   int64
    APIKeyID int64
}
```

`OwnerRef` 只用于请求关联、会话粘连和审计，不代表 Runtime 可以读取产品资产。请求中不得出现倍率、订阅 ID、余额、套餐额度或最终费用。

### 响应与流式事件

本地实现和未来 HTTP/SSE 实现都使用同一组语义事件：

- `response_started`：状态码和安全响应头；
- `response_chunk`：原始 SSE/JSON/二进制响应片段；
- `response_finished`：客户端已可见的正常终态；
- `runtime_failed`：结构化 RuntimeError；
- `usage_final`：唯一最终 UsageFacts；
- `stream_cancelled`：客户端或上游取消的明确终态。

流式响应不能用“连接断开即成功”推断终态。Driver 必须明确记录是否已发送首字节、是否收到上游终态、客户端是否中途断开以及最终上游账号。

### 用量事实

`UsageFacts` 只描述运行事实，不描述扣费决定：

- RequestID、实际 AccountID、平台和端点；
- 请求模型、实际上游模型和计费模型标识；
- 输入、输出、缓存读写 Token；
- 图片/视频数量；
- FirstTokenMilliseconds 和 DurationMilliseconds；
- 客户端 stream、部分响应和 terminal status；
- 失败切换次数和安全错误分类。

最终扣费仍由 ProductCore 创建的 UsageSink 完成。每个请求只能产生一个终态事件；成功终态才允许进入扣费，失败或取消终态只能形成可审计的运行事实。UsageSink 按 RequestID 和 terminal sequence 去重。

## 请求数据流

1. Handler 从认证中间件提取标量 UserID、APIKeyID，并读取请求体。
2. ProductCore 根据模型、平台能力和 API Key 授权选择 PlatformRoute 与 BillingAsset。
3. ApplicationGateway 复制 DecisionSnapshot，创建 Product UsageSink。
4. RuntimeBridge 将 PlatformRoute 转成 contract Request；不传 BillingAsset。
5. Sub2API Driver 选择可用账号，执行 OAuth、协议转换、重试和失败切换。
6. Driver 返回响应事件和唯一 `usage_final`；失败 attempt 只产生 RuntimeError/Ops 事实。
7. ApplicationGateway 将最终 UsageFacts 投递给 Product UsageSink。
8. ProductCore PricingEngine 使用模型目录计算基础费用，再应用套餐倍率或余额倍率，并写入使用记录和扣费。

失败账号不能触发最终扣费。只有真实成功终态且包含正数 AccountID 时，ProductCore 才允许成功用量归属。

## 账号身份和失败切换

- `AccountID` 是最终实际成功或最终失败归属，不能用平台 ID、调度粘连键或候选账号替代。
- Driver 可以在内部尝试多个账号，但只提交一个最终 UsageFacts。
- RuntimeError 必须包含 `category`、`retryable`、`attempted_accounts`（仅 ID）和安全诊断信息；禁止凭证、Token 和完整请求体。
- 账号能力必须按请求端点判断；不能因为平台支持 Responses 就把 Chat 请求强行送到 Responses，反之亦然。
- OAuth 刷新、429 冷却、502/503 失败切换和首字超时继续由 Sub2API Driver 负责。

## 计费与使用记录隔离

- Runtime 不读取 UserSubscription、余额或套餐实体。
- ProductCore 的 DecisionSnapshot 在请求开始时冻结；Runtime 不能修改它。
- 套餐倍率和余额倍率只在 ProductCore/UsageSink 中计算，不能传给 Runtime。
- 使用记录同时保存平台、实际账号、实际上游端点、模型、Token、延迟、缓存率、资产来源和最终费用。
- UsageSink 失败必须对调用方可见，不能用零费用或默认倍率吞掉错误。
- 异步写入必须携带独立于父请求取消信号的 RequestID、DecisionSnapshot 和 UsageFacts 副本。

## 进程外置条件

本阶段不启用独立进程。只有以下条件全部满足后才允许增加外置 Bridge：

1. 所有生产端点不再依赖 Gin carrier、公开 Handler 和隐式 Service context。
2. `pkg/runtimebridge/v1` 有跨实现 contract conformance tests。
3. Runtime 能通过明确的 RuntimeStore/ControlPort 访问账号、OAuth 和平台运行时配置。
4. 账号管理、OAuth 刷新和能力探测有明确的 Bridge Control API 或事件同步机制。
5. HTTP/SSE transport 已覆盖首字延迟、客户端取消、断线、重试和唯一终态。
6. ProductCore 与 Bridge 使用服务鉴权、contract version 和健康检查。
7. 单进程与外置模式的黑盒回归结果一致。

外置后，XCode 不与 Bridge 共享 Gin、Ent 实体或内部 Service。数据库是否共享必须以 RuntimeStore 所有权为准，不能让两个进程随意写同一组运行时表。

## 测试策略

### Contract tests

- Chat、Responses、Messages 的 JSON/SSE 和强制/自动端点选择；
- OpenAI Images、Count Tokens 及当前已迁移入口；
- 多账号、失败切换、OAuth 刷新、429 冷却和首字超时；
- 实际 AccountID、UpstreamEndpoint、Token、缓存 Token、延迟和终态；
- 失败 attempt 不扣费、成功只扣一次、客户端取消不重复扣费。

### Architecture guards

- `pkg/runtimebridge/v1` 和 `productcore` 不导入 Gin、Ent、`internal/service` 或 Sub2API 类型；
- Driver 不导入公开 Handler，不实现 `ginContextCarrier`，不通过 type assertion 取 Gin；
- Runtime contract 不出现 APIKey、UserSubscription、Balance 或套餐实体；
- 生产路径只有一个 Runtime 注册；
- 不新增旧 Group/Channel 运行时权威；
- 已发布迁移 checksum 和 `8000/9000` 编号规则继续受守卫保护。

### 发布验证

- 后端 unit/integration、前端测试、typecheck、lint 和生产构建；
- Docker 单进程镜像和离线包构建；
- 现有服务器部署只替换应用镜像，不重建 PostgreSQL、Redis 和数据卷；
- GPT Chat、GPT Responses、GLM Chat、失败切换、套餐扣费、余额回退和使用记录各执行两轮真实验证。

## 回滚与兼容

- 第一阶段不改变数据库 schema、外部 API 和部署文件。
- 每个端点迁移独立提交，任何切片失败都可以回滚到上一版 Sub2API Driver。
- 不保留“新 Driver 失败后静默调用旧 Handler”的 fallback；迁移未完成的端点明确留在旧 Driver 注册表中。
- 只有全部端点通过 contract conformance tests 后，才删除对应旧兼容桥。

## 验收标准

1. ProductCore 只看到 RuntimeBridge contract，不看到 Sub2API 内部类型。
2. Sub2API 只作为 Driver 负责运行时，不参与套餐、余额、倍率和扣费决定。
3. 默认单进程部署的外部行为、失败切换和计费结果与当前版本一致。
4. 未来新增 Driver 只需实现 contract 和 conformance tests，不修改 ProductCore、前端或数据库业务 schema。
5. 进程外置是可选部署形态，而不是当前版本的运行前提。

## 相关现有文件

- `backend/internal/productcore/`
- `backend/internal/applicationgateway/`
- `backend/internal/gatewayruntime/`
- `backend/internal/handler/sub2api_runtime_composition.go`
- `backend/internal/handler/sub2api_runtime_adapter.go`
- `backend/internal/handler/sub2api_messages_executor.go`
- `backend/internal/service/sub2api_product_usage_finalizer.go`
- `backend/internal/service/platform_asset_request.go`
- `docs/superpowers/specs/2026-08-10-runtime-adapter-boundary-design.md`
- `docs/superpowers/specs/2026-08-11-sub2api-adapter-purification-design.md`
