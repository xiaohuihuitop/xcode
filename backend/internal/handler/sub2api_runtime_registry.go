package handler

import (
	"errors"
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/gatewayruntime"
)

var (
	ErrSub2APIRuntimeDuplicateEndpoint = errors.New("sub2api runtime endpoint already registered")
	ErrSub2APIRuntimeInvalidEndpoint   = errors.New("sub2api runtime endpoint is invalid")
)

// Sub2APIRuntimeRegistry owns the endpoint-to-executor mapping used by the
// production adapter. Keeping registration separate makes missing and
// duplicate endpoint wiring visible in tests instead of silently selecting a
// different protocol implementation.
type Sub2APIRuntimeRegistry struct {
	executors map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor
}

func NewSub2APIRuntimeRegistry() *Sub2APIRuntimeRegistry {
	return &Sub2APIRuntimeRegistry{executors: make(map[gatewayruntime.Endpoint]Sub2APIEndpointExecutor)}
}

func (r *Sub2APIRuntimeRegistry) Register(endpoint gatewayruntime.Endpoint, executor Sub2APIEndpointExecutor) error {
	if r == nil || r.executors == nil {
		return ErrSub2APIRuntimeUnavailable
	}
	if !isRegisteredRuntimeEndpoint(endpoint) || executor == nil {
		return ErrSub2APIRuntimeInvalidEndpoint
	}
	if _, exists := r.executors[endpoint]; exists {
		return ErrSub2APIRuntimeDuplicateEndpoint
	}
	r.executors[endpoint] = executor
	return nil
}

func (r *Sub2APIRuntimeRegistry) Endpoints() []gatewayruntime.Endpoint {
	if r == nil {
		return nil
	}
	endpoints := make([]gatewayruntime.Endpoint, 0, len(r.executors))
	for endpoint := range r.executors {
		endpoints = append(endpoints, endpoint)
	}
	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i] < endpoints[j] })
	return endpoints
}

func (r *Sub2APIRuntimeRegistry) Adapter() (*Sub2APIRuntimeAdapter, error) {
	if r == nil || len(r.executors) == 0 {
		return nil, ErrSub2APIRuntimeUnavailable
	}
	return NewSub2APIRuntimeAdapter(r.executors), nil
}

func isRegisteredRuntimeEndpoint(endpoint gatewayruntime.Endpoint) bool {
	switch endpoint {
	case gatewayruntime.EndpointMessages,
		gatewayruntime.EndpointChatCompletions,
		gatewayruntime.EndpointResponses,
		gatewayruntime.EndpointGeminiNative,
		gatewayruntime.EndpointEmbeddings,
		gatewayruntime.EndpointAlphaSearch,
		gatewayruntime.EndpointImages,
		gatewayruntime.EndpointVideos,
		gatewayruntime.EndpointCountTokens,
		gatewayruntime.EndpointResponsesInputTokens,
		gatewayruntime.EndpointLive,
		gatewayruntime.EndpointWebSocket:
		return true
	default:
		return false
	}
}

var _ gatewayruntime.GatewayRuntime = (*Sub2APIRuntimeAdapter)(nil)
