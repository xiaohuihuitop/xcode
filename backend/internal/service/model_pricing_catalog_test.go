//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type modelPricingOverrideRepoStub struct {
	rules []ModelPricingOverride
	calls int
}

func (s *modelPricingOverrideRepoStub) List(context.Context, string) ([]ModelPricingOverride, error) {
	s.calls++
	return append([]ModelPricingOverride(nil), s.rules...), nil
}
func (s *modelPricingOverrideRepoStub) Get(context.Context, int64) (*ModelPricingOverride, error) {
	return nil, ErrModelPricingOverrideNotFound
}
func (s *modelPricingOverrideRepoStub) Create(context.Context, *ModelPricingOverride) error {
	return nil
}
func (s *modelPricingOverrideRepoStub) Update(context.Context, *ModelPricingOverride) error {
	return nil
}
func (s *modelPricingOverrideRepoStub) Delete(context.Context, int64) error { return nil }

func TestModelPricingCatalogExactPatternWins(t *testing.T) {
	catalog := NewModelPricingCatalog(&modelPricingOverrideRepoStub{rules: []ModelPricingOverride{
		{Adapter: PlatformOpenAI, ModelPattern: "gpt-*", InputPrice: floatPtr(1e-6), Status: ModelPricingStatusActive},
		{Adapter: PlatformOpenAI, ModelPattern: "gpt-5.6", InputPrice: floatPtr(2.5e-6), Status: ModelPricingStatusActive},
	}})

	got, err := catalog.Resolve(context.Background(), PlatformOpenAI, "gpt-5.6")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.InDelta(t, 2.5e-6, *got.InputPrice, 1e-12)
}

func TestModelPricingCatalogLongestWildcardWins(t *testing.T) {
	catalog := NewModelPricingCatalog(&modelPricingOverrideRepoStub{rules: []ModelPricingOverride{
		{Adapter: PlatformOpenAI, ModelPattern: "gpt-*", InputPrice: floatPtr(1e-6), Status: ModelPricingStatusActive},
		{Adapter: PlatformOpenAI, ModelPattern: "gpt-5.*", InputPrice: floatPtr(2e-6), Status: ModelPricingStatusActive},
	}})

	got, err := catalog.Resolve(context.Background(), PlatformOpenAI, "gpt-5.6")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.InDelta(t, 2e-6, *got.InputPrice, 1e-12)
}

