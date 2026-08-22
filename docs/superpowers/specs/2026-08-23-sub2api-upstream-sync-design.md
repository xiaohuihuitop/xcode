# Sub2API Upstream Sync Design

- 状态：已确认
- 日期：2026-08-23

## 目标

在不破坏 XCode ProductCore、RuntimeBridge、平台模型价格、API Key 授权、套餐计费和生产数据库的前提下，建立可重复的 Sub2API 官方更新审查与选择性移植流程。

## 当前基线

- XCode 发布分支是 `main`，当前发布版本为 `v1.0.7`。
- Sub2API 仍是唯一 Runtime 实现，但 XCode 通过 `pkg/runtimebridge/v1`、本地 RuntimeBridge 和 `internal/runtime/sub2api` Driver 约束 Runtime 边界。
- 官方更新不能整体合并到 XCode；官方补丁必须先判断其属于 Runtime、适配器、ProductCore、前端或部署层。
- 生产数据库和 Redis 只允许通过经过审查的应用升级保持不变；任何 schema 变化必须单独审查迁移和数据兼容性。

## 方案

1. 在独立 worktree 中配置官方 `upstream` 远程，仅更新审查分支的远程引用。
2. 以官方 `v0.1.179` 到 `main` 的提交作为候选池，优先筛选 GPT/Codex、Responses、调度、重试和 HTTP Bridge 修复。
3. Grok、Gemini、Antigravity、DeepSeek、无关 UI 和产品功能暂不移植。
4. 对候选提交先生成文件范围和冲突报告，再将逻辑移植到 XCode RuntimeBridge 端口或 Sub2API Driver；不直接覆盖 ProductCore 或数据库代码。
5. 每个移植单元配套回归测试，验证 Chat Completions、Responses、账号失败切换、终态 usage 和计费归属。
6. 本地验证通过后，才允许合并到 XCode `main`、发布离线包和升级香港应用容器。

## 明确边界

- 不执行 `merge upstream/main`、`reset --hard upstream/main` 或整体替换 XCode 文件树。
- 不修改生产数据库、Redis、服务器配置或现有未提交用户文件。
- 不把官方 Runtime 内部类型泄漏到 ProductCore 或公开 Handler。
- 不为了兼容补丁新增静默 fallback；失败语义必须保持可诊断。

## 验收

- 官方远程引用、候选提交和排除理由可追溯。
- RuntimeBridge 契约、后端单测、前端门禁和生产构建通过。
- 真实 GPT Chat/Responses 请求成功，usage 的平台、账号、订阅和扣费来源正确。
- 数据库 schema 与业务数据在发布前后无未审查变化。
