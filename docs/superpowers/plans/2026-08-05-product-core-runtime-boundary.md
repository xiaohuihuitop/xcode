# 产品核心与网关运行时隔离 Implementation Plan

> **已完成的历史计划：** 本文件用于追溯第一阶段边界实现，不是当前执行入口。后续继续
> 保留 ProductCore/GatewayRuntime 分层，但不得恢复本文的 Legacy Key 或旧 Group 运行时
> 回退；当前设计以 `docs/superpowers/specs/2026-08-06-platform-pool-account-adapter-design.md`
> 为准。

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不修改 Sub2API 账号调度、OAuth 刷新、协议适配或上游重试的情况下，为 My2 V2 平台资产链路建立自有产品核心和稳定的网关运行时契约。

**Architecture:** `internal/productcore` 保存 API Key 授权、平台能力和资产选择的纯领域决策；`internal/gatewayruntime` 保存不可变的调度意图。`internal/service` 以适配器把现有 Platform、订阅、余额和旧定价上下文接到这两个纯模块，并与现有 `GatewayPlatformAssetContext` 双写，保证所有原有网关服务继续按当前方式调度账号、刷新 OAuth、转换协议和记录用量。

**Tech Stack:** Go 1.26、Gin、现有 Ent/PostgreSQL/Redis、Go testing、Testify；本阶段不改数据库、Ent、Vue、Docker 或前端依赖。

---

## 命令工作目录

除非命令另有说明，所有 `go test` 和 `go build` 均在 `backend` 目录执行。PowerShell 示例：

```powershell
Push-Location backend
go test -tags=unit ./internal/productcore -count=1
Pop-Location
```

每个任务完成后的提交只在用户单独授权提交时执行；本计划中的提交命令用于给执行阶段提供原子提交边界。

## 范围与禁止项

本计划只覆盖无行为变化的第一阶段。以下文件不得进行业务算法修改：

- `backend/internal/service/gateway_scheduling.go`
- `backend/internal/service/openai_gateway_scheduling.go`
- `backend/internal/service/openai_account_scheduler.go`
- `backend/internal/service/account_credentials_persistence.go`
- `backend/internal/service/openai_gateway_*.go`
- `backend/internal/service/antigravity_gateway_*.go`

不得新增数据库迁移、修改 Ent schema、修改 Vue 页面、拆 Docker 容器或将账号选择逻辑移出当前 Sub2API 服务。

## 文件与职责

| 文件 | 职责 |
| --- | --- |
| `backend/internal/productcore/types.go` | 产品核心的授权、平台、资产和决策纯数据类型。 |
| `backend/internal/productcore/errors.go` | 不依赖 HTTP 的内部领域错误。 |
| `backend/internal/productcore/ports.go` | 平台解析与资产选择端口。 |
| `backend/internal/productcore/authorizer.go` | 先平台、再授权/能力、最后资产的纯决策算法。 |
| `backend/internal/productcore/authorizer_test.go` | 平台权限、端点能力、套餐和余额顺序测试。 |
| `backend/internal/gatewayruntime/intent.go` | 不可变 `DispatchIntent` 与 context 存取。 |
| `backend/internal/gatewayruntime/intent_test.go` | 深拷贝和空资产语义测试。 |
| `backend/internal/service/product_core_adapter.go` | 把现有 API Key、订阅服务和平台解析器适配为产品核心端口。 |
| `backend/internal/service/product_core_adapter_test.go` | 适配器保留套餐实例、倍率和现有错误语义。 |
| `backend/internal/service/gateway_runtime_bridge.go` | `Decision`、`DispatchIntent` 与现有网关上下文间的双向转换。 |
| `backend/internal/service/gateway_runtime_bridge_test.go` | 平台范围、公开/上游模型和旧定价引用的桥接测试。 |
| `backend/internal/service/platform_asset_request.go` | 保留兼容门面，但委托给产品核心适配器和运行时桥。 |
| `backend/internal/service/platform_asset_request_test.go` | 保留既有 V2 行为，并覆盖门面委托。 |
| `backend/internal/server/middleware/platform_asset_auth.go` | HTTP 读取模型、调用门面、写订阅 Gin 上下文，不组合业务规则。 |
| `backend/internal/server/middleware/platform_asset_auth_test.go` | HTTP 入口保持请求体、错误和订阅上下文的测试。 |
| `backend/internal/server/routes/gateway.go` | 构造一个产品核心门面，再注册 JSON 与 Google 中间件。 |
| `docs/superpowers/specs/2026-08-05-product-core-runtime-boundary-design.md` | 已确认的长期边界。 |
| `docs/memory/决策/2026-08-05-product-core-runtime-boundary.md` | 同步官方时必须遵守的长期决策。 |

### Task 1: 建立纯产品核心决策

**Files:**
- Create: `backend/internal/productcore/types.go`
- Create: `backend/internal/productcore/errors.go`
- Create: `backend/internal/productcore/ports.go`
- Create: `backend/internal/productcore/authorizer.go`
- Create: `backend/internal/productcore/authorizer_test.go`

- [x] **Step 1: 写失败测试，锁定既有授权顺序**

```go
func TestAuthorizerSelectsPlatformBeforeBillingAsset(t *testing.T) {
    subscriptionID := int64(21)
    platforms := platformCatalogStub{platform: &Platform{
        ID: 3, AccountPlatform: "openai",
        EndpointCapabilities: []string{"chat_completions"},
    }}
    assets := &assetSelectorStub{asset: &BillingAsset{
        Source: "subscription", SubscriptionID: &subscriptionID, RateMultiplier: 0.5,
    }}
    authorizer := NewAuthorizer(platforms, assets)

    decision, err := authorizer.Resolve(context.Background(), AccessGrant{
        KeyID: 10, UserID: 7, PlatformIDs: []int64{3}, AllowBalance: true,
    }, Request{Model: "gpt-4o", EndpointCapability: "chat_completions"})

    require.NoError(t, err)
    require.Equal(t, int64(3), decision.Platform.ID)
    require.Equal(t, "subscription", decision.BillingAsset.Source)
    require.True(t, assets.called)
}

func TestAuthorizerRejectsUnapprovedPlatformBeforeSelectingAsset(t *testing.T) {
    platforms := platformCatalogStub{platform: &Platform{ID: 3, AccountPlatform: "openai"}}
    assets := &assetSelectorStub{}
    _, err := NewAuthorizer(platforms, assets).Resolve(context.Background(), AccessGrant{
        PlatformIDs: []int64{4},
    }, Request{Model: "gpt-4o", EndpointCapability: "chat_completions"})

    require.ErrorIs(t, err, ErrPlatformForbidden)
    require.False(t, assets.called)
}

func TestAuthorizerRejectsUnsupportedEndpointBeforeSelectingAsset(t *testing.T) {
    platforms := platformCatalogStub{platform: &Platform{
        ID: 3, EndpointCapabilities: []string{"chat_completions"},
    }}
    assets := &assetSelectorStub{}
    _, err := NewAuthorizer(platforms, assets).Resolve(context.Background(), AccessGrant{
        PlatformIDs: []int64{3},
    }, Request{Model: "gpt-4o", EndpointCapability: "responses"})

    require.ErrorIs(t, err, ErrEndpointUnsupported)
    require.False(t, assets.called)
}

func TestAuthorizerAllowsUnclassifiedEndpoint(t *testing.T) {
    platforms := platformCatalogStub{platform: &Platform{
        ID: 3, EndpointCapabilities: []string{"chat_completions"},
    }}
    assets := &assetSelectorStub{asset: &BillingAsset{Source: "balance", RateMultiplier: 1}}

    decision, err := NewAuthorizer(platforms, assets).Resolve(context.Background(), AccessGrant{
        PlatformIDs: []int64{3}, AllowBalance: true,
    }, Request{Model: "gpt-4o"})

    require.NoError(t, err)
    require.Equal(t, "balance", decision.BillingAsset.Source)
    require.True(t, assets.called)
}

type platformCatalogStub struct {
    platform *Platform
    err      error
}

func (s platformCatalogStub) ResolveModel(context.Context, string) (*Platform, error) {
    return s.platform, s.err
}

type assetSelectorStub struct {
    asset  *BillingAsset
    err    error
    called bool
}

func (s *assetSelectorStub) Select(context.Context, AccessGrant, bool) (*BillingAsset, error) {
    s.called = true
    return s.asset, s.err
}
```

