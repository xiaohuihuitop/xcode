//go:build unit

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type modelPricingHandlerManagementStub struct {
	exact          *service.ModelPricingOverride
	upserted       service.ModelPricingOverride
	upsertPlatform *service.Platform
	upsertModel    string
}

func (s *modelPricingHandlerManagementStub) List(context.Context, string) ([]service.ModelPricingOverride, error) {
	return nil, nil
}
func (s *modelPricingHandlerManagementStub) Get(context.Context, int64) (*service.ModelPricingOverride, error) {
	return nil, service.ErrModelPricingOverrideNotFound
}
func (s *modelPricingHandlerManagementStub) Create(context.Context, service.ModelPricingOverride) (*service.ModelPricingOverride, error) {
	return nil, nil
}
func (s *modelPricingHandlerManagementStub) Update(context.Context, int64, service.ModelPricingOverride) (*service.ModelPricingOverride, error) {
	return nil, nil
}
func (s *modelPricingHandlerManagementStub) Delete(context.Context, int64) error { return nil }
func (s *modelPricingHandlerManagementStub) GetExact(context.Context, string, string) (*service.ModelPricingOverride, error) {
	return s.exact, nil
}
func (s *modelPricingHandlerManagementStub) UpsertPlatformSale(_ context.Context, platform *service.Platform, model string, input service.ModelPricingOverride) (*service.ModelPricingOverride, error) {
	s.upsertPlatform = platform
	s.upsertModel = model
	s.upserted = input
	input.ID = 41
	input.Adapter = platform.Code
	input.ModelPattern = model
	return &input, nil
}

type modelPricingHandlerCatalogStub struct {
	items []service.PlatformCatalogPlatform
}

func (s *modelPricingHandlerCatalogStub) ListPricingCatalog(context.Context) ([]service.PlatformCatalogPlatform, error) {
	return s.items, nil
}

type modelPricingHandlerPlatformStub struct {
	platform *service.Platform
}

func (s *modelPricingHandlerPlatformStub) GetByID(context.Context, int64) (*service.Platform, error) {
	return s.platform, nil
}

func setupModelPricingHandlerRouter(
	pricing modelPricingManagementService,
	catalog modelPricingPlatformCatalogService,
	platforms modelPricingPlatformService,
) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewModelPricingHandler(pricing, catalog, platforms)
	router.GET("/api/v1/admin/model-pricing/catalog", handler.Catalog)
	router.PUT("/api/v1/admin/model-pricing/platform-sale", handler.UpsertPlatformSale)
	return router
}

