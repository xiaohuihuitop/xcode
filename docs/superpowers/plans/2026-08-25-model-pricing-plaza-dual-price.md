# Model Pricing Plaza Dual Price Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在管理员价格页面和模型广场统一展示官方公开价与平台售价，并确保实际扣费继续使用同一平台售价乘套餐倍率。

**Architecture:** 扩展现有价格服务，使基础价格查询携带匹配模型与真实来源；将 `ModelPricingResolver` 改为显式返回解析错误，并让 `PlatformCatalogService` 输出官方价、有效售价和命中规则。管理员端在现有覆盖表上增加平台模型聚合目录与精确售价 Upsert，模型广场以加法字段保持旧接口兼容，前端统一按 USD/1M Token 编辑和展示 Token 价格。

**Tech Stack:** Go 1.26、Gin、PostgreSQL、Wire、Vue 3、TypeScript、Vitest、Tailwind CSS、pnpm 9。

---

## File Map

### Backend pricing core

- Modify `backend/internal/service/pricing_service.go`: record per-model catalog provenance and return the matched catalog model.
- Modify `backend/internal/service/pricing_service_test.go`: provenance, cache, bundled catalog, alias-match regression tests.
- Modify `backend/internal/service/billing_service.go`: expose `ModelPricingLookup`, preserve code-fallback provenance, keep old `GetModelPricing` compatibility.
- Modify `backend/internal/service/billing_service_test.go`: lookup provenance and final multiplier tests.
- Modify `backend/internal/service/model_pricing_resolver.go`: return explicit errors and retain official price separately from effective sale price.
- Modify `backend/internal/service/model_pricing_catalog.go`: validate intervals, batch-load rule snapshots, and upsert exact platform sales.
- Modify `backend/internal/service/model_pricing_catalog_test.go`: priority, batch, explicit zero, partial inheritance, repository error tests.
- Modify `backend/internal/repository/model_pricing_override_repo.go`: atomic PostgreSQL upsert using the existing expression index.

### Backend catalogs and HTTP

- Modify `backend/internal/service/platform_catalog.go`: resolve pricing in a batch and expose official/effective sale metadata.
- Modify `backend/internal/service/platform_catalog_test.go`: dual-price catalog behavior and pricing lookup failure.
- Modify `backend/internal/service/batch_image_settlement.go`: handle the resolver's explicit error return.
- Modify `backend/internal/service/gateway_usage_billing.go`: propagate pricing-rule lookup failures instead of falling back to zero or official pricing.
- Modify `backend/internal/service/openai_gateway_usage.go`: apply the same fail-closed behavior to OpenAI/Codex usage settlement.
- Modify `backend/internal/service/sub2api_pricing_adapter.go`: propagate resolver errors through Runtime pricing quotes.
- Modify `backend/internal/handler/admin/model_pricing_handler.go`: add catalog and platform-sale Upsert endpoints.
- Create `backend/internal/handler/admin/model_pricing_handler_test.go`: handler contract and validation tests.
- Modify `backend/internal/handler/model_plaza_handler.go`: additive official/sale response fields.
- Modify `backend/internal/handler/model_plaza_handler_test.go`: backward compatibility and non-leakage tests.
- Modify `backend/internal/handler/wire.go`, `backend/internal/service/wire.go`, and regenerate `backend/cmd/server/wire_gen.go`: wire platform catalog and platform service into the admin pricing handler.
- Modify `backend/internal/server/routes/admin.go`: register catalog and platform-sale routes before `/:id`.
- Modify `backend/internal/server/middleware/audit_log.go`: allow bounded, non-secret before/after pricing summaries.
- Modify `backend/internal/server/middleware/audit_log_test.go`: verify pricing audit details are retained and bounded.

### Frontend

