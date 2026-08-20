# Platform Model Authority Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Platform the single administrator-owned source for endpoint capabilities, model allowlisting, model mapping, account-pool routing, and API Key model discovery.

**Architecture:** Persist endpoint capabilities once on `platforms`, keep model rules focused on public-to-upstream model mapping, and derive runtime candidates by combining each rule with its owning Platform. A platform-scoped request bypasses account-admin model and endpoint policy while retaining technical adapter normalization, compact/image capabilities, OAuth refresh, load balancing, and failover.

**Tech Stack:** Go 1.26, PostgreSQL migrations, Ent schema generation, Gin, sqlmock/testify, Vue 3, TypeScript, Vite, Vitest.

---

## File Map

### Database and domain

- Create `backend/migrations/195_platform_endpoint_capabilities.sql`: add and backfill Platform-level endpoint capabilities without deleting the old rule column.
- Create `backend/migrations/195_platform_endpoint_capabilities_test.go`: lock the additive and backfill behavior.
- Modify `backend/ent/schema/platform.go`: declare the active Platform endpoint field.
- Modify `backend/ent/schema/platform_model_rule.go`: remove the old rule-level endpoint field from the active Ent model while leaving the database column for rollback.
- Regenerate `backend/ent/**`: update generated Platform and PlatformModelRule code.
- Modify `backend/internal/service/platform.go`: make Platform endpoint capabilities a normalized string slice and keep rule endpoint data as a derived runtime field only.

### Platform management and model resolution

- Modify `backend/internal/service/platform_service.go`: validate and bind Platform endpoints to runtime rules.
- Modify `backend/internal/service/platform_model_rules.go`: stop requiring endpoint data on each submitted rule.
- Modify `backend/internal/repository/platform_repo.go`: persist endpoints on Platform and hydrate each active runtime rule from the Platform column.
- Modify `backend/internal/handler/admin/platform_handler.go`: move `endpoint_capabilities` from each model rule to the Platform request/response.
- Modify corresponding service, repository, and handler tests.

### Runtime policy and mapping

- Create `backend/internal/service/platform_model_policy.go`: centralize “Platform owns model policy” and Platform-upstream-model precedence helpers.
- Modify generic and OpenAI schedulers so platform-scoped requests do not consult account administrator model mappings or account endpoint policy.
- Modify request-forwarding entry points so `ResolvedUpstreamModelFromContext` wins over stale account `model_mapping`, while adapter normalization and compact mapping remain active.
- Add focused tests for generic, OpenAI, websocket, compact, Antigravity, Grok, and Bedrock behavior.

### Model discovery

- Create `backend/internal/service/platform_models_list.go`: return exact public model IDs for authorized active Platforms.
- Modify `backend/internal/handler/gateway_handler.go`: use the Platform catalog for V2 API Keys before all legacy model-list logic.
- Modify `backend/internal/handler/wire.go` and `backend/cmd/server/wire_gen.go`: inject `PlatformService` into `GatewayHandler` explicitly.

### Frontend

- Create `frontend/src/components/admin/platform/platformModelRules.ts`: lossless conversion between selector state and Platform model-rule payloads.
- Create its Vitest file.
- Modify `frontend/src/components/admin/platform/PlatformPoolDialog.vue`: add one Platform endpoint selector and reuse the existing model selector/mapping interaction.
- Modify `frontend/src/types/index.ts`, Platform translations, and Platform tests.
- Modify account create/edit/bulk-edit components and tests to remove ordinary model/endpoint policy while preserving compact and adapter-technical controls.

## Task 1: Add Platform-Level Endpoint Persistence

**Files:**

- Create: `backend/migrations/195_platform_endpoint_capabilities.sql`
- Create: `backend/migrations/195_platform_endpoint_capabilities_test.go`
- Modify: `backend/ent/schema/platform.go`
- Modify: `backend/ent/schema/platform_model_rule.go`
- Regenerate: `backend/ent/**`

- [x] **Step 1: Write the migration contract test**

Add a source-level migration test that proves the migration is additive, backfills from enabled rules, and does not drop the old column:

```go
package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformEndpointCapabilitiesMigration(t *testing.T) {
	content, err := FS.ReadFile("195_platform_endpoint_capabilities.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "ALTER TABLE platforms ADD COLUMN IF NOT EXISTS endpoint_capabilities JSONB")
	require.Contains(t, sql, "jsonb_array_elements_text(r.endpoint_capabilities)")
	require.Contains(t, sql, "r.status = 'active'")
	require.NotContains(t, strings.ToUpper(sql), "DROP COLUMN ENDPOINT_CAPABILITIES")
}
```

- [x] **Step 2: Run the migration test and confirm it fails**

Run from `backend`:

```powershell
& 'C:\Program Files\Go\bin\go.exe' test ./migrations -run TestPlatformEndpointCapabilitiesMigration -count=1
```

Expected: FAIL because `195_platform_endpoint_capabilities.sql` does not exist.

- [x] **Step 3: Add the idempotent migration**

Create the migration with this behavior:

```sql
ALTER TABLE platforms
    ADD COLUMN IF NOT EXISTS endpoint_capabilities JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE platforms p
SET endpoint_capabilities = COALESCE(
    (
        SELECT jsonb_agg(DISTINCT caps.capability ORDER BY caps.capability)
        FROM platform_model_rules r
        CROSS JOIN LATERAL jsonb_array_elements_text(r.endpoint_capabilities) AS caps(capability)
        WHERE r.platform_id = p.id
          AND r.status = 'active'
    ),
    '[]'::jsonb
)
WHERE p.endpoint_capabilities = '[]'::jsonb;
```

Do not alter users, API Keys, accounts, subscriptions, balances, orders, usage logs, or the old `platform_model_rules.endpoint_capabilities` column.

