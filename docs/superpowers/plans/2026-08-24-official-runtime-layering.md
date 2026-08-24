# 官方 Runtime 分层与持续同步实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. 每个任务完成后运行本任务门禁，再进入下一个任务。

**目标：** 将 Sub2API 官方 Runtime 代码与 XCode ProductCore/Adapter 的所有权彻底分开，使后续每个官方正式版都能通过固定工具生成差异、按边界同步和回归验证，而不是重新人工移植整个仓库。

**架构：** 保持单进程、单容器部署。XCode ProductCore 继续拥有用户、API Key、平台、模型价格、套餐、余额、订单、计费和 usage；官方 Runtime 区只负责协议、Provider、AI 账号调度、OAuth、额度、冷却、重试、失败切换和上游执行；`RuntimeBridge v1` 与 XCode Adapter 负责两者之间的纯数据映射。官方同步不执行 `merge upstream/main`、不覆盖 XCode 文件树、不直接执行官方 migration。

**技术栈：** Go 1.26.6、Python 3 标准库同步工具、PostgreSQL migration、Redis、现有 `pkg/runtimebridge/v1` 契约、GitHub Actions、离线 Docker Release。

---

## 结果与硬约束

每个官方正式版完成后必须得到以下结果：

1. `docs/upstream/sub2api-v0.1.180/`（后续版本按相同规则建立独立目录）保存不可变的官方 Tag、commit SHA、归档哈希、提交清单、文件清单、功能矩阵和数据库影响映射。
2. 官方 Runtime 改动只能进入清单标记为 `direct_sync` 的 Runtime 区；ProductCore、计费、套餐、Key、模型广场和支付目录不能被同步覆盖。
3. 需要 XCode 语义的改动只能进入 Adapter/Port 映射，所有改动列在该版本的 `xcode-adapter-overrides.md` 中。
4. 官方 migration 不直接执行；必要字段只能翻译为 `8000-8999` Runtime migration，ProductCore 自有 migration 使用 `9000-9999`。
5. 未配置的 Provider、模型和账号默认不启用，不改变香港生产流量。
6. 只有自动化门禁、离线镜像验证、临时环境业务验收和数据库对账均通过后，才允许 Tag、Release 和香港部署。

## 阶段总览

| 阶段 | 目标 | 行为变化 | 产出 |
| --- | --- | --- | --- |
| 0 | 固定所有权和同步契约 | 无 | 边界规则、版本基线、回滚清单 |
| 1 | 自动生成官方版本同步包 | 无 | 可重复的 snapshot/inventory/sync 工具 |
| 2 | 收口 RuntimeBridge 与 Adapter | 保持现有 GPT 行为 | ProductCore 不再依赖 Runtime 内部类型 |
| 3 | 同步 P0 GPT/Codex Runtime | GPT/Responses/Chat/SSE 行为增强 | 官方 Runtime 区、Adapter 回归、GPT 兼容门禁 |
| 4 | 同步 P1 Provider 与账号池能力 | 按平台逐项启用 | GLM/DeepSeek 等 Provider 适配和额度测试 |
| 5 | 同步扩展端点 | 默认不启用 | Images、Search、Media、WebSocket 等按能力上线 |
| 6 | 数据库、发布和生产流程固化 | 无结构破坏 | migration/恢复演练、Release 和香港灰度门禁 |

每一阶段都必须能单独提交、单独验证、单独回滚；不把 36 个功能矩阵组一次性混在同一提交中。

## Task 0: 固定同步契约和所有权清单

**Files:**
- Create: `docs/upstream/SYNC_POLICY.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/DEVELOPMENT_GUIDE.md`
- Modify: `docs/memory/当前状态.md`
- Test: `tools/test_sub2api_upstream_inventory.py`

- [x] **Step 1: 固定目录所有权表**

在 `docs/upstream/SYNC_POLICY.md` 记录以下不可变规则：