- [x] **Step 2: 运行测试，确认实现前失败**

Run: `go test -tags=unit ./internal/productcore -run TestAuthorizer -count=1`

Expected: FAIL，提示 `internal/productcore` 或 `NewAuthorizer` 尚不存在。

- [x] **Step 3: 实现无框架依赖的类型、端口和算法**

```go
// types.go
type AccessGrant struct {
    KeyID                  int64
    UserID                 int64
    Balance                float64
    PlatformIDs            []int64
    SubscriptionPlanIDs    []int64
    AllowBalance           bool
}

type Request struct {
    Model              string
    EndpointCapability string
    SkipBilling        bool
}

type Platform struct {
    ID                   int64
    Code                 string
    AccountPlatform      string
    RequestedModel       string
    UpstreamModel        string
    EndpointCapabilities []string
    LegacyPricingGroupID *int64
}

type BillingAsset struct {
    Source         string
    SubscriptionID *int64
    PlanID         *int64
    RateMultiplier float64
}

type Decision struct {
    Platform     Platform
    BillingAsset *BillingAsset
}
```

```go
// ports.go
type PlatformCatalog interface {
    ResolveModel(context.Context, string) (*Platform, error)
}

type AssetSelector interface {
    Select(context.Context, AccessGrant, bool) (*BillingAsset, error)
}
```

```go
// errors.go
var (
    ErrModelUnavailable       = errors.New("product core model is unavailable")
    ErrPlatformForbidden      = errors.New("api key is not authorized for platform")
    ErrEndpointUnsupported    = errors.New("platform does not support endpoint")
    ErrNoBillingAsset         = errors.New("no usable billing asset")
    ErrInsufficientBalance    = errors.New("insufficient balance")
    ErrDailyLimitExceeded     = errors.New("daily limit exceeded")
    ErrWeeklyLimitExceeded    = errors.New("weekly limit exceeded")
    ErrMonthlyLimitExceeded   = errors.New("monthly limit exceeded")
)
```

```go
// authorizer.go
type Authorizer struct {
    platforms PlatformCatalog
    assets    AssetSelector
}

func NewAuthorizer(platforms PlatformCatalog, assets AssetSelector) *Authorizer {
    return &Authorizer{platforms: platforms, assets: assets}
}
```

```go
// authorizer.go
func (a *Authorizer) Resolve(ctx context.Context, grant AccessGrant, request Request) (*Decision, error) {
    if a == nil || a.platforms == nil {
        return nil, ErrModelUnavailable
    }
    platform, err := a.platforms.ResolveModel(ctx, request.Model)
    if err != nil {
        return nil, err
    }
    if platform == nil || !allowsPlatform(grant.PlatformIDs, platform.ID) {
        return nil, ErrPlatformForbidden
    }
    if !supportsEndpoint(platform.EndpointCapabilities, request.EndpointCapability) {
        return nil, ErrEndpointUnsupported
    }
    if a.assets == nil {
        return nil, ErrNoBillingAsset
    }
    asset, err := a.assets.Select(ctx, grant, request.SkipBilling)
    if err != nil {
        return nil, err
    }
    if asset == nil && !request.SkipBilling {
        return nil, ErrNoBillingAsset
    }
    return &Decision{Platform: clonePlatform(*platform), BillingAsset: cloneBillingAsset(asset)}, nil
}
```

```go
func allowsPlatform(allowed []int64, platformID int64) bool {
    for _, candidate := range allowed {
        if candidate == platformID {
            return true
        }
    }
    return false
}

func supportsEndpoint(configured []string, requested string) bool {
    requested = strings.TrimSpace(requested)
    if requested == "" || len(configured) == 0 {
        return true
    }
    for _, capability := range configured {
        if strings.EqualFold(strings.TrimSpace(capability), requested) {
            return true
        }
    }
    return false
}

func clonePlatform(value Platform) Platform {
    value.EndpointCapabilities = append([]string(nil), value.EndpointCapabilities...)
    value.LegacyPricingGroupID = cloneInt64(value.LegacyPricingGroupID)
    return value
}

func cloneBillingAsset(value *BillingAsset) *BillingAsset {
    if value == nil {
        return nil
    }
    cloned := *value
    cloned.SubscriptionID = cloneInt64(value.SubscriptionID)
    cloned.PlanID = cloneInt64(value.PlanID)
    return &cloned
}

func cloneInt64(value *int64) *int64 {
    if value == nil {
        return nil
    }
    cloned := *value
    return &cloned
}
```

`EndpointCapability` 由服务适配器调用现有 `billingEndpointCapability(endpoint)` 生成，`productcore` 不能再复制一套 HTTP 路径判断。请求的能力为空或平台能力列表为空时，必须保持当前“未配置即放行”语义；只有两者均非空且不匹配时才返回 `ErrEndpointUnsupported`。`SkipBilling=true` 时 `AssetSelector` 可以返回 `nil, nil`，`Decision.BillingAsset` 也必须保持 `nil`。`types.go` 需要导入 `context`，`errors.go` 需要导入 `errors`，`authorizer.go` 需要导入 `context` 和 `strings`。

- [x] **Step 4: 运行单元测试，确认通过**

Run: `go test -tags=unit ./internal/productcore -count=1`

Expected: PASS。

- [ ] **Step 5: 提交纯产品核心**

```powershell
git add backend/internal/productcore
git commit -m "refactor(核心): 抽取平台资产决策契约"
```

### Task 2: 建立不可变网关运行时意图

**Files:**
- Create: `backend/internal/gatewayruntime/intent.go`
- Create: `backend/internal/gatewayruntime/intent_test.go`

- [x] **Step 1: 写失败测试，防止下游改写同一请求的决策**

```go
func TestDispatchIntentContextRoundTripsIndependentCopies(t *testing.T) {
    intent := &DispatchIntent{
        Platform: productcore.Platform{ID: 3, EndpointCapabilities: []string{"responses"}},
        BillingAsset: &productcore.BillingAsset{Source: "balance", RateMultiplier: 1.25},
    }
    ctx := WithDispatchIntent(context.Background(), intent)
    intent.Platform.EndpointCapabilities[0] = "mutated-before-read"

    first, ok := DispatchIntentFromContext(ctx)
    require.True(t, ok)
    require.Equal(t, "responses", first.Platform.EndpointCapabilities[0])

    first.Platform.EndpointCapabilities[0] = "mutated-after-read"
    second, ok := DispatchIntentFromContext(ctx)
    require.True(t, ok)
    require.Equal(t, "responses", second.Platform.EndpointCapabilities[0])
}

func TestDispatchIntentAllowsNilBillingAsset(t *testing.T) {
    ctx := WithDispatchIntent(context.Background(), &DispatchIntent{
        Platform: productcore.Platform{ID: 3},
    })
    got, ok := DispatchIntentFromContext(ctx)
    require.True(t, ok)
    require.Nil(t, got.BillingAsset)
}
```

