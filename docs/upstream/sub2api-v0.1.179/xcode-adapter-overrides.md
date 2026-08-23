# XCode Adapter/Port 覆盖清单

本文列出同步 Sub2API `v0.1.179` 时必须保留的 XCode 所有权点。官方代码只能在这些边界后同步，不能通过整文件覆盖把 ProductCore 或 RuntimeBridge 重新耦合进官方 Handler/Service。

## RuntimeBridge v1 公开契约

- XCode 路径：`backend/pkg/runtimebridge/v1/types.go`、`events.go`、`errors.go`。
- 用途：定义纯 Go 的 Request、HTTPExchange、UsageSink、终态事件和错误语义，是 ProductCore 与 Runtime 的公开边界。
- 官方对应：官方没有等价稳定 Port；其 Handler/Service 内部类型只能由 Driver 转换，不能泄漏到本包。
- 保留/替换规则：保留公开类型和 exactly-once 终态语义。阶段二/四如确需扩展字段，先做 contract 变更审查，不允许引入 Gin、Ent、Service 或 Provider 内部类型。
- 契约测试：`backend/pkg/runtimebridge/v1/contract_test.go`、`conformance_test.go`。
- 负责阶段：2、4、6。

## ApplicationGateway 与 GatewayRuntime

- XCode 路径：`backend/internal/applicationgateway/gateway.go`；`backend/internal/gatewayruntime/errors.go`、`exchange.go`、`intent.go`、`runtime.go`、`terminal.go`、`types.go`、`usage_context.go`。
- 用途：ApplicationGateway 组合 ProductCore 授权决策和 Runtime 执行；GatewayRuntime 保存内部 intent、交换和终态约束。
- 官方对应：官方公开 Handler 到 Service 的直接调用链。
- 保留/替换规则：保留 XCode 编排；官方协议或 Provider 代码不能直接决定用户平台、套餐、余额或扣费。Runtime 成功只上报 facts，失败只上报失败终态。
- 契约测试：`backend/internal/applicationgateway/gateway_test.go`、`backend/internal/gatewayruntime/runtime_test.go`、`intent_test.go`。
- 负责阶段：2、3、4、6。

## Sub2API Driver

- XCode 路径：`backend/internal/runtime/sub2api/adapter.go`、`openai_executor.go`、`registry.go`。
- 用途：把 RuntimeBridge 请求转换为当前唯一 Sub2API Runtime 的执行调用，并注册 Provider executor。
- 官方对应：`backend/internal/service/openai_gateway*`、Provider Service、协议转换和调度实现。
- 保留/替换规则：目录继续只拥有 Runtime 执行，不导入 Handler/Gin/ProductCore。官方核心尽量按原函数结构同步，XCode 差异集中到本 Driver 和 Port。
- 契约测试：`backend/internal/runtime/sub2api/adapter_test.go`、`openai_executor_test.go`；架构守卫 `backend/internal/architecture/runtime_sub2api_purity_guard_test.go`。
- 负责阶段：2、3、4。

## Runtime Ingress

- XCode 路径：`backend/internal/handler/runtime_ingress.go`、`runtime_ingress_preflight.go`。
- 用途：解析公开请求，执行模型、平台、API Key、资产资格、并发和内容策略预检，再交给 ApplicationGateway。
- 官方对应：官方 OpenAI/Anthropic/Gemini/Grok 等公开 Handler。
- 保留/替换规则：保留 ProductCore 预检和错误可见性；新增端点只能复用共同 ingress，不得绕过 API Key 平台授权或资产检查。
- 契约测试：`backend/internal/handler/runtime_ingress_test.go`、`runtime_ingress_preflight_test.go`。
- 负责阶段：2、4、5。

## Sub2API Handler Adapter 与 Port

### 组合与注册

- XCode 路径：`backend/internal/handler/sub2api_runtime_composition.go`、`sub2api_runtime_registry.go`、`sub2api_runtime_adapter.go`、`sub2api_execution.go`、`sub2api_gateway_entrypoints.go`。
- 用途：组合本地 Runtime、注册 executor、连接 ingress 与 Driver。
- 官方对应：官方 Handler 构造和 Service wiring。
- 保留/替换规则：保留本地端口和注册入口；新 Provider 默认没有平台、没有账号且不注册生产流量，管理员配置后才可调度。
- 契约测试：`sub2api_runtime_registry_test.go`、`sub2api_runtime_adapter_test.go`。
- 负责阶段：2、3、4、5。

### OpenAI/Messages/同步执行

- XCode 路径：`backend/internal/handler/sub2api_openai_port.go`、`sub2api_openai_executor.go`、`sub2api_messages_executor.go`、`sub2api_sync_port.go`、`sub2api_sync_executor.go`。
- 用途：把 RuntimeBridge HTTPExchange 映射到 Chat、Responses、Messages 和同步请求实现。
- 官方对应：官方 OpenAI gateway、apicompat、Responses WebSocket 和 Provider Service。
- 保留/替换规则：同步官方协议算法，但继续通过 XCode Port 产生唯一终态；流式写出后遵守 `SafeToFailoverAfterWrite`。
- 契约测试：对应的 `sub2api_openai_*_test.go`、`sub2api_messages_executor_test.go`、`sub2api_sync_executor_test.go`。
- 负责阶段：2。

