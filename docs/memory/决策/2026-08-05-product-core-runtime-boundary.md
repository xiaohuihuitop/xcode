# 产品核心与网关运行时隔离

- 状态：已确认
- 日期：2026-08-05
- 结论：My2 将把用户资产、平台授权、API Key、套餐、余额、倍率、用量归属和后台 UI 作为自有产品核心；Sub2API 的账号调度、OAuth 刷新、协议适配、上游失败切换和模型基础定价继续保留为运行时，不重写、不替换。

## 原因

- 用户的套餐、余额和平台授权模型已经与官方 `Group` 为中心的模型分歧，继续把这些规则散落在网关和旧分组逻辑中会提高每次同步官方的冲突与回归风险。
- 账号池调度、OAuth 刷新和协议兼容具有高复杂度，用户明确要求继续采用当前已验证的 Sub2API 实现，不自行维护替代方案。

## 影响

- 第一阶段只在同一仓库、同一进程内新增 `ProductCore`、`GatewayRuntime` 契约、Sub2API 适配器和上下文桥；不拆 Docker、不迁库、不改 UI。
- 新产品功能只能进入产品核心或其端口实现；账号调度、OAuth 和协议实现保持在受保护的运行时区域。
- 旧 Group 相关数据只可为历史记录和支付审计只读归档，不提供新请求兼容；平台授权、模型定价、资产选择和倍率都不能读取它。
- 每次同步 `upstream/main` 后，必须先运行产品核心与运行时桥的契约测试，再检查 V2 网关用量和平台权限回归。

## 首次发布验证

- `my2-v0.2.3` 使用该边界实现构建并成功发布为独立预发布镜像，未覆盖正式 `latest` 标签。
- 部署时只替换 `sub2api` 应用容器，保留 PostgreSQL、Redis 和现有数据卷；升级前必须备份 Compose、`.env`、当前镜像信息和 PostgreSQL 归档。
- 部署后必须检查三个容器健康状态、首页 HTTP 200、匿名入口 401，以及启动日志无 `panic`/`fatal`；真实 API Key 业务场景仍需人工验收。

## 相关文件

- `docs/superpowers/specs/2026-08-05-product-core-runtime-boundary-design.md`
- `docs/superpowers/plans/2026-08-05-product-core-runtime-boundary.md`
- `backend/internal/server/middleware/platform_asset_auth.go`
- `backend/internal/service/platform_asset_request.go`
- `backend/internal/service/api_key_asset_resolver.go`
- `backend/internal/service/gateway_scheduling.go`