- [x] **Step 2: 运行测试，确认实现前失败**

Run: `go test -tags=unit ./internal/gatewayruntime -count=1`

Expected: FAIL，提示包或 `DispatchIntent` 尚不存在。

- [x] **Step 3: 实现 context 契约**

```go
package gatewayruntime

type DispatchIntent struct {
    Platform     productcore.Platform
    BillingAsset *productcore.BillingAsset
}

type dispatchIntentContextKey struct{}

func WithDispatchIntent(ctx context.Context, intent *DispatchIntent) context.Context {
    if ctx == nil || intent == nil {
        return ctx
    }
    return context.WithValue(ctx, dispatchIntentContextKey{}, cloneIntent(intent))
}

func DispatchIntentFromContext(ctx context.Context) (*DispatchIntent, bool) {
    if ctx == nil {
        return nil, false
    }
    intent, ok := ctx.Value(dispatchIntentContextKey{}).(*DispatchIntent)
    if !ok || intent == nil {
        return nil, false
    }
    return cloneIntent(intent), true
}

func cloneIntent(intent *DispatchIntent) *DispatchIntent {
    if intent == nil {
        return nil
    }
    return &DispatchIntent{
        Platform: productcore.Platform{
            ID: intent.Platform.ID, Code: intent.Platform.Code, AccountPlatform: intent.Platform.AccountPlatform,
            RequestedModel: intent.Platform.RequestedModel, UpstreamModel: intent.Platform.UpstreamModel,
            EndpointCapabilities: append([]string(nil), intent.Platform.EndpointCapabilities...),
            LegacyPricingGroupID: cloneInt64(intent.Platform.LegacyPricingGroupID),
        },
        BillingAsset: cloneBillingAsset(intent.BillingAsset),
    }
}

func cloneBillingAsset(asset *productcore.BillingAsset) *productcore.BillingAsset {
    if asset == nil {
        return nil
    }
    cloned := *asset
    cloned.SubscriptionID = cloneInt64(asset.SubscriptionID)
    cloned.PlanID = cloneInt64(asset.PlanID)
    return &cloned
}

func cloneInt64(value *int64) *int64 {
    if value == nil {
        return nil
    }
    cloned := *value
    return &cloned
}
```

`intent.go` 导入 `context` 和 `internal/productcore`。`cloneIntent` 必须复制 `Platform.EndpointCapabilities`、`LegacyPricingGroupID`、`BillingAsset.SubscriptionID` 和 `BillingAsset.PlanID`。不得使用字符串型 context key，也不得把已有 `ctxkey.GatewayPlatformAsset` 迁移或删除。

- [x] **Step 4: 运行单元测试，确认通过**

Run: `go test -tags=unit ./internal/gatewayruntime -count=1`

Expected: PASS。

- [ ] **Step 5: 提交运行时意图契约**

```powershell
git add backend/internal/gatewayruntime
git commit -m "refactor(网关): 增加不可变调度意图"
```

### Task 3: 用现有 Sub2API 服务实现产品核心端口

**Files:**
- Create: `backend/internal/service/product_core_adapter.go`
- Create: `backend/internal/service/product_core_adapter_test.go`

- [x] **Step 1: 写失败测试，锁定现有套餐实例与错误语义**

```go
func TestPlatformAssetProductCoreAdapterKeepsSelectedSubscription(t *testing.T) {
    planID := int64(17)
    subscriptionID := int64(21)
    subscription := &UserSubscription{ID: subscriptionID, SubscriptionPlanID: &planID, RateMultiplierSnapshot: 0.5}
    adapter := newPlatformAssetProductCoreAdapterForTest(subscription)

    resolution, err := adapter.Resolve(context.Background(), apiKeyWithPlatformAndPlan(3, planID),
        "gpt-4o", "/v1/chat/completions", false)

    require.NoError(t, err)
    require.Equal(t, subscriptionID, *resolution.Decision.BillingAsset.SubscriptionID)
    require.Equal(t, subscription, resolution.Subscription)
    require.Equal(t, 0.5, resolution.Decision.BillingAsset.RateMultiplier)
}

func TestPlatformAssetProductCoreAdapterMapsBalanceErrorBackToService(t *testing.T) {
    adapter := newPlatformAssetProductCoreAdapterForInsufficientBalance()
    _, err := adapter.Resolve(context.Background(), apiKeyWithBalanceOnly(3),
        "gpt-4o", "/v1/chat/completions", false)

    require.ErrorIs(t, err, ErrInsufficientBalance)
}

func apiKeyWithPlatformAndPlan(platformID, planID int64) *APIKey {
    return &APIKey{
        ID: 10, UserID: 7, User: &User{ID: 7, Balance: 10},
        AllowedPlatformIDs: []int64{platformID},
        AllowedSubscriptionPlanIDs: []int64{planID},
        AllowBalance: true,
    }
}

func apiKeyWithBalanceOnly(platformID int64) *APIKey {
    return &APIKey{
        ID: 10, UserID: 7, User: &User{ID: 7, Balance: 0},
        AllowedPlatformIDs: []int64{platformID}, AllowBalance: true,
    }
}

func newPlatformAssetProductCoreAdapterForTest(subscription *UserSubscription) *PlatformAssetProductCoreAdapter {
    return NewPlatformAssetProductCoreAdapter(
        &APIKeyService{},
        &assetSubscriptionResolverStub{candidates: []UserSubscription{*subscription}},
        platformModelResolverStub{resolved: &ResolvedPlatformModel{
            PlatformID: 3, PlatformCode: "gpt", AccountPlatform: PlatformOpenAI,
            RequestedModel: "gpt-4o",
            EndpointCapabilities: []string{string(OpenAIEndpointCapabilityChatCompletions)},
        }},
    )
}

func newPlatformAssetProductCoreAdapterForInsufficientBalance() *PlatformAssetProductCoreAdapter {
    return NewPlatformAssetProductCoreAdapter(
        &APIKeyService{},
        nil,
        platformModelResolverStub{resolved: &ResolvedPlatformModel{
            PlatformID: 3, PlatformCode: "gpt", AccountPlatform: PlatformOpenAI,
            RequestedModel: "gpt-4o",
            EndpointCapabilities: []string{string(OpenAIEndpointCapabilityChatCompletions)},
        }},
    )
}
```

- [x] **Step 2: 运行测试，确认实现前失败**

Run: `go test -tags=unit ./internal/service -run TestPlatformAssetProductCoreAdapter -count=1`

Expected: FAIL，提示 `PlatformAssetProductCoreAdapter` 尚不存在。

- [x] **Step 3: 实现服务适配器**

