# Revision 001: F001 Adapter Rebaseline

- 日期：2026-09-05
- 功能组：P0 F001 OpenAI/Codex Runtime
- 原因：完成 Official Runtime direct sync 后，将已验证协议行为移植到实际生产导入的 `backend/internal/pkg/apicompat` 与 OpenAI service 调用链。
- 保留内容：rebaseline 前的机器清单、完整同步计划、F001 三份投影、direct-sync 目标基线和 Adapter 审计。
- 验证证据：活动与 Official Runtime `apicompat` 测试通过；带 `unit` 标签的 service 定向测试通过。
- 后续规则：本目录中的 F001 apply plan 和目标基线只用于追溯，rebaseline 后不得再次 apply。
