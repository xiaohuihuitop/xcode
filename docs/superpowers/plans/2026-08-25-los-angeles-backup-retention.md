# Los Angeles Backup Retention Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为洛杉矶 XCode 服务器增加独立、可审计的备份保留任务：每日整机备份滚动保留 72 小时，升级备份按目标版本保留最新 3 个版本且每个版本只留最新一份。

**Architecture:** 将可复现的 Bash 清理器、无依赖测试脚本和 systemd 单元保存在 `deploy/backup-retention/`，服务器仅安装这些受版本控制的产物。清理器默认 dry-run，生产模式固定两个允许根目录，只有 `--apply` 才删除；测试模式只允许显式指定 `/tmp/xhh-prune-backups-test.*` 下的镜像目录。systemd timer 独立于现有备份任务，每日持久化触发并通过 `flock` 避免并发。

**Tech Stack:** Bash 5、GNU coreutils/findutils、util-linux `flock`、systemd、GitHub Actions 现有发布流程。

---

## File Map

- Create `deploy/backup-retention/xhh-prune-backups`: 生产清理器，负责安全枚举、保留决策、dry-run/apply、日志和索引重建。
- Create `deploy/backup-retention/xhh-backup-retention.service`: oneshot 清理服务，调用 `--apply`。
- Create `deploy/backup-retention/xhh-backup-retention.timer`: 每日独立调度，错过后补跑。
- Create `deploy/tests/backup-retention-test.sh`: 使用临时镜像目录覆盖 72 小时边界、版本去重、版本数量、非法命名、符号链接和 dry-run。
- Modify `docs/memory/当前状态.md`: 仅在服务器验收后记录已安装策略、单元状态和验证证据。

## Task 1: Build the Retention Contract as Failing Tests

**Files:**
- Create: `deploy/tests/backup-retention-test.sh`
- Test target: `deploy/backup-retention/xhh-prune-backups`

- [ ] **Step 1: Add a dependency-free Bash test harness**

Create a test script with `set -Eeuo pipefail`, a `trap` that removes its own `mktemp -d -t xhh-prune-backups-test.XXXXXX` fixture, `pass`, `fail`, `assert_exists`, `assert_missing`, and `run_pruner` helpers. The fixture must mirror production paths under the temporary prefix:

```bash
TEST_ROOT="$(mktemp -d -t xhh-prune-backups-test.XXXXXX)"
SERVER_ROOT="$TEST_ROOT/opt/server-backups"
UPGRADE_ROOT="$TEST_ROOT/opt/xcode/backups"
mkdir -p "$SERVER_ROOT" "$UPGRADE_ROOT"

run_pruner() {
  XHH_PRUNE_ALLOW_TEST_ROOTS=1 \
    "$PRUNER" --test-root-prefix "$TEST_ROOT" "$@"
}
```

Each test creates a fresh pair of directories and captures stdout/stderr. Use `touch -d` for deterministic mtimes and create only empty fixture files/directories; no real backup contents are needed.

- [ ] **Step 2: Add the daily-backup boundary tests**

Create daily archives at 71 hours 59 minutes, exactly 72 hours, and 72 hours 1 minute before a fixed `--now-epoch`. Assert dry-run removes nothing, then assert `--apply` deletes only the 72-hour-1-minute archive. Assert `.partial`, `.staging-*`, a nonmatching archive, and a matching symlink are retained and reported as skipped where applicable.

Use a fixed current time so the boundary is exact:

```bash
NOW_EPOCH=1787587200
touch -d "@$((NOW_EPOCH - 4319 * 60))" "$SERVER_ROOT/server-backup-new.tar.gz"
touch -d "@$((NOW_EPOCH - 4320 * 60))" "$SERVER_ROOT/server-backup-boundary.tar.gz"
touch -d "@$((NOW_EPOCH - 4321 * 60))" "$SERVER_ROOT/server-backup-old.tar.gz"
run_pruner --now-epoch "$NOW_EPOCH" --apply
```

- [ ] **Step 3: Add upgrade-version retention tests**

Create two backups targeting `v1.0.12`, plus one each targeting `v1.0.9`, `v1.0.10`, and `v1.0.11`. Give the duplicate `v1.0.12` directories different mtimes. Assert apply mode keeps only the newest `v1.0.12` backup and the three newest target versions (`1.0.10`, `1.0.11`, `1.0.12`), deleting target `1.0.9`.

Use names shaped like the actual server convention:

```text
v1.0.11-before-v1.0.12-20260825010000
v1.0.11-before-v1.0.12-20260825020000
```

- [ ] **Step 4: Add semantic comparison and skip-safety tests**

Verify `1.0.10` sorts newer than `1.0.9` rather than lexically. Add an invalid target version, a malformed directory, a regular file matching the upgrade glob, and a symlink matching the upgrade glob; assert all survive and the output contains a concrete skip reason. Add a test proving a `--test-root-prefix` outside `/tmp/xhh-prune-backups-test.*` exits nonzero before any deletion.

- [ ] **Step 5: Run the tests and verify the expected initial failure**

Run on a Linux Bash environment:

```bash
bash deploy/tests/backup-retention-test.sh
```

Expected: nonzero exit because `deploy/backup-retention/xhh-prune-backups` does not exist yet.

## Task 2: Implement the Safe Backup Pruner

**Files:**
- Create: `deploy/backup-retention/xhh-prune-backups`
- Test: `deploy/tests/backup-retention-test.sh`

- [ ] **Step 1: Add argument parsing, strict mode, and production roots**

The script starts with:

```bash
#!/usr/bin/env bash
set -Eeuo pipefail

readonly PROD_SERVER_ROOT="/opt/server-backups"
readonly PROD_UPGRADE_ROOT="/opt/xcode/backups"
readonly LOCK_FILE="/run/lock/xhh-prune-backups.lock"
apply=false
now_epoch="$(date +%s)"
root_prefix=""
```

Accept only `--apply`, `--now-epoch <non-negative integer>`, and `--test-root-prefix <absolute path>`. Reject unknown or repeated malformed arguments. Permit a test prefix only when `XHH_PRUNE_ALLOW_TEST_ROOTS=1`, its canonical path matches `/tmp/xhh-prune-backups-test.*`, and its two expected child roots resolve to `<prefix>/opt/server-backups` and `<prefix>/opt/xcode/backups`. Without the explicit test switch, canonical roots must equal the two production constants exactly.

- [ ] **Step 2: Add locking and root/candidate validation**

Open file descriptor 9 on the production lock file, or `<test-prefix>/xhh-prune-backups.lock` in test mode, and acquire it with:

```bash
if ! flock -n 9; then
  printf 'ERROR action=skip reason=lock_busy\n' >&2
  exit 75
fi
```

Before planning any deletion, require both roots to exist as real directories and not symbolic links. For every candidate, require an absolute path, reject symlinks, canonicalize the parent with `realpath -e`, require that parent to equal the expected root, and require the expected type (`-f` for daily archives, `-d` for upgrade backups).

- [ ] **Step 3: Implement exact 72-hour daily retention**

Enumerate only first-level regular files matching `server-backup-*.tar.gz` with `find -P ... -mindepth 1 -maxdepth 1 -type f -print0`. Compare GNU `stat -c %Y` against `cutoff_epoch=$((now_epoch - 72 * 60 * 60))`; retain files whose mtime is equal to or newer than the cutoff and plan deletion only when `mtime < cutoff_epoch`.

Separately enumerate first-level matching symlinks and log:

```text
action=skip type=daily reason=symlink path=...
```

Do not enumerate or delete `.partial`, `.staging-*`, or nonmatching names.

- [ ] **Step 4: Implement upgrade target parsing and SemVer comparison**

For each first-level real directory matching `v*-before-v*-*`, remove the prefix through `-before-v`, remove the final `-<backup-id>` suffix, then validate the remaining target as stable SemVer `MAJOR.MINOR.PATCH`, with each component decimal and no leading sign. Invalid names are logged with `reason=invalid_target_version` and retained.

Store target version, mtime, and path in Bash arrays. Compare versions numerically component by component using `10#` conversion, so `1.0.10 > 1.0.9`. For each target version, retain the greatest-mtime directory (breaking exact mtime ties by lexically greatest full path), mark older duplicates with `reason=duplicate_target_version`, then keep only the three numerically greatest target versions and mark all representatives of older versions with `reason=target_version_outside_latest_three`.

- [ ] **Step 5: Add auditable dry-run/apply behavior**

For every planned deletion print mode, type, absolute path, mtime in epoch and ISO-8601 UTC, apparent size from `du -sh`, and reason. Dry-run prints `action=would_delete` and does not mutate. Apply mode revalidates each candidate immediately before deletion, then uses `rm --` for files and `rm -rf --one-file-system --` for validated first-level directories.

