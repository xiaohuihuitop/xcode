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
}

func (s *modelPricingOverrideRepoStub) List(context.Context, string) ([]ModelPricingOverride, error) {
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
	}, base)

	require.Equal(t, PricingSourceOverride, resolved.Source)
	require.True(t, resolved.BasePricing.CacheCreationPriceExplicit)
	require.Zero(t, resolved.BasePricing.CacheCreation5mPrice)
	require.Len(t, resolved.Intervals, 1)
	require.InDelta(t, intervalPrice, *resolved.Intervals[0].InputPrice, 1e-12)
}

func floatPtr(value float64) *float64 { return &value }
