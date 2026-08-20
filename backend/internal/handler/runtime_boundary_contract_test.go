//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRuntimeBoundaryBaselineKeepsInboundEndpointSelection(t *testing.T) {
	tests := []struct {
		name string
		path string
		mode openai_compat.ResponsesSupportMode
		want string
	}{
		{name: "chat automatic", path: EndpointChatCompletions, mode: openai_compat.ResponsesSupportModeAuto, want: EndpointChatCompletions},
		{name: "chat force responses", path: EndpointChatCompletions, mode: openai_compat.ResponsesSupportModeForceResponses, want: EndpointResponses},
		{name: "responses automatic", path: EndpointResponses, mode: openai_compat.ResponsesSupportModeAuto, want: EndpointResponses},
		{name: "responses force chat", path: EndpointResponses, mode: openai_compat.ResponsesSupportModeForceChatCompletions, want: EndpointChatCompletions},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, nil)
			c.Set(ctxKeyInboundEndpoint, tt.path)
			account := &service.Account{
				Platform: service.PlatformOpenAI,
				Type:     service.AccountTypeAPIKey,
				Extra: map[string]any{
					openai_compat.ExtraKeyResponsesMode:      string(tt.mode),
					openai_compat.ExtraKeyResponsesSupported: true,
				},
			}

			require.Equal(t, tt.want, resolveOpenAIUpstreamEndpoint(c, account, &service.OpenAIForwardResult{}))
		})
	}
}

func TestRuntimeBoundaryBaselineDerivesMessagesAndChatEndpoints(t *testing.T) {
	require.Equal(t, EndpointResponses, DeriveUpstreamEndpoint(
		EndpointMessages, "/v1/messages", service.PlatformOpenAI,
	))
	require.Equal(t, EndpointResponses, DeriveUpstreamEndpoint(
		EndpointChatCompletions, "/v1/chat/completions", service.PlatformOpenAI,
	))
	require.Equal(t, EndpointMessages, DeriveUpstreamEndpoint(
		EndpointMessages, "/v1/messages", service.PlatformAnthropic,
	))
}
