# My2 预发布 Docker 通道设计

## 状态

- 已确认
- 日期：2026-08-04

## 目标

让 `my2.0` 的订阅与计费重构能够以独立的 Docker 测试包交付，而不覆盖正式 `my` 渠道的镜像、离线包或版本文件。

## 方案

- 使用独立 Git Tag 约定 `my2-v<版本>`，例如 `my2-v0.2.0`。
- 新增独立工作流 `.github/workflows/my2-release.yml`；正式 `release.yml` 保持不变，因此 `my2` Tag 不会触发正式发布。
- 工作流从 Tag 指向的精确提交构建前端、嵌入式 Go 服务和 `linux/amd64` Docker 镜像。
- 测试镜像使用同一 GHCR 仓库中的隔离标签：
  - `ghcr.io/xiaohuihuitop/sub2api:my2-latest`
  - `ghcr.io/xiaohuihuitop/sub2api:my2-<版本>`
- 离线包命名为 `sub2api_my2_latest.tar`，内部只包含 `sub2api:my2-latest` 与 `sub2api:my2-<版本>`，避免 `docker load` 后意外替换正式 `sub2api:latest`。
- 每个 `my2-v*` Tag 创建 GitHub prerelease 并附加离线包和 SHA256 校验文件。

## 隔离边界

- 不发布 `:latest`、`:major.minor`、`:major` 标签。
- 不使用正式 Docker Hub 发布路径。
- 不执行正式工作流中的 `sync-version-file`，绝不回写 GitHub 默认分支 `main`。
- 测试包仅构建 `linux/amd64`，与现有服务器离线部署架构一致。

## 验证

- 用 `actionlint` 静态校验 GitHub Actions 语法和表达式。
- 用 YAML 解析器检查工作流结构。
- 检查正式工作流未被修改，且 `my2-release.yml` 只匹配 `my2-v*`。
- 推送首个 `my2-v0.2.0` Tag 后，以 GitHub Actions 实际构建结果作为端到端验证。
