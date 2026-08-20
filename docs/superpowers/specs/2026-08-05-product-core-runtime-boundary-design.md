# My2 产品核心与 Sub2API 运行时隔离设计

> **边界继续有效，兼容策略已修订：** ProductCore 与 GatewayRuntime 的职责边界继续保留；
> 本文件中的 Legacy Key、旧 Group 路由兼容和旧定价引用仅描述第一阶段实现，已被
> [平台唯一入口与账号适配器派生设计](2026-08-06-platform-pool-account-adapter-design.md)
> 替代。

## 状态

- 状态：已确认，等待实施
- 日期：2026-08-05
- 开发分支：`my2.0`
- 前置设计：[平台与资产解耦](2026-08-04-platform-assets-decoupling-design.md)

## 目标

在不替换 Sub2API 账号调度、OAuth 刷新、协议适配和上游故障切换的前提下，明确切开两个长期职责：

- `ProductCore`：用户、API Key、平台授权、套餐、余额、倍率、资产选择、用量归属和产品后台。
- `GatewayRuntime`：账号凭据、OAuth、账号健康、并发与调度、协议转换、流式转发、上游重试和模型基础定价。

第一阶段只在同一仓库、同一 Go 进程内建立模块和接口边界。它不是微服务拆分，不新增 Docker 容器，不迁移数据库，也不改变前端 UI 或现有请求语义。

## 已确认约束

1. Sub2API 的账号调度、OAuth 刷新和协议适配必须保留，不能重写或替换。
2. 平台账号池仍由现有运行时实际调度；产品核心只能指定已授权的平台范围，不能挑选或管理具体账号。
3. 现有 V2 资产规则保持不变：Key 先验证平台与端点能力；有效套餐按最早到期、再创建时间优先；全部套餐不可用后，只有 Key 明确允许且余额充足时才能回退余额。
4. 套餐倍率取订阅实例快照，余额倍率取全局设置；两者绝不叠乘。
5. 模型基础价格、渠道自定义价格和上游 Token 解析继续使用现有 Sub2API 实现。
6. `Group`、`BillingProfile`、`group_id`、`group_ids` 和 `account_groups` 继续只服务旧 Key、历史数据和定价兼容，不能重新成为 V2 产品决策输入。
7. 旧路径和 V2 路径的 HTTP 错误码、请求体可重复读取、订阅上下文、账号失败切换和用量归属必须保持兼容。

## 现状与切入点

现有 V2 请求在 `backend/internal/server/middleware/platform_asset_auth.go` 中读取模型，调用 `APIKeyService.ResolvePlatformAssetRequest`，再把 `GatewayPlatformAssetContext` 写入请求上下文。后续 `GatewayService`、`OpenAIGatewayService` 和 Antigravity 路径继续使用同一上下文完成平台账号池调度、模型映射、基础定价和用量记录。

这条链路已经具备正确的产品语义，但领域类型和上下文类型都位于 `internal/service`，导致产品规则直接依附于 Sub2API 网关服务。第一阶段不移动调度代码，只把“决策”和“交给运行时的意图”抽成独立的纯数据契约。

## 目标结构

```text
HTTP API Key 认证
  -> ProductCore.Authorizer
       -> 平台模型解析
       -> Key 平台权限与端点能力校验
       -> 套餐/余额资产选择
  -> GatewayRuntime.DispatchIntent
       -> Sub2API Context Bridge
       -> 现有 GatewayService / OpenAIGatewayService / AntigravityGatewayService
       -> 账号池、OAuth、调度、协议、重试和上游请求
  -> 现有用量记录与扣费收口
```

依赖方向固定为：

```text
productcore       不依赖 service、handler、repository、Gin 或 Ent
gatewayruntime    只依赖 productcore 的只读决策类型
service           实现 productcore 的端口，并桥接 gatewayruntime 到现有 Sub2API 上下文
middleware        只处理 HTTP、错误响应、订阅上下文和调用门面
gateway services  保持现有账号调度、OAuth、协议和上游转发实现
```

## 新模块职责

### `internal/productcore`

