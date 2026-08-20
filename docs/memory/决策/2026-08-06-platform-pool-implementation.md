# Platform-only routing implementation checkpoint

- Trigger: implementation of the approved `my2.0` platform/account/adapter design.
- Rule: `platform_id` is the only configurable account ownership. The service derives and validates the account adapter from the selected platform. The client cannot submit a second platform or group source.
- Rule: model requests resolve platform candidates first, then API-key platform grants and endpoint capabilities. New routes never fall back to `api_keys.group_id`, `legacy_group_id`, or `PricingGroupID` for scheduling or pricing.
- Rule: platform routes use adapter/model pricing indexed independently from legacy group pricing. Subscription and balance multipliers remain separate asset decisions.
- Rule: data import/export carries `platform_id`; imports without it are rejected.
- Rule: the user-facing UI calls the concept “Platform”. Account forms expose one platform selector and show the adapter as derived/read-only information.
- Cutover: `backend/cmd/platform-rebuild` is dry-run by default. `--apply` is required after a verified backup; it disables old platform routing and detaches platform accounts/keys without deleting users, keys, balances, plans, subscriptions, payment records, or usage history.
- Validation: run focused Go unit tests for productcore/service/handler, the full `internal/repository` package, frontend component tests, `npm run typecheck`, `npm run lint:check`, `npm run build`, and `git diff --check`. Repository tests must keep raw SQL dialect-aware and usage-log argument assertions aligned with the insert column order.
