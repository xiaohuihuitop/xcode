# Sub2API v0.2.0 官方同步审计

本目录固定 XCode 从官方正式版 `v0.1.179` 同步到 `v0.2.0` 的可审计输入。P0 F001 OpenAI/Codex Runtime 与 F006 共享重试、冷却、安全审计和 reasoning 一致性已完成本地同步与 Adapter 移植；F002-F005 仍未实施。`v1.0.16` 发布门禁发现管理员额度重置后未即时刷新 scheduler 进程内快照，修复提交 `7f623c5` 已通过 GitHub 完整 CI，`v1.0.16` 不部署，后续版本从修复提交发布。

## 不可变身份

- 仓库：`Wei-Shaw/sub2api`
- 范围：`v0.1.179...v0.2.0`
- 目标 commit：`aa236488351eb71e120fc2b6fb32e36b0374c918`
- 注释 Tag 对象：`dd07c4d8d484878e617c945cc8bacc304a5a6560`
- 官方归档 SHA-256：`24a60d8cccb8b62ba184e5f8c4c636907af14e997be6a166ccd05e26d620bf9d`
- Release：正式版，发布时间 `2026-09-02T03:24:41Z`
- 提交数：481
- 官方 `VERSION`：`0.1.185`；它与 Tag 不一致，身份始终以 Tag、完整 commit 和归档哈希为准。

`git ls-remote` 本轮因 GitHub 网络超时未完成；Tag 身份随后由官方 Release API、canonical repository API、注释 Tag 对象和归档哈希交叉固定，没有使用移动的 `upstream/main`。

## 规模与所有权

- 最终 rebaseline 后全树清单 4,194 个路径；人工分类与完整计划均为 4,194 条。
- GitHub compare API 返回路径达到 300 个上限，因此不能把 300 当作完整变更文件数。
- 提交分类已清零 `needs_review`；Runtime 提交 208 个，功能矩阵 F001-F006 全量覆盖。
- 完整同步计划保持 `approved=true` 为 0；rebaseline 后有 16 个尚未实施的 `direct_sync` candidate。

## F001 本地同步结果

- 固定身份未漂移：目标仍为 `v0.2.0@aa236488351eb71e120fc2b6fb32e36b0374c918`，归档 SHA-256 仍为 `24a60d8cccb8b62ba184e5f8c4c636907af14e997be6a166ccd05e26d620bf9d`。
- 已同步 automation/delegation bootstrap、WebSocket 客户端关闭归因、client-tool/item ID、reasoning alias/cache、非法参数过滤、`created_at` 和 `service_tier`。
- 活动生产路径使用 `backend/internal/pkg/apicompat`；Official Runtime 隔离区作为已验证参考实现，两侧相关包均已通过回归。
- rebaseline 前机器清单与 apply 证据保存在 `revisions/001/`。根目录旧 F001 投影和 target baseline 不得复用。
- 完整 backend unit 测试、server 构建、同步工具单测、两套 validator 和差异检查均通过。
- 真实 Provider 验收为 NOT RUN；本轮不包含凭据、生产请求、计费核对、发布或部署。

## F006 本地同步结果

- F006-A：统一同账号 retry limit、deadline、delay 和请求级瞬态语义；请求级瞬态耗尽后可切号但不封禁账号。管理员额度重置以单条 SQL 同时清配额与账号级 rate-limit cooldown，保留 overload、临时禁用和模型级 cooldown。
- F006-B：`prompt_guard.config_loaded` 仅在首次加载、配置版本变化、全局风控变化或加载错误恢复时记录；Composite 路由、Ops 平台归属和 IPv6 使用 XCode 既有等价实现。
- F006-C：Chat/Responses fallback 按 mapped、original、body model 的顺序提取 reasoning effort；指定模型族保留原生 `max`，其他模型继续映射为 `xhigh`。
- 分组提交：`5ec2160`、`aba1dd8`、`3d8fb63`。审计 revision 005-010 保留设计、各组、最终文档与验证状态 rebaseline 前证据。
- Release 门禁修复：GitHub PostgreSQL 集成测试在 `account_repo_integration_test.go:869` 观测到数据库重置成功但 scheduler 缓存没有刷新；修复复用现有 `syncSchedulerAccountSnapshot`，Revision 011 保存修复前清单与失败证据。
- 无 migration、Ent schema、依赖、前端、根配置、CI、Release 或部署变化；真实 Provider 验收为 NOT RUN。

## 建议功能分组

- P0 F001：已完成本地同步与验证。
- P0 F006：已完成本地同步、验证和分组提交。
- P1 F004：Kimi/DeepSeek/GLM/Ollama，重点是 Kimi 原生 Responses、403 recoverable 和 GLM team quota。
- P1 F005：Anthropic/Claude/Fable/Bedrock，重点是 fallback beta、工具参数、归因头与 Fable 5.1。
- P2 F002：Grok 4.6、Realtime、媒体、工具和容量恢复。
- P2 F003：Gemini/Antigravity endpoint、工具 schema、模型目录和 token 限制。

每次只能确认并实施一个组。后续 F002-F005 仍需分别审查、确认和验证，不随已完成的 P0 自动授权。

## 必须重新确认的高风险面

- Schema：官方新增 migration `229-233`，含插件表/二进制包、usage 字段、Group/Channel Fast、reasoning 与价格列。全部禁止直接执行。
- 公共 contract：官方新增 `backend/pkg/pluginapi/v1` protobuf/gRPC/manifest contract。当前明确排除插件产品。
- 依赖：官方 Go 版本升到 1.27.0，新增 HashiCorp plugin、gRPC/protobuf 并升级 OpenTelemetry。根依赖不直接同步。
- 根配置/CI/部署：Dockerfile、compose、GitHub workflows 与 gateway 环境变量均有变化。当前明确排除。
- 前端/ProductCore：官方模型广场、Group/Channel 定价、用户资产、usage 持久化和管理员功能不覆盖 XCode 现有产品实现。

## 文件说明

- 机器生成：`metadata.json`、`commits.csv`、`files.csv`、`sync-plan.json`。
- 人工审阅：`runtime-feature-matrix.md`、`database-impact.md`、`xcode-adapter-overrides.md`、`direct-sync-baselines.md`、本 README。

## 验证命令

```powershell
python -B -m unittest tools.test_sub2api_upstream_inventory tools.test_sub2api_upstream_sync
python -B tools/sub2api_upstream_inventory.py validate --inventory-dir docs/upstream/sub2api-v0.2.0 --current-root .
python -B tools/sub2api_upstream_sync.py validate --inventory-dir docs/upstream/sub2api-v0.2.0 --current-root .
```

重新生成机器文件会覆盖 CSV 分类和同步计划，因此必须重新完成人工审查；不得用 generate/plan 覆盖人工维护文档。
