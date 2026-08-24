# Official Runtime zone

本目录是 Sub2API 官方 Runtime 代码的受控同步区。同步进来的代码只负责协议、Provider、OAuth、AI 账号调度、额度、冷却、重试、失败切换和上游执行。

## 边界

- `ProductCore` 继续拥有用户、API Key、平台、模型价格、套餐、余额、支付、订单和 usage 扣费。
- `RuntimeBridge v1` 和 XCode Adapter 负责纯请求、交换、事件和 usage facts 映射。
- 本目录不得 import ProductCore、公开 Handler、Gin context、支付、套餐或用户资产内部类型。
- 官方文件只有在版本 `sync-plan.json` 标记为 `direct_sync`、具有明确 `target_path` 且人工设置 `approved: true` 后才能进入本目录。
- 需要 XCode 语义的改动必须留在 `backend/internal/runtime/sub2api` Adapter，并记录在对应版本的 `xcode-adapter-overrides.md`。
- 本目录不执行官方 migration，不创建第二套账号或计费数据模型。

同步门禁由仓库根目录的 `tools/sub2api_upstream_sync.py` 和 `backend/internal/architecture` 测试共同执行。
