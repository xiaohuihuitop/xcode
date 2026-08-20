# 统一 Responses 模式按请求类型路由

## 背景

此前网关曾根据 Platform 的端点能力为某些平台增加专属 Chat 直转分支。这样会绕过账号编辑页的“Responses API 支持”三态设置，平台名称变化还可能改变同一账号的协议行为。

## 决策

Responses 协议选择只由账号的 `openai_responses_mode` 和入站请求类型决定，任何平台不得添加专属协议改写。

- `auto`：`/v1/chat/completions` 直连上游 Chat；`/v1/responses` 优先使用上游 Responses，并继续使用能力探测和已有失败回退判断可用性。
- `force_responses`：Chat 请求转换到上游 Responses；Responses 请求使用上游 Responses。
- `force_chat_completions`：Chat 请求直连上游 Chat；Responses 请求转换到上游 Chat。

Platform 仍负责模型白名单、模型映射、平台授权和账号池选择；它不再根据平台代码覆盖账号的协议选择。OAuth 和 Grok 等必要的技术适配器继续保留，但不能成为某个平台的隐藏模式开关。

## 实施约束

- Chat 网关入口使用 `openai_compat.ShouldRouteChatCompletionsViaResponses`，只有 `force_responses` 才进入 Chat 到 Responses 转换。
- 使用记录和 Ops 错误日志必须优先读取服务层 `SetActualOpenAIUpstreamEndpoint` 保存的实际端点；只有转发层没有提供运行时端点时，才按入站请求和账号三态推导。
- 每一次上游尝试都必须主动覆盖运行时端点：Responses 记录完整路径（包含 `/compact` 等合法子路径），Chat 记录 `/v1/chat/completions`。此规则同时适用于 Responses 入站、Chat→Responses 兼容、Messages→Responses 和 Messages→Chat 兼容转发器；失败切换到下一账号时不得沿用前一次的端点。
- 同步官方代码时不得恢复平台专属分支；先保留 `ShouldRouteChatCompletionsViaResponses` helper 和三态回归测试，再处理冲突。

## 验证

- `go test -tags=unit ./internal/pkg/openai_compat -run 'TestShouldRouteChatCompletionsViaResponses|TestShouldUseResponsesAPI|TestResolveResponsesSupport' -count=1`
- `go test -tags=unit ./internal/handler -run 'TestGetUpstreamEndpointPrefersServiceRuntimeEndpoint|TestResolveOpenAIUpstreamEndpoint' -count=1`
- `go test -tags=unit ./internal/service -run 'TestForwardResponses.*ActualEndpoint|TestForwardResponsesCompactMarksFullActualEndpoint|TestForwardAsChatCompletions_APIKeyPropagatesPromptCacheKeyInResponsesBody|TestForwardAsAnthropic_(ForceChatCompletionsNonStreaming|ResponsesSupportedAccountStillUsesResponsesEndpoint)' -count=1`
- `go test -tags=unit ./internal/service -count=1 -timeout=15m`
