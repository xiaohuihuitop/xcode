# XCode Sub2API 同步 Skill 设计

## 目标

创建个人 Skill `xcode-sub2api-sync`，把 XCode 从上一轮已完成的 `Wei-Shaw/sub2api` 官方正式 Tag 同步到新正式 Tag 的流程编排为可重复工作流。Skill 默认完成只读评估、本地制品生成、人工范围确认、分组同步和完整验证，并在提交、推送、Tag、Release 或服务器部署前停止。

## 适用范围

Skill 仅用于 `xiaohuihuitop/xcode` 仓库，触发场景包括：

- 用户要求检查、盘点或同步 Sub2API 官方更新。
- 用户指定旧正式 Tag 和新正式 Tag，要求生成同步计划。
- 用户要求继续一轮尚未完成的官方 Runtime 同步。

Skill 不用于普通 Git 上游合并、其他仓库同步或未经盘点的生产升级。进入流程后仍以当前仓库 `AGENTS.md`、正式同步策略和代码事实为准。

Canonical upstream 固定为 GitHub 仓库 `Wei-Shaw/sub2api`；Tag、完整 commit SHA、`snapshot.json.repo` 和 `sync-plan.json.source` 必须来自并一致指向该仓库。

## 设计选择

采用薄编排 Skill，不复制仓库里的确定性同步逻辑。Skill 负责阶段选择、所有权判断、人工确认、验证门禁和结果汇总；以下现有资源继续作为唯一实现来源：

- `tools/sub2api_upstream_inventory.py`
- `tools/sub2api_upstream_sync.py`
- `docs/upstream/SYNC_POLICY.md`
- 上一轮 `docs/upstream/sub2api-v*/` 同步包
- `docs/superpowers/plans/2026-08-24-official-runtime-layering.md`

这样可以避免个人 Skill 与仓库工具产生两套路径分类、migration 映射或同步命令。

## Skill 结构

Skill 安装到个人 Codex Skill 目录：

```text
~/.codex/skills/xcode-sub2api-sync/
├── SKILL.md
├── agents/
│   └── openai.yaml
└── references/
    ├── workflow.md
    └── verification.md
```

- `SKILL.md`：触发条件、仓库识别、模式选择、硬边界和引用路由。
- `workflow.md`：预检、盘点、确认、分组同步、断点续跑和停止条件。
- `verification.md`：按变更范围选择测试、架构、数据库、前端和最终差异门禁。
- `openai.yaml`：用户界面名称、简短说明和默认调用提示；保留自动发现能力。

不在 Skill 内新增同步脚本，也不复制服务器凭据、API Key、账号密码或环境配置。

## 工作模式

Skill 根据用户措辞选择两种模式：

### 只读盘点

当用户说“看看更新”“检查差异”“评估同步难度”时，只允许检查仓库文件/status、已有同步包，并用 `git ls-remote` 等非变异命令查询 `Wei-Shaw/sub2api` 正式 Tag。只有确认 validate 自身不写入时，才可用 `python -B` 验证已有同步包。不得 fetch、snapshot、plan、generate、创建缓存/输出目录或修改文件/Git refs。

若不存在当前可审计同步包，只报告只读来源能够确认的目标身份/差异信息、拟生成的路径和文件及风险，并请求进入本地制品生成；不得声称已有完整 inventory。

### 本地同步与验证

当用户明确说“同步 Sub2API”“开始同步”“继续同步”时：

1. 先完成不写文件或 Git refs 的只读评估。
2. 获得本地制品生成授权后，针对 `Wei-Shaw/sub2api` 固定 snapshot/目标身份并生成 preliminary package。
3. 人工解决全部 `needs_review`，完成 feature matrix、database impact 和 Adapter coverage，再运行 full validate。
4. 向用户展示已审阅且已验证的范围并等待本轮功能组确认。
5. 只在确认后修改本地业务代码并运行分组验证。
6. 输出可提交结果，但默认不执行 Git 远程或生产操作。

用户对 Skill 的调用不构成提交、推送、Tag、Release 或部署授权。

