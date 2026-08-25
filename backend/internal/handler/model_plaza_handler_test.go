//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelPlazaHandler_NilSettingServiceFailsClosed404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &ModelPlazaHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/model-plaza", nil)

	h.Get(c)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestModelPlazaHandlerReturnsPlatformCatalogWithoutLegacyFields(t *testing.T) {
	settings := modelPlazaSettingsStub{runtime: service.ModelPlazaRuntime{Enabled: true}}
	catalog := service.NewPlatformCatalogService(platformCatalogPlatformRepoStubForHandler{
		platforms: []service.Platform{{
			ID: 7, Code: "openai", Name: "OpenAI", AccountPlatform: service.PlatformOpenAI,
			Status:     service.PlatformStatusActive,
			ModelRules: []service.PlatformModelRule{{ModelPattern: "gpt-*", Enabled: true}},
		}},
	}, nil)
	h := NewModelPlazaHandler(catalog, settings)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/model-plaza", h.Get)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/model-plaza", nil))

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"platforms"`)
	require.NotContains(t, w.Body.String(), `"groups"`)
	require.NotContains(t, w.Body.String(), `"group_id"`)
	require.NotContains(t, w.Body.String(), `"channel_id"`)
}

func TestModelPlazaHandlerReturnsDualPricingWithoutAdminSourceDetails(t *testing.T) {
	officialInput, saleInput, explicitZero := 5e-6, 6e-6, 0.0
	officialOutput, officialCacheWrite, officialCacheRead := 30e-6, 6.25e-6, 0.5e-6
	saleCacheWrite, saleCacheRead, intervalInput := 7.5e-6, 0.6e-6, 8e-6
	maxTokens := 128000
	settings := modelPlazaSettingsStub{runtime: service.ModelPlazaRuntime{Enabled: true}}
	catalog := service.NewPlatformCatalogService(platformCatalogPlatformRepoStubForHandler{
		platforms: []service.Platform{{
			ID: 7, Code: "codex", Name: "Codex", AccountPlatform: service.PlatformOpenAI,
			Status:     service.PlatformStatusActive,
			ModelRules: []service.PlatformModelRule{{ModelPattern: "gpt-5.6-sol", Enabled: true}},
		}},
	}, modelPlazaPricingResolverStub{resolved: &service.ResolvedPricing{
		Mode: service.BillingModeToken,
		OfficialPricing: &service.ModelPricing{
			InputPricePerToken: officialInput, OutputPricePerToken: officialOutput,
			CacheCreationPricePerToken: officialCacheWrite, CacheReadPricePerToken: officialCacheRead,
		},
		BasePricing: &service.ModelPricing{
			InputPricePerToken: saleInput, OutputPriceExplicit: true,
			CacheCreationPricePerToken: saleCacheWrite, CacheReadPricePerToken: saleCacheRead,
		},
		Intervals: []service.PricingInterval{{
			MinTokens: 0, MaxTokens: &maxTokens, InputPrice: &intervalInput, CacheWritePrice: &explicitZero,
		}},
		MatchedOverride: &service.ModelPricingOverride{
			ID: 91, InputPrice: &saleInput, OutputPrice: &explicitZero,
			CacheWritePrice: &saleCacheWrite, CacheReadPrice: &saleCacheRead,
		},
	}})
	h := NewModelPlazaHandler(catalog, settings)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/model-plaza", h.Get)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/model-plaza", nil))

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data struct {
			Platforms []struct {
				Models []struct {
					Pricing           modelPlazaPricing `json:"pricing"`
					OfficialPricing   modelPlazaPricing `json:"official_pricing"`
					SalePricing       modelPlazaPricing `json:"sale_pricing"`
					SalePricingSource string            `json:"sale_pricing_source"`
				} `json:"models"`
			} `json:"platforms"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	model := body.Data.Platforms[0].Models[0]
	require.NotNil(t, model.Pricing.InputPrice)
	require.NotNil(t, model.OfficialPricing.InputPrice)
	require.NotNil(t, model.SalePricing.InputPrice)
	require.Equal(t, saleInput, *model.Pricing.InputPrice)
	require.Equal(t, officialInput, *model.OfficialPricing.InputPrice)
	require.Equal(t, officialOutput, *model.OfficialPricing.OutputPrice)
	require.Equal(t, officialCacheWrite, *model.OfficialPricing.CacheWritePrice)
	require.Equal(t, officialCacheRead, *model.OfficialPricing.CacheReadPrice)
	require.Equal(t, saleInput, *model.SalePricing.InputPrice)
	require.NotNil(t, model.SalePricing.OutputPrice)
	require.Zero(t, *model.SalePricing.OutputPrice)
	require.Equal(t, saleCacheWrite, *model.SalePricing.CacheWritePrice)
	require.Equal(t, saleCacheRead, *model.SalePricing.CacheReadPrice)
	require.Len(t, model.Pricing.Intervals, 1)
	require.Len(t, model.SalePricing.Intervals, 1)
	require.Equal(t, intervalInput, *model.SalePricing.Intervals[0].InputPrice)
	require.NotNil(t, model.SalePricing.Intervals[0].CacheWritePrice)
	require.Zero(t, *model.SalePricing.Intervals[0].CacheWritePrice)
	require.Equal(t, "custom", model.SalePricingSource)
	for _, forbidden := range []string{
		"matched_override", "MatchedOverride",
		"source_url", "SourceURL",
		"fallback_file", "FallbackFile",
		"rule_id", "RuleID",
		"official_source", "OfficialSource",
	} {
		require.NotContains(t, w.Body.String(), forbidden)
	}
}

