//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/runtimebridge"
	"github.com/Wei-Shaw/sub2api/internal/service"
	v1 "github.com/Wei-Shaw/sub2api/pkg/runtimebridge/v1"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type messagesExecutorSink struct {
	events []gatewayruntime.UsageEvent
}

type productionDriverFunc func(context.Context, v1.Request, runtimebridge.EventSink) (v1.Result, error)

func (f productionDriverFunc) Dispatch(ctx context.Context, request v1.Request, sink runtimebridge.EventSink) (v1.Result, error) {
	return f(ctx, request, sink)
}

func (s *messagesExecutorSink) RecordFinal(_ context.Context, event gatewayruntime.UsageEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestSub2APIMessagesExecutorSelectsAndFailsOverForGatewayProtocols(t *testing.T) {
	for _, endpoint := range []gatewayruntime.Endpoint{
		gatewayruntime.EndpointMessages,
		gatewayruntime.EndpointChatCompletions,
		gatewayruntime.EndpointResponses,
	} {
		t.Run(string(endpoint), func(t *testing.T) {
			attempted := make([]int64, 0, 2)
			executor := sub2APIMessagesExecutor{
				endpoint: endpoint,
				executeGateway: func(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
					route, ok := service.GatewayPlatformAssetContextFromContext(ctx)
					require.True(t, ok)
					require.Equal(t, int64(42), route.Platform.PlatformID)
					require.Equal(t, service.PlatformAnthropic, route.SchedulingScope.AccountPlatform)
					require.Equal(t, "claude-requested", route.Platform.RequestedModel)
					require.Equal(t, "claude-upstream", route.Platform.UpstreamModel)
					for _, accountID := range []int64{101, 202} {
						attempted = append(attempted, accountID)
						if accountID == 101 {
							continue
						}
						event := gatewayruntime.UsageEvent{
							RequestID: request.RequestID,
							Success:   true,
							Facts: gatewayruntime.UsageFacts{
								AccountID:        accountID,
								UpstreamEndpoint: runtimeEndpointCapability(endpoint),
								UpstreamModel:    request.UpstreamModel,
							},
						}
						require.NoError(t, sink.RecordFinal(ctx, event))
						return gatewayruntime.Result{StatusCode: http.StatusOK, AccountID: accountID}, nil
					}
					return gatewayruntime.Result{}, errors.New("unreachable")
				},
			}
			sink := &messagesExecutorSink{}
			adapter := NewSub2APIRuntimeAdapter(map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor{endpoint: executor})

			result, err := adapter.Dispatch(context.Background(), gatewayruntime.Request{
				RequestID:      "req-failover-" + string(endpoint),
				PlatformID:     42,
				PlatformCode:   "anthropic-main",
				Adapter:        service.PlatformAnthropic,
				Endpoint:       endpoint,
				RequestedModel: "claude-requested",
				UpstreamModel:  "claude-upstream",
			}, sink)

			require.NoError(t, err)
			require.Equal(t, []int64{101, 202}, attempted)
			require.Equal(t, int64(202), result.AccountID)
			require.Len(t, sink.events, 1)
			require.True(t, sink.events[0].Success)
			require.Equal(t, int64(202), sink.events[0].Facts.AccountID)
			require.Equal(t, runtimeEndpointCapability(endpoint), sink.events[0].Facts.UpstreamEndpoint)
			require.Equal(t, "claude-upstream", sink.events[0].Facts.UpstreamModel)
		})
	}
}

func TestSub2APIMessagesExecutorRecordsStreamingSuccessExactlyOnce(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	exchange := NewGinHTTPExchange(c)
	executor := sub2APIMessagesExecutor{
		endpoint: gatewayruntime.EndpointMessages,
		executeGateway: func(ctx context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
			request.Exchange.WriteHeader(http.StatusOK)
			_, err := request.Exchange.Write([]byte("event: message_start\n\n"))
			require.NoError(t, err)
			request.Exchange.Flush()
			event := gatewayruntime.UsageEvent{
				RequestID: request.RequestID,
				Success:   true,
				Facts: gatewayruntime.UsageFacts{
					AccountID:                303,
					RequestWasClientStream:   true,
					ResponseWasPartiallySent: true,
				},
			}
			require.NoError(t, sink.RecordFinal(ctx, event))
			require.ErrorIs(t, sink.RecordFinal(ctx, event), gatewayruntime.ErrTerminalAlreadyRecorded)
			return gatewayruntime.Result{
				StatusCode: http.StatusOK,
				AccountID:  303,
				Response:   gatewayruntime.Response{Streamed: true},
			}, nil
		},
	}
	sink := &messagesExecutorSink{}
	adapter := NewSub2APIRuntimeAdapter(map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor{
		gatewayruntime.EndpointMessages: executor,
	})

	result, err := adapter.Dispatch(context.Background(), gatewayruntime.Request{
		RequestID:  "req-stream-success",
		PlatformID: 42,
		Adapter:    service.PlatformAnthropic,
		Endpoint:   gatewayruntime.EndpointMessages,
		Stream:     true,
		Exchange:   exchange,
	}, sink)

	require.NoError(t, err)
	require.True(t, result.Response.Streamed)
	require.Contains(t, recorder.Body.String(), "event: message_start")
	require.Len(t, sink.events, 1)
	require.True(t, sink.events[0].Facts.ResponseWasPartiallySent)
}

func TestSub2APIMessagesExecutorAllAccountsFailedDoesNotRecordSuccessfulUsage(t *testing.T) {
	upstreamErr := &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway}
	executor := sub2APIMessagesExecutor{
		endpoint: gatewayruntime.EndpointResponses,
		executeGateway: func(context.Context, gatewayruntime.Request, gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
			return gatewayruntime.Result{StatusCode: http.StatusBadGateway}, upstreamErr
		},
	}
	sink := &messagesExecutorSink{}
	adapter := NewSub2APIRuntimeAdapter(map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor{
		gatewayruntime.EndpointResponses: executor,
	})

	_, err := adapter.Dispatch(context.Background(), gatewayruntime.Request{
		RequestID:  "req-all-failed",
		PlatformID: 42,
		Adapter:    service.PlatformAnthropic,
		Endpoint:   gatewayruntime.EndpointResponses,
	}, sink)

	require.ErrorIs(t, err, upstreamErr)
	require.Len(t, sink.events, 1)
	require.False(t, sink.events[0].Success)
	require.NotNil(t, sink.events[0].Error)
	require.Equal(t, gatewayruntime.ErrorUpstream5xx, sink.events[0].Error.Category)
}

