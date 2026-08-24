//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type platformCatalogPlatformRepoStub struct {
	platforms []Platform
}

func (s platformCatalogPlatformRepoStub) List(context.Context) ([]Platform, error) {
	return append([]Platform(nil), s.platforms...), nil
}

func (s platformCatalogPlatformRepoStub) ListModelRules(context.Context) ([]PlatformModelRule, error) {
	var rules []PlatformModelRule
	for _, platform := range s.platforms {
		rules = append(rules, platform.ModelRules...)
	}
	return rules, nil
}

type platformCatalogPricingResolverStub struct {
	seen       []PricingInput
	err        error
	batchCalls int
	batchErr   error
}

func (s *platformCatalogPricingResolverStub) Resolve(_ context.Context, input PricingInput) (*ResolvedPricing, error) {
	s.seen = append(s.seen, input)
	if s.err != nil {
		return nil, s.err
	}
	price := 0.000001
	return &ResolvedPricing{
		Mode: BillingModeToken,
		BasePricing: &ModelPricing{
			InputPricePerToken:  price,
			OutputPricePerToken: price * 2,
		},
		Source: PricingSourceLiteLLM,
	}, nil
}

func (s *platformCatalogPricingResolverStub) ResolveBatch(ctx context.Context, inputs []PricingInput) ([]*ResolvedPricing, error) {
	s.batchCalls++
	if s.batchErr != nil {
		return nil, s.batchErr
	}
	resolved := make([]*ResolvedPricing, len(inputs))
	for i := range inputs {
		item, err := s.Resolve(ctx, inputs[i])
		if errors.Is(err, ErrModelPricingUnavailable) {
			resolved[i] = &ResolvedPricing{
				OfficialSource: PricingSourceInfo{Type: PricingSourceUnavailable, MatchedModel: inputs[i].Model},
				Source:         string(PricingSourceUnavailable),
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		resolved[i] = item
	}
	return resolved, nil
}

func TestPlatformCatalogEmptyPricingCatalogSkipsResolver(t *testing.T) {
	pricing := &platformCatalogPricingResolverStub{batchErr: errors.New("resolver must not be called")}
	service := NewPlatformCatalogService(platformCatalogPlatformRepoStub{platforms: []Platform{{
		ID: 7, Code: "codex", Name: "Codex", AccountPlatform: PlatformOpenAI, Status: PlatformStatusActive,
		ModelRules: []PlatformModelRule{{ModelPattern: "disabled-model", Enabled: false}},
	}}}, pricing)

	items, err := service.ListPlaza(context.Background())

	require.NoError(t, err)
	require.Empty(t, items)
	require.Zero(t, pricing.batchCalls)
}

func TestPlatformCatalogBatchPricingLoadsRuleSnapshotOnce(t *testing.T) {
	salePrice := 6e-6
	repo := &modelPricingOverrideRepoStub{rules: []ModelPricingOverride{
		{Adapter: "codex", ModelPattern: "gpt-5.6-sol", BillingMode: BillingModeToken, InputPrice: &salePrice, Status: ModelPricingStatusActive},
		{Adapter: "codex", ModelPattern: "gpt-5.6-terra", BillingMode: BillingModeToken, InputPrice: &salePrice, Status: ModelPricingStatusActive},
	}}
	resolver := NewModelPricingResolverWithCatalog(newTestBillingService(), NewModelPricingCatalog(repo))
	service := NewPlatformCatalogService(platformCatalogPlatformRepoStub{platforms: []Platform{{
		ID: 7, Code: "codex", Name: "Codex", AccountPlatform: PlatformOpenAI, Status: PlatformStatusActive,
		ModelRules: []PlatformModelRule{
			{ModelPattern: "gpt-5.6-sol", Enabled: true},
			{ModelPattern: "gpt-5.6-terra", Enabled: true},
		},
	}}}, resolver)

	items, err := service.ListPlaza(context.Background())

	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Len(t, items[0].Models, 2)
	require.Equal(t, 1, repo.calls)
	for i := range items[0].Models {
		require.NotNil(t, items[0].Models[i].Pricing)
		require.NotNil(t, items[0].Models[i].Pricing.MatchedOverride)
		require.InDelta(t, salePrice, items[0].Models[i].Pricing.BasePricing.InputPricePerToken, 1e-12)
	}
}

func TestPlatformCatalogBatchPricingRepositoryErrorReturnsNoPartialCatalog(t *testing.T) {
	repoErr := errors.New("database unavailable")
	resolver := NewModelPricingResolverWithCatalog(
		newTestBillingService(),
		NewModelPricingCatalog(&modelPricingOverrideRepoStub{err: repoErr}),
	)
	service := NewPlatformCatalogService(platformCatalogPlatformRepoStub{platforms: []Platform{{
		ID: 7, Code: "codex", Name: "Codex", AccountPlatform: PlatformOpenAI, Status: PlatformStatusActive,
		ModelRules: []PlatformModelRule{
			{ModelPattern: "gpt-5.6-sol", Enabled: true},
			{ModelPattern: "gpt-5.6-terra", Enabled: true},
		},
	}}}, resolver)

	items, err := service.ListPlaza(context.Background())

	require.Nil(t, items)
	require.ErrorContains(t, err, repoErr.Error())
}

func TestPlatformPricingCatalogKeepsUnavailableModelsVisible(t *testing.T) {
	pricing := &platformCatalogPricingResolverStub{err: ErrModelPricingUnavailable}
	service := NewPlatformCatalogService(platformCatalogPlatformRepoStub{platforms: []Platform{{
		ID: 7, Code: "codex", Name: "Codex", AccountPlatform: PlatformOpenAI, Status: PlatformStatusActive,
		ModelRules: []PlatformModelRule{{ModelPattern: "unknown-model", Enabled: true}},
	}}}, pricing)

	items, err := service.ListPricingCatalog(context.Background())

	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Len(t, items[0].Models, 1)
	require.Equal(t, PricingSourceUnavailable, items[0].Models[0].Pricing.OfficialSource.Type)
	require.Equal(t, "unknown-model", items[0].Models[0].Pricing.OfficialSource.MatchedModel)
}

func TestPlatformPricingCatalogPropagatesPricingRepositoryErrors(t *testing.T) {
	pricing := &platformCatalogPricingResolverStub{err: errors.New("database unavailable")}
	service := NewPlatformCatalogService(platformCatalogPlatformRepoStub{platforms: []Platform{{
		ID: 7, Code: "codex", Name: "Codex", AccountPlatform: PlatformOpenAI, Status: PlatformStatusActive,
		ModelRules: []PlatformModelRule{{ModelPattern: "gpt-5.6-sol", Enabled: true}},
	}}}, pricing)

	items, err := service.ListPricingCatalog(context.Background())

	require.Nil(t, items)
	require.ErrorContains(t, err, "database unavailable")
}

func TestPlatformCatalogListsOnlyActivePlatformsAndEnabledRules(t *testing.T) {
	service := NewPlatformCatalogService(platformCatalogPlatformRepoStub{platforms: []Platform{
		{
			ID: 2, Code: "disabled", Name: "Disabled", AccountPlatform: PlatformOpenAI, Status: StatusDisabled,
			ModelRules: []PlatformModelRule{{ModelPattern: "hidden-*", Enabled: true}},
		},
		{
			ID: 1, Code: "openai", Name: "OpenAI", AccountPlatform: PlatformOpenAI, Status: PlatformStatusActive,
			EndpointCapabilities: []string{"responses"},
			ModelRules: []PlatformModelRule{
				{ModelPattern: "gpt-*", UpstreamModel: "", Enabled: true},
				{ModelPattern: "disabled-model", Enabled: false},
			},
		},
	}}, nil)

	items, err := service.ListAvailable(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "openai", items[0].Code)
	require.Equal(t, []string{"gpt-*"}, []string{items[0].Models[0].Pattern})
	require.Equal(t, []string{"responses"}, items[0].Models[0].EndpointCapabilities)
}

func TestPlatformCatalogPlazaUsesPlatformPricingWithoutLegacyAssets(t *testing.T) {
	pricing := &platformCatalogPricingResolverStub{}
	service := NewPlatformCatalogService(platformCatalogPlatformRepoStub{platforms: []Platform{
		{
			ID: 1, Code: "glm", Name: "GLM", AccountPlatform: PlatformOpenAI, Status: PlatformStatusActive,
			ModelRules: []PlatformModelRule{{ModelPattern: "glm-5.2", UpstreamModel: "glm-5.2", Enabled: true}},
		},
	}}, pricing)

	items, err := service.ListPlaza(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "glm", items[0].Code)
	require.Len(t, items[0].Models, 1)
	require.NotNil(t, items[0].Models[0].Pricing)
	require.Equal(t, "openai", pricing.seen[0].Adapter)
	require.Equal(t, "glm-5.2", pricing.seen[0].Model)
	require.Equal(t, "glm", pricing.seen[0].PlatformCode)
	require.Equal(t, "glm-5.2", pricing.seen[0].PublicModel)
}
