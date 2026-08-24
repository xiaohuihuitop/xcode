# Sub2API 官方 Runtime 同步规则

## 目标

XCode 继续保持单进程、单容器部署，但将 Sub2API 官方 Runtime 与 XCode 产品代码按所有权分开。官方同步的对象是 Runtime 能力，不是把 XCode 恢复成官方 Sub2API 产品。

## 所有权边界

| 区域 | 目录/契约 | 所有者 | 同步规则 |
| --- | --- | --- | --- |
| ProductCore | `backend/internal/service`、`backend/internal/handler` 中的用户/Key/平台/套餐/支付/计费代码、`frontend/src` 产品页面 | XCode | 官方提交只能做 `productcore_mapping`，不得直接覆盖 |
| Runtime contract | `backend/pkg/runtimebridge/v1`、`backend/internal/gatewayruntime` | XCode | 公开请求/事件/交换/usage surface 保持向后兼容；官方变化必须单独审查，不得 `direct_sync` 覆盖 |
| XCode Adapter | `backend/internal/runtime/sub2api`、`backend/internal/gatewayruntime` 与端口适配 | XCode | 只接收明确列入 `xcode-adapter-overrides.md` 的适配改动 |
| Official Runtime zone | `backend/internal/runtime/sub2api/upstream` 及版本同步清单指定的纯 Runtime 包 | Sub2API 官方代码 | 仅允许纯 Runtime 的 `direct_sync`，不得引用 ProductCore、支付、套餐和用户资产 |
| Database | `backend/migrations`、Ent schema | XCode | 已发布 `000-079`、`192-200` checksum 冻结；Runtime 只能使用 `8000-8999`，ProductCore 使用 `9000-9999` |
| Release/CI | `.github/workflows`、Docker 和部署脚本 | XCode | 官方构建/发布变化只做门禁适配，不自动覆盖离线包契约 |

## 同步允许和禁止

允许：

- 以官方不可变 Tag 和完整 commit SHA 生成版本快照。
- 将纯协议、Provider、OAuth、账号调度、额度、冷却、重试和失败切换代码同步到 Official Runtime zone。
- 通过 `RuntimeBridge v1` 和 Adapter 将官方 Runtime 事实映射到 XCode 平台、AI 账号和 usage。
- 为必要 Runtime 状态建立幂等的 `8000-8999` migration，并提供存量数据和恢复测试。

禁止：

- `merge upstream/main`、`reset --hard upstream/main` 或官方文件树整体覆盖。
- 将 ProductCore、套餐、余额、API Key、支付、订单或模型价格标记为 `direct_sync`。
- 直接执行官方 migration，恢复 Group/Channel 产品表，或建立第二套用户/AI 账号/计费模型。
- 让官方 Runtime 直接写用户余额、套餐用量或订单状态。
- 新 Provider 自动创建平台、账号或生产流量。

## 每版同步输入和产出

上一轮已完成同步的官方 Tag 作为 `--base`，新正式 Tag 作为 `--target`；禁止使用移动的 `upstream/main` 作为发布身份。每个版本保存独立目录，例如 `docs/upstream/sub2api-v0.1.179/`，至少包含：

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

自动工具先生成候选清单，再由人工审查 `needs_review`、ProductCore 映射、数据库影响和 Adapter 覆盖点。未列入同步计划的文件不得被应用。

## 门禁顺序

```text
snapshot 官方 Tag
  -> 生成 commit/file/feature/database 清单
  -> 审查所有权和 Adapter 覆盖
  -> 先写失败测试，再同步一个功能组
  -> RuntimeBridge/架构/后端/前端门禁
  -> 临时环境真实 Provider 验收
  -> 数据库计数和恢复对账
  -> 离线镜像生成与校验
  -> Tag/Release/香港部署
```

任何一项未通过，只能保持当前生产版本，不得用静默 fallback 或直接覆盖绕过门禁。