## 阶段状态机

### 阶段一：预检

- 确认当前目录属于 XCode 仓库，并核对 `origin`、`upstream` 和当前分支。
- 读取项目 `AGENTS.md`、`docs/memory/当前状态.md`、`docs/upstream/SYNC_POLICY.md` 和上一轮同步包的当前状态。
- 检查工作树并区分既有用户改动与本轮产物；不得清理或覆盖未知改动。
- 从上一轮已完成同步包确定 `--base`，并通过 canonical `Wei-Shaw/sub2api` remote 的正式 Tag 确定 `--target` 和完整 commit SHA。
- 禁止使用 `upstream/main`、分支名或未固定 commit 作为同步身份。

若仓库错误、基线不明确、目标不是正式 Tag、工作树冲突无法隔离或同步工具门禁失败，则停止并报告。

### 阶段二：生成可审计同步包

- 仅在本地制品生成获授权后运行 `snapshot` 和 `plan`；snapshot 必须使用 `Wei-Shaw/sub2api`，且 `snapshot.json.repo`、`sync-plan.json.source` 与目标 Tag/full SHA 必须匹配同一 canonical remote。
- 在新的 `docs/upstream/sub2api-v<version>/` 目录保存版本身份、commit/file 清单、功能矩阵、数据库影响、Adapter 覆盖和 `sync-plan.json`。
- 对候选文件分类为 `direct_sync`、`adapter_port`、`xcode_equivalent`、`productcore_mapping` 或 `not_runtime`。
- 汇总 Runtime、Provider、ProductCore、前端、database、依赖、CI 和基础设施影响。
- 自动生成内容不得覆盖人工维护的功能矩阵、数据库影响或 Adapter 覆盖结论。
- 人工解决 `commits.csv`/`files.csv` 的全部 `needs_review`，对齐 `sync-plan.json` 重复字段与 disposition；只有 `needs_review` 为零后才运行 full validate。

### 阶段三：人工范围确认

preliminary generation、人工审阅和 full validate 依次完成后，在任何业务代码写入前向用户报告：

- 基线 Tag、目标 Tag、目标 commit 和差异规模。
- 建议同步的功能组及优先级。
- 已解决的 `needs_review` 处置结果，以及 ProductCore、schema/migration、公共契约、依赖、根配置和 CI 风险。
- 明确排除的官方产品功能。

默认优先级为 P0 OpenAI/Codex Runtime、P1 当前已配置 Provider、P2 扩展端点，最后才评估 UI 或运营能力。用户可缩小范围；扩大到 schema、公共契约、依赖、根配置或 CI 时必须再次明确确认。

### 阶段四：分组同步

- 每个功能组单独建立失败测试或可验证基线，再实施最小适配。
- `direct_sync` 只允许复制到 Official Runtime zone；完整已验证 plan 保持审计来源，实际 apply 使用经结构化 JSON 严格投影和校验的分组 plan。
- apply 前对全部选中行完成 source preflight：`source_path` 必须解析在 `source_root` 内、文件存在且 SHA-256 等于 `official_sha256`；同时保留 target drift 检查。
- apply 是顺序、非事务操作。开始前备份全部既有 target 并记录 absent targets；失败后枚举 partial writes、保留备份，并在恢复或删除前请求明确授权，不得静默恢复或继续下一组。
- `adapter_port` 通过 RuntimeBridge、GatewayRuntime、Driver、Port 或 XCode 配置映射接入。
- `productcore_mapping` 只能按 XCode 语义重新实现，不允许整体覆盖官方文件。
- 保持用户、API Key、平台、模型价格、套餐、余额、订单、支付、计费和 usage 的 ProductCore 所有权。
- 每个功能组独立验证并形成清晰差异；一个分组失败时不继续叠加其他分组。

### 阶段五：本地验证与交付

