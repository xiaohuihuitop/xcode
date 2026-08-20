//go:build unit

package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/stretchr/testify/require"
)

func TestSub2APIRuntimeRegistryRejectsDuplicateAndUnknownEndpoints(t *testing.T) {
	registry := NewSub2APIRuntimeRegistry()
	executor := &runtimeExecutorStub{}

	require.NoError(t, registry.Register(gatewayruntime.EndpointResponses, executor))
	require.ErrorIs(t, registry.Register(gatewayruntime.EndpointResponses, executor), ErrSub2APIRuntimeDuplicateEndpoint)
	require.ErrorIs(t, registry.Register(gatewayruntime.Endpoint("unknown"), executor), ErrSub2APIRuntimeInvalidEndpoint)
	require.ErrorIs(t, registry.Register(gatewayruntime.EndpointChatCompletions, nil), ErrSub2APIRuntimeInvalidEndpoint)

	adapter, err := registry.Adapter()
	require.NoError(t, err)
	require.NotNil(t, adapter)
	_, err = adapter.Dispatch(context.Background(), gatewayruntime.Request{Endpoint: gatewayruntime.EndpointResponses}, &runtimeSinkStub{})
	require.NoError(t, err)
}

func TestSub2APIRuntimeRegistryRejectsEmptyAdapter(t *testing.T) {
	adapter, err := NewSub2APIRuntimeRegistry().Adapter()
	require.ErrorIs(t, err, ErrSub2APIRuntimeUnavailable)
	require.Nil(t, adapter)
}
