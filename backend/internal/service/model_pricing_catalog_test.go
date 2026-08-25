//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type modelPricingOverrideRepoStub struct {
	rules    []ModelPricingOverride
	calls    int
	err      error
	upserted *ModelPricingOverride
}

func (s *modelPricingOverrideRepoStub) List(context.Context, string) ([]ModelPricingOverride, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
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

func (s *modelPricingOverrideRepoStub) Upsert(_ context.Context, override *ModelPricingOverride) error {
	copy := *override
	s.upserted = &copy
	override.ID = 91
	return s.err
}

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

func TestModelPricingCatalogRejectsOverlappingPerRequestAndImageIntervals(t *testing.T) {
	max := 2000
	price := 0.04
	for _, mode := range []BillingMode{BillingModePerRequest, BillingModeImage} {
		t.Run(string(mode), func(t *testing.T) {
			catalog := NewModelPricingCatalog(&modelPricingOverrideRepoStub{})

			_, err := catalog.Create(context.Background(), ModelPricingOverride{
				Adapter:      "image-adapter",
				ModelPattern: "image-model",
				BillingMode:  mode,
				Intervals: []domain.ModelPricingInterval{
					{MinTokens: 0, MaxTokens: &max, TierLabel: "SD", PerRequestPrice: &price},
					{MinTokens: 1000, TierLabel: "HD", PerRequestPrice: &price},
				},
			})

			require.ErrorContains(t, err, "overlap")
		})
	}
}

func TestValidateIntervalsRejectsUnboundedNonFinalPerRequestAndImageIntervals(t *testing.T) {
	max := 2000
	price := 0.04
	intervals := []PricingInterval{
		{MinTokens: 0, TierLabel: "SD", PerRequestPrice: &price},
		{MinTokens: 1000, MaxTokens: &max, TierLabel: "HD", PerRequestPrice: &price},
	}

	for _, mode := range []BillingMode{BillingModePerRequest, BillingModeImage} {
		t.Run(string(mode), func(t *testing.T) {
			err := ValidateIntervals(intervals, mode)

			require.ErrorContains(t, err, "unbounded interval")
		})
	}
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

func TestResolverReturnsPricingRepositoryError(t *testing.T) {
	repoErr := errors.New("database unavailable")
	resolver := NewModelPricingResolverWithCatalog(
		newTestBillingService(),
		NewModelPricingCatalog(&modelPricingOverrideRepoStub{err: repoErr}),
	)

	resolved, err := resolver.Resolve(context.Background(), PricingInput{
		Adapter: "openai", PlatformCode: "codex", PublicModel: "gpt-5.6-sol", Model: "gpt-5.6-sol",
	})

	require.Nil(t, resolved)
	require.ErrorContains(t, err, "database unavailable")
}

func TestModelPricingCatalogUpsertPlatformSaleForcesPlatformCodeAndEnabledModel(t *testing.T) {
	repo := &modelPricingOverrideRepoStub{}
	catalog := NewModelPricingCatalog(repo)
	platform := &Platform{
		ID: 7, Code: "Codex", Status: PlatformStatusActive,
		ModelRules: []PlatformModelRule{{ModelPattern: "gpt-5.6-sol", Enabled: true}},
	}
	inputPrice := 6e-6

	result, err := catalog.UpsertPlatformSale(context.Background(), platform, "GPT-5.6-SOL", ModelPricingOverride{
		Adapter: "untrusted", ModelPattern: "other", BillingMode: BillingModeToken,
		InputPrice: &inputPrice, Status: ModelPricingStatusActive,
	})

	require.NoError(t, err)
	require.Equal(t, int64(91), result.ID)
	require.Equal(t, "codex", repo.upserted.Adapter)
	require.Equal(t, "gpt-5.6-sol", repo.upserted.ModelPattern)
}

func TestModelPricingCatalogUpsertPlatformSaleReusesExistingPatternCase(t *testing.T) {
	repo := &modelPricingOverrideRepoStub{rules: []ModelPricingOverride{{
		Adapter: "codex", ModelPattern: "GPT-5.6-SOL", Status: ModelPricingStatusActive,
	}}}
	catalog := NewModelPricingCatalog(repo)
	platform := &Platform{
		ID: 7, Code: "codex", Status: PlatformStatusActive,
		ModelRules: []PlatformModelRule{{ModelPattern: "gpt-5.6-sol", Enabled: true}},
	}

	result, err := catalog.UpsertPlatformSale(context.Background(), platform, "gpt-5.6-sol", ModelPricingOverride{
		BillingMode: BillingModeToken,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "GPT-5.6-SOL", repo.upserted.ModelPattern)
}

func TestModelPricingCatalogUpsertPlatformSaleRejectsDisabledModel(t *testing.T) {
	repo := &modelPricingOverrideRepoStub{}
	catalog := NewModelPricingCatalog(repo)
	platform := &Platform{
		ID: 7, Code: "codex", Status: PlatformStatusActive,
		ModelRules: []PlatformModelRule{{ModelPattern: "gpt-5.6-sol", Enabled: false}},
	}

	result, err := catalog.UpsertPlatformSale(context.Background(), platform, "gpt-5.6-sol", ModelPricingOverride{})

	require.Nil(t, result)
	require.ErrorContains(t, err, "not enabled")
	require.Nil(t, repo.upserted)
}

func TestModelPricingCatalogGetExactIgnoresDisabledRules(t *testing.T) {
	catalog := NewModelPricingCatalog(&modelPricingOverrideRepoStub{rules: []ModelPricingOverride{
		{Adapter: "codex", ModelPattern: "gpt-5.6-sol", Status: "disabled"},
	}})

	result, err := catalog.GetExact(context.Background(), "codex", "gpt-5.6-sol")

	require.NoError(t, err)
	require.Nil(t, result)
}

func TestApplyOverridePricesMarksExplicitZeroValues(t *testing.T) {
	zero := 0.0
	pricing := &ModelPricing{}
	override := &ModelPricingOverride{
		InputPrice: &zero, OutputPrice: &zero,
		CacheWritePrice: &zero, CacheReadPrice: &zero,
		ImageInputPrice: &zero, ImageOutputPrice: &zero,
	}

	applyOverridePrices(override, pricing)

	require.True(t, pricing.InputPriceExplicit)
	require.True(t, pricing.OutputPriceExplicit)
	require.True(t, pricing.CacheCreationPriceExplicit)
	require.True(t, pricing.CacheReadPriceExplicit)
	require.True(t, pricing.ImageInputPriceExplicit)
	require.True(t, pricing.ImageOutputPriceExplicit)
}

func TestResolverKeepsAndInheritsOfficialPerRequestPrice(t *testing.T) {
	officialPrice := 0.04
	pricingService := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"image-model": {
				OutputCostPerImage:         officialPrice,
				OutputCostPerImageExplicit: true,
				TokenPricingAbsent:         true,
			},
		},
		pricingSources: map[string]PricingSourceInfo{
			"image-model": {Type: PricingSourceBundledCatalog, MatchedModel: "image-model"},
		},
	}
	billing := NewBillingService(&config.Config{}, pricingService)
	catalog := NewModelPricingCatalog(&modelPricingOverrideRepoStub{rules: []ModelPricingOverride{{
		Adapter: "gemini", ModelPattern: "image-model", BillingMode: BillingModeImage,
		Status: ModelPricingStatusActive,
	}}})

	resolved, err := NewModelPricingResolverWithCatalog(billing, catalog).Resolve(context.Background(), PricingInput{
		Adapter: "gemini", Model: "image-model",
	})

	require.NoError(t, err)
	require.InDelta(t, officialPrice, resolved.OfficialDefaultPerRequestPrice, 1e-12)
	require.True(t, resolved.OfficialDefaultPerRequestPriceExplicit)
	require.InDelta(t, officialPrice, resolved.DefaultPerRequestPrice, 1e-12)
	require.True(t, resolved.DefaultPerRequestPriceExplicit)
}

func TestResolverKeepsOfficialModeWhenSaleOverrideDiffers(t *testing.T) {
	officialPrice := 0.04
	inputPrice, outputPrice := 1e-6, 2e-6
	cacheWritePrice, cacheReadPrice := 1.25e-6, 0.1e-6
	pricingService := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"image-model": {OutputCostPerImage: officialPrice, TokenPricingAbsent: true},
		},
		pricingSources: map[string]PricingSourceInfo{
			"image-model": {Type: PricingSourceBundledCatalog, MatchedModel: "image-model"},
		},
	}
	resolver := NewModelPricingResolverWithCatalog(
		NewBillingService(&config.Config{}, pricingService),
		NewModelPricingCatalog(&modelPricingOverrideRepoStub{rules: []ModelPricingOverride{{
			Adapter: "gemini", ModelPattern: "image-model", BillingMode: BillingModeToken,
			InputPrice: &inputPrice, OutputPrice: &outputPrice,
			CacheWritePrice: &cacheWritePrice, CacheReadPrice: &cacheReadPrice,
			Status: ModelPricingStatusActive,
		}}}),
	)

	resolved, err := resolver.Resolve(context.Background(), PricingInput{Adapter: "gemini", Model: "image-model"})

	require.NoError(t, err)
	require.Equal(t, BillingModeToken, resolved.Mode)
	require.Equal(t, BillingModeImage, resolved.OfficialMode)
	require.Empty(t, resolved.OfficialIntervals)
	require.Empty(t, resolved.OfficialRequestTiers)
}

func TestResolverMarksUnavailableOfficialSourceForCompleteCustomSale(t *testing.T) {
	inputPrice, outputPrice := 1e-6, 2e-6
	cacheWritePrice, cacheReadPrice := 1.25e-6, 0.1e-6
	catalog := NewModelPricingCatalog(&modelPricingOverrideRepoStub{rules: []ModelPricingOverride{{
		Adapter: "custom", ModelPattern: "private-model", BillingMode: BillingModeToken,
		InputPrice: &inputPrice, OutputPrice: &outputPrice,
		CacheWritePrice: &cacheWritePrice, CacheReadPrice: &cacheReadPrice,
		Status: ModelPricingStatusActive,
	}}})

	resolved, err := NewModelPricingResolverWithCatalog(newTestBillingService(), catalog).Resolve(
		context.Background(), PricingInput{Adapter: "custom", Model: "private-model"},
	)

	require.NoError(t, err)
	require.Equal(t, PricingSourceUnavailable, resolved.OfficialSource.Type)
	require.Equal(t, "private-model", resolved.OfficialSource.MatchedModel)
}

func floatPtr(value float64) *float64 { return &value }
