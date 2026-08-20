# Subscription Billing Redesign Implementation Plan

> **已完成的历史计划，不再执行：** 本文件用于追溯 `my2-v0.2.0`。后续不得继续其中的
> Group 路由、BillingProfile 或双读迁移步骤；套餐实例规则继续保留，当前平台与定价设计
> 以 `docs/superpowers/specs/2026-08-06-platform-pool-account-adapter-design.md` 为准。

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. Every production behavior change starts with a focused failing test and ends with the named verification command.

**Goal:** 将套餐、余额定价与账号分组拆分：套餐保存金额额度、有效期和独立倍率；余额与套餐都可使用同一个账号分组；同组多个套餐实例按最早到期、再按创建时间依次使用，耗尽或过期后自动切换，最后才回退余额。

**Architecture:** `Group` 仅保留账号池、平台、接口能力、路由、调度与 RPM 等通道属性。新增一对一的 `BillingProfile` 保存余额计价与媒体价格配置。现有 `SubscriptionPlan` 继续作为支付/售卖套餐模板，补充额度和套餐倍率；每次购买、兑换或后台发放新建一个 `UserSubscription`，并把套餐条款快照写入该实例。API Key 的 `group_id`/`group_ids` 仍表示允许路由的分组；解析器在这些候选分组中选择可用订阅实例，随后才尝试余额。

**Tech Stack:** Go, Ent, PostgreSQL migrations, Redis cache, Vue 3, TypeScript, Vitest, Go unit/integration tests.

---

## Delivery Rules

- 保留现有 `group_id` 与 `group_ids` 兼容行为、多分组 API Key、端点能力过滤和 RPM 覆盖。
- 不新增平行的套餐表；扩展既有 `subscription_plans`，从而保持支付订单、商品展示和支付回调入口稳定。
- 套餐倍率完全覆盖余额倍率。账号 `rate_multiplier` 仅用于上游成本记录，不作为用户收费倍率。
- 不实现“某个用户的余额倍率”。`user_group_rate_multipliers` 中的 RPM override 保留；倍率字段和相关管理 API/UI 在本次切换中停止读取和写入。
- 第一个发布周期保留 `groups` 的旧收费字段作迁移回退，只写新模型；完成数据核对后再安排单独的删除迁移。
- 任何套餐选择、额度变化、发放、撤销或扣费都必须同步失效本地和跨实例订阅认证缓存。

## Task 1: Schema, Ent, and Backfill Migration

**Files:**
- Add: `backend/ent/schema/billing_profile.go`
- Modify: `backend/ent/schema/group.go`
- Modify: `backend/ent/schema/subscription_plan.go`
- Modify: `backend/ent/schema/user_subscription.go`
- Add: `backend/migrations/192_subscription_billing_redesign.sql`
- Regenerate: `backend/ent/**`, `backend/cmd/server/wire_gen.go`
- Add: `backend/migrations/192_subscription_billing_redesign_test.go`

- [ ] 写迁移断言：已有套餐不丢失支付字段；旧的活动订阅都得到计划关联和额度/倍率快照；同一用户同一分组可拥有两条活动订阅。
- [ ] 运行该测试，确认现有模型不能满足多活动套餐与快照要求。
- [ ] 新建 `billing_profiles`，以 `group_id` 建唯一约束，并复制余额倍率、峰时和媒体价格字段。
- [ ] 扩展 `subscription_plans`：`daily_limit_usd`、`weekly_limit_usd`、`monthly_limit_usd`、`rate_multiplier`；为每个旧订阅型分组补齐一个套餐模板，已有售卖套餐只补默认权益字段。
- [ ] 扩展 `user_subscriptions`：`subscription_plan_id`、`plan_name_snapshot`、三种额度快照及 `rate_multiplier_snapshot`；回填所有存量记录。
- [ ] 删除 `user_subscriptions_user_group_unique_active`，改为 `(user_id, group_id, status, expires_at, created_at)` 的候选查询索引；保留软删除和历史索引。
- [ ] 在 Ent 模型定义完整边、字段、索引，运行 `cd backend; go generate ./ent; go generate ./cmd/server`，只提交可复现的生成结果。
- [ ] 运行 `cd backend; go test -tags=integration ./migrations -run TestSubscriptionBillingRedesign -count=1`。