- Modify `frontend/src/utils/pricing.ts`: lossless USD/Token and USD/1M Token conversion helpers.
- Create `frontend/src/utils/__tests__/pricing.spec.ts`: conversion and explicit-zero tests.
- Modify `frontend/src/api/admin/modelPricing.ts`: administrator catalog and Upsert contracts.
- Modify `frontend/src/api/modelPlaza.ts`: additive official/sale fields.
- Create `frontend/src/components/admin/modelPricing/PricingValueEditor.vue`: inherited/custom/zero value control.
- Create `frontend/src/components/admin/modelPricing/PricingIntervalsEditor.vue`: structured interval editor.
- Create `frontend/src/components/admin/modelPricing/__tests__/PricingValueEditor.spec.ts`: value-state behavior.
- Create `frontend/src/components/admin/modelPricing/__tests__/PricingIntervalsEditor.spec.ts`: interval validation behavior.
- Replace `frontend/src/views/admin/ModelPricingView.vue`: platform-model catalog, source details, price comparison, and advanced rules.
- Create `frontend/src/views/admin/__tests__/ModelPricingView.spec.ts`: catalog rendering and save payload tests.
- Modify `frontend/src/components/modelPlaza/PlatformModelPricingTable.vue`: dual-price responsive table for all billing modes.
- Create `frontend/src/components/modelPlaza/__tests__/PlatformModelPricingTable.spec.ts`: token, request, image, interval, zero, and unavailable rendering.
- Modify `frontend/src/components/modelPlaza/__tests__/ModelPlazaContent.spec.ts`: new API fixture compatibility.
- Modify `frontend/src/i18n/locales/zh/admin/modelPricing.ts`, `frontend/src/i18n/locales/en/admin/modelPricing.ts`, `frontend/src/i18n/locales/zh/dashboard.ts`, and `frontend/src/i18n/locales/en/dashboard.ts`: new labels and error copy.

### Project state

- Modify `docs/memory/当前状态.md`: record the confirmed implementation stage after verification without overwriting unrelated existing status.

## Task 1: Add Official Pricing Provenance

**Files:**
- Modify: `backend/internal/service/pricing_service.go`
- Test: `backend/internal/service/pricing_service_test.go`

- [ ] **Step 1: Write failing provenance tests**

Add tests that construct a `PricingService` with two entries and assert both the matched model and source:

```go
func TestLookupModelPricingReturnsMatchedModelAndSource(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.6-sol": {InputCostPerToken: 5e-6},
		},
		pricingSources: map[string]PricingSourceInfo{
			"gpt-5.6-sol": {Type: PricingSourceRemoteCatalog, Name: "LiteLLM price catalog"},
		},
	}

	lookup := svc.LookupModelPricing("gpt-5.6-sol-preview")
	require.NotNil(t, lookup)
	require.Equal(t, "gpt-5.6-sol", lookup.MatchedModel)
	require.Equal(t, PricingSourceRemoteCatalog, lookup.Source.Type)
}
```

Add separate tests proving fallback-only merged entries are `bundled_catalog` and a local primary file is `cached_remote_catalog` unless its hash equals the bundled file.

- [ ] **Step 2: Run the focused tests and verify they fail**

Run:

```bash
cd backend
go test -tags=unit ./internal/service -run 'TestLookupModelPricing|TestPricingService.*Source' -count=1
```

Expected: build failure because `PricingSourceInfo`, `pricingSources`, and `LookupModelPricing` do not exist.

- [ ] **Step 3: Add provenance types and state**

Add these service types:

```go
type PricingSourceType string

const (
	PricingSourceRemoteCatalog       PricingSourceType = "remote_catalog"
	PricingSourceCachedRemoteCatalog PricingSourceType = "cached_remote_catalog"
	PricingSourceBundledCatalog      PricingSourceType = "bundled_catalog"
	PricingSourceCodeFallback        PricingSourceType = "code_fallback"
	PricingSourceUnavailable         PricingSourceType = "unavailable"
)

type PricingSourceInfo struct {
	Type         PricingSourceType
	Name         string
	URL          string
	MatchedModel string
	UpdatedAt    *time.Time
}

type LiteLLMPricingLookup struct {
	Pricing      *LiteLLMModelPricing
	MatchedModel string
	Source       PricingSourceInfo
}
```

Add `pricingSources map[string]PricingSourceInfo` to `PricingService` and initialize it in `NewPricingService`.

- [ ] **Step 4: Refactor lookup to preserve the matched key**

Implement `LookupModelPricing(modelName string) *LiteLLMPricingLookup` by moving the exact, alias, base-name, family, and OpenAI fallback selection from `GetModelPricing` into a helper that returns both the selected pointer and selected catalog key. Keep compatibility:

```go
func (s *PricingService) GetModelPricing(modelName string) *LiteLLMModelPricing {
	lookup := s.LookupModelPricing(modelName)
	if lookup == nil {
		return nil
	}
	return lookup.Pricing
}
```

When a static OpenAI fallback declared in `pricing_service.go` is selected, return `code_fallback` and the canonical fallback model name.

- [ ] **Step 5: Track sources during download, local load, and fallback merge**

Build a source map alongside parsed pricing data. Downloaded primary entries use `remote_catalog`; data-file entries use `cached_remote_catalog`; fallback-only merged entries use `bundled_catalog`. Compare the data-file SHA-256 with the bundled file SHA-256 and classify identical content as `bundled_catalog`.

