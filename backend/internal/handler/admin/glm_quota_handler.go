package admin

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GLMQuotaHandler exposes read-only Runtime quota facts for user-configured
// GLM platform accounts. Credentials remain owned by the account service and
// are never copied into the response.
type GLMQuotaHandler struct {
	quotaService *service.GLMQuotaService
}

func NewGLMQuotaHandler(quotaService *service.GLMQuotaService) *GLMQuotaHandler {
	return &GLMQuotaHandler{quotaService: quotaService}
}

// Query returns the latest Coding Plan quota snapshot for an account.
// GET /api/v1/admin/accounts/:id/glm-quota
func (h *GLMQuotaHandler) Query(c *gin.Context) {
	if h == nil || h.quotaService == nil {
		response.Error(c, http.StatusServiceUnavailable, "GLM quota service unavailable")
		return
	}
	accountID, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}
	result, err := h.quotaService.Query(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