Any root mismatch or candidate escape is fatal. A malformed or wrong-type first-level upgrade target is retained with a skip record, because it is not an allowed deletion target.

- [ ] **Step 6: Rebuild the daily backup index only after successful apply**

After apply mode completes without deletion errors, atomically write `/opt/server-backups/backup-index.txt` (or the test mirror) using a temporary file inside the same root. List remaining matching regular archives in newest-first mtime order, one basename per line, then `mv --` the temporary file into place. Dry-run must not change an existing index.

- [ ] **Step 7: Run syntax and retention tests**

```bash
bash -n deploy/backup-retention/xhh-prune-backups
bash -n deploy/tests/backup-retention-test.sh
bash deploy/tests/backup-retention-test.sh
```

Expected: all assertions pass and the test script exits 0.

## Task 3: Add Independent systemd Scheduling

**Files:**
- Create: `deploy/backup-retention/xhh-backup-retention.service`
- Create: `deploy/backup-retention/xhh-backup-retention.timer`

- [ ] **Step 1: Create the oneshot service**

Use this exact unit contract:

```ini
[Unit]
Description=Prune XCode server and upgrade backups
Documentation=https://github.com/xiaohuihui222/XCode
After=local-fs.target
ConditionPathIsDirectory=/opt/server-backups
ConditionPathIsDirectory=/opt/xcode/backups

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/xhh-prune-backups --apply
Nice=10
IOSchedulingClass=best-effort
IOSchedulingPriority=7
```

- [ ] **Step 2: Create the daily timer**

Schedule after the existing daily backup window without coupling to the backup service:

```ini
[Unit]
Description=Run XCode backup retention daily

[Timer]
OnCalendar=*-*-* 04:30:00
RandomizedDelaySec=10m
Persistent=true
AccuracySec=1m
Unit=xhh-backup-retention.service

[Install]
WantedBy=timers.target
```

- [ ] **Step 3: Verify unit syntax offline**

On Linux with systemd tooling:

```bash
systemd-analyze verify \
  deploy/backup-retention/xhh-backup-retention.service \
  deploy/backup-retention/xhh-backup-retention.timer
```

Expected: exit 0 with no unit syntax errors. A missing installed `/usr/local/sbin/xhh-prune-backups` warning is acceptable only during offline repository verification; repeat verification after installation with no warning.

## Task 4: Repository Verification and Checkpoint Commit

**Files:**
- Create: `deploy/backup-retention/xhh-prune-backups`
- Create: `deploy/backup-retention/xhh-backup-retention.service`
- Create: `deploy/backup-retention/xhh-backup-retention.timer`
- Create: `deploy/tests/backup-retention-test.sh`

- [ ] **Step 1: Run all focused checks in a Linux environment**

```bash
bash -n deploy/backup-retention/xhh-prune-backups
bash -n deploy/tests/backup-retention-test.sh
bash deploy/tests/backup-retention-test.sh
systemd-analyze verify deploy/backup-retention/xhh-backup-retention.service deploy/backup-retention/xhh-backup-retention.timer
git diff --check
```

Expected: syntax and fixture tests pass, unit files parse, and no whitespace errors are reported.

- [ ] **Step 2: Review the exact repository diff**

```bash
git status --short
git diff -- deploy/backup-retention deploy/tests/backup-retention-test.sh docs/superpowers
```

Expected: only the approved retention artifacts and documents are included; preserve and do not stage `tools/__pycache__/`.

- [ ] **Step 3: Commit the implementation checkpoint**

```bash
git add deploy/backup-retention deploy/tests/backup-retention-test.sh docs/superpowers/plans/2026-08-25-los-angeles-backup-retention.md
git commit -m "feat(ops): 增加备份自动保留任务"
```

Expected: commit succeeds; the unrelated `tools/__pycache__/` remains untracked.

## Task 5: Install and Exercise the Los Angeles Server

**Files on server:**
- Install: `/usr/local/sbin/xhh-prune-backups`
- Install: `/etc/systemd/system/xhh-backup-retention.service`
- Install: `/etc/systemd/system/xhh-backup-retention.timer`

- [ ] **Step 1: Capture the pre-install baseline**

Over an in-memory SSH session, record but do not persist credentials or secrets:

```bash
find /opt/server-backups -maxdepth 1 -type f -name 'server-backup-*.tar.gz' -printf '%T@ %s %p\n' | sort -n
find /opt/xcode/backups -mindepth 1 -maxdepth 1 -printf '%y %T@ %p\n' | sort -n
df -h / /opt
systemctl status xhh-server-backup.timer --no-pager
```