Update `pricingData` and `pricingSources` under the same mutex so callers never observe mismatched values.

- [ ] **Step 6: Run provenance and existing pricing tests**

Run:

```bash
cd backend
go test -tags=unit ./internal/service -run 'TestLookupModelPricing|TestPricingService|TestGetModelPricing' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit checkpoint after explicit Git authorization**

```bash
git add backend/internal/service/pricing_service.go backend/internal/service/pricing_service_test.go
git commit -m "feat(pricing): 追踪官方价格来源"
```

## Task 2: Return Official and Effective Sale Pricing Together

**Files:**
- Modify: `backend/internal/service/billing_service.go`
- Modify: `backend/internal/service/model_pricing_resolver.go`
- Modify: `backend/internal/service/model_pricing_catalog.go`
- Test: `backend/internal/service/billing_service_test.go`
- Test: `backend/internal/service/model_pricing_catalog_test.go`

- [ ] **Step 1: Write failing lookup and inheritance tests**

Add a billing lookup test for code fallback:

```go
func TestBillingLookupMarksProgramFallback(t *testing.T) {
	lookup, err := newTestBillingService().LookupModelPricing("glm-5.2")
	require.NoError(t, err)
	require.Equal(t, PricingSourceCodeFallback, lookup.Source.Type)
	require.Equal(t, "glm-5.2", lookup.Source.MatchedModel)
}
```

Add a resolver test proving official and sale prices remain separate:

```go
func TestResolverKeepsOfficialAndEffectiveSalePricing(t *testing.T) {
	official := &ModelPricing{InputPricePerToken: 5e-6, OutputPricePerToken: 30e-6}
	outputSale := 36e-6
	resolved := pricingOverrideToResolved(&ModelPricingOverride{
		Adapter: "codex", ModelPattern: "gpt-5.6-sol",
		OutputPrice: &outputSale, Status: ModelPricingStatusActive,
	}, official, PricingSourceInfo{Type: PricingSourceBundledCatalog})

	require.InDelta(t, 5e-6, resolved.OfficialPricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 30e-6, resolved.OfficialPricing.OutputPricePerToken, 1e-12)
	require.InDelta(t, 5e-6, resolved.BasePricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 36e-6, resolved.BasePricing.OutputPricePerToken, 1e-12)
}
```

- [ ] **Step 2: Run the focused tests and verify they fail**

```bash
cd backend
go test -tags=unit ./internal/service -run 'TestBillingLookup|TestResolverKeepsOfficial' -count=1
```

Expected: build failure for missing lookup and official-pricing fields.

- [ ] **Step 3: Add the billing lookup contract**

Add:

```go
type ModelPricingLookup struct {
	Pricing                *ModelPricing
	Mode                   BillingMode
	DefaultPerRequestPrice float64
	Source                 PricingSourceInfo
}

func (s *BillingService) LookupModelPricing(model string) (*ModelPricingLookup, error)
```

Move existing dynamic-catalog and hardcoded-fallback selection into `LookupModelPricing`; keep `GetModelPricing` as a wrapper returning `lookup.Pricing`. Dynamic results copy the provenance from `PricingService.LookupModelPricing`; hardcoded `fallbackPrices` results use `code_fallback`.

Map image-only catalog entries with `output_cost_per_image` to `BillingModeImage` and `DefaultPerRequestPrice`. Catalog entries with token prices remain `BillingModeToken`, including image models priced by image tokens. Preserve this mode/default price in `ResolvedPricing` so the administrator catalog and model plaza can show image and per-request units without inventing Token prices.

- [ ] **Step 4: Extend resolved pricing without breaking cost calculation**

Add these fields to `ResolvedPricing`:

```go
OfficialPricing *ModelPricing
OfficialSource  PricingSourceInfo
MatchedOverride *ModelPricingOverride
```

`BasePricing` remains the effective platform sale used by cost calculation. Without an override, clone the official price into both `OfficialPricing` and `BasePricing`. With an override, keep `OfficialPricing` unchanged and apply the selected rule only to the `BasePricing` clone.

- [ ] **Step 5: Validate intervals and add a batch rule snapshot**

Convert `domain.ModelPricingInterval` values to `PricingInterval` and call `ValidateIntervals` from `validateModelPricingOverride`. Add a request-scoped snapshot that groups one `repo.List(ctx, "")` result by normalized adapter, then resolves all platform models without repeated database calls.

Use the existing `resolveModelPricingRules` for each identity so exact and wildcard priority remains unchanged.

- [ ] **Step 6: Preserve explicit zero and unavailable semantics**

Do not use numeric zero as a missing-price signal in service DTOs. An override pointer containing zero must replace the official field. If no official price exists and an override leaves a required field unresolved for the selected billing mode, return `ErrModelPricingUnavailable` during cost calculation.

- [ ] **Step 7: Run catalog and billing tests**

```bash
cd backend
go test -tags=unit ./internal/service -run 'TestModelPricingCatalog|TestBillingLookup|TestResolver|TestCalculateCostUnified' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit checkpoint after explicit Git authorization**