```go
type PlatformAssetResolution struct {
    Decision     *productcore.Decision
    Subscription *UserSubscription
}

type PlatformAssetProductCoreAdapter struct {
    apiKeyService       *APIKeyService
    subscriptionService apiKeySubscriptionResolver
    platformResolver    PlatformModelResolver
}

func NewPlatformAssetProductCoreAdapter(
    apiKeyService *APIKeyService,
    subscriptionService apiKeySubscriptionResolver,
    platformResolver PlatformModelResolver,
) *PlatformAssetProductCoreAdapter {
    return &PlatformAssetProductCoreAdapter{
        apiKeyService: apiKeyService, subscriptionService: subscriptionService, platformResolver: platformResolver,
    }
}

func (a *PlatformAssetProductCoreAdapter) Resolve(
    ctx context.Context,
    apiKey *APIKey,
    model, endpoint string,
    skipBilling bool,
) (*PlatformAssetResolution, error)
```

`Resolve` 每次调用都创建一个请求范围的 `AssetSelector`。该 selector 直接复用 `APIKeyService.ResolveBillingAssetForRequest`，将其 `ResolvedBillingAsset` 映射为 `productcore.BillingAsset`，并在同一个请求范围中保存返回的订阅快照。不得重新实现套餐排序、额度检查、余额开关或倍率规则。

服务适配器必须使用现有 endpoint 归一化，而不是在新包中解析 HTTP 路径：

```go
request := productcore.Request{
    Model:              requestedModel,
    EndpointCapability: string(billingEndpointCapability(endpoint)),
    SkipBilling:        skipBilling,
}
```

平台适配器必须把现有 `ResolvedPlatformModel` 映射为 `productcore.Platform`：

```go
productcore.Platform{
    ID:                   resolved.PlatformID,
    Code:                 resolved.PlatformCode,
    AccountPlatform:      resolved.AccountPlatform,
    RequestedModel:       resolved.RequestedModel,
    UpstreamModel:        resolved.UpstreamModel,
    EndpointCapabilities: append([]string(nil), resolved.EndpointCapabilities...),
    LegacyPricingGroupID: clonePlatformInt64Pointer(resolved.LegacyGroupID),
}
```

`product_core_adapter.go` 导入 `context`、`errors`、`fmt` 和 `internal/productcore`。其完整适配结构如下。该代码只依赖现有 `PlatformModelResolver`、`APIKeyService.ResolveBillingAssetForRequest` 和订阅 resolver；不读取旧 Group：

```go
type platformCatalogAdapter struct {
    resolver PlatformModelResolver
}

func (a platformCatalogAdapter) ResolveModel(ctx context.Context, model string) (*productcore.Platform, error) {
    resolved, err := a.resolver.ResolveModel(ctx, model)
    if err != nil {
        if errors.Is(err, ErrPlatformModelNotFound) {
            return nil, productcore.ErrModelUnavailable
        }
        return nil, err
    }
    if resolved == nil {
        return nil, productcore.ErrModelUnavailable
    }
    return productPlatformFromResolved(resolved), nil
}

type requestAssetSelector struct {
    service       *APIKeyService
    apiKey        *APIKey
    subscriptions apiKeySubscriptionResolver
    subscription  *UserSubscription
}

func (s *requestAssetSelector) Select(ctx context.Context, _ productcore.AccessGrant, skipBilling bool) (*productcore.BillingAsset, error) {
    asset, err := s.service.ResolveBillingAssetForRequest(ctx, s.apiKey, s.subscriptions, skipBilling)
    if err != nil {
        return nil, mapServiceAssetError(err)
    }
    if asset == nil {
        return nil, nil
    }
    s.subscription = cloneUserSubscription(asset.Subscription)
    return &productcore.BillingAsset{
        Source: asset.Source, SubscriptionID: clonePlatformInt64Pointer(asset.SubscriptionID),
        PlanID: clonePlatformInt64Pointer(asset.PlanID), RateMultiplier: asset.RateMultiplier,
    }, nil
}

func (a *PlatformAssetProductCoreAdapter) Resolve(
    ctx context.Context, apiKey *APIKey, model, endpoint string, skipBilling bool,
) (*PlatformAssetResolution, error) {
    if a == nil || a.apiKeyService == nil || a.platformResolver == nil || apiKey == nil {
        return nil, fmt.Errorf("%w: product core adapter dependencies are required", ErrPlatformInvalid)
    }
    selector := &requestAssetSelector{service: a.apiKeyService, apiKey: apiKey, subscriptions: a.subscriptionService}
    decision, err := productcore.NewAuthorizer(platformCatalogAdapter{resolver: a.platformResolver}, selector).Resolve(
        ctx,
        productcore.AccessGrant{
            KeyID: apiKey.ID, UserID: apiKey.UserID, PlatformIDs: append([]int64(nil), apiKey.AllowedPlatformIDs...),
            SubscriptionPlanIDs: append([]int64(nil), apiKey.AllowedSubscriptionPlanIDs...), AllowBalance: apiKey.AllowBalance,
        },
        productcore.Request{Model: model, EndpointCapability: string(billingEndpointCapability(endpoint)), SkipBilling: skipBilling},
    )
    if err != nil {
        return nil, mapProductCoreError(err)
    }
    scope := PlatformSchedulingScope{PlatformID: decision.Platform.ID, PlatformCode: decision.Platform.Code, AccountPlatform: decision.Platform.AccountPlatform}
    if _, ok := normalizePlatformSchedulingScope(scope); !ok {
        return nil, fmt.Errorf("%w: resolved platform has no account adapter", ErrPlatformInvalid)
    }
    return &PlatformAssetResolution{Decision: decision, Subscription: selector.subscription}, nil
}
```

增加以下转换函数，所有 ID 指针均通过 `clonePlatformInt64Pointer` 复制：

```go
func productPlatformFromResolved(resolved *ResolvedPlatformModel) *productcore.Platform {
    if resolved == nil {
        return nil
    }
    return &productcore.Platform{
        ID: resolved.PlatformID, Code: resolved.PlatformCode, AccountPlatform: resolved.AccountPlatform,
        RequestedModel: resolved.RequestedModel, UpstreamModel: resolved.UpstreamModel,
        EndpointCapabilities: append([]string(nil), resolved.EndpointCapabilities...),
        LegacyPricingGroupID: clonePlatformInt64Pointer(resolved.LegacyGroupID),
    }
}

func mapServiceAssetError(err error) error {
    switch {
    case errors.Is(err, ErrNoUsableBillingSource):
        return productcore.ErrNoBillingAsset
    case errors.Is(err, ErrInsufficientBalance):
        return productcore.ErrInsufficientBalance
    case errors.Is(err, ErrDailyLimitExceeded):
        return productcore.ErrDailyLimitExceeded
    case errors.Is(err, ErrWeeklyLimitExceeded):
        return productcore.ErrWeeklyLimitExceeded
    case errors.Is(err, ErrMonthlyLimitExceeded):
        return productcore.ErrMonthlyLimitExceeded
    default:
        return err
    }
}

func mapProductCoreError(err error) error {
    switch {
    case errors.Is(err, productcore.ErrModelUnavailable):
        return ErrPlatformModelNotFound
    case errors.Is(err, productcore.ErrPlatformForbidden):
        return ErrAPIKeyPlatformForbidden
    case errors.Is(err, productcore.ErrEndpointUnsupported):
        return ErrPlatformEndpointUnsupported
    case errors.Is(err, productcore.ErrNoBillingAsset):
        return ErrNoUsableBillingSource
    case errors.Is(err, productcore.ErrInsufficientBalance):
        return ErrInsufficientBalance
    case errors.Is(err, productcore.ErrDailyLimitExceeded):
        return ErrDailyLimitExceeded
    case errors.Is(err, productcore.ErrWeeklyLimitExceeded):
        return ErrWeeklyLimitExceeded
    case errors.Is(err, productcore.ErrMonthlyLimitExceeded):
        return ErrMonthlyLimitExceeded
    default:
        return err
    }
}
```

