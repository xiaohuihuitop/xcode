# XCode 架构

## 分层

```text
HTTP API / Admin UI
        |
   ProductCore
  用户、Key、平台、套餐、余额、计费、用量
        |
 GatewayRuntime Port
  Request / HTTPExchange / UsageSink
        |
 Sub2API Adapter（当前唯一 Runtime）
  调度、OAuth、协议转换、重试、冷却、失败切换
        |
  OpenAI / 上游服务
```

`ProductCore` 只拥有产品和财产语义；`GatewayRuntime` 只拥有上游执行语义。适配器把两者连接起来，不能把 Gin context、公开 Handler 或 Sub2API 内部类型泄漏到 ProductCore。

## 主要实体

- `Platform`：平台、统一端点能力、模型白名单和账号池的唯一配置入口。
- `Account`：只归属一个平台；不单独维护普通模型白名单或模型映射。
- `API Key`：可同时授权多个平台、多个套餐和余额。
- `SubscriptionPlan`：金额、时长、额度、周期限制和套餐倍率。
- `UserSubscription`：购买时保存套餐倍率快照；每次购买生成独立实例，不累计原订阅时长。
- 余额：跨平台共享，使用全局余额倍率；不与套餐倍率叠加。
- 使用记录：保存 API Key、模型、端点、平台、Token、费用、延迟、输出速率、缓存率和实际扣费来源。

## 请求与计费流程

1. Runtime ingress 校验 JSON、模型、流式参数、内容策略、并发和计费资格。
2. ProductCore 根据模型候选、Key 授权、平台端点能力和有效资产解析目标平台。
3. Runtime 在该平台账号池中执行 OAuth 刷新、粘连、冷却、重试和失败切换。
4. `UsageSink` 接收唯一终态用量，PricingEngine 按最终上游模型计算基础费用。
5. 有有效套餐时使用订阅快照倍率并扣套餐；套餐不可用时，仅当 Key 明确允许余额且余额足够才扣余额。
6. 使用记录写入实际平台、账号、倍率和扣费来源；失败尝试不产生成功扣费终态。

## 平台身份

`PlatformSchedulingID` 是调度、粘连和缓存命名空间；正数 `PlatformAssetID` 是数据库、用量、Ops、风控和 Live 身份记录。两者不能混用。

## OpenAI Runtime

当前生产路径为：

`ApplicationGateway -> Sub2API OpenAI Executor -> HTTPExchange`

覆盖 Images、Chat Completions、Responses 和 Messages，包含 JSON/SSE 转换、usage 提取、端点 marker、错误透传、失败切换和 exactly-once 终态。生产执行器不重新接收 Gin context，也不回调旧公开 Handler。

## 数据库与迁移边界

- 用户、Key、余额、套餐、订阅、支付审计和历史使用记录必须保留。
- 仍被历史数据引用的旧行只读归档，不参与新请求。
- 已发布自定义 migration `192-200` 冻结；官方同步迁移使用 `8000-8999`，ProductCore 自有迁移使用 `9000-9999`。
- 更新镜像不能重建 PostgreSQL、Redis 或其数据卷。

## 内核替换边界

替换上游内核时，只实现同一组 Runtime 端口，不修改 ProductCore、前端、计费和业务 schema。Sub2API 的调度、OAuth 和协议算法在当前适配器内保持原样；后续内核必须通过纯 Go 请求和交换接口接入。