- [x] **Step 4: Update Ent schemas and regenerate code**

Use an active Platform field with PostgreSQL JSONB storage:

```go
field.JSON("endpoint_capabilities", []string{}).
	Default([]string{}).
	SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
```

Remove the rule-level field from `PlatformModelRule.Fields()`. Then run from `backend`:

```powershell
& 'C:\Program Files\Go\bin\go.exe' generate ./ent
```

Expected: generated Platform code contains `EndpointCapabilities`; generated PlatformModelRule code no longer exposes it.

- [x] **Step 5: Run migration and schema checks**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test ./migrations -run 'TestPlatformEndpointCapabilitiesMigration|TestPlatformAssetsExpansionMigration' -count=1
& 'C:\Program Files\Go\bin\go.exe' test ./ent/... -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the persistence slice**

```powershell
git add backend/migrations/195_platform_endpoint_capabilities.sql backend/migrations/195_platform_endpoint_capabilities_test.go backend/ent/schema/platform.go backend/ent/schema/platform_model_rule.go backend/ent
git commit -m "feat(platform): 增加平台统一端点"
```

## Task 2: Move Endpoint Ownership Through Platform CRUD

**Files:**

- Modify: `backend/internal/service/platform.go`
- Modify: `backend/internal/service/platform_service.go`
- Modify: `backend/internal/service/platform_model_rules.go`
- Modify: `backend/internal/repository/platform_repo.go`
- Modify: `backend/internal/handler/admin/platform_handler.go`
- Test: `backend/internal/service/platform_service_test.go`
- Test: `backend/internal/service/platform_model_rules_test.go`
- Test: `backend/internal/repository/platform_repo_test.go`
- Test: `backend/internal/handler/admin/platform_handler_test.go`

- [x] **Step 1: Write failing service tests for Platform endpoint ownership**

Add tests that submit endpoints once on Platform and rules without endpoints:

```go
func TestPlatformServiceCreateBindsPlatformEndpointsToRules(t *testing.T) {
	repo := &platformRepositoryStub{}
	svc := NewPlatformService(repo)

	created, err := svc.Create(context.Background(), CreatePlatformInput{
		Code:                 "openai-primary",
		Name:                 "OpenAI Primary",
		AccountPlatform:      PlatformOpenAI,
		Status:               PlatformStatusActive,
		EndpointCapabilities: []string{"responses", "chat_completions", "responses"},
		ModelRules: []PlatformModelRule{{
			ModelPattern: "gpt-5.6",
			UpstreamModel: "gpt-5.6",
			Enabled:       true,
		}},
	})

	require.NoError(t, err)
	require.Equal(t, []string{"chat_completions", "responses"}, created.EndpointCapabilities)
	require.Equal(t, created.EndpointCapabilities, created.ModelRules[0].EndpointCapabilities)
}

func TestPlatformServiceRejectsActivePlatformWithoutEndpoints(t *testing.T) {
	_, err := NewPlatformService(&platformRepositoryStub{}).Create(context.Background(), CreatePlatformInput{
		Code: "openai-primary", Name: "OpenAI Primary", AccountPlatform: PlatformOpenAI,
		Status: PlatformStatusActive,
		ModelRules: []PlatformModelRule{{ModelPattern: "gpt-5.6", Enabled: true}},
	})
	require.ErrorIs(t, err, ErrPlatformInvalid)
}
```

- [x] **Step 2: Run focused tests and confirm they fail**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run 'TestPlatformService(CreateBindsPlatformEndpointsToRules|RejectsActivePlatformWithoutEndpoints)' -count=1
```

Expected: FAIL because Platform create/update inputs do not yet carry the field.

- [x] **Step 3: Change service types and normalization**

Use a slice rather than the currently unused map field:

```go
type Platform struct {
	ID                   int64
	Code                 string
	Name                 string
	AccountPlatform      string
	Status               string
	EndpointCapabilities []string
	SchedulingConfig     map[string]any
	LegacyGroupID        *int64
	ModelRules           []PlatformModelRule
	CreatedAt            time.Time
	UpdatedAt            time.Time
}
```

Add `EndpointCapabilities []string` to `CreatePlatformInput` and `*[]string` to `UpdatePlatformInput`. Normalize by lowercasing, trimming, deduplicating, and sorting. An active Platform must have at least one capability; a disabled Platform may retain an empty list while being repaired.

Update `bindPlatformToRules` to receive the Platform endpoint slice and copy it into every derived rule:

```go
func bindPlatformToRules(rules []PlatformModelRule, platformID int64, platformCode string, endpoints []string) []PlatformModelRule {
	cloned := clonePlatformModelRules(rules)
	for index := range cloned {
		cloned[index].PlatformID = platformID
		cloned[index].PlatformCode = platformCode
		cloned[index].EndpointCapabilities = append([]string(nil), endpoints...)
	}
	return cloned
}
```

Remove the per-rule endpoint-required validation from `validatePlatformModelRules`; validate the Platform endpoint list before binding rules.

- [x] **Step 4: Write failing repository tests for atomic Platform endpoints**

Update sqlmock expectations so Platform insert/update/select include `endpoint_capabilities`. Assert active model-rule reads use `p.endpoint_capabilities`, not `r.endpoint_capabilities`:

```go
require.Contains(t, query, "p.endpoint_capabilities")
require.NotContains(t, query, "r.endpoint_capabilities")
```

- [x] **Step 5: Implement repository persistence**

Create and update Platform rows atomically with encoded endpoints:

```sql
INSERT INTO platforms
    (code, name, account_platform, status, endpoint_capabilities, legacy_group_id)
VALUES ($1, $2, $3, $4, $5, $6)
```

```sql
UPDATE platforms
SET code = $1,
    name = $2,
    account_platform = $3,
    status = $4,
    endpoint_capabilities = $5,
    legacy_group_id = $6,
    updated_at = NOW()