该任务不修改 `APIKeyService.ResolvePlatformAssetRequest`，以保证适配器先独立通过定向回归；Task 4 再把旧门面改为委托本适配器和运行时桥。HTTP 中间件不得认识产品核心错误。

- [x] **Step 4: 扩展适配器回归测试**

在 `product_core_adapter_test.go` 增加套餐不可用时回退余额，以及 simple mode 跳过资产选择的断言：

```go
func TestPlatformAssetProductCoreAdapterFallsBackToBalance(t *testing.T) {
    fallback := NewPlatformAssetProductCoreAdapter(
        &APIKeyService{globalBalanceRateProvider: globalBalanceRateProviderStub{rate: 1.75}},
        &assetSubscriptionResolverStub{
            candidates: []UserSubscription{{ID: 21, UserID: 7, SubscriptionPlanID: int64Pointer(17)}},
            validateErrs: map[int64]error{21: ErrDailyLimitExceeded},
        },
        platformModelResolverStub{resolved: &ResolvedPlatformModel{PlatformID: 3, AccountPlatform: PlatformOpenAI}},
    )
    resolution, err := fallback.Resolve(context.Background(), apiKeyWithPlatformAndPlan(3, 17), "gpt-4o", "/v1/chat/completions", false)

    require.NoError(t, err)
    require.Equal(t, BillingSourceBalance, resolution.Decision.BillingAsset.Source)
    require.Equal(t, 1.75, resolution.Decision.BillingAsset.RateMultiplier)
}

func TestPlatformAssetProductCoreAdapterSkipsBillingInSimpleMode(t *testing.T) {
    simple, err := newPlatformAssetProductCoreAdapterForInsufficientBalance().Resolve(
        context.Background(), apiKeyWithBalanceOnly(3), "gpt-4o", "/v1/chat/completions", true,
    )

    require.NoError(t, err)
    require.Nil(t, simple.Decision.BillingAsset)
}

func int64Pointer(value int64) *int64 {
    return &value
}
```

余额回退测试的订阅 resolver 必须提供候选实例，否则不会执行 `ValidateAndCheckLimits`；上述候选会先报告 `ErrDailyLimitExceeded`，随后继续走余额回退。

- [x] **Step 5: 运行服务定向回归**

Run: `go test -tags=unit ./internal/service -run 'Test(PlatformAssetProductCoreAdapter|ResolvePlatformAssetRequest|ResolveBillingAsset)' -count=1`

Expected: PASS。

- [ ] **Step 6: 提交服务适配器**

```powershell
git add backend/internal/service/product_core_adapter.go backend/internal/service/product_core_adapter_test.go
git commit -m "refactor(服务): 由产品核心解析平台资产"
```

### Task 4: 双写运行时意图与旧网关上下文

**Files:**
- Create: `backend/internal/service/gateway_runtime_bridge.go`
- Create: `backend/internal/service/gateway_runtime_bridge_test.go`
- Modify: `backend/internal/service/platform_asset_request.go`
- Modify: `backend/internal/service/platform_asset_request_test.go`

- [x] **Step 1: 写失败测试，固定桥接字段**

```go
func TestGatewayRuntimeBridgePreservesSchedulerAndPricingFacts(t *testing.T) {
    legacyGroupID := int64(9)
    subscriptionID := int64(22)
    decision := &productcore.Decision{
        Platform: productcore.Platform{
            ID: 3, Code: "gpt", AccountPlatform: PlatformOpenAI,
            RequestedModel: "public-gpt", UpstreamModel: "gpt-4o-2024-08-06",
            LegacyPricingGroupID: &legacyGroupID,
        },
        BillingAsset: &productcore.BillingAsset{
            Source: "subscription", SubscriptionID: &subscriptionID, RateMultiplier: 0.5,
        },
    }

    ctx := attachProductDecision(context.Background(), decision, nil)
    intent, ok := gatewayruntime.DispatchIntentFromContext(ctx)
    require.True(t, ok)
    require.Equal(t, int64(3), intent.Platform.ID)

    legacy, ok := GatewayPlatformAssetContextFromContext(ctx)
    require.True(t, ok)
    require.Equal(t, PlatformOpenAI, legacy.SchedulingScope.AccountPlatform)
    require.Equal(t, legacyGroupID, *legacy.PricingGroupID)
    require.Equal(t, 0.5, legacy.BillingAsset.RateMultiplier)
}
```

- [x] **Step 2: 运行测试，确认实现前失败**

Run: `go test -tags=unit ./internal/service -run TestGatewayRuntimeBridge -count=1`

Expected: FAIL，提示桥接函数尚不存在。

- [x] **Step 3: 实现双写桥接，不改调度器**

```go
// gateway_runtime_bridge.go
func gatewayPlatformAssetContextFromDecision(
    decision *productcore.Decision,
    subscription *UserSubscription,
) *GatewayPlatformAssetContext {
    if decision == nil {
        return nil
    }
    platform := decision.Platform
    return &GatewayPlatformAssetContext{
        Platform: &ResolvedPlatformModel{
            PlatformID: platform.ID, PlatformCode: platform.Code, AccountPlatform: platform.AccountPlatform,
            RequestedModel: platform.RequestedModel, UpstreamModel: platform.UpstreamModel,
            EndpointCapabilities: append([]string(nil), platform.EndpointCapabilities...),
            LegacyGroupID: clonePlatformInt64Pointer(platform.LegacyPricingGroupID),
        },
        BillingAsset: resolvedBillingAssetFromProduct(decision.BillingAsset, subscription),
        SchedulingScope: PlatformSchedulingScope{
            PlatformID: platform.ID, PlatformCode: platform.Code, AccountPlatform: platform.AccountPlatform,
        },
        PricingGroupID: clonePlatformInt64Pointer(platform.LegacyPricingGroupID),
    }
}

func resolvedBillingAssetFromProduct(asset *productcore.BillingAsset, subscription *UserSubscription) *ResolvedBillingAsset {
    if asset == nil {
        return nil
    }
    return &ResolvedBillingAsset{
        Source: asset.Source, SubscriptionID: clonePlatformInt64Pointer(asset.SubscriptionID),
        PlanID: clonePlatformInt64Pointer(asset.PlanID), RateMultiplier: asset.RateMultiplier,
        Subscription: cloneUserSubscription(subscription),
    }
}

func productBillingAssetFromResolved(asset *ResolvedBillingAsset) *productcore.BillingAsset {
    if asset == nil {
        return nil
    }
    return &productcore.BillingAsset{
        Source: asset.Source, SubscriptionID: clonePlatformInt64Pointer(asset.SubscriptionID),
        PlanID: clonePlatformInt64Pointer(asset.PlanID), RateMultiplier: asset.RateMultiplier,
    }
}

func dispatchIntentFromGatewayRoute(route *GatewayPlatformAssetContext) *gatewayruntime.DispatchIntent {
    if route == nil || route.Platform == nil {
        return nil
    }
    platform := productPlatformFromResolved(route.Platform)
    if platform == nil {
        return nil
    }
    // PricingGroupID 是旧上下文的显式定价引用，优先级高于 Platform.LegacyGroupID。
    if route.PricingGroupID != nil {
        platform.LegacyPricingGroupID = clonePlatformInt64Pointer(route.PricingGroupID)
    }
    return &gatewayruntime.DispatchIntent{Platform: *platform, BillingAsset: productBillingAssetFromResolved(route.BillingAsset)}
}

func cloneGatewayPlatformAssetContext(route *GatewayPlatformAssetContext) *GatewayPlatformAssetContext {
    if route == nil {
        return nil
    }
    return &GatewayPlatformAssetContext{
        Platform: cloneResolvedPlatformModel(route.Platform), BillingAsset: cloneResolvedBillingAsset(route.BillingAsset),
        SchedulingScope: route.SchedulingScope, PricingGroupID: clonePlatformInt64Pointer(route.PricingGroupID),
    }
}

func attachGatewayPlatformAssetRoute(ctx context.Context, route *GatewayPlatformAssetContext) context.Context {
    if ctx == nil || route == nil || route.Platform == nil {
        return ctx
    }
    scope, ok := normalizePlatformSchedulingScope(route.SchedulingScope)
    if !ok {
        return ctx
    }
    cloned := cloneGatewayPlatformAssetContext(route)
    if intent := dispatchIntentFromGatewayRoute(cloned); intent != nil {
        ctx = gatewayruntime.WithDispatchIntent(ctx, intent)
    }
    ctx = context.WithValue(ctx, ctxkey.GatewayPlatformAsset, cloned)
    ctx = WithPlatformSchedulingScope(ctx, scope)
    ctx = WithResolvedTargetPlatform(ctx, scope.AccountPlatform)
    if model := strings.TrimSpace(cloned.Platform.UpstreamModel); model != "" {
        ctx = context.WithValue(ctx, ctxkey.ResolvedUpstreamModel, model)
    }
    if model := strings.TrimSpace(cloned.Platform.RequestedModel); model != "" {
        ctx = context.WithValue(ctx, ctxkey.RequestedPublicModel, model)
    }
    return ctx
}

func attachProductDecision(
    ctx context.Context,
    decision *productcore.Decision,
    subscription *UserSubscription,
) context.Context {
    return attachGatewayPlatformAssetRoute(ctx, gatewayPlatformAssetContextFromDecision(decision, subscription))
}
```

