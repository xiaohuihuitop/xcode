//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRuntimeGeminiModelAction(t *testing.T) {
	model, action, err := runtimeGeminiModelAction(gatewayruntime.Request{
		InboundEndpoint: "/v1beta/models/gemini-2.5-flash:streamGenerateContent",
		RequestedModel:  "ignored",
	})
	require.NoError(t, err)
	require.Equal(t, "gemini-2.5-flash", model)
	require.Equal(t, "streamGenerateContent", action)
}

func TestRuntimeGeminiSessionHashPreservesCLIStickyNamespace(t *testing.T) {
	request := gatewayruntime.Request{
		Payload: []byte(`{"contents":[{"parts":[{"text":"/Users/test/.gemini/tmp/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}]}]}`),
		Metadata: gatewayruntime.RequestMetadata{Headers: map[string]string{
			"X-Gemini-Api-Privileged-User-Id": "user-123",
		}},
	}
	hash := runtimeGeminiSessionHash(request, nil)
	require.True(t, strings.HasPrefix(hash, "gemini:"))
	require.Len(t, strings.TrimPrefix(hash, "gemini:"), 64)
}

func TestRuntimeGrokMediaEndpointAndRequestID(t *testing.T) {
	generation := gatewayruntime.Request{InboundEndpoint: "/v1/videos/generations"}
	require.Equal(t, service.GrokMediaEndpointVideosGenerations, runtimeGrokMediaEndpoint(generation))
	require.Empty(t, runtimeGrokMediaRequestID(generation))

	request := gatewayruntime.Request{InboundEndpoint: "/v1/videos/vid_123"}
	require.Equal(t, service.GrokMediaEndpointVideoStatus, runtimeGrokMediaEndpoint(request))
	require.Equal(t, "vid_123", runtimeGrokMediaRequestID(request))

	request.InboundEndpoint = "/v1/videos/vid_123/content"
	require.Equal(t, service.GrokMediaEndpointVideoContent, runtimeGrokMediaEndpoint(request))
	require.Equal(t, "vid_123", runtimeGrokMediaRequestID(request))
}

func TestRuntimeRequestHeaderIsCaseInsensitive(t *testing.T) {
	request := gatewayruntime.Request{Metadata: gatewayruntime.RequestMetadata{Headers: map[string]string{"content-type": "multipart/form-data; boundary=test"}}}
	require.Equal(t, "multipart/form-data; boundary=test", runtimeRequestHeader(request, "Content-Type"))
}

func TestRuntimeMediaUsageFactsDerivesImageEndpoint(t *testing.T) {
	account := &service.Account{ID: 7, Platform: service.PlatformOpenAI}
	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/v1/images/generations", want: "/v1/images/generations"},
		{path: "/v1/images/edits", want: "/v1/images/edits"},
	} {
		facts := openAIMediaUsageFacts(gatewayruntime.Request{InboundEndpoint: test.path}, account, nil)
		require.Equal(t, test.want, facts.UpstreamEndpoint)
	}
}

func TestRuntimeSessionHashDoesNotInventStickyKey(t *testing.T) {
	require.Empty(t, runtimeSessionHash(gatewayruntime.Request{RequestID: "request-id"}))
}

func TestSub2APIGeminiMediaExecutorRecordsFailureOnceWithoutLegacyFallback(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", nil)
	sink := &messagesExecutorSink{}
	executor := sub2APIGeminiMediaExecutor{
		endpoint: gatewayruntime.EndpointGeminiNative,
	}
	result, err := executor.Execute(context.Background(), gatewayruntime.Request{
		RequestID:       "gemini-runtime-unavailable",
		Adapter:         service.PlatformGemini,
		Endpoint:        gatewayruntime.EndpointGeminiNative,
		InboundEndpoint: "/v1beta/models/gemini-2.5-flash:generateContent",
		RequestedModel:  "gemini-2.5-flash",
		Payload:         []byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`),
		Exchange:        NewGinHTTPExchange(c),
	}, sink)

	require.Error(t, err)
	require.Equal(t, http.StatusBadGateway, result.StatusCode)
	require.Len(t, sink.events, 1)
	require.False(t, sink.events[0].Success)
}

func TestGeminiForwardUsageFactsPreservesAccountAndUsage(t *testing.T) {
	account := &service.Account{ID: 55, Platform: service.PlatformGemini}
	result := &service.ForwardResult{
		Model:         "gemini-2.5-flash",
		UpstreamModel: "gemini-2.5-flash-preview",
		Usage:         service.ClaudeUsage{InputTokens: 12, OutputTokens: 7},
	}
	facts := geminiForwardUsageFacts(gatewayruntime.Request{
		Adapter:         service.PlatformGemini,
		RequestedModel:  "gemini-2.5-flash",
		UpstreamModel:   "gemini-2.5-flash",
		InboundEndpoint: "/v1beta/models/gemini-2.5-flash:generateContent",
	}, account, result)
	require.Equal(t, int64(55), facts.AccountID)
	require.Equal(t, 12, facts.InputTokens)
	require.Equal(t, 7, facts.OutputTokens)
	require.Equal(t, "gemini-2.5-flash-preview", facts.UpstreamModel)
}