```bash
git add backend/internal/service/billing_service.go backend/internal/service/billing_service_test.go backend/internal/service/model_pricing_resolver.go backend/internal/service/model_pricing_catalog.go backend/internal/service/model_pricing_catalog_test.go
git commit -m "feat(pricing): 统一官方价与平台售价解析"
```

## Task 3: Make Pricing Resolution Fail Closed

**Files:**
- Modify: `backend/internal/service/model_pricing_resolver.go`
- Modify: `backend/internal/service/billing_service.go`
- Modify: `backend/internal/service/batch_image_settlement.go`
- Modify: `backend/internal/service/gateway_usage_billing.go`
- Modify: `backend/internal/service/openai_gateway_usage.go`
- Modify: `backend/internal/service/sub2api_pricing_adapter.go`
- Test: `backend/internal/service/billing_service_test.go`
- Test: focused gateway usage tests found by `rg --files backend/internal/service | rg 'gateway.*usage.*test'`

- [ ] **Step 1: Write failing repository-error tests**

Create a repository stub whose `List` returns `errors.New("database unavailable")`. Assert resolver and unified billing return the error instead of official pricing:

```go
resolved, err := resolver.Resolve(context.Background(), PricingInput{
	Adapter: "openai", PlatformCode: "codex", PublicModel: "gpt-5.6-sol", Model: "gpt-5.6-sol",
})
require.Nil(t, resolved)
require.ErrorContains(t, err, "database unavailable")
```

Add a gateway usage regression proving the pricing error prevents a successful zero-cost settlement record.

- [ ] **Step 2: Run the focused tests and verify they fail**

```bash
cd backend
go test -tags=unit ./internal/service -run 'Test.*Pricing.*RepositoryError|Test.*Pricing.*FailClosed' -count=1
```

Expected: failure because `Resolve` currently swallows the repository error.

- [ ] **Step 3: Change resolver signatures to return errors**

Change:

```go
func (r *ModelPricingResolver) Resolve(ctx context.Context, input PricingInput) (*ResolvedPricing, error)
```

Return catalog read errors directly. Return `ErrModelPricingUnavailable` only when neither official pricing nor a complete custom sale can price the request.

Update `BillingService.CalculateCostUnified` to propagate the resolver error before calculating cost.

- [ ] **Step 4: Update all resolver callers**

Update batch image settlement, gateway settlement, OpenAI settlement, Runtime pricing adapter, and platform catalog. Remove any branch that converts a pricing resolution error into `CostBreakdown{ActualCost: 0}` for a successful request.

Gateway helpers should return `(*ResolvedPricing, error)` so rule-store failures cannot be mistaken for “no override”. Preserve ordinary “no override” behavior when resolution succeeds with an official source.

- [ ] **Step 5: Run service tests and compile all callers**

```bash
cd backend
go test -tags=unit ./internal/service -run 'Test.*Pricing|Test.*RecordUsage|Test.*Settlement' -count=1
go test -tags=unit ./internal/handler/... -run '^$'
```

Expected: both commands PASS; the second command proves handler packages compile against the new signature.

- [ ] **Step 6: Commit checkpoint after explicit Git authorization**

```bash
git add backend/internal/service/model_pricing_resolver.go backend/internal/service/billing_service.go backend/internal/service/batch_image_settlement.go backend/internal/service/gateway_usage_billing.go backend/internal/service/openai_gateway_usage.go backend/internal/service/sub2api_pricing_adapter.go backend/internal/service/*pricing*test.go backend/internal/service/*usage*test.go
git commit -m "fix(pricing): 价格解析异常时失败关闭"
```

## Task 4: Add the Administrator Pricing Catalog and Platform Sale Upsert

**Files:**
- Modify: `backend/internal/repository/model_pricing_override_repo.go`
- Modify: `backend/internal/service/model_pricing_catalog.go`
- Modify: `backend/internal/service/platform_catalog.go`
- Modify: `backend/internal/handler/admin/model_pricing_handler.go`
- Create: `backend/internal/handler/admin/model_pricing_handler_test.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/internal/server/middleware/audit_log.go`
- Test: `backend/internal/server/middleware/audit_log_test.go`
- Regenerate: `backend/cmd/server/wire_gen.go`

