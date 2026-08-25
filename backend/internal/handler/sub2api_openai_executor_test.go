//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	sub2api "github.com/Wei-Shaw/sub2api/internal/runtime/sub2api"
	"github.com/Wei-Shaw/sub2api/internal/service"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIExecutorSink struct {
	events []gatewayruntime.UsageEvent
}

func (s *openAIExecutorSink) RecordFinal(_ context.Context, event gatewayruntime.UsageEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestSub2APIOpenAIPortRejectsUnavailablePricingBeforeAccountSelection(t *testing.T) {
	cfg := &config.Config{}
	billing := service.NewBillingService(cfg, nil)
	resolver := service.NewModelPricingResolver(billing)
	gateway := service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, cfg, nil, nil, billing,
		nil, nil, nil, nil, nil, nil, resolver, nil, nil, nil,
	)
	ctx := service.WithGatewayPlatformAssetContext(context.Background(), &service.GatewayPlatformAssetContext{
		Platform: &service.ResolvedPlatformModel{
			PlatformID: 1, PlatformCode: "codex", AccountPlatform: service.PlatformOpenAI,
			RequestedModel: "pricing-missing-model", UpstreamModel: "pricing-missing-model",
		},
		SchedulingScope: service.PlatformSchedulingScope{
			PlatformID: 1, PlatformCode: "codex", AccountPlatform: service.PlatformOpenAI,
		},
	})
	port := &sub2APIOpenAIPort{service: gateway}
	for _, endpoint := range []v1.Endpoint{
		v1.EndpointChatCompletions,
		v1.EndpointResponses,
		v1.EndpointMessages,
	} {
		t.Run(string(endpoint), func(t *testing.T) {
			_, err := port.Select(ctx, v1.Request{
				ContractVersion: v1.ContractVersionV1,
				Platform: v1.PlatformRoute{
					ID: 1, Code: "codex", RuntimeAdapter: service.PlatformOpenAI,
					RequestedModel: "pricing-missing-model", UpstreamModel: "pricing-missing-model",
				},
				Endpoint: endpoint,
			}, nil, string(endpoint))

			require.ErrorIs(t, err, service.ErrModelPricingUnavailable)
			var retryErr *sub2api.RetryError
			require.False(t, errors.As(err, &retryErr))
		})
	}
}

func TestSub2APIOpenAIExecutorDispatchesRegisteredProtocol(t *testing.T) {
	for _, endpoint := range []gatewayruntime.Endpoint{
		gatewayruntime.EndpointMessages,
		gatewayruntime.EndpointChatCompletions,
		gatewayruntime.EndpointResponses,
	} {
		t.Run(string(endpoint), func(t *testing.T) {
			sink := &openAIExecutorSink{}
			executor := sub2APIOpenAIExecutor{
				execute: func(context.Context, gatewayruntime.Request, gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
					return gatewayruntime.Result{StatusCode: http.StatusOK}, nil
				},
			}
			result, err := executor.Execute(context.Background(), gatewayruntime.Request{Endpoint: endpoint}, sink)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, result.StatusCode)
			require.Len(t, sink.events, 1)
			require.True(t, sink.events[0].Success)
		})
	}
}

func TestSub2APIOpenAIExecutorRuntimeFailoverRecordsSelectedEndpointOnce(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	exchange := NewGinHTTPExchange(c)

	attempted := make([]int64, 0, 2)
	executor := sub2APIOpenAIExecutor{
		endpoint: gatewayruntime.EndpointChatCompletions,
		selectAccountRuntime: func(_ context.Context, _ gatewayruntime.Request, excluded map[int64]struct{}, _ service.OpenAIEndpointCapability) (*service.AccountSelectionResult, error) {
			for _, id := range []int64{101, 202} {
				if _, skip := excluded[id]; skip {
					continue
				}
				return &service.AccountSelectionResult{Account: &service.Account{ID: id, Platform: service.PlatformOpenAI}}, nil
			}
			return nil, service.ErrNoAvailableAccounts
		},
		forwardRuntime: func(_ context.Context, request gatewayruntime.Request, account *service.Account) (*service.OpenAIForwardResult, error) {
			attempted = append(attempted, account.ID)
			if account.ID == 101 {
				request.Exchange.SetState("openai_actual_upstream_endpoint", "/v1/responses")
				return nil, &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway}
			}
			require.Empty(t, service.ActualOpenAIUpstreamEndpointFromExchange(request.Exchange))
			request.Exchange.SetState("openai_actual_upstream_endpoint", "/v1/chat/completions")
			request.Exchange.WriteHeader(http.StatusOK)
			_, err := request.Exchange.Write([]byte(`{"ok":true}`))
			return &service.OpenAIForwardResult{
				Model:            "gpt-5.6",
				BillingModel:     "gpt-5.6-billing",
				UpstreamModel:    "gpt-5.6",
				UpstreamEndpoint: "/v1/chat/completions",
				Usage:            service.OpenAIUsage{InputTokens: 3, OutputTokens: 2},
			}, err
		},
	}
	sink := &openAIExecutorSink{}

	result, err := executor.Execute(context.Background(), gatewayruntime.Request{
		RequestID:      "req-openai-runtime-failover",
		Endpoint:       gatewayruntime.EndpointChatCompletions,
		Adapter:        service.PlatformOpenAI,
		RequestedModel: "gpt-5.6",
		UpstreamModel:  "gpt-5.6",
		Exchange:       exchange,
	}, sink)

	require.NoError(t, err)
	require.Equal(t, []int64{101, 202}, attempted)
	require.Equal(t, int64(202), result.AccountID)
	require.Equal(t, "/v1/chat/completions", result.UpstreamEndpoint)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, `{"ok":true}`, recorder.Body.String())
	require.Len(t, sink.events, 1)
	require.True(t, sink.events[0].Success)
	require.Equal(t, int64(202), sink.events[0].Facts.AccountID)
	require.Equal(t, "/v1/chat/completions", sink.events[0].Facts.UpstreamEndpoint)
	require.Equal(t, 3, sink.events[0].Facts.InputTokens)
	require.Equal(t, 2, sink.events[0].Facts.OutputTokens)
	require.Equal(t, "gpt-5.6-billing", sink.events[0].Facts.BillingModel)
	require.False(t, errors.Is(err, service.ErrNoAvailableAccounts))
}
