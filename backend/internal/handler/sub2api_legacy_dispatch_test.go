//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayMessagesUsesRuntimeIngress(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	source, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "gateway_handler.go"))
	require.NoError(t, err)
	text := string(source)
	start := strings.Index(text, "func (h *GatewayHandler) Messages(")
	require.GreaterOrEqual(t, start, 0)
	rest := text[start:]
	end := strings.Index(rest, "\n}")
	require.Greater(t, end, 0)
	method := rest[:end]
	require.Contains(t, method, "dispatchRuntimeEndpoint")
	require.NotContains(t, method, "dispatchLegacyEndpoint")
}

func TestGatewayOpenAICompatibleHandlersUseRuntimeIngress(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Dir(filename)
	for _, test := range []struct {
		file   string
		method string
	}{
		{file: "gateway_handler_chat_completions.go", method: "func (h *GatewayHandler) ChatCompletions("},
		{file: "gateway_handler_responses.go", method: "func (h *GatewayHandler) Responses("},
	} {
		t.Run(test.file, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join(root, test.file))
			require.NoError(t, err)
			text := string(source)
			start := strings.Index(text, test.method)
			require.GreaterOrEqual(t, start, 0)
			rest := text[start:]
			end := strings.Index(rest, "\n}")
			require.Greater(t, end, 0)
			method := rest[:end]
			require.Contains(t, method, "dispatchRuntimeEndpoint")
			require.NotContains(t, method, "dispatchLegacyEndpoint")
		})
	}
}

func TestOpenAIPublicHandlersDoNotFallbackToLegacyIngress(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Dir(filename)
	for _, test := range []struct {
		file   string
		method string
		legacy string
	}{
		{file: "openai_chat_completions.go", method: "func (h *OpenAIGatewayHandler) ChatCompletions(", legacy: "legacyChatCompletions"},
		{file: "openai_gateway_handler.go", method: "func (h *OpenAIGatewayHandler) Responses(", legacy: "legacyResponses"},
		{file: "openai_gateway_handler.go", method: "func (h *OpenAIGatewayHandler) Messages(", legacy: "legacyMessages"},
		{file: "openai_images.go", method: "func (h *OpenAIGatewayHandler) Images(", legacy: "legacyImages"},
		{file: "openai_embeddings.go", method: "func (h *OpenAIGatewayHandler) Embeddings(", legacy: "legacyEmbeddings"},
		{file: "openai_alpha_search.go", method: "func (h *OpenAIGatewayHandler) AlphaSearch(", legacy: "legacyAlphaSearch"},
		{file: "openai_gateway_count_tokens.go", method: "func (h *OpenAIGatewayHandler) CountTokens(", legacy: "legacyCountTokens"},
	} {
		t.Run(test.file+"/"+test.legacy, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join(root, test.file))
			require.NoError(t, err)
			text := string(source)
			start := strings.Index(text, test.method)
			require.GreaterOrEqual(t, start, 0)
			rest := text[start:]
			end := strings.Index(rest, "\n}")
			require.Greater(t, end, 0)
			method := rest[:end]
			require.Contains(t, method, "dispatchRuntimeEndpoint")
			require.NotContains(t, method, test.legacy)
			require.NotContains(t, method, "applicationGateway == nil")
		})
	}
}

func TestSub2APIMessagesExecutorDoesNotExposeOpenAILegacyBridge(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	source, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "sub2api_messages_executor.go"))
	require.NoError(t, err)
	require.NotContains(t, string(source), "openAIHandlerForEndpoint")
}

func TestOpenAIGatewayHandlerDoesNotRetainHTTPLegacyHandlers(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	source, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "openai_gateway_handler.go"))
	require.NoError(t, err)
	text := string(source)
	require.NotContains(t, text, "func (h *OpenAIGatewayHandler) legacyResponses(")
	require.NotContains(t, text, "func (h *OpenAIGatewayHandler) legacyMessages(")
	// These helpers remain part of the explicit WebSocket/compatibility seam.
	require.Contains(t, text, "func (h *OpenAIGatewayHandler) ensureResponsesDependencies(")
	require.Contains(t, text, "func (h *OpenAIGatewayHandler) acquireResponsesUserSlot(")
}

func TestDispatchLegacyEndpointUsesApplicationGatewayRoute(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 11, UserID: 12, User: &service.User{ID: 12}})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 12, Concurrency: 1})
	c.Request = c.Request.WithContext(service.WithGatewayPlatformAssetContext(c.Request.Context(), &service.GatewayPlatformAssetContext{
		Platform: &service.ResolvedPlatformModel{
			PlatformID:      42,
			PlatformCode:    "openai-main",
			AccountPlatform: service.PlatformOpenAI,
			RequestedModel:  "gpt-5.6",
			UpstreamModel:   "gpt-5.6",
		},
		SchedulingScope: service.PlatformSchedulingScope{
			PlatformID:      42,
			PlatformCode:    "openai-main",
			AccountPlatform: service.PlatformOpenAI,
		},
	}))

	called := false
	err := (&GatewayHandler{}).dispatchLegacyEndpoint(c, gatewayruntime.EndpointResponses, func(ctx *gin.Context) {
		called = true
		ctx.Status(http.StatusOK)
	})

	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, http.StatusOK, recorder.Code)
}