- [ ] **Step 1: Write failing administrator handler tests**

Add tests for:

```go
GET /model-pricing/catalog
PUT /model-pricing/platform-sale
```

The catalog fixture must include `official_pricing`, `official_source`, `sale_pricing`, `sale_source`, and `override`. The Upsert test must verify platform ID 7 is resolved to adapter `codex` and model `gpt-5.6-sol` before persistence.

- [ ] **Step 2: Run handler tests and verify they fail**

```bash
cd backend
go test -tags=unit ./internal/handler/admin -run 'TestModelPricingHandler.*Catalog|TestModelPricingHandler.*PlatformSale' -count=1
```

Expected: build or route failure because the methods do not exist.

- [ ] **Step 3: Add atomic repository Upsert**

Extend `ModelPricingOverrideRepository` with `Upsert`. Use the existing expression index:

```sql
INSERT INTO model_pricing_overrides (...)
VALUES (...)
ON CONFLICT ((LOWER(adapter)), model_pattern) DO UPDATE SET
  billing_mode = EXCLUDED.billing_mode,
  input_price = EXCLUDED.input_price,
  output_price = EXCLUDED.output_price,
  cache_write_price = EXCLUDED.cache_write_price,
  cache_read_price = EXCLUDED.cache_read_price,
  image_input_price = EXCLUDED.image_input_price,
  image_output_price = EXCLUDED.image_output_price,
  per_request_price = EXCLUDED.per_request_price,
  intervals = EXCLUDED.intervals,
  status = EXCLUDED.status,
  updated_at = NOW()
RETURNING id
```

Reuse the existing interval JSON normalization and validation.

- [ ] **Step 4: Expose catalog and Upsert service methods**

Add `PlatformCatalogService.ListPricingCatalog(ctx)` for active platform models and add a service method that accepts a validated platform plus model pattern, forcing `Adapter = platform.Code` before calling repository Upsert.

Reject models that are not enabled on the selected platform. Preserve advanced CRUD for arbitrary adapter and wildcard rules through the existing endpoints.

- [ ] **Step 5: Add handler DTOs and methods**

Define a catalog response with separate official and sale pricing values. Define the Upsert request with `platform_id`, `model_pattern`, `billing_mode`, nullable price fields, intervals, and status.

Use the existing admin audit middleware and do not add a second audit table. Before Upsert, load the currently effective exact rule and encode a compact, non-secret pricing summary. Add `platform_id`, `model_pattern`, `before_pricing`, and `after_pricing` to the audit-extra allowlist. Keep the existing request body as the full post-change payload and attach the pre-change summary through `middleware.SetAuditExtra`.

Allow up to 512 characters for the two pricing-summary keys while retaining the existing 128-character limit for other audit strings. Add middleware tests proving overlong summaries are truncated and unrelated keys remain rejected.

- [ ] **Step 6: Register routes in safe order**

Register static routes before `/:id`:

```go
pricing.GET("/catalog", h.Admin.ModelPricing.Catalog)
pricing.PUT("/platform-sale", h.Admin.ModelPricing.UpsertPlatformSale)
pricing.GET("/:id", h.Admin.ModelPricing.Get)
```

- [ ] **Step 7: Update Wire providers and regenerate**

Change the handler provider to accept `*service.ModelPricingCatalog`, `*service.PlatformCatalogService`, and `*service.PlatformService`, then run:

```bash
cd backend
go generate ./cmd/server
```

Expected: `cmd/server/wire_gen.go` updates without manual edits.

- [ ] **Step 8: Run administrator tests and build**

```bash
cd backend
go test -tags=unit ./internal/handler/admin ./internal/server/routes -run 'TestModelPricing|TestAdmin' -count=1
go build ./cmd/server
```

Expected: PASS.

- [ ] **Step 9: Commit checkpoint after explicit Git authorization**

```bash
git add backend/internal/repository/model_pricing_override_repo.go backend/internal/service/model_pricing_catalog.go backend/internal/service/platform_catalog.go backend/internal/handler/admin/model_pricing_handler.go backend/internal/handler/admin/model_pricing_handler_test.go backend/internal/handler/wire.go backend/internal/service/wire.go backend/internal/server/routes/admin.go backend/internal/server/middleware/audit_log.go backend/internal/server/middleware/audit_log_test.go backend/cmd/server/wire_gen.go
git commit -m "feat(pricing): 增加平台售价管理目录"
```

## Task 5: Extend the Model Plaza Contract Additively