## Task 2: Subscription Snapshot Repository and Issuance Semantics

**Files:**
- Modify: `backend/internal/service/user_subscription.go`
- Modify: `backend/internal/service/user_subscription_port.go`
- Modify: `backend/internal/repository/user_subscription_repo.go`
- Modify: `backend/internal/service/subscription_service.go`
- Modify: `backend/internal/service/subscription_service_test.go`
- Modify: `backend/internal/repository/user_subscription_repo_integration_test.go`

- [ ] 先加入失败测试：同一用户/分组的两次套餐发放产生两条独立实例；查询按 `expires_at ASC, created_at ASC, id ASC` 返回；旧实例的额度、名称和倍率不会因模板编辑而改变。
- [ ] 运行服务与仓储定向测试，确认当前 `AssignOrExtendSubscription` 的唯一分组复用语义会失败。
- [ ] 为服务领域对象补齐计划 ID、套餐名称、额度和倍率快照；把限额检查改为读取订阅快照而非 `Group`。
- [ ] 增加 `ListActiveByUserIDAndGroupIDs` 和 `ListActiveByUserIDAndGroupID`，数据库查询固定使用候选排序。
- [ ] 新增 `AssignSubscriptionFromPlan`：读取模板后在事务中创建新实例并写入快照，不延长已有实例。后台手工“续期”保留为显式旧实例调整操作，不由支付/兑换隐式触发。
- [ ] 将缓存键从“一个用户+分组对应一条订阅”的实现升级为“候选订阅列表”，或者在列表变更时统一失效该用户+分组的认证缓存。
- [ ] 运行 `cd backend; go test -tags=unit ./internal/service -run 'TestAssign.*Plan|TestValidateAndCheckLimits|TestSelect.*Subscription' -count=1` 和 `go test -tags=integration ./internal/repository -run 'Test.*UserSubscription' -count=1`。

## Task 3: Payment, Redeem, and Admin Grant Cutover

**Files:**
- Modify: `backend/internal/service/payment_fulfillment.go`
- Modify: `backend/internal/service/payment_order.go`
- Modify: `backend/internal/service/redeem_service.go`
- Modify: `backend/internal/service/subscription_service.go`
- Modify: `backend/internal/service/payment_fulfillment_test.go`
- Modify: `backend/internal/service/redeem_service_test.go`
- Modify: `backend/internal/service/subscription_assign_idempotency_test.go`

- [ ] 先为支付回调写失败测试：订单首次完成只创建一个带 `subscription_plan_id` 的实例；重试同一订单不重复创建；连续购买同一套餐创建两条实例。
- [ ] 运行支付定向测试，确认目前订单按 `SubscriptionGroupID` 查找并续期旧实例。
- [ ] 让订单履约以 `PlanID` 作为权益来源，保留订单的 `SubscriptionGroupID`/`SubscriptionDays` 为历史响应兼容值。
- [ ] 使用订单 ID 作为幂等归属标识，改为按“该订单对应的实例”恢复，不再依据“用户+分组唯一订阅”判断是否已经发放。
- [ ] 兑换码和管理员发放若有明确套餐 ID，一律调用按套餐新建实例；仅旧 API 的 `group_id + days` 使用兼容发放器并写入迁移期默认快照。
- [ ] 运行 `cd backend; go test -tags=unit ./internal/service -run 'TestExecuteSubscriptionFulfillment|Test.*Redeem.*Subscription|Test.*Assign.*Subscription' -count=1`。

## Task 4: Resolver, Quota Cache, and Automatic Fallback

**Files:**
- Modify: `backend/internal/service/api_key_billing_group_resolver.go`
- Modify: `backend/internal/service/api_key_billing_group_resolver_test.go`
- Modify: `backend/internal/service/subscription_service.go`
- Modify: `backend/internal/service/billing_cache_service.go`
- Modify: `backend/internal/repository/billing_cache.go`
- Modify: `backend/internal/service/subscription_limit_boundary_test.go`

