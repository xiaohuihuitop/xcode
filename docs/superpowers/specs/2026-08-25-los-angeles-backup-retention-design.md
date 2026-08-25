# 洛杉矶服务器备份保留策略设计

## 目标

- 防止洛杉矶服务器备份持续占用系统盘。
- 每日整机备份保留滚动 72 小时。
- 升级前备份保留最近 3 个目标版本；同一目标版本存在多份时只保留最新一份。
- 清理任务独立于备份任务运行，备份失败时仍可按计划清理旧备份。

## 范围

只处理以下目标：

- `/opt/server-backups/server-backup-*.tar.gz`
- `/opt/xcode/backups/v*-before-v*-*` 一级目录

明确不处理：

- `/opt/xcode/releases`
- Docker 镜像和回滚标签
- PostgreSQL、Redis 及应用实时数据目录
- 不符合约定命名格式的文件或目录

## 保留规则

### 每日整机备份

- 以文件修改时间为准，保留最近 72 小时内的所有完整归档。
- 删除修改时间超过 72 小时的 `server-backup-*.tar.gz`。
- 清理后重新生成 `/opt/server-backups/backup-index.txt`。
- `.partial` 和 `.staging-*` 不纳入本轮自动删除，避免掩盖正在执行或异常中断的备份。

### 升级前备份

- 从目录名中的 `before-v<版本>` 提取目标版本。
- 每个目标版本只保留修改时间最新的一份备份。
- 目标版本按语义版本号排序，只保留最新 3 个版本。
- 删除同版本较旧的重复目录，以及不属于最新 3 个目标版本的备份目录。
- 无法解析目标版本的目录不删除，只记录跳过原因。

## 实现

新增宿主机脚本：

```text
/usr/local/sbin/xhh-prune-backups
```

脚本要求：

- 使用 Bash 严格模式。
- 固定并校验两个允许的根目录，解析后的真实路径必须分别等于 `/opt/server-backups` 和 `/opt/xcode/backups`。
- 只枚举根目录下一层的约定目标，不跟随符号链接。
- 默认执行 dry-run；只有传入 `--apply` 才实际删除。
- 删除前逐项输出绝对路径、类型、时间、大小和删除原因。
- 任一目标逃逸允许根目录、类型不符或解析失败时立即退出，不执行该目标删除。
- 使用 `flock` 防止清理任务并发执行。

新增 systemd 单元：

```text
/etc/systemd/system/xhh-backup-retention.service
/etc/systemd/system/xhh-backup-retention.timer
```

Timer 每天运行一次，安排在现有每日备份任务之后，并启用 `Persistent=true`，关机期间错过调度后会在开机时补跑。Service 调用脚本的 `--apply` 模式，输出写入 systemd journal。

## 验证

1. 脚本语法检查通过。
2. dry-run 输出与人工盘点一致，且不改变任何文件。
3. 使用临时测试目录验证 72 小时边界、同版本去重、最近 3 个版本、非法命名跳过和符号链接拒绝。
4. 安装后执行一次 service，核对实际删除清单、剩余备份、索引、磁盘空间和 journal。
5. 验证 timer 为 enabled/active 且下次运行时间正确。
6. 验证 XCode 三个容器健康，公网 `/health` 返回 200。

## 当前首次清理预期

- `/opt/server-backups` 当前 7 份归档均未超过 72 小时，首次执行不删除。
- `/opt/xcode/backups` 当前只有 `v1.0.11` 和 `v1.0.12` 两个目标版本，首次执行不删除。
- 策略会在备份超过保留边界后自动生效。