func TestModelPlazaHandlerMapsIndependentOfficialModeAndRequestTiers(t *testing.T) {
	zero, officialImage, saleImage := 0.0, 0.04, 0.05
	officialTierPrice, saleIntervalPrice, ignoredSaleTierPrice := 0.07, 8e-6, 0.08
	saleOutputPrice, saleCacheWritePrice, saleCacheReadPrice := 30e-6, 6.25e-6, 0.5e-6
	settings := modelPlazaSettingsStub{runtime: service.ModelPlazaRuntime{Enabled: true}}
	catalog := service.NewPlatformCatalogService(platformCatalogPlatformRepoStubForHandler{
		platforms: []service.Platform{{
			ID: 8, Code: "image", Name: "Image", AccountPlatform: service.PlatformOpenAI,
			Status:     service.PlatformStatusActive,
			ModelRules: []service.PlatformModelRule{{ModelPattern: "image-model", Enabled: true}},
		}},
	}, modelPlazaPricingResolverStub{resolved: &service.ResolvedPricing{
		Mode:         service.BillingModeToken,
		OfficialMode: service.BillingModeImage,
		OfficialPricing: &service.ModelPricing{
			ImageInputPricePerToken: zero, ImageInputPriceExplicit: true,
			ImageOutputPricePerToken: officialImage, ImageOutputPriceExplicit: true,
		},
		OfficialDefaultPerRequestPrice: officialImage, OfficialDefaultPerRequestPriceExplicit: true,
		OfficialRequestTiers: []service.PricingInterval{{
			TierLabel: "official-hd", PerRequestPrice: &officialTierPrice,
		}},
		BasePricing: &service.ModelPricing{
			InputPricePerToken: saleIntervalPrice, InputPriceExplicit: true,
			OutputPricePerToken: saleOutputPrice, OutputPriceExplicit: true,
			CacheCreationPricePerToken: saleCacheWritePrice, CacheCreationPriceExplicit: true,
			CacheReadPricePerToken: saleCacheReadPrice, CacheReadPriceExplicit: true,
			ImageInputPricePerToken: zero, ImageInputPriceExplicit: true,
			ImageOutputPricePerToken: saleImage, ImageOutputPriceExplicit: true,
		},
		DefaultPerRequestPrice: saleImage, DefaultPerRequestPriceExplicit: true,
		Intervals: []service.PricingInterval{{
			TierLabel: "sale-context", InputPrice: &saleIntervalPrice,
		}},
		RequestTiers: []service.PricingInterval{{
			TierLabel: "ignored-sale-request", PerRequestPrice: &ignoredSaleTierPrice,
		}},
		MatchedOverride: &service.ModelPricingOverride{
			BillingMode: service.BillingModeToken,
			InputPrice:  &saleIntervalPrice, OutputPrice: &saleOutputPrice,
			CacheWritePrice: &saleCacheWritePrice, CacheReadPrice: &saleCacheReadPrice,
			ImageInputPrice: &zero, ImageOutputPrice: &saleImage, PerRequestPrice: &saleImage,
		},
	}})
	h := NewModelPlazaHandler(catalog, settings)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/model-plaza", h.Get)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/model-plaza", nil))

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data struct {
			Platforms []struct {
				Models []struct {
					OfficialPricing modelPlazaPricing `json:"official_pricing"`
					SalePricing     modelPlazaPricing `json:"sale_pricing"`
				} `json:"models"`
			} `json:"platforms"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	model := body.Data.Platforms[0].Models[0]
	require.Equal(t, string(service.BillingModeImage), model.OfficialPricing.BillingMode)
	require.Equal(t, string(service.BillingModeToken), model.SalePricing.BillingMode)
	require.NotNil(t, model.OfficialPricing.ImageInputPrice)
	require.NotNil(t, model.OfficialPricing.ImageOutputPrice)
	require.NotNil(t, model.OfficialPricing.PerRequestPrice)
	require.NotNil(t, model.SalePricing.ImageInputPrice)
	require.NotNil(t, model.SalePricing.ImageOutputPrice)
	require.NotNil(t, model.SalePricing.PerRequestPrice)
	require.Equal(t, float64(0), *model.OfficialPricing.ImageInputPrice)
	require.Equal(t, officialImage, *model.OfficialPricing.ImageOutputPrice)
	require.Equal(t, officialImage, *model.OfficialPricing.PerRequestPrice)
	require.Equal(t, float64(0), *model.SalePricing.ImageInputPrice)
	require.Equal(t, saleImage, *model.SalePricing.ImageOutputPrice)
	require.Equal(t, saleImage, *model.SalePricing.PerRequestPrice)
	require.Len(t, model.OfficialPricing.Intervals, 1)
	require.Equal(t, "official-hd", model.OfficialPricing.Intervals[0].TierLabel)
	require.Equal(t, officialTierPrice, *model.OfficialPricing.Intervals[0].PerRequestPrice)
	require.Len(t, model.SalePricing.Intervals, 1)
	require.Equal(t, "sale-context", model.SalePricing.Intervals[0].TierLabel)
	require.Equal(t, saleIntervalPrice, *model.SalePricing.Intervals[0].InputPrice)
}

func TestModelPlazaHandlerReturnsNilDualPricingWhenUnavailable(t *testing.T) {
	settings := modelPlazaSettingsStub{runtime: service.ModelPlazaRuntime{Enabled: true}}
	catalog := service.NewPlatformCatalogService(platformCatalogPlatformRepoStubForHandler{
		platforms: []service.Platform{{
			ID: 9, Code: "private", Name: "Private", AccountPlatform: service.PlatformOpenAI,
			Status:     service.PlatformStatusActive,
			ModelRules: []service.PlatformModelRule{{ModelPattern: "unknown-model", Enabled: true}},
		}},
	}, modelPlazaPricingResolverStub{resolved: &service.ResolvedPricing{
		OfficialSource: service.PricingSourceInfo{Type: service.PricingSourceUnavailable},
		Source:         string(service.PricingSourceUnavailable),
	}})
	h := NewModelPlazaHandler(catalog, settings)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/model-plaza", h.Get)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/model-plaza", nil))

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Data struct {
			Platforms []struct {
				Models []struct {
					Pricing           *modelPlazaPricing `json:"pricing"`
					OfficialPricing   *modelPlazaPricing `json:"official_pricing"`
					SalePricing       *modelPlazaPricing `json:"sale_pricing"`
					SalePricingSource string             `json:"sale_pricing_source"`
				} `json:"models"`
			} `json:"platforms"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	model := body.Data.Platforms[0].Models[0]
	require.Nil(t, model.Pricing)
	require.Nil(t, model.OfficialPricing)
	require.Nil(t, model.SalePricing)
	require.Equal(t, "unavailable", model.SalePricingSource)
}

type platformCatalogPlatformRepoStubForHandler struct {
	platforms []service.Platform
}

type modelPlazaPricingResolverStub struct {
	resolved *service.ResolvedPricing
	err      error
}

func (s modelPlazaPricingResolverStub) Resolve(context.Context, service.PricingInput) (*service.ResolvedPricing, error) {
	return s.resolved, s.err
}

func (s modelPlazaPricingResolverStub) ResolveBatch(_ context.Context, inputs []service.PricingInput) ([]*service.ResolvedPricing, error) {
	if s.err != nil {
		return nil, s.err
	}
	resolved := make([]*service.ResolvedPricing, len(inputs))
	for i := range resolved {
		resolved[i] = s.resolved
	}
	return resolved, nil
}

func (s platformCatalogPlatformRepoStubForHandler) List(_ context.Context) ([]service.Platform, error) {
	return append([]service.Platform(nil), s.platforms...), nil
}

type modelPlazaSettingsStub struct {
	runtime service.ModelPlazaRuntime
}

func (s modelPlazaSettingsStub) GetModelPlazaRuntime(context.Context) service.ModelPlazaRuntime {
	return s.runtime
}