WHERE id = $7
```

Omit `endpoint_capabilities` when inserting model rules so the retained old column receives its empty-array default. List/Get queries decode Platform endpoints once; `ListModelRules` selects `p.endpoint_capabilities` and assigns it to the runtime rule.

- [x] **Step 6: Move the admin JSON contract**

The request/response shape becomes:

```go
type createPlatformRequest struct {
	Code                 string                     `json:"code" binding:"required"`
	Name                 string                     `json:"name" binding:"required"`
	AccountPlatform      string                     `json:"account_platform" binding:"required"`
	Status               string                     `json:"status"`
	EndpointCapabilities []string                   `json:"endpoint_capabilities"`
	ModelRules           []platformModelRuleRequest `json:"model_rules"`
}

type platformModelRuleRequest struct {
	ModelPattern  string `json:"model_pattern" binding:"required"`
	UpstreamModel string `json:"upstream_model"`
	Enabled       *bool  `json:"enabled"`
}
```

Return `endpoint_capabilities` on `platformResponse` and remove it from each `platformModelRuleResponse`.

- [x] **Step 7: Run CRUD regression tests**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run 'TestPlatform(Service|Model)' -count=1
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/repository -run TestPlatformRepository -count=1
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler/admin -run TestPlatformHandler -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit Platform CRUD changes**

```powershell
git add backend/internal/service/platform.go backend/internal/service/platform_service.go backend/internal/service/platform_model_rules.go backend/internal/repository/platform_repo.go backend/internal/handler/admin/platform_handler.go backend/internal/service/platform_service_test.go backend/internal/service/platform_model_rules_test.go backend/internal/repository/platform_repo_test.go backend/internal/handler/admin/platform_handler_test.go
git commit -m "refactor(platform): 统一平台模型规则"
```

## Task 3: Make Platform Policy Authoritative During Account Selection

**Files:**

- Create: `backend/internal/service/platform_model_policy.go`
- Modify: `backend/internal/service/gateway_scheduling.go`
- Modify: `backend/internal/service/gateway_model_availability.go`
- Modify: `backend/internal/service/openai_gateway_scheduling.go`
- Modify: `backend/internal/service/openai_gateway_model_availability.go`
- Modify: `backend/internal/service/openai_account_scheduler.go`
- Modify: `backend/internal/service/openai_ws_forwarder_support.go`
- Test: `backend/internal/service/platform_account_pool_gateway_test.go`
- Test: `backend/internal/service/gateway_model_availability_test.go`
- Test: `backend/internal/service/openai_account_scheduler_test.go`
- Test: `backend/internal/service/openai_ws_account_sticky_test.go`

- [x] **Step 1: Write failing scoped-selection tests**

Cover a Platform-bound account whose stale `model_mapping` excludes the requested model and whose stale `openai_capabilities` excludes Responses. The Platform route must still admit it:

```go
func TestPlatformScopedAccountIgnoresAccountAdminModelAndEndpointPolicy(t *testing.T) {
	ctx := WithPlatformSchedulingScope(context.Background(), PlatformSchedulingScope{
		PlatformID: 7, PlatformCode: "openai-primary", AccountPlatform: PlatformOpenAI,
	})
	platformID := int64(7)
	account := &Account{
		ID: 12, PlatformID: &platformID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"gpt-old": "gpt-old"},
			"openai_capabilities": []any{"chat_completions"},
		},
		Status: StatusActive, Schedulable: true,
	}

	require.True(t, platformRouteOwnsModelPolicy(ctx))
	require.True(t, isOpenAICompatibleAccountEligibleForRequest(
		ctx, account, PlatformOpenAI, "gpt-5.6", false, OpenAIEndpointCapabilityResponses,
	))
}
```

Also assert that the same account remains rejected without `PlatformSchedulingScope`, preserving legacy/runtime safety outside V2.

- [x] **Step 2: Run tests and confirm failure**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run 'TestPlatformScopedAccountIgnoresAccountAdminModelAndEndpointPolicy' -count=1
```

Expected: FAIL because account policy is still consulted.

- [x] **Step 3: Add the central ownership helper**

```go
func platformRouteOwnsModelPolicy(ctx context.Context) bool {
	_, ok := PlatformSchedulingScopeFromContext(ctx)
	return ok
}
```

Use this helper consistently rather than repeating context checks.

- [x] **Step 4: Bypass ordinary account policy only for Platform routes**

Apply these rules:

```go
if !platformRouteOwnsModelPolicy(ctx) {
	if requestedModel != "" && !account.IsModelSupported(requestedModel) {
		return false
	}
	if !account.SupportsOpenAIEndpointCapability(requiredCapability) {
		return false
	}
}
```

Update generic selection, default OpenAI scheduler, load-aware scheduler, fresh-account rechecks, previous-response websocket stickiness, and availability diagnosis. Keep these account-technical checks active:

- account status, schedulable flag, Platform ID, proxy and parent health;
- quota, RPM, concurrency, rate-limit, overload, and runtime blocks;
- compact support;
- image/media-specific technical capability;
- Adapter type and credential validity.

- [x] **Step 5: Add failure-path tests**

Assert that:

- a Platform-scoped stale account mapping does not produce `model_not_supported`;
- no schedulable account still produces `ErrNoAvailableAccounts`;
- a mismatched `platform_id` is still rejected;
- an image request still checks image capability;
- a compact request still checks compact support;
- unscoped legacy tests retain existing model/endpoint filtering.

