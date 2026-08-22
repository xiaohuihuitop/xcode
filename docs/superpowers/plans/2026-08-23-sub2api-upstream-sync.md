# Sub2API Upstream Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task with verification checkpoints.

**Goal:** 建立 Sub2API 官方更新的可审查同步基线，并将首批 GPT/Codex/Responses Runtime 修复选择性移植到 XCode。

**Architecture:** 官方仓库只作为 Runtime 更新来源。同步在独立 worktree 进行，候选补丁先经过文件范围、运行路径和数据库影响审查，再通过 XCode RuntimeBridge v1 端口进入生产路径；ProductCore、前端产品规则和业务 schema 不随官方分支整体替换。

**Tech Stack:** Git worktree/remotes, Go 1.26.6, Go tests, RuntimeBridge v1 contract tests, Vue/Vitest, Docker offline release workflow.

---

### Task 1: Establish the upstream review baseline

**Files:**
- Modify: Git remote configuration only; no tracked source files.
- Record: `docs/superpowers/specs/2026-08-23-sub2api-upstream-sync-design.md`
- Record: `docs/superpowers/plans/2026-08-23-sub2api-upstream-sync.md`

- [ ] **Step 1: Add the official remote in the isolated worktree**

Run:

```powershell
git remote add upstream https://github.com/Wei-Shaw/sub2api.git
git fetch --prune --tags upstream
```

Expected: `upstream/main` resolves to the official repository without changing XCode `main`.

- [ ] **Step 2: Capture the baseline and working tree state**

Run:

```powershell
git status --short
git log -1 --oneline
git show upstream/main:backend/cmd/server/VERSION
git show HEAD:backend/cmd/server/VERSION
```

Expected: the review branch is clean, XCode remains at its own release commit, and the Runtime version difference is explicit.

- [ ] **Step 3: Commit the approved design and plan**

```powershell
git add docs/superpowers/specs/2026-08-23-sub2api-upstream-sync-design.md docs/superpowers/plans/2026-08-23-sub2api-upstream-sync.md
git commit -m "docs(runtime): 记录 Sub2API 官方同步方案"
```

### Task 2: Build the candidate patch inventory

**Files:**
- Read: `backend/internal/runtime/sub2api/`, `backend/internal/handler/sub2api_openai_port.go`, `backend/internal/service/openai_*`.
- Read: official commits `d45135d8`, `fd6cd474`, `ffc01f9c`, `6244090c`, `b1e60ba4`, `e45490a3`.
- Create: `docs/superpowers/plans/2026-08-23-sub2api-upstream-candidate-inventory.md`.

- [ ] **Step 1: List official commits after the latest release**

```powershell
git log --oneline --decorate v0.1.179..upstream/main
git diff --stat v0.1.179...upstream/main
```

- [ ] **Step 2: Classify candidate commits by the XCode runtime path**

For each candidate, record the commit, changed files, whether the current production path calls those files through `sub2APIOpenAIPort`, and whether the patch must move into `internal/runtime/sub2api`.

- [ ] **Step 3: Record exclusions and database impact**

Explicitly exclude Grok, Gemini, Antigravity, DeepSeek, unrelated UI and product changes. Record whether the selected range changes `backend/migrations/`, `backend/ent/migrate/schema.go`, `backend/ent/schema/`, or dependency/toolchain files.

### Task 3: Port the first GPT/Responses compatibility unit

**Files:**
- Modify: `backend/internal/pkg/apicompat/chatcompletions_responses_bridge.go`.
- Modify: `backend/internal/service/openai_gateway_responses_chat_fallback.go`.
- Test: `backend/internal/pkg/apicompat/chatcompletions_responses_bridge_test.go`.
- Test: `backend/internal/pkg/apicompat/chatcompletions_responses_stream_lifecycle_test.go`.
- Test: `backend/internal/service/openai_gateway_responses_chat_fallback_test.go`.
- Test: the relevant RuntimeBridge contract tests under `backend/pkg/runtimebridge/v1/`.

- [ ] **Step 1: Write a failing regression test for the selected upstream behavior**

The test must exercise the pure `v1.Request`/`HTTPExchange` path and assert the exact response, retry or terminal usage behavior affected by the upstream patch.

- [ ] **Step 2: Apply only the smallest compatible implementation**

Keep the public RuntimeBridge contract unchanged. Translate upstream behavior at the Sub2API Driver or port boundary and preserve XCode platform/account/billing facts.

- [ ] **Step 3: Run the focused Go tests**

```powershell
go test -tags=unit ./internal/runtime/sub2api ./internal/handler ./internal/service
go test ./pkg/runtimebridge/v1
```

- [ ] **Step 4: Run the full local gates before selecting another patch**

```powershell
make test-unit
go build ./cmd/server
pnpm --dir frontend run test:run
pnpm --dir frontend run typecheck
pnpm --dir frontend run build
```

### Task 4: Review and hand off for release

**Files:**
- Read: `docs/ARCHITECTURE.md`, `docs/DEVELOPMENT_GUIDE.md`, `docs/HANDOFF.md`.
- Update: `docs/memory/当前状态.md` only after the selected patch is verified.

- [ ] **Step 1: Inspect the final diff and migration scope**

```powershell
git diff --check
git diff --stat HEAD
git diff --name-only -- backend/migrations backend/ent/migrate backend/ent/schema
```

- [ ] **Step 2: Run the release acceptance checklist**

Verify GPT Chat Completions, Responses, account failover, usage platform/account/subscription attribution, and `total_cost=actual_cost` in a disposable environment before any Hong Kong deployment.

- [ ] **Step 3: Commit the verified runtime port**

Use a Chinese Conventional Commit scoped to the actual behavior, for example:

```powershell
git add backend/internal/pkg/apicompat/chatcompletions_responses_bridge.go backend/internal/pkg/apicompat/chatcompletions_responses_bridge_test.go backend/internal/pkg/apicompat/chatcompletions_responses_stream_lifecycle_test.go backend/internal/service/openai_gateway_responses_chat_fallback.go backend/internal/service/openai_gateway_responses_chat_fallback_test.go
git commit -m "fix(runtime): 移植官方 Responses 兼容修复"
```

The staged file set must be exactly the files changed by Task 3, obtained with:

```powershell
git diff --name-only 7d0834f..HEAD -- backend/internal/pkg/apicompat backend/internal/service backend/internal/runtime backend/pkg/runtimebridge
```

- [ ] **Step 4: Stop before production release unless the user confirms deployment**

Generating a new XCode tag, uploading an offline package, and upgrading Hong Kong are separate release actions after this review branch is merged.
