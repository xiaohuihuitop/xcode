// Package handler provides HTTP request handlers for the application.
package handler

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// APIKeyHandler handles API key-related requests
type APIKeyHandler struct {
	apiKeyService *service.APIKeyService
	platformPools platformPoolLister
}

// platformPoolLister keeps the API-key selector independent from platform
// administration and exposes only the read capability it needs.
type platformPoolLister interface {
	List(ctx context.Context) ([]service.Platform, error)
}

// NewAPIKeyHandler creates a new APIKeyHandler.
//
// The platform-pool lister is mandatory for the V2 API Key authorization
// selector. Keeping it in the constructor makes stale dependency injection a
// compile-time failure instead of silently rendering an empty selector.
func NewAPIKeyHandler(apiKeyService *service.APIKeyService, platformPools platformPoolLister) *APIKeyHandler {
	return &APIKeyHandler{
		apiKeyService: apiKeyService,
		platformPools: platformPools,
	}
}

type availablePlatformPoolResponse struct {
	ID              int64                            `json:"id"`
	Code            string                           `json:"code"`
	Name            string                           `json:"name"`
	AccountPlatform string                           `json:"account_platform"`
	Models          []availablePlatformModelResponse `json:"models,omitempty"`
}

type availablePlatformModelResponse struct {
	Pattern              string   `json:"pattern"`
	UpstreamModel        string   `json:"upstream_model,omitempty"`
	EndpointCapabilities []string `json:"endpoint_capabilities,omitempty"`
}

// CreateAPIKeyRequest represents the create API key request payload
type CreateAPIKeyRequest struct {
	Name                string   `json:"name" binding:"required"`
	PlatformIDs         []int64  `json:"platform_ids"`
	SubscriptionPlanIDs []int64  `json:"subscription_plan_ids"`
	AllowAllSubscriptions *bool  `json:"allow_all_subscriptions"`
	AllowBalance        *bool    `json:"allow_balance"`
	CustomKey           *string  `json:"custom_key"`      // 可选的自定义key
	IPWhitelist         []string `json:"ip_whitelist"`    // IP 白名单
	IPBlacklist         []string `json:"ip_blacklist"`    // IP 黑名单
	Quota               *float64 `json:"quota"`           // 配额限制 (USD)
	ExpiresInDays       *int     `json:"expires_in_days"` // 过期天数

	// Rate limit fields (0 = unlimited)
	RateLimit5h *float64 `json:"rate_limit_5h"`
	RateLimit1d *float64 `json:"rate_limit_1d"`
	RateLimit7d *float64 `json:"rate_limit_7d"`
}

// UpdateAPIKeyRequest represents the update API key request payload
type UpdateAPIKeyRequest struct {
	Name                string    `json:"name"`
	PlatformIDs         *[]int64  `json:"platform_ids"`
	SubscriptionPlanIDs *[]int64  `json:"subscription_plan_ids"`
	AllowAllSubscriptions *bool   `json:"allow_all_subscriptions"`
	AllowBalance        *bool     `json:"allow_balance"`
	Status              string    `json:"status" binding:"omitempty,oneof=active inactive"`
	IPWhitelist         *[]string `json:"ip_whitelist"` // IP 白名单（nil 不修改，空数组清空）
	IPBlacklist         *[]string `json:"ip_blacklist"` // IP 黑名单（nil 不修改，空数组清空）
	Quota               *float64  `json:"quota"`        // 配额限制 (USD), 0=无限制
	ExpiresAt           *string   `json:"expires_at"`   // 过期时间 (ISO 8601)
	ResetQuota          *bool     `json:"reset_quota"`  // 重置已用配额

	// Rate limit fields (nil = no change, 0 = unlimited)
	RateLimit5h         *float64 `json:"rate_limit_5h"`
	RateLimit1d         *float64 `json:"rate_limit_1d"`
	RateLimit7d         *float64 `json:"rate_limit_7d"`
	ResetRateLimitUsage *bool    `json:"reset_rate_limit_usage"` // 重置限速用量
}

// List handles listing user's API keys with pagination
// GET /api/v1/api-keys
func (h *APIKeyHandler) List(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	page, pageSize := response.ParsePagination(c)
	params := pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.DefaultQuery("sort_by", "created_at"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	}

	// Parse filter parameters
	var filters service.APIKeyListFilters
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		if len(search) > 100 {
			search = search[:100]
		}
		filters.Search = search
	}
	filters.Status = c.Query("status")
	keys, result, err := h.apiKeyService.List(c.Request.Context(), subject.UserID, params, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.APIKey, 0, len(keys))
	for i := range keys {
		out = append(out, *dto.APIKeyFromService(&keys[i]))
	}
	response.Paginated(c, out, result.Total, page, pageSize)
}