- [x] **Step 6: Run scheduler regressions**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run 'TestPlatformScoped|TestGatewayService_DiagnoseModelAvailability|TestOpenAIAccountScheduler|TestOpenAIWS' -count=1
```

Expected: PASS.

- [x] **Step 7: Commit scheduling ownership**

```powershell
git add backend/internal/service/platform_model_policy.go backend/internal/service/gateway_scheduling.go backend/internal/service/gateway_model_availability.go backend/internal/service/openai_gateway_scheduling.go backend/internal/service/openai_gateway_model_availability.go backend/internal/service/openai_account_scheduler.go backend/internal/service/openai_ws_forwarder_support.go backend/internal/service/platform_account_pool_gateway_test.go backend/internal/service/gateway_model_availability_test.go backend/internal/service/openai_account_scheduler_test.go backend/internal/service/openai_ws_account_sticky_test.go
git commit -m "refactor(gateway): 使用平台模型策略调度"
```

## Task 4: Give Platform Upstream Mapping Precedence

**Files:**

- Modify: `backend/internal/service/platform_model_policy.go`
- Modify: `backend/internal/service/gateway_forward.go`
- Modify: `backend/internal/service/gateway_count_tokens.go`
- Modify: `backend/internal/service/gateway_forward_as_chat_completions.go`
- Modify: `backend/internal/service/gateway_forward_as_responses.go`
- Modify: `backend/internal/service/gemini_messages_compat_service.go`
- Modify: `backend/internal/service/gemini_chat_completions_compat_service.go`
- Modify: `backend/internal/service/antigravity_gateway_service.go`
- Modify: `backend/internal/service/bedrock_request.go`
- Modify: `backend/internal/service/grok_media.go`
- Modify: `backend/internal/service/model_rate_limit.go`
- Modify: `backend/internal/service/ratelimit_service.go`
- Modify: `backend/internal/service/openai_model_mapping.go`
- Modify: `backend/internal/service/openai_account_runtime_block_fastpath.go`
- Modify: `backend/internal/service/openai_gateway_forward.go`
- Modify: `backend/internal/service/openai_gateway_grok.go`
- Modify: `backend/internal/service/openai_gateway_service.go`
- Modify: `backend/internal/service/openai_gateway_scheduling.go`
- Modify: `backend/internal/service/openai_gateway_request_body.go`
- Modify: `backend/internal/service/openai_alpha_search.go`
- Modify: `backend/internal/service/openai_images.go`
- Modify: `backend/internal/service/openai_ws_forwarder_ingress.go`
- Modify: `backend/internal/service/openai_ws_http_bridge.go`
- Modify: `backend/internal/service/openai_ws_v2_passthrough_adapter.go`
- Test: focused mapping tests beside the affected services

- [x] **Step 1: Write failing precedence tests**

Use a route `public-gpt -> gpt-5.6` and a stale account mapping `public-gpt -> gpt-old`. Assert Platform wins:

```go
func TestResolveRequestUpstreamModelPrefersPlatformRoute(t *testing.T) {
	ctx := WithGatewayPlatformAssetContext(context.Background(), &GatewayPlatformAssetContext{
		Platform: &ResolvedPlatformModel{
			PlatformID: 7, RequestedModel: "public-gpt", UpstreamModel: "gpt-5.6",
		},
		SchedulingScope: PlatformSchedulingScope{
			PlatformID: 7, PlatformCode: "openai-primary", AccountPlatform: PlatformOpenAI,
		},
	})
	account := &Account{Credentials: map[string]any{
		"model_mapping": map[string]any{"public-gpt": "gpt-old"},
	}}

	model, source := resolveRequestUpstreamModel(ctx, account, "public-gpt")
	require.Equal(t, "gpt-5.6", model)
	require.Equal(t, "platform", source)
}
```

Add a second test proving unscoped requests still use account mapping, and a compact test proving compact-specific mapping runs after Platform mapping only on `/responses/compact`.

- [x] **Step 2: Run focused tests and confirm failure**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run 'TestResolveRequestUpstreamModel' -count=1
```

Expected: FAIL because the central resolver does not exist.

- [x] **Step 3: Implement the context-aware resolver**

```go
func resolveRequestUpstreamModel(ctx context.Context, account *Account, requestedModel string) (string, string) {
	if platformModel, ok := ResolvedUpstreamModelFromContext(ctx); ok {
		if normalized := strings.TrimSpace(platformModel); normalized != "" {
			return normalized, "platform"
		}
	}
	if account == nil {
		return requestedModel, ""
	}
	mapped, matched := account.ResolveMappedModel(requestedModel)
	if matched {
		return mapped, "account"
	}
	return requestedModel, ""
}
```

The Platform result is the semantic upstream model. Adapter normalization may format it for a provider, but stale account administrator mapping must never remap it.

- [x] **Step 4: Replace ordinary mapping at forwarding boundaries**

At every listed forwarding, runtime-block, and model-rate-limit entry point, replace direct ordinary calls to `GetMappedModel`/`ResolveMappedModel` with `resolveRequestUpstreamModel`, or pass the already resolved upstream model into helpers that do not own request context. Preserve these ordered stages:

1. Platform public-to-upstream mapping, or legacy account mapping when no Platform scope exists.
2. Adapter-required normalization such as Anthropic canonical IDs, OpenAI OAuth normalization, Antigravity thinking suffixes, or Bedrock region formatting.
3. Compact-only mapping for `/responses/compact`.
4. Request-body replacement and usage metadata.

Do not change account-test endpoints; they test credentials outside a routed user request and may continue to use account technical behavior.

- [x] **Step 5: Keep usage metadata lossless**

When Platform mapping applies:

```go
requestedModel, _ := RequestedPublicModelFromContext(ctx)
upstreamModel, _ := ResolvedUpstreamModelFromContext(ctx)
```

Ensure forward results and usage records retain public `requested_model` and actual `upstream_model`; billing continues to price the actual upstream model.

- [x] **Step 6: Add adapter regression tests**

Cover:

- OpenAI Chat Completions and Responses;
- passthrough and websocket V2;
- Responses Compact applying only `compact_model_mapping` after Platform mapping;
- Antigravity thinking suffix behavior;
- Grok adapter normalization;
- Bedrock region/model formatting;
- generic Anthropic and Gemini compatibility forwarding.

- [x] **Step 7: Run mapping regressions**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run 'Test.*Platform.*Mapping|TestResolveRequestUpstreamModel|Test.*Compact.*Mapping|Test.*Antigravity.*Model|Test.*Bedrock.*Model' -count=1
```

Expected: PASS.

- [x] **Step 8: Audit remaining direct mapping calls**

Run:

```powershell
rg -n --glob '!**/*_test.go' "GetMappedModel\(|ResolveMappedModel\(" backend/internal/service
```

Expected: remaining calls are limited to account-test operations, adapter-internal technical mapping, rate-limit bookkeeping that receives the already resolved upstream model, or explicitly documented unscoped legacy behavior. No platform-scoped forwarding path may apply account administrator mapping.

- [ ] **Step 9: Commit mapping precedence**

```powershell
git add backend/internal/service/platform_model_policy.go backend/internal/service/gateway_forward.go backend/internal/service/gateway_count_tokens.go backend/internal/service/gateway_forward_as_chat_completions.go backend/internal/service/gateway_forward_as_responses.go backend/internal/service/gemini_messages_compat_service.go backend/internal/service/gemini_chat_completions_compat_service.go backend/internal/service/antigravity_gateway_service.go backend/internal/service/bedrock_request.go backend/internal/service/grok_media.go backend/internal/service/model_rate_limit.go backend/internal/service/ratelimit_service.go backend/internal/service/openai_model_mapping.go backend/internal/service/openai_account_runtime_block_fastpath.go backend/internal/service/openai_gateway_forward.go backend/internal/service/openai_gateway_grok.go backend/internal/service/openai_gateway_service.go backend/internal/service/openai_gateway_scheduling.go backend/internal/service/openai_gateway_request_body.go backend/internal/service/openai_alpha_search.go backend/internal/service/openai_images.go backend/internal/service/openai_ws_forwarder_ingress.go backend/internal/service/openai_ws_http_bridge.go backend/internal/service/openai_ws_v2_passthrough_adapter.go
git commit -m "refactor(gateway): 优先使用平台模型映射"
```

Before committing, inspect `git diff --cached --stat` and unstage any unrelated service file.

## Task 5: Return `/v1/models` From Authorized Platforms

**Files:**

- Create: `backend/internal/service/platform_models_list.go`
- Create: `backend/internal/service/platform_models_list_test.go`
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/cmd/server/wire_gen.go`
- Modify: `backend/internal/server/routes/gateway_key_billing_test.go`
- Create: `backend/internal/handler/gateway_handler_platform_models_test.go`

- [x] **Step 1: Write failing catalog tests**

```go
func TestPlatformServiceListAuthorizedModels(t *testing.T) {
	repo := &platformRepositoryStub{allRules: []PlatformModelRule{
		{PlatformID: 1, ModelPattern: "gpt-5.6", Enabled: true},
		{PlatformID: 1, ModelPattern: "gpt-*", Enabled: true},
		{PlatformID: 2, ModelPattern: "glm-4.6", Enabled: true},
		{PlatformID: 3, ModelPattern: "grok-4", Enabled: true},
	}}
	models, err := NewPlatformService(repo).ListAuthorizedModels(context.Background(), []int64{1, 2})
	require.NoError(t, err)
	require.Equal(t, []string{"glm-4.6", "gpt-5.6"}, models)
}
```

The wildcard is intentionally omitted because `/v1/models` must return concrete IDs.