### 媒体与扩展端点

- XCode 路径：`backend/internal/handler/sub2api_media_port.go`、`sub2api_gemini_media_executor.go`、`sub2api_auxiliary_executor.go`。
- 用途：承接图片、视频、音频、Search、Live 和其他扩展终态。
- 官方对应：官方 Images/Grok/Gemini/Live/Search Handler 与异步任务 Service。
- 保留/替换规则：Runtime 拥有 Provider 异步状态，ProductCore 拥有用户授权和最终扣费；失败不能伪造成功 usage。
- 契约测试：`sub2api_gemini_media_executor_test.go`、`sub2api_auxiliary_executor_test.go`。
- 负责阶段：4、6。

### 旧路径隔离

- XCode 路径：`backend/internal/handler/sub2api_legacy_driver.go`、`sub2api_legacy_dispatch.go`。
- 用途：仅承载尚未迁完的官方旧调用点，受调用数量守卫约束。
- 官方对应：官方直接 Handler/Service 调用。
- 保留/替换规则：不得增加新旧调用点；阶段二至四只允许减少。新增 fallback 或静默降级必须另行确认。
- 契约测试：`sub2api_legacy_driver_test.go`、`sub2api_legacy_dispatch_test.go`、`backend/internal/architecture/sub2api_adapter_purity_guard_test.go`。
- 负责阶段：2、3、4。

## ProductCore 平台和 AI 账号边界

- XCode 路径：`backend/internal/productcore/authorizer.go`、`ports.go`、`types.go`；`backend/internal/service/platform.go`、`platform_service.go`、`platform_account_pool.go`、`platform_asset_request.go`、`platform_model_policy.go`、`platform_model_rules.go`、`platform_models_list.go`。
- 用途：平台是模型授权和 AI 账号池配置入口；用户账号由 ProductCore 管理，AI 账号运行状态由 Runtime 管理。
- 官方对应：官方 Group/Channel/Account Service 和调度选择。
- 保留/替换规则：不恢复 Group/Channel 产品模型。官方 Provider 能力映射到平台和 AI 账号配置；未配置 Provider 明确不可用且不能接收流量。
- 契约测试：`platform_account_pool_test.go`、`platform_account_pool_gateway_test.go`、`platform_model_policy_test.go`、`platform_model_rules_test.go`、`platform_service_test.go`。
- 负责阶段：3、5。

## ProductCore 模型目录和价格

- XCode 路径：`backend/internal/service/model_pricing_catalog.go`、`model_pricing_resolver.go`、`model_pricing_types.go`、`model_platform_detection.go`、`sub2api_pricing_adapter.go`。
- 用途：模型广场、模型价格、平台候选和最终价格解析。
- 官方对应：官方 Channel pricing、Group model pricing、服务层级/上下文倍率和 Provider 价格事实。
- 保留/替换规则：保留 XCode 价格源和用户资产语义。官方 service tier、Fast、媒体和长上下文信息只能作为 pricing facts 输入；不得采用官方破坏性默认倍率。
- 契约测试：`model_pricing_catalog_test.go`、`sub2api_pricing_adapter_test.go`、`account_long_context_billing_test.go`、`crs_sync_long_context_billing_test.go`。
- 负责阶段：5、6。

## ProductCore usage、套餐、余额和扣费

- XCode 路径：`backend/internal/service/product_usage_sink.go`、`sub2api_product_usage_finalizer.go`、`gateway_usage_billing.go`、`usage_billing.go`、`billing_service.go`、`billing_multiplier_selection.go`、`user_subscription.go`、`subscription_plan_snapshot.go`。
- 用途：接收唯一成功终态，解析基础费用，选择套餐或余额，写 usage 和最终扣费来源。
- 官方对应：官方 UsageLog、Group/Channel billing、subscription quota 和媒体计费。
- 保留/替换规则：Runtime 不写用户余额或套餐用量。失败终态不得产生成功 usage；重复终态不得重复扣费；购买时倍率快照保持不变。
- 契约测试：`product_usage_sink_test.go`、`sub2api_product_usage_finalizer_test.go`、`runtime_boundary_billing_contract_test.go`、`gateway_usage_billing_fallback_test.go`、`subscription_plan_snapshot_test.go`。
- 负责阶段：2、4、6。

## 架构门禁

- `backend/internal/architecture/legacy_configuration_guard_test.go`：ProductCore/Runtime contract 不导入应用框架和数据库层。
- `backend/internal/architecture/runtime_external_gate_test.go`：未来进程外 Runtime 需要显式 Store/Control Port。
- `backend/internal/architecture/migration_namespace_guard_test.go`：Runtime migration 只允许 `8000-8999`，ProductCore 只允许 `9000-9999`。
- `backend/internal/architecture/sub2api_adapter_purity_guard_test.go`：旧 Sub2API 调用点不得增长。
- `backend/internal/architecture/runtime_sub2api_purity_guard_test.go`：Driver 不依赖 Handler/Gin。
