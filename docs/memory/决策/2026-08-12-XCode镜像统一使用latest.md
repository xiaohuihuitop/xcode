# XCode 镜像统一使用 latest

- 状态：已确认
- 日期：2026-08-12
- 结论：XCode 的 Docker 交付统一使用 `xcode:latest`；离线包为 `xcode_latest.tar`，加载后只产生本地镜像 `xcode:latest`。
- 原因：部署端固定使用一个简洁镜像名，不再依赖 `sub2api`、`my2` 或版本标签。
- 影响：Git Tag 使用 `v<版本>` 触发 GitHub Actions、标识源码和区分 Release；Docker 镜像不发布版本标签或远程镜像，回滚时从目标 GitHub Release 下载对应的历史 `xcode_latest.tar`。工作流必须先完成质量门禁、离线包校验和 Release 发布。
- 相关文件：`.github/workflows/my2-release.yml`、`backend/internal/architecture/my2_release_gate_test.go`
