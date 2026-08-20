package handler

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ChatCompletions handles OpenAI Chat Completions API requests.
// POST /v1/chat/completions
func (h *OpenAIGatewayHandler) ChatCompletions(c *gin.Context) {
	if shouldPreserveDirectImageRejection(c) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "This model is not supported on the Chat Completions endpoint")
		return
	}
	h.dispatchRuntimeEndpoint(c, gatewayruntime.EndpointChatCompletions)
}

// resolveOpenAIUpstreamEndpoint returns the actual upstream endpoint for an
// OpenAI-compatible account. A forwarding result is authoritative because a
// single inbound route may choose raw Chat or a Responses bridge at runtime.
// The account-based derivation remains as a fallback for existing callers and
// forwarding paths that do not report their endpoint yet.
func resolveOpenAIUpstreamEndpoint(c *gin.Context, account *service.Account, result *service.OpenAIForwardResult) string {
	if result != nil {
		if endpoint := strings.TrimSpace(result.UpstreamEndpoint); endpoint != "" {
			return endpoint
		}
	}
	if endpoint := service.GetActualOpenAIUpstreamEndpoint(c); endpoint != "" {
		return endpoint
	}
	if account != nil && account.Type == service.AccountTypeAPIKey {
		switch GetInboundEndpoint(c) {
		case EndpointChatCompletions:
			if !openai_compat.ShouldRouteChatCompletionsViaResponses(account.Extra) {
				return EndpointChatCompletions
			}
		case EndpointResponses, EndpointResponsesCompact, EndpointMessages:
			if !openai_compat.ShouldUseResponsesAPI(account.Extra) {
				return EndpointChatCompletions
			}
		}
	}
	return GetUpstreamEndpoint(c, account.Platform)
}
