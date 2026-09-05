# Revision 009: F006 Final Documentation Rebaseline

- 日期：2026-09-05
- 功能组：P0 F006 最终审计与项目记忆
- 原因：F006-A/B/C 均已本地提交，最终审计文档和项目记忆更新会改变当前文件树 hash，需要在完整验证前刷新机器清单。
- 保留内容：最终文档回填前的机器清单、完整同步计划和人工审计文档。
- 后续规则：根目录清单使用相同冻结 snapshot 重新生成并恢复人工分类；本 revision 只用于追溯，不得复用旧 apply 计划或 target baseline。
