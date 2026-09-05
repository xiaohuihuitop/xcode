# Sub2API v0.2.0 数据库影响

## 结论

- 官方目标树包含 migration `217-233`；相对上一基线新增 `229-233` 共 9 个文件。
- XCode 不直接执行任何官方 SQL，也不复用官方编号。Runtime 新 migration 只允许 `8000-8999`，ProductCore 只允许 `9001-9999`。
- 本轮 Runtime 功能默认零 schema 变更。账号配额、冷却和 Provider 状态优先复用 `accounts.extra`，用户定价与用量字段必须单独进行 ProductCore contract 审查。
- 当前清单 validator 的固定检查范围仍为 `217-228`；`229-233` 已在下方单独人工审阅，不因工具范围较旧而遗漏风险。

## Validator 强制映射

| 官方迁移 | 官方目的 | 处理方式 | XCode 结论 |
| --- | --- | --- | --- |
| `217_group_video_model_prices.sql` | Group 视频价格 | productcore_mapping | 不恢复 Group；Runtime 只上报媒体事实 |
| `218_group_audio_voice_pricing.sql` | Group 音频/Voice 价格 | productcore_mapping | 不恢复 Group；价格归 ProductCore |
| `219_group_search_price_per_1k.sql` | Group Search 价格 | productcore_mapping | SearchCount 仅作为 usage fact |
| `220_clear_non_grok_video_generation_config.sql` | 清理 Group 视频配置 | not_runtime | XCode 已删除对应 Group/Channel 模型 |
| `221_group_model_pricing.sql` | Group 模型及长上下文价格 | xcode_equivalent | 保留 XCode 模型价格目录与 override |
| `222_group_usage_daily_rollups.sql` | Group 日用量汇总 | not_runtime | 不恢复 Group rollup 表和触发器 |
| `223_group_usage_rollup_timezone.sql` | Group 汇总时区 | not_runtime | 依赖 222，XCode 不采用 |
| `224_user_platform_quotas_add_cn_providers.sql` | 用户平台额度约束 | not_runtime | 用户资产归套餐/余额/API Key；AI 账号额度归 Runtime |
| `225_backfill_codex_fingerprint_seed.sql` | Codex 指纹 seed 回填 | adapter_port | 继续用 `accounts.extra` 惰性、幂等初始化，不全表更新 |
| `225_channel_model_time_pricing.sql` | Channel 分时价格 | productcore_mapping | 不恢复 Channel；现有价格规则保持唯一来源 |
| `226_add_usage_log_effective_model_indexes_notx.sql` | usage effective model 索引 | xcode_equivalent | 仅有生产规模查询计划证据时另提 `9001+` |
| `226_channel_monitor_quota_mode.sql` | Channel Monitor 配额历史 | adapter_port | 映射到现有 AI 账号额度快照，不建 Channel Monitor 表 |
| `227_composite_routes_add_cn_providers.sql` | Composite 国产 Provider 路由 | adapter_port | 映射到平台规则/Runtime settings，不恢复官方外键表 |
| `228_channel_pricing_multipliers.sql` | Fast/Flex 与区间倍率 | productcore_mapping | service tier 只传事实，不采用官方默认倍率 |

## v0.2.0 新增迁移人工审阅

| 新增迁移 | 官方目的 | 处理方式（人工） | 风险与决定 |
| --- | --- | --- | --- |
| `229_plugins.sql` | 插件安装、绑定和 rollout 表 | not_runtime | 新产品域、外部二进制执行与授权模型；当前明确排除 |
| `230_plugin_artifacts.sql` | 数据库存储插件二进制包 | not_runtime | BYTEA 大对象、签名和多实例分发风险；当前明确排除 |
| `231_add_usage_log_native_compaction_v2.sql` | 记录原生 compaction v2 | productcore_mapping | Runtime 可上报事实；是否持久化须另审 usage contract |
| `231_add_usage_log_requested_reasoning_effort.sql` | 保存映射前 reasoning effort | productcore_mapping | XCode 已有 reasoning facts；字段变更不随 Runtime 自动引入 |
| `231_user_restrict_public_groups.sql` | 限制用户可绑定公共 Group | not_runtime | XCode 没有官方 Group 授权模型 |
| `232_channel_cache_write_1h_pricing.sql` | Channel 1h cache write 价格 | productcore_mapping | 只能映射到 XCode 官方成本/平台售价目录，不能建第二价格源 |
| `232_group_force_openai_fast.sql` | Group 强制 Fast | productcore_mapping | Runtime 可接收平台策略事实；不恢复 Group 列 |
| `232_group_reasoning_effort_over_limit.sql` | Group reasoning 超限拒绝/降级 | productcore_mapping | 映射为平台模型策略前需公共 contract 审查 |
| `233_group_free_openai_fast.sql` | Group 免费 Fast、按 Standard 计费 | productcore_mapping | 属于平台销售策略，不能由 Runtime 或官方默认值决定 |

## 条件门槛

只有出现可复现的查询计划、唯一性约束或持久化需求证据，才允许另提 XCode migration。任何提案都必须包含存量数据对账、锁/索引影响、重复执行、失败可见性和恢复验证；不存在直接执行官方 migration 的路径。