- [ ] 先写失败测试：同组两条可用套餐按最早到期选中；第一条达到日/周/月额度后请求自动选第二条；第二条也不可用时才选择允许的余额分组；不同 `chat/completions` 与 `responses` 能各自通过端点能力筛选。
- [ ] 运行解析器和缓存测试，确认当前代码只查询一个“订阅型分组”的活动订阅。
- [ ] 移除 `splitAPIKeyBillingGroups` 对 `Group.SubscriptionType` 的依赖：按 API Key 允许分组、平台、端点能力和排序遍历，在每个组内选择可用套餐实例。
- [ ] 让 `BillingCacheService.CheckBillingEligibility` 由 `subscription != nil` 判定套餐模式，额度从实例快照读取；余额分支继续检查余额和用户平台配额。
- [ ] 缓存记录带上订阅实例 ID 和额度快照；扣费更新与失效都以实际 `subscription_id` 为准，避免同组两条实例混用用量。
- [ ] 保留先前边界：达到额度时不再调度该实例；一次最终扣费可以正好消耗剩余额度；跨实例缓存失效必须同步完成后下个请求再选候选。
- [ ] 运行 `cd backend; go test -tags=unit ./internal/service -run 'TestResolveBillingGroup|Test.*Subscription.*Fallback|Test.*LimitBoundary|TestQueueUpdateSubscriptionUsage' -count=1`。

## Task 5: Billing Profile and Multiplier Context

**Files:**
- Add: `backend/internal/service/billing_profile.go`
- Add: `backend/internal/repository/billing_profile_repo.go`
- Modify: `backend/internal/service/group.go`
- Modify: `backend/internal/service/gateway_usage_billing.go`
- Modify: `backend/internal/service/openai_gateway_usage.go`
- Modify: `backend/internal/service/batch_image_public.go`
- Modify: `backend/internal/service/user_group_rate.go`
- Modify: `backend/internal/repository/user_group_rate_repo.go`
- Modify: `backend/internal/service/*_test.go` covering billing calculation

- [ ] 先写失败测试：余额请求使用 `BillingProfile.BalanceRateMultiplier`；套餐请求始终使用 `UserSubscription.RateMultiplierSnapshot`；修改用户旧倍率记录不再改变任一请求价格；账号倍率仍进入上游成本记录。
- [ ] 运行现有网关计费测试，确认它们仍从 `Group.RateMultiplier` 或 `UserGroupRateRepository.GetByUserAndGroup` 取用户收费倍率。
- [ ] 在请求计费上下文中显式传入 `BillingProfile` 与选中的订阅实例；套餐倍率优先且不可与余额倍率叠加。
- [ ] 把峰时、图片、视频与 Web Search 的余额计价配置迁移到 `BillingProfile`；套餐使用相同原始价格规则但用套餐倍率覆盖余额倍率。
- [ ] 从用户分组倍率服务/API/缓存中删除倍率读写；只保留 `RPMOverride` 相关接口和表字段，必要时重命名 DTO 以免 UI 继续展示倍率。
- [ ] 运行 `cd backend; go test -tags=unit ./internal/service -run 'Test.*Usage.*Billing|Test.*OpenAI.*Usage|Test.*BatchImage|Test.*RPM' -count=1`。

## Task 6: API, Wiring, and Compatibility Responses

**Files:**
- Modify: `backend/internal/service/payment_config_service.go`
- Modify: `backend/internal/service/payment_config_plans.go`
- Modify: `backend/internal/handler/admin/payment_handler.go`
- Modify: `backend/internal/handler/payment_handler.go`
- Modify: `backend/internal/handler/admin/*group*handler*.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/internal/server/routes/user.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/internal/repository/wire.go`
- Regenerate: `backend/cmd/server/wire_gen.go`
- Modify: `backend/internal/handler/admin/payment_handler_test.go`

