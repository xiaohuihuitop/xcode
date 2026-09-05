# XCode Adapter/Port 覆盖清单（Sub2API v0.2.0）

## 必须保留的边界

- `backend/pkg/runtimebridge/v1/`：公开 Runtime 契约。任何字段变化都属于 public contract 变更，必须单独确认；禁止把 Gin、Ent、官方 Group/Channel 或 Provider 内部类型带入。
- `backend/internal/applicationgateway/` 与 `backend/internal/gatewayruntime/`：继续组合 ProductCore 授权、Runtime 执行和 exactly-once 终态。
- `backend/internal/runtime/sub2api/`：只拥有 Runtime driver/executor；官方协议文件只能落到 `upstream/`，不能直接调用 ProductCore。
- `backend/internal/handler/runtime_ingress*` 与 `sub2api_*` Port：所有公开端点继续经过 API Key、平台、模型、资产和并发预检。
- `backend/internal/productcore/`、平台/AI 账号、模型目录、价格、套餐、余额和 usage sink：继续是产品行为唯一所有者。
- 旧 Sub2API 调用点只能减少，不能新增；不得用 fallback、静默吞错或旧 Handler 绕过 RuntimeBridge。

## v0.2.0 特别覆盖

- OpenAI Fast/free Fast、service tier 和 reasoning effort：Runtime 只透传请求/响应事实；平台策略、官方成本、平台售价和最终扣费仍由 XCode ProductCore 决定。
- Kimi 原生 Responses、Fable 5.1、定时自动化无 call ID、WebSocket terminal event、404/429 分类：经现有 executor/adapter 接入，不恢复官方 Group/Channel。
- 官方模型广场和 Channel 分时价格：不覆盖 XCode 已实现的“官方成本价 + 平台售卖价”模型目录，也不改变用户 UI。
- 官方插件系统：`pluginapi`、插件 manager/repository、上传包、gRPC bridge、migration 229/230 均属于新的产品与公共 contract，当前明确排除。
- OAuth outbound transport plugin：只把提交作为 Runtime 风险项审阅；在插件产品未获单独批准前，不引入外部二进制执行路径。

## 依赖、配置和发布门禁

- 官方 `backend/go.mod` 把 Go 从 `1.26.6` 提升到 `1.27.0`，新增 HashiCorp plugin、gRPC/protobuf 等依赖，并升级 OpenTelemetry；本轮不直接同步依赖。
- `frontend/package.json`、`pnpm-lock.yaml`、Dockerfile、compose、根配置和 `.github/workflows/*` 均不进入 Runtime 批次。
- 若某个已确认 Runtime 功能确实需要新依赖，必须先证明最小依赖集合并重新取得依赖范围确认。

## 验证门禁

- RuntimeBridge contract、ApplicationGateway、gateway terminal/usage、adapter purity 与 migration namespace 测试必须通过。
- OpenAI/Codex P0 必须覆盖 Chat/Responses/Messages、SSE/WS、tool replay、Fast/service tier、404/429、定时自动化 bootstrap 和失败不扣费。
- 每个 Provider 分组必须证明未配置平台/账号时不可调度，且账号冷却、额度和错误不会串池。
