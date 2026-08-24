# Subscription Daily Window Maintenance Design

## Problem

Billing and presentation currently share `ListActiveUserSubscriptions`. The
presentation normalization clears an expired window's usage and start time in
memory. Billing then cannot detect that the persisted window still needs an
atomic reset, so the later cache-backed eligibility check sees yesterday's
usage and rejects the request with `daily usage limit exceeded`.

This also prevents fallback to a second active subscription because the first
candidate appears usable during asset resolution and only fails after it has
already been selected.

## Design

Keep the existing presentation behavior unchanged. Billing candidate methods
will read active subscriptions directly from the repository and return cloned
database snapshots without presentation normalization. The existing
`ValidateAndCheckLimits` and `EnsureWindowMaintenance` flow will then detect an
expired window, conditionally reset it in the database, invalidate billing
caches, reread the subscription, and validate the fresh snapshot before
selection.

No database schema, API contract, dependency, or configuration changes are
required.

## Data Flow

1. API-key billing requests all active subscription candidates.
2. Candidate loading preserves persisted window start times and usage.
3. An expired reusable window returns `needsMaintenance=true`.
4. Existing repository methods reset the window conditionally and invalidate
   the relevant cache.
5. Asset resolution rereads and validates the fresh subscription.
6. Same-day exhausted subscriptions remain unavailable and selection advances
   to the next candidate or balance.

## Edge Cases

- A one-day subscription keeps its one-time daily quota and is not reset.
- A same-day exhausted first subscription is skipped in favor of the second.
- Explicit-plan and allow-all API keys use the same raw candidate semantics.
- Presentation endpoints continue showing normalized current-window values.

## Verification

- Add a regression test proving allow-all candidate loading preserves an
  expired daily window and triggers maintenance.
- Add a resolver regression test for two subscriptions where yesterday's
  exhausted first candidate is maintained and selected successfully.
- Keep the existing same-day exhaustion and one-day-card tests passing.
- Run focused service tests, the complete unit suite, backend build, formatting,
  and diff checks before deployment.

## Release

Build and deploy this as patch version `v1.0.11`. Preserve the current image as
a rollback target, take the existing production backup set, deploy without data
migration, and verify both the affected user and administrator paths.