func TestSub2APIMessagesExecutorBuildsSchedulingRouteFromRuntimeRequest(t *testing.T) {
	route := runtimeCompatibilityRoute(gatewayruntime.Request{
		PlatformID:     42,
		PlatformCode:   "openai-main",
		Adapter:        "openai",
		Endpoint:       gatewayruntime.EndpointResponses,
		RequestedModel: "gpt-5.6",
		UpstreamModel:  "gpt-5.6-2026",
	})

	require.NotNil(t, route)
	require.Equal(t, int64(42), route.Platform.PlatformID)
	require.Equal(t, service.PlatformOpenAI, route.SchedulingScope.AccountPlatform)
	require.Equal(t, []string{"responses"}, route.Platform.EndpointCapabilities)
}

func TestSub2APIMessagesExecutorUsesPureDriverForOpenAIProductionRoute(t *testing.T) {
	sink := &messagesExecutorSink{}
	executor := sub2APIMessagesExecutor{
		endpoint: gatewayruntime.EndpointResponses,
		openAIDriver: productionDriverFunc(func(ctx context.Context, request v1.Request, events runtimebridge.EventSink) (v1.Result, error) {
			if request.Endpoint != v1.EndpointResponses || request.Platform.RuntimeAdapter != service.PlatformOpenAI {
				t.Fatalf("contract request = %#v, want OpenAI responses route", request)
			}
			if err := events.Publish(ctx, v1.Event{Sequence: 1, Kind: v1.EventUsageFinal, Usage: &v1.UsageFacts{AccountID: 202, TerminalStatus: "success"}}); err != nil {
				return v1.Result{}, err
			}
			return v1.Result{StatusCode: http.StatusOK, AccountID: 202}, nil
		}),
	}
	result, err := executor.Execute(context.Background(), gatewayruntime.Request{
		RequestID:      "pure-production-route",
		PlatformID:     42,
		PlatformCode:   "openai-main",
		Adapter:        service.PlatformOpenAI,
		Endpoint:       gatewayruntime.EndpointResponses,
		RequestedModel: "gpt-test",
	}, sink)
	if err != nil || result.AccountID != 202 || len(sink.events) != 1 || !sink.events[0].Success {
		t.Fatalf("pure production result/error/events = %#v/%v/%#v", result, err, sink.events)
	}
}
