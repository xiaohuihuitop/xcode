# My2 Release Quality Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 阻止未通过完整测试或不属于 `my2.0` 的 Tag 发布 My2 Docker 镜像和离线包。

**Architecture:** 在 My2 Release 内建立来源、后端、前端和 Go Lint 四个门禁作业，发布作业通过 `needs` 等待全部成功。用 Go 架构测试解析工作流 YAML，防止后续同步官方时意外移除门禁或提前更新 `my2-latest`。

**Tech Stack:** GitHub Actions、YAML、Go `testing`、`gopkg.in/yaml.v3`、Make、pnpm、Docker Buildx

---

### Task 1：固定发布工作流契约

**Files:**
- Create: `backend/internal/architecture/my2_release_gate_test.go`
- Verify: `.github/workflows/my2-release.yml`

- [x] 编写 YAML 解析测试，断言 `source-gate`、三个质量门禁和 `release.needs` 存在。
- [x] 断言来源校验包含 `origin/my2.0` 与 `git merge-base --is-ancestor`。
- [x] 断言后端、前端、Go Lint 命令完整，并固定版本镜像、离线包校验和 latest 最后发布的顺序。
- [x] 运行测试验证旧工作流按预期失败，再在新工作流上验证通过。

### Task 2：实现完整门禁

**Files:**
- Modify: `.github/workflows/my2-release.yml`

- [x] 新增 `source-gate`，输出 `tag_name`、`version`、`image` 和 `commit_sha`。
- [x] 新增并行的 `backend-gate`、`frontend-gate`、`lint-gate`，全部从已验证 Tag checkout。
- [x] 将 `release` 设置为等待四个门禁，并只使用 `source-gate` 输出的版本元数据。
- [x] 将版本镜像推送、离线包检查、GitHub Release 和 latest 更新拆成不可越过的顺序。

### Task 3：验证与记录

**Files:**
- Modify: `docs/memory/当前状态.md`
- Create: `docs/memory/决策/2026-08-11-My2发布必须通过完整质量门禁.md`

- [x] 运行架构测试，已通过。
- [x] 运行 `go test -tags=unit ./internal/... -count=1 -timeout=35m` 和修正后的 `make test-integration`，均通过；原始 `go test -tags=integration ./...` 的既有编译失败已定位为旧夹具/生成目录误纳入。
- [x] 在 `frontend` 运行 `pnpm run test:run`、`pnpm run typecheck`、`pnpm run lint:check`，均通过。
- [x] 运行前后端 build，均通过；本机未安装 `golangci-lint`，工作流固定使用 `golangci/golangci-lint-action@v9` / `v2.9`。
- [x] 运行 `git diff --check`，审查最终工作流 diff，并更新项目记忆。
