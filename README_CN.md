# XCode

XCode 是一个可自托管的 AI API 网关与订阅分发平台，统一管理平台账号池、API Key 授权、套餐、余额、计费和使用记录，并通过适配器连接上游运行时。

## 功能范围

- 平台统一维护模型白名单、端点能力和账号池。
- API Key 可同时授权多个平台、多个独立套餐和余额回退。
- 套餐优先扣费，使用订阅倍率快照；余额使用全局倍率，两者不叠加。
- 当前生产运行时覆盖 OpenAI Images、Chat Completions、Responses 和 Messages。
- GitHub Actions 生成可离线加载的 Docker 包，适用于没有镜像仓库依赖的服务器。

产品层属于 XCode 自有实现。Sub2API 是当前负责调度、OAuth 刷新、协议适配、重试、冷却和失败切换的运行时实现，边界见[架构文档](docs/ARCHITECTURE.md)。

## 快速部署

下载 Release 中的离线包后，为继承的 compose 文件建立镜像覆盖，再在已有部署目录执行：

```yaml
# docker-compose.xcode.yml
services:
  sub2api:
    image: xcode:latest
```

```bash
docker load -i xcode_latest.tar
docker compose -f deploy/docker-compose.yml -f docker-compose.xcode.yml up -d
```

首次部署前请从 `deploy/.env.example` 复制并配置环境文件。继承的基础 compose 仍使用上游镜像名，生产部署必须保留上面的 `xcode:latest` 覆盖。发布契约是本地镜像 `xcode:latest` 和离线包 `xcode_latest.tar`；本项目不发布 GHCR 镜像。

## 项目文档

- [项目定义](docs/PROJECT_DEFINITION.md)
- [架构说明](docs/ARCHITECTURE.md)
- [开发指南](docs/DEVELOPMENT_GUIDE.md)
- [开发路线](docs/ROADMAP.md)
- [部署与交接](docs/HANDOFF.md)
- [当前状态](docs/memory/当前状态.md)

## 本地开发

前端使用 Vue 3、TypeScript、Vite 和 Vitest；后端使用 Go、PostgreSQL 和 Redis。修改产品规则、Runtime 边界、迁移或发布流程前，必须先阅读开发指南。

```bash
cd backend
make test-unit
go build ./cmd/server
cd ../frontend
pnpm run test:run
pnpm run typecheck
pnpm run lint:check
```

## 官方来源与许可证

官方源通过 `upstream` 远端按需同步；XCode 的生产分支是 `main`，发布 Tag 从 `v1.0.0` 开始。仓库保留上游许可证和必要声明，详见 [LICENSE](LICENSE)。
