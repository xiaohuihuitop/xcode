# XCode 项目交接与仓库重建设计

- 状态：已确认
- 日期：2026-08-20
- 范围：新仓库文档、Git 历史重建和发布身份整理

## 背景

`xiaohuihuitop/xcode` 最初从旧的 Sub2API 定制仓库复制而来，因此保留了官方项目的完整提交历史、旧 Tag、旧项目首页和过程性文档。代码基线是已经验证过的 XCode 产品代码，但仓库身份仍然像一个 Fork，容易让贡献者统计、发布入口和后续开发方向产生误解。

## 目标

1. 让 `main` 成为 XCode 的唯一生产分支。
2. 用清晰的 README 和交接文档说明产品目标、架构边界、开发规则和发布流程。
3. 保留当前代码、许可证、上游来源说明和运行所需文件。
4. 重建 Git 历史，使远程仓库从一个新的根提交开始，并从 `v1.0.0` 重新建立发布线。
5. 保持应用行为、数据库 schema、内部二进制路径和现有服务器数据不变。

## 非目标

- 不重写 ProductCore、GatewayRuntime 或 Sub2API 运行时实现。
- 不删除 PostgreSQL、Redis、应用数据卷或服务器配置。
- 不发布 GHCR 镜像；发布产物仍是 `xcode_latest.tar`。
- 不把清理 Git 历史误认为数据库迁移或数据清理。

## 文档结构

- `docs/PROJECT_DEFINITION.md`：立项、范围和非目标。
- `docs/ARCHITECTURE.md`：ProductCore、GatewayRuntime、适配器和数据流。
- `docs/DEVELOPMENT_GUIDE.md`：官方同步、测试、提交、Tag 和安全规则。
- `docs/ROADMAP.md`：当前阶段、上线前风险和后续路线。
- `docs/HANDOFF.md`：部署、数据兼容、已完成定制和接手说明。
- `docs/memory/`：短小、可执行的项目记忆；不承担完整过程日志。

## 历史重建流程

1. 在当前工作树完成文档变更并通过静态检查。
2. 创建 orphan 分支，将当前工作树作为新的根提交。
3. 将新的根提交替换远程 `main`，使用显式强制推送。
4. 删除旧的 `v1.0.0`、`v1.0.1` Tag，再创建干净的 XCode `v1.0.0`。
5. 等待 GitHub Actions 的 source、frontend、backend、lint 和 release job 全部通过。
6. 验证 Release 同时包含 `xcode_latest.tar` 和 `xcode_latest.tar.sha256`。

历史重写只影响 Git 提交、Tag 和贡献者统计，不影响工作树代码、Docker 镜像、数据库或服务器数据。旧提交在远程不再作为发布依据，因此需要把旧仓库地址和旧 Tag 视为历史参考，而不是更新入口。

## 发布身份

- 产品仓库：`git@github.com:xiaohuihuitop/xcode.git`
- 生产分支：`main`
- 官方同步源：`upstream/main`
- Tag：语义版本格式，例如 `v1.0.0`、`v1.0.1`
- 本地镜像：`xcode:latest`
- 离线包：`xcode_latest.tar`

内部 `/app/sub2api` 二进制名、数据库表名、compose 服务名和 API 路径属于兼容边界，本次不因仓库改名而修改。

## 验收标准

- `git log --oneline` 只显示新的 XCode 根提交链。
- `git ls-remote --tags origin` 不再包含旧发布线，且存在新的 `v1.0.0`。
- README 不再出现旧项目赞助、旧项目 Release 或误导性的 Sub2API 产品介绍。
- 文档中不出现 API Key、密码、支付密钥或服务器登录信息。
- `git diff --check` 通过；仓库既有应用门禁在发布前按开发指南执行。
