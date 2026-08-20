//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/applicationgateway"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSub2APISyncExecutorCountTokensGrokUsesPureRuntimeExchange(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	exchange := NewGinHTTPExchange(c)

	executor := sub2APISyncExecutor{endpoint: gatewayruntime.EndpointCountTokens}
	sink := &messagesExecutorSink{}
	result, err := executor.Execute(context.Background(), gatewayruntime.Request{
		RequestID:      "count-tokens-grok",
		Adapter:        service.PlatformGrok,
		Endpoint:       gatewayruntime.EndpointCountTokens,
		RequestedModel: "grok-4",
		Payload:        []byte(`{"model":"grok-4","messages":[{"role":"user","content":"hello"}]}`),
		Exchange:       exchange,
	}, sink)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Len(t, sink.events, 1)
	require.True(t, sink.events[0].Success)
	var body map[string]int
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Positive(t, body["input_tokens"])
}

func TestSub2APISyncExecutorDispatchesInjectedEmbeddings(t *testing.T) {
	executed := false
	executor := sub2APISyncExecutor{
		endpoint: gatewayruntime.EndpointEmbeddings,
		executeEmbeddings: func(_ context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
			executed = true
			require.Equal(t, gatewayruntime.EndpointEmbeddings, request.Endpoint)
			require.NoError(t, sink.RecordFinal(context.Background(), gatewayruntime.UsageEvent{
				RequestID: request.RequestID,
				Success:   true,
				Facts:     gatewayruntime.UsageFacts{AccountID: 7, InputTokens: 3},
			}))
			return gatewayruntime.Result{StatusCode: http.StatusOK, AccountID: 7}, nil
		},
	}
	sink := &messagesExecutorSink{}
	result, err := executor.Execute(context.Background(), gatewayruntime.Request{
		RequestID: "embeddings-injected",
		Adapter:   service.PlatformOpenAI,
		Endpoint:  gatewayruntime.EndpointEmbeddings,
	}, sink)

	require.NoError(t, err)
	require.True(t, executed)
	require.Equal(t, int64(7), result.AccountID)
	require.Len(t, sink.events, 1)
}

func TestSub2APISyncExecutorEmbeddingsFailoverAttributesSecondAccount(t *testing.T) {
	first := &service.Account{ID: 101, Platform: service.PlatformOpenAI}
	second := &service.Account{ID: 202, Platform: service.PlatformOpenAI}
	selected := 0
	forwarded := make([]int64, 0, 2)
	released := make([]int64, 0, 2)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)

	executor := sub2APISyncExecutor{
		endpoint: gatewayruntime.EndpointEmbeddings,
		selectAccount: func(context.Context, gatewayruntime.Request, map[int64]struct{}, service.OpenAIEndpointCapability) (*service.AccountSelectionResult, error) {
			selected++
			account := first
			if selected > 1 {
				account = second
			}
			accountID := account.ID
			return &service.AccountSelectionResult{Account: account, Acquired: true, ReleaseFunc: func() { released = append(released, accountID) }}, nil
		},
		forwardEmbeddings: func(_ context.Context, account *service.Account) (*service.OpenAIForwardResult, error) {
			forwarded = append(forwarded, account.ID)
			if account.ID == first.ID {
				return nil, &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway, NextAccountAction: service.NextAccountRetry}
			}
			return &service.OpenAIForwardResult{Model: "text-embedding-3-small", UpstreamModel: "text-embedding-3-small", Usage: service.OpenAIUsage{InputTokens: 3}}, nil
		},
		recordSwitch: func() {},
	}

	sink := &messagesExecutorSink{}
	result, err := executor.executeEmbeddingsRuntime(context.Background(), gatewayruntime.Request{
		RequestID:      "embeddings-failover",
		Adapter:        service.PlatformOpenAI,
		Endpoint:       gatewayruntime.EndpointEmbeddings,
		RequestedModel: "text-embedding-3-small",
		Exchange:       NewGinHTTPExchange(c),
	}, sink)

	require.NoError(t, err)
	require.Equal(t, int64(202), result.AccountID)
	require.Equal(t, []int64{101, 202}, forwarded)
	require.Equal(t, []int64{101, 202}, released)
	require.Len(t, sink.events, 1)
	require.True(t, sink.events[0].Success)
	require.Equal(t, int64(202), sink.events[0].Facts.AccountID)
}

func TestGrokCountTokensPublicHandlerUsesRuntimeIngress(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(`{"model":"grok-4","messages":[{"role":"user","content":"hello"}]}`))
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 11, UserID: 22, AllowBalance: true})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 22})
	c.Request = c.Request.WithContext(service.WithGatewayPlatformAssetContext(c.Request.Context(), &service.GatewayPlatformAssetContext{
		Platform: &service.ResolvedPlatformModel{
			PlatformID:           7,
			PlatformCode:         "grok-main",
			AccountPlatform:      service.PlatformGrok,
			RequestedModel:       "grok-4",
			UpstreamModel:        "grok-4",
			EndpointCapabilities: []string{"count_tokens"},
		},
		SchedulingScope: service.PlatformSchedulingScope{PlatformID: 7, PlatformCode: "grok-main", AccountPlatform: service.PlatformGrok},
	}))

	runtime := NewSub2APIRuntimeAdapter(map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor{
		gatewayruntime.EndpointCountTokens: sub2APISyncExecutor{},
	})
	appGateway := applicationgateway.New(contextDecisionProvider{}, runtime, noOpRuntimeUsageSinkFactory{})
	h := &OpenAIGatewayHandler{applicationGateway: appGateway}

	h.GrokCountTokens(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]int
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Positive(t, body["input_tokens"])
}