- `adapter_port`、`productcore_mapping` 或 `xcode_equivalent` 改动后先归档当前清单并按同一 frozen snapshot/identity 可审计 rebaseline，再验证同步清单，确保源码变化全部可解释。
- 运行同步工具测试、Official Runtime、RuntimeBridge、架构、相关后端测试和服务构建。
- 触及共享前端 contract 或前端代码时，运行相关 Vitest、typecheck、lint 和生产构建。
- 触及 migration 或 schema 时，必须执行号段审计、临时数据库升级与恢复；官方 migration 不得直接执行。
- 执行 `git diff --check`，审查无关改动、敏感信息、依赖、根配置、CI、migration 和 Ent schema 差异。
- 给出已同步、未同步、阻塞、验证结果、残余风险和建议提交拆分。

完成阶段五后停止。提交、推送、Tag、Release 和服务器部署属于后续独立工作流。

## 所有权与安全边界

- 禁止执行 `merge upstream/main`、整体文件树覆盖或把官方仓库作为 XCode 发布身份。
- 官方上游身份只能是 `Wei-Shaw/sub2api`；任何 repo/source/Tag/SHA 不一致都必须停止。
- ProductCore、前端产品、database、Release/CI 和部署文件不能标记为 `direct_sync`。
- 已发布 migration checksum 保持冻结；必要 Runtime migration 只能翻译到 `8000-8999`，ProductCore migration 使用 `9000-9999`。
- 不新增静默 fallback，不吞掉同步、验证、数据库或 Provider 错误。
- 不把凭据写入 Skill、仓库、同步包、日志或最终回复。
- 不删除或提交用户原有未跟踪文件。
- 没有真实 Provider 凭据时，只能声明代码和模拟测试通过，不得宣称真实验收通过。

## 断点续跑

每个阶段开始时重新读取当前代码、同步包和 Git 状态，不只依赖历史对话。Skill 根据以下事实判断续跑点：

- 新版本同步目录及其 `metadata.json`、`sync-plan.json` 是否存在且通过 validate。
- 人工审阅文件是否已完成。
- 哪些功能组已有提交或工作树差异及对应测试证据。
- 当前目标 Tag 与计划中的 commit 是否仍一致。

身份漂移、清单过期或当前代码哈希变化时，先重新生成和验证，不沿用陈旧批准结果。

## 错误处理

- snapshot 或下载失败：保留现有生产和本地代码不变，报告 canonical upstream 身份或网络错误。
- apply 失败：按 target baseline 枚举 partial writes 并保留任务备份；未经明确授权不恢复既有 target、不删除新建 target，也不继续下一组。
- validate 失败：停止 apply，列出清单与当前代码不一致项。
- 所有权冲突：停止对应文件，不把它降级为 `direct_sync`。
- 测试或构建失败：停留在当前功能组，定位根因后再继续。
- migration、依赖、根配置、CI 或公共 contract 意外出现：暂停并请求范围确认。
- 无法执行真实上游验收：明确记录为阻塞，不用模拟结果替代。

## 验收标准

Skill 完成后应满足：

1. 在 XCode 仓库中能正确区分零 filesystem/Git-ref 写入的只读评估和本地同步请求。
2. 能从上一轮正式同步 Tag 生成到 `Wei-Shaw/sub2api` 新正式 Tag 的可审计同步包。
3. 未经人工确认不会修改业务代码。
4. 未经单独授权不会提交、推送、打 Tag、发布或部署。
5. ProductCore、database、前端产品和基础设施不会被误判为 `direct_sync`。
6. 本地同步按功能组执行，失败不会继续叠加后续组。
7. 验证结果区分已执行、未执行、模拟通过和真实 Provider 通过。
8. Skill 通过官方 `quick_validate.py`，界面元数据和引用路径有效。
9. 使用无 Skill 的基线场景与启用 Skill 的相同场景做行为对照，确认 Skill 能阻止整体 merge、未经确认写代码和越权发布。

## 非目标

- 不自动决定所有官方功能都必须进入 XCode。
- 不把 Sub2API 官方前端或产品模型整体迁入 XCode。
- 不替代仓库同步工具、测试框架、GitHub Actions 或生产部署 runbook。
- 不在本 Skill 中保存服务器地址和凭据。
- 不保证一次调用完成跨多个高风险功能组的全部实现。
