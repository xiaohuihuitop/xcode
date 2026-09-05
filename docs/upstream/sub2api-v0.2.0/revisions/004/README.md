# Revision 004: F001 Fixture Whitespace Rebaseline

- 日期：2026-09-05
- 功能组：P0 F001 OpenAI/Codex Runtime
- 原因：提交后 `git show --check` 发现三个活动包 JSON fixture 含多余尾部空白行，需要在进入 F006 前修正并刷新 current hash。
- 保留内容：fixture 空白修正前的机器清单、完整同步计划和人工审计文档。
- 后续规则：根目录最终清单以同一冻结 snapshot 再生成并重审；本 revision 仅用于追溯，不得复用任何旧 apply 计划。