// GetByID handles getting a single API key
// GET /api/v1/api-keys/:id
func (h *APIKeyHandler) GetByID(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid key ID")
		return
	}

	key, err := h.apiKeyService.GetByID(c.Request.Context(), keyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 验证所有权
	if key.UserID != subject.UserID {
		response.NotFound(c, "API key not found")
		return
	}

	response.Success(c, dto.APIKeyFromService(key))
}

// Create handles creating a new API key
// POST /api/v1/api-keys
func (h *APIKeyHandler) Create(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	svcReq := service.CreateAPIKeyRequest{
		Name:                req.Name,
		PlatformIDs:         req.PlatformIDs,
		SubscriptionPlanIDs: req.SubscriptionPlanIDs,
		AllowAllSubscriptions: req.AllowAllSubscriptions,
		AllowBalance:        req.AllowBalance,
		CustomKey:           req.CustomKey,
		IPWhitelist:         req.IPWhitelist,
		IPBlacklist:         req.IPBlacklist,
		ExpiresInDays:       req.ExpiresInDays,
	}
	if req.Quota != nil {
		svcReq.Quota = *req.Quota
	}
	if req.RateLimit5h != nil {
		svcReq.RateLimit5h = *req.RateLimit5h
	}
	if req.RateLimit1d != nil {
		svcReq.RateLimit1d = *req.RateLimit1d
	}
	if req.RateLimit7d != nil {
		svcReq.RateLimit7d = *req.RateLimit7d
	}

	executeUserIdempotentJSON(c, "user.api_keys.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		key, err := h.apiKeyService.Create(ctx, subject.UserID, svcReq)
		if err != nil {
			return nil, err
		}
		return dto.APIKeyFromService(key), nil
	})
}

// Update handles updating an API key
// PUT /api/v1/api-keys/:id
func (h *APIKeyHandler) Update(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid key ID")
		return
	}

	var req UpdateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	svcReq := service.UpdateAPIKeyRequest{
		PlatformIDs:         req.PlatformIDs,
		SubscriptionPlanIDs: req.SubscriptionPlanIDs,
		AllowAllSubscriptions: req.AllowAllSubscriptions,
		AllowBalance:        req.AllowBalance,
		IPWhitelist:         req.IPWhitelist,
		IPBlacklist:         req.IPBlacklist,
		Quota:               req.Quota,
		ResetQuota:          req.ResetQuota,
		RateLimit5h:         req.RateLimit5h,
		RateLimit1d:         req.RateLimit1d,
		RateLimit7d:         req.RateLimit7d,
		ResetRateLimitUsage: req.ResetRateLimitUsage,
	}
	if req.Name != "" {
		svcReq.Name = &req.Name
	}
	if req.Status != "" {
		svcReq.Status = &req.Status
	}
	// Parse expires_at if provided
	if req.ExpiresAt != nil {
		if *req.ExpiresAt == "" {
			// Empty string means clear expiration
			svcReq.ExpiresAt = nil
			svcReq.ClearExpiration = true
		} else {
			t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
			if err != nil {
				response.BadRequest(c, "Invalid expires_at format: "+err.Error())
				return
			}
			svcReq.ExpiresAt = &t
		}
	}

	key, err := h.apiKeyService.Update(c.Request.Context(), keyID, subject.UserID, svcReq)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.APIKeyFromService(key))
}

// Delete handles deleting an API key
// DELETE /api/v1/api-keys/:id
func (h *APIKeyHandler) Delete(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid key ID")
		return
	}

	err = h.apiKeyService.Delete(c.Request.Context(), keyID, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "API key deleted successfully"})
}

// GetAvailablePlatforms returns active platform-pool metadata for API Key
// authorization and the user-facing platform catalog. It exposes only active
// model patterns and endpoint capabilities; account details
// never leave the service boundary.
// GET /api/v1/platforms/available
func (h *APIKeyHandler) GetAvailablePlatforms(c *gin.Context) {
	if _, ok := middleware2.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if h.platformPools == nil {
		response.InternalError(c, "Platform pools are unavailable")
		return
	}

	platforms, err := h.platformPools.List(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	available := make([]availablePlatformPoolResponse, 0, len(platforms))
	for index := range platforms {
		platform := platforms[index]
		if !platform.IsActive() {
			continue
		}
		available = append(available, availablePlatformPoolResponse{
			ID:              platform.ID,
			Code:            platform.Code,
			Name:            platform.Name,
			AccountPlatform: platform.AccountPlatform,
			Models:          availablePlatformModels(platform.ModelRules),
		})
	}
	response.Success(c, available)
}

func availablePlatformModels(rules []service.PlatformModelRule) []availablePlatformModelResponse {
	models := make([]availablePlatformModelResponse, 0, len(rules))
	for _, rule := range rules {
		if !rule.Enabled || strings.TrimSpace(rule.ModelPattern) == "" {
			continue
		}
		models = append(models, availablePlatformModelResponse{
			Pattern:              rule.ModelPattern,
			UpstreamModel:        rule.UpstreamModel,
			EndpointCapabilities: append([]string(nil), rule.EndpointCapabilities...),
		})
	}
	return models
}
