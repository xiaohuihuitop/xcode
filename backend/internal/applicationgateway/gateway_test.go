//go:build unit

package applicationgateway

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/productcore"
	"github.com/stretchr/testify/require"
)

type decisionProviderStub struct {
	decision *productcore.Decision
	err      error
}

func (s decisionProviderStub) Resolve(context.Context, productcore.AccessGrant, productcore.Request) (*productcore.Decision, error) {
	return s.decision, s.err
}

type runtimeCaptureStub struct {
	called  bool
	request gatewayruntime.Request
	sink    gatewayruntime.UsageSink
}

func (s *runtimeCaptureStub) Dispatch(_ context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	s.called = true
	s.request = request
	s.sink = sink
	_ = sink.RecordFinal(context.Background(), gatewayruntime.UsageEvent{
		RequestID: request.RequestID,
		Success:   true,
	})
	return gatewayruntime.Result{StatusCode: 200}, nil
}

type runtimeWithoutTerminalStub struct{}

func (runtimeWithoutTerminalStub) Dispatch(context.Context, gatewayruntime.Request, gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	return gatewayruntime.Result{StatusCode: 200}, nil
}

type usageSinkFactoryStub struct {
	called   bool
	snapshot DecisionSnapshot
	sink     gatewayruntime.UsageSink
}

func (s *usageSinkFactoryStub) ForDecision(snapshot DecisionSnapshot) gatewayruntime.UsageSink {
	s.called = true
	s.snapshot = snapshot
	return s.sink
}

type noOpUsageSink struct{}

func (noOpUsageSink) RecordFinal(context.Context, gatewayruntime.UsageEvent) error { return nil }

func TestGatewayDispatchBindsProductDecisionToRuntimeRequest(t *testing.T) {
	runtime := &runtimeCaptureStub{}
	factory := &usageSinkFactoryStub{sink: &noOpUsageSink{}}
	decision := &productcore.Decision{
		Platform: productcore.Platform{
			ID:              42,
			Code:            "openai-main",
			AccountPlatform: "openai",
			RequestedModel:  "gpt-5.6",
			UpstreamModel:   "gpt-5.6-2026",
		},
		BillingAsset: &productcore.BillingAsset{
			Source:         "subscription",
			SubscriptionID: int64Ptr(9),
			RateMultiplier: 1.5,
		},
	}
	gateway := New(decisionProviderStub{decision: decision}, runtime, factory)

	result, err := gateway.Dispatch(context.Background(), DispatchRequest{
		Grant:   productcore.AccessGrant{KeyID: 7, UserID: 8},
		Product: productcore.Request{Model: "gpt-5.6", EndpointCapability: "responses"},
		Runtime: gatewayruntime.Request{
			RequestID:       "request-1",
			Endpoint:        gatewayruntime.EndpointResponses,
			InboundEndpoint: "/v1/responses",
			Payload:         []byte(`{"model":"gpt-5.6"}`),
		},
	})

	require.NoError(t, err)
	require.Equal(t, 200, result.StatusCode)
	require.True(t, runtime.called)
	require.Equal(t, int64(42), runtime.request.PlatformID)
	require.Equal(t, "openai", runtime.request.Adapter)
	require.Equal(t, "gpt-5.6-2026", runtime.request.UpstreamModel)
	require.Equal(t, "/v1/responses", runtime.request.InboundEndpoint)
	require.Equal(t, []byte(`{"model":"gpt-5.6"}`), runtime.request.Payload)
	require.True(t, factory.called)
	require.Equal(t, int64(9), *factory.snapshot.Decision.BillingAsset.SubscriptionID)
	require.NotContains(t, fmt.Sprintf("%#v", runtime.request), "subscription")
	require.IsType(t, &gatewayruntime.TerminalRecorder{}, runtime.sink)
}

func TestGatewayDispatchDoesNotCallRuntimeWhenDecisionFails(t *testing.T) {
	runtime := &runtimeCaptureStub{}
	factory := &usageSinkFactoryStub{sink: &noOpUsageSink{}}
	wantErr := errors.New("decision failed")
	gateway := New(decisionProviderStub{err: wantErr}, runtime, factory)

	_, err := gateway.Dispatch(context.Background(), DispatchRequest{})

	require.ErrorIs(t, err, wantErr)
	require.False(t, runtime.called)
	require.False(t, factory.called)
}

func TestGatewayDispatchRejectsMissingDependencies(t *testing.T) {
	_, err := New(nil, nil, nil).Dispatch(context.Background(), DispatchRequest{})
	require.ErrorIs(t, err, ErrGatewayUnavailable)
}

func TestGatewayDispatchRejectsMissingTerminalUsage(t *testing.T) {
	decision := &productcore.Decision{Platform: productcore.Platform{ID: 42, Code: "openai-main", AccountPlatform: "openai"}}
	gateway := New(
		decisionProviderStub{decision: decision},
		runtimeWithoutTerminalStub{},
		&usageSinkFactoryStub{sink: &noOpUsageSink{}},
	)

	_, err := gateway.Dispatch(context.Background(), DispatchRequest{
		Product: productcore.Request{Model: "gpt-5.6", EndpointCapability: "responses"},
		Runtime: gatewayruntime.Request{RequestID: "missing-terminal"},
	})

	require.ErrorIs(t, err, ErrGatewayTerminalMissing)
}

func TestGatewayUsesNonBillingSinkForCapabilityEndpoints(t *testing.T) {
	require.True(t, isNonBillingEndpoint(gatewayruntime.EndpointCountTokens))
	require.True(t, isNonBillingEndpoint(gatewayruntime.EndpointLive))
	require.True(t, isNonBillingEndpoint(gatewayruntime.EndpointWebSocket))
	require.False(t, isNonBillingEndpoint(gatewayruntime.EndpointResponses))
}

func int64Ptr(value int64) *int64 { return &value }
