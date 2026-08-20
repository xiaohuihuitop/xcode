//go:build unit

package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/stretchr/testify/require"
)

type runtimeExecutorStub struct {
	called  bool
	request gatewayruntime.Request
	sink    gatewayruntime.UsageSink
}

type runtimeExecutorWithoutTerminalStub struct{}

func (runtimeExecutorWithoutTerminalStub) Execute(context.Context, gatewayruntime.Request, gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	return gatewayruntime.Result{StatusCode: 200}, nil
}

func (s *runtimeExecutorStub) Execute(_ context.Context, request gatewayruntime.Request, sink gatewayruntime.UsageSink) (gatewayruntime.Result, error) {
	s.called = true
	s.request = request
	s.sink = sink
	if err := sink.RecordFinal(context.Background(), gatewayruntime.UsageEvent{RequestID: request.RequestID, Success: true}); err != nil {
		return gatewayruntime.Result{}, err
	}
	return gatewayruntime.Result{StatusCode: 200, AccountID: 17}, nil
}

type runtimeSinkStub struct {
	calls int
}

func (s *runtimeSinkStub) RecordFinal(context.Context, gatewayruntime.UsageEvent) error {
	s.calls++
	return nil
}

func TestSub2APIRuntimeAdapterDispatchesRegisteredExecutorAndRecordsOnce(t *testing.T) {
	executor := &runtimeExecutorStub{}
	adapter := NewSub2APIRuntimeAdapter(map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor{
		gatewayruntime.EndpointResponses: executor,
	})
	sink := &runtimeSinkStub{}

	result, err := adapter.Dispatch(context.Background(), gatewayruntime.Request{
		RequestID: "adapter-request-1",
		Endpoint:  gatewayruntime.EndpointResponses,
	}, sink)

	require.NoError(t, err)
	require.Equal(t, 200, result.StatusCode)
	require.True(t, executor.called)
	require.Equal(t, gatewayruntime.EndpointResponses, executor.request.Endpoint)
	require.NotNil(t, executor.sink)
	require.Equal(t, 1, sink.calls)
}

func TestSub2APIRuntimeAdapterRejectsUnregisteredEndpoint(t *testing.T) {
	adapter := NewSub2APIRuntimeAdapter(nil)

	_, err := adapter.Dispatch(context.Background(), gatewayruntime.Request{Endpoint: gatewayruntime.EndpointMessages}, &runtimeSinkStub{})

	require.ErrorIs(t, err, ErrSub2APIRuntimeEndpointUnavailable)
}

func TestSub2APIRuntimeAdapterRejectsMissingTerminalUsage(t *testing.T) {
	adapter := NewSub2APIRuntimeAdapter(map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor{
		gatewayruntime.EndpointResponses: runtimeExecutorWithoutTerminalStub{},
	})

	_, err := adapter.Dispatch(context.Background(), gatewayruntime.Request{
		RequestID: "adapter-request-without-terminal",
		Endpoint:  gatewayruntime.EndpointResponses,
	}, &runtimeSinkStub{})

	require.ErrorIs(t, err, ErrSub2APIRuntimeTerminalMissing)
}
