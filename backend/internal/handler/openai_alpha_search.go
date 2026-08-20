package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/gin-gonic/gin"
)

// AlphaSearch proxies the standalone search endpoint used by Codex Responses Lite.
func (h *OpenAIGatewayHandler) AlphaSearch(c *gin.Context) {
	h.dispatchRuntimeEndpoint(c, gatewayruntime.EndpointAlphaSearch)
}
