# Subscription Daily Window Maintenance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent stale prior-day subscription usage from causing false daily-limit 403 responses.

**Architecture:** Separate billing candidate snapshots from presentation-normalized subscription lists. Reuse the existing atomic maintenance, cache invalidation, and reread flow.

**Tech Stack:** Go 1.26, testify, PostgreSQL repository ports, Redis billing cache

---

### Task 1: Lock the regression with tests

**Files:**
- Modify: `backend/internal/service/subscription_candidates_test.go`
- Modify: `backend/internal/service/api_key_asset_resolver_test.go`

- [ ] Add a test in `subscription_candidates_test.go` with an expired daily
  window and over-limit usage. Assert `ListActiveSubscriptions` preserves both
  fields exactly as returned by the repository.
- [ ] Extend `assetSubscriptionResolverStub` so validation can request
  maintenance and return a refreshed subscription snapshot.
- [ ] Add a two-subscription resolver test where the first candidate initially
  requires maintenance and becomes usable after refresh. Assert maintenance is
  called once and the first candidate is selected.
- [ ] Run:

  ```powershell
  go test -tags=unit ./internal/service -run 'TestListActiveSubscriptionsPreservesExpiredWindowForMaintenance|TestResolveBillingAssetMaintainsExpiredDailyWindowBeforeSelection' -count=1
  ```

  Expected: at least the candidate-loading test fails because the current
  method clears `DailyWindowStart`.

### Task 2: Preserve raw billing candidates

**Files:**
- Modify: `backend/internal/service/subscription_candidates.go`

- [ ] Change `ListActiveSubscriptions` to call
  `userSubRepo.ListActiveByUserID` directly and return
  `cloneUserSubscriptions(subscriptions)` after checking the repository error.
- [ ] Do not change `ListActiveUserSubscriptions`; handlers keep their current
  presentation normalization.
- [ ] Re-run the focused tests and confirm they pass.
- [ ] Run existing boundary tests:

  ```powershell
  go test -tags=unit ./internal/service -run 'TestResolveBillingAsset|TestValidateAndCheckLimits|TestCheckAndResetWindows|TestUserSubscriptionNeedsDailyReset|TestListActiveSubscriptions' -count=1
  ```

### Task 3: Verify and package

**Files:**
- No additional source files expected.

- [ ] Run `gofmt` on the modified Go files.
- [ ] Run `go test -tags=unit ./internal/... ./ent/... ./migrations ./cmd/...`.
- [ ] Run `go build ./cmd/server` from `backend`.
- [ ] Run `git diff --check` and inspect the scoped diff.
- [ ] Build the existing Docker release path with version `v1.0.11` and verify
  the embedded version/revision.

### Task 4: Deploy and production-verify

**Files:**
- No repository file changes expected.

- [ ] Record current container/image/database counters and create the existing
  application, PostgreSQL, Redis, compose, and environment backup set.
- [ ] Tag the current production image as the `v1.0.10` rollback image.
- [ ] Load the verified `v1.0.11` image and recreate only the application
  container; no migration is expected.
- [ ] Confirm health checks and unchanged business-data counters.
- [ ] Send a minimal Responses request with the affected user's key and confirm
  it no longer returns the false daily-limit 403.
- [ ] Confirm the daily window was advanced to the current configured day and
  usage was charged to the selected subscription.
- [ ] Verify an administrator-key request remains successful and inspect logs
  for billing/cache errors.