只保存与产品规则有关的纯类型和算法：

| 类型 | 责任 |
| --- | --- |
| `AccessGrant` | API Key 的用户、平台、套餐和余额授权快照 |
| `Request` | 客户端模型、由现有服务归一化后的端点能力和是否跳过计费 |
| `Platform` | 已解析的平台、账号平台标识、端点能力、公开/上游模型和只读旧定价引用 |
| `BillingAsset` | 本次使用的套餐或余额、倍率和实例事实字段 |
| `Decision` | 已授权的平台和资产选择结果 |
| `Authorizer` | 按既有顺序完成模型、授权、能力与资产选择 |

`PlatformCatalog` 与 `AssetSelector` 是端口。它们由 `internal/service` 的适配器实现，因此 `productcore` 不认识 `APIKey`、`UserSubscription`、`Group` 或数据库对象。

### `internal/gatewayruntime`

只定义运行时接收的不可变 `DispatchIntent`：

```go
type DispatchIntent struct {
    Platform     productcore.Platform
    BillingAsset *productcore.BillingAsset
}
```

它可安全写入并从 `context.Context` 读取；读写都返回深拷贝，禁止请求链路的任意下游修改共享决策。

### `internal/service`

保留现有 Sub2API 服务，并增加两类适配：

1. `PlatformAssetProductCoreAdapter`：将现有平台解析器、订阅服务和余额倍率提供者映射为 `productcore` 端口；它不自行调度账号。
2. `GatewayRuntimeBridge`：将 `DispatchIntent` 转为已有 `GatewayPlatformAssetContext`、`PlatformSchedulingScope`、公开模型、上游模型和旧定价引用。第一阶段双写新意图与旧上下文，确保现有网关无需大改。

`GatewayPlatformAssetContext`、`ResolvedPlatformModel`、`ResolvedBillingAsset` 和 `APIKeyService.ResolvePlatformAssetRequest` 仍保留为兼容门面；其实现改为调用产品核心和桥接层，不得保留第二套独立的 V2 决策逻辑。

## 运行时边界

下列文件属于受保护的运行时区域。第一阶段不得修改其业务算法：

- `backend/internal/service/gateway_scheduling.go`
- `backend/internal/service/openai_gateway_scheduling.go`
- `backend/internal/service/openai_account_scheduler.go`
- `backend/internal/service/account_credentials_persistence.go`
- `backend/internal/service/openai_gateway_*.go`
- `backend/internal/service/antigravity_gateway_*.go`
- 各 Token Provider、OAuth 登录与刷新实现

允许改变的只有入口桥接、数据类型转换和为契约补充的只读访问。任何需要改变上述运行时行为的需求必须另立设计，不能借“核心隔离”名义混入。

## 请求行为

### V2 Key

```text
request model
  -> ProductCore 解析唯一 Platform
  -> 校验 API Key 已授权该 Platform
  -> 校验该 Platform 支持当前端点
  -> 选择有效 Subscription；没有则按 allow_balance 选择余额
  -> 生成 DispatchIntent
  -> Runtime Bridge 写入现有网关上下文
  -> 原有运行时在该 Platform 账号池内调度、刷新、转发和失败切换
  -> 原有模型价格计算 x Decision 中的资产倍率
  -> 写入平台、账号、资产和倍率归属
```

### Legacy Key

没有显式 `platform_ids` 的 Key 不进入 `ProductCore`。它继续走现有 Group 兼容路径，避免架构隔离被误用为放大旧 Key 权限。

### 简易模式

简易模式继续跳过资产扣费选择，但仍可通过产品核心解析平台和端点能力。`Decision.BillingAsset` 在该模式下为空；运行时不得将空资产解释为余额授权。

## 错误兼容

产品核心可以有内部错误类型，但对 HTTP 层必须映射回当前语义：