**Files:**
- Modify: `backend/internal/service/platform_catalog.go`
- Modify: `backend/internal/service/platform_catalog_test.go`
- Modify: `backend/internal/handler/model_plaza_handler.go`
- Modify: `backend/internal/handler/model_plaza_handler_test.go`

- [ ] **Step 1: Write a failing dual-price response test**

Build a platform model with official input `$5/MTok` and custom sale input `$6/MTok`. Assert JSON contains:

```json
{
  "pricing": { "input_price": 0.000006 },
  "official_pricing": { "input_price": 0.000005 },
  "sale_pricing": { "input_price": 0.000006 },
  "sale_pricing_source": "custom"
}
```

Also assert the public body does not contain `source_url`, `fallback_file`, `rule_id`, or administrator data.

- [ ] **Step 2: Run the focused tests and verify they fail**

```bash
cd backend
go test -tags=unit ./internal/handler ./internal/service -run 'TestModelPlaza.*Dual|TestPlatformCatalog.*Pricing' -count=1
```

Expected: missing response fields.

- [ ] **Step 3: Add additive response fields**

Keep `pricing` as the effective platform sale. Add `official_pricing`, `sale_pricing`, and `sale_pricing_source`. Replace `nonZeroFloatPointer` with pointer semantics that preserve explicit zero; unavailable pricing remains `nil`.

Map token intervals, request tiers, image token values, and per-request defaults for both price views.

- [ ] **Step 4: Batch pricing resolution per plaza request**

Collect pricing inputs for all enabled platform models, load one rule snapshot, and resolve all entries. A rule-store error returns an API error instead of a partially incorrect plaza.

- [ ] **Step 5: Run model plaza tests**

```bash
cd backend
go test -tags=unit ./internal/handler ./internal/service -run 'TestModelPlaza|TestPlatformCatalog' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit checkpoint after explicit Git authorization**

```bash
git add backend/internal/service/platform_catalog.go backend/internal/service/platform_catalog_test.go backend/internal/handler/model_plaza_handler.go backend/internal/handler/model_plaza_handler_test.go
git commit -m "feat(model-plaza): 返回官方价与平台售价"
```

## Task 6: Add Frontend Price Units and Structured Editors

**Files:**
- Modify: `frontend/src/utils/pricing.ts`
- Create: `frontend/src/utils/__tests__/pricing.spec.ts`
- Create: `frontend/src/components/admin/modelPricing/PricingValueEditor.vue`
- Create: `frontend/src/components/admin/modelPricing/PricingIntervalsEditor.vue`
- Create: `frontend/src/components/admin/modelPricing/__tests__/PricingValueEditor.spec.ts`
- Create: `frontend/src/components/admin/modelPricing/__tests__/PricingIntervalsEditor.spec.ts`

- [ ] **Step 1: Write failing conversion tests**

```ts
expect(toPerMillionTokens(0.000005)).toBe(5)
expect(fromPerMillionTokens(5)).toBeCloseTo(0.000005, 12)
expect(fromPerMillionTokens(0)).toBe(0)
expect(fromPerMillionTokens(null)).toBeNull()
```

Add component tests proving inherited state emits `null`, explicit-zero state emits `0`, and custom `$5/MTok` emits `0.000005`.

- [ ] **Step 2: Run tests and verify they fail**

```bash
cd frontend
pnpm test:run -- src/utils/__tests__/pricing.spec.ts src/components/admin/modelPricing/__tests__
```

Expected: missing helpers and components.

- [ ] **Step 3: Add unit conversion helpers**

```ts
export const TOKEN_PRICE_SCALE = 1_000_000

export function toPerMillionTokens(value: number | null | undefined): number | null {
  return value == null ? null : value * TOKEN_PRICE_SCALE
}

