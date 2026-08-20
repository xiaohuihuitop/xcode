package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/gin-gonic/gin"
)

// Embeddings handles the OpenAI-compatible Embeddings API.
// POST /v1/embeddings
func (h *OpenAIGatewayHandler) Embeddings(c *gin.Context) {
	h.dispatchRuntimeEndpoint(c, gatewayruntime.EndpointEmbeddings)
}
