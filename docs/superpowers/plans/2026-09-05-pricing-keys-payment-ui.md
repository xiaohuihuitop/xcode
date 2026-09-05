# Pricing, Keys, and Payment UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve pricing presentation, expose a copyable API endpoint, and make recharge currency and configured limits explicit.

**Architecture:** Reuse the existing pricing modes, endpoint popover, payment currency helpers, and payment config endpoint. Keep billing, persistence, migrations, and public checkout contracts unchanged; implement the behavior through focused Vue component changes with regression tests.

**Tech Stack:** Vue 3, TypeScript, Vue I18n, Tailwind CSS, Vitest, Vue Test Utils.

---

### Task 1: Model plaza custom-price presentation

**Files:**
- Modify: `frontend/src/components/modelPlaza/PlatformModelPricingTable.vue`
- Test: `frontend/src/components/modelPlaza/__tests__/PlatformModelPricingTable.spec.ts`

- [x] Add failing tests proving the custom badge is absent and only differing comparable platform price values use the danger text class.
- [x] Run the component test and confirm the new assertions fail for the missing behavior.
- [x] Add a field-level comparison helper and apply the danger class to changed platform values; remove the custom badge.
- [x] Run the component test and confirm it passes.

### Task 2: Administrator image pricing mode

**Files:**
- Modify: `frontend/src/views/admin/ModelPricingView.vue`
- Test: `frontend/src/views/admin/__tests__/ModelPricingView.spec.ts`

- [x] Add a failing test that switches a token-priced model to image mode, enters a per-image price, and expects an image-mode save payload without token prices.
- [x] Run the test and confirm it fails for the missing or incorrect interaction.
- [x] Make the existing mutually exclusive mode selector and image price editor explicit enough to satisfy the test, clearing incompatible draft prices when the mode changes if required.
- [x] Run the administrator pricing test and confirm it passes.

### Task 3: Always-visible API endpoint

**Files:**
- Modify: `frontend/src/views/user/KeysView.vue`
- Modify: `frontend/src/components/keys/EndpointPopover.vue`
- Test: `frontend/src/views/user/__tests__/KeysView.spec.ts`
- Test: `frontend/src/components/keys/__tests__/EndpointPopover.spec.ts`

- [x] Add failing tests for the current-origin `/v1` fallback, configured endpoint precedence, and preserved custom endpoints.
- [x] Run both endpoint tests and confirm the fallback assertion fails.
- [x] Normalize the default endpoint and render the existing endpoint popover unconditionally.
- [x] Run both endpoint tests and confirm they pass.

### Task 4: Recharge currency, title, and configured limits

**Files:**
- Modify: `frontend/src/components/payment/AmountInput.vue`
- Modify: `frontend/src/views/user/PaymentView.vue`
- Modify: `frontend/src/i18n/locales/zh/misc.ts`
- Modify: `frontend/src/i18n/locales/en/misc.ts`
- Test: `frontend/src/components/payment/__tests__/AmountInput.spec.ts`
- Test: `frontend/src/views/user/__tests__/PaymentView.spec.ts`

- [x] Add failing AmountInput tests for currency-aware prefixes, emphasized Chinese title markup, and formatted minimum/maximum helper text.
- [x] Run the AmountInput test and confirm the new assertions fail.
- [x] Add explicit currency, minimum, and maximum display props to AmountInput using the existing currency formatter.
- [x] Run the AmountInput test and confirm it passes.
- [x] Add failing PaymentView tests proving `/payment/config` limits are passed to the input and participate in submit validation while provider limits remain enforced.
- [x] Run PaymentView tests and confirm the new assertions fail.
- [x] Load the existing payment config beside checkout info, compute effective boundaries, and pass the selected currency and configured limits to AmountInput.
- [x] Run PaymentView tests and confirm they pass.

### Task 5: Full verification and visual QA

**Files:**
- Modify: `docs/memory/当前状态.md`

- [x] Run all directly affected Vitest files.
- [x] Run the complete frontend Vitest suite.
- [x] Run `pnpm typecheck`, `pnpm lint:check`, and `pnpm build` from `frontend`.
- [x] Start the local Vite server and inspect model plaza, model pricing, API keys, and payment pages at desktop and mobile sizes.
- [x] Check for overlap, horizontal overflow, unreadable helper text, missing focus affordances, and dark-mode contrast.
- [x] Review the final diff against every design requirement and run `git diff --check`.
- [x] Update project memory with verified results and leave commit, push, tag, release, and deployment untouched pending separate authorization.
