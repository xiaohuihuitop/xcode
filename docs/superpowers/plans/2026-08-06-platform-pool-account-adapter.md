# 平台账号池与适配器边界实施计划

> 执行方式：当前 `my2.0` 分支内联实施；每个行为变化先补回归测试，再修改实现。

## 目标

将“平台”作为唯一可配置的路由入口：平台拥有适配器、模型规则、端点能力和账号集合；API Key 只授权平台、套餐和余额；调度继续复用现有 GatewayRuntime 的 OAuth、协议适配、账号轮换与失败切换。模型定价从旧 Group 关系中移出，旧配置不再作为运行时回退路径。

## 边界与不做事项

- 不重写 GatewayRuntime 的 OAuth 刷新、协议适配、账号调度和失败切换。
- 不保留“平台账号池”作为第二套业务概念；账号通过 `platform_id` 归属平台。
- 不把旧 `group_id` 作为新请求的路由或计费回退；历史表仍可只读展示。
- 不在本次实现中物理删除旧表或旧历史数据；配置重建采用备份、归档、重新录入。

## 实施任务

### 1. 平台模型候选与端点筛选

先修改并扩展以下测试，确保测试先失败：

- `backend/internal/productcore/authorizer_test.go`
- `backend/internal/service/platform_model_rules_test.go`
- `backend/internal/service/platform_service_test.go`

覆盖同一模型位于多个平台、不同端点能力分别命中、授权平台过滤、端点不支持、同优先级歧义、同一平台重复规则和空端点能力拒绝。

随后实现：

- 将 `productcore.PlatformCatalog` 改为候选列表接口。
- 为模型候选携带精确匹配/通配符优先级，ProductCore 在授权和端点筛选后选择最高优先级；同优先级多个候选返回明确歧义错误。
- `platformModelResolver` 返回跨平台候选，取消跨平台同模型的全局冲突；同一平台内部仍拒绝重复规则。
- 模型规则端点能力必须显式且非空；运行时空能力不再隐式代表全部端点。
- 保留必要的兼容包装函数，但新请求链路只调用候选列表接口。

验证命令：

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/productcore ./internal/service -run 'Test(Authorize|Platform|Model)' -count=1
```

### 2. 平台授权、账号适配器与旧分组隔离

先补 `backend/internal/service/platform_asset_request_test.go`、账号服务测试和相关路由测试，覆盖 API Key 无平台、平台未授权、端点能力不匹配、账号平台快照与平台适配器不一致。

实现：

- API Key 没有平台授权时直接拒绝，不回退 `api_keys.group_id`。
- 请求按授权平台候选和端点能力选择实际平台，再进入现有 GatewayRuntime。
- 创建/编辑账号必须选择一个平台；适配器由平台派生并只读，客户端不能再提交第二个独立平台或分组。
- 账号保存 `platform_id` 与服务端派生的 `platform` 快照；快照不一致的账号禁止调度并给出可诊断错误。
- 平台和模型规则保存前校验适配器、端点能力和规则冲突。

验证命令：

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service ./internal/handler ./internal/repository -run 'Test(Account|Platform|APIKey|Gateway)' -count=1
```

### 3. 独立定价与资产扣费

先为定价选择、路由上下文和扣费归属补测试，重点覆盖：平台切换后实际计费上下文正确、套餐倍率优先于余额倍率、套餐耗尽即时切换下一套餐、无套餐时余额扣费、旧分组字段不会影响新请求。

实现：

- 新增独立的定价目录/解析入口，按适配器、上游模型、计费模式和阶梯查找模型价格；不再从 `Platform.legacy_group_id` 或 API Key 旧分组推导新请求价格。
- 保留现有模型基础价格与管理员自定义价格能力，将自定义价格迁移到独立目录；旧 Group 自定义价格不自动作为运行时回退。
- 请求上下文携带实际平台和实际定价项；用量记录、倍率、RPM、套餐扣费均写入实际资产上下文。
- 账号倍率仅用于上游成本/账号统计，不参与用户资产倍率。
- 保持余额和套餐可独立勾选；创建 Key 默认勾选余额，套餐可多选或为空。

验证命令：

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service ./internal/productcore -run 'Test(Billing|Usage|Subscription|Pricing|Asset)' -count=1
```

### 4. 前端概念收口

先补或更新组件测试：

- `frontend/src/components/admin/platform/PlatformPoolDialog.vue` 及对应 spec
- `frontend/src/components/account/CreateAccountModal.vue`、`EditAccountModal.vue` 及对应 spec
- `frontend/src/components/keys/KeyAssetPermissionsForm.vue` 及 API Key 表单 spec
- 管理员套餐页面/路由 spec

实现：

- 导航和页面只显示“平台”，不再显示“平台账号池”。
- 平台表单只配置适配器、模型规则、端点能力和状态；移除旧分组/旧定价字段。
- 账号创建先选择平台，适配器自动显示为只读；编辑时不能改成第二个平台。
- API Key 平台、套餐、余额在同一次表单提交中保存并在重新打开时完整回显；无平台授权时给出明确提示。
- 恢复管理员套餐创建、编辑和列表入口，保证平台重构不隐藏套餐管理。

验证命令：

```powershell
Set-Location frontend
npm run test:run -- src/components/admin/platform src/components/account src/components/keys
npm run typecheck
npm run lint:check
```

### 5. 配置重建工具、文档与最终验证

实现一个可重复执行且默认只读预览的配置重建命令/脚本：先导出备份，再归档旧平台/规则/账号关系，清理 API Key 平台关系，要求重新录入平台和账号；用户、Key、余额、套餐、订阅、支付/退款审计和使用记录保留。脚本必须有 `--apply` 明确开关，禁止默认删除历史。

同步维护：

- `docs/superpowers/specs/2026-08-06-platform-pool-account-adapter-design.md`
- `docs/memory/当前状态.md`
- `docs/memory/项目概览.md`
- 新增本次实现决策记录，包含触发信号、约束、正确做法和验证方式。

最终验证：

```powershell
git diff --check
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/...
Set-Location frontend
npm run test:run
npm run typecheck
npm run lint:check
npm run build
```

若完整测试受环境限制，必须记录具体失败命令、错误和已完成的定向证据，不得将未执行的检查称为通过。

## 风险控制

- 共享接口变更集中在 ProductCore 与平台解析层，先更新测试桩和调用方，再进入计费改造。
- 定价迁移不删除旧数据；运行时显式禁用旧 Group 回退，出现缺价时返回可诊断错误。
- UI 与后端契约同时验证“保存后重新打开”，防止平台和套餐互相覆盖。
- 部署前只在 `my2.0` 发布；不触碰稳定分支 `my`。

## 本次执行结果

- 已完成平台候选解析、端点能力过滤、平台授权、平台账号调度、独立定价上下文、账号表单和配置重建工具。
- 前端定向回归为 27 个测试文件、292 个测试通过；`npm run typecheck`、`npm run lint:check` 和 `npm run build` 通过。
- 后端 ProductCore、平台/计费/网关相关 Service、Handler、Middleware 定向回归及 `go test ./cmd/...` 通过。
- `go test -tags=unit ./internal/repository -count=1` 已通过；API Key 资产权限读取按数据库方言区分 PostgreSQL 数组和 SQLite `IN` 查询，usage-log sqlmock 参数及图片尺寸元数据断言已同步当前字段顺序。
- `go test -tags=unit ./internal/...` 与前端 `npm run test:run` 在本机五分钟长测窗口内未完成并被工具超时终止，不将长测标记为通过；可控包级和前端定向门禁已通过。
