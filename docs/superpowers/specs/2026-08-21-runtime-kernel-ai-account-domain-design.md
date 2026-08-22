# Runtime Kernel 与 AI 账号域分离设计

## 状态

- 状态：暂缓，非当前实施项
- 原确认日期：2026-08-21
- 当前修正日期：2026-08-22
- 目标 Runtime：继续使用 Sub2API；CAP 替换不进入当前生产路线
- 修正原因：用户确认当前 GPT 生产路径继续使用 Sub2API 作为唯一 Runtime；本设计保留为历史边界分析，不作为实施计划。
- 部署形态：同一 Go 进程、同一生产部署，不新增独立容器
- 生产原则：任意时刻只有一个 Runtime Kernel，不做 Sub2API/CAP 并行流量切换

## 背景与结论

XCode 中存在两类完全不同的账号：

1. 用户账号：使用 XCode 的人或组织，拥有 API Key、套餐、余额、权限和计费关系。
2. AI 账号：OpenAI、Claude、Gemini、GLM 等上游账号，拥有 OAuth/API Key、provider quota、冷却状态和上游模型能力。

本设计确认以下长期边界：

- XCode ProductCore 只管理用户账号和产品财产语义。
- Runtime Kernel 只管理 AI 账号和上游执行语义。
- Sub2API 与 CAP 都是 Runtime Kernel 的具体实现，不暴露给 ProductCore、前端或用户。
- CAP 替换 Sub2API 是一次性 Runtime 替换，不是按平台或请求选择两个 Runtime。
- 迁移期间允许保留旧版本回滚包，但旧 Runtime 不参与正常流量。

## 目标

1. 让用户账号与 AI 账号在代码、存储访问和接口语义上分离。
2. 让 XCode 上层只依赖统一 Runtime Kernel 契约。
3. 让 CAP 接管 AI 账号池、账号选择、OAuth、quota 观察、冷却、重试和失败切换。
4. 保留 XCode 的用户授权、套餐、余额、定价、扣费和 usage exactly-once 语义。
5. 保留香港生产环境现有 AI 账号 ID、平台归属和历史 usage 引用。
6. 在不拆独立容器的前提下完成 CAP 集成。

## 非目标

- 不让 CAP 管理 XCode 用户、API Key、套餐、余额或用户扣费。
- 不让 XCode 在每次请求中选择具体 AI 账号。
- 不让 Sub2API 与 CAP 在生产中并行承接请求。
- 不增加按平台、按用户或按请求的 Runtime 开关。
- 第一阶段不重建 PostgreSQL、Redis 或历史 usage 表。
- 不新增失败后的静默旧 Runtime fallback。
- 不把上游 provider quota 当成 XCode 用户套餐额度。

## 领域边界

### XCode 用户域

XCode 继续拥有：

- `User` 身份、登录和管理员关系；
- API Key 和用户授权；
- Platform 产品配置、模型规则和端点权限；
- Subscription、余额、倍率和扣费；
- 用户侧 usage、订单、支付审计和管理后台；
- 请求前的产品策略与计费资格判断。

### Runtime AI 账号域

Runtime Kernel 负责：

- AI 账号记录和凭据生命周期；
- provider OAuth/API Key 刷新；
- AI 账号模型目录和端点能力；
- provider quota、credits、reset window 的观察；
- 账号健康、禁用、冷却、负载和权重；
- 账号选择、会话粘性、重试和失败切换；
- 上游协议转换、流式传输、错误归一化和 usage 提取。

`Platform` 仍属于 XCode，因为它表示用户可授权的平台和产品路由；但 Platform 不负责选择具体 AI 账号。Runtime 返回的账号快照只能作为 XCode 后台的脱敏运维读模型，不成为 ProductCore 的调度事实源。

## 目标架构

```text
HTTP API / Admin UI
        |
        v
XCode ProductCore
  用户账号、API Key、Platform、套餐、余额、计费、用户 usage
        |
        v
唯一 RuntimeKernel
  AI 账号池、OAuth、quota、冷却、负载、失败切换、上游执行
        |
        v
CLIProxyAPI (CAP)
        |
        v
OpenAI / Claude / Gemini / GLM / 其他上游
```

当前生产路径仍然可以是：

```text
XCode ProductCore -> RuntimeKernel(Sub2API)
```

CAP 通过 conformance tests 后，生产组合根整体改为：

```text
XCode ProductCore -> RuntimeKernel(CAP)
```

