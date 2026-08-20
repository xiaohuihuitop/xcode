//go:build unit

package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/applicationgateway"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type runtimeIngressExecutorFunc func(context.Context, gatewayruntime.Request, gatewayruntime.UsageSink) (gatewayruntime.Result, error)

func (f runtimeIngressExecutorFunc) Execute(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	return f(ctx, request, sink)
}

func TestBuildRuntimeDispatchRequestCopiesIngressIdentityAndRoute(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-5.6"}`)))
	request = request.WithContext(context.WithValue(request.Context(), ctxkey.RequestID, "request-id-from-middleware"))
	c.Request = request
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:                         17,
		UserID:                     42,
		AllowedPlatformIDs:         []int64{9},
		AllowedSubscriptionPlanIDs: []int64{5},
		AllowBalance:               true,
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42, Concurrency: 3})
	c.Request = c.Request.WithContext(service.WithGatewayPlatformAssetContext(c.Request.Context(), &service.GatewayPlatformAssetContext{
		Platform: &service.ResolvedPlatformModel{
			PlatformID:           9,
			PlatformCode:         "openai-main",
			AccountPlatform:      service.PlatformOpenAI,
			RequestedModel:       "gpt-5.6",
			UpstreamModel:        "gpt-5.6-2026",
			EndpointCapabilities: []string{"chat_completions"},
		},
		SchedulingScope: service.PlatformSchedulingScope{
			PlatformID:      9,
			PlatformCode:    "openai-main",
			AccountPlatform: service.PlatformOpenAI,
		},
	}))

	got, err := buildRuntimeDispatchRequest(c, gatewayruntime.EndpointChatCompletions)

	require.NoError(t, err)
	require.Equal(t, int64(17), got.Grant.KeyID)
	require.Equal(t, int64(42), got.Grant.UserID)
	require.Equal(t, []int64{9}, got.Grant.PlatformIDs)
	require.Equal(t, []int64{5}, got.Grant.SubscriptionPlanIDs)
	require.True(t, got.Grant.AllowBalance)
	require.Equal(t, "gpt-5.6", got.Product.Model)
	require.Equal(t, "chat_completions", got.Product.EndpointCapability)
	require.Equal(t, gatewayruntime.EndpointChatCompletions, got.Runtime.Endpoint)
	require.Equal(t, "request-id-from-middleware", got.Runtime.RequestID)
	require.Equal(t, "/v1/chat/completions", got.Runtime.InboundEndpoint)
	require.Equal(t, int64(9), got.Runtime.PlatformID)
	require.Equal(t, service.PlatformOpenAI, got.Runtime.Adapter)
	require.Equal(t, []byte(`{"model":"gpt-5.6"}`), got.Runtime.Payload)
	require.Equal(t, int64(17), got.Runtime.Metadata.APIKeyID)
	require.Equal(t, int64(42), got.Runtime.Metadata.UserID)
	require.NotNil(t, got.Runtime.Exchange)
}

func TestBuildRuntimeDispatchRequestReadsOpenAIStreamFromJSONBody(t *testing.T) {
	c := newRuntimeIngressTestContext(t, "/v1/chat/completions", `{"model":"gpt-5.6","stream":true}`)

	got, err := buildRuntimeDispatchRequest(c, gatewayruntime.EndpointChatCompletions)

	require.NoError(t, err)
	require.True(t, got.Runtime.Stream)
}