把现有 `WithGatewayPlatformAssetContext` 的函数体替换为下面的门面，不能保留第二个同名实现：

```go
func WithGatewayPlatformAssetContext(ctx context.Context, route *GatewayPlatformAssetContext) context.Context {
    return attachGatewayPlatformAssetRoute(ctx, route)
}
```

将现有 `GatewayPlatformAssetContextFromContext` 改为“优先旧上下文、回退新意图”的实现：

```go
func GatewayPlatformAssetContextFromContext(ctx context.Context) (*GatewayPlatformAssetContext, bool) {
    if ctx == nil {
        return nil, false
    }
    if route, ok := ctx.Value(ctxkey.GatewayPlatformAsset).(*GatewayPlatformAssetContext); ok && route != nil && route.Platform != nil {
        return cloneGatewayPlatformAssetContext(route), true
    }
    intent, ok := gatewayruntime.DispatchIntentFromContext(ctx)
    if !ok {
        return nil, false
    }
    route := gatewayPlatformAssetContextFromDecision(&productcore.Decision{
        Platform: intent.Platform, BillingAsset: intent.BillingAsset,
    }, nil)
    if route == nil {
        return nil, false
    }
    return route, true
}
```

增加公开门面并由 HTTP 层委托它：

```go
func AttachPlatformAssetResolution(ctx context.Context, resolution *PlatformAssetResolution) context.Context {
    if resolution == nil {
        return ctx
    }
    return attachProductDecision(ctx, resolution.Decision, resolution.Subscription)
}
```

将 `APIKeyService.ResolvePlatformAssetRequest` 的旧算法替换为兼容门面。签名不变，并继续拒绝未迁移的 Legacy Key：

```go
func (s *APIKeyService) ResolvePlatformAssetRequest(
    ctx context.Context, apiKey *APIKey, resolver PlatformModelResolver,
    subscriptions apiKeySubscriptionResolver, requestedModel, endpoint string, skipBilling bool,
) (*GatewayPlatformAssetContext, error) {
    if apiKey == nil || !UsesPlatformAssetPermissions(apiKey) {
        return nil, ErrAPIKeyPlatformForbidden
    }
    resolution, err := NewPlatformAssetProductCoreAdapter(s, subscriptions, resolver).
        Resolve(ctx, apiKey, requestedModel, endpoint, skipBilling)
    if err != nil {
        return nil, err
    }
    return gatewayPlatformAssetContextFromDecision(resolution.Decision, resolution.Subscription), nil
}
```

`gateway_runtime_bridge.go` 导入 `context`、`strings`、`internal/pkg/ctxkey`、`internal/gatewayruntime` 和 `internal/productcore`。上下文读写函数集中在该文件；`cloneResolvedPlatformModel` 与 `cloneResolvedBillingAsset` 继续留在 `platform_asset_request.go` 作为兼容门面的纯克隆 helper，桥接文件直接复用它们，避免无关移动。`GatewayPlatformAssetContextFromContext` 先读现有 `ctxkey.GatewayPlatformAsset`，在它不存在时再由 `gatewayruntime.DispatchIntent` 重建不含 `UserSubscription` 的兼容视图。这样第一阶段不会丢失现有 Gin 订阅对象，同时让以后运行时消费者可以迁移到新契约。

桥接还必须继续写入现有 `PlatformSchedulingScope`、`ResolvedTargetPlatform`、`ResolvedUpstreamModel` 和 `RequestedPublicModel` context 值；这些值是现有调度与模型映射的输入，不能移除或改名。

- [x] **Step 4: 运行平台资产与用量回归**

Run: `go test -tags=unit ./internal/service -run 'Test(GatewayRuntimeBridge|GatewayPlatformAssetContext|PlatformAssetBillingFactsOverrideLegacyGroupValues|OpenAIGatewayServiceRecordUsage_PlatformAssetUsesResolvedBalanceMultiplier|GatewayServiceRecordUsage_PlatformAssetUsesResolvedBalanceMultiplier)' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交上下文桥**

```powershell
git add backend/internal/service/gateway_runtime_bridge.go backend/internal/service/gateway_runtime_bridge_test.go backend/internal/service/platform_asset_request.go backend/internal/service/platform_asset_request_test.go
git commit -m "refactor(网关): 桥接产品决策与运行时上下文"
```

### Task 5: 让 HTTP 中间件只负责入口适配

**Files:**
- Modify: `backend/internal/server/middleware/platform_asset_auth.go`
- Modify: `backend/internal/server/middleware/platform_asset_auth_test.go`
- Modify: `backend/internal/server/routes/gateway.go`
- Create: `backend/internal/server/middleware/platform_asset_authorizer_test.go`

- [x] **Step 1: 写失败测试，确认 HTTP 层不再组合领域依赖**

```go
type platformAssetAuthorizerStub struct {
    t                *testing.T
    expectedModel    string
    expectedEndpoint string
    resolution       *service.PlatformAssetResolution
    err              error
    calls            int
}

