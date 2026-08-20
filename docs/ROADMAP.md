# XCode 路线图

## 当前阶段：可上线基线

- OpenAI Images、Chat Completions、Responses、Messages 已进入纯 Runtime 生产路径。
- 平台模型白名单、平台账号池、多平台 Key、多套餐和余额优先级已确定。
- 离线 Docker 发布线使用 `main`、`v1.0.0+` Tag、`xcode:latest` 和 `xcode_latest.tar`。

## 上线前门禁

1. 固定生产环境的 HTTPS、JWT、支付回调和管理员访问策略。
2. 为 PostgreSQL 建立异地备份并完成一次真实恢复演练。
3. 为支付订单增加订单级套餐快照，避免套餐改名或删除影响已付款交付。
4. 验证退款提供商幂等、超时恢复和重复通知处理。
5. 用真实 Key 做 Chat/Responses 双端点回归，核对费用与延迟记录。

## 下一阶段

- 补齐订单和退款可靠性，建立可审计的充值/订阅状态机。
- 继续把适配器内部遗留的传输兼容桥收口为纯 `HTTPExchange`。
- 加强发布门禁：迁移审计、镜像内容检查、Tag 祖先检查和离线包可加载验证。
- 为平台、账号和资产调度增加可观测性与故障演练。

## 后续内核替换

只有当 Runtime 端口、UsageSink、端点能力和失败终态契约稳定后，才评估替换 Sub2API。替换顺序是新 Runtime 适配器、定向端点回归、灰度流量、计费对账，最后才考虑移除旧适配器；ProductCore 和前端不随内核内部实现变化。
