//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSub2APIAuxiliaryExecutorOnlyOwnsLive(t *testing.T) {
	executor := sub2APIAuxiliaryExecutor{openAIHandler: &OpenAIGatewayHandler{}, endpoint: gatewayruntime.EndpointLive}
	require.NotNil(t, executor.handlerFor(gatewayruntime.Request{}, service.PlatformOpenAI))
	for _, endpoint := range []gatewayruntime.Endpoint{
		gatewayruntime.EndpointGeminiNative,
		gatewayruntime.EndpointImages,
		gatewayruntime.EndpointVideos,
	} {
		executor.endpoint = endpoint
		require.Nil(t, executor.handlerFor(gatewayruntime.Request{}, service.PlatformOpenAI))
	}
}

func TestSub2APIGeminiMediaExecutorOwnsGeminiAndMediaFamilies(t *testing.T) {
	for _, endpoint := range []gatewayruntime.Endpoint{
		gatewayruntime.EndpointGeminiNative,
		gatewayruntime.EndpointImages,
		gatewayruntime.EndpointVideos,
	} {
		executor := sub2APIGeminiMediaExecutor{endpoint: endpoint}
		require.Equal(t, endpoint, executor.endpoint)
	}
}

func TestSub2APISyncExecutorOwnsSynchronousEndpointFamilies(t *testing.T) {
	for _, endpoint := range []gatewayruntime.Endpoint{
		gatewayruntime.EndpointCountTokens,
		gatewayruntime.EndpointEmbeddings,
		gatewayruntime.EndpointAlphaSearch,
	} {
		t.Run(string(endpoint), func(t *testing.T) {
			executor := sub2APISyncExecutor{endpoint: endpoint}
			require.NotNil(t, executor)
			require.Equal(t, endpoint, executor.endpoint)
		})
	}
}