func TestModelPricingHandlerCatalogReturnsOfficialAndSalePricing(t *testing.T) {
	officialInput := 5e-6
	saleInput := 6e-6
	override := &service.ModelPricingOverride{
		ID: 12, Adapter: "codex", ModelPattern: "gpt-5.6-sol",
		BillingMode: service.BillingModeToken, InputPrice: &saleInput,
		Status: service.ModelPricingStatusActive,
	}
	router := setupModelPricingHandlerRouter(
		&modelPricingHandlerManagementStub{},
		&modelPricingHandlerCatalogStub{items: []service.PlatformCatalogPlatform{{
			ID: 7, Code: "codex", Name: "Codex", AccountPlatform: service.PlatformOpenAI,
			Models: []service.PlatformCatalogModel{{
				Pattern: "gpt-5.6-sol", UpstreamModel: "gpt-5.6-sol-upstream",
				Pricing: &service.ResolvedPricing{
					Mode:            service.BillingModeToken,
					OfficialPricing: &service.ModelPricing{InputPricePerToken: officialInput},
					OfficialSource: service.PricingSourceInfo{
						Type: service.PricingSourceBundledCatalog, Name: "Bundled pricing catalog", MatchedModel: "gpt-5.6-sol",
					},
					BasePricing:     &service.ModelPricing{InputPricePerToken: saleInput},
					MatchedOverride: override,
					Source:          service.PricingSourceOverride,
				},
			}},
		}}},
		&modelPricingHandlerPlatformStub{},
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/model-pricing/catalog", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Len(t, payload.Data, 1)
	row := payload.Data[0]
	require.Equal(t, "codex", row["platform_code"])
	require.Equal(t, "gpt-5.6-sol", row["model_pattern"])
	require.NotNil(t, row["official_pricing"])
	require.NotNil(t, row["official_source"])
	require.NotNil(t, row["sale_pricing"])
	require.Equal(t, "custom", row["sale_source"])
	require.NotNil(t, row["override"])
}

func TestModelPricingHandlerUpsertsPlatformSaleUsingPlatformCode(t *testing.T) {
	pricing := &modelPricingHandlerManagementStub{}
	platform := &service.Platform{
		ID: 7, Code: "codex", Name: "Codex", AccountPlatform: service.PlatformOpenAI,
		Status:     service.PlatformStatusActive,
		ModelRules: []service.PlatformModelRule{{ModelPattern: "gpt-5.6-sol", Enabled: true}},
	}
	router := setupModelPricingHandlerRouter(
		pricing,
		&modelPricingHandlerCatalogStub{},
		&modelPricingHandlerPlatformStub{platform: platform},
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/model-pricing/platform-sale", bytes.NewBufferString(`{
		"platform_id":7,"model_pattern":"gpt-5.6-sol","billing_mode":"token",
		"input_price":0.000006,"output_price":0.000036,"cache_write_price":0,"cache_read_price":0,"status":"active"
	}`))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Same(t, platform, pricing.upsertPlatform)
	require.Equal(t, "gpt-5.6-sol", pricing.upsertModel)
	require.Equal(t, service.BillingModeToken, pricing.upserted.BillingMode)
	require.NotNil(t, pricing.upserted.CacheWritePrice)
	require.Zero(t, *pricing.upserted.CacheWritePrice)
	require.Contains(t, recorder.Body.String(), `"adapter":"codex"`)
}

func TestOfficialPricingValuesPreservesExplicitZero(t *testing.T) {
	values := officialPricingValues(&service.ResolvedPricing{
		OfficialPricing: &service.ModelPricing{
			OutputPricePerToken: 0, OutputPriceExplicit: true,
			CacheReadPricePerToken: 0, CacheReadPriceExplicit: true,
		},
		OfficialDefaultPerRequestPrice:         0,
		OfficialDefaultPerRequestPriceExplicit: true,
	})

	require.NotNil(t, values)
	require.NotNil(t, values.OutputPrice)
	require.Zero(t, *values.OutputPrice)
	require.NotNil(t, values.CacheReadPrice)
	require.Zero(t, *values.CacheReadPrice)
	require.NotNil(t, values.PerRequestPrice)
	require.Zero(t, *values.PerRequestPrice)
}

func TestModelPricingHandlerCatalogMarksIncompleteCustomSaleUnavailable(t *testing.T) {
	inputPrice := 1e-6
	router := setupModelPricingHandlerRouter(
		&modelPricingHandlerManagementStub{},
		&modelPricingHandlerCatalogStub{items: []service.PlatformCatalogPlatform{{
			ID: 7, Code: "codex", Name: "Codex", AccountPlatform: service.PlatformOpenAI,
			Models: []service.PlatformCatalogModel{{
				Pattern: "private-model",
				Pricing: &service.ResolvedPricing{
					Mode:        service.BillingModeToken,
					BasePricing: &service.ModelPricing{InputPricePerToken: inputPrice},
					MatchedOverride: &service.ModelPricingOverride{
						Adapter: "codex", ModelPattern: "private-model", BillingMode: service.BillingModeToken,
						InputPrice: &inputPrice, Status: service.ModelPricingStatusActive,
					},
					OfficialSource: service.PricingSourceInfo{Type: service.PricingSourceUnavailable},
					Source:         service.PricingSourceOverride,
				},
			}},
		}}},
		&modelPricingHandlerPlatformStub{},
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/model-pricing/catalog", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, "unavailable", payload.Data[0]["sale_source"])
	require.Nil(t, payload.Data[0]["sale_pricing"])
	require.NotNil(t, payload.Data[0]["override"])
}
