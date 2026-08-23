# Sub2API v0.1.179 数据库影响

## 结论

- 官方 `v0.1.179` 在本次范围内新增 14 个 migration 文件，编号覆盖 `217-228`，其中 `225` 和 `226` 各有两个不同文件。
- XCode 不执行这些官方 SQL，也不复用其编号。已发布 migration `192-200` 和 `9000` 保持 checksum 不变。
- Runtime 同步默认不新增数据库 migration。AI 账号运行状态优先复用 `accounts.extra`，Runtime 全局配置优先复用现有 `settings`，价格继续由 ProductCore 的模型目录和价格解析器管理。
- 只有阶段六拿到查询计划、唯一性或完整性证据后，才允许提出一个小型增量：Runtime 使用 `8000-8999`，ProductCore 使用 `9001-9999`。这不是当前已批准变更。

## 逐项映射

| 官方迁移 | 官方目的 | Runtime 是否必需 | XCode 等价状态 | 处理方式 | 目标迁移 | 数据风险 | 验证 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `217_group_video_model_prices.sql` | Group 按模型族和分辨率配置视频每秒价格 | 否；Runtime 只需上报模型、分辨率、时长和终态 | `usage_logs` 已有 video 字段；价格归 `model_pricing`/资源目录 | productcore_mapping | 无；如未来要求管理员覆盖新计价单位，另提 `9001+` ProductCore 设计 | 直接复制会恢复 Group 并改变价格来源 | 视频 usage facts、模型价格解析、套餐/余额扣费回归 |
| `218_group_audio_voice_pricing.sql` | Group 配置 Realtime/TTS/STT 价格 | 否；Runtime 只上报音频事实 | 资源价格目录已包含 audio token/second 与 audio endpoint 信息 | productcore_mapping | 无；管理端覆盖需求另行评审 `9001+` | 直接复制会恢复 Group，且三种单位与当前 override contract 不同 | 音频 usage facts、价格单位和失败不扣费测试 |
| `219_group_search_price_per_1k.sql` | Group 配置 Search 每千次价格 | 否；Runtime 只上报 SearchCount | Search 价格由 XCode 模型/端点价格事实处理 | productcore_mapping | 无 | 直接复制会恢复 Group 并绕过套餐/余额价格 | SearchCount 去重、价格和扣费来源测试 |
| `220_clear_non_grok_video_generation_config.sql` | 清空非 Grok/Composite Group 的视频价格并建立备份表 | 否 | XCode 已删除 Group/Channel 配置，没有对应脏数据 | not_runtime | 无 | 执行会创建无所有权的备份表并触碰已删除模型 | 确认 migration 200 已删除 Group/Channel 表 |
| `221_group_model_pricing.sql` | Group 逐模型价格和长上下文开关 | 否 | `model_pricing_overrides`、价格资源目录、`usage_logs.long_context_billing_applied` 已提供 XCode 等价语义 | xcode_equivalent | 无 | 官方默认 `long_context_pricing_enabled=true` 不能覆盖 XCode 已冻结规则 | 价格 override、长上下文开关和倍率快照回归 |
| `222_group_usage_daily_rollups.sql` | Group 日用量汇总、水位和失效触发器 | 否 | XCode 报表按 Platform/ProductCore usage 维度查询，不再拥有 Group | not_runtime | 无 | 高写入频率触发器会增加 usage 热路径锁竞争 | 平台报表查询与 usage 计数对账 |
| `223_group_usage_rollup_timezone.sql` | 让 Group 日汇总跟随服务端时区 | 否 | 依赖 222 的 Group rollup，XCode 不采用 | not_runtime | 无 | 执行会依赖不存在的表/函数 | 时区报表现有测试，不创建官方 rollup 表 |
| `224_user_platform_quotas_add_cn_providers.sql` | 扩充用户平台额度约束到 Kimi/Zhipu/DeepSeek | 否；这是用户资产额度，不是 AI 账号额度 | XCode 用户资产由套餐/余额/API Key/平台授权管理；Runtime AI 账号额度存在 `accounts.extra` | not_runtime | 无 | 混用用户账号和 AI 账号额度会破坏所有权边界 | 新 Provider 未配置默认不可用；用户资产对账不变 |
| `225_backfill_codex_fingerprint_seed.sql` | 为已启用 OpenAI OAuth 账号回填 Codex 指纹 seed | 是，但无需结构化列 | `accounts.extra` 已稳定承载 Provider 特定状态；指纹模式为 opt-in | adapter_port | 无；启用时由 repository CAS/lazy init 幂等写入 `accounts.extra` | 全表 UPDATE 会在功能未启用时修改所有存量账号 | seed 格式、幂等、并发初始化、禁用账号不变和凭据脱敏 |
| `225_channel_model_time_pricing.sql` | Channel 模型分时价格 JSON | 否 | XCode `model_pricing_overrides.intervals` 管理 token 区间；时间定价是 ProductCore 产品需求 | productcore_mapping | 无；若将来明确需要分时价格，单独设计 `9001+` | 直接复制会恢复 Channel 价格表和第二套价格来源 | 当前价格/套餐倍率不变；未来需求单独验收 |
| `226_add_usage_log_effective_model_indexes_notx.sql` | 为 requested/upstream effective model 查询增加表达式索引 | 否；只影响报表查询性能 | XCode 已有 076/078 复合索引和 requested/upstream 字段，语义完整但索引形状不同 | xcode_equivalent | 默认无；只有 PostgreSQL `EXPLAIN (ANALYZE, BUFFERS)` 证明必要时提 `9001+` 非事务索引 | 新增大索引会占空间并增加写放大 | 生产规模副本上比较查询计划、索引大小和写入延迟 |
| `226_channel_monitor_quota_mode.sql` | Channel Monitor 关联账号、保存 quota history 并展示额度 | 是账号额度查询，不需要 Channel 产品模型 | 当前 AI 账号额度/余额快照可放 `accounts.extra`，公开设置可复用 `settings` | adapter_port | 无；默认不建 Channel Monitor/History 表 | 恢复 Channel 表会建立第二套 AI 账号观测和授权模型 | Provider 额度查询、账号删除、失效快照和凭据隔离 |
| `227_composite_routes_add_cn_providers.sql` | Composite route 允许目标为国产 Provider | 是 | XCode 已删除 `composite_model_routes`；平台规则和 Runtime settings 可承载路由配置，但完整性需阶段四/五验证 | adapter_port | 默认复用 `settings` JSON；仅在证明需要数据库查询/唯一约束时提 `8000-8999` | 直接恢复旧表会重新引入 Group 外键；JSON 配置则需写入校验和原子替换 | Composite Codex/CN 路由、无效目标拒绝、并发配置更新和默认关闭 |
| `228_channel_pricing_multipliers.sql` | Channel Fast/Flex 与时间区间 token/cache 倍率 | 否；Runtime 只上报 service tier/context facts | RuntimeBridge 和 usage 已有 `service_tier`；BillingService 已有 priority/flex 与长上下文价格逻辑 | productcore_mapping | 无 | 官方 multiplier 默认值不能覆盖 XCode 模型价格、套餐倍率或余额倍率 | Fast/Flex、service tier、缓存、长上下文和套餐/余额价格回归 |

## 条件变更门槛

阶段六之前数据库变更数量为零。阶段六只在以下证据存在时才允许提出 migration：

1. Composite 路由无法用现有 `settings` JSON 在原子写入、校验和读取性能上满足要求，才评审一个 `8000-8999` Runtime 配置迁移。
2. effective model 报表在生产规模副本上出现可重复的慢查询，且官方表达式索引显著改善计划，才评审一个 `9001+` ProductCore 非事务索引迁移。
3. 用户明确要求 Grok 视频/Voice/Search 的管理员自定义价格单位，且现有价格资源和 override contract 无法表达，才单独设计 `9001+` ProductCore 迁移；该需求不由 Runtime 同步自动触发。

任何条件迁移都必须包含存量行计数/金额对账、重复执行、失败可见性、索引/锁影响和恢复演练。不存在直接执行官方 SQL 的路径。