生产中只存在一个绑定，不存在每个平台或每个请求的 Runtime 选择。

## Runtime Kernel 与 Control Plane 契约

请求执行层和 AI 账号管理层分为两个接口。上层不传入具体 AI 账号，也不传入用户套餐或余额实体。

```go
type RuntimeKernel interface {
    Dispatch(context.Context, RuntimeRequest, EventSink) (RuntimeResult, error)
    Capabilities(context.Context, CapabilityRequest) (CapabilitySnapshot, error)
}

type RuntimeControlPlane interface {
    ListAccounts(context.Context, AccountQuery) ([]AccountSnapshot, error)
    UpsertAccount(context.Context, AccountSpec) (AccountSnapshot, error)
    RemoveAccount(context.Context, RuntimeAccountRef) error
    RefreshAccount(context.Context, RuntimeAccountRef) (AccountSnapshot, error)
}
```

`RuntimeKernel` 只负责请求执行；`RuntimeControlPlane` 负责 AI 账号的新增、删除、OAuth/凭据刷新、quota 探测和运维快照。两者共享 Runtime AI 账号域，但不把控制操作混入用户请求路径。

### RuntimeRequest

请求包含：

- RequestID；
- Platform/provider 路由快照；
- endpoint、model、stream 和 payload；
- 会话粘性信息；
- 用户/API Key 的审计引用。

请求禁止包含：

- 具体 AI 账号 ID；
- AI OAuth token 或 API Key；
- UserSubscription、余额、套餐倍率或最终费用。

### RuntimeResult 与终态

Runtime 返回：

- 响应状态、响应头、响应体或流式结果；
- 实际成功 AI 账号引用；
- 实际上游 endpoint/model；
- UsageFacts；
- 明确的成功、失败或取消终态。

Runtime 内部可以尝试多个 AI 账号，但对 XCode 只提交一个最终账号事实和一个终态事件。XCode 的 UsageSink 继续按 RequestID 做 exactly-once 扣费。

### AccountSnapshot

Runtime 对 XCode 暴露脱敏快照：

- Runtime account ID；
- provider 和模型能力；
- active/disabled/cooling/unavailable 状态；
- quota/credits 摘要和 reset 时间；
- 当前负载、错误率和最近观测时间；
- 最近错误的安全分类。

快照不包含 token、API Key、完整请求体或敏感诊断。

### 管理面

AI 账号的新增、删除、OAuth 登录、凭据刷新、quota 查询和冷却重置由 Runtime Control Plane 实现。XCode 后台可以提供统一入口，但只作为鉴权和审计门面，将操作转发给 Runtime；XCode 不直接读写 AI 凭据，也不直接修改 Runtime 调度状态。用户账号、API Key、套餐、余额和扣费管理仍由 XCode 自己处理。

## 账号存储与迁移

### 第一阶段：逻辑迁移

不立即重建数据库。新增 `RuntimeAIAccountStore` 端口，把现有 AI 账号 repository 包装为 Runtime 所有的存储接口。迁移期间：

- 现有 AI 账号 ID 保持不变；
- 为每个 AI 账号建立稳定的 `RuntimeAccountRef`，至少包含 XCode 历史整数 ID 和 Runtime 内部字符串 ID 的映射；
- 现有平台归属和凭据保持不变；
- 历史 usage 的 `account_id` 继续表示 AI 账号引用；
- ProductCore 停止直接调用 AI 账号调度、OAuth 和 quota 逻辑；
- Runtime 通过 store 读取和更新 AI 账号。

### CAP 接入

CAP 使用同进程 SDK 接入。CAP 的 auth manager、scheduler 和 provider executor 通过 Runtime 适配器使用 Runtime AI 账号 store。CAP 的字符串 auth ID 不得直接替代历史整数 AI 账号 ID，必须由 Runtime 维护双向映射。需要验证当前 CAP v7 SDK 的配置、auth manager、token provider 和模型注册接口与 XCode 的凭据格式兼容；如果 SDK 默认只支持文件 auth store，则实现 XCode 凭据到 CAP auth 的受控内存加载和刷新桥接，禁止把凭据复制到 ProductCore 的用户接口。

CAP 不建立用户账号体系，也不写 XCode 用户、套餐和扣费表。

### 后续物理拆分

只有在 CAP 运行稳定后，才评估将 AI 账号物理迁移到 Runtime 专用表或专用存储。候选结构包括：

