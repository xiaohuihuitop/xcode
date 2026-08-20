package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

func ensureModelTargetPlatform(c *gin.Context, model string) {
	if c == nil || c.Request == nil {
		return
	}
	if _, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context()); ok {
		return
	}
	if platform, ok := service.DetectModelPlatform(model); ok {
		c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), platform))
	}
}

func modelTargetPlatformAllowed(c *gin.Context, model string, allowed ...string) bool {
	if c == nil || c.Request == nil {
		return false
	}
	ensureModelTargetPlatform(c, model)
	platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	if !ok {
		return false
	}
	for _, allowedPlatform := range allowed {
		if platform == allowedPlatform {
			return true
		}
	}
	return false
}

func modelTargetPlatformResolved(c *gin.Context, model string) bool {
	if c == nil || c.Request == nil {
		return false
	}
	ensureModelTargetPlatform(c, model)
	_, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	return ok
}

func effectiveAPIKeyPlatform(c *gin.Context, apiKey *service.APIKey) string {
	if c != nil && c.Request != nil {
		if platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context()); ok {
			return platform
		}
	}
	if c != nil && c.Request != nil {
		if scope, ok := service.PlatformSchedulingScopeFromContext(c.Request.Context()); ok {
			return scope.AccountPlatform
		}
	}
	return service.PlatformFromAPIKey(apiKey)
}
