# Sub2API v0.1.179 官方同步基线

本目录固定 XCode 同步 Sub2API 正式版 `v0.1.179` 的可审计输入。同步身份以不可变的 Git Tag 和完整 commit SHA 为准：

- 仓库：`Wei-Shaw/sub2api`
- 基线范围：`v0.1.169...v0.1.179`
- 目标提交：`75f88be5f75c27771836b586f7de1503afa0e3bc`
- 官方归档 SHA-256：`80b2720941a10d4160090e2f99f3c8e2dd67cc2c6606aa4efc506b58ebff0ff4`
- 提交数：594

目标 Tag 的 `backend/cmd/server/VERSION` 内容仍为 `0.1.178`。这是官方源码与 Tag 的偏差，不得据此把同步目标降为 `v0.1.178`；后续验证始终使用 Tag、完整 commit SHA 和归档哈希。

## 生成与验证

从仓库根目录执行。快照只写入本机临时缓存；可选的 `GITHUB_TOKEN` 只从环境变量读取，不会写入清单。

```powershell
$cache = Join-Path $env:TEMP 'xcode-sub2api-v0.1.179-inventory'

python -B tools/sub2api_upstream_inventory.py snapshot `
  --repo Wei-Shaw/sub2api `
  --base v0.1.169 `
  --target v0.1.179 `
  --expected-commit 75f88be5f75c27771836b586f7de1503afa0e3bc `
  --cache-dir $cache

python -B tools/sub2api_upstream_inventory.py generate `
  --snapshot-dir $cache `
  --current-root . `
  --output-dir docs/upstream/sub2api-v0.1.179

python -B -m unittest tools/test_sub2api_upstream_inventory.py -v
python -B tools/sub2api_upstream_inventory.py validate `
  --inventory-dir docs/upstream/sub2api-v0.1.179
```

`snapshot` 重新下载官方归档并校验身份；日常复核已有缓存时可以只运行 `generate` 和 `validate`。`generate` 会更新当前 XCode 文件的 SHA-256，因此工具自身改变后，`files.csv` 中对应行也会改变。`validate` 默认把当前目录作为 `--current-root`，会拒绝新增、删除或哈希变化但尚未重新生成的源码文件；Git 忽略的构建产物和清单目录自身不参与比较。

## 文件所有权

机器生成文件：

- `metadata.json`：官方 Tag、commit、Release、归档哈希和生成时间。
- `commits.csv`：594 个提交的稳定顺序和自动分类。
- `files.csv`：官方与 XCode 文件存在性、哈希状态、类别和 migration 编号。
- `sync-plan.json`：官方来源路径、XCode Runtime 目标路径、候选动作和人工批准状态。

人工审阅文件：

- `runtime-feature-matrix.md`：Runtime 提交的功能归宿、实施阶段和验收测试。
- `xcode-adapter-overrides.md`：同步时必须保留的 XCode Adapter、Port、ProductCore 和架构门禁。
- `database-impact.md`：官方 migration `217-228` 的逐文件 XCode 映射。

重新生成机器文件后必须再次运行 `validate`。不得用自动生成覆盖三份人工审阅文件。

## 功能归宿

- `direct_sync`：纯 Runtime 行为，按官方结构同步并保留对应测试语义。
- `adapter_port`：Runtime 能力通过 RuntimeBridge、Driver、Port 或 XCode 配置映射接入。
- `xcode_equivalent`：XCode 已有等价实现，保留现有实现并补充对照回归。
- `productcore_mapping`：官方 Group、Channel、定价或资产行为映射到 XCode ProductCore。
- `not_runtime`：官方产品、营销、构建或已删除领域，不进入 Runtime 实现。

## 阶段门槛

进入阶段二之前必须同时满足：

- `metadata.json` 的目标提交与上方完整 SHA 完全一致。
- `commits.csv` 恰好 594 行，commit 和 file 类别均无 `needs_review`。
- Runtime 功能矩阵覆盖全部 Runtime 提交，且归宿和后续阶段非空。
- 官方 migration `217-228` 的 14 个文件全部映射，没有 `direct_sql`。
- 阶段一没有修改 Runtime、schema、migration、前端、依赖或部署文件。

XCode 与官方已经分叉，禁止执行 `merge upstream/main`、整体文件覆盖或直接执行官方 migration。阶段二至阶段六必须按矩阵逐批同步，ProductCore 继续唯一拥有用户账号、平台、模型价格、套餐、余额、API Key、支付、用量和扣费。

## 当前同步状态

- P0 GPT/Codex（F019、F022、R002-R005）已完成代码接入、本地回归和香港隔离副本验收。
- Official Runtime 区仍保持纯 Runtime 边界；ProductCore、migration、Ent schema、前端产品和香港生产均未被本阶段改动。
- 下一阶段只先处理 OpenAI/Codex 的 AI 账号状态映射；GLM 缺少可用上游凭据时仅允许代码和模拟测试，Gemini 不在当前范围。

## 后续正式版

升级到新的官方正式 Tag 时，保留本目录作为不可变历史，在新的版本目录重新运行流程。`--base` 使用本轮已完成同步的官方 Tag，`--target` 使用新 Tag，`--expected-commit` 使用新 Tag 解析出的完整 commit SHA；不得把移动的分支名作为同步身份。

```text
旧正式 Tag..新正式 Tag
  -> snapshot
  -> generate
  -> 人工清零 needs_review
  -> 更新功能矩阵和数据库映射
  -> validate
  -> 分阶段同步与验收
```
