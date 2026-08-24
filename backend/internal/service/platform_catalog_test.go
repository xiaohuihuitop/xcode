//go:build unit

package service

import (
	"context"
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
	seen []PricingInput
}

func (s *platformCatalogPricingResolverStub) Resolve(_ context.Context, input PricingInput) (*ResolvedPricing, error) {
	s.seen = append(s.seen, input)
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
