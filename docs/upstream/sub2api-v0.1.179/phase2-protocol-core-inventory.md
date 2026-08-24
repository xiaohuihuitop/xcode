# v0.1.179 协议核心同步清单

官方基线：`Wei-Shaw/sub2api@v0.1.179`

固定提交：`75f88be5f75c27771836b586f7de1503afa0e3bc`

本清单是功能矩阵 `F019`（OpenAI/Codex Runtime）和 `F022`（通用协议兼容层）的唯一提交集合。`R002-R005` 是验收交叉索引，不重复计入提交集合。官方提交不直接覆盖 ProductCore、Handler 业务编排、数据库 migration 或前端产品目录。

## 提交集合

| SHA | 官方范围 | XCode 归宿 | 结论 |
| --- | --- | --- | --- |
| `272735b0a7bcc4a56c1c4ebe45792da444abb010` | Codex Responses namespace tools | Runtime Adapter / apicompat | direct_sync |
| `7d3bf86e550e149c28a230fe039bc1691b1b6b05` | Responses tool/input conversion | Runtime Adapter / apicompat | direct_sync |
| `fe217258653bf924e159c6b36ddaceb0af28b011` | Responses stream lifecycle | Runtime Adapter / apicompat | direct_sync |
| `85a27fae39f9ba0a2b35c791e998cf873c901eb1` | Responses terminal output | Runtime Adapter / apicompat | direct_sync |
| `ddf4c6fd813c7bab3d776c64bf0b6c604b350f34` | Chat/Responses fallback | Runtime Adapter | direct_sync |
| `21aacde0b3d340e21253b73a04f6e724b40a77de` | Responses request shaping | Runtime Adapter / apicompat | direct_sync |
| `30d2589ef0f0dc839b934b0b21a270d18b7af52b` | Responses tool calls | Runtime Adapter / apicompat | direct_sync |
| `e1b76e2245cf099485c86a1d3ebca5303daa690a` | Responses stream events | Runtime Adapter / apicompat | direct_sync |
| `fc5a1b78d20977a1afe235a36328932732f41eed` | Responses tool output | Runtime Adapter / apicompat | direct_sync |
| `ab326c96eb6dac0637a980251b73c04b94826944` | Responses compatibility | Runtime Adapter / apicompat | direct_sync |
| `f3c94d2099e92efaf0e20742fa338b6dbfb483e8` | OpenAI request/response bridge | Runtime Adapter | direct_sync |
| `915cc7e7bdd86cd390c41d7cb13be42c64285047` | Codex response normalization | Runtime Adapter | direct_sync |
| `74fcdf3d42b28a48446a6c5b1fda4e9a4ef23c49` | Responses tool protocol | Runtime Adapter / apicompat | direct_sync |
| `9f31df3fa836d7dff967e030e57b3c062818e9de` | Reasoning item ID validation | Runtime Adapter / apicompat | direct_sync |
| `280c1c86232fc2eac1e36fc1593cf00e0f245521` | Responses request validation | Runtime Adapter | direct_sync |
| `e91b4941680bf948166c4f1dbd96356c8a623bbb` | Responses compatibility fixes | Runtime Adapter / apicompat | direct_sync |
| `591d47fb9b706d3d96d1e36e0ea57a2fb8521fb1` | Responses tools | Runtime Adapter / apicompat | direct_sync |
| `fd9ce53281af6010cba6d8841aa435cc2de6c842` | Responses stream protocol | Runtime Adapter / apicompat | direct_sync |
| `e24cb99b79379c7294b552422861cc89028753ff` | OpenAI Responses transport | Runtime Adapter | direct_sync |
| `900194fab2a5485fe1aa19d350e448adce347b7c` | Codex tool bridge | Runtime Adapter / apicompat | direct_sync |
| `9662cff2e7c62fcfb99111415f5ac11a15748e14` | Responses terminal semantics | Runtime Adapter | direct_sync |
| `a8b9ea22b701704507fa597c03c1835173248f36` | Responses SSE | Runtime Adapter / apicompat | direct_sync |
| `44ef88f659a02ca9b725ce11b110e03cd43d814d` | Responses continuation | Runtime Adapter | direct_sync |
| `8219dcfc87ac270fe11414a98e326b06f5b4309f` | Codex turn-state headers | Runtime Adapter | direct_sync |
| `8ae6d8f67e72b099ed581b1455840ca62bb25561` | Responses protocol | Runtime Adapter | direct_sync |
| `c3063e01a4bf8c389bcd9dbef1ef6bd92477e107` | Message-only capacity recovery | Runtime Adapter / scheduling port | adapter_port |
| `539064798965888f14605eb76c2733cd58851c7f` | Request-scoped capacity recovery | Runtime Adapter / scheduling port | adapter_port |
| `612436a5a7cbb7ab9d4ce4174fcdff948fab31fb` | Encrypted reasoning cache bridge | Runtime Adapter / apicompat | direct_sync |
| `7e579cb28df5f2e7ed9ef84bc8bca6370acfa8f7` | WebSocket client tools | Runtime Adapter | direct_sync |
| `c253bd2c72dcce1aee21a4bc671ad23eb1bf5a34` | WebSocket terminal tools | Runtime Adapter | direct_sync |
| `b228b93e9c40ae9d3452890425c7dcea8a3a336b` | Buffered Chat read failover | Runtime Adapter / scheduling port | adapter_port |
| `bfac49fef9e9ba7543e312327c3625ab5210668f` | Responses input-token preflight | Runtime Adapter | direct_sync |
| `b94e484e23698621f8fd3b339eb9df8679009d64` | WebSocket tool replay | Runtime Adapter | direct_sync |
| `82cbe6aff7d963e5096d26df73671236a707ad24` | WebSocket later-turn 429 | Runtime Adapter | direct_sync |
| `2ef124629526439d71fb7410951ed21795d030d9` | Reasoning aliases | Runtime Adapter / apicompat | direct_sync |
| `fad2f215e88b974cd9073f2278b852bcbe2fdd8f` | Anthropic block conversion | Runtime Adapter / apicompat | direct_sync |
| `66ad405dd6ad3d6f5f286b14a85835843553c2cf` | Anthropic SSE conversion | Runtime Adapter / apicompat | direct_sync |
| `2f109e74caee1a33248744b05a700a65f03bec5c` | Failover classification | Runtime Adapter | adapter_port |
| `8aa425d22f943d0aa289794c98f0d233aadf3ec9` | Dial timeout handling | Runtime Adapter | direct_sync |
| `c33c3208e307c53c82daebc0ba303c3f09b51308` | Deferred client tools | Runtime Adapter / apicompat | direct_sync |
| `64090de6645f738e1a6dbdcf84cc08495304dd0a` | Tool history replay | Runtime Adapter / apicompat | direct_sync |
| `9c36b75a7d2feaf20db7de25b3ab33f108e04279` | Error frame flush | Runtime Adapter | direct_sync |
| `0b35370a7ab9fd419574922411bf94531e4350ee` | Stream failover boundary | Runtime Adapter | adapter_port |
| `76a13a5a8d56dca2befb8a3d70daf356c540f336` | Responses tool history | Runtime Adapter / apicompat | direct_sync |
| `a288bab73ace3ec789dd3af26ca8e98f3277c766` | Anthropic request compatibility | Runtime Adapter / apicompat | direct_sync |
| `4d9fedee204012ee51089259ce28e25add21e541` | Anthropic response compatibility | Runtime Adapter / apicompat | direct_sync |
| `401dd43b4b27883ae60d27c8270942a5fdbf2d07` | Chained-tool reasoning replay | Runtime Adapter / apicompat | direct_sync |
| `14a27f1960c46244977fec18ad25acb438261c0a` | Compatibility error semantics | Runtime Adapter | direct_sync |

## 验收交叉索引

| ID | 行为 | 必须证明 |
| --- | --- | --- |
| R002 | Responses input-token preflight | 成功响应含 `input_tokens`；无可用上游时返回明确错误；不写 usage/扣费事件 |
| R003 | Chat 非流式读取失败与容量恢复 | 上游响应体读取失败且客户端尚未写出时允许切换；写出任意语义字节后禁止拼接第二账号响应 |
| R004 | Responses WebSocket 多轮 | later-turn 429 只重试当前轮；client tools、item ID 和终态事件每轮 exactly once |
| R005 | reasoning/tool history 回放 | encrypted-only reasoning 不泄漏明文；缓存存在时按 item ID 回注链式工具 reasoning_content |

## 边界

- 以上提交只作为 Runtime 协议和 Provider 行为输入；官方 ProductCore、Group/Channel、支付、价格、用户资产和 migration 不在本阶段同步。
- `adapter_port` 项必须通过现有 `UpstreamFailoverError`、`RuntimeBridge v1` 和调度端口映射，不能把 Handler、Gin、Ent 类型引入 Runtime。
- 本清单完成不代表生产启用；矩阵只有在定向测试、构建和临时 GPT 验收完成后才可标记 `implemented`。
