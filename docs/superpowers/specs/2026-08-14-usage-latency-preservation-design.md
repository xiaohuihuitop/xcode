# 使用记录延迟保真设计

## 背景

OpenAI Runtime 已在上游转发结果中计算总耗时和首字延迟，并通过 `gatewayruntime.UsageFacts` 传递。`Sub2APIProductUsageFinalizer` 将这些事实重建为原有计费服务输入时漏掉了两个字段，导致适配器迁移后的成功记录写入 `duration_ms = 0`、`first_token_ms = NULL`。

## 设计

- `gatewayruntime.UsageFacts` 继续作为 Runtime 到 ProductCore 的延迟事实来源，不在前端或异步计费阶段重新计时。
- OpenAI 路径把 `DurationMilliseconds` 转换为 `OpenAIForwardResult.Duration`，把正数 `FirstTokenMilliseconds` 转换为 `OpenAIForwardResult.FirstTokenMs`。
- 通用 Gateway 路径对 `ForwardResult` 做同样映射，避免同一适配器缺陷影响其他已迁移端点。
- `FirstTokenMilliseconds <= 0` 表示没有首字事实，保持 `nil`；总耗时允许零值，以兼容非网络类同步端点。
- 不修改数据库 schema、用量 API 或前端显示。

## 验证

- 单元测试先复现映射缺失，再验证 OpenAI 与通用结果均保留延迟。
- 执行 Service、Handler 和 Runtime 相关回归及后端构建。
- 部署到新加坡测试服务器后，验证 Chat Completions 与 Responses 的流式和非流式请求；数据库成功记录必须满足 `duration_ms > 0`，流式记录必须满足 `first_token_ms > 0`。
