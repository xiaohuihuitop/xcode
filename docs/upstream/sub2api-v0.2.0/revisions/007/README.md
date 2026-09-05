# Revision 007: F006-B Rebaseline

- 日期：2026-09-05
- 功能组：P0 F006-B SecurityAudit 日志去重
- 原因：F006-B 完成 `prompt_guard.config_loaded` 变化信号去重后，当前文件树 hash 发生变化，需要在进入 F006-C 前刷新机器清单。
- 保留内容：F006-B rebaseline 前的机器清单、完整同步计划和人工审计文档。
- 后续规则：根目录清单使用相同冻结 snapshot 重新生成并恢复人工分类；本 revision 只用于追溯，不得复用旧 apply 计划或 target baseline。
