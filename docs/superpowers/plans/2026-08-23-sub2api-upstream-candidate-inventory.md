# Sub2API Upstream Candidate Inventory

- 审查日期：2026-08-23
- XCode 审查分支：`codex/upstream-review-20260823`
- 官方来源：[Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api)
- 官方最新正式版：[v0.1.179](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.179)
- 官方 `main` 对比范围：[v0.1.179...main](https://github.com/Wei-Shaw/sub2api/compare/v0.1.179...main)
- Git 传输状态：HTTPS pack 下载在本机卡住，未建立 `upstream/main` ref；本清单使用 GitHub API 的 commit/file/patch 数据，未使用半截 pack。

## 当前基线

- XCode 发布提交：`7d0834f`（`v1.0.7`）。
- XCode 内嵌 Runtime 版本文件：`backend/cmd/server/VERSION` 为 `0.1.169`。
- XCode 自定义改动相对初始发布提交覆盖后端 RuntimeBridge、ProductCore、API Key、计费、模型广场和前端；生产入口由 `sub2APIOpenAIPort` 调用 `OpenAIGatewayService` 的 Runtime 方法。
- 当前审查工作树没有执行官方整体 merge、rebase 或 reset。

## 优先候选

| 提交 | 官方主题 | 直接命中 XCode 路径 | 处理结论 |
|---|---|---|---|
| `fd6cd474` | Responses/Chat fallback 拒绝畸形 tool arguments | `backend/internal/pkg/apicompat/*` 与 `backend/internal/service/openai_gateway_responses_chat_fallback.go`；XCode 的 `ForwardRuntime` 会经过该服务 | 已手工移植并完成本地回归 |
| `ffc01f9c` | HTTP Bridge replay 修复 | `openai_tool_continuation.go`、`openai_ws_http_bridge.go`；需确认当前 GPT Responses 是否使用该桥 | 第二批候选，先做路径验证 |
| `d45135d8` | Codex guardian/parent account affinity | `openai_account_scheduler.go`、`openai_gateway_scheduling.go`；XCode 通过端口调用调度服务，但 Driver 自己维护切号状态 | 第二批候选，需防止重复调度语义 |
| `b1e60ba4` | 池模式同账号错误重试 | `gateway`/调度失败策略；与 XCode Driver 的同账号重试上限可能重叠 | 第二批候选，需先做策略对照 |
| `6244090c` | file part 转 Responses `input_file` | `backend/internal/pkg/apicompat/*` | 仅在当前产品范围重新开放文件输入时移植 |
| `e45490a3` | OpenAI chat sticky hash 稳定化 | `openai_gateway_scheduling.go` | 账号粘连相关，需和平台调度 ID 规则对照 |

## 暂不移植

- Grok Realtime、Grok 媒体、Grok 429/容量重试及默认模型更新：当前 XCode 产品范围未开放 Grok。
- Gemini、Antigravity、Ollama Cloud：不属于当前 GPT 主线。
- DeepSeek Responses 原生工具修复：当前验收重点为 GPT；除非后续开启 DeepSeek 平台。
- 前端首页、CN 供应商额度、支付/运维 UI：会进入 XCode 产品层，不能随 Runtime 同步。
- 官方 `main` 的整体 108 提交：禁止整体合并。

## 数据库与依赖检查

- `v0.1.179...main` 的 273 个变更文件中没有 `backend/migrations/` 文件。
- 官方历史范围仍可能修改 `backend/ent/migrate/schema.go`、`backend/go.mod` 或工具链；移植单元必须逐项检查，未确认前不改变 XCode migration 或数据库。
- 当前首个候选 `fd6cd474` 只涉及协议转换、Responses fallback 和测试，不包含 migration、Ent schema、依赖或部署文件。

## 首个移植验收

1. 畸形历史 `function_call` 不再被转发给 Chat Completions 上游。
2. 流式工具参数在终态校验失败时不产生伪造的 completed tool item。
3. 真实 GPT Chat/Responses 的成功 usage、平台、账号、订阅和扣费来源保持不变。
4. 失败请求不写成功 usage，不产生额外扣费。

## 本地移植结果

- 已移植 `fd6cd474` 的核心行为，并吸收其后续 `fbc9ee6` 对空 `call_id` 输出和合法 `length` 工具调用的收窄修复。
- 生产代码仅变更协议桥和 Chat fallback；未修改 RuntimeBridge v1、调度、ProductCore、数据库 schema、migration、依赖或部署文件。
- 已验证：
  - `go test -tags=unit ./internal/pkg/apicompat`
  - `go test -tags=unit ./internal/runtime/sub2api ./internal/handler ./internal/service`
  - `go test ./pkg/runtimebridge/v1`
  - `go build ./cmd/server`
  - `git diff --check`
- 未执行香港部署、发布 Tag、在线真实请求和扣费验收；这些动作留待用户确认移植结果后单独执行。
