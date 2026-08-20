# OpenAI Images Runtime 纯化设计

## 目标

将 OpenAI 图片请求从 Sub2API 适配器内部的 Gin 兼容桥迁移到纯 `context.Context + gatewayruntime.HTTPExchange` 边界，保持现有外部 API、账号调度、OAuth 刷新、协议转换、失败切换、响应格式和计费数据不变。

## 范围

本阶段只处理 OpenAI Images：

- API Key 与 OAuth 账号；
- `/v1/images/generations` 与 `/v1/images/edits`；
- JSON 与 multipart 请求；
- 非流式响应与 SSE 流式响应；
- upstream 错误分类、账号冷却、同账号重试和跨账号 failover；
- 图片数量、尺寸、usage、首字节延迟和响应头记录。

Gemini、Grok、Anthropic 以及 OpenAI Chat/Responses/Messages 不在本阶段改动。Chat/Responses/Messages 将沿用同一边界在后续阶段迁移。

## 设计

`OpenAIGatewayService` 新增以 `gatewayruntime.HTTPExchange` 为输出边界的 Images Forward 方法。请求构造从 Gin 请求头读取改为接收 `http.Header`；响应写入、SSE flush、keepalive、已写入状态和错误响应通过 `HTTPExchange` 完成。现有 Gin 方法保留为未迁移旧入口的兼容实现，但 `ForwardImagesRuntime` 只能调用纯 exchange 方法，生产 runtime 不再创建 `gin.Context`。

API Key 和 OAuth 继续使用现有的账号凭据、代理、模型映射、请求重写和 upstream client。每一个 upstream attempt 在发送前清理并设置实际端点标记；失败切换不得继承上一次 attempt 的端点或响应状态。错误路径必须保持“已向客户端写出响应则不可重复写出”的语义。

## 验收

1. 纯 service 单测能够通过 API Key JSON、API Key multipart、OAuth 非流式和 OAuth SSE 最小成功用例。
2. 至少覆盖一个可重试 upstream 错误，确认返回 `UpstreamFailoverError` 且不会提前记成功用量。
3. runtime handler 回归确认 `ForwardImagesRuntime` 不再调用 `withRuntimeGinContext` 或旧 Gin Forward。
4. `go test -tags=unit ./internal/service -run 'OpenAIImages|GatewayRuntimeExchange'`、对应 handler/architecture 定向测试和 `git diff --check` 通过。
5. 不新增第二套 Runtime，不改变数据库 schema、公共 API、账号调度、OAuth、计费或其他渠道行为。
