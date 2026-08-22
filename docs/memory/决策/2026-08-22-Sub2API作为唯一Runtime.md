# Sub2API 作为唯一 Runtime

- 状态：已确认
- 日期：2026-08-22
- 结论：XCode 继续使用 Sub2API 作为唯一生产 Runtime，不接入 CAP，不做 Runtime 开关或双 Runtime 并行。
- 原因：当前 GPT 范围内 Sub2API 已具备账号选择、粘性调度、负载均衡、并发控制、冷却、重试、失败切换、OAuth 刷新和账号可用性管理；继续复用可避免重复实现和账号状态同步。
- 影响：ProductCore 继续管理用户账号、API Key、平台、套餐、余额、计费和用户 usage；Sub2API Runtime 管理 AI 账号、凭据、quota 观察、冷却、调度和上游执行；保留 RuntimeBridge 作为可替换边界，但当前不实施 CAP 替换。
- 数据库：不新增 CAP 账号表、CAP 配置字段或 migration，继续复用现有 `accounts`、`accounts.extra`、usage、套餐和订阅数据。
- 后续：优先完成 GPT 生产可靠性、订单套餐快照、退款幂等、数据库恢复演练和调度可观测性。
- 相关文件：`docs/ARCHITECTURE.md`、`docs/ROADMAP.md`、`backend/internal/handler/sub2api_runtime_composition.go`、`backend/internal/runtime/sub2api/`。