- [ ] 先写失败测试：套餐创建/编辑可提交独立倍率和三种额度；公共支付套餐响应从套餐本身读取倍率额度，并仍带通道名称、平台与模型能力；原有 API Key 多分组请求格式不变。
- [ ] 运行处理器测试，确认其当前从 `Group` 拼装套餐定价与限额字段。
- [ ] 扩展套餐 CRUD DTO、公开 checkout 响应、管理员响应以及输入校验，删除组编辑和用户管理中面向终端收费的字段。
- [ ] 移除用户分组倍率 HTTP 路由；保留 RPM override 路由及管理员能力。
- [ ] 注入 `BillingProfileRepository`，重新生成 Wire 代码，并执行 `cd backend; go build ./cmd/server`。
- [ ] 运行 `cd backend; go test -tags=unit ./internal/handler/... ./internal/service -run 'Test.*Plan|Test.*Rate|Test.*Payment' -count=1`。

## Task 7: Frontend Subscription and Billing Configuration

**Files:**
- Modify: `frontend/src/types/payment.ts`
- Modify: `frontend/src/api/payment.ts`
- Modify: `frontend/src/views/admin/orders/PlanEditDialog.vue`
- Modify: `frontend/src/views/admin/orders/AdminPaymentPlansView.vue`
- Modify: `frontend/src/components/payment/SubscriptionPlanCard.vue`
- Modify: `frontend/src/views/user/PaymentView.vue`
- Modify: `frontend/src/views/admin/groups/**`
- Modify: `frontend/src/views/admin/users/**`
- Modify: relevant `frontend/src/locales/*.json`
- Add/Modify: focused Vitest specs next to changed components

- [ ] 先写失败 Vitest：套餐编辑框显示并提交套餐倍率、日/周/月额度；售卖卡从套餐数据显示倍率和限额；组页面不再显示余额/套餐收费配置；用户页不再展示个人余额倍率。
- [ ] 运行相关 Vitest，确认旧断言或 UI 仍依赖 `group.rate_multiplier`、`subscription_type` 和 group 限额。
- [ ] 保持支付商品、货币、在售状态和续购工作流；以套餐字段渲染套餐权益，以分组字段渲染平台/模型能力。
- [ ] 在分组配置中保留账号、能力、路由与 RPM；余额计价显示为独立 `BillingProfile` 配置界面或同页的“余额计价”区域，但不再作为分组字段提交。
- [ ] 运行 `cd frontend; npm run test:run -- <changed specs>`、`npm run typecheck`、`npm run lint:check`。

## Task 8: End-to-End Verification, Migration Audit, and Release Readiness

**Files:**
- Modify as needed only for verification fixes
- Add: `docs/memory/决策/2026-08-03-订阅计费重构实施结果.md`
- Modify: `docs/memory/当前状态.md`

- [ ] 使用干净数据库运行 192 及之前迁移；使用含旧分组/旧订阅/旧套餐的夹具运行回填审计，比较行数、到期时间、额度和倍率快照。
- [ ] 跑 Go 定向套件、相关处理器套件、前端 Vitest、前端类型检查和 lint、`go build ./cmd/server`、`git diff --check`。
- [ ] 人工验证四条请求路线：单套餐、同组双套餐先后切换、两组各有套餐、套餐耗尽后余额分组；分别覆盖 `/v1/chat/completions` 与 `/v1/responses`。
- [ ] 在本分支记录最终字段映射、旧字段保留期、缓存失效规则和回滚步骤；不在未核对完成前删除旧 `groups` 收费字段。
- [ ] 每个可独立回滚阶段用符合仓库规范的提交信息提交；通过所有验证后再由用户决定合并、打 Tag 和推送。

## Rollout and Rollback

1. 先部署包含迁移和双读能力的版本，迁移只新增字段、回填与索引，不删除旧数据。
2. 校验每个旧订阅都有套餐 ID、条款快照和可用 BillingProfile，再切换新写入与选择逻辑。
3. 若运行期发现选择或计费异常，可切回保留的旧 Group 字段读取路径；已发放的新实例因具备快照不会丢失权益。
4. 仅在一个发布周期后的独立变更中删除旧字段和旧倍率接口。