func TestModelPricingCatalogPlatformPricingPrefersPlatformCodeAndPublicModel(t *testing.T) {
	catalog := NewModelPricingCatalog(&modelPricingOverrideRepoStub{rules: []ModelPricingOverride{
		{Adapter: PlatformOpenAI, ModelPattern: "gpt-*", InputPrice: floatPtr(1e-6), Status: ModelPricingStatusActive},
		{Adapter: "codex", ModelPattern: "gpt-5.6-sol", InputPrice: floatPtr(3e-6), Status: ModelPricingStatusActive},
	}})

	got, err := catalog.ResolveForPricingInput(context.Background(), PricingInput{
		Adapter:      PlatformOpenAI,
		PlatformCode: "codex",
		PublicModel:  "gpt-5.6-sol",
		Model:        "gpt-5.6-upstream",
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.InDelta(t, 3e-6, *got.InputPrice, 1e-12)
}

func TestModelPricingCatalogKeepsLegacyAdapterRulesWhenPlatformOverrideMissing(t *testing.T) {
	catalog := NewModelPricingCatalog(&modelPricingOverrideRepoStub{rules: []ModelPricingOverride{
		{Adapter: PlatformOpenAI, ModelPattern: "gpt-5.6-upstream", InputPrice: floatPtr(2e-6), Status: ModelPricingStatusActive},
	}})

	got, err := catalog.ResolveForPricingInput(context.Background(), PricingInput{
		Adapter:      PlatformOpenAI,
		PlatformCode: "codex",
		PublicModel:  "gpt-5.6-sol",
		Model:        "gpt-5.6-upstream",
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.InDelta(t, 2e-6, *got.InputPrice, 1e-12)
}

func TestPricingOverrideToResolvedPreservesExplicitZeroAndIntervals(t *testing.T) {
	zero := 0.0
	intervalPrice := 3e-6
	base := &ModelPricing{InputPricePerToken: 9e-6, SupportsCacheBreakdown: true}
	resolved := pricingOverrideToResolved(&ModelPricingOverride{
		BillingMode:     BillingModeToken,
		CacheWritePrice: &zero,
		Intervals: []domain.ModelPricingInterval{{
			MinTokens: 0, InputPrice: &intervalPrice,
		}},
	}, base, PricingSourceInfo{Type: PricingSourceBundledCatalog})

	require.Equal(t, PricingSourceOverride, resolved.Source)
	require.InDelta(t, 9e-6, resolved.OfficialPricing.InputPricePerToken, 1e-12)
	require.Equal(t, PricingSourceBundledCatalog, resolved.OfficialSource.Type)
	require.True(t, resolved.BasePricing.CacheCreationPriceExplicit)
	require.Zero(t, resolved.BasePricing.CacheCreation5mPrice)
	require.Len(t, resolved.Intervals, 1)
	require.InDelta(t, intervalPrice, *resolved.Intervals[0].InputPrice, 1e-12)
}

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
	require.NotNil(t, resolved.MatchedOverride)
}

func TestResolverPreservesExplicitZeroWithoutChangingOfficialPricing(t *testing.T) {
	zero := 0.0
	official := &ModelPricing{InputPricePerToken: 5e-6, OutputPricePerToken: 30e-6}

	resolved := pricingOverrideToResolved(&ModelPricingOverride{
		InputPrice: &zero,
	}, official, PricingSourceInfo{Type: PricingSourceRemoteCatalog})

	require.InDelta(t, 5e-6, resolved.OfficialPricing.InputPricePerToken, 1e-12)
	require.Zero(t, resolved.BasePricing.InputPricePerToken)
	require.InDelta(t, 30e-6, resolved.BasePricing.OutputPricePerToken, 1e-12)
}

func TestModelPricingCatalogRejectsOverlappingIntervals(t *testing.T) {
	max := 2000
	inputPrice := 1e-6
	catalog := NewModelPricingCatalog(&modelPricingOverrideRepoStub{})

	_, err := catalog.Create(context.Background(), ModelPricingOverride{
		Adapter:      "codex",
		ModelPattern: "gpt-5.6-sol",
		BillingMode:  BillingModeToken,
		Intervals: []domain.ModelPricingInterval{
			{MinTokens: 0, MaxTokens: &max, InputPrice: &inputPrice},
			{MinTokens: 1000, InputPrice: &inputPrice},
		},
	})

	require.ErrorContains(t, err, "overlap")
}

func TestModelPricingCatalogSnapshotLoadsRulesOnce(t *testing.T) {
	repo := &modelPricingOverrideRepoStub{rules: []ModelPricingOverride{
		{Adapter: "codex", ModelPattern: "gpt-*", InputPrice: floatPtr(1e-6), Status: ModelPricingStatusActive},
		{Adapter: PlatformOpenAI, ModelPattern: "gpt-5.6-upstream", InputPrice: floatPtr(2e-6), Status: ModelPricingStatusActive},
	}}
	catalog := NewModelPricingCatalog(repo)

	snapshot, err := catalog.LoadSnapshot(context.Background())
	require.NoError(t, err)
	first := snapshot.ResolveForPricingInput(PricingInput{PlatformCode: "codex", PublicModel: "gpt-5.6-sol"})
	second := snapshot.ResolveForPricingInput(PricingInput{Adapter: PlatformOpenAI, Model: "gpt-5.6-upstream"})

	require.NotNil(t, first)
	require.NotNil(t, second)
	require.Equal(t, 1, repo.calls)
}

func floatPtr(value float64) *float64 { return &value }