| 产品核心条件 | 当前 HTTP 语义 |
| --- | --- |
| 模型没有启用平台 | `PLATFORM_MODEL_NOT_FOUND`，400 |
| Key 未授权平台 | `API_KEY_PLATFORM_FORBIDDEN`，403 |
| 平台不支持端点 | `PLATFORM_ENDPOINT_UNSUPPORTED`，403 |
| 套餐与余额均不可用 | `NO_USABLE_BILLING_SOURCE`，403 |
| 余额不足 | `INSUFFICIENT_BALANCE`，403 |
| 日/周/月额度耗尽 | `USAGE_LIMIT_EXCEEDED`，429 |

错误映射只发生在服务适配层或 HTTP 中间件，不能让 Gin 或 HTTP 状态码进入 `productcore`。

## 数据与 UI 边界

第一阶段不新增、删除或迁移数据库表，不改 Ent schema，不改 Vue 页面。

- 现有 `Platform`、`SubscriptionPlan`、`UserSubscription`、API Key 权限和使用记录字段继续使用原表。
- 账号管理 UI 仍展示用户确认的“平台账号池”模型；其操作继续调用现有账号服务。
- `ProductCore` 的授权快照来自已认证 API Key，不新建第二套用户、Key 或套餐数据。

## 迁移阶段

### 阶段 1：建立无行为变化的契约

- 新增 `productcore`、`gatewayruntime`、Sub2API 适配器与桥接。
- 现有 `ResolvePlatformAssetRequest` 委托给新门面。
- 保留旧上下文双写和全部原有调用点。
- 新增契约测试，确认平台、套餐、余额、订阅上下文、上下文克隆与错误响应未变。

### 阶段 2：逐步收口调用依赖

- 允许中间件依赖产品核心门面，而不直接组合 `APIKeyService`、订阅服务和平台解析器。
- 逐个将运行时读取统一改为 `GatewayRuntimeBridge`，但不改调度算法。
- 每次官方同步后只检查适配层、上下文桥和契约测试，而不是重新寻找所有业务规则。

### 阶段 3：长期维护

- 新的套餐、余额、Key 和后台功能只进入 `ProductCore` 或其适配器。
- 新的上游平台、OAuth、协议或调度功能只进入 `GatewayRuntime` 受保护区域。
- 不在本路线中替换运行时；若未来用户改变该决定，必须重新设计和评估。

## 验收标准

1. 同一组 V2 输入在改造前后得到相同的平台、资产、倍率和 HTTP 错误语义。
2. `GatewayPlatformAssetContext` 与新 `DispatchIntent` 互相转换后，平台调度范围、公开模型、上游模型和 `legacy_group_id` 定价引用相同。
3. 套餐订阅对象仍可通过 Gin 上下文供现有并发、额度和 `/v1/usage` 逻辑使用。
4. `/v1/chat/completions`、`/v1/responses` 和 Google 原生入口均保持请求体可读取、端点能力校验和平台隔离。
5. 账号选择、OAuth 刷新、协议适配和上游重试代码没有被行为性修改。
6. 不新增数据库迁移、Ent 生成物、Docker 改动或 UI 改动。

## 风险与防护

| 风险 | 防护 |
| --- | --- |
| 决策双实现逐渐漂移 | 原服务门面必须委托产品核心；测试禁止两套独立排序或权限判断。 |
| 请求上下文被下游修改 | `DispatchIntent` 写入和读取均深拷贝。 |
| 丢失订阅对象，影响并发或额度 | 服务门面额外返回 `UserSubscription`，中间件继续写 `ContextKeySubscription`。 |
| 官方同步覆盖 V2 注入点 | 使用稳定文件边界、契约测试和项目记忆中的同步检查项。 |
| 误触运行时算法 | 将调度、OAuth、协议文件列为第一阶段禁止修改区域。 |

## 相关文件

- `backend/internal/server/middleware/platform_asset_auth.go`
- `backend/internal/service/platform_asset_request.go`
- `backend/internal/service/api_key_asset_resolver.go`
- `backend/internal/service/platform_account_pool.go`
- `backend/internal/service/gateway_scheduling.go`
- `backend/internal/service/openai_gateway_scheduling.go`
- `backend/internal/pkg/ctxkey/ctxkey.go`
- `docs/memory/决策/2026-08-05-product-core-runtime-boundary.md`
