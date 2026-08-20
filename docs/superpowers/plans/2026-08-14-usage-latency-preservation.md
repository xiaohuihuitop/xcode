# Usage Latency Preservation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve Runtime-computed total duration and first-token latency when ProductCore writes usage records.

**Architecture:** Keep `gatewayruntime.UsageFacts` as the timing source and repair only the compatibility reconstruction in `Sub2APIProductUsageFinalizer`. Cover both OpenAI and generic Gateway results so all migrated protocols preserve the same contract.

**Tech Stack:** Go 1.26, `testing`, `testify/require`, existing Sub2API service and gatewayruntime packages.

---

### Task 1: Add timing reconstruction regression tests

**Files:**
- Modify: `backend/internal/service/sub2api_product_usage_finalizer_test.go`

- [x] **Step 1: Write failing tests for OpenAI and Gateway timing reconstruction**

Add tests that create `gatewayruntime.UsageFacts` with `DurationMilliseconds: 4321` and `FirstTokenMilliseconds: 876`, call the reconstruction helpers, and assert that `Duration == 4321*time.Millisecond` and `FirstTokenMs` points to `876`. Add a zero-first-token case that asserts the pointer remains `nil`.

- [x] **Step 2: Run the tests and verify RED**

Run:

```powershell
go test -tags=unit ./internal/service -run 'ProductUsage.*Latency|ProductUsage.*Timing' -count=1
```

Expected: FAIL because the reconstruction helpers or timing fields are missing.

### Task 2: Preserve timing in ProductCore reconstruction

**Files:**
- Modify: `backend/internal/service/sub2api_product_usage_finalizer.go`
- Test: `backend/internal/service/sub2api_product_usage_finalizer_test.go`

- [x] **Step 1: Add the minimal reconstruction helpers**

Create focused helpers that build `OpenAIForwardResult` and `ForwardResult` from `gatewayruntime.UsageFacts`. Convert milliseconds with `time.Duration(value) * time.Millisecond`; only create a first-token pointer for positive values.

- [x] **Step 2: Use the helpers in `Finalize`**

Replace the duplicated inline result literals with the helpers while preserving all existing token, model, endpoint, stream and media fields.

- [x] **Step 3: Run the tests and verify GREEN**

Run:

```powershell
go test -tags=unit ./internal/service -run 'ProductUsage.*Latency|ProductUsage.*Timing|Sub2APIProductUsageFinalizer' -count=1
```

Expected: PASS.

### Task 3: Run regression and build gates

**Files:**
- No production files beyond Task 2.

- [x] **Step 1: Run targeted Runtime usage tests**

```powershell
go test -tags=unit ./internal/service ./internal/handler -run 'ProductUsage|RuntimeUsage|UsageFacts|OpenAI.*Usage|RecordUsage' -count=1 -timeout=20m
```

Expected: PASS.

- [x] **Step 2: Run complete affected package tests and build**

```powershell
go test -tags=unit ./internal/service ./internal/handler -count=1 -timeout=30m
go build ./cmd/server
git diff --check
```

Expected: all commands exit with code 0.

### Task 4: Deploy and verify the real environment

**Files:**
- No repository changes.

- [x] **Step 1: Back up server deployment metadata and database**

Back up `/opt/sub2api/docker-compose.yml`, relevant environment configuration, and create a compressed PostgreSQL logical backup before replacing the application container.

- [x] **Step 2: Deploy the fixed application only**

Replace only the `sub2api`/`xcode` application container. Keep PostgreSQL, Redis, volumes and certificates unchanged.

- [x] **Step 3: Execute endpoint matrix**

Send one non-streaming and one streaming request to each of `/v1/chat/completions` and `/v1/responses` using the provided temporary API key.

- [x] **Step 4: Verify persisted timing facts**

Query the four new `usage_logs` rows by request ID. Assert every successful row has `duration_ms > 0`; both streaming rows have `first_token_ms > 0`; non-streaming rows may keep `first_token_ms IS NULL`.
