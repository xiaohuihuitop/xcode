package admin

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type modelPricingManagementService interface {
	List(ctx context.Context, adapter string) ([]service.ModelPricingOverride, error)
	Get(ctx context.Context, id int64) (*service.ModelPricingOverride, error)
	Create(ctx context.Context, input service.ModelPricingOverride) (*service.ModelPricingOverride, error)
	Update(ctx context.Context, id int64, input service.ModelPricingOverride) (*service.ModelPricingOverride, error)
	GetExact(ctx context.Context, adapter, modelPattern string) (*service.ModelPricingOverride, error)
	UpsertPlatformSale(ctx context.Context, platform *service.Platform, modelPattern string, input service.ModelPricingOverride) (*service.ModelPricingOverride, error)
	Delete(ctx context.Context, id int64) error
}

type modelPricingPlatformCatalogService interface {
	ListPricingCatalog(ctx context.Context) ([]service.PlatformCatalogPlatform, error)
}

type modelPricingPlatformService interface {
	GetByID(ctx context.Context, id int64) (*service.Platform, error)
}

// ModelPricingHandler exposes adapter/model price overrides without exposing
// Group or Channel concepts to the administrator.
type ModelPricingHandler struct {
	pricing   modelPricingManagementService
	catalog   modelPricingPlatformCatalogService
	platforms modelPricingPlatformService
}

func NewModelPricingHandler(
	pricing modelPricingManagementService,
	catalog modelPricingPlatformCatalogService,
	platforms modelPricingPlatformService,
) *ModelPricingHandler {
	return &ModelPricingHandler{pricing: pricing, catalog: catalog, platforms: platforms}
}

type modelPricingRequest struct {
	Adapter          string                        `json:"adapter" binding:"required,max=50"`
	ModelPattern     string                        `json:"model_pattern" binding:"required,max=100"`
	BillingMode      string                        `json:"billing_mode" binding:"omitempty,oneof=token per_request image"`
	InputPrice       *float64                      `json:"input_price" binding:"omitempty,min=0"`
	OutputPrice      *float64                      `json:"output_price" binding:"omitempty,min=0"`
	CacheWritePrice  *float64                      `json:"cache_write_price" binding:"omitempty,min=0"`
	CacheReadPrice   *float64                      `json:"cache_read_price" binding:"omitempty,min=0"`
	ImageInputPrice  *float64                      `json:"image_input_price" binding:"omitempty,min=0"`
	ImageOutputPrice *float64                      `json:"image_output_price" binding:"omitempty,min=0"`
	PerRequestPrice  *float64                      `json:"per_request_price" binding:"omitempty,min=0"`
	Intervals        []domain.ModelPricingInterval `json:"intervals"`
	Status           string                        `json:"status" binding:"omitempty,oneof=active disabled"`
}

func (r modelPricingRequest) toService() service.ModelPricingOverride {
	return service.ModelPricingOverride{
		Adapter:          r.Adapter,
		ModelPattern:     r.ModelPattern,
		BillingMode:      service.BillingMode(r.BillingMode),
		InputPrice:       r.InputPrice,
		OutputPrice:      r.OutputPrice,
		CacheWritePrice:  r.CacheWritePrice,
		CacheReadPrice:   r.CacheReadPrice,
		ImageInputPrice:  r.ImageInputPrice,
		ImageOutputPrice: r.ImageOutputPrice,
		PerRequestPrice:  r.PerRequestPrice,
		Intervals:        r.Intervals,
		Status:           r.Status,
	}
}

type modelPricingResponse struct {
	ID               int64                         `json:"id"`
	Adapter          string                        `json:"adapter"`
	ModelPattern     string                        `json:"model_pattern"`
	BillingMode      string                        `json:"billing_mode"`
	InputPrice       *float64                      `json:"input_price"`
	OutputPrice      *float64                      `json:"output_price"`
	CacheWritePrice  *float64                      `json:"cache_write_price"`
	CacheReadPrice   *float64                      `json:"cache_read_price"`
	ImageInputPrice  *float64                      `json:"image_input_price"`
	ImageOutputPrice *float64                      `json:"image_output_price"`
	PerRequestPrice  *float64                      `json:"per_request_price"`
	Intervals        []domain.ModelPricingInterval `json:"intervals"`
	Status           string                        `json:"status"`
}

