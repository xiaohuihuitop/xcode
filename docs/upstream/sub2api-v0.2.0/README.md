# Sub2API v0.2.0 官方同步审计

本目录固定 XCode 从官方正式版 `v0.1.179` 同步到 `v0.2.0` 的可审计输入。P0 F001 OpenAI/Codex Runtime 已完成本地同步、Adapter 移植和验证；其余 F002-F006 仍未实施。本地结果尚未提交、发布或部署。

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

- rebaseline 后全树清单 4,192 个路径：1,899 same、1,154 different、703 official-only、436 current-only。
- GitHub compare API 返回路径达到 300 个上限，因此不能把 300 当作完整变更文件数。
- 提交分类已清零 `needs_review`；Runtime 提交 208 个，功能矩阵 F001-F006 全量覆盖。
- 完整同步计划保持 `approved=true` 为 0；rebaseline 后有 19 个尚未实施的 `direct_sync` candidate。

## F001 本地同步结果

- 固定身份未漂移：目标仍为 `v0.2.0@aa236488351eb71e120fc2b6fb32e36b0374c918`，归档 SHA-256 仍为 `24a60d8cccb8b62ba184e5f8c4c636907af14e997be6a166ccd05e26d620bf9d`。
- 已同步 automation/delegation bootstrap、WebSocket 客户端关闭归因、client-tool/item ID、reasoning alias/cache、非法参数过滤、`created_at` 和 `service_tier`。
- 活动生产路径使用 `backend/internal/pkg/apicompat`；Official Runtime 隔离区作为已验证参考实现，两侧相关包均已通过回归。
- rebaseline 前机器清单与 apply 证据保存在 `revisions/001/`。根目录旧 F001 投影和 target baseline 不得复用。
- 完整 backend unit 测试、server 构建、同步工具单测、两套 validator 和差异检查均通过。
- 真实 Provider 验收为 NOT RUN；本轮不包含凭据、生产请求、计费核对、发布或部署。

## 建议功能分组

- P0 F001：OpenAI/Codex 协议、Responses/Chat/WS、工具、Fast/service tier、定时自动化与账号错误语义。
- P0 F006：共享重试/冷却、Composite、404/429、reasoning、监控与调度一致性。
- P1 F004：Kimi/DeepSeek/GLM/Ollama，重点是 Kimi 原生 Responses、403 recoverable 和 GLM team quota。
- P1 F005：Anthropic/Claude/Fable/Bedrock，重点是 fallback beta、工具参数、归因头与 Fable 5.1。
- P2 F002：Grok 4.6、Realtime、媒体、工具和容量恢复。
- P2 F003：Gemini/Antigravity endpoint、工具 schema、模型目录和 token 限制。

每次只能确认并实施一个组。建议先确认 P0 F001，再根据回归结果决定 F006；其他组不随 P0 自动授权。

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
