# Sub2API 官方同步目录

每个官方正式版本使用独立目录，例如 `sub2api-v0.1.179/`。目录中的机器生成文件由同步工具产生，人工审阅文件必须在验证前补齐，不得用空表或自动默认值掩盖未分类功能。

## 机器生成文件

```text
metadata.json
commits.csv
files.csv
sync-plan.json
```

生成命令示例：

```powershell
python -B tools/sub2api_upstream_sync.py snapshot `
  --repo Wei-Shaw/sub2api `
  --base v0.1.169 `
  --target v0.1.179 `
  --expected-commit 75f88be5f75c27771836b586f7de1503afa0e3bc `
  --cache-dir C:\Temp\xcode-sub2api-v0.1.179

python -B tools/sub2api_upstream_sync.py plan `
  --snapshot-dir C:\Temp\xcode-sub2api-v0.1.179 `
  --current-root . `
  --output-dir docs/upstream/sub2api-v0.1.179
```

## 人工审阅文件

```text
runtime-feature-matrix.md
database-impact.md
xcode-adapter-overrides.md
README.md
```

同步工具会在每行同时记录官方 `source_path` 和 XCode `target_path`，并将候选默认设为 `approved: false`。它不会自动决定 ProductCore 等价实现、数据库语义或 Adapter 代码。人工审阅完成后，只能将确认的纯 Runtime 行改为 `approved: true`，再运行：

```powershell
python -B tools/sub2api_upstream_sync.py validate `
  --inventory-dir docs/upstream/sub2api-v0.1.179 `
  --current-root .
```

`validate` 要求 `sync-plan.json` 与 `files.csv` 的路径、状态、类别和哈希完全一致。`apply` 只复制 `approved: true`、目标路径位于 `backend/internal/runtime/sub2api/upstream/` 的 `direct_sync` 文件；Adapter、ProductCore、migration、CI 和前端变化必须停下进行人工处理。