type platformSalePricingRequest struct {
	PlatformID       int64                         `json:"platform_id" binding:"required,min=1"`
	ModelPattern     string                        `json:"model_pattern" binding:"required,max=100"`
	BillingMode      string                        `json:"billing_mode" binding:"omitempty,oneof=token per_request image"`
	InputPrice       *float64                      `json:"input_price" binding:"omitempty,min=0"`
	OutputPrice      *float64                      `json:"output_price" binding:"omitempty,min=0"`
	CacheWritePrice  *float64                      `json:"cache_write_price" binding:"omitempty,min=0"`
	CacheReadPrice   *float64                      `json:"cache_read_price" binding:"omitempty,min=0"`
	ImageInputPrice  *float64                      `json:"image_input_price" binding:"omitempty,min=0"`
	ImageOutputPrice *float64                      `json:"image_output_price" binding:"omitempty,min=0"`
	PerRequestPrice  *float64                      `json:"per_request_price" binding:"omitempty,min=0"`
	Intervals        []domain.ModelPricingInterval `json:"intervals"`
	Status           string                        `json:"status" binding:"omitempty,oneof=active disabled"`
}

func (r platformSalePricingRequest) toService() service.ModelPricingOverride {
	return service.ModelPricingOverride{
		BillingMode: service.BillingMode(r.BillingMode), InputPrice: r.InputPrice,
		OutputPrice: r.OutputPrice, CacheWritePrice: r.CacheWritePrice,
		CacheReadPrice: r.CacheReadPrice, ImageInputPrice: r.ImageInputPrice,
		ImageOutputPrice: r.ImageOutputPrice, PerRequestPrice: r.PerRequestPrice,
		Intervals: r.Intervals, Status: r.Status,
	}
}

type pricingValuesResponse struct {
	InputPrice       *float64                      `json:"input_price"`
	OutputPrice      *float64                      `json:"output_price"`
	CacheWritePrice  *float64                      `json:"cache_write_price"`
	CacheReadPrice   *float64                      `json:"cache_read_price"`
	ImageInputPrice  *float64                      `json:"image_input_price"`
	ImageOutputPrice *float64                      `json:"image_output_price"`
	PerRequestPrice  *float64                      `json:"per_request_price"`
	Intervals        []domain.ModelPricingInterval `json:"intervals"`
}

type modelPricingCatalogResponse struct {
	PlatformID      int64                         `json:"platform_id"`
	PlatformCode    string                        `json:"platform_code"`
	PlatformName    string                        `json:"platform_name"`
	AccountPlatform string                        `json:"account_platform"`
	ModelPattern    string                        `json:"model_pattern"`
	UpstreamModel   string                        `json:"upstream_model"`
	BillingMode     string                        `json:"billing_mode"`
	OfficialPricing *pricingValuesResponse        `json:"official_pricing"`
	OfficialSource  service.PricingSourceInfo     `json:"official_source"`
	SalePricing     *pricingValuesResponse        `json:"sale_pricing"`
	SaleSource      string                        `json:"sale_source"`
	Override        *modelPricingResponse         `json:"override"`
	Intervals       []domain.ModelPricingInterval `json:"intervals"`
}

func modelPricingResponseFromService(item *service.ModelPricingOverride) modelPricingResponse {
	if item == nil {
		return modelPricingResponse{Intervals: []domain.ModelPricingInterval{}}
	}
	intervals := append([]domain.ModelPricingInterval(nil), item.Intervals...)
	if intervals == nil {
		intervals = []domain.ModelPricingInterval{}
	}
	return modelPricingResponse{
		ID: item.ID, Adapter: item.Adapter, ModelPattern: item.ModelPattern,
		BillingMode: string(item.BillingMode), InputPrice: item.InputPrice,
		OutputPrice: item.OutputPrice, CacheWritePrice: item.CacheWritePrice,
		CacheReadPrice: item.CacheReadPrice, ImageInputPrice: item.ImageInputPrice,
		ImageOutputPrice: item.ImageOutputPrice, PerRequestPrice: item.PerRequestPrice,
		Intervals: intervals, Status: item.Status,
	}
}

func pricingFloatPointer(value float64, explicit bool) *float64 {
	if value == 0 && !explicit {
		return nil
	}
	result := value
	return &result
}