func (s *platformAssetAuthorizerStub) Resolve(
    _ context.Context,
    _ *service.APIKey,
    model, endpoint string,
    skipBilling bool,
) (*service.PlatformAssetResolution, error) {
    s.calls++
    if s.expectedModel != "" {
        require.Equal(s.t, s.expectedModel, model)
    }
    if s.expectedEndpoint != "" {
        require.Equal(s.t, s.expectedEndpoint, endpoint)
    }
    require.False(s.t, skipBilling)
    return s.resolution, s.err
}

func TestPlatformAssetAuthorizationUsesFacadeAndKeepsSubscriptionContext(t *testing.T) {
    gin.SetMode(gin.TestMode)
    subscriptionID := int64(21)
    subscription := &service.UserSubscription{ID: subscriptionID}
    authorizer := &platformAssetAuthorizerStub{
        t: t,
        expectedModel: "gpt-4o",
        expectedEndpoint: "/v1/chat/completions",
        resolution: &service.PlatformAssetResolution{
            Decision: &productcore.Decision{
                Platform: productcore.Platform{ID: 3, AccountPlatform: service.PlatformOpenAI},
                BillingAsset: &productcore.BillingAsset{
                    Source: "subscription", SubscriptionID: &subscriptionID, RateMultiplier: 0.5,
                },
            },
            Subscription: subscription,
        },
    }
    router := gin.New()
    router.Use(func(c *gin.Context) {
        c.Set(string(ContextKeyAPIKey), &service.APIKey{AllowedPlatformIDs: []int64{3}})
        c.Next()
    })
    router.Use(NewPlatformAssetAuthorizationMiddleware(authorizer, &config.Config{}))
    router.POST("/v1/chat/completions", func(c *gin.Context) {
        body, err := io.ReadAll(c.Request.Body)
        require.NoError(t, err)
        require.JSONEq(t, `{"model":"gpt-4o"}`, string(body))
        intent, ok := gatewayruntime.DispatchIntentFromContext(c.Request.Context())
        require.True(t, ok)
        require.Equal(t, int64(3), intent.Platform.ID)
        gotSubscription, ok := GetSubscriptionFromContext(c)
        require.True(t, ok)
        require.Same(t, subscription, gotSubscription)
        c.Status(http.StatusNoContent)
    })

    response := httptest.NewRecorder()
    request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
    router.ServeHTTP(response, request)

    require.Equal(t, http.StatusNoContent, response.Code)
    require.Equal(t, 1, authorizer.calls)
}
```

- [x] **Step 2: 运行测试，确认实现前失败**

Run: `go test -tags=unit ./internal/server/middleware -run TestPlatformAssetAuthorizationUsesFacadeAndKeepsSubscriptionContext -count=1`

Expected: FAIL，中间件构造函数仍要求 `APIKeyService`、订阅服务和平台解析器。

- [x] **Step 3: 改为门面依赖并保持 HTTP 逻辑不变**

```go
type PlatformAssetRequestAuthorizer interface {
    Resolve(context.Context, *service.APIKey, string, string, bool) (*service.PlatformAssetResolution, error)
}

func NewPlatformAssetAuthorizationMiddleware(
    authorizer PlatformAssetRequestAuthorizer,
    cfg *config.Config,
) gin.HandlerFunc {
    return newPlatformAssetAuthorizationMiddleware(
        authorizer, cfg, platformAssetJSONRequestModel, abortPlatformAssetRequestError,
    )
}

func NewPlatformAssetAuthorizationGoogleMiddleware(
    authorizer PlatformAssetRequestAuthorizer,
    cfg *config.Config,
) gin.HandlerFunc {
    return newPlatformAssetAuthorizationMiddleware(
        authorizer, cfg, platformAssetGoogleRequestModel, abortPlatformAssetGoogleRequestError,
    )
}

func newPlatformAssetAuthorizationMiddleware(
    authorizer PlatformAssetRequestAuthorizer,
    cfg *config.Config,
    readModel platformAssetRequestModelReader,
    writeError platformAssetErrorWriter,
) gin.HandlerFunc {
    return func(c *gin.Context) {
        apiKey, ok := GetAPIKeyFromContext(c)
        if !ok || !service.UsesPlatformAssetPermissions(apiKey) {
            c.Next()
            return
        }
        model, err := readModel(c)
        if err != nil {
            writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body")
            return
        }
        if model == "" {
            c.Next()
            return
        }
        if authorizer == nil {
            writeError(c, http.StatusInternalServerError, "PLATFORM_ASSET_RESOLUTION_FAILED", "Failed to resolve platform request")
            return
        }
        resolution, err := authorizer.Resolve(
            c.Request.Context(), apiKey, model, apiKeyBillingRequestEndpoint(c),
            cfg != nil && cfg.RunMode == config.RunModeSimple,
        )
        if err != nil {
            abortPlatformAssetResolutionError(c, err, writeError)
            return
        }
        if resolution == nil || resolution.Decision == nil {
            writeError(c, http.StatusInternalServerError, "PLATFORM_ASSET_RESOLUTION_FAILED", "Failed to resolve platform request")
            return
        }
        c.Request = c.Request.WithContext(service.AttachPlatformAssetResolution(c.Request.Context(), resolution))
        if resolution.Subscription != nil {
            c.Set(string(ContextKeySubscription), resolution.Subscription)
        }
        c.Next()
    }
}
```

`NewPlatformAssetAuthorizationGoogleMiddleware` 使用相同接口。中间件只允许完成以下动作：

1. 从已认证的 Gin context 读取 API Key。
2. 从 JSON body 或 Google URL 读取模型并复位 body。
3. 调用 `authorizer.Resolve`。
4. 使用 `service.AttachPlatformAssetResolution` 写入运行时上下文。
5. 存在套餐实例时继续设置 `ContextKeySubscription`。
6. 将服务错误映射为当前 JSON 或 Google 错误包络。

`RegisterGatewayRoutes` 在创建中间件前构造一次：

```go
platformAssetAuthorizer := service.NewPlatformAssetProductCoreAdapter(
    apiKeyService, subscriptionService, platformResolver,
)
platformAssetAuth := middleware.NewPlatformAssetAuthorizationMiddleware(platformAssetAuthorizer, cfg)
platformAssetGoogleAuth := middleware.NewPlatformAssetAuthorizationGoogleMiddleware(platformAssetAuthorizer, cfg)
```

除了构造参数外，路由顺序不得改变：API Key 认证、平台资产授权、复合路由、旧 Group 兼容检查的执行顺序保持现状。

将现有三个中间件测试改为先构造同一个适配器再传入构造函数：

```go
authorizer := service.NewPlatformAssetProductCoreAdapter(apiKeyService, subscriptionService, resolver)
router.Use(NewPlatformAssetAuthorizationMiddleware(authorizer, cfg))