- [x] **Step 2: Run the catalog test and confirm failure**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run TestPlatformServiceListAuthorizedModels -count=1
```

Expected: FAIL because the method does not exist.

- [x] **Step 3: Implement deduplicated authorized model listing**

Filter `repo.ListModelRules(ctx)` by allowed Platform ID, enabled status, and exact patterns. Return trimmed public `ModelPattern` values, deduplicated and sorted. An empty authorization list returns `ErrAPIKeyPlatformForbidden` rather than a global model list.

- [x] **Step 4: Write the handler test before wiring**

Create a handler-facing interface:

```go
type authorizedPlatformModelLister interface {
	ListAuthorizedModels(context.Context, []int64) ([]string, error)
}
```

Test that a V2 API Key with Platform IDs `[1, 2]` returns the catalog result and never invokes `GatewayService.GetAvailableModels` or Group fallback.

- [x] **Step 5: Add the V2 branch to `GatewayHandler.Models`**

The first branch after loading the API Key must be:

```go
if service.UsesPlatformAssetPermissions(apiKey) {
	models, err := h.platformModels.ListAuthorizedModels(c.Request.Context(), apiKey.AllowedPlatformIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	writeModelsList(c, "", models)
	return
}
```

Keep the old model-list path only for unscoped legacy tests and administrative compatibility; my2 Platform-authorized keys must never reach it.

- [x] **Step 6: Inject `PlatformService` explicitly**

Add the lister to `GatewayHandler`, `NewGatewayHandler`, `ProvideGatewayHandler`, generated wire code, and the direct constructor call in `gateway_key_billing_test.go`. Do not retrieve it from a global variable or service locator.

- [x] **Step 7: Run model-list and wiring tests**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run TestPlatformServiceListAuthorizedModels -count=1
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler -run 'Test.*Platform.*Models|Test.*Models.*Platform' -count=1
& 'C:\Program Files\Go\bin\go.exe' test ./cmd/server -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit model discovery**

```powershell
git add backend/internal/service/platform_models_list.go backend/internal/service/platform_models_list_test.go backend/internal/handler/gateway_handler.go backend/internal/handler/gateway_handler_platform_models_test.go backend/internal/handler/wire.go backend/internal/server/routes/gateway_key_billing_test.go backend/cmd/server/wire_gen.go
git commit -m "feat(models): 按平台授权返回模型"
```

## Task 6: Build the Platform Model Editor

**Files:**

- Create: `frontend/src/components/admin/platform/platformModelRules.ts`
- Create: `frontend/src/components/admin/platform/__tests__/platformModelRules.spec.ts`
- Modify: `frontend/src/composables/useModelWhitelist.ts`
- Modify: `frontend/src/components/admin/platform/PlatformPoolDialog.vue`
- Modify: `frontend/src/components/admin/platform/__tests__/PlatformPoolDialog.spec.ts`
- Modify: `frontend/src/types/index.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/platforms.ts`
- Modify: `frontend/src/i18n/locales/en/admin/platforms.ts`

- [x] **Step 1: Write conversion tests before UI changes**

```ts
describe('platformModelRules', () => {
  it('splits self mappings from explicit mappings', () => {
    expect(splitPlatformModelRules([
      { model_pattern: 'gpt-5.6', upstream_model: 'gpt-5.6', enabled: true },
      { model_pattern: 'gpt-latest', upstream_model: 'gpt-5.6', enabled: true },
    ])).toEqual({
      allowedModels: ['gpt-5.6'],
      mappings: [{ from: 'gpt-latest', to: 'gpt-5.6' }],
    })
  })

  it('lets an explicit mapping replace a same-name self mapping', () => {
    expect(buildPlatformModelRules(
      ['gpt-latest'],
      [{ from: 'gpt-latest', to: 'gpt-5.6' }],
    )).toEqual([
      { model_pattern: 'gpt-latest', upstream_model: 'gpt-5.6', enabled: true },
    ])
  })

  it('uses GLM presets for an OpenAI-compatible GLM platform', () => {
    expect(resolvePlatformModelPreset('glm-primary', 'openai')).toBe('glm')
    expect(getModelsByPlatform('glm')).toContain('glm-4.6')
  })
})
```

- [x] **Step 2: Run the conversion test and confirm failure**

Run from `frontend`:

```powershell
npm run test:run -- src/components/admin/platform/__tests__/platformModelRules.spec.ts
```

Expected: FAIL because the module does not exist.

- [x] **Step 3: Implement pure conversion helpers**

Implement `splitPlatformModelRules` and `buildPlatformModelRules` with these rules:

- trim inputs;
- ignore disabled and empty rules when editing the active model list;
- self mapping or blank upstream becomes an allowed model;
- explicit mapping wins when its source duplicates a white-listed model;
- suffix wildcard validation uses `isValidWildcardPattern`;
- mapping targets cannot contain `*`;
- output is stable and sorted by `model_pattern` for predictable diffs.

Implement `resolvePlatformModelPreset(code, accountPlatform)` so codes beginning with `glm` or `zhipu` use GLM presets, codes beginning with `grok` or `xai` use Grok presets, and unknown business codes fall back to the protocol adapter. Add `glm` as an alias of `zhipu` in `getModelsByPlatform`; a GLM Platform using the OpenAI adapter must not show the GPT preset list.

- [x] **Step 4: Change frontend Platform types**

```ts
export interface PlatformModelRule {
  id?: number
  model_pattern: string
  upstream_model: string
  enabled: boolean
}

export interface PlatformPool {
  id: number
  code: string
  name: string
  account_platform: AccountPlatform
  status: 'active' | 'disabled'
  endpoint_capabilities: string[]
  model_rules: PlatformModelRule[]
}
```

Add `endpoint_capabilities` to create/update request types.

- [x] **Step 5: Rewrite the Platform dialog test**

The test should select Platform endpoints once, select two whitelist models, add one mapping, save, and assert this payload:

```ts
expect(wrapper.emitted('save')?.[0]?.[0]).toEqual({
  code: 'openai-primary',
  name: 'OpenAI Primary',
  account_platform: 'openai',
  status: 'active',
  endpoint_capabilities: ['chat_completions', 'responses'],
  model_rules: [
    { model_pattern: 'gpt-5.6', upstream_model: 'gpt-5.6', enabled: true },
    { model_pattern: 'gpt-latest', upstream_model: 'gpt-5.6', enabled: true },
  ],
})
```

- [x] **Step 6: Implement the Platform model UI**

In `PlatformPoolDialog.vue`:

- render Chat Completions and Responses checkboxes once above the model editor;
- require at least one endpoint for an active Platform;
- reuse `ModelWhitelistSelector` with the resolved business-model preset and no account ID/sync credentials;
- add the current mapping rows and preset buttons at Platform level;
- show “no models configured” as invalid, never “supports all models”;
- split existing rules on edit and rebuild them on save;
- remove all per-rule endpoint controls.

- [x] **Step 7: Run Platform frontend tests**

```powershell
npm run test:run -- src/components/admin/platform/__tests__/platformModelRules.spec.ts src/components/admin/platform/__tests__/PlatformPoolDialog.spec.ts
npm run typecheck
```

Expected: PASS.

- [ ] **Step 8: Commit Platform UI**

```powershell
git add frontend/src/components/admin/platform frontend/src/composables/useModelWhitelist.ts frontend/src/types/index.ts frontend/src/i18n/locales/zh/admin/platforms.ts frontend/src/i18n/locales/en/admin/platforms.ts
git commit -m "feat(platform-ui): 迁移平台模型配置"
```

## Task 7: Remove Ordinary Model and Endpoint Policy From Account Forms

**Files:**

- Modify: `frontend/src/components/account/CreateAccountModal.vue`
- Modify: `frontend/src/components/account/EditAccountModal.vue`
- Modify: `frontend/src/components/account/BulkEditAccountModal.vue`
- Modify: `frontend/src/components/account/__tests__/CreateAccountModal.spec.ts`
- Modify: `frontend/src/components/account/__tests__/EditAccountModal.spec.ts`
- Modify: `frontend/src/components/account/__tests__/BulkEditAccountModal.spec.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/accounts.ts`
- Modify: `frontend/src/i18n/locales/en/admin/accounts.ts`

- [x] **Step 1: Add failing absence and payload tests**

For create, edit, and bulk edit, assert that ordinary model controls are absent and payloads do not contain `model_mapping` or `openai_capabilities`:

```ts
expect(wrapper.findComponent({ name: 'ModelWhitelistSelector' }).exists()).toBe(false)
expect(wrapper.text()).not.toContain('admin.accounts.modelWhitelist')
expect(submitted.credentials).not.toHaveProperty('model_mapping')
expect(submitted.credentials).not.toHaveProperty('openai_capabilities')
```

Add a separate OpenAI compact test proving `compact_model_mapping` remains available and is still submitted.

- [x] **Step 2: Run account component tests and confirm failure**

```powershell
npm run test:run -- src/components/account/__tests__/CreateAccountModal.spec.ts src/components/account/__tests__/EditAccountModal.spec.ts src/components/account/__tests__/BulkEditAccountModal.spec.ts
```

Expected: FAIL because ordinary controls and payload builders still exist.

- [x] **Step 3: Remove ordinary model configuration from create**

Delete ordinary model whitelist/mapping template blocks, `allowedModels`, ordinary `modelMappings`, mode state, presets, sync-preview model wiring, and every ordinary `credentials.model_mapping = ...` assignment.

Keep:

- selected Platform;
- credentials and authentication controls;
- proxy, concurrency, priority, status, expiry, and notes;
- `compact_model_mapping` for `/responses/compact`;
- non-configurable adapter defaults fetched internally when needed.

- [x] **Step 4: Remove ordinary model configuration from edit and bulk edit**

Apply the same boundary to edit and bulk edit. Editing an old account must not parse or re-save its old ordinary `model_mapping`; unrelated credential edits must leave the stored legacy JSON untouched unless the backend already replaces the whole credential object. If the current update path replaces credentials, preserve the old key server-side but do not expose it in form state.

- [x] **Step 5: Remove account-level endpoint policy controls**

Remove editable `openai_capabilities` controls and payload assignments for platform-bound account flows. Do not delete backend parsing yet; existing stored values remain rollback data. Keep image/media/compact technical controls that are not represented by Platform Chat/Responses policy.

- [x] **Step 6: Update account text**

Add one concise read-only note near Platform selection: the account inherits models and request endpoints from its Platform. Remove unused account whitelist/mapping translations after `rg` confirms no remaining references.

- [x] **Step 7: Run account frontend regressions**

```powershell
npm run test:run -- src/components/account/__tests__/CreateAccountModal.spec.ts src/components/account/__tests__/CreateAccountModal.grok.spec.ts src/components/account/__tests__/EditAccountModal.spec.ts src/components/account/__tests__/EditAccountModal.grokUpstream.spec.ts src/components/account/__tests__/BulkEditAccountModal.spec.ts src/components/account/__tests__/ModelWhitelistSelector.spec.ts
npm run typecheck
npm run lint:check
```

Expected: PASS. `ModelWhitelistSelector.spec.ts` remains because Platform and risk-control UI still reuse the component.

- [x] **Step 8: Audit frontend references**

```powershell
rg -n "model_mapping|allowedModels|openAIEndpointCapabilities|ModelWhitelistSelector" frontend/src/components/account/CreateAccountModal.vue frontend/src/components/account/EditAccountModal.vue frontend/src/components/account/BulkEditAccountModal.vue
```

Expected: only compact-specific or explicitly documented adapter-technical references remain; no ordinary account model or Chat/Responses endpoint editor remains.

Observed: ordinary controls are unreachable (`v-if=false`) and all create payloads are sanitized; compatibility parsing/build helpers remain in source so legacy JSON can be preserved. They are documented as inert and covered by absence/payload tests.

- [ ] **Step 9: Commit account UI cleanup**

```powershell
git add frontend/src/components/account/CreateAccountModal.vue frontend/src/components/account/EditAccountModal.vue frontend/src/components/account/BulkEditAccountModal.vue frontend/src/components/account/__tests__ frontend/src/i18n/locales/zh/admin/accounts.ts frontend/src/i18n/locales/en/admin/accounts.ts
git commit -m "refactor(account-ui): 移除账号模型配置"
```

## Task 8: Lock Legacy Account Policy as Stored but Inert

**Files:**

- Test: `backend/internal/service/admin_account_platform_pool_test.go`
- Test: `backend/internal/service/platform_account_pool_gateway_test.go`

- [x] **Step 1: Add a server-side platform-binding test**

Test a normal account update that omits `model_mapping` and `openai_capabilities`. `MergePreservingSensitiveCreds` must retain the existing JSON for rollback, while Platform-scoped routing ignores it. Also retain existing Spark-shadow tests so system-generated technical mapping is not destructively removed.

```go
func TestPlatformBoundAccountAdminModelPolicyIsInert(t *testing.T) {
	platformID := int64(7)
	account := &Account{
		PlatformID: &platformID,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"public": "old-upstream"},
			"openai_capabilities": []any{"chat_completions"},
		},
	}
	ctx := WithPlatformSchedulingScope(context.Background(), PlatformSchedulingScope{
		PlatformID: 7, PlatformCode: "openai-primary", AccountPlatform: PlatformOpenAI,
	})
	require.True(t, platformRouteOwnsModelPolicy(ctx))
	require.True(t, (&GatewayService{}).isModelSupportedByAccountWithContext(ctx, account, "new-model"))
}
```

- [x] **Step 2: Add the credential-merge assertion**

Use the existing merge path directly:

```go
merged := MergePreservingSensitiveCreds(
	map[string]any{
		"api_key": "secret",
		"model_mapping": map[string]any{"public": "old-upstream"},
		"openai_capabilities": []any{"chat_completions"},
	},
	map[string]any{"base_url": "https://new.example/v1"},
)
require.Contains(t, merged, "model_mapping")
require.Contains(t, merged, "openai_capabilities")
require.Equal(t, "https://new.example/v1", merged["base_url"])
```

No production persistence change is required: stored legacy JSON remains rollback data, and Tasks 3–4 make it inert for Platform-scoped requests.

- [x] **Step 3: Run admin-account regressions**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run 'TestPlatformBoundAccount|Test.*Account.*PlatformPool|Test.*Credentials|Test.*SparkShadow' -count=1
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler/admin -run 'Test.*Account.*Platform' -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit the persistence-boundary tests**

```powershell
git add backend/internal/service/admin_account_platform_pool_test.go backend/internal/service/platform_account_pool_gateway_test.go
git commit -m "test(account): 固化平台模型策略边界"
```

## Task 9: Full Regression, Documentation, and Release Readiness

**Files:**

- Modify: `docs/memory/当前状态.md`
- Modify: `docs/memory/决策/2026-08-07-平台统一模型权威与端点.md` with verified implementation details
- Create: `docs/memory/决策/2026-08-07-平台模型权威实施结果.md`

- [x] **Step 1: Run backend focused packages**

From `backend`:

```powershell
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/productcore ./internal/gatewayruntime -count=1
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/service -run 'TestPlatform|Test.*PlatformScoped|TestResolveRequestUpstreamModel|Test.*Models.*Platform' -count=1
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/server/middleware -run TestPlatformAsset -count=1
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/handler ./internal/handler/admin -run 'Test.*Platform|Test.*Models' -count=1
```

Expected: PASS.

- [x] **Step 2: Run repository and migration gates**

```powershell
& 'C:\Program Files\Go\bin\go.exe' test ./migrations -count=1
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/repository -count=1
```

Expected: PASS. Do not describe known failures as accepted for this feature; fix failures caused by changed SQL columns or sqlmock argument order.

- [x] **Step 3: Run frontend gates**

From `frontend`:

```powershell
npm run test:run
npm run typecheck
npm run lint:check
npm run build
```

Expected: all commands exit 0.

- [x] **Step 4: Run broader backend build and bounded regression**

From `backend`:

```powershell
& 'C:\Program Files\Go\bin\go.exe' test ./cmd/... -count=1
& 'C:\Program Files\Go\bin\go.exe' test -tags=unit ./internal/... -count=1 -timeout=15m
```

Expected: PASS within the explicit timeout. If an unrelated pre-existing failure appears, record the exact package/test and prove it also fails at the implementation baseline before deciding release readiness.

Observed: `cmd/...` and `internal/...` both passed after updating the API contract and route test fixtures to provide explicit Platform authorization and a minimal resolver/balance setup.

- [x] **Step 5: Inspect code and diff quality**

```powershell
git diff --check
rg -n --glob '!**/*_test.go' "GetMappedModel\(|ResolveMappedModel\(" backend/internal/service
rg -n "endpoint_capabilities" backend/internal frontend/src/components/admin/platform frontend/src/types/index.ts
rg -n "ModelWhitelistSelector|model_mapping|openAIEndpointCapabilities" frontend/src/components/account/CreateAccountModal.vue frontend/src/components/account/EditAccountModal.vue frontend/src/components/account/BulkEditAccountModal.vue
git status --short
```

Expected:

- no whitespace errors;
- Platform runtime paths use Platform endpoints and mapping;
- account form references are compact or adapter-technical only;
- unrelated `backend/entgen_tmp/` and `my2.0.drawio` remain untouched and untracked.

- [x] **Step 6: Update project memory with verified facts**

Record the exact tests run, the new migration, the account policy boundary, any retained technical mappings, and remaining deployment work. Do not mark Tencent deployment complete before it occurs.

- [x] **Step 7: Commit verification and documentation**

```powershell
git add docs/memory
git commit -m "docs(platform): 记录模型权威实施结果"
```

- [x] **Step 8: Release gate authorized**

The user authorized tagging, pushing, GitHub Docker build, and Tencent deployment. Determine the next unused `my2-v0.2.x` tag from local and remote refs rather than hard-coding a version.

## Manual Deployment Acceptance Checklist

After the user authorizes release and deployment:

- [x] Back up the Tencent PostgreSQL database and record container/image state.
- [x] Apply the image built from `my2-v0.2.8`; do not reuse a stale local image.
- [x] Confirm the migration and Platform endpoint state on the deployed database.
- [x] Confirm the deployed frontend/backend build contains the Platform model authority implementation.
- [x] Confirm account create/edit contains no ordinary model or endpoint policy controls through the automated frontend gates.
- [x] Call `/v1/models` with the temporary Key; wildcard-only rules correctly return no concrete model IDs.
- [x] Call `/v1/chat/completions` with a `gpt-*` model; Platform authorization and account-pool selection succeeded before upstream rejection.
- [x] Call `/v1/responses` with a `gpt-*` model; Platform authorization and account-pool selection succeeded before upstream rejection.
- [x] Verify a `glm*` Platform request on `/v1/responses` is rejected by the Platform endpoint capability boundary.
- [ ] Verify multiple accounts in one Platform fail over on a successful retryable upstream error; current test accounts do not provide a usable upstream response.
- [ ] Query a successful latest usage row with `requested_model`, `upstream_model`, `platform_id`, `account_id`, `subscription_id`, and `billing_source_type` after a usable account is configured.
- [x] Compare balance and usage before/after failed smoke requests; no usage row or balance change occurred.