func pricingIntervalsFromService(intervals []service.PricingInterval) []domain.ModelPricingInterval {
	result := make([]domain.ModelPricingInterval, 0, len(intervals))
	for _, interval := range intervals {
		result = append(result, domain.ModelPricingInterval{
			MinTokens: interval.MinTokens, MaxTokens: interval.MaxTokens,
			TierLabel: interval.TierLabel, InputPrice: interval.InputPrice,
			OutputPrice: interval.OutputPrice, CacheWritePrice: interval.CacheWritePrice,
			CacheReadPrice: interval.CacheReadPrice, PerRequestPrice: interval.PerRequestPrice,
			SortOrder: interval.SortOrder,
		})
	}
	return result
}

func officialPricingValues(resolved *service.ResolvedPricing) *pricingValuesResponse {
	if resolved == nil || resolved.OfficialPricing == nil {
		return nil
	}
	pricing := resolved.OfficialPricing
	return &pricingValuesResponse{
		InputPrice:       pricingFloatPointer(pricing.InputPricePerToken, pricing.InputPriceExplicit),
		OutputPrice:      pricingFloatPointer(pricing.OutputPricePerToken, pricing.OutputPriceExplicit),
		CacheWritePrice:  pricingFloatPointer(pricing.CacheCreationPricePerToken, pricing.CacheCreationPriceExplicit),
		CacheReadPrice:   pricingFloatPointer(pricing.CacheReadPricePerToken, pricing.CacheReadPriceExplicit),
		ImageInputPrice:  pricingFloatPointer(pricing.ImageInputPricePerToken, pricing.ImageInputPriceExplicit),
		ImageOutputPrice: pricingFloatPointer(pricing.ImageOutputPricePerToken, pricing.ImageOutputPriceExplicit),
		PerRequestPrice:  pricingFloatPointer(resolved.OfficialDefaultPerRequestPrice, resolved.OfficialDefaultPerRequestPriceExplicit),
		Intervals:        []domain.ModelPricingInterval{},
	}
}

func salePricingValues(resolved *service.ResolvedPricing) *pricingValuesResponse {
	if resolved == nil || resolved.BasePricing == nil || !service.IsResolvedPricingAvailable(resolved) {
		return nil
	}
	pricing := resolved.BasePricing
	var override *service.ModelPricingOverride
	if resolved.MatchedOverride != nil {
		override = resolved.MatchedOverride
	}
	value := func(actual float64, explicit *float64, flagged bool) *float64 {
		if explicit != nil {
			result := *explicit
			return &result
		}
		return pricingFloatPointer(actual, flagged)
	}
	var input, output, cacheWrite, cacheRead, imageInput, imageOutput, perRequest *float64
	if override != nil {
		input, output = override.InputPrice, override.OutputPrice
		cacheWrite, cacheRead = override.CacheWritePrice, override.CacheReadPrice
		imageInput, imageOutput = override.ImageInputPrice, override.ImageOutputPrice
		perRequest = override.PerRequestPrice
	}
	return &pricingValuesResponse{
		InputPrice:       value(pricing.InputPricePerToken, input, pricing.InputPriceExplicit),
		OutputPrice:      value(pricing.OutputPricePerToken, output, pricing.OutputPriceExplicit),
		CacheWritePrice:  value(pricing.CacheCreationPricePerToken, cacheWrite, pricing.CacheCreationPriceExplicit),
		CacheReadPrice:   value(pricing.CacheReadPricePerToken, cacheRead, pricing.CacheReadPriceExplicit),
		ImageInputPrice:  value(pricing.ImageInputPricePerToken, imageInput, pricing.ImageInputPriceExplicit),
		ImageOutputPrice: value(pricing.ImageOutputPricePerToken, imageOutput, pricing.ImageOutputPriceExplicit),
		PerRequestPrice:  value(resolved.DefaultPerRequestPrice, perRequest, resolved.DefaultPerRequestPriceExplicit),
		Intervals:        pricingIntervalsFromService(resolved.Intervals),
	}
}

func pricingSaleSource(resolved *service.ResolvedPricing) string {
	if resolved == nil || !service.IsResolvedPricingAvailable(resolved) {
		return "unavailable"
	}
	if resolved.MatchedOverride != nil {
		return "custom"
	}
	if resolved.OfficialPricing != nil {
		return "official"
	}
	return "unavailable"
}