```text
ProductCore: backend/internal/productcore, 产品 service/handler, frontend 产品页面
Runtime contract: backend/pkg/runtimebridge/v1
XCode Adapter: backend/internal/runtime/sub2api, gatewayruntime 端口适配
Official Runtime zone: backend/internal/runtime/sub2api/upstream 及版本清单指定的纯 Runtime 包
Database: 现有 000-079 与 192-200 保持 checksum；Runtime 只允许 8000-8999
```

规则必须明确：官方代码不得 import ProductCore、订单、套餐或用户资产；ProductCore 不得 import `upstream` 内部类型。

- [x] **Step 2: 为当前 `v1.0.8` 建立基线快照**

记录 `main=a01d2272d0e549d0ec2c18ab914b619b67c96503`、当前生产 Tag、当前 migration=253、香港回滚镜像和生产计数基线。该步骤只写文档，不修改数据库。

- [x] **Step 3: 增加边界回归断言**

扩展 `tools/test_sub2api_upstream_inventory.py`，让测试拒绝以下情况：

```text
official migration 直接落入 backend/migrations/192-200
ProductCore 文件被标记为 direct_sync
RuntimeBridge v1 的公开字段因官方版本同步被删除
生产发布清单缺少旧镜像、数据库计数和回滚命令
```

- [x] **Step 4: 验证并记录**

运行：

```powershell
python -B -m unittest tools/test_sub2api_upstream_inventory.py -v
git diff --check
```

预期：测试全部通过，且没有 migration/契约边界违规。提交：

```text
docs(runtime): 固定官方同步所有权契约
```

## Task 1: 建立可重复的官方版本同步工具

**Files:**
- Create: `tools/sub2api_upstream_sync.py`
- Create: `tools/test_sub2api_upstream_sync.py`
- Modify: `tools/sub2api_upstream_inventory.py`
- Modify: `docs/upstream/sub2api-v0.1.179/README.md`
- Create: `docs/upstream/README.md`

- [x] **Step 1: 先写同步工具测试**

测试固定以下输入输出：

```python
class SyncPlanTests(unittest.TestCase):
    def test_plan_uses_immutable_tag_and_sha(self):
        plan = build_sync_plan(
            "v0.1.179",
            "v0.1.180",
            "0123456789abcdef0123456789abcdef01234567",
        )
        self.assertEqual(
            plan.target,
            "v0.1.180@0123456789abcdef0123456789abcdef01234567",
        )

    def test_plan_rejects_moving_branch(self):
        with self.assertRaisesRegex(ValueError, "immutable tag"):
            build_sync_plan(
                "v0.1.179",
                "main",
                "0123456789abcdef0123456789abcdef01234567",
            )

    def test_productcore_paths_are_never_direct_sync(self):
        self.assertEqual(
            classify_path("backend/internal/service/subscription.go"),
            "productcore_mapping",
        )

    def test_runtime_migration_is_rewritten_to_reserved_range(self):
        self.assertEqual(translate_migration_number(217), 8017)
```

测试实际使用 `unittest` 和标准库，不引入新的 Python 依赖。

- [x] **Step 2: 实现 snapshot/plan/apply/validate 四个命令**

工具接口固定为：

```text
python -B tools/sub2api_upstream_sync.py snapshot --repo Wei-Shaw/sub2api --base v0.1.169 --target v0.1.179 --expected-commit 75f88be5f75c27771836b586f7de1503afa0e3bc --cache-dir $env:TEMP\xcode-sub2api-v0.1.179
python -B tools/sub2api_upstream_sync.py plan --snapshot-dir $env:TEMP\xcode-sub2api-v0.1.179 --current-root . --output-dir docs/upstream/sub2api-v0.1.179
python -B tools/sub2api_upstream_sync.py apply --plan docs/upstream/sub2api-v0.1.179/sync-plan.json --mode runtime-only --worktree C:\Users\xiaohuihui\Desktop\XCODE
python -B tools/sub2api_upstream_sync.py validate --inventory-dir docs/upstream/sub2api-v0.1.180 --current-root .
```

