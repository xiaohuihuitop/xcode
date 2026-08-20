package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AvailablePlatformHandler exposes the authenticated user's platform catalog.
// The same catalog is used by the API-key form and the user-facing directory;
// it has no dependency on API-key billing assets.
type AvailablePlatformHandler struct {
	catalog *service.PlatformCatalogService
}

func NewAvailablePlatformHandler(catalog *service.PlatformCatalogService) *AvailablePlatformHandler {
	return &AvailablePlatformHandler{catalog: catalog}
}

func (h *AvailablePlatformHandler) List(c *gin.Context) {
	if _, ok := middleware.GetAuthSubjectFromContext(c); !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if h == nil || h.catalog == nil {
		response.InternalError(c, "Platform catalog is unavailable")
		return
	}
	platforms, err := h.catalog.ListAvailable(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, platforms)
}
