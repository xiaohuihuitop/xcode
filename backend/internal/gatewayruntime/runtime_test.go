//go:build unit

package gatewayruntime

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type recordingUsageSink struct {
	events []UsageEvent
}

func (s *recordingUsageSink) RecordFinal(_ context.Context, event UsageEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestTerminalRecorderRecordsFinalUsageExactlyOnce(t *testing.T) {
	sink := &recordingUsageSink{}
	recorder := NewTerminalRecorder(sink)
	event := UsageEvent{RequestID: "runtime-terminal-1", Success: true}

	require.NoError(t, recorder.RecordFinal(context.Background(), event))
	require.ErrorIs(t, recorder.RecordFinal(context.Background(), event), ErrTerminalAlreadyRecorded)
	require.Len(t, sink.events, 1)
}

func TestRuntimeErrorDoesNotExposeSensitiveDetails(t *testing.T) {
	err := NewRuntimeError(ErrorUpstreamForbidden, false, "upstream request denied")

	require.Equal(t, ErrorUpstreamForbidden, err.Category)
	require.NotContains(t, err.Error(), "access_token")
	require.NotContains(t, err.Error(), "Authorization")
}

func TestRuntimeErrorFromStatusAndContext(t *testing.T) {
	for _, test := range []struct {
		status   int
		category ErrorCategory
	}{
		{http.StatusUnauthorized, ErrorCredentialInvalid},
		{http.StatusForbidden, ErrorUpstreamForbidden},
		{http.StatusTooManyRequests, ErrorRateLimited},
		{http.StatusBadGateway, ErrorUpstream5xx},
		{http.StatusServiceUnavailable, ErrorUpstream5xx},
		{http.StatusRequestTimeout, ErrorUpstreamTimeout},
	} {
		require.Equal(t, test.category, RuntimeErrorFromStatus(test.status, "upstream").Category)
	}
	require.Equal(t, ErrorClientCancelled, RuntimeErrorFromContext(context.Canceled).Category)
	require.Equal(t, ErrorClientCancelled, RuntimeErrorFromContext(fmt.Errorf("wrapped: %w", context.Canceled)).Category)
	require.Equal(t, ErrorUpstreamTimeout, RuntimeErrorFromContext(context.DeadlineExceeded).Category)
}

func TestRuntimeContractsUseStandardLibraryTransportOnly(t *testing.T) {
	var _ GatewayRuntime = runtimeStub{}
	var _ TokenCounter = tokenCounterStub{}
	var _ AccountRuntime = accountRuntimeStub{}
	var _ PricingEngine = pricingEngineStub{}
	var _ UsageSink = (*recordingUsageSink)(nil)

	request := Request{
		Endpoint: EndpointResponses,
		Exchange: nil,
		Metadata: RequestMetadata{Headers: map[string]string{"Accept": "application/json"}},
	}
	require.Equal(t, EndpointResponses, request.Endpoint)
	require.Equal(t, http.MethodPost, http.MethodPost)
}

func TestUsageSinkContextRoundTrip(t *testing.T) {
	sink := &recordingUsageSink{}
	ctx := WithUsageSink(context.Background(), sink)
	got, ok := UsageSinkFromContext(ctx)
	require.True(t, ok)
	require.Same(t, sink, got)

	without, ok := UsageSinkFromContext(context.Background())
	require.False(t, ok)
	require.Nil(t, without)
}

type runtimeStub struct{}

func (runtimeStub) Dispatch(context.Context, Request, UsageSink) (Result, error) {
	return Result{}, nil
}

type tokenCounterStub struct{}

func (tokenCounterStub) CountTokens(context.Context, Request) (TokenCountResult, error) {
	return TokenCountResult{}, nil
}

type accountRuntimeStub struct{}

func (accountRuntimeStub) ProbeAccount(context.Context, AccountProbeRequest) (AccountProbeResult, error) {
	return AccountProbeResult{}, nil
}

type pricingEngineStub struct{}

func (pricingEngineStub) Quote(context.Context, PricingRequest) (PricingQuote, error) {
	return PricingQuote{}, nil
}