`apply` 的安全闸门已经实现：计划同时记录官方 `source_path` 和 XCode `target_path`，所有候选默认 `approved=false`；只有人工审阅后标记为 `approved=true` 的 `direct_sync` 文件才能复制到 Official Runtime zone，遇到 ProductCore、frontend 产品、database、CI 或根配置时必须停止并列出文件。官方 migration 编号 `n` 统一映射为 `8000 + (n - 200)`，因此 `217 -> 8017`、`228 -> 8028`；超出 `8000-8999` 的结果直接失败。

- [x] **Step 3: 生成版本同步包**

每个新版本目录至少包含：

```text
metadata.json
commits.csv
files.csv
runtime-feature-matrix.md
database-impact.md
xcode-adapter-overrides.md
sync-plan.json
README.md
```

- [x] **Step 4: 做离线可重复验证**

运行：

```powershell
python -B -m unittest tools/test_sub2api_upstream_inventory.py tools/test_sub2api_upstream_sync.py -v
python -B tools/sub2api_upstream_sync.py validate --inventory-dir docs/upstream/sub2api-v0.1.179 --current-root .
```

预期：同一快照重复生成的 `files.csv`、`sync-plan.json` 内容稳定；计划之外的源码变化会失败。

- [x] **Step 5: 记录工具和规范**

提交：

```text
feat(runtime): 建立官方版本可重复同步工具
```

## Task 2: 收口 RuntimeBridge 与 XCode Adapter

**Files:**
- Modify: `backend/pkg/runtimebridge/v1/*.go`
- Modify: `backend/internal/runtime/sub2api/adapter.go`
- Modify: `backend/internal/runtime/sub2api/registry.go`
- Modify: `backend/internal/runtime/sub2api/openai_executor.go`
- Create: `backend/internal/architecture/sub2api_upstream_boundary_test.go`
- Modify: `backend/internal/runtime/sub2api/*_test.go`

- [x] **Step 1: 为 ProductCore/Runtime 依赖方向写失败测试**

架构测试扫描 Go import，拒绝以下依赖：

```text
backend/internal/productcore -> backend/internal/runtime/sub2api/upstream
backend/internal/runtime/sub2api/upstream -> productcore、payment、subscription、api_key
handler -> 官方 Runtime 私有类型
```

- [x] **Step 2: 保持公开契约稳定**

所有上游执行都只能通过现有 `v1.Request`、`gatewayruntime.HTTPExchange`、`gatewayruntime.UsageSink` 和 v1 事件终态；如必须扩展，只能增加向后兼容字段，并同时更新 `backend/pkg/runtimebridge/v1/contract_test.go` 与对应 gatewayruntime 契约测试。

- [x] **Step 3: 将当前 GPT 适配逻辑集中到 Adapter**

`adapter.go` 负责 XCode 平台/账号配置到 Runtime 请求的映射，`openai_executor.go` 只负责调用 Runtime 端口和转换终态；ProductCore 计费代码不得进入 executor。

- [x] **Step 4: 验证现有行为无回归**

运行：

```powershell
Set-Location backend
go test ./pkg/runtimebridge/v1 ./internal/runtime/sub2api ./internal/architecture -count=1
go build ./cmd/server
```

预期：现有 GPT Chat、Responses、SSE 的契约测试全部通过，运行时行为不变。提交：

```text
refactor(runtime): 收口 RuntimeBridge 与 Sub2API Adapter 边界
```

## Task 3: 建立官方 Runtime 区并完成 P0 GPT/Codex 同步

**Files:**
- Create: `backend/internal/runtime/sub2api/upstream/README.md`
- Create/Modify: `backend/internal/runtime/sub2api/upstream/openai/`
- Create/Modify: `backend/internal/runtime/sub2api/upstream/protocol/`
- Modify: `backend/internal/runtime/sub2api/adapter.go`
- Modify: `backend/internal/runtime/sub2api/openai_executor.go`
- Create: `backend/internal/runtime/sub2api/upstream_boundary_test.go`
- Modify: `docs/upstream/sub2api-v0.1.179/runtime-feature-matrix.md`

- [x] **Step 1: 建立官方 Runtime 区清单**