export function fromPerMillionTokens(value: number | null | undefined): number | null {
  return value == null ? null : value / TOKEN_PRICE_SCALE
}
```

Keep `formatScaled` for display.

- [ ] **Step 4: Implement explicit value states**

`PricingValueEditor.vue` uses a segmented control with `inherit`, `custom`, and `zero`. Only `custom` shows a numeric input. It emits `null`, converted custom value, or `0`; clearing custom input produces a validation error and never silently emits zero.

- [ ] **Step 5: Implement structured interval rows**

`PricingIntervalsEditor.vue` edits `min_tokens`, optional `max_tokens`, label, and applicable prices. Validate non-negative bounds, `max_tokens > min_tokens`, and no overlap after sorting. Emit normalized intervals with `sort_order`.

- [ ] **Step 6: Run component tests**

```bash
cd frontend
pnpm test:run -- src/utils/__tests__/pricing.spec.ts src/components/admin/modelPricing/__tests__
```

Expected: PASS.

- [ ] **Step 7: Commit checkpoint after explicit Git authorization**

```bash
git add frontend/src/utils/pricing.ts frontend/src/utils/__tests__/pricing.spec.ts frontend/src/components/admin/modelPricing
git commit -m "feat(pricing-ui): 增加售价单位与结构化编辑器"
```

## Task 7: Rebuild the Administrator Model Pricing Page

**Files:**
- Modify: `frontend/src/api/admin/modelPricing.ts`
- Replace: `frontend/src/views/admin/ModelPricingView.vue`
- Create: `frontend/src/views/admin/__tests__/ModelPricingView.spec.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/modelPricing.ts`
- Modify: `frontend/src/i18n/locales/en/admin/modelPricing.ts`

- [ ] **Step 1: Write a failing view contract test**

Mock `adminAPI.modelPricing.catalog()` with one inherited and one custom model. Assert the page renders platform, official price, sale price, source badge, and custom/inherited state. Open the editor and assert a `$6/MTok` custom value saves as `0.000006`.

- [ ] **Step 2: Run the view test and verify it fails**

```bash
cd frontend
pnpm test:run -- src/views/admin/__tests__/ModelPricingView.spec.ts
```

Expected: missing catalog API and old override-only rendering.

- [ ] **Step 3: Add administrator API types and methods**

Add `PricingSourceInfo`, `EffectivePricingValue`, `ModelPricingCatalogRow`, and `PlatformSaleInput`. Implement:

```ts
catalog(params?: { platform_id?: number; query?: string }): Promise<ModelPricingCatalogRow[]>
upsertPlatformSale(input: PlatformSaleInput): Promise<ModelPricingCatalogRow>
```

Keep existing list/get/create/update/remove methods for advanced rules.

- [ ] **Step 4: Build the catalog-first page**

Render active platform models in a dense, responsive table with official price, platform sale, delta, source, sale state, and edit command. Expanded details show cache/image/request/interval values and administrator-only provenance.

Use 8px-or-smaller radii, stable column widths, no nested cards, and icon buttons with tooltips. On narrow screens, switch each row to a two-column definition layout without horizontal text overlap.

- [ ] **Step 5: Add the comparison editor and advanced rules section**

The primary editor shows official values read-only and platform sale controls beside them. Billing mode controls which price fields and units are visible. Reuse `PricingIntervalsEditor` instead of raw JSON.

Keep existing wildcard/adapter CRUD in a separate “高级规则” section so current records remain manageable.

- [ ] **Step 6: Add translations and error states**

Add labels for official public price, reference cost, platform sale, inherited/custom/unavailable, source types, matched model, updated time, price delta, and validation errors in Chinese and English.

- [ ] **Step 7: Run administrator frontend checks**

```bash
cd frontend
pnpm test:run -- src/views/admin/__tests__/ModelPricingView.spec.ts src/components/admin/modelPricing/__tests__ src/utils/__tests__/pricing.spec.ts
pnpm typecheck
```

Expected: PASS.

- [ ] **Step 8: Commit checkpoint after explicit Git authorization**

```bash
git add frontend/src/api/admin/modelPricing.ts frontend/src/views/admin/ModelPricingView.vue frontend/src/views/admin/__tests__/ModelPricingView.spec.ts frontend/src/i18n/locales/zh/admin/modelPricing.ts frontend/src/i18n/locales/en/admin/modelPricing.ts
git commit -m "feat(admin): 重构模型价格管理页面"
```

## Task 8: Show Dual Prices in the Model Plaza

**Files:**
- Modify: `frontend/src/api/modelPlaza.ts`
- Modify: `frontend/src/components/modelPlaza/PlatformModelPricingTable.vue`
- Create: `frontend/src/components/modelPlaza/__tests__/PlatformModelPricingTable.spec.ts`
- Modify: `frontend/src/components/modelPlaza/__tests__/ModelPlazaContent.spec.ts`
- Modify: `frontend/src/i18n/locales/zh/dashboard.ts`
- Modify: `frontend/src/i18n/locales/en/dashboard.ts`

- [ ] **Step 1: Write failing dual-price table tests**

Cover these fixtures:

- Token model with different official and sale input/output/cache prices.
- Token model whose sale inherits official.
- Explicit zero sale price, rendered as `$0.00` rather than `-`.
- Missing official price, rendered as “暂无价格”.
- Per-request and image billing modes.
- At least two interval tiers.

- [ ] **Step 2: Run the table test and verify it fails**

```bash
cd frontend
pnpm test:run -- src/components/modelPlaza/__tests__/PlatformModelPricingTable.spec.ts
```

Expected: old table has only one price set and ignores billing mode.

- [ ] **Step 3: Extend API contracts additively**

Keep `pricing` optional for old responses and add optional `official_pricing`, `sale_pricing`, and `sale_pricing_source`. Normalize sale rendering with `sale_pricing ?? pricing` so mixed-version deployments remain readable.

- [ ] **Step 4: Implement the responsive dual-price table**

Desktop columns are model, official public price, and platform sale. Each price cell groups input/output/cache without oversized headings. Mobile uses stacked labeled rows with stable spacing. Render billing-mode-specific units and interval tiers.

Do not expose administrator source details. Use an inherited/custom status badge only when it clarifies why equal prices are shown twice.

- [ ] **Step 5: Update translations and fixtures**

Add “官方公开价（参考成本）”, “平台售价”, “继承官方价”, “暂无价格”, per-million, per-request, per-image, and tier labels. Update `ModelPlazaContent.spec.ts` fixtures to include the additive fields while retaining one legacy-only fixture.

- [ ] **Step 6: Run model plaza tests and frontend build**

```bash
cd frontend
pnpm test:run -- src/components/modelPlaza/__tests__
pnpm typecheck
pnpm build
```

Expected: PASS.

- [ ] **Step 7: Start the frontend and perform visual verification**

Run an available local backend or fixture-compatible frontend server, then inspect `/model-plaza` and `/admin/model-pricing` at desktop and mobile viewports. Verify no text overlap, clipped controls, horizontal page overflow, or unstable row heights.

- [ ] **Step 8: Commit checkpoint after explicit Git authorization**

```bash
git add frontend/src/api/modelPlaza.ts frontend/src/components/modelPlaza frontend/src/i18n/locales/zh/dashboard.ts frontend/src/i18n/locales/en/dashboard.ts
git commit -m "feat(model-plaza): 展示官方价与平台售价"
```

## Task 9: Full Verification and Project State

**Files:**
- Modify: `docs/memory/当前状态.md`

- [ ] **Step 1: Format changed code**

```bash
cd backend
gofmt -w internal/service/pricing_service.go internal/service/billing_service.go internal/service/model_pricing_resolver.go internal/service/model_pricing_catalog.go internal/service/platform_catalog.go internal/service/batch_image_settlement.go internal/service/gateway_usage_billing.go internal/service/openai_gateway_usage.go internal/service/sub2api_pricing_adapter.go internal/repository/model_pricing_override_repo.go internal/handler/admin/model_pricing_handler.go internal/handler/admin/model_pricing_handler_test.go internal/handler/model_plaza_handler.go internal/handler/model_plaza_handler_test.go internal/handler/wire.go internal/server/routes/admin.go cmd/server/wire_gen.go
```

Expected: command exits 0.

- [ ] **Step 2: Run focused backend verification**

```bash
cd backend
go test -tags=unit ./internal/service ./internal/handler ./internal/handler/admin ./internal/server/routes -run 'Test.*Pricing|TestModelPlaza|TestPlatformCatalog' -count=1
go build ./cmd/server
```

Expected: PASS.

- [ ] **Step 3: Run the full backend unit gate**

```bash
cd backend
make test-unit
```

Expected: PASS.

- [ ] **Step 4: Run the frontend gate**

```bash
cd frontend
pnpm test:run
pnpm typecheck
pnpm build
pnpm lint:check
```

Expected: PASS.

- [ ] **Step 5: Check generated and textual diffs**

```bash
git diff --check
git status --short
```

Expected: no whitespace errors; only intended feature files plus pre-existing unrelated user changes are present.

- [ ] **Step 6: Update project memory after verified completion**

Update only the current section of `docs/memory/当前状态.md` with the confirmed dual-price implementation, verification commands, remaining deployment status, and no-migration conclusion. Preserve the existing subscription-window work and unrelated status entries.

- [ ] **Step 7: Perform production-style acceptance only after deployment authorization**

Read the administrator catalog and public model plaza, then use a temporary API Key for one real request. Verify:

```text
model plaza sale price = usage base pricing
usage actual cost = platform sale price × subscription multiplier
subscription usage delta = usage actual cost
```

Delete the temporary API Key after validation and preserve its usage audit record.

- [ ] **Step 8: Final commit after explicit Git authorization**

```bash
git add docs/memory/当前状态.md
git commit -m "docs(status): 记录模型双价格实现状态"
```

Do not tag, push, publish, or deploy without separate user authorization.
