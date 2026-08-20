# My2 发布质量门禁设计

## 状态

- 状态：已确认
- 日期：2026-08-11
- 范围：`.github/workflows/my2-release.yml` 及其架构守卫

## 问题

推送 `my2-v*` Tag 时，通用 CI 与 My2 Release 是两个互不依赖的工作流。Release 当前只执行前端构建和 Go 编译，因此完整测试失败时仍可能推送 Docker 镜像并创建 GitHub Release。

## 决策

My2 Release 自身承担发布门禁，不等待另一个工作流的运行状态：

1. `source-gate` 验证 Tag 格式、解析实际提交，并确认该提交属于远程 `my2.0` 历史。
2. `backend-gate`、`frontend-gate`、`lint-gate` 在来源校验后并行运行。
3. `release` 通过 `needs` 等待全部门禁成功，任一失败都不得编译、推送或发布镜像。
4. 先推送不可变版本镜像 `my2-<version>`，生成离线包并执行 gzip、SHA-256、镜像架构检查。
5. GitHub Release 成功后才更新 `my2-latest`，避免中途失败污染测试通道的 latest。

## 门禁内容

- 后端：`make test-unit`、`make test-integration`。
- 前端：全量 Vitest、TypeScript typecheck、ESLint。
- Go：`golangci-lint`。
- 工作流结构：Go 架构测试解析 YAML，固定来源校验、门禁依赖、测试命令、离线包校验和 latest 发布顺序。

## 外部行为

- 应用代码、数据库、Docker 镜像命名和离线包名称不变。
- 测试失败时不产生新版本镜像、GitHub Release 或 latest 更新。
- 已发布的不可变版本镜像不覆盖；部署继续显式使用 `sub2api:my2-<version>`。

## 验收

- 架构测试能在旧工作流上失败，并在新工作流上通过。
- My2 Release YAML 可被 `gopkg.in/yaml.v3` 解析。
- 后端架构测试、全量单元/集成、前端全量测试/typecheck/lint 和 Go lint 均通过。
- `git diff --check` 无错误。