官方同步进来的文件必须在 `sync-plan.json` 中记录来源 commit、目标路径、归宿和测试；`README.md` 说明这些文件禁止直接加入 ProductCore 逻辑。

- [x] **Step 2: 以测试锁定 GPT 行为**

先为以下场景补回归测试，再同步实现：

```text
Responses input-token preflight
Chat 非流式读取错误的同账号重试和跨账号切换
Responses SSE pre-output failover 与写出后的不可切换
Responses WebSocket later-turn 429、工具 replay 和终态事件
reasoning/tool history 与 encrypted-only reasoning 回注
```

- [x] **Step 3: 只同步 P0 官方提交**

按 `F019/F022/R002-R005` 的 commit 列表逐组应用，不处理 Grok、支付、Channel、前端产品和官方 migration。每组完成后单独提交，例如：

```text
feat(runtime): 同步官方 Responses 终态与失败切换
```

- [x] **Step 4: 做 GPT 定向门禁**

运行：

```powershell
Set-Location backend
go test ./internal/runtime/sub2api ./internal/handler -run 'Responses|Chat|SSE|WebSocket|Failover|Usage' -count=1
go test ./internal/architecture ./pkg/runtimebridge/v1 -count=1
```

临时环境必须用 `gpt-5.6-sol` 完成 Chat、Responses、SSE、失败切换和失败不扣费验证；香港生产不在本阶段直接升级。

完成证据（2026-08-24）：本地 Handler/Routes/Service、Official Runtime、架构、RuntimeBridge、同步工具与服务构建门禁通过；香港隔离副本完成 Chat、Responses、SSE、普通 HTTP 403 跨账号切换、WebSocket 正常多轮和 later-turn 429 当前轮重放。成功 usage 的平台、AI 账号、订阅和套餐增量一致，失败请求不扣费，用户余额不变；migration 和 Ent schema 无变化，香港 `v1.0.8` 生产未升级。

## Task 4: 按 Provider 分批同步账号池、额度和协议能力

**Files:**
- Modify: `backend/internal/runtime/sub2api/upstream/provider/`
- Modify: `backend/internal/runtime/sub2api/adapter.go`
- Modify: `backend/internal/service/platform_account_pool.go`
- Modify: `backend/internal/service/account_usage_service.go`
- Modify: `backend/internal/handler/admin/account_handler.go`
- Modify: `frontend/src/views/admin/AccountsView.vue`
- Modify: `frontend/src/api/admin/accounts.ts`
- Create: `backend/internal/runtime/sub2api/provider_*_test.go`

- [x] **Step 1: 先完成 OpenAI/Codex 的 AI 账号状态映射**

只允许 Runtime 维护 AI 账号的 OAuth、quota、cooldown、retry、failover 和可调度状态；用户账号、Key 和套餐仍由 ProductCore 管理。账号事实必须写入现有 `accounts.extra` 或已批准的 Runtime migration，不新增第二套账号表。

完成证据（2026-08-24）：OpenAI/Codex OAuth 出站身份已统一覆盖 HTTP、raw passthrough、Runtime、WebSocket、账号测试、额度快照、PAT、OAuth exchange/refresh 和 models manifest；账号级 UA 只保留客户端类型与设备指纹，版本由官方稳定 Release 自动向前同步。Codex 指纹 seed 仅在管理员显式启用收敛模式时写入现有 `accounts.extra`，创建、单个更新和批量更新均保护为系统管理字段；现有 quota、cooldown、retry、failover、临时不可调度和 scheduler outbox 继续作为唯一账号状态链路。完整 service、repository、OpenAI 包、ApplicationGateway、architecture、RuntimeBridge、Wire、Server 构建和随机顺序回归通过；`backend/migrations` 与 Ent schema 无变化。worker 审查补齐了 Start/Stop 幂等及设置/GitHub/写库错误可见性。本机 `-race` 因 C 编译器不支持 64 位模式无法执行，不计为通过。

- [ ] **Step 2: 以平台为单位同步国产 Provider**