- `runtime_ai_accounts`；
- `runtime_ai_account_credentials`；
- `runtime_ai_account_states`；
- `runtime_ai_quota_snapshots`；
- `runtime_ai_scheduler_events`。

物理拆分不是 CAP 接入的前置条件，且必须保留历史 usage 的账号引用映射。

## 单 Runtime 替换流程

1. 在本地或复制数据的隔离环境中实现 CAP Runtime。
2. 用 Sub2API 当前回归矩阵验证 CAP：Chat、Responses、SSE、模型路由、账号调度、quota、错误终态和扣费。
3. 完成现有 AI 账号到 Runtime store/CAP auth manager 的迁移验证。
4. 在一个新版本中将生产组合根从 Sub2API Driver 改为 CAP Driver。
5. 香港生产整体升级到该版本，所有请求统一进入 CAP。
6. 保留升级前 Sub2API 镜像和数据库备份作为部署级回滚手段，不做请求级 fallback。
7. CAP 稳定后，再清理 Sub2API 兼容代码和旧 Runtime 专用路径。

## 调度职责

Runtime 内部只有一个账号调度器。XCode 不参与具体账号选择，只传递产品约束和请求上下文。

CAP 可以使用自己的：

- round-robin、weighted、fill-first 和插件调度；
- auth 状态和模型状态；
- cooldown/backoff；
- provider-specific credits/quota 观察；
- 会话粘性和失败切换。

XCode 仍然负责用户套餐额度、余额和扣费。上游 AI 账号的 quota 只能影响 Runtime 是否选择该 AI 账号，不能直接改变用户套餐扣费结果。

## 旧字段和组合根迁移

当前 `Platform.RuntimeAdapter` 不能继续作为按平台选择 Sub2API/CAP 的开关。单 Runtime 替换完成后，生产组合根只绑定一个 `RuntimeKernel`；该字段在迁移期仅作为兼容数据保留，后续应改名为 provider/runtime capability hint，或从产品路由契约中删除。具体 Runtime 选择不进入平台配置，也不进入请求参数。

当前 `NewSub2APIProductionApplicationGateway` 是唯一需要替换的生产组合点之一。实现阶段应新增 CAP 组合根并在发布版本中整体替换绑定，而不是保留两个可选执行器供请求路径选择。

## 错误与计费

- AI 账号失败、冷却或切换不产生成功扣费；
- 只有 Runtime 正常成功终态才提交成功 UsageFacts；
- 客户端中途取消必须形成明确取消终态；
- XCode 按最终 usage 和自己的 PricingEngine 扣用户套餐或余额；
- Runtime 不读取 UserSubscription、余额或倍率；
- Runtime 错误不能泄露 token、API Key 或完整上游响应。

## 测试与验收

### Contract tests

- Chat Completions、Responses、SSE；
- 多 AI 账号选择、冷却、重试和失败切换；
- OAuth 刷新和凭据失效；
- provider quota/credits 快照；
- 实际 AI 账号 ID、上游 endpoint/model 和 usage；
- 成功、失败、部分流式和取消终态。

### Product regression

- 用户 API Key 权限；
- 套餐扣费 exactly-once；
- 余额回退；
- 用户 usage、平台归属和历史账号引用；
- 管理后台用户域与 AI 账号运维视图不混淆。

### Architecture guards

- ProductCore 不导入 CAP、Sub2API、Gin、Ent AI 账号实体或 provider executor；
- Runtime 不读取 UserSubscription、余额或套餐实体；
- Runtime Control Plane 是 AI 账号凭据和调度状态的唯一写入口；
- XCode 后台对 AI 账号的操作必须经过 Runtime Control Plane 并留下审计记录；
- 生产组合根只注册一个 Runtime Kernel；
- 不存在按平台/请求的 Sub2API/CAP runtime switch；
- AI 账号凭据不进入用户 API 或日志；
- 历史 usage 的 AI 账号引用保持可解析。

## 相关文件

- `docs/ARCHITECTURE.md`
- `backend/pkg/runtimebridge/v1/`
- `backend/internal/runtimebridge/`
- `backend/internal/handler/sub2api_runtime_composition.go`
- `backend/internal/service/account.go`
- `backend/internal/service/openai_account_scheduler.go`
- `backend/internal/service/openai_quota_service.go`
- `backend/internal/service/antigravity_quota_fetcher.go`
- `docs/superpowers/specs/2026-08-20-runtimebridge-sub2api-separation-design.md`
