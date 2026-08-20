# XCode 开发指南

## 开始前

按顺序阅读：

1. `docs/PROJECT_DEFINITION.md`
2. `docs/ARCHITECTURE.md`
3. `docs/HANDOFF.md`
4. `docs/memory/当前状态.md`
5. 与改动范围对应的 `docs/memory/决策/` 和测试

代码事实优先于正式文档，正式文档优先于项目记忆。发现冲突时修正文档和记忆，不在代码里保留双重解释。

## 修改边界

- 产品规则、资产授权、套餐、余额、计费和用量属于 ProductCore。
- 调度、OAuth、协议转换、重试和失败切换属于 GatewayRuntime/适配器。
- 新端点先定义纯 Runtime 请求、`HTTPExchange` 和 `UsageSink`，再接入公开 Handler。
- 平台是模型白名单和账号池的唯一入口；不要新增账号级模型规则或旧 Group 回退。
- 需要 schema 变更时，先确认迁移编号区间和数据兼容，再写迁移与回滚验证。

## 官方同步

```bash
git fetch upstream
git log --oneline --decorate main..upstream/main
git diff --stat main...upstream/main
```

只合并经过审查的官方提交。同步后重新检查平台/套餐/计费边界、适配器架构守卫和 UI 定制，不直接把 `upstream/main` 当作产品发布分支。

## 本地验证

后端在 `backend/` 执行：

```bash
make test-unit
make test-integration
go build ./cmd/server
```

前端在 `frontend/` 执行：

```bash
pnpm run test:run
pnpm run typecheck
pnpm run lint:check
pnpm run build
```

行为变更必须补回归测试；真实服务器验证至少覆盖一次 Chat Completions 和一次 Responses，并检查使用记录、延迟、Token、费用和扣费来源。

## 提交与发布

```bash
git diff --check
git add README.md docs
git commit -m "docs(project): 更新项目文档"
git push origin main
git tag -a v1.0.1 -m "release: XCode v1.0.1"
git push origin v1.0.1
```

Tag 必须指向 `origin/main` 的祖先。GitHub Actions 只生成离线包 `xcode_latest.tar` 和 `xcode_latest.tar.sha256`，不发布 GHCR。服务器加载：

```bash
docker load -i xcode_latest.tar
```

更新前先备份 compose、环境文件和数据库；只替换应用容器，保留 PostgreSQL、Redis 和应用数据卷。

## 安全规则

- API Key、OAuth 凭据、服务器密码、支付密钥只能通过环境变量或受保护的部署配置提供。
- 不把真实凭据写入源码、测试夹具、日志、Issue、文档或提交信息。
- 管理后台、登录和支付回调使用 HTTPS；备份需要异地保存并定期演练恢复。
- 不用静默 fallback 掩盖调度、计费或上游错误；错误必须保留可诊断的来源和终态。
