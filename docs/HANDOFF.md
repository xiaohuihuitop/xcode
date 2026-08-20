# XCode 项目交接

## 当前身份

- 仓库：`xiaohuihuitop/xcode`
- 分支：`main`
- 官方来源：`upstream/main`
- 发布 Tag：从 `v1.0.0` 开始
- 离线镜像包：`xcode_latest.tar`

XCode 是独立产品仓库，不是官方 Sub2API 的发布分支。Sub2API 只作为当前 Runtime 实现和官方同步来源；产品层的订阅、平台、余额、计费、用量和 UI 是 XCode 自有定制。

## 已完成定制

- 平台统一管理模型白名单、端点能力和账号池。
- API Key 可授权多个平台、多个套餐和余额；套餐优先，余额按授权回退。
- 套餐倍率使用订阅快照，余额使用全局倍率，两者不叠加。
- 使用记录包含平台资产归属、Token、费用、延迟、输出速率和缓存率。
- 左上角品牌入口回到 `/`；订阅在仪表盘展示，购买重复套餐创建新实例。
- OpenAI Images、Chat、Responses、Messages 使用纯 Runtime 入口。

## 部署原则

1. 先下载并校验 `xcode_latest.tar`。
2. 备份 compose、环境配置和 PostgreSQL。
3. `docker load -i xcode_latest.tar`，只替换应用容器。
4. 保持 PostgreSQL、Redis、应用数据卷和外部网络配置不变。
5. 健康检查后做 Chat/Responses 真实请求和数据库用量对账。

仓库历史重写不会触碰服务器数据；镜像更新也不应执行 `down -v`、删除数据库卷或重新初始化数据库。

## 已知风险

- 已付款订单仍需要订单级套餐快照才能抵抗套餐后续修改或删除。
- 退款提供商的幂等和超时恢复需要上线前演练。
- 生产管理入口和支付回调必须使用 HTTPS。
- 远程服务器凭据不写入仓库；接手者从受保护的部署系统获取。

## 接手流程

先读 `PROJECT_DEFINITION.md`、`ARCHITECTURE.md` 和 `DEVELOPMENT_GUIDE.md`，再看 `docs/memory/当前状态.md`。修改前确认属于 ProductCore、Runtime 还是适配器边界，并补对应测试和决策记录。官方更新只从 `upstream/main` 审查合并，发布只从 XCode `main` 打 Tag。
