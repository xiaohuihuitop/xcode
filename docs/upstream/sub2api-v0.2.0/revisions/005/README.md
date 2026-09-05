# Revision 005: F006 Design And Plan Rebaseline

- 日期：2026-09-05
- 功能组：P0 F006 共享重试、冷却、监控与 reasoning 一致性
- 原因：新增已确认的 F006 设计规格和实施计划后，当前 XCode 文件树 hash 发生变化，需要在写入业务代码前刷新机器清单。
- 保留内容：F006 业务代码实施前的机器清单、完整同步计划和人工审计文档。
- 后续规则：根目录清单使用相同冻结 snapshot 重新生成并恢复人工分类；本 revision 只用于追溯，不得复用旧 apply 计划或 target baseline。