顺序固定为 `glm -> deepseek -> 其他已配置平台`。每个平台必须分别完成：Chat、Messages/Responses（若官方支持）、Base URL/Header、账号测试、额度查询、429/401/403/5xx 分类、冷却和失败切换。没有有效上游凭据时只完成代码和模拟测试，不宣称真实验收通过。

- [ ] **Step 3: 完成后台只读和配置回填测试**

后台只能展示 AI 账号 Runtime 事实，不暴露凭据，不恢复官方 Group/Channel 配置。前端测试必须覆盖平台筛选、额度刷新、错误状态和账号可调度状态。

- [ ] **Step 4: 每个平台独立提交和验收**

每个平台单独提交，命令：

```powershell
Set-Location backend
go test ./internal/runtime/sub2api ./internal/service ./internal/handler/admin -run 'Account|Quota|Platform|Provider' -count=1
```

提交示例：

```text
feat(runtime): 同步 GLM Provider 账号池能力
```

当前进度（2026-08-24）：GLM 已完成 XCode 平台适配的代码与模拟验收。额度查询通过现有 `accounts.extra` 写入 5h/weekly 使用事实，后台提供只读 `GET /api/v1/admin/accounts/:id/glm-quota`，账号测试、Chat/Responses、Header/Base URL、冷却和失败切换继续复用 `openai` 技术适配器与独立 `platform_id` 账号池；未恢复官方 Group/Channel。真实 GLM 上游仍因现有凭据返回 401，未宣称真实验收通过。DeepSeek 及其他平台尚未开始。

## Task 5: 同步扩展端点但保持默认关闭

**Files:**
- Modify: `backend/internal/runtime/sub2api/upstream/media/`
- Modify: `backend/internal/handler/sub2api_media_port.go`
- Modify: `backend/internal/handler/sub2api_auxiliary_executor.go`
- Modify: `backend/internal/service/product_usage_sink.go`
- Create: `backend/internal/runtime/sub2api/media_*_test.go`
- Modify: `docs/upstream/sub2api-v0.1.179/runtime-feature-matrix.md`

- [ ] **Step 1: 按能力拆分 Images、Search、Audio、Video、Live、Composite**

每个端点必须明确请求映射、上游模型、终态 usage facts、客户端断开、失败切换和计费归属；没有平台配置时返回明确的 unavailable，不自动创建平台或账号。

- [ ] **Step 2: 先完成测试 double，再接真实 Provider**

所有媒体/异步任务先使用现有 exchange/upstream recorder 测试成功、失败、超时和 exactly-once usage；真实账号验收单独记录，不把模拟测试当成生产通过。

- [ ] **Step 3: 扩展 ProductCore usage 事实而不让 Runtime 扣费**

Runtime 只发送 token、媒体数量、时延、上游模型和终态；套餐/余额扣费仍由 `product_usage_sink.go` 完成，失败终态不得生成成功扣费。

## Task 6: 数据库影响翻译和恢复门禁

**Files:**
- Create: `backend/migrations/8001_runtime_provider_quota.sql`
- Create: `backend/migrations/8001_runtime_provider_quota_test.go`
- Modify: `docs/upstream/sub2api-v0.1.180/database-impact.md`
- Modify: `backend/internal/migrations/*`
- Create: `tools/runtime_migration_audit.py`
- Create: `tools/test_runtime_migration_audit.py`

- [ ] **Step 1: 对每个官方 migration 做语义拆分**

逐项标记为 `xcode_equivalent`、`adapter_mapping`、`new_runtime_field` 或 `not_applicable`；禁止复制官方 SQL、Group/Channel 表和官方计费字段。

- [ ] **Step 2: 为必要 Runtime 字段建立幂等 migration**

每条迁移必须包含重复执行保护、旧数据默认值、失败可见性和 rollback 说明；执行前后核对 users、api_keys、platforms、accounts、subscriptions、usage_logs、schema_migrations 和 Redis keys。

- [ ] **Step 3: 做真实恢复演练**

在临时 PostgreSQL/Redis 上执行升级、导出、恢复和回放请求；只有恢复后的关键计数、套餐扣费和 usage 归属一致，才能进入发布阶段。