func modelPricingAuditSummary(item *service.ModelPricingOverride) string {
	if item == nil {
		return "null"
	}
	encoded, err := json.Marshal(modelPricingResponseFromService(item))
	if err != nil {
		return "<unavailable>"
	}
	return string(encoded)
}

func (h *ModelPricingHandler) List(c *gin.Context) {
	items, err := h.pricing.List(c.Request.Context(), c.Query("adapter"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result := make([]modelPricingResponse, len(items))
	for i := range items {
		result[i] = modelPricingResponseFromService(&items[i])
	}
	response.Success(c, result)
}

func (h *ModelPricingHandler) Catalog(c *gin.Context) {
	if h.catalog == nil {
		response.ErrorFrom(c, service.ErrModelPricingOverrideNotFound)
		return
	}
	items, err := h.catalog.ListPricingCatalog(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	platformID := int64(0)
	if raw := strings.TrimSpace(c.Query("platform_id")); raw != "" {
		platformID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || platformID <= 0 {
			response.BadRequest(c, "Invalid platform_id")
			return
		}
	}
	modelQuery := strings.ToLower(strings.TrimSpace(c.Query("model")))
	result := make([]modelPricingCatalogResponse, 0)
	for _, platform := range items {
		if platformID > 0 && platform.ID != platformID {
			continue
		}
		for _, model := range platform.Models {
			if modelQuery != "" && !strings.Contains(strings.ToLower(model.Pattern), modelQuery) &&
				!strings.Contains(strings.ToLower(model.UpstreamModel), modelQuery) {
				continue
			}
			row := modelPricingCatalogResponse{
				PlatformID: platform.ID, PlatformCode: platform.Code, PlatformName: platform.Name,
				AccountPlatform: platform.AccountPlatform, ModelPattern: model.Pattern,
				UpstreamModel: model.UpstreamModel, SaleSource: pricingSaleSource(model.Pricing),
				OfficialPricing: officialPricingValues(model.Pricing), SalePricing: salePricingValues(model.Pricing),
				Intervals: []domain.ModelPricingInterval{},
			}
			if model.Pricing != nil {
				row.BillingMode = string(model.Pricing.Mode)
				row.OfficialSource = model.Pricing.OfficialSource
				row.Intervals = pricingIntervalsFromService(model.Pricing.Intervals)
				if model.Pricing.MatchedOverride != nil {
					override := modelPricingResponseFromService(model.Pricing.MatchedOverride)
					row.Override = &override
				}
			}
			result = append(result, row)
		}
	}
	response.Success(c, result)
}

func (h *ModelPricingHandler) UpsertPlatformSale(c *gin.Context) {
	var req platformSalePricingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if h.platforms == nil {
		response.ErrorFrom(c, service.ErrPlatformNotFound)
		return
	}
	platform, err := h.platforms.GetByID(c.Request.Context(), req.PlatformID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	modelPattern := strings.TrimSpace(req.ModelPattern)
	before, err := h.pricing.GetExact(c.Request.Context(), platform.Code, modelPattern)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	middleware.SetAuditExtra(c, map[string]any{
		"platform_id": req.PlatformID, "model_pattern": modelPattern,
		"before_pricing": modelPricingAuditSummary(before),
	})
	item, err := h.pricing.UpsertPlatformSale(c.Request.Context(), platform, modelPattern, req.toService())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	middleware.SetAuditExtra(c, map[string]any{"after_pricing": modelPricingAuditSummary(item)})
	response.Success(c, modelPricingResponseFromService(item))
}

func (h *ModelPricingHandler) Get(c *gin.Context) {
	id, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	item, err := h.pricing.Get(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, modelPricingResponseFromService(item))
}

func (h *ModelPricingHandler) Create(c *gin.Context) {
	var req modelPricingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.pricing.Create(c.Request.Context(), req.toService())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, modelPricingResponseFromService(item))
}

func (h *ModelPricingHandler) Update(c *gin.Context) {
	id, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	var req modelPricingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.pricing.Update(c.Request.Context(), id, req.toService())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, modelPricingResponseFromService(item))
}

func (h *ModelPricingHandler) Delete(c *gin.Context) {
	id, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.pricing.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}