Expected: inventory matches the previously observed 7 daily archives and 2 upgrade directories, unless a scheduled backup has legitimately added newer entries.

- [ ] **Step 2: Transfer to temporary server paths and verify hashes**

Upload the three repository artifacts to root-owned temporary files under `/tmp`, compare local and remote SHA-256 values, then install atomically with:

```bash
install -o root -g root -m 0755 /tmp/xhh-prune-backups /usr/local/sbin/xhh-prune-backups
install -o root -g root -m 0644 /tmp/xhh-backup-retention.service /etc/systemd/system/xhh-backup-retention.service
install -o root -g root -m 0644 /tmp/xhh-backup-retention.timer /etc/systemd/system/xhh-backup-retention.timer
systemctl daemon-reload
```

Expected: installed hashes equal repository artifact hashes and ownership/modes are root:root `0755`, `0644`, and `0644`.

- [ ] **Step 3: Run the production dry-run and reconcile every candidate**

```bash
/usr/local/sbin/xhh-prune-backups
```

Expected: exit 0, no filesystem changes, and every `would_delete` or `skip` record is explainable from the baseline inventory. Under the known current inventory, no backup should be selected for deletion.

- [ ] **Step 4: Run the first apply through systemd**

```bash
systemctl start xhh-backup-retention.service
systemctl status xhh-backup-retention.service --no-pager
journalctl -u xhh-backup-retention.service -n 100 --no-pager
```

Expected: oneshot exits successfully. Daily archives older than 72 hours, if any appeared since the earlier inventory, are removed; exact-boundary/newer archives remain. Upgrade backups retain one newest directory for each of the three newest valid target versions. Invalid targets remain with skip logs.

- [ ] **Step 5: Enable and verify the timer**

```bash
systemctl enable --now xhh-backup-retention.timer
systemctl is-enabled xhh-backup-retention.timer
systemctl is-active xhh-backup-retention.timer
systemctl list-timers xhh-backup-retention.timer --all --no-pager
systemd-analyze verify /etc/systemd/system/xhh-backup-retention.service /etc/systemd/system/xhh-backup-retention.timer
```

Expected: timer is `enabled` and `active`, the next trigger is around 04:30 local server time plus at most 10 minutes, and unit verification exits 0.

- [ ] **Step 6: Verify retention results and application health**

```bash
find /opt/server-backups -maxdepth 1 -type f -name 'server-backup-*.tar.gz' -printf '%T@ %s %p\n' | sort -n
cat /opt/server-backups/backup-index.txt
find /opt/xcode/backups -mindepth 1 -maxdepth 1 -printf '%y %T@ %p\n' | sort -n
df -h / /opt
docker ps --format '{{.Names}} {{.Status}}'
curl -fsS -o /dev/null -w '%{http_code}\n' https://www.aitodo.work/health
```

Expected: index exactly lists remaining daily archives newest first, no out-of-policy valid backup remains, all three XCode containers report healthy/running, and public health returns HTTP 200.

## Task 6: Record Verified State, Push, and Monitor GitHub

**Files:**
- Modify: `docs/memory/当前状态.md`

- [ ] **Step 1: Update project memory with facts only**

Append a concise dated entry containing the exact installed script/unit paths, daily and upgrade retention rules, first apply result, timer next-run evidence, remaining backup counts/sizes, and XCode health result. Do not record SSH credentials, API keys, account passwords, or unrelated operational data.

- [ ] **Step 2: Verify the final repository state**

```bash
git diff --check
git status --short
git log -3 --oneline
```

Expected: only `docs/memory/当前状态.md` is pending after the implementation commit; `tools/__pycache__/` remains untracked and unstaged.

- [ ] **Step 3: Commit the verified deployment record**

```bash
git add docs/memory/当前状态.md
git commit -m "docs(ops): 记录备份保留任务部署状态"
```

- [ ] **Step 4: Push the approved commits**

```bash
git push origin main
```

Expected: push succeeds and `main` matches `origin/main` except for the preserved untracked `tools/__pycache__/`.

- [ ] **Step 5: Monitor GitHub release automation**

Inspect the GitHub Actions runs triggered by the push until the relevant CI, security scan, and release workflow reach a terminal state. Report exact workflow conclusions and the latest release/tag produced. If a workflow fails, diagnose from its logs before claiming completion; do not rewrite history or force-push.