func TestNewSub2APIExecutionKeepsProductAssetOutsideRuntimeCompatibilityRoute(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	exchange := NewGinHTTPExchange(c)
	sink := noOpRuntimeUsageSink{}
	request := gatewayruntime.Request{
		RequestID:      "request-pure-runtime",
		PlatformID:     9,
		PlatformCode:   "anthropic-main",
		Adapter:        service.PlatformAnthropic,
		Endpoint:       gatewayruntime.EndpointMessages,
		RequestedModel: "claude-requested",
		UpstreamModel:  "claude-upstream",
		Exchange:       exchange,
	}

	parent := service.WithGatewayPlatformAssetContext(context.Background(), &service.GatewayPlatformAssetContext{
		Platform: &service.ResolvedPlatformModel{
			PlatformID:      request.PlatformID,
			PlatformCode:    request.PlatformCode,
			AccountPlatform: request.Adapter,
			RequestedModel:  request.RequestedModel,
			UpstreamModel:   request.UpstreamModel,
		},
		BillingAsset: &service.ResolvedBillingAsset{Source: service.BillingSourceBalance, RateMultiplier: 1.25},
		SchedulingScope: service.PlatformSchedulingScope{
			PlatformID:      request.PlatformID,
			PlatformCode:    request.PlatformCode,
			AccountPlatform: request.Adapter,
		},
	})
	execution, err := newSub2APIExecution(parent, request, sink)

	require.NoError(t, err)
	require.NotNil(t, execution)
	route, ok := service.GatewayPlatformAssetContextFromContext(execution.Context())
	require.True(t, ok)
	require.Equal(t, int64(9), route.Platform.PlatformID)
	require.Equal(t, service.PlatformAnthropic, route.SchedulingScope.AccountPlatform)
	require.Nil(t, route.BillingAsset)
	require.Same(t, exchange, execution.Exchange())
	require.Equal(t, request.RequestID, execution.Request().RequestID)
}

func TestDispatchRuntimeEndpointStopsWhenProductPreflightBlocks(t *testing.T) {
	c := newRuntimeIngressTestContext(t, "/v1/embeddings", `{"model":"text-embedding-3-small"}`)
	runtimeCalled := false
	gateway := applicationgateway.New(
		contextDecisionProvider{},
		NewSub2APIRuntimeAdapter(map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor{
			gatewayruntime.EndpointEmbeddings: runtimeIngressExecutorFunc(func(context.Context, gatewayruntime.Request, gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
				runtimeCalled = true
				return gatewayruntime.Result{}, nil
			}),
		}),
		noOpRuntimeUsageSinkFactory{},
	)

	err := dispatchRuntimeEndpoint(c, gatewayruntime.EndpointEmbeddings, gateway, func(_ *gin.Context, request applicationgateway.DispatchRequest) (func(), bool) {
		require.Equal(t, []byte(`{"model":"text-embedding-3-small"}`), request.Runtime.Payload)
		return nil, false
	})

	require.NoError(t, err)
	require.False(t, runtimeCalled)
}

func TestDispatchRuntimeEndpointReleasesProductPreflightAfterRuntime(t *testing.T) {
	c := newRuntimeIngressTestContext(t, "/v1/embeddings", `{"model":"text-embedding-3-small"}`)
	released := false
	gateway := applicationgateway.New(
		contextDecisionProvider{},
		NewSub2APIRuntimeAdapter(map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor{
			gatewayruntime.EndpointEmbeddings: runtimeIngressExecutorFunc(func(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
				err := sink.RecordFinal(ctx, gatewayruntime.UsageEvent{RequestID: request.RequestID, Success: true})
				return gatewayruntime.Result{StatusCode: http.StatusOK}, err
			}),
		}),
		noOpRuntimeUsageSinkFactory{},
	)

	err := dispatchRuntimeEndpoint(c, gatewayruntime.EndpointEmbeddings, gateway, func(_ *gin.Context, _ applicationgateway.DispatchRequest) (func(), bool) {
		return func() { released = true }, true
	})

	require.NoError(t, err)
	require.True(t, released)
}

func newRuntimeIngressTestContext(t *testing.T, path, body string) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:                 17,
		UserID:             42,
		AllowedPlatformIDs: []int64{9},
		AllowBalance:       true,
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42, Concurrency: 3})
	c.Request = c.Request.WithContext(service.WithGatewayPlatformAssetContext(c.Request.Context(), &service.GatewayPlatformAssetContext{
		Platform: &service.ResolvedPlatformModel{
			PlatformID:           9,
			PlatformCode:         "openai-main",
			AccountPlatform:      service.PlatformOpenAI,
			RequestedModel:       "text-embedding-3-small",
			UpstreamModel:        "text-embedding-3-small",
			EndpointCapabilities: []string{"embeddings"},
		},
		SchedulingScope: service.PlatformSchedulingScope{
			PlatformID:      9,
			PlatformCode:    "openai-main",
			AccountPlatform: service.PlatformOpenAI,
		},
	}))
	return c
}
