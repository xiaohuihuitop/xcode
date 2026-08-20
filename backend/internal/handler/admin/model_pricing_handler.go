package admin

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type modelPricingManagementService interface {
	List(ctx context.Context, adapter string) ([]service.ModelPricingOverride, error)
	Get(ctx context.Context, id int64) (*service.ModelPricingOverride, error)
	Create(ctx context.Context, input service.ModelPricingOverride) (*service.ModelPricingOverride, error)
	Update(ctx context.Context, id int64, input service.ModelPricingOverride) (*service.ModelPricingOverride, error)
	Delete(ctx context.Context, id int64) error
}

// ModelPricingHandler exposes adapter/model price overrides without exposing
// Group or Channel concepts to the administrator.
type ModelPricingHandler struct {
	pricing modelPricingManagementService
}

func NewModelPricingHandler(pricing modelPricingManagementService) *ModelPricingHandler {
	return &ModelPricingHandler{pricing: pricing}
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