## Task 7: CI、Release 和香港发布流程固化

**Files:**
- Modify: `.github/workflows/backend-ci.yml`
- Modify: `.github/workflows/release.yml`
- Modify: `.github/workflows/source-gate.yml`
- Modify: `.github/workflows/security-scan.yml`
- Modify: `scripts/build-offline-image.ps1`
- Modify: `docs/BRANCH_AND_IMAGE_BUILD_CN.md`
- Modify: `docs/HANDOFF.md`

- [ ] **Step 1: 加入同步包门禁**

CI 必须执行：

```powershell
python -B -m unittest tools/test_sub2api_upstream_inventory.py tools/test_sub2api_upstream_sync.py -v
python -B tools/sub2api_upstream_sync.py validate --inventory-dir docs/upstream/sub2api-v0.1.180 --current-root .
```

并拒绝 ProductCore 直接同步、migration 号段越界、未列出的 Adapter 改动和缺少官方 commit 身份的同步提交。

- [ ] **Step 2: 固化离线镜像内容检查**

Release 必须验证镜像架构、版本、revision、migration 清单、健康检查和 `xcode_latest.tar.sha256`；不发布 GHCR，保持 `xcode:latest` 和离线 tar 契约。

- [ ] **Step 3: 固化香港发布 runbook**

每次升级顺序固定为：下载 tar → SHA-256 → 记录旧镜像 → PostgreSQL/Redis/data/.env/Compose 备份 → 载入镜像 → 启动并等待 healthy → HTTPS/后台/API/GPT 业务验收 → 数据计数对账 → 保留 rollback 标签。

- [ ] **Step 4: 只读观察窗口后再正式切换**

新镜像先在临时环境和本地生产副本验收；香港生产采用短观察窗口，发现健康检查、usage、扣费、错误切换或 schema 对账异常立即回滚旧镜像，不回滚数据库之外的历史数据。

## Task 8: 每次官方正式版的固定执行顺序

**Files:**
- Modify: `docs/upstream/README.md`
- Modify: `docs/memory/当前状态.md`

- [ ] **Step 1: 固定版本输入**

只接受官方正式 Tag 和完整 commit SHA，不使用 `upstream/main` 作为发布身份。上一轮已完成同步 Tag 作为 `--base`，新 Tag 作为 `--target`。

- [ ] **Step 2: 生成并审查同步包**

运行 snapshot、plan、validate；人工只审查 `needs_review`、ProductCore 映射、数据库影响和 Adapter 覆盖点。

- [ ] **Step 3: 按优先级同步**

顺序固定为：`P0 GPT/Codex` → `P1 已配置国产 Provider` → `P2 扩展端点` → 低优先级 UI/运营能力。每组单独提交和回归。

- [ ] **Step 4: 发布前验收**

必须完成后端、前端、架构、migration、离线镜像、临时环境真实请求、失败不扣费、数据库恢复和生产备份清单；任一项未完成只能标记为“未发布/阻塞”。

## 完成标准

该计划不以“文件树与官方仓库完全相同”为完成标准，而以以下事实为完成标准：

- 新官方正式版可通过一个固定命令生成可审计同步包。
- ProductCore、计费、套餐、Key、用户和生产数据没有被官方同步覆盖。
- P0 GPT Runtime 通过真实 Chat/Responses/SSE、失败切换、usage 和不扣费验收。
- 每个已启用 Provider 有独立的协议、账号池、额度和错误分类测试；无凭据 Provider 明确记录阻塞。
- 数据库 migration 只进入批准号段，升级和恢复演练可重复。
- Release 生成并校验离线镜像，香港部署可回滚，且升级前后核心计数和计费事实一致。

## 首个执行批次

首个批次只执行 `Task 0`、`Task 1` 和 `Task 2`，不改业务行为、不改生产数据库、不发布新 Tag。三项完成后，才开始 `Task 3` 的 P0 GPT 同步；这样可以先验证“后续更新是否真的变简单”，再投入 Provider 和扩展端点迁移。
