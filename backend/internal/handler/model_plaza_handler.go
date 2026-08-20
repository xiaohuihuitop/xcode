package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type modelPlazaSettings interface {
	GetModelPlazaRuntime(context.Context) service.ModelPlazaRuntime
}

// ModelPlazaHandler exposes the administrator-owned Platform catalog. The
// response deliberately contains no user, Group or Channel visibility data.
type ModelPlazaHandler struct {
	catalog  *service.PlatformCatalogService
	settings modelPlazaSettings
}

func NewModelPlazaHandler(catalog *service.PlatformCatalogService, settings modelPlazaSettings) *ModelPlazaHandler {
	return &ModelPlazaHandler{catalog: catalog, settings: settings}
}

type modelPlazaPricing struct {
	BillingMode      string                  `json:"billing_mode"`
	InputPrice       *float64                `json:"input_price"`
	OutputPrice      *float64                `json:"output_price"`
	CacheWritePrice  *float64                `json:"cache_write_price"`
	CacheReadPrice   *float64                `json:"cache_read_price"`
	ImageInputPrice  *float64                `json:"image_input_price"`
	ImageOutputPrice *float64                `json:"image_output_price"`
	PerRequestPrice  *float64                `json:"per_request_price"`
	Intervals        []modelPlazaPricingTier `json:"intervals"`
}

type modelPlazaPricingTier struct {
	MinTokens       int      `json:"min_tokens"`
	MaxTokens       *int     `json:"max_tokens"`
	TierLabel       string   `json:"tier_label,omitempty"`
	InputPrice      *float64 `json:"input_price"`
	OutputPrice     *float64 `json:"output_price"`
	CacheWritePrice *float64 `json:"cache_write_price"`
	CacheReadPrice  *float64 `json:"cache_read_price"`
	PerRequestPrice *float64 `json:"per_request_price"`
}

type modelPlazaModel struct {
	Pattern              string             `json:"pattern"`
	UpstreamModel        string             `json:"upstream_model,omitempty"`
	EndpointCapabilities []string           `json:"endpoint_capabilities"`
	Pricing              *modelPlazaPricing `json:"pricing"`
}

type modelPlazaPlatform struct {
	ID                   int64             `json:"id"`
	Code                 string            `json:"code"`
	Name                 string            `json:"name"`
	AccountPlatform      string            `json:"account_platform"`
	EndpointCapabilities []string          `json:"endpoint_capabilities"`
	Models               []modelPlazaModel `json:"models"`
}

type modelPlazaResponse struct {
	Description string               `json:"description"`
	Platforms   []modelPlazaPlatform `json:"platforms"`
}

func (h *ModelPlazaHandler) Get(c *gin.Context) {
	if h == nil || h.settings == nil || h.catalog == nil {
		response.NotFound(c, "Model plaza is not enabled")
		return
	}
	runtime := h.settings.GetModelPlazaRuntime(c.Request.Context())
	if !runtime.Enabled {
		response.NotFound(c, "Model plaza is not enabled")
		return
	}
	if runtime.RequireAuth && !hasAuthenticatedSubject(c) {
		response.Unauthorized(c, "Authentication required")
		return
	}
	platforms, err := h.catalog.ListPlaza(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]modelPlazaPlatform, 0, len(platforms))
	for i := range platforms {
		platform := &platforms[i]
		models := make([]modelPlazaModel, 0, len(platform.Models))
		for j := range platform.Models {
			model := &platform.Models[j]
			models = append(models, modelPlazaModel{
				Pattern:              model.Pattern,
				UpstreamModel:        model.UpstreamModel,
				EndpointCapabilities: append([]string(nil), model.EndpointCapabilities...),
				Pricing:              platformPricingResponse(model.Pricing),
			})
		}
		out = append(out, modelPlazaPlatform{
			ID:                   platform.ID,
			Code:                 platform.Code,
			Name:                 platform.Name,
			AccountPlatform:      platform.AccountPlatform,
			EndpointCapabilities: append([]string(nil), platform.EndpointCapabilities...),
			Models:               models,
		})
	}
	response.Success(c, modelPlazaResponse{Description: runtime.Description, Platforms: out})
}

func hasAuthenticatedSubject(c *gin.Context) bool {
	if c == nil {
		return false
	}
	_, ok := middleware.GetAuthSubjectFromContext(c)
	return ok
}

func platformPricingResponse(pricing *service.ResolvedPricing) *modelPlazaPricing {
	if pricing == nil {
		return nil
	}
	result := &modelPlazaPricing{BillingMode: string(pricing.Mode), Intervals: []modelPlazaPricingTier{}}
	if result.BillingMode == "" {
		result.BillingMode = string(service.BillingModeToken)
	}
	if base := pricing.BasePricing; base != nil {
		result.InputPrice = nonZeroFloatPointer(base.InputPricePerToken)
		result.OutputPrice = nonZeroFloatPointer(base.OutputPricePerToken)
		result.CacheWritePrice = nonZeroFloatPointer(base.CacheCreationPricePerToken)
		result.CacheReadPrice = nonZeroFloatPointer(base.CacheReadPricePerToken)
		result.ImageInputPrice = nonZeroFloatPointer(base.ImageInputPricePerToken)
		result.ImageOutputPrice = nonZeroFloatPointer(base.ImageOutputPricePerToken)
	}
	if pricing.DefaultPerRequestPrice > 0 {
		result.PerRequestPrice = &pricing.DefaultPerRequestPrice
	}
	for _, interval := range pricing.Intervals {
		result.Intervals = append(result.Intervals, modelPlazaPricingTier{
			MinTokens: interval.MinTokens, MaxTokens: interval.MaxTokens, TierLabel: interval.TierLabel,
			InputPrice: interval.InputPrice, OutputPrice: interval.OutputPrice,
			CacheWritePrice: interval.CacheWritePrice, CacheReadPrice: interval.CacheReadPrice,
			PerRequestPrice: interval.PerRequestPrice,
		})
	}
	return result
}

func nonZeroFloatPointer(value float64) *float64 {
	if value == 0 {
		return nil
	}
	return &value
}