googleAuthorizer := service.NewPlatformAssetProductCoreAdapter(apiKeyService, nil, resolver)
router.Use(NewPlatformAssetAuthorizationGoogleMiddleware(googleAuthorizer, cfg))
```

前两个 JSON 测试复用其已有的 `subscriptionService` 变量；Google 测试仍传入 `nil` 订阅服务。除 `platform_asset_auth.go`、其测试文件及 `routes/gateway.go` 外，`rg -n 'NewPlatformAssetAuthorization(Middleware|GoogleMiddleware)\\(' backend --glob '*.go'` 不应存在其他调用点。

- [x] **Step 4: 补齐 JSON、Google 和 Legacy Key 回归**

在现有 `platform_asset_auth_test.go` 中保留并扩展：

```go
require.JSONEq(t, `{"model":"gpt-4o"}`, string(body))
require.Equal(t, int64(3), route.Platform.PlatformID)
require.Equal(t, service.BillingSourceBalance, route.BillingAsset.Source)
require.Equal(t, int64(21), subscription.ID)
```

在新测试文件中补充以下两个可直接执行的边界测试：

```go
func TestPlatformAssetAuthorizationSkipsFacadeForLegacyKey(t *testing.T) {
    authorizer := &platformAssetAuthorizerStub{t: t}
    router := gin.New()
    router.Use(func(c *gin.Context) {
        c.Set(string(ContextKeyAPIKey), &service.APIKey{})
        c.Next()
    })
    router.Use(NewPlatformAssetAuthorizationMiddleware(authorizer, &config.Config{}))
    router.POST("/v1/chat/completions", func(c *gin.Context) { c.Status(http.StatusNoContent) })

    response := httptest.NewRecorder()
    router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`)))

    require.Equal(t, http.StatusNoContent, response.Code)
    require.Zero(t, authorizer.calls)
}

func TestPlatformAssetAuthorizationKeepsEndpointErrorEnvelope(t *testing.T) {
    authorizer := &platformAssetAuthorizerStub{
        t: t, expectedModel: "gpt-4o", expectedEndpoint: "/v1/responses",
        err: service.ErrPlatformEndpointUnsupported,
    }
    router := gin.New()
    router.Use(func(c *gin.Context) {
        c.Set(string(ContextKeyAPIKey), &service.APIKey{AllowedPlatformIDs: []int64{3}})
        c.Next()
    })
    router.Use(NewPlatformAssetAuthorizationMiddleware(authorizer, &config.Config{}))
    router.POST("/v1/responses", func(c *gin.Context) { c.Status(http.StatusNoContent) })

    response := httptest.NewRecorder()
    router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o"}`)))

    require.Equal(t, http.StatusForbidden, response.Code)
    require.Contains(t, response.Body.String(), "PLATFORM_ENDPOINT_UNSUPPORTED")
}
```

Google 原生路径继续使用现有 `TestGooglePlatformAssetAuthorizationResolvesModelFromPath`，并在其中通过 facade stub 断言模型为 `gemini-2.5-pro`。为 Google 错误包络新增同样的 `ErrPlatformEndpointUnsupported` 场景，只断言 HTTP 403 和 Google 格式 `error` 字段，不复用 OpenAI JSON 的 `code` 断言。

- [x] **Step 5: 运行中间件与路由测试**

Run: `go test -tags=unit ./internal/server/middleware -run TestPlatformAssetAuthorization -count=1`

Expected: PASS。

Run: `go test -tags=unit ./internal/server/routes -run TestGateway -count=1`

Expected: PASS。

- [ ] **Step 6: 提交 HTTP 边界收口**

```powershell
git add backend/internal/server/middleware/platform_asset_auth.go backend/internal/server/middleware/platform_asset_auth_test.go backend/internal/server/middleware/platform_asset_authorizer_test.go backend/internal/server/routes/gateway.go
git commit -m "refactor(入口): 通过产品核心门面授权请求"
```

### Task 6: 运行时保护、最终验证与记忆回填

**Files:**
- Modify: `docs/memory/当前状态.md`
- Modify: `docs/memory/项目概览.md`
- Modify: `docs/memory/决策/2026-08-05-product-core-runtime-boundary.md`
- Modify: `docs/superpowers/plans/2026-08-05-product-core-runtime-boundary.md`

- [x] **Step 1: 检查受保护运行时没有行为性差异**

Run:

```powershell
$protectedExact = @(
  'backend/internal/service/gateway_scheduling.go',
  'backend/internal/service/openai_gateway_scheduling.go',
  'backend/internal/service/openai_account_scheduler.go',
  'backend/internal/service/account_credentials_persistence.go'
)
$protectedChanges = git diff --name-only | Where-Object {
  $_ -in $protectedExact -or
  $_ -like 'backend/internal/service/openai_gateway_*.go' -or
  $_ -like 'backend/internal/service/antigravity_gateway_*.go'
}
if ($protectedChanges) {
  $protectedChanges
  throw '本阶段不允许修改受保护网关运行时文件'
}
```

Expected: 无输出且命令正常结束。若抛出异常，停止本计划并将该改动拆入独立运行时设计，不得在本计划中继续。

- [x] **Step 2: 运行产品核心、服务、网关和构建回归**

Run: `go test -tags=unit ./internal/productcore ./internal/gatewayruntime -count=1`

Expected: PASS。

Run: `go test -tags=unit ./internal/service -run 'Test(PlatformAsset|GatewayRuntimeBridge|GatewayPlatformAssetContext|ResolveBillingAsset|OpenAIGatewayServiceRecordUsage_PlatformAssetUsesResolvedBalanceMultiplier|GatewayServiceRecordUsage_PlatformAssetUsesResolvedBalanceMultiplier)' -count=1`

Expected: PASS。

Run: `go test -tags=unit ./internal/server/middleware ./internal/server/routes -count=1`

Expected: PASS。

Run: `go build ./cmd/server`

Expected: Exit code 0。

Run: `git diff --check`

Expected: Exit code 0。

- [ ] **Step 3: 执行人工 HTTP 验收**

在 My2 测试环境用同一 API Key 验证：

```text
1. Key 允许 OpenAI 平台、一个有效套餐和余额；/v1/chat/completions 成功，记录为套餐倍率。
2. 套餐耗尽后同一 Key 再请求；若余额允许且充足，记录为余额倍率。
3. 平台只允许 chat completions；请求 /v1/responses 返回 403 PLATFORM_ENDPOINT_UNSUPPORTED。
4. 使首个账号临时不可用；请求仍由既有同平台账号池故障切换成功。
5. 使用 Legacy Key 请求，确认没有新增平台权限或资产授权，旧兼容路径保持可用。
```

- [x] **Step 4: 回填真实验证结果与同步检查项**

在 `当前状态.md` 中记录实际通过的命令、未执行项和用户验收结果；在决策文件中补充任何经验证的新约束。不得把未运行的完整测试或人工场景写成“通过”。

- [x] **Step 5: 提交验证与文档收口**

```powershell
git add docs backend/internal/productcore backend/internal/gatewayruntime backend/internal/service backend/internal/server
git commit -m "test(架构): 验证产品核心运行时边界"
```

本步骤已完成：实现、测试和项目记忆已提交到 `my2.0`。Tag、推送和部署在提交后单独执行；人工 HTTP 验收仍待测试环境更新。

发布、打 Tag、推送和部署在提交完成且验证证据齐全后执行；人工 HTTP 验收仍需部署新构建后补齐。

## 规格覆盖自检

| 已确认需求 | 覆盖任务 |
| --- | --- |
| 保留账号调度、OAuth、协议适配 | 范围与禁止项、Task 4、Task 6 |
| 产品核心拥有平台授权、套餐、余额、倍率 | Task 1、Task 3 |
| 运行时接收稳定、不可变的调度意图 | Task 2、Task 4 |
| 不拆服务、不迁库、不改 UI | 目标架构、范围与禁止项、Task 6 |
| Legacy Group 只作兼容 | Task 3、Task 5 |
| 套餐优先、余额回退和倍率不变 | Task 1、Task 3、Task 6 |
| chat、responses、Google 入口错误语义不变 | Task 5、Task 6 |
| 官方同步后有固定检查点 | Task 6 与项目记忆决策 |
