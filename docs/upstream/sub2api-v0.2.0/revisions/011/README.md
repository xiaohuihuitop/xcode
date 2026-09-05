# Revision 011: F006 Release Gate Fix Rebaseline

- 日期：2026-09-05
- 功能组：P0 F006 管理员额度重置
- 原因：GitHub PostgreSQL 集成门禁发现额度重置后未即时刷新 scheduler 进程内账号快照；本 revision 保存修复前的机器清单和人工审阅文档。
- 失败证据：`TestAccountRepoSuite/TestResetQuotaUsedAndClearRateLimitCooldownPreservesOtherRuntimeState` 在 `account_repo_integration_test.go:869` 预期一条缓存刷新记录，实际为零。
- 修复范围：仅在成功更新并写入 scheduler outbox 后调用现有 `syncSchedulerAccountSnapshot`；无 schema、migration、依赖、前端、根配置、CI 或部署变化。
- 后续规则：根目录清单使用同一冻结 snapshot 重新生成并恢复人工分类；本 revision 只用于追溯，不得复用旧 apply 计划或 target baseline。
