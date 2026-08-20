# XCode 独立仓库迁移实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将已验证的 `my2.0` 代码基线发布到独立的 `xiaohuihuitop/xcode` 仓库，并从 `v1.0.0` 开始使用 XCode 发布线。

**Architecture:** 新仓库的 `main` 分支从当前 `my2.0` 提交建立；官方 `Wei-Shaw/sub2api` 只作为 `upstream` 远端继续按需同步。发布工作流保留现有质量门禁和 `xcode:latest` / `xcode_latest.tar` 产物，仅将来源分支和 Tag 校验改为新仓库的 `main` / `v*`。

**Tech Stack:** Git worktree、GitHub SSH remote、GitHub Actions、GHCR、Docker Buildx、Go、pnpm。

---

### Task 1: 建立新仓库基线

**Files:**
- Remote: `git@github.com:xiaohuihuitop/xcode.git`
- Source commit: `d3d4ad1fd` (`my2.0`)

- [x] **Step 1: Verify the destination is empty and the source is the published baseline**

Run `git ls-remote git@github.com:xiaohuihuitop/xcode.git` and `git show -s --format='%H %s' my2.0`.
Expected: destination has no branch refs; source is `d3d4ad1fd fix(usage): 恢复使用记录延迟字段`.

- [x] **Step 2: Push only the product baseline as destination `main`**

Run `git push git@github.com:xiaohuihuitop/xcode.git my2.0:refs/heads/main`.
Expected: destination `main` points to `d3d4ad1fd`; old `my2-v0.2.x` tags are not copied into the new product repository.

### Task 2: Switch the release identity

**Files:**
- Modify: `.github/workflows/my2-release.yml:1-73,220-305`
- Modify: `docs/memory/项目概览.md:22`
- Modify: `docs/memory/当前状态.md` (prepend current migration state)
- Create: `docs/memory/决策/2026-08-20-XCode独立仓库与v1发布线.md`

- [x] **Step 1: Change workflow source validation**

Change the workflow display name to `XCode Release`, tag trigger and validation to `v*`, workflow input example to `v1.0.0`, concurrency group to `xcode-release-*`, fetch `origin/main`, and verify the tag is an ancestor of `origin/main`.

- [x] **Step 2: Keep product image names and set the build identity**

Keep the local `xcode:latest` image and `xcode_latest.tar` offline package; do not publish to GHCR. Change the Go build metadata `main.BuildType` from `my2` to `xcode` and release text from “My2 branch” to “XCode main branch”. Do not rename the internal `/app/sub2api` binary, database tables, service labels, or runtime package paths in this migration.

- [x] **Step 3: Record the confirmed repository boundary**

Record that `xiaohuihuitop/xcode` is the product repository, `main` is its release branch, `upstream` remains `Wei-Shaw/sub2api`, tags start at `v1.0.0`, and the old fork remains historical. Update stale current-state sentences that claim the published `my2-v0.2.32` work is uncommitted or undispatched.

### Task 3: Validate and publish the first XCode release

**Files:**
- No application code changes.

- [x] **Step 1: Run static and workflow checks**

Run `git diff --check`, the repository workflow/static release gate tests, and inspect that no `my2-v` or `origin/my2.0` source-gate rule remains in the active workflow.

- [x] **Step 2: Commit migration metadata and workflow changes**

Run `git add .github/workflows/my2-release.yml docs/memory docs/superpowers/plans/2026-08-20-xcode-repository-migration.md` and commit with `chore(release): 建立 XCode 独立发布线`.

- [x] **Step 3: Push the migration commit to the new repository**

Run `git push xcode HEAD:main` and verify `git ls-remote xcode refs/heads/main` equals the local commit.

- [x] **Step 4: Create and push `v1.0.0`**

Run `git tag -a v1.0.0 -m "release: XCode v1.0.0"` and `git push xcode refs/tags/v1.0.0`.

- [x] **Step 5: Verify GitHub Actions and release assets**

Wait for all source, frontend, backend, lint, and release jobs to complete successfully. Verify the release contains `xcode_latest.tar` and `xcode_latest.tar.sha256`; GHCR is intentionally excluded from the release contract.

### Task 4: Preserve update and deployment boundaries

**Files:**
- No database or server data changes.

- [x] **Step 1: Verify upstream sync commands remain explicit**

Keep the official source remote as `upstream` and document that updates use reviewed commits from `upstream/main`; never merge the old fork `main` into XCode `main`.

- [ ] **Step 2: Verify deployment compatibility**

Confirm the existing server can continue loading `xcode_latest.tar`; do not recreate PostgreSQL, Redis, or data volumes for the repository move.
