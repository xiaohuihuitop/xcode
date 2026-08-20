package admin

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// platformManagementService keeps the handler independent from the concrete
// service, making the administrator contract directly testable.
type platformManagementService interface {
	List(ctx context.Context) ([]service.Platform, error)
	GetByID(ctx context.Context, id int64) (*service.Platform, error)
	Create(ctx context.Context, input service.CreatePlatformInput) (*service.Platform, error)
	Update(ctx context.Context, id int64, input service.UpdatePlatformInput) (*service.Platform, error)
	PreviewDelete(ctx context.Context, id int64) (*service.PlatformDeleteImpact, error)
	Delete(ctx context.Context, id int64) (*service.PlatformDeleteResult, error)
}

// PlatformHandler manages business platform account pools.
type PlatformHandler struct {
	platforms platformManagementService
}

func NewPlatformHandler(platforms platformManagementService) *PlatformHandler {
	return &PlatformHandler{platforms: platforms}
}

type platformModelRuleRequest struct {
	ModelPattern  string `json:"model_pattern" binding:"required"`
	UpstreamModel string `json:"upstream_model"`
	Enabled       *bool  `json:"enabled"`
}

func (r platformModelRuleRequest) toServiceRule() service.PlatformModelRule {
	enabled := true
	if r.Enabled != nil {
		enabled = *r.Enabled
	}
	return service.PlatformModelRule{
		ModelPattern:  r.ModelPattern,
		UpstreamModel: r.UpstreamModel,
		Enabled:       enabled,
	}
}

type createPlatformRequest struct {
	Code                 string                     `json:"code" binding:"required"`
	Name                 string                     `json:"name" binding:"required"`
	AccountPlatform      string                     `json:"account_platform" binding:"required"`
	Status               string                     `json:"status"`
	EndpointCapabilities []string                   `json:"endpoint_capabilities"`
	ModelRules           []platformModelRuleRequest `json:"model_rules"`
}

func (r createPlatformRequest) toServiceInput() service.CreatePlatformInput {
	rules := make([]service.PlatformModelRule, len(r.ModelRules))
	for index := range r.ModelRules {
		rules[index] = r.ModelRules[index].toServiceRule()
	}
	return service.CreatePlatformInput{
		Code:                 r.Code,
		Name:                 r.Name,
		AccountPlatform:      r.AccountPlatform,
		Status:               r.Status,
		EndpointCapabilities: r.EndpointCapabilities,
		ModelRules:           rules,
	}
}

type updatePlatformRequest struct {
	Code                 *string                     `json:"code"`
	Name                 *string                     `json:"name"`
	AccountPlatform      *string                     `json:"account_platform"`
	Status               *string                     `json:"status"`
	EndpointCapabilities *[]string                   `json:"endpoint_capabilities"`
	ModelRules           *[]platformModelRuleRequest `json:"model_rules"`
}

func (r updatePlatformRequest) toServiceInput() service.UpdatePlatformInput {
	result := service.UpdatePlatformInput{
		Code:                 r.Code,
		Name:                 r.Name,
		AccountPlatform:      r.AccountPlatform,
		Status:               r.Status,
		EndpointCapabilities: r.EndpointCapabilities,
	}
	if r.ModelRules == nil {
		return result
	}
	rules := make([]service.PlatformModelRule, len(*r.ModelRules))
	for index := range *r.ModelRules {
		rules[index] = (*r.ModelRules)[index].toServiceRule()
	}
	result.ModelRules = &rules
	return result
}

type platformModelRuleResponse struct {
	ID            int64  `json:"id"`
	ModelPattern  string `json:"model_pattern"`
	UpstreamModel string `json:"upstream_model"`
	Enabled       bool   `json:"enabled"`
}

type platformResponse struct {
	ID                   int64                       `json:"id"`
	Code                 string                      `json:"code"`
	Name                 string                      `json:"name"`
	AccountPlatform      string                      `json:"account_platform"`
	Status               string                      `json:"status"`
	EndpointCapabilities []string                    `json:"endpoint_capabilities"`
	ModelRules           []platformModelRuleResponse `json:"model_rules"`
}

func platformResponseFromService(platform *service.Platform) platformResponse {
	if platform == nil {
		return platformResponse{EndpointCapabilities: []string{}, ModelRules: []platformModelRuleResponse{}}
	}
	rules := make([]platformModelRuleResponse, len(platform.ModelRules))
	for index := range platform.ModelRules {
		rules[index] = platformModelRuleResponse{
			ID:            platform.ModelRules[index].ID,
			ModelPattern:  platform.ModelRules[index].ModelPattern,
			UpstreamModel: platform.ModelRules[index].UpstreamModel,
			Enabled:       platform.ModelRules[index].Enabled,
		}
	}
	endpointCapabilities := append([]string(nil), platform.EndpointCapabilities...)
	if endpointCapabilities == nil {
		endpointCapabilities = []string{}
	}
	return platformResponse{
		ID:                   platform.ID,
		Code:                 platform.Code,
		Name:                 platform.Name,
		AccountPlatform:      platform.AccountPlatform,
		Status:               platform.Status,
		EndpointCapabilities: endpointCapabilities,
		ModelRules:           rules,
	}
}

// List returns all platform account pools, including disabled configurations.
func (h *PlatformHandler) List(c *gin.Context) {
	platforms, err := h.platforms.List(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result := make([]platformResponse, len(platforms))
	for index := range platforms {
		result[index] = platformResponseFromService(&platforms[index])
	}
	response.Success(c, result)
}

// GetByID returns one platform pool configuration.
func (h *PlatformHandler) GetByID(c *gin.Context) {
	id, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	platform, err := h.platforms.GetByID(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, platformResponseFromService(platform))
}

// Create creates a platform account pool and its model rules atomically.
func (h *PlatformHandler) Create(c *gin.Context) {
	var req createPlatformRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	platform, err := h.platforms.Create(c.Request.Context(), req.toServiceInput())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, platformResponseFromService(platform))
}

// Update edits a platform pool.
func (h *PlatformHandler) Update(c *gin.Context) {
	id, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	var req updatePlatformRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	platform, err := h.platforms.Update(c.Request.Context(), id, req.toServiceInput())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, platformResponseFromService(platform))
}

// DeleteImpact previews blockers and historical data that deletion will clear.
func (h *PlatformHandler) DeleteImpact(c *gin.Context) {
	id, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	impact, err := h.platforms.PreviewDelete(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, impact)
}

// Delete performs a fresh atomic blocker check and controlled cleanup.
func (h *PlatformHandler) Delete(c *gin.Context) {
	id, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	result, err := h.platforms.Delete(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
